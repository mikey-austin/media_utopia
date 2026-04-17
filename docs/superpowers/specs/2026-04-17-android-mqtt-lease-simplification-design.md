# Android app: MQTT + lease simplification

**Date:** 2026-04-17
**Scope:** `integrations/android_app/`

## Problem

The Android app's MQTT connection is unreliable when idle or backgrounded. Symptoms observed by the user:

- Silent idle disconnects (HiveMQ's `DisconnectedListener` never fires, so the UI still reports CONNECTED while commands time out).
- "Force reconnect" in the UI getting stuck — a second tap has no effect until the app is force-closed and reopened.
- The local lease on the phone's own renderer cannot be released from the UI.
- The phone appears to "fight itself": every local control action round-trips the broker and validates a lease, even though controller and renderer are the same process.

The user wants leases preserved as a real multi-device feature (Home Assistant can still take exclusive control, and the UI should reflect that) but invisible in the normal single-device case.

## Root cause summary

1. **Two retry loops collide.** The HiveMQ client runs its own auto-reconnect (2–30s backoff). `MqttForegroundService.triggerHardReconnect` runs a second, independent reconnect on top. They race.
2. **`disconnect()` can hang.** It awaits `whenComplete` without a timeout. When the HiveMQ client is stuck, the callback never fires, the reconnect mutex is never released, and every subsequent reconnect tap gets swallowed by the `if (existing.isActive)` guard in `triggerHardReconnect`.
3. **No silent-socket-death detector.** On Android Doze, the TCP socket can be reclaimed without an FIN. Neither HiveMQ's keepalive nor the `DisconnectedListener` catches this until the next publish fails.
4. **Local control goes through the broker.** The controller-side `LeaseManager` and `CommandCorrelator` publish to `mu/v1/cmd/{localNodeId}` and wait for a reply on `mu/v1/reply/{clientId}` to drive a renderer running in the same Android process. When MQTT is sick, local playback stops working even though nothing about it needs the network.
5. **Lease UX has no escape hatch.** `LocalRendererCard` hardcodes `showRelease = false`. The renderer engine's `acquire` refuses when a lease is held — there's no force-steal path. If HA crashes mid-lease, the phone is locked out for up to 5 minutes (the TTL).

## Design

Three coordinated changes, each addressing one root cause. They compose: the in-process transport (1) eliminates the broker from the local hot path, the reconnect rewrite (3) makes the broker reliable for the remote paths that remain, and the lease UX changes (2) give the user an escape hatch when another controller genuinely holds the lease.

### 1. Transport abstraction & in-process local path

Introduce a `RendererTransport` interface between the controller-side code and the MQTT plumbing:

```kotlin
interface RendererTransport {
    suspend fun send(
        nodeId: String,
        envelope: CommandEnvelope,
        timeout: Duration = 2.seconds,
    ): ReplyEnvelope

    fun sendFireAndForget(nodeId: String, envelope: CommandEnvelope)
}
```

Two implementations:

- **`MqttTransport`** — today's `CommandCorrelator.send` logic, unchanged. Publishes to `mu/v1/cmd/{nodeId}`, awaits correlated reply on `mu/v1/reply/{clientId}`.
- **`LocalTransport`** — holds an injected reference to `LocalRendererService` (registered on service start, cleared on stop). Dispatches envelopes through a new `LocalRendererService.processLocal(cmd): ReplyEnvelope` method that reuses the same threading logic as the MQTT-driven path: session commands inline, playback commands on `Dispatchers.Main` with audio-focus requests, everything else on the engine dispatcher. The only behavioural difference vs the current MQTT-driven handler is that the reply is *returned* to the caller instead of published via `publishReply`. No envelope `id` correlation needed. The MQTT receive path keeps its `cmdChannel` backpressure; the in-process path skips the channel (concurrency is already handled by the engine's locks).

A `TransportRouter` wraps both and picks at send time: if `nodeId == localRendererService.nodeId` AND the engine is registered → `LocalTransport`; else → `MqttTransport`.

**`LeaseManager` and any other command-sending caller** (`MainViewModel`, `NowPlayingViewModel`, `QueueViewModel`, etc.) receive the router via DI and never branch on locality themselves.

**State propagation for remote observers** is unchanged. `LocalRendererEngine.stateFlow` is still published to `mu/v1/state/{nodeId}` by `LocalRendererService`, so HA and the desktop app still see state updates normally.

**Broker-down behaviour.** With `LocalTransport`, the local UI can control local playback with no broker at all. The phone still appears to remote controllers as disconnected (no retained state updates propagate), but the user's ability to play music on their own phone is no longer coupled to MQTT health.

**Identity for envelopes.** `LocalTransport` uses the same `controllerIdentity` that `CommandCorrelator` uses (the `identity` from `SettingsDataStore`). So the lease `owner` field looks the same whether the envelope came via MQTT or in-process — remote state observers see `owner = mikey` (or whatever the user set) regardless of transport.

### 2. Lease model & release/take-control UX

**Engine side** (`RendererLeaseManager` in `LocalRendererEngine.kt`):

Add a force-acquire path that overwrites an existing lease:

```kotlin
fun forceAcquire(requestOwner: String, ttlMs: Long): SessionLease {
    lock.withLock {
        clearLocked()
        return newLeaseLocked(requestOwner, ttlMs)
    }
}
```

`acquire` stays strict (null when held). `renew` and `release` are unchanged.

Dispatch a new command type in `LocalRendererEngine.handleSessionCommand`:

- `session.takeControl` → calls `forceAcquire`, returns the new lease in a `SessionReplyBody` identical to `session.acquire`. No lease validation on entry — this is the whole point.

Symmetric server-side support (Go renderers) is **out of scope** for this spec — the Android app implements the client half. Server support can be added separately when we want force-steal to work against network renderers.

**Controller side** (`LeaseManager`):

Add:

```kotlin
suspend fun takeControl(rendererId: String): Lease
```

Sends `session.takeControl` via the transport router, caches the returned lease in the same map as regular leases, publishes the updated `_leaseInfos`.

Simplify `releaseLease`: drop the "acquire then release" fallback. If we don't hold a cached lease, `releaseLease` is a no-op (just clear the cache and return). Callers that want contested-takeover semantics should call `takeControl` instead.

**UI** (`RenderersScreen.kt`):

Remove the `showRelease = false` hardcode on `LocalRendererCard`. Menu action depends on the lease state and whether the renderer is local:

| Card | `isOwnLease` | `leaseOwner != null` | Menu action |
|---|---|---|---|
| Local | true  | true  | "Release lease" → `LeaseManager.releaseLease(nodeId)` |
| Local | false | true  | "Take control" → confirm dialog → `LeaseManager.takeControl(nodeId)` |
| Local | —     | false | No lease action (just "Select") |
| Network | true | true | "Release lease" → `LeaseManager.releaseLease(nodeId)` |
| Network | false | true | No lease action — remote server doesn't yet support `session.takeControl`. Status text shows "Held by {OWNER}" so the user understands why. |
| Network | — | false | No lease action |

The confirm dialog text for local take-control: "Take control from {OWNER}?" with Cancel / Take Control buttons. No confirmation on release (low-stakes).

Why no remote take-control: `session.takeControl` is a new protocol command. Until the desktop and gstreamer renderers implement it server-side, wiring up UI that silently fails would regress the UX. Remote lease contention still resolves via the 5-min TTL as today.

**Transport-control feedback** in `NowPlayingScreen`: when the active renderer's state shows a lease held by a non-`isOwnLease` owner, disable play/pause/next/prev buttons and show a thin note under the transport row: "Controlled by {OWNER} — take control in the renderers menu". Prevents users from tapping controls that would silently fail with `LEASE_MISMATCH`.

### 3. MQTT connection lifecycle

Rewrite `MqttConnectionManager` and the reconnect plumbing in `MqttForegroundService` for a single authoritative reconnect loop.

**Drop HiveMQ auto-reconnect.** Remove the `.automaticReconnect()` block from the client builder. The client connects only when we call `connectWith().send()`, and stays disconnected when any connection drops.

**Deterministic timeouts on connect and disconnect:**

- `connect()` wraps `whenComplete` in `withTimeout(10.seconds)`. On timeout: null the client reference, transition to `DISCONNECTED`, propagate a connect failure.
- `disconnect()` wraps in `withTimeout(5.seconds)`. On timeout: null the client reference and mark `DISCONNECTED` anyway. The mutex is guaranteed to release.

The "null the client" step is what prevents the current stuck-client symptom: we never wait indefinitely for a zombie HiveMQ client to acknowledge a call. An in-flight publish whose `whenComplete` fires after we've nulled the reference just logs and discards — the command had already timed out from the caller's perspective (via `CommandCorrelator`'s `withTimeout`).

**Connection state enum reduces to three states:**

```kotlin
enum class ConnectionState {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
}
```

`RECONNECTING` is removed — with one loop owned by the app, there's no intermediate state to represent.

**Single reconnect loop in `MqttForegroundService`:**

A dedicated coroutine collects `mqttConnectionManager.connectionState` (distinct-until-changed) within the service's scope. On each transition *into* `DISCONNECTED` while the service is running:

```
on DISCONNECTED transition:
  delay(backoff)   // 2s, 4s, 8s, 16s, 30s, 30s, ...
  if state is no longer DISCONNECTED, abort (someone else already reconnected)
  reconnectMutex.withLock {
      stopSession()
      startSession()
  }
  if startSession succeeded and state stays CONNECTED for 60s, reset backoff to 2s
```

The loop lives on the service scope, so `onDestroy`'s `scope.cancel()` ends it cleanly.

The UI reconnect button and the `ConnectivityManager.NetworkCallback.onAvailable` callback no longer call `triggerHardReconnect` directly. They just call a new `MqttConnectionManager.markDisconnected()` that forces state to `DISCONNECTED`. The reconnect loop picks up from there. One path, one backoff policy, no mutex-deadlock window.

**Heartbeat watchdog** (silent-socket-death detector):

While `CONNECTED`, a coroutine:

1. Subscribes once to `mu/v1/heartbeat/{clientId}` (QoS 0).
2. Every 30 seconds, publishes an empty payload to the same topic (QoS 0, not retained).
3. Resets a `lastEcho` timestamp each time the subscribe handler fires.
4. If `now - lastEcho > 40s` (heartbeat interval + 10s grace), calls `markDisconnected()` to trip the reconnect loop.

Why self-echo over HiveMQ's built-in ping: the MQTT PINGREQ/PINGRESP cycle is handled inside the client library — on Doze-reclaimed sockets, the library can sit in a state where it thinks pings are succeeding when no bytes are flowing. A full publish → broker → subscribe roundtrip forces real bidirectional traffic and surfaces a dead connection.

**Backoff reset condition:** a successful `CONNECTED` transition that stays stable for 60s resets the backoff to 2s. Without the 60s soak, flapping (connect → immediate fail → reconnect → immediate fail) would stay at the minimum interval forever.

**Cost:** one publish + one small receive every 30s per phone per broker. Bandwidth is negligible; Doze wakeup cost is mitigated because the service is already in a foreground state.

## Files touched

New files:
- `data/mqtt/RendererTransport.kt` — interface + `MqttTransport` + `LocalTransport` + `TransportRouter`.

Modified:
- `data/mqtt/MqttConnectionManager.kt` — drop auto-reconnect, add timeouts, `markDisconnected()`, `heartbeat` loop, remove `RECONNECTING` state.
- `service/MqttForegroundService.kt` — replace `triggerHardReconnect` with reconnect-loop coroutine; `NetworkCallback` calls `markDisconnected()`.
- `domain/usecase/LeaseManager.kt` — route through `TransportRouter` instead of `CommandCorrelator` directly; add `takeControl`; simplify `releaseLease`.
- `domain/usecase/CommandCorrelator.kt` — stays, but now only one of two transports.
- `renderer/LocalRendererEngine.kt` — add `forceAcquire` and `session.takeControl` dispatch.
- `renderer/LocalRendererService.kt` — register/unregister itself with the `LocalTransport` on start/stop; expose a new `processLocal(cmd): ReplyEnvelope` that reuses the MQTT-driven threading logic but returns the reply.
- `ui/screen/renderers/RenderersScreen.kt` — unified menu action, remove `showRelease = false`, confirm dialog for take-control.
- `ui/screen/renderers/RenderersViewModel.kt` — `takeControl(nodeId)` entry point.
- `ui/screen/nowplaying/NowPlayingScreen.kt` — disable transport buttons under foreign lease, add note.

## Out of scope

- Go-side renderer support for `session.takeControl`. The Android engine and Android controller agree on the command string in-process only; no other component needs to know, so no Go files are touched in this change. Cross-renderer force-steal is future work.
- Android foreground service / notification behaviour beyond what the existing code does.
- Changes to the existing queue, library, or metadata-resolution flows.

## Test plan

Manual verification (real broker + real phone):

1. **Baseline local control with broker down.** Stop mosquitto. Tap play on the phone — music starts. Stop, seek, volume, next — all work. Reconnects automatically when broker returns.
2. **Idle disconnect recovery.** Put the phone in Doze (airplane mode toggle or developer settings), wait 2 min, restore network. Notification text returns to "Connected" within ~60s without user action.
3. **Force reconnect button.** Tap reconnect repeatedly while idle — each tap trips state to DISCONNECTED and the loop rebuilds. No stuck state.
4. **Lease release (own).** Control local playback (phone holds its own lease). Open renderers sheet, menu → Release lease. Lease indicator clears, subsequent play re-acquires.
5. **Take control from HA.** From HA, acquire the phone's lease (play something). In the Android app, local card shows "LEASE: HOME-ASSISTANT" and transport controls are disabled. Menu → Take control → confirm. Lease flips to the phone, HA gets `LEASE_MISMATCH` on next renewal.
6. **Broker-down lease take-control.** With broker offline, take-control on local renderer still works (in-process call on `RendererLeaseManager.forceAcquire`).
7. **Heartbeat-driven recovery.** Simulate Doze-dead socket (iptables drop on the broker port for 45s). Within ~45s of traffic resumption, the watchdog trips and reconnects.

No automated tests added — the existing Android module has no test suite, and the behaviours under test are all cross-process / network. Adding a harness is out of scope.
