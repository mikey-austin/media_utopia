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
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.renderer.LocalRendererService
import dagger.hilt.android.AndroidEntryPoint
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import javax.inject.Inject

@AndroidEntryPoint
class MqttForegroundService : Service() {

    @Inject lateinit var mqttConnectionManager: MqttConnectionManager
    @Inject lateinit var commandCorrelator: CommandCorrelator
    @Inject lateinit var nodeRepository: NodeRepository
    @Inject lateinit var settingsDataStore: SettingsDataStore
    @Inject lateinit var rendererStateRepository: RendererStateRepository
    @Inject lateinit var queueStore: QueueStore

    private val tag = "MqttForegroundService"
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var localRenderer: LocalRendererService? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null

    companion object {
        private const val CHANNEL_ID = "mu_service"
        private const val MEDIA_CHANNEL_ID = "mu_media"
        private const val NOTIFICATION_ID = 1

        fun start(context: Context) {
            val intent = Intent(context, MqttForegroundService::class.java)
            context.startForegroundService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, MqttForegroundService::class.java)
            context.stopService(intent)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private var isStarted = false

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (isStarted) return START_STICKY

        createNotificationChannels()
        try {
            startForeground(NOTIFICATION_ID, buildServiceNotification("Connecting..."))
        } catch (e: Exception) {
            Log.e(tag, "startForeground failed: ${e.message}")
            stopSelf()
            return START_NOT_STICKY
        }
        isStarted = true

        serviceScope.launch {
            try {
                val brokerUrl = settingsDataStore.brokerUrl.first()
                val clientId = "mu-android-${settingsDataStore.clientId.first()}"
                val identity = settingsDataStore.identity.first()

                mqttConnectionManager.connect(brokerUrl, clientId)

                commandCorrelator.setup(
                    topicBase = MqttTopics.BASE,
                    controllerId = clientId,
                    identity = identity,
                )

                nodeRepository.startDiscovery()

                // Start the local renderer so this phone appears as a
                // renderer on the network.
                val renderer = LocalRendererService(
                    mqtt = mqttConnectionManager,
                    nodeRepository = nodeRepository,
                    rendererStateRepository = rendererStateRepository,
                    context = this@MqttForegroundService,
                    queueStore = queueStore,
                )

                // When renderer state changes, update the notification.
                renderer.onNotificationUpdate = { state ->
                    updateMediaNotification(renderer, state)
                }

                renderer.start()
                localRenderer = renderer

                Log.i(tag, "MQTT connected, correlator set up, discovery and local renderer started")
            } catch (e: Exception) {
                Log.e(tag, "Failed to start MQTT session: ${e.message}", e)
            }
        }

        // Monitor network changes — trigger MQTT reconnect when network is regained.
        registerNetworkCallback()

        // Observe connection state and update the notification text.
        serviceScope.launch {
            mqttConnectionManager.connectionState.collect { state ->
                val text = when (state) {
                    ConnectionState.CONNECTED -> "Connected"
                    ConnectionState.CONNECTING -> "Connecting..."
                    ConnectionState.RECONNECTING -> "Reconnecting..."
                    ConnectionState.DISCONNECTED -> "Disconnected"
                }
                // Only update with service notification when no media is playing.
                val renderer = localRenderer
                if (renderer?.mediaSession == null) {
                    val manager = getSystemService(NotificationManager::class.java)
                    manager.notify(NOTIFICATION_ID, buildServiceNotification(text))
                }
            }
        }

        return START_STICKY
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
                Log.i(tag, "Network available, checking MQTT connection")
                if (mqttConnectionManager.connectionState.value == ConnectionState.DISCONNECTED) {
                    serviceScope.launch {
                        try {
                            val brokerUrl = settingsDataStore.brokerUrl.first()
                            val clientId = "mu-android-${settingsDataStore.clientId.first()}"
                            val identity = settingsDataStore.identity.first()
                            mqttConnectionManager.connect(brokerUrl, clientId)
                            commandCorrelator.setup(
                                topicBase = MqttTopics.BASE,
                                controllerId = clientId,
                                identity = identity,
                            )
                            nodeRepository.stopDiscovery()
                            nodeRepository.startDiscovery()
                            Log.i(tag, "Reconnected after network change")
                        } catch (e: Exception) {
                            Log.w(tag, "Reconnect on network change failed: ${e.message}")
                        }
                    }
                }
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
