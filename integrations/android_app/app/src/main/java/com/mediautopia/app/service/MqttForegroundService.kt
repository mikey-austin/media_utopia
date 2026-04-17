package com.mediautopia.app.service

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaStyleNotificationHelper
import com.mediautopia.app.MainActivity
import com.mediautopia.app.data.cache.QueueStore
import com.mediautopia.app.data.cache.SettingsDataStore
import com.mediautopia.app.data.mqtt.ConnectionState
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.domain.usecase.LeaseManager
import com.mediautopia.app.renderer.LocalRendererService
import dagger.hilt.android.AndroidEntryPoint
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import javax.inject.Inject

@AndroidEntryPoint
class MqttForegroundService : Service() {

    @Inject lateinit var mqttConnectionManager: MqttConnectionManager
    @Inject lateinit var commandCorrelator: CommandCorrelator
    @Inject lateinit var nodeRepository: NodeRepository
    @Inject lateinit var settingsDataStore: SettingsDataStore
    @Inject lateinit var rendererStateRepository: RendererStateRepository
    @Inject lateinit var queueStore: QueueStore
    @Inject lateinit var audioSessionHolder: com.mediautopia.app.renderer.AudioSessionHolder
    @Inject lateinit var leaseManager: LeaseManager
    @Inject lateinit var localTransport: com.mediautopia.app.data.transport.LocalTransport

    private val tag = "MqttForegroundService"
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var localRenderer: LocalRendererService? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private val sessionMutex = Mutex()
    private val reconnectTrigger = kotlinx.coroutines.channels.Channel<Unit>(
        capacity = kotlinx.coroutines.channels.Channel.CONFLATED
    )
    private var reconnectLoopJob: Job? = null

    companion object {
        private const val CHANNEL_ID = "mu_service"
        private const val MEDIA_CHANNEL_ID = "mu_media"
        private const val NOTIFICATION_ID = 1

        const val ACTION_RECONNECT = "com.mediautopia.app.action.RECONNECT"

        fun start(context: Context) {
            val intent = Intent(context, MqttForegroundService::class.java)
            context.startForegroundService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, MqttForegroundService::class.java)
            context.stopService(intent)
        }

        /**
         * Trigger a full hard reset of MQTT state: stop the local renderer,
         * clear every cached lease, drop all discovered nodes, disconnect
         * the broker connection, and re-initialise from scratch. Routed
         * through the foreground service so the whole pipeline — including
         * the local renderer — can be cleanly restarted.
         */
        fun reconnect(context: Context) {
            val intent = Intent(context, MqttForegroundService::class.java).apply {
                action = ACTION_RECONNECT
            }
            context.startForegroundService(intent)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private var isStarted = false

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Every call to startForegroundService() comes with a 5-second
        // promise to call startForeground(); failure crashes the process
        // with ForegroundServiceDidNotStartInTimeException. The OS does
        // not exempt subsequent intents to an already-foreground service,
        // so we must call startForeground() unconditionally on EVERY
        // onStartCommand. createNotificationChannels() is idempotent.
        createNotificationChannels()
        val statusText = when (mqttConnectionManager.connectionState.value) {
            ConnectionState.CONNECTED -> "Connected"
            ConnectionState.CONNECTING -> "Connecting..."
            ConnectionState.DISCONNECTED -> "Disconnected"
        }
        try {
            startForeground(NOTIFICATION_ID, buildServiceNotification(statusText))
        } catch (e: Exception) {
            Log.e(tag, "startForeground failed: ${e.message}")
            if (!isStarted) {
                stopSelf()
                return START_NOT_STICKY
            }
        }

        if (intent?.action == ACTION_RECONNECT) {
            if (isStarted) {
                Log.i(tag, "ACTION_RECONNECT received")
                mqttConnectionManager.markDisconnected()
                reconnectTrigger.trySend(Unit)
                return START_STICKY
            }
            // Fall through to normal startup.
        }

        if (isStarted) return START_STICKY

        isStarted = true

        // Single authoritative reconnect loop. Also runs the initial session
        // startup by observing the initial DISCONNECTED state.
        reconnectLoopJob = serviceScope.launch { reconnectLoop() }

        // Kick the loop once so the initial startSession runs even if the
        // state flow doesn't emit a fresh DISCONNECTED value at subscription
        // time.
        reconnectTrigger.trySend(Unit)

        // Observe connection state and update the notification text.
        serviceScope.launch {
            mqttConnectionManager.connectionState.collect { state ->
                val text = when (state) {
                    ConnectionState.CONNECTED -> "Connected"
                    ConnectionState.CONNECTING -> "Connecting..."
                    ConnectionState.DISCONNECTED -> "Disconnected"
                }
                val renderer = localRenderer
                if (renderer?.mediaSession == null) {
                    val manager = getSystemService(NotificationManager::class.java)
                    manager.notify(NOTIFICATION_ID, buildServiceNotification(text))
                }
            }
        }

        registerNetworkCallback()

        return START_STICKY
    }

    /**
     * Bring the full pipeline up from a fresh state: connect MQTT, set up
     * command correlator, reserve the local node id, start discovery, start
     * the local renderer, and kick off the background lease renewal loop.
     * Must be called from within [serviceScope], holding [sessionMutex].
     */
    private suspend fun startSession() {
        // Defensive guard: creating a second LocalRendererService while one
        // is already live would collide on the MediaSession id (which is
        // derived from the process pid). Callers must always tear down
        // first via stopSession().
        if (localRenderer != null) {
            Log.w(tag, "startSession called while localRenderer is non-null; skipping")
            return
        }

        val brokerUrl = settingsDataStore.brokerUrl.first()
        val clientId = "mu-android-${settingsDataStore.clientId.first()}"
        val identity = settingsDataStore.identity.first()

        // Reserve the local node id *before* starting discovery. This prevents
        // any retained presence from a previous session from being mistaken
        // for a remote renderer while the local renderer is still booting.
        nodeRepository.reserveLocalNodeId(ActiveRendererRepository.LOCAL_RENDERER_ID)

        mqttConnectionManager.connect(brokerUrl, clientId)

        commandCorrelator.setup(
            topicBase = MqttTopics.BASE,
            controllerId = clientId,
            identity = identity,
        )

        nodeRepository.startDiscovery()

        // Re-wire MQTT subscriptions for any renderer state flows that
        // survived from a previous session — their old subscription IDs
        // were wiped during stopSession and would otherwise never emit
        // again.
        rendererStateRepository.reobserveAll()

        // Start the local renderer so this phone appears as a
        // renderer on the network.
        val renderer = LocalRendererService(
            mqtt = mqttConnectionManager,
            nodeRepository = nodeRepository,
            rendererStateRepository = rendererStateRepository,
            context = this@MqttForegroundService,
            queueStore = queueStore,
            audioSessionHolder = audioSessionHolder,
            localTransport = localTransport,
            settingsDataStore = settingsDataStore,
        )

        // When renderer state changes, update the notification.
        renderer.onNotificationUpdate = { state ->
            updateMediaNotification(renderer, state)
        }

        renderer.start()
        localRenderer = renderer

        // Keep cached leases alive so the server-side lease never silently
        // expires on us — otherwise media controls start getting dropped
        // after the first 5 minutes of idle time.
        leaseManager.startRenewal(serviceScope)

        Log.i(tag, "MQTT connected, correlator set up, discovery and local renderer started")
    }

    /**
     * Tear down the session in reverse order of [startSession]. Safe to call
     * even if individual components never started — each step is best-effort.
     *
     * Callers must hold [sessionMutex] for the duration of this call to
     * serialise against concurrent [startSession] / reconnect attempts.
     */
    private suspend fun stopSession() {
        try {
            localRenderer?.stop()
        } catch (e: Exception) {
            Log.w(tag, "localRenderer.stop() threw: ${e.message}")
        }
        localRenderer = null

        try {
            leaseManager.clearAll()
        } catch (e: Exception) {
            Log.w(tag, "leaseManager.clearAll() threw: ${e.message}")
        }

        try {
            nodeRepository.stopDiscovery()
        } catch (_: Exception) {}

        try {
            nodeRepository.clearAll()
        } catch (_: Exception) {}

        try {
            commandCorrelator.cleanup()
        } catch (_: Exception) {}

        try {
            mqttConnectionManager.disconnect()
        } catch (e: Exception) {
            Log.w(tag, "Disconnect during stopSession threw: ${e.message}")
        }

        // Belt-and-braces: drop any lingering subscriptions from the map so
        // nothing auto-resubscribes to zombie topics on the next connect.
        mqttConnectionManager.clearSubscriptions()
    }

    /**
     * Single authoritative reconnect loop. Performs the initial startSession
     * and every subsequent rebuild, driven by [reconnectTrigger]. The trigger
     * is poked on any DISCONNECTED transition (via a state observer launched
     * here) and on explicit external events (UI reconnect button, network-
     * available callback). Exponential backoff grows on failed attempts and
     * resets to 2s once a connection stays CONNECTED for 60s.
     */
    private suspend fun reconnectLoop() = coroutineScope {
        val backoffMs = java.util.concurrent.atomic.AtomicLong(2_000L)

        // Side observer: parent it to this coroutine so cancellation of the
        // reconnect loop also tears down the observer.
        launch {
            mqttConnectionManager.connectionState
                // StateFlow de-duplicates equal emissions natively. If this
                // is ever migrated to SharedFlow, restore distinctUntilChanged.
                .collect { state ->
                    when (state) {
                        ConnectionState.DISCONNECTED -> reconnectTrigger.trySend(Unit)
                        ConnectionState.CONNECTED -> {
                            launch {
                                delay(60_000)
                                if (mqttConnectionManager.connectionState.value == ConnectionState.CONNECTED) {
                                    backoffMs.set(2_000L)
                                    Log.i(tag, "Connection stable 60s; backoff reset")
                                }
                            }
                        }
                        ConnectionState.CONNECTING -> {}
                    }
                }
        }

        // Main reconnect driver.
        for (ignored in reconnectTrigger) {
            if (!isStarted) continue
            val cs = mqttConnectionManager.connectionState.value
            if (cs == ConnectionState.CONNECTED || cs == ConnectionState.CONNECTING) continue

            Log.i(tag, "Reconnect trigger; waiting ${backoffMs.get()}ms before attempt")
            delay(backoffMs.get())
            val cs2 = mqttConnectionManager.connectionState.value
            if (cs2 == ConnectionState.CONNECTED || cs2 == ConnectionState.CONNECTING) continue

            sessionMutex.withLock {
                try {
                    if (localRenderer != null) {
                        stopSession()
                    }
                    if (isStarted) {
                        startSession()
                    }
                } catch (e: Exception) {
                    Log.e(tag, "Reconnect attempt failed: ${e.message}", e)
                }
            }

            if (mqttConnectionManager.connectionState.value != ConnectionState.CONNECTED) {
                backoffMs.set((backoffMs.get() * 2).coerceAtMost(30_000L))
            }
        }
    }

    override fun onDestroy() {
        Log.i(tag, "Service destroying, cleaning up")
        unregisterNetworkCallback()
        localRenderer?.stop()
        localRenderer = null
        nodeRepository.stopDiscovery()
        commandCorrelator.cleanup()

        serviceScope.launch {
            try {
                mqttConnectionManager.disconnect()
            } catch (e: Exception) {
                Log.e(tag, "Error disconnecting MQTT: ${e.message}", e)
            }
        }

        serviceScope.cancel()
        super.onDestroy()
    }

    private fun registerNetworkCallback() {
        val cm = getSystemService(ConnectivityManager::class.java) ?: return
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                Log.i(tag, "Network available")
                reconnectTrigger.trySend(Unit)
            }
        }
        networkCallback = callback
        cm.registerNetworkCallback(request, callback)
    }

    private fun unregisterNetworkCallback() {
        networkCallback?.let { cb ->
            try {
                val cm = getSystemService(ConnectivityManager::class.java)
                cm?.unregisterNetworkCallback(cb)
            } catch (_: Exception) {}
        }
        networkCallback = null
    }

    private fun updateMediaNotification(renderer: LocalRendererService, state: RendererState) {
        val session = renderer.mediaSession ?: return
        val manager = getSystemService(NotificationManager::class.java)

        val status = state.playback?.status ?: "stopped"
        if (status == "stopped" && state.current == null) {
            // No media playing — revert to simple service notification.
            manager.notify(NOTIFICATION_ID, buildServiceNotification("Connected"))
            return
        }

        val notification = buildMediaNotification(session, state)
        manager.notify(NOTIFICATION_ID, notification)
    }

    private fun buildMediaNotification(session: MediaSession, state: RendererState): android.app.Notification {
        val contentIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        val status = state.playback?.status ?: "stopped"
        val title = state.current?.metadata?.let { meta ->
            (meta["title"] as? kotlinx.serialization.json.JsonPrimitive)?.content
        } ?: "Media Utopia"
        val artist = state.current?.metadata?.let { meta ->
            (meta["artist"] as? kotlinx.serialization.json.JsonPrimitive)?.content
        }

        return NotificationCompat.Builder(this, MEDIA_CHANNEL_ID)
            .setContentTitle(title)
            .setContentText(artist ?: if (status == "playing") "Playing" else "Paused")
            .setSmallIcon(android.R.drawable.ic_media_play)
            .setContentIntent(contentIntent)
            .setOngoing(status == "playing")
            .setCategory(NotificationCompat.CATEGORY_TRANSPORT)
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .setStyle(MediaStyleNotificationHelper.MediaStyle(session))
            .setSilent(true)
            .build()
    }

    private fun createNotificationChannels() {
        val manager = getSystemService(NotificationManager::class.java)

        // Service channel (low priority, connection status).
        val serviceChannel = NotificationChannel(
            CHANNEL_ID,
            "Media Utopia Service",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "Keeps the MQTT connection alive"
            setSound(null, null)
        }
        manager.createNotificationChannel(serviceChannel)

        // Media channel (default priority, playback controls).
        val mediaChannel = NotificationChannel(
            MEDIA_CHANNEL_ID,
            "Media Utopia Playback",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "Media playback controls"
            setSound(null, null)
        }
        manager.createNotificationChannel(mediaChannel)
    }

    private fun buildServiceNotification(text: String) =
        NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("Media Utopia")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_media_play)
            .setOngoing(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setSilent(true)
            .build()
}
