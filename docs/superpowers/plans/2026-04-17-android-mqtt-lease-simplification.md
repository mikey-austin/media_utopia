# Android MQTT + Lease Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Android app's MQTT connection reliable across idle/Doze, eliminate the self-fighting "controller-to-self-renderer" MQTT round-trip, and give the lease model a proper release/take-control UX.

**Architecture:** Three coordinated changes. (1) A `RendererTransport` abstraction splits local-in-process dispatch from MQTT dispatch — local control never touches the broker. (2) The engine gets a `forceAcquire` path and a new `session.takeControl` command, surfaced in the UI as a "Take control" action. (3) The MQTT lifecycle is rewritten around a single reconnect loop with deterministic timeouts and a heartbeat watchdog that catches silent socket death during Android Doze.

**Tech Stack:** Kotlin, Hilt DI, Coroutines, HiveMQ MQTT 3 client, Media3/ExoPlayer, Jetpack Compose.

**Repo root conventions:**
- Build: `make -C integrations/android_app compile` (fast Kotlin type-check).
- Full debug APK: `make -C integrations/android_app debug`.
- Source root: `integrations/android_app/app/src/main/java/com/mediautopia/app/`. All file paths in this plan are relative to repo root.
- Commit message convention (from recent history): `fix(android): <short description>` or `feat(android): <...>`.

**Design doc:** `docs/superpowers/specs/2026-04-17-android-mqtt-lease-simplification-design.md`.

**No automated tests.** The Android module has no test suite; verification is compile-check per task, plus manual validation at the end matching the spec's test plan. Each task ends with a compile check and a commit.

---

## File structure

New files:
- `integrations/android_app/app/src/main/java/com/mediautopia/app/data/transport/RendererTransport.kt` — interface + `MqttTransport` + `LocalTransport` + `TransportRouter`.

Modified files (roughly by task):
- `data/mqtt/MqttConnectionManager.kt` — Tasks 1, 2, 3 (timeouts, no auto-reconnect, heartbeat, enum).
- `service/MqttForegroundService.kt` — Task 2 (reconnect-loop coroutine).
- `data/transport/RendererTransport.kt` — Task 4 (new file).
- `renderer/LocalRendererService.kt` — Task 5 (extract `processLocal`, register with `LocalTransport`).
- `domain/usecase/LeaseManager.kt` — Task 6, 8 (route through transport; `takeControl`; simplify release).
- `renderer/LocalRendererEngine.kt` — Task 7 (`forceAcquire`, `session.takeControl`).
- `ui/screen/renderers/RenderersViewModel.kt` — Task 8 (take-control entry).
- `ui/screen/renderers/RenderersScreen.kt` — Task 9 (menu action matrix, confirm dialog).
- `ui/screen/nowplaying/NowPlayingViewModel.kt` + `NowPlayingScreen.kt` — Task 10 (foreign-lease button disable).

---

## Task 1: Lifecycle timeouts and `markDisconnected()`

Add deterministic timeouts around `connect()` and `disconnect()` and expose a `markDisconnected()` that consumers call to force state back to `DISCONNECTED`. Drop the `RECONNECTING` enum value (it was only meaningful while HiveMQ's auto-reconnect layer existed; a later task removes that layer).

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/data/mqtt/MqttConnectionManager.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/service/MqttForegroundService.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersScreen.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersViewModel.kt`

- [ ] **Step 1: Drop `RECONNECTING` from the enum**

Edit `MqttConnectionManager.kt`:

```kotlin
enum class ConnectionState {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
}
```

- [ ] **Step 2: Remove the `wasReconnect` branch in the connected listener**

In `MqttConnectionManager.kt`, inside the `connect()` method, replace the existing `addConnectedListener` block:

```kotlin
.addConnectedListener(object : MqttClientConnectedListener {
    override fun onConnected(ctx: MqttClientConnectedContext) {
        Log.i(tag, "MQTT connected")
        _connectionState.value = ConnectionState.CONNECTED
        // Resubscribe is triggered by the connect() coroutine on successful
        // ConnAck; the library's re-entrant "connected" path is gone.
    }
})
.addDisconnectedListener(object : MqttClientDisconnectedListener {
    override fun onDisconnected(ctx: MqttClientDisconnectedContext) {
        Log.w(tag, "MQTT disconnected: ${ctx.cause?.message}")
        _connectionState.value = ConnectionState.DISCONNECTED
    }
})
```

- [ ] **Step 3: Wrap `connect()` in a 10s timeout, null the client on failure**

Replace the body of `suspend fun connect(brokerUrl: String, clientId: String)` in `MqttConnectionManager.kt` with:

```kotlin
suspend fun connect(brokerUrl: String, clientId: String) {
    if (_connectionState.value == ConnectionState.CONNECTED ||
        _connectionState.value == ConnectionState.CONNECTING
    ) {
        Log.w(tag, "Already connected or connecting")
        return
    }

    val previous = client
    if (previous != null) {
        try {
            withTimeoutOrNull(2.seconds) {
                suspendCancellableCoroutine<Unit> { cont ->
                    previous.disconnect().whenComplete { _, _ -> cont.resume(Unit) }
                }
            }
        } catch (e: Exception) {
            Log.w(tag, "Previous client disconnect failed: ${e.message}")
        }
        client = null
    }

    _connectionState.value = ConnectionState.CONNECTING

    val uri = URI(brokerUrl)
    val useSsl = uri.scheme == "mqtts" || uri.scheme == "ssl"
    val host = uri.host ?: throw IllegalArgumentException("Broker URL must have a host")
    val port = if (uri.port > 0) uri.port else if (useSsl) 8883 else 1883

    val builder = MqttClient.builder()
        .useMqttVersion3()
        .identifier(clientId)
        .serverHost(host)
        .serverPort(port)
        // NOTE: automaticReconnect intentionally removed — we own the loop.
        .addConnectedListener(object : MqttClientConnectedListener {
            override fun onConnected(ctx: MqttClientConnectedContext) {
                Log.i(tag, "MQTT connected")
                _connectionState.value = ConnectionState.CONNECTED
            }
        })
        .addDisconnectedListener(object : MqttClientDisconnectedListener {
            override fun onDisconnected(ctx: MqttClientDisconnectedContext) {
                Log.w(tag, "MQTT disconnected: ${ctx.cause?.message}")
                _connectionState.value = ConnectionState.DISCONNECTED
            }
        })

    if (useSsl) {
        builder.sslWithDefaultConfig()
    }

    val mqtt3Client = builder.buildAsync()
    client = mqtt3Client

    mqtt3Client.publishes(MqttGlobalPublishFilter.ALL) { publish ->
        val msgTopic = publish.topic.toString()
        val msgPayload = publish.payloadAsBytes
        callbackScope.launch {
            routeMessage(msgTopic, msgPayload)
        }
    }

    try {
        withTimeout(10.seconds) {
            suspendCancellableCoroutine<Unit> { cont ->
                mqtt3Client.connectWith()
                    .cleanSession(true)
                    .keepAlive(60)
                    .send()
                    .whenComplete { _: Mqtt3ConnAck?, error: Throwable? ->
                        if (error != null) {
                            cont.resumeWithException(error)
                        } else {
                            cont.resume(Unit)
                        }
                    }
            }
        }
        _connectionState.value = ConnectionState.CONNECTED
        resubscribeAll()
    } catch (e: Exception) {
        Log.w(tag, "connect() failed or timed out: ${e.message}")
        client = null
        _connectionState.value = ConnectionState.DISCONNECTED
        throw e
    }
}
```

Add imports at the top of the file:

```kotlin
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.time.Duration.Companion.seconds
```

- [ ] **Step 4: Wrap `disconnect()` in a 5s timeout**

Replace `suspend fun disconnect()` in `MqttConnectionManager.kt`:

```kotlin
suspend fun disconnect() {
    val c = client ?: run {
        _connectionState.value = ConnectionState.DISCONNECTED
        return
    }
    try {
        withTimeout(5.seconds) {
            suspendCancellableCoroutine<Unit> { cont ->
                c.disconnect()
                    .whenComplete { _, error ->
                        if (error != null) {
                            Log.w(tag, "Disconnect completed with error: ${error.message}")
                        }
                        cont.resume(Unit)
                    }
            }
        }
    } catch (e: Exception) {
        Log.w(tag, "Disconnect threw or timed out: ${e.message}")
    } finally {
        client = null
        _connectionState.value = ConnectionState.DISCONNECTED
    }
}
```

- [ ] **Step 5: Add `markDisconnected()`**

Add a new public method to `MqttConnectionManager.kt`, right below `disconnect()`:

```kotlin
/**
 * Force the connection state to DISCONNECTED without waiting for broker
 * acknowledgement. Safe to call from any thread. Used by the watchdog, the
 * network callback, and the UI reconnect button to trip the reconnect loop
 * in [MqttForegroundService] without holding any mutex.
 *
 * Best-effort drops the underlying client reference so no stale callback
 * can resurrect the state later. The reconnect loop will build a fresh
 * client on the next attempt.
 */
fun markDisconnected() {
    val previous = client
    client = null
    _connectionState.value = ConnectionState.DISCONNECTED
    if (previous != null) {
        callbackScope.launch {
            try {
                withTimeoutOrNull(2.seconds) {
                    suspendCancellableCoroutine<Unit> { cont ->
                        previous.disconnect().whenComplete { _, _ -> cont.resume(Unit) }
                    }
                }
            } catch (_: Exception) {}
        }
    }
}
```

- [ ] **Step 6: Purge `RECONNECTING` usages in consumers**

Edit `MqttForegroundService.kt` — in `onStartCommand`'s state-observer block, remove the `RECONNECTING` case:

```kotlin
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
```

In `registerNetworkCallback()`, replace the condition:

```kotlin
if (state == ConnectionState.DISCONNECTED) {
    mqttConnectionManager.markDisconnected()
}
```

Edit `RenderersScreen.kt` — in `ConnectionStatusBar`, remove the `RECONNECTING` case:

```kotlin
val (dotColor, label) = when (state) {
    ConnectionState.CONNECTED -> Color(0xFF4CAF50) to "CONNECTED"
    ConnectionState.CONNECTING -> Color(0xFFFF9800) to "CONNECTING..."
    ConnectionState.DISCONNECTED -> Color(0xFFF44336) to "DISCONNECTED"
}
```

Edit `RenderersViewModel.kt` — in the `uiState` combine block, update the `isConnected` derivation (it used to include `RECONNECTING`):

```kotlin
val isConnected = connectionState == ConnectionState.CONNECTED
```

- [ ] **Step 7: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL. Any "unresolved reference RECONNECTING" means a consumer was missed — grep for it and update.

- [ ] **Step 8: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/data/mqtt/MqttConnectionManager.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/service/MqttForegroundService.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersScreen.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersViewModel.kt
git commit -m "fix(android): add connect/disconnect timeouts and markDisconnected()"
```

---

## Task 2: Single reconnect loop replaces `triggerHardReconnect`

Replace the ad-hoc `triggerHardReconnect` with a single coroutine that observes `connectionState` and performs stop+start on every transition into `DISCONNECTED` with exponential backoff. The UI reconnect button and the network callback both now call `markDisconnected()` — one backoff policy, no mutex-deadlock surface.

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/service/MqttForegroundService.kt`

- [ ] **Step 1: Update the service's field declarations**

In `MqttForegroundService.kt`, replace the existing field block at the top of the class with:

```kotlin
private val tag = "MqttForegroundService"
private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
private var localRenderer: LocalRendererService? = null
private var networkCallback: ConnectivityManager.NetworkCallback? = null
private val sessionMutex = Mutex()
private val reconnectTrigger = kotlinx.coroutines.channels.Channel<Unit>(
    capacity = kotlinx.coroutines.channels.Channel.CONFLATED
)
private var reconnectLoopJob: Job? = null
```

(Renames `reconnectMutex` → `sessionMutex` and deletes the `reconnectJob` field — the new loop is one long-lived job on `reconnectLoopJob`.)

Add the import for the flow operator used later:

```kotlin
import kotlinx.coroutines.flow.distinctUntilChanged
```

- [ ] **Step 2: Rewrite `onStartCommand`**

Replace the existing `onStartCommand` body with:

```kotlin
override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
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

    createNotificationChannels()
    try {
        startForeground(NOTIFICATION_ID, buildServiceNotification("Connecting..."))
    } catch (e: Exception) {
        Log.e(tag, "startForeground failed: ${e.message}")
        stopSelf()
        return START_NOT_STICKY
    }
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
```

- [ ] **Step 3: Add the `reconnectLoop` coroutine**

Add a new method to `MqttForegroundService.kt`:

```kotlin
/**
 * Single authoritative reconnect loop. Performs the initial startSession
 * and every subsequent rebuild, driven by [reconnectTrigger]. The trigger
 * is poked on any DISCONNECTED transition (via a state observer launched
 * here) and on explicit external events (UI reconnect button, network-
 * available callback). Exponential backoff grows on failed attempts and
 * resets to 2s once a connection stays CONNECTED for 60s.
 */
private suspend fun reconnectLoop() {
    var backoffMs = 2_000L

    // Side observer: trip the trigger on DISCONNECTED; schedule a backoff
    // reset when CONNECTED stays stable.
    serviceScope.launch {
        mqttConnectionManager.connectionState
            .distinctUntilChanged()
            .collect { state ->
                when (state) {
                    ConnectionState.DISCONNECTED -> reconnectTrigger.trySend(Unit)
                    ConnectionState.CONNECTED -> {
                        serviceScope.launch {
                            delay(60_000)
                            if (mqttConnectionManager.connectionState.value == ConnectionState.CONNECTED) {
                                backoffMs = 2_000L
                                Log.i(tag, "Connection stable 60s; backoff reset")
                            }
                        }
                    }
                    ConnectionState.CONNECTING -> {}
                }
            }
    }

    // Main reconnect driver.
    for (unit in reconnectTrigger) {
        if (!isStarted) continue
        if (mqttConnectionManager.connectionState.value == ConnectionState.CONNECTED) continue

        Log.i(tag, "Reconnect trigger; waiting ${backoffMs}ms before attempt")
        delay(backoffMs)
        if (mqttConnectionManager.connectionState.value == ConnectionState.CONNECTED) continue

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

        // Grow backoff only if we didn't reach CONNECTED.
        if (mqttConnectionManager.connectionState.value != ConnectionState.CONNECTED) {
            backoffMs = (backoffMs * 2).coerceAtMost(30_000L)
        }
    }
}
```

- [ ] **Step 4: Simplify `registerNetworkCallback()`**

Replace the method body in `MqttForegroundService.kt`:

```kotlin
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
```

- [ ] **Step 5: Delete the old reconnect plumbing**

In `MqttForegroundService.kt`, delete:

- The entire `triggerHardReconnect()` method.
- The `updateServiceNotificationText` helper (no remaining callers).
- Any remaining reference to `reconnectMutex` or `reconnectJob` (replaced by `sessionMutex` and `reconnectLoopJob`).

Verify `stopSession` / `startSession` / `onDestroy` still reference only the renamed `sessionMutex` where applicable. `onDestroy`'s `serviceScope.cancel()` cancels `reconnectLoopJob` transitively — no extra teardown needed.

- [ ] **Step 6: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 7: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/service/MqttForegroundService.kt
git commit -m "fix(android): replace triggerHardReconnect with single reconnect loop"
```

---

## Task 3: Heartbeat watchdog

Detect silent socket death (Doze-mode reclaim). Every 30s while connected, publish an empty payload to a self-owned topic and verify we receive our own echo. If no echo within 40s, call `markDisconnected()` to trip the reconnect loop.

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/data/mqtt/MqttConnectionManager.kt`

- [ ] **Step 1: Add heartbeat fields**

At the top of `MqttConnectionManager` class body, add:

```kotlin
private var heartbeatJob: Job? = null
private var heartbeatSubscriptionId: String? = null
@Volatile private var lastEchoMs: Long = 0L
private var currentClientId: String? = null

companion object {
    private const val HEARTBEAT_INTERVAL_MS = 30_000L
    private const val HEARTBEAT_GRACE_MS = 10_000L
    private const val HEARTBEAT_TIMEOUT_MS = HEARTBEAT_INTERVAL_MS + HEARTBEAT_GRACE_MS
    private const val HEARTBEAT_TOPIC_PREFIX = "mu/v1/heartbeat/"
}
```

- [ ] **Step 2: Start heartbeat after successful connect; stop on disconnect**

In `MqttConnectionManager.kt`, add the helper methods:

```kotlin
private fun startHeartbeat(clientId: String) {
    stopHeartbeat()

    val topic = "$HEARTBEAT_TOPIC_PREFIX$clientId"
    lastEchoMs = System.currentTimeMillis()
    currentClientId = clientId

    heartbeatSubscriptionId = subscribe(topic = topic, qos = 0) { _, _ ->
        lastEchoMs = System.currentTimeMillis()
    }

    heartbeatJob = callbackScope.launch {
        while (isActive) {
            delay(HEARTBEAT_INTERVAL_MS)
            if (_connectionState.value != ConnectionState.CONNECTED) continue
            try {
                publish(topic = topic, qos = 0, retained = false, payload = ByteArray(0))
            } catch (e: Exception) {
                Log.w(tag, "heartbeat publish failed: ${e.message}")
            }
            val silenceMs = System.currentTimeMillis() - lastEchoMs
            if (silenceMs > HEARTBEAT_TIMEOUT_MS) {
                Log.w(tag, "heartbeat silent for ${silenceMs}ms, tripping reconnect")
                markDisconnected()
                return@launch
            }
        }
    }
}

private fun stopHeartbeat() {
    heartbeatJob?.cancel()
    heartbeatJob = null
    heartbeatSubscriptionId?.let { unsubscribe(it) }
    heartbeatSubscriptionId = null
    currentClientId = null
}
```

Add imports if missing:

```kotlin
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
```

- [ ] **Step 3: Wire heartbeat into connect/disconnect/markDisconnected**

In `connect()`, at the point where we transition to CONNECTED (both inside the builder's `onConnected` *and* after the `withTimeout` succeeds in the coroutine-driven path), ensure `startHeartbeat(clientId)` runs. Because `onConnected` fires before `send().whenComplete` returns, the simplest placement is right after `resubscribeAll()` in the coroutine:

```kotlin
try {
    withTimeout(10.seconds) { /* ... unchanged ... */ }
    _connectionState.value = ConnectionState.CONNECTED
    resubscribeAll()
    startHeartbeat(clientId)
} catch (e: Exception) {
    Log.w(tag, "connect() failed or timed out: ${e.message}")
    stopHeartbeat()
    client = null
    _connectionState.value = ConnectionState.DISCONNECTED
    throw e
}
```

In `disconnect()`, call `stopHeartbeat()` inside the `finally` block:

```kotlin
} finally {
    stopHeartbeat()
    client = null
    _connectionState.value = ConnectionState.DISCONNECTED
}
```

In `markDisconnected()`, call `stopHeartbeat()` before setting state:

```kotlin
fun markDisconnected() {
    stopHeartbeat()
    val previous = client
    client = null
    _connectionState.value = ConnectionState.DISCONNECTED
    // ... rest unchanged
}
```

- [ ] **Step 4: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 5: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/data/mqtt/MqttConnectionManager.kt
git commit -m "feat(android): add heartbeat watchdog for silent-socket recovery"
```

---

## Task 4: `RendererTransport` interface + MQTT + Local + Router

Introduce the transport abstraction. `MqttTransport` wraps today's `CommandCorrelator`. `LocalTransport` is a stub with a `register`/`unregister` API — actual dispatch lands in Task 5 once `LocalRendererService.processLocal` exists. `TransportRouter` picks between them by node id.

**Files:**
- Create: `integrations/android_app/app/src/main/java/com/mediautopia/app/data/transport/RendererTransport.kt`

- [ ] **Step 1: Create the transport file**

Write `integrations/android_app/app/src/main/java/com/mediautopia/app/data/transport/RendererTransport.kt`:

```kotlin
package com.mediautopia.app.data.transport

import android.util.Log
import com.mediautopia.app.data.protocol.Lease
import com.mediautopia.app.data.protocol.ReplyEnvelope
import com.mediautopia.app.domain.usecase.CommandCorrelator
import kotlinx.serialization.json.JsonElement
import java.util.concurrent.atomic.AtomicReference
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds

/**
 * Uniform command-send interface used by controllers. Two implementations:
 *   - [MqttTransport]: the classic broker-mediated path.
 *   - [LocalTransport]: in-process direct dispatch to the phone's own
 *     [com.mediautopia.app.renderer.LocalRendererService], bypassing MQTT
 *     entirely for self-control.
 *
 * [TransportRouter] picks between them per-send by node id. Callers (e.g.
 * [com.mediautopia.app.domain.usecase.LeaseManager]) inject the router and
 * never branch on locality themselves.
 */
interface RendererTransport {
    suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease? = null,
        ifRevision: Long? = null,
        timeout: Duration = 2.seconds,
    ): ReplyEnvelope

    fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease? = null,
    )
}

/** Implementation that publishes commands over MQTT and awaits a correlated reply. */
@Singleton
class MqttTransport @Inject constructor(
    private val correlator: CommandCorrelator,
) : RendererTransport {

    override suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
        timeout: Duration,
    ): ReplyEnvelope = correlator.send(
        nodeId = nodeId,
        cmdType = cmdType,
        body = body,
        lease = lease,
        ifRevision = ifRevision,
        timeout = timeout,
    )

    override fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ) {
        correlator.sendFireAndForget(
            nodeId = nodeId,
            cmdType = cmdType,
            body = body,
            lease = lease,
        )
    }
}

/**
 * In-process transport. Holds a weak-ish reference to the active
 * [com.mediautopia.app.renderer.LocalRendererService] via [register] /
 * [unregister], called from the service's own start/stop. When no service
 * is registered, [isAvailable] returns false and [send] throws — callers
 * should route through [TransportRouter] which falls back to MQTT.
 *
 * Typed as `Any` here to avoid a forward dependency on the renderer
 * package; Task 5 wires the real call site.
 */
@Singleton
class LocalTransport @Inject constructor() : RendererTransport {
    private val tag = "LocalTransport"

    /** Set to a non-null value while the local renderer service is alive. */
    private val serviceRef = AtomicReference<LocalDispatcher?>(null)

    fun register(dispatcher: LocalDispatcher) {
        serviceRef.set(dispatcher)
        Log.i(tag, "LocalTransport registered for node ${dispatcher.nodeId}")
    }

    fun unregister() {
        serviceRef.set(null)
        Log.i(tag, "LocalTransport unregistered")
    }

    fun isAvailable(nodeId: String): Boolean =
        serviceRef.get()?.nodeId == nodeId

    override suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
        timeout: Duration,
    ): ReplyEnvelope {
        val dispatcher = serviceRef.get()
            ?: throw IllegalStateException("LocalTransport has no active service")
        return dispatcher.dispatch(nodeId, cmdType, body, lease, ifRevision)
    }

    override fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ) {
        val dispatcher = serviceRef.get() ?: return
        // Best-effort: launch on the dispatcher's own scope.
        dispatcher.dispatchFireAndForget(nodeId, cmdType, body, lease)
    }
}

/**
 * Minimal interface the in-process dispatcher exposes. Implemented by
 * [com.mediautopia.app.renderer.LocalRendererService]. Lives here to keep
 * [LocalTransport] agnostic of renderer internals.
 */
interface LocalDispatcher {
    val nodeId: String
    suspend fun dispatch(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
    ): ReplyEnvelope

    fun dispatchFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    )
}

/**
 * Routes each send to the appropriate transport. Inject this (not the
 * individual transports) in consumers.
 */
@Singleton
class TransportRouter @Inject constructor(
    private val mqtt: MqttTransport,
    private val local: LocalTransport,
) : RendererTransport {

    override suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
        timeout: Duration,
    ): ReplyEnvelope {
        val transport: RendererTransport =
            if (local.isAvailable(nodeId)) local else mqtt
        return transport.send(nodeId, cmdType, body, lease, ifRevision, timeout)
    }

    override fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ) {
        val transport: RendererTransport =
            if (local.isAvailable(nodeId)) local else mqtt
        transport.sendFireAndForget(nodeId, cmdType, body, lease)
    }
}
```

- [ ] **Step 2: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 3: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/data/transport/RendererTransport.kt
git commit -m "feat(android): add RendererTransport abstraction with Mqtt/Local/Router"
```

---

## Task 5: `LocalRendererService.processLocal` + `LocalDispatcher` impl

Extract the threading and audio-focus logic out of the MQTT-driven `processCommand` into a reusable `processLocal(envelope)` path that returns the `ReplyEnvelope` instead of publishing it. Make `LocalRendererService` implement `LocalDispatcher`. Register/unregister with `LocalTransport` on start/stop.

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/renderer/LocalRendererService.kt`

- [ ] **Step 1: Inject `LocalTransport` and implement `LocalDispatcher`**

In `LocalRendererService.kt`, change the class signature and add the transport parameter:

```kotlin
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
```

Add imports at the top:

```kotlin
import com.mediautopia.app.data.protocol.Lease
import kotlinx.serialization.json.JsonElement
import kotlinx.coroutines.flow.first
```

- [ ] **Step 2: Add `dispatch` and `dispatchFireAndForget`**

Below `handleTransportCommand`, add:

```kotlin
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
    val identity = settingsDataStore.identity.first()
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
```

- [ ] **Step 3: Refactor the existing `processCommand` to reuse `processLocal`**

Replace the body of `suspend fun processCommand(eng: LocalRendererEngine, cmd: CommandEnvelope)`:

```kotlin
private suspend fun processCommand(eng: LocalRendererEngine, cmd: CommandEnvelope) {
    Log.d(tag, "Processing ${cmd.type} id=${cmd.id} from=${cmd.from}")
    val reply = processLocal(cmd)
    publishReply(cmd.replyTo, reply)
}
```

Delete the now-unused local helper logic inside the old `processCommand` body (the `if (isSessionCommand)` branches are now in `processLocal`).

- [ ] **Step 4: Register/unregister with `LocalTransport`**

In `startAsync(eng)`, at the very end (after `Log.i(tag, "Local renderer fully started")`), add:

```kotlin
localTransport.register(this@LocalRendererService)
```

In `stop()`, near the top (before `cmdSubscriptionId?.let { mqtt.unsubscribe(it) }`), add:

```kotlin
localTransport.unregister()
```

Also expose `nodeId` — it's already a `val` on the class, but `LocalDispatcher` declares it as an interface member. Kotlin's property-matching takes care of this automatically since both have the same name and type. No code change needed for that.

- [ ] **Step 5: Inject the new constructor params at the call site**

In `MqttForegroundService.kt`, inject the new dependencies:

```kotlin
@Inject lateinit var localTransport: com.mediautopia.app.data.transport.LocalTransport
```

Update the `LocalRendererService` construction in `startSession`:

```kotlin
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
```

- [ ] **Step 6: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 7: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/renderer/LocalRendererService.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/service/MqttForegroundService.kt
git commit -m "feat(android): wire LocalRendererService as an in-process LocalDispatcher"
```

---

## Task 6: Migrate all command senders to `TransportRouter`

The whole point of Task 4's abstraction is that local-addressed commands skip MQTT. That only materializes once every caller — not just `LeaseManager` — routes through `TransportRouter`. This task also simplifies `releaseLease`'s acquire-then-release fallback (obsolete once `takeControl` exists in Task 8).

The `TransportRouter.send` and `.sendFireAndForget` signatures match `CommandCorrelator`'s exactly, so each migration is a constructor param rename + method-call rename.

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/domain/usecase/LeaseManager.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/nowplaying/NowPlayingViewModel.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/queue/QueueViewModel.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/zones/ZonesViewModel.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/library/LibraryViewModel.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/data/repository/LibraryRepository.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/data/repository/PlaylistRepository.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/data/repository/ZoneRepository.kt`

- [ ] **Step 1: Migrate `LeaseManager`**

Change the constructor:

```kotlin
@Singleton
class LeaseManager @Inject constructor(
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
) {
```

Replace every `correlator.send(...)` call in `LeaseManager.kt` with `transport.send(...)` — the parameter names and types are identical.

Then rewrite `suspend fun releaseLease(rendererId: String)`:

```kotlin
suspend fun releaseLease(rendererId: String) {
    val cached = leases.remove(rendererId) ?: run {
        publishLeases()
        return
    }
    publishLeases()

    try {
        val body = json.encodeToJsonElement(
            mapOf("sessionId" to cached.sessionId, "token" to cached.token)
        )
        transport.send(
            nodeId = rendererId,
            cmdType = "session.release",
            body = body,
            lease = Lease(sessionId = cached.sessionId, token = cached.token),
        )
        Log.i(tag, "Released lease for $rendererId")
    } catch (e: Exception) {
        Log.w(tag, "Failed to release lease for $rendererId: ${e.message}")
    }
}
```

- [ ] **Step 2: Migrate the view models**

Apply the same literal rename in each of:
- `NowPlayingViewModel.kt`
- `QueueViewModel.kt`
- `ZonesViewModel.kt`
- `LibraryViewModel.kt`

In each file:

1. Change the constructor parameter:

```kotlin
// Before:
private val correlator: CommandCorrelator,
// After:
private val transport: com.mediautopia.app.data.transport.TransportRouter,
```

2. Replace every `correlator.send(` with `transport.send(`.
3. Replace every `correlator.sendFireAndForget(` with `transport.sendFireAndForget(`.
4. Remove the now-unused `CommandCorrelator` import if present.

For reference, file-level occurrence counts as of the spec:
- `NowPlayingViewModel.kt`: 9 `correlator.send` calls.
- `QueueViewModel.kt`: 8 calls.
- `ZonesViewModel.kt`: 1 call.
- `LibraryViewModel.kt`: 14 calls.

- [ ] **Step 3: Migrate the repositories**

Same rename pattern in:
- `LibraryRepository.kt` (7 calls)
- `PlaylistRepository.kt` (2 calls)
- `ZoneRepository.kt` (3 calls)

These repositories address server-side library/playlist/zone nodes that are never the local renderer, so the router will always pick `MqttTransport` for them. The migration is for uniformity and so no caller bypasses the router — if you ever add a local command path for a repository, it'll just work.

- [ ] **Step 4: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL. If Hilt complains about a missing binding, confirm the transport classes in Task 4 are annotated with `@Singleton` and `@Inject constructor`. Any lingering `correlator.` references will surface as unresolved symbols.

- [ ] **Step 5: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/domain/usecase/LeaseManager.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/nowplaying/NowPlayingViewModel.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/queue/QueueViewModel.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/zones/ZonesViewModel.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/library/LibraryViewModel.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/data/repository/LibraryRepository.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/data/repository/PlaylistRepository.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/data/repository/ZoneRepository.kt
git commit -m "refactor(android): route all command senders through TransportRouter"
```

---

## Task 7: Engine-side `forceAcquire` and `session.takeControl`

Add a force-acquire path on `RendererLeaseManager` and dispatch the new `session.takeControl` command type in `LocalRendererEngine`.

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/renderer/LocalRendererEngine.kt`

- [ ] **Step 1: Add `forceAcquire` to `RendererLeaseManager`**

At the bottom of `LocalRendererEngine.kt`, inside the `internal class RendererLeaseManager` block, add this method after `acquire`:

```kotlin
fun forceAcquire(requestOwner: String, ttlMs: Long): SessionLease {
    lock.withLock {
        clearLocked()
        return newLeaseLocked(requestOwner, ttlMs)
    }
}
```

- [ ] **Step 2: Add the `session.takeControl` handler**

In `LocalRendererEngine.kt`, add a new handler method in the Session handlers section:

```kotlin
private fun handleSessionTakeControl(cmd: CommandEnvelope): ReplyEnvelope {
    val body = decodeBody<SessionAcquireBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
    val ttlMs = if (body.ttlMs <= 0) 300_000L else body.ttlMs
    val lease = leaseManager.forceAcquire(cmd.from, ttlMs)

    val sessionState = SessionState(
        id = lease.id,
        owner = lease.owner,
        leaseExpiresAt = lease.leaseExpiresAt,
    )
    stateVersion++
    val replyBody = SessionReplyBody(session = lease, stateVersion = stateVersion)
    emitStateWithSession(sessionState)
    return withBody(cmd, json.encodeToJsonElement(replyBody))
}
```

- [ ] **Step 3: Route the new command type**

In `handleSessionCommand`, add the new case:

```kotlin
fun handleSessionCommand(cmd: CommandEnvelope): ReplyEnvelope {
    sessionLock.withLock {
        return when (cmd.type) {
            "session.acquire" -> handleSessionAcquire(cmd)
            "session.renew" -> handleSessionRenew(cmd)
            "session.release" -> handleSessionRelease(cmd)
            "session.takeControl" -> handleSessionTakeControl(cmd)
            else -> errorReply(cmd, "INVALID", "not a session command")
        }
    }
}
```

Also in the general `handleCommand` dispatch, add the take-control case alongside the other session branches:

```kotlin
"session.acquire" -> {
    sessionLock.withLock { handleSessionAcquire(cmd) }
}
"session.renew" -> {
    sessionLock.withLock { handleSessionRenew(cmd) }
}
"session.release" -> {
    sessionLock.withLock { handleSessionRelease(cmd) }
}
"session.takeControl" -> {
    sessionLock.withLock { handleSessionTakeControl(cmd) }
}
```

And update the companion `isSessionCommand` to include it:

```kotlin
companion object {
    fun isSessionCommand(cmdType: String): Boolean = when (cmdType) {
        "session.acquire", "session.renew", "session.release", "session.takeControl" -> true
        else -> false
    }
}
```

- [ ] **Step 4: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 5: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/renderer/LocalRendererEngine.kt
git commit -m "feat(android): add RendererLeaseManager.forceAcquire and session.takeControl"
```

---

## Task 8: Client-side `LeaseManager.takeControl`

Expose a client-side entry point that sends `session.takeControl` and caches the returned lease.

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/domain/usecase/LeaseManager.kt`

- [ ] **Step 1: Add the method**

Add to `LeaseManager.kt`, alongside `ensureLease`:

```kotlin
/**
 * Force-acquire a lease by sending `session.takeControl`, overwriting any
 * active lease on the renderer. Used by the UI when the user explicitly
 * wants to kick a current holder. Caches the new lease identically to
 * [acquireLease].
 */
suspend fun takeControl(rendererId: String): Lease {
    val body = json.encodeToJsonElement(SessionAcquireBody(ttlMs = TTL_MS))

    val reply = transport.send(
        nodeId = rendererId,
        cmdType = "session.takeControl",
        body = body,
    )

    if (!reply.ok) {
        val errorCode = reply.err?.code ?: "UNKNOWN"
        throw LeaseException("session.takeControl failed for $rendererId: $errorCode - ${reply.err?.message}")
    }

    val sessionReply = json.decodeFromJsonElement<SessionReplyBody>(
        reply.body ?: throw LeaseException("session.takeControl reply missing body")
    )

    val cached = CachedLease(
        sessionId = sessionReply.session.id,
        token = sessionReply.session.token,
        expiresAt = sessionReply.session.leaseExpiresAt * 1000,
    )
    leases[rendererId] = cached
    publishLeases()

    Log.i(tag, "Took control of $rendererId, session=${cached.sessionId}")
    return Lease(sessionId = cached.sessionId, token = cached.token)
}
```

- [ ] **Step 2: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 3: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/domain/usecase/LeaseManager.kt
git commit -m "feat(android): add LeaseManager.takeControl client entry point"
```

---

## Task 9: UI menu — unified release/take-control with confirm dialog

Replace the hardcoded `showRelease = false` on the local card with a menu-action matrix driven by `isOwnLease` + `leaseOwner` + `isLocal`. Add a confirm dialog for take-control. Remote cards keep only the "Release lease" action (remote force-steal is out of scope per the spec).

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersViewModel.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersScreen.kt`

- [ ] **Step 1: Add `takeControl` to the view model**

In `RenderersViewModel.kt`, add this method next to `releaseLease`:

```kotlin
fun takeControl(nodeId: String) {
    viewModelScope.launch {
        try {
            leaseManager.takeControl(nodeId)
            Log.i(tag, "Took control of $nodeId")
            snackbarManager.show("You now control this renderer")
        } catch (e: Exception) {
            Log.e(tag, "takeControl failed for $nodeId: ${e.message}", e)
            snackbarManager.show("Take control failed: ${e.message}")
        }
    }
}
```

- [ ] **Step 2: Wire `onTakeControl` through the composables**

Edit `RenderersScreen.kt`. In `RenderersSheet`, pass a take-control lambda:

```kotlin
RenderersContent(
    state = uiState,
    onSelectRenderer = { nodeId ->
        viewModel.selectRenderer(nodeId)
        onDismiss()
    },
    onReleaseLease = viewModel::releaseLease,
    onTakeControl = viewModel::takeControl,
    onReconnect = viewModel::reconnect,
    onDismiss = onDismiss,
)
```

Update `RenderersContent`'s signature:

```kotlin
@Composable
private fun RenderersContent(
    state: RenderersUiState,
    onSelectRenderer: (String) -> Unit,
    onReleaseLease: (String) -> Unit,
    onTakeControl: (String) -> Unit,
    onReconnect: () -> Unit,
    onDismiss: () -> Unit,
) {
```

Pass `onTakeControl` into each card:

```kotlin
LocalRendererCard(
    item = localRenderer,
    onClick = { onSelectRenderer(localRenderer.nodeId) },
    onReleaseLease = { onReleaseLease(localRenderer.nodeId) },
    onTakeControl = { onTakeControl(localRenderer.nodeId) },
)
```

```kotlin
NetworkRendererCard(
    item = renderer,
    onClick = { onSelectRenderer(renderer.nodeId) },
    onReleaseLease = { onReleaseLease(renderer.nodeId) },
)
```

(Network card does NOT receive `onTakeControl` — remote take-control is out of scope this pass.)

- [ ] **Step 3: Rewrite `LocalRendererCard` menu logic**

Replace the `RendererMenu` call at the bottom of `LocalRendererCard` with the new action-aware variant:

```kotlin
@Composable
private fun LocalRendererCard(
    item: RendererItem,
    onClick: () -> Unit,
    onReleaseLease: () -> Unit,
    onTakeControl: () -> Unit,
) {
    // ... existing body unchanged up to the menu ...

    LocalRendererMenu(
        item = item,
        onRelease = onReleaseLease,
        onTakeControl = onTakeControl,
        onSelect = onClick,
    )
}
```

Add a new composable below `RendererMenu`:

```kotlin
@Composable
private fun LocalRendererMenu(
    item: RendererItem,
    onRelease: () -> Unit,
    onTakeControl: () -> Unit,
    onSelect: () -> Unit,
) {
    var showMenu by remember { mutableStateOf(false) }
    var showConfirmTakeControl by remember { mutableStateOf(false) }

    Box {
        IconButton(onClick = { showMenu = true }, modifier = Modifier.size(36.dp)) {
            Icon(
                imageVector = Icons.Filled.MoreVert,
                contentDescription = "Options",
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
        }
        DropdownMenu(
            expanded = showMenu,
            onDismissRequest = { showMenu = false },
        ) {
            when {
                item.isOwnLease -> {
                    DropdownMenuItem(
                        text = { Text("Release lease") },
                        onClick = {
                            showMenu = false
                            onRelease()
                        },
                    )
                }
                item.leaseOwner != null -> {
                    DropdownMenuItem(
                        text = { Text("Take control") },
                        onClick = {
                            showMenu = false
                            showConfirmTakeControl = true
                        },
                    )
                }
                else -> {
                    // No lease held; nothing lease-related to offer.
                }
            }
            DropdownMenuItem(
                text = { Text("Select") },
                onClick = {
                    showMenu = false
                    onSelect()
                },
            )
        }
    }

    if (showConfirmTakeControl) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { showConfirmTakeControl = false },
            title = { Text("Take control?") },
            text = {
                val owner = item.leaseOwner ?: "another device"
                Text("Take control from ${owner.uppercase()}?")
            },
            confirmButton = {
                androidx.compose.material3.TextButton(
                    onClick = {
                        showConfirmTakeControl = false
                        onTakeControl()
                    },
                ) { Text("Take control") }
            },
            dismissButton = {
                androidx.compose.material3.TextButton(
                    onClick = { showConfirmTakeControl = false },
                ) { Text("Cancel") }
            },
        )
    }
}
```

- [ ] **Step 4: Update `NetworkRendererCard`'s menu**

The network card's existing `RendererMenu(hasLease = item.leaseOwner != null, showRelease = true, ...)` is fine, but the "Force release" alias text misleads users into thinking it force-steals. Tighten it. Replace the existing `RendererMenu` helper:

```kotlin
@Composable
private fun RendererMenu(
    hasLease: Boolean,
    showRelease: Boolean,
    isOwnLease: Boolean,
    onRelease: () -> Unit,
    onSelect: () -> Unit,
) {
    var showMenu by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { showMenu = true }, modifier = Modifier.size(36.dp)) {
            Icon(
                imageVector = Icons.Filled.MoreVert,
                contentDescription = "Options",
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
        }
        DropdownMenu(
            expanded = showMenu,
            onDismissRequest = { showMenu = false },
        ) {
            if (showRelease && hasLease && isOwnLease) {
                DropdownMenuItem(
                    text = { Text("Release lease") },
                    onClick = {
                        showMenu = false
                        onRelease()
                    },
                )
            }
            DropdownMenuItem(
                text = { Text("Select") },
                onClick = {
                    showMenu = false
                    onSelect()
                },
            )
        }
    }
}
```

Update the `NetworkRendererCard` call site:

```kotlin
RendererMenu(
    hasLease = item.leaseOwner != null,
    showRelease = true,
    isOwnLease = item.isOwnLease,
    onRelease = onReleaseLease,
    onSelect = onClick,
)
```

- [ ] **Step 5: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 6: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersViewModel.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/renderers/RenderersScreen.kt
git commit -m "feat(android): add take-control UI and unify lease menu actions"
```

---

## Task 10: `NowPlayingScreen` — disable transport buttons under foreign lease

When the active renderer's state shows a non-own lease, disable play/pause/next/prev and show an explanatory note. Prevents silent `LEASE_MISMATCH` errors from commands that would fail anyway.

**Files:**
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/nowplaying/NowPlayingViewModel.kt`
- Modify: `integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/nowplaying/NowPlayingScreen.kt`

- [ ] **Step 1: Expose lease state from the view model**

In `NowPlayingViewModel.kt`, `NowPlayingUiState` is the existing ui-state data class (see the file's top). Add two fields to it:

```kotlin
data class NowPlayingUiState(
    val playbackStatus: String = "stopped",
    val trackTitle: String? = null,
    val artist: String? = null,
    val album: String? = null,
    val artworkUrl: String? = null,
    val positionMs: Long = 0,
    val durationMs: Long = 0,
    val volume: Float = 1f,
    val isMuted: Boolean = false,
    val shuffle: Boolean = false,
    val repeatMode: String = "",
    val hiResInfo: String? = null,
    val rendererName: String = "This Phone",
    val isConnected: Boolean = false,
    val visualizerEnabled: Boolean = false,
    val isLocalRenderer: Boolean = true,
    val leaseOwner: String? = null,
    val isOwnLease: Boolean = false,
)
```

`NowPlayingViewModel` already injects `settingsDataStore: SettingsDataStore` (confirm by grep). Use it to derive `isOwnLease`.

In the view model's state-assembly pipeline (the block where it builds a `NowPlayingUiState` from the observed `RendererState`), thread in the identity flow and compute the two new fields. The exact assembly uses `combine` / `map` — follow the existing pattern. The inputs you need are the current `RendererState` for the active renderer and `settingsDataStore.identity`:

```kotlin
val session = rendererState.session
val leaseOwner = session?.owner
val appIdentity = settingsDataStore.identity.first() // or bind via combine if the existing pipeline uses flows
val isOwnLease = leaseOwner != null && leaseOwner == appIdentity
// ... pass leaseOwner and isOwnLease into the NowPlayingUiState copy
```

If the existing pipeline uses `combine` over flows rather than suspending `.first()` calls, add `settingsDataStore.identity` as one more source in the `combine`.

- [ ] **Step 2: Disable buttons and show the note in the screen**

In `NowPlayingScreen.kt`, find the transport controls block (play/pause/next/prev buttons). Wrap each button's `onClick` and `enabled` with a guard derived from the ui-state:

```kotlin
val blockedByLease = uiState.leaseOwner != null && !uiState.isOwnLease

IconButton(
    onClick = { if (!blockedByLease) viewModel.togglePlayPause() },
    enabled = !blockedByLease,
) { /* icon */ }

IconButton(
    onClick = { if (!blockedByLease) viewModel.next() },
    enabled = !blockedByLease,
) { /* icon */ }

// ... similar for prev, seek etc.
```

Below the transport row, add a note:

```kotlin
if (blockedByLease) {
    Spacer(modifier = Modifier.height(8.dp))
    Text(
        text = "Controlled by ${uiState.leaseOwner!!.uppercase()} — take control in the renderers menu",
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        modifier = Modifier.fillMaxWidth(),
    )
}
```

(Place and style this to fit the existing visual hierarchy — the exact padding/typography follows what's already in the screen.)

- [ ] **Step 3: Compile check**

Run: `make -C integrations/android_app compile`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 4: Commit**

```bash
git add integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/nowplaying/NowPlayingViewModel.kt \
  integrations/android_app/app/src/main/java/com/mediautopia/app/ui/screen/nowplaying/NowPlayingScreen.kt
git commit -m "feat(android): disable Now Playing transport under foreign lease"
```

---

## Final: End-to-end manual verification

With all commits made, build and install a debug APK, then run through the spec's test plan. Each item is a manual check — no automated test exists for these behaviours.

- [ ] **Step 1: Full build + install**

```bash
make -C integrations/android_app install-pixel
```

Expected: APK built and installed on the Pixel device. Open the app and confirm it connects to the broker (notification reads "Connected").

- [ ] **Step 2: Baseline local control with broker down**

Stop mosquitto (or iptables-drop the broker port). On the phone, tap play on a track. Expected: music starts. Stop/seek/volume/next all work. Restart mosquitto — notification returns to "Connected" within ~60s.

- [ ] **Step 3: Idle disconnect recovery**

Leave the app foregrounded but idle for 2+ minutes with the screen off. Toggle airplane mode on and off. Expected: notification returns to "Connected" within ~60s without touching the app.

- [ ] **Step 4: Force reconnect button doesn't get stuck**

Open the renderers sheet, tap Reconnect 5 times rapidly. Expected: each tap trips state to DISCONNECTED briefly; the app reconnects cleanly without needing force-close.

- [ ] **Step 5: Release own lease**

Start playback on the phone (phone holds its own lease — "CONTROLLED" badge visible with a countdown). Renderers menu → Release lease. Expected: badge clears. Tap play again — lease re-acquires, badge reappears.

- [ ] **Step 6: Take control from HA**

On HA, send a command that acquires the phone's lease (e.g. start playback via HA). In the Android app, the local card shows "LEASE: HOME-ASSISTANT" and transport buttons on Now Playing are disabled with the "Controlled by HOME-ASSISTANT" note. Open renderers menu → Take control → confirm dialog → accept. Expected: lease flips to the phone, transport buttons re-enable. HA will get `LEASE_MISMATCH` on its next renewal.

- [ ] **Step 7: Broker-down take-control**

Stop mosquitto. On the phone, take control (if the lease is held by HA from a prior run, this tests the in-process force-acquire path). Expected: take-control succeeds purely in-process with no broker involvement.

- [ ] **Step 8: Heartbeat-driven recovery**

On the broker host, `iptables -A INPUT -p tcp --dport 1883 -j DROP` for 45 seconds, then remove the rule. Expected: within ~45s of restoration, the Android notification returns to "Connected" without user action — the heartbeat watchdog detects the silent death and trips the reconnect loop.

---

## Self-review notes

**Spec coverage:** Each Design section maps to explicit tasks — Section 1 (transport) → Tasks 4, 5, 6; Section 2 (lease UX) → Tasks 7, 8, 9; Section 3 (MQTT lifecycle) → Tasks 1, 2, 3. `NowPlayingScreen` transport-control feedback (Section 2) is Task 10. All "Files touched" entries in the spec have a corresponding task.

**Out of scope, as declared:** Go-side `session.takeControl` server support is not implemented in this plan (confirmed by Task 9 Step 2 deliberately not passing `onTakeControl` to `NetworkRendererCard`).

**Verification:** No automated tests; final manual verification section covers each item from the spec's test plan.
