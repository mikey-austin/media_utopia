# Media Utopia Android App

Native Android controller and renderer for the Media Utopia multi-room audio system.

## Features

- **Full MU Controller** -- browse libraries, control playback, manage queues and playlists, select renderers and zones
- **Local Renderer** -- the phone itself acts as an MU renderer, receiving MQTT commands and playing audio via ExoPlayer
- **Multi-room Zones** -- master volume and per-zone gain/mute/source control
- **"Sonic Curator" Design** -- premium dark theme inspired by high-end audio hardware

## Architecture

The app is both a controller (sends commands) and a renderer (receives commands), mirroring the GNOME applet architecture. All communication is JSON over MQTT.

```
Single MQTT Connection
+-- Renderer (LocalRendererService)
|   +-- Subscribes to mu/v1/node/{ownNodeId}/cmd
|   +-- Processes commands via LocalRendererEngine + ExoPlayerDriver
|   +-- Publishes presence + state (MQTT retained)
|
+-- Controller (Repositories + ViewModels)
    +-- Discovers nodes via presence wildcard
    +-- Sends commands via CommandCorrelator
    +-- Manages leases via LeaseManager
```

Commands flow through MQTT even when controlling the local renderer, ensuring external controllers always see consistent state.

## Requirements

- Android SDK 35+ (platforms and build-tools)
- JDK 21 (JDK 25 is not supported by Gradle)
- Gradle 8.14.1 (wrapper included)

## Build

```bash
# Verify environment
make check-env

# Compile (fast check)
make compile

# Build debug APK
make debug

# Build and install on connected device
make install

# Build, install, and launch
make run

# All targets
make help
```

Or use Gradle directly:

```bash
export ANDROID_HOME=~/Android/Sdk
export JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64
./gradlew assembleDebug
```

## Configuration

On first launch, go to Settings (gear icon) to configure:

- **Broker URL** -- MQTT broker address (default: `mqtt://localhost:1883`)
- **Identity** -- controller identity string

## Tech Stack

| Component | Technology |
|-----------|------------|
| Language | Kotlin |
| UI | Jetpack Compose + Material 3 |
| MQTT | HiveMQ MQTT Client |
| Audio | AndroidX Media3 (ExoPlayer) |
| DI | Hilt |
| Images | Coil |
| JSON | kotlinx.serialization |
| Persistence | DataStore |

## Project Structure

```
app/src/main/java/com/mediautopia/app/
  data/
    mqtt/        -- MQTT connection, topic builders
    protocol/    -- Wire types (port of pkg/mu/protocol.go + bodies.go)
    repository/  -- Node discovery, state observation, library, zones
    cache/       -- Metadata LRU cache, settings, lease store
  domain/
    model/       -- Domain models
    usecase/     -- Command correlation, lease management
  renderer/      -- Local renderer (ExoPlayer driver, engine, queue, dedup)
  service/       -- MQTT foreground service
  ui/
    theme/       -- Sonic Curator design system
    navigation/  -- Bottom nav, routes
    components/  -- Shared components (MiniPlayer, HiResBadge, GradientButton)
    screen/      -- Now Playing, Library, Renderers, Zones, Queue, Settings
```

## Screens

| Screen | Description |
|--------|-------------|
| Now Playing | Album art, transport controls, seek scrubber, volume |
| Library | Hierarchical browsing, search, album grid, track list |
| Renderers | "This Phone" + network renderer discovery and selection |
| Zones | Master volume, per-zone gain/mute/source control |
| Queue | Drag-to-reorder, swipe-to-remove, metadata resolution |
| Settings | Broker URL, identity, connection status |
