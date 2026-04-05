package com.mediautopia.app.renderer

import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.protocol.CommandEnvelope
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
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive

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
) {
    private val tag = "LocalRendererService"
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    private val deviceName: String = Build.MODEL.replace(" ", "-")
    val nodeId: String = "mu:renderer:media3:android:$deviceName:default"
    private val displayName: String = Build.MODEL

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val mainHandler = Handler(Looper.getMainLooper())

    private var driver: ExoPlayerDriver? = null
    private var engine: LocalRendererEngine? = null
    private var audioFocusManager: AudioFocusManager? = null

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

            // Now launch the async work on the coroutine scope.
            scope.launch { startAsync(eng) }
        }
    }

    private suspend fun startAsync(eng: LocalRendererEngine) {
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

        // State debouncer: collect stateFlow, debounce 50ms, publish.
        workerJobs += scope.launch {
            @OptIn(FlowPreview::class)
            eng.stateFlow
                .debounce(50)
                .collectLatest { state ->
                    publishState(state)
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
    }

    fun stop() {
        Log.i(tag, "Stopping local renderer: $nodeId")

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

        // Release ExoPlayer on main thread.
        mainHandler.post {
            audioFocusManager?.abandonFocus()
            audioFocusManager = null
            driver?.release()
            driver = null
            engine = null
        }

        Log.i(tag, "Local renderer stopped")
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

        val reply: ReplyEnvelope = if (LocalRendererEngine.isSessionCommand(cmd.type)) {
            eng.handleSessionCommand(cmd)
        } else if (isPlaybackCommand(cmd.type)) {
            // Playback commands touch ExoPlayer which requires the main thread.
            withContext(Dispatchers.Main) {
                // Request audio focus before play commands.
                if (cmd.type == "playback.play") {
                    audioFocusManager?.requestFocus()
                }
                eng.handleCommand(cmd)
            }
        } else {
            eng.handleCommand(cmd)
        }

        publishReply(cmd.replyTo, reply)
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
