package com.mediautopia.app.renderer

import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.media3.session.MediaSession
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.protocol.CommandEnvelope
import com.mediautopia.app.data.protocol.Lease
import com.mediautopia.app.data.protocol.PlaybackSeekBody
import com.mediautopia.app.data.protocol.Presence
import com.mediautopia.app.data.protocol.ReplyEnvelope
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.domain.model.Node
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.encodeToJsonElement

/**
 * Wires together the local renderer: ExoPlayer driver, command engine,
 * MQTT subscription, state publishing, and position polling.
 *
 * Port of `renderer_gstreamer/module.go` adapted for Android/coroutines.
 *
 * This is a plain Kotlin class managed by MqttForegroundService -- NOT
 * an Android Service subclass.
 */
class LocalRendererService(
    private val mqtt: MqttConnectionManager,
    private val nodeRepository: NodeRepository,
    private val rendererStateRepository: RendererStateRepository,
    private val context: Context,
    private val queueStore: com.mediautopia.app.data.cache.QueueStore,
    private val audioSessionHolder: AudioSessionHolder,
    private val localTransport: com.mediautopia.app.data.transport.LocalTransport,
    private val settingsDataStore: com.mediautopia.app.data.cache.SettingsDataStore,
) : com.mediautopia.app.data.transport.LocalDispatcher {
    private val tag = "LocalRendererService"
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    private val deviceName: String = Build.MODEL.replace(" ", "-")
    override val nodeId: String = "mu:renderer:media3:android:$deviceName:default"
    private val displayName: String = Build.MODEL

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val mainHandler = Handler(Looper.getMainLooper())

    private var driver: ExoPlayerDriver? = null
    @Volatile private var engine: LocalRendererEngine? = null
    private var audioFocusManager: AudioFocusManager? = null
    private var mediaSessionManager: MediaSessionManager? = null

    @Volatile private var cachedIdentity: String = ""

    /** Exposed so MqttForegroundService can use the session token for MediaStyle notification. */
    var mediaSession: MediaSession? = null
        private set

    /** Callback invoked when the notification needs updating (state or metadata changed). */
    var onNotificationUpdate: ((RendererState) -> Unit)? = null

    private var cmdSubscriptionId: String? = null
    private val cmdChannel = Channel<CommandEnvelope>(capacity = 64)

    private val workerJobs = mutableListOf<Job>()

    // -----------------------------------------------------------------
    // Lifecycle
    // -----------------------------------------------------------------

    fun start() {
        Log.i(tag, "Starting local renderer: $nodeId")

        // ExoPlayer must be created on the main thread.
        mainHandler.post {
            val exoDriver = ExoPlayerDriver(context) {
                // onTrackFinished callback -- dispatch to engine.
                engine?.onTrackFinished()
            }
            driver = exoDriver
            audioSessionHolder.set(exoDriver.exoPlayer.audioSessionId)

            val eng = LocalRendererEngine(
                nodeId = nodeId,
                name = displayName,
                driver = exoDriver,
            )
            engine = eng

            // Audio focus.
            val afm = AudioFocusManager(context, object : AudioFocusManager.AudioFocusListener {
                override fun onFocusGained() {
                    exoDriver.resume()
                }

                override fun onFocusLost() {
                    exoDriver.stop()
                }

                override fun onFocusLostTransient() {
                    exoDriver.pause()
                }

                override fun onFocusDuck() {
                    exoDriver.setVolume(0.2f)
                }
            })
            audioFocusManager = afm

            // MediaSession: wraps ExoPlayer with ForwardingPlayer that routes
            // transport commands through the MU engine (with lease validation).
            val msm = MediaSessionManager(
                context = context,
                exoPlayer = exoDriver.exoPlayer,
                scope = scope,
                onTransportCommand = { command -> handleTransportCommand(eng, command) },
            )
            val session = msm.create()
            mediaSessionManager = msm
            mediaSession = session

            // Now launch the async work on the coroutine scope.
            scope.launch { startAsync(eng) }
        }
    }

    private suspend fun startAsync(eng: LocalRendererEngine) {
        // Restore persisted queue before publishing state.
        try {
            val snapshot = queueStore.load()
            if (snapshot != null && snapshot.entries.isNotEmpty()) {
                withContext(Dispatchers.Main) {
                    eng.restoreFromSnapshot(snapshot)
                }
                Log.i(tag, "Restored ${snapshot.entries.size} queue entries from disk")
            }
        } catch (e: Exception) {
            Log.w(tag, "Failed to restore queue: ${e.message}")
        }

        // Publish presence (retained).
        publishPresence()

        // Publish initial state.
        publishState(eng.buildState())

        // Subscribe to command topic.
        cmdSubscriptionId = mqtt.subscribe(
            topic = MqttTopics.commands(nodeId = nodeId),
            qos = 1,
        ) { _, payload ->
            handleMessage(payload)
        }
        Log.i(tag, "Subscribed to ${MqttTopics.commands(nodeId = nodeId)}")

        // Launch 4 command workers.
        repeat(4) { i ->
            workerJobs += scope.launch {
                commandWorker(i, eng)
            }
        }

        // State debouncer: collect stateFlow, debounce 50ms, publish + update MediaSession.
        workerJobs += scope.launch {
            @OptIn(FlowPreview::class)
            eng.stateFlow
                .debounce(50)
                .collectLatest { state ->
                    publishState(state)
                    // Update MediaSession on main thread.
                    withContext(Dispatchers.Main) {
                        mediaSessionManager?.updateState(state, eng.currentEntry())
                    }
                    onNotificationUpdate?.invoke(state)
                }
        }

        // Persistence debouncer: save queue snapshot every 2 seconds on change.
        workerJobs += scope.launch {
            @OptIn(FlowPreview::class)
            eng.stateFlow
                .debounce(2000)
                .collectLatest { state ->
                    saveSnapshot(eng, state)
                }
        }

        // Position updater: every 1 second on the main thread (ExoPlayer requirement).
        workerJobs += scope.launch {
            while (isActive) {
                delay(1_000)
                withContext(Dispatchers.Main) {
                    eng.updatePosition()
                }
            }
        }

        // Register with repositories.
        val localNode = Node(
            nodeId = nodeId,
            kind = "renderer",
            name = displayName,
            caps = mapOf(
                "seek" to "true",
                "volume" to "true",
                "queue" to "true",
                "shuffle" to "true",
                "repeat" to "true",
            ),
            isLocal = true,
        )
        nodeRepository.registerLocalNode(localNode)
        rendererStateRepository.registerLocalStateSource(nodeId, eng.stateFlow)

        Log.i(tag, "Local renderer fully started")

        cachedIdentity = settingsDataStore.identity.first()

        localTransport.register(this@LocalRendererService)
    }

    fun stop() {
        Log.i(tag, "Stopping local renderer: $nodeId")

        localTransport.unregister()

        // Persist queue state before shutdown.
        engine?.let { eng ->
            try {
                kotlinx.coroutines.runBlocking {
                    saveSnapshot(eng, eng.buildState())
                }
                Log.i(tag, "Queue snapshot saved on stop")
            } catch (e: Exception) {
                Log.w(tag, "Failed to save queue on stop: ${e.message}")
            }
        }

        // Unsubscribe from commands.
        cmdSubscriptionId?.let { mqtt.unsubscribe(it) }
        cmdSubscriptionId = null

        // Clear retained presence (empty payload).
        mqtt.publish(
            topic = MqttTopics.presence(nodeId = nodeId),
            qos = 0,
            retained = true,
            payload = ByteArray(0),
        )

        // Unregister from repositories.
        rendererStateRepository.unregisterLocalStateSource(nodeId)
        nodeRepository.unregisterLocalNode(nodeId)

        // Cancel all workers.
        scope.cancel()
        cmdChannel.close()

        // Release MediaSession + ExoPlayer on main thread.
        mainHandler.post {
            mediaSessionManager?.release()
            mediaSessionManager = null
            mediaSession = null
            audioFocusManager?.abandonFocus()
            audioFocusManager = null
            audioSessionHolder.clear()
            driver?.release()
            driver = null
            engine = null
        }

        Log.i(tag, "Local renderer stopped")
    }

    /**
     * Handle transport commands from the MediaSession bridge player.
     * These come from the system notification / lock screen / Bluetooth controls.
     */
    private fun handleTransportCommand(eng: LocalRendererEngine, command: String) {
        scope.launch {
            try {
                val lease = eng.currentLease()
                if (lease == null) {
                    Log.w(tag, "Transport command $command ignored — no lease")
                    return@launch
                }
                val emptyBody = json.encodeToJsonElement(mapOf<String, String>())

                when {
                    command == "playback.play" || command == "playback.pause" ||
                    command == "playback.stop" || command == "playback.next" ||
                    command == "playback.prev" -> {
                        val cmd = CommandEnvelope(
                            id = "mediasession-${System.currentTimeMillis()}",
                            type = command,
                            from = "mediasession",
                            lease = lease,
                            body = emptyBody,
                            ts = System.currentTimeMillis() / 1000,
                        )
                        withContext(Dispatchers.Main) {
                            if (command == "playback.play") {
                                audioFocusManager?.requestFocus()
                            }
                            eng.handleCommand(cmd)
                        }
                    }
                    command.startsWith("playback.seek:") -> {
                        val posMs = command.substringAfter(":").toLongOrNull() ?: return@launch
                        val cmd = CommandEnvelope(
                            id = "mediasession-${System.currentTimeMillis()}",
                            type = "playback.seek",
                            from = "mediasession",
                            lease = lease,
                            body = json.encodeToJsonElement(PlaybackSeekBody(positionMs = posMs)),
                            ts = System.currentTimeMillis() / 1000,
                        )
                        withContext(Dispatchers.Main) {
                            eng.handleCommand(cmd)
                        }
                    }
                }
            } catch (e: Exception) {
                Log.e(tag, "Transport command $command failed: ${e.message}")
            }
        }
    }

    // -----------------------------------------------------------------
    // MQTT message handling
    // -----------------------------------------------------------------

    private fun handleMessage(payload: ByteArray) {
        val cmd = try {
            json.decodeFromString(CommandEnvelope.serializer(), payload.toString(Charsets.UTF_8))
        } catch (e: Exception) {
            Log.w(tag, "Invalid command: ${e.message}")
            return
        }

        val sent = cmdChannel.trySend(cmd)
        if (sent.isFailure) {
            Log.w(tag, "Command queue full, dropping ${cmd.type} id=${cmd.id}")
            if (cmd.replyTo != null) {
                val reply = ReplyEnvelope(
                    id = cmd.id,
                    type = "error",
                    ok = false,
                    ts = System.currentTimeMillis() / 1000,
                    err = com.mediautopia.app.data.protocol.ReplyError(
                        code = "OVERLOADED",
                        message = "command queue full",
                    ),
                )
                publishReply(cmd.replyTo, reply)
            }
        }
    }

    private suspend fun commandWorker(workerId: Int, eng: LocalRendererEngine) {
        for (cmd in cmdChannel) {
            try {
                processCommand(eng, cmd)
            } catch (e: Exception) {
                Log.e(tag, "Worker $workerId error processing ${cmd.type}: ${e.message}", e)
            }
        }
    }

    private suspend fun processCommand(eng: LocalRendererEngine, cmd: CommandEnvelope) {
        Log.d(tag, "Processing ${cmd.type} id=${cmd.id} from=${cmd.from}")
        val reply = processLocal(cmd)
        publishReply(cmd.replyTo, reply)
    }

    // -----------------------------------------------------------------
    // LocalDispatcher implementation (in-process command entry points)
    // -----------------------------------------------------------------

    override suspend fun dispatch(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
    ): com.mediautopia.app.data.protocol.ReplyEnvelope {
        val identity = cachedIdentity
        val envelope = com.mediautopia.app.data.protocol.CommandEnvelope(
            id = java.util.UUID.randomUUID().toString(),
            type = cmdType,
            ts = System.currentTimeMillis() / 1000,
            from = identity,
            replyTo = null,
            lease = lease,
            ifRevision = ifRevision,
            body = body,
        )
        return processLocal(envelope)
    }

    override fun dispatchFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ) {
        scope.launch {
            try {
                dispatch(nodeId, cmdType, body, lease, null)
            } catch (e: Exception) {
                Log.w(tag, "fire-and-forget dispatch failed: ${e.message}")
            }
        }
    }

    /**
     * Run a command in-process and return the resulting reply. This is the
     * same dispatch path [processCommand] uses for MQTT-driven commands, with
     * one difference: the reply is *returned* to the caller instead of being
     * published via [publishReply]. Safe to call from any coroutine context
     * (the engine handles its own threading for playback commands).
     */
    private suspend fun processLocal(
        cmd: com.mediautopia.app.data.protocol.CommandEnvelope,
    ): com.mediautopia.app.data.protocol.ReplyEnvelope {
        val eng = engine ?: return com.mediautopia.app.data.protocol.ReplyEnvelope(
            id = cmd.id,
            type = "error",
            ok = false,
            ts = System.currentTimeMillis() / 1000,
            err = com.mediautopia.app.data.protocol.ReplyError(
                code = "UNAVAILABLE",
                message = "local engine not started",
            ),
        )

        return if (LocalRendererEngine.isSessionCommand(cmd.type)) {
            eng.handleSessionCommand(cmd)
        } else if (isPlaybackCommand(cmd.type)) {
            withContext(Dispatchers.Main) {
                if (cmd.type == "playback.play") {
                    audioFocusManager?.requestFocus()
                }
                eng.handleCommand(cmd)
            }
        } else {
            eng.handleCommand(cmd)
        }
    }

    // -----------------------------------------------------------------
    // MQTT publishing
    // -----------------------------------------------------------------

    private fun publishPresence() {
        val presence = Presence(
            nodeId = nodeId,
            kind = "renderer",
            name = displayName,
            caps = mapOf(
                "seek" to JsonPrimitive(true),
                "volume" to JsonPrimitive(true),
                "queue" to JsonPrimitive(true),
                "shuffle" to JsonPrimitive(true),
                "repeat" to JsonPrimitive(true),
            ),
            ts = System.currentTimeMillis() / 1000,
        )
        val payload = json.encodeToString(Presence.serializer(), presence)
        mqtt.publish(
            topic = MqttTopics.presence(nodeId = nodeId),
            qos = 1,
            retained = true,
            payload = payload,
        )
        Log.i(tag, "Published presence for $nodeId")
    }

    private fun publishState(state: RendererState) {
        val payload = json.encodeToString(RendererState.serializer(), state)
        mqtt.publish(
            topic = MqttTopics.state(nodeId = nodeId),
            qos = 0,
            retained = true,
            payload = payload,
        )
    }

    private fun publishReply(replyTo: String?, reply: ReplyEnvelope) {
        if (replyTo.isNullOrEmpty()) return
        val payload = json.encodeToString(ReplyEnvelope.serializer(), reply)
        mqtt.publish(
            topic = replyTo,
            qos = 1,
            retained = false,
            payload = payload,
        )
    }

    private suspend fun saveSnapshot(eng: LocalRendererEngine, state: RendererState) {
        val entries = eng.queue.entries.map { e ->
            com.mediautopia.app.data.cache.QueueEntrySnapshot(
                queueEntryId = e.queueEntryId,
                itemId = e.itemId,
                url = e.url,
                mime = e.mime,
                metadata = e.metadata,
            )
        }
        val snapshot = com.mediautopia.app.data.cache.QueueSnapshot(
            entries = entries,
            index = eng.queue.index,
            revision = eng.queue.revision,
            shuffle = eng.queue.shuffle,
            repeat = eng.queue.repeat,
            repeatMode = eng.queue.repeatMode,
            volume = state.playback?.volume ?: 1.0,
            mute = state.playback?.mute ?: false,
            positionMs = state.playback?.positionMs ?: 0,
            playbackStatus = state.playback?.status ?: "stopped",
        )
        queueStore.save(snapshot)
    }

    companion object {
        private fun isPlaybackCommand(cmdType: String): Boolean = when (cmdType) {
            "playback.play", "playback.pause", "playback.stop",
            "playback.seek", "playback.next", "playback.prev",
            "playback.setVolume", "playback.setMute",
            "queue.jump",
            -> true
            else -> false
        }
    }
}
