# `mu` CLI v1 Specification

**Purpose:** Reference interactive client for Media Utopia (`mu`).
**Design:** Stateless, lease-aware, scripts well, HA-friendly.
**Exit codes:** Unix-like, predictable.

---

## 1. Global behavior

### Command format

```bash
mu [GLOBAL_OPTS] <command> [subcommand] [args...] [flags...]
```

### Global options

* `-b, --broker <url>` MQTT broker URL (e.g. `mqtts://broker.local:8883`)
* `--topic-base <prefix>` default `mu/v1` (for dev/test)
* `-i, --identity <id>` controller identity string (default: hostname or `mu:<user>@<host>`)
* `-t, --timeout <dur>` command timeout (default `2s`)
* `-q, --quiet` suppress non-essential output
* `-j, --json` output JSON
* `--no-color` disable color
* `-v, --verbose` more logs (stderr)
* `--tls-ca <path>`, `--tls-cert <path>`, `--tls-key <path>` TLS options
* `--user <u>`, `--pass <p>` MQTT auth (if used)

### Target renderer selection (common)

Many commands accept a renderer “selector”:

* explicit ID: `mu:renderer:...`
* friendly alias: `livingroom`, `kitchen`
* `@default` (if configured)

Resolution algorithm (v1):

1. If argument starts with `mu:` treat as exact nodeId
2. Else match against presence `name` or configured aliases
3. If multiple matches → error (exit 2) with suggestions

---

## 2. Output formats

### Default (human)

Concise, one-line friendly, like:

```
Living Room  [playing]  Miles Davis – So What  01:04 / 05:22  vol 72%
Queue: 12 tracks (index 4)  rev 102  owner phone-mikey (lease 9s)
```

### `--json`

Always prints a single JSON object or array, stable keys, no extra text.

### `--quiet`

Suppresses progress and banners; still prints required data.

---

## 3. Exit codes

* `0` success
* `1` runtime error (network, timeout, server error)
* `2` usage error / ambiguous selector / invalid args
* `3` permission denied / lease required / lease mismatch
* `4` not found (renderer/playlist/item)
* `5` conflict (revision mismatch)

---

## 4. Command surface

### 4.1 `mu ls` — list nodes

```bash
mu ls [--kind renderer|library|playlist|advisor] [--online] [--json]
```

Output: table of nodes (id, kind, name, caps, online).

---

### 4.2 `mu status` — show renderer status

```bash
mu status <renderer> [--watch] [--json]
```

* `--watch`: stream updates (subscribes to state + evt), prints on change.

---

### 4.3 Session/lease commands

#### Acquire

```bash
mu acquire <renderer> [--ttl <dur>] [--wait] [--json]
```

Defaults:

* `--ttl 5m`
* prints session id + lease expiry

`--wait`: if currently owned, wait until released/expired (bounded by timeout unless `--timeout` increased).

#### Renew

```bash
mu renew <renderer> [--ttl <dur>] [--json]
```

#### Release

```bash
mu release <renderer> [--json]
```

#### Who owns it?

```bash
mu owner <renderer> [--json]
```

---

### 4.4 Playback controls

```bash
mu play   <renderer> [--index <n>|--entry <queueEntryId>]
mu pause  <renderer>
mu toggle <renderer>
mu stop   <renderer>
mu seek   <renderer> <+/-dur|ms>
mu next   <renderer>
mu prev   <renderer>
```

Notes:

* These **require lease** (exit 3 if not held).
* `seek` accepts `+10s`, `-5s`, `120000ms`, `2m03s`.
* If your shell treats `-5s` as a flag, use `-- -5s`.

---

### 4.5 Volume controls

```bash
mu vol <renderer> [<0..100>|<+/-n>] [--mute|--unmute]
```

Examples:

```bash
mu vol livingroom 35
mu vol livingroom +5
mu vol livingroom --mute
```

Lease required.
If your shell treats `-5` as a flag, use `-- -5`.

---

## 5. Queue commands (canonical)

### 5.1 Inspect queue

```bash
mu queue list <renderer> [--from <n>] [--count <n>] [--full] [--json]
mu queue now  <renderer> [--json]
```

`--full` shows full ids instead of truncated versions.

### 5.2 Modify queue (lease required)

When a lease is missing, `mu` will automatically acquire one and retry the command (it prints a notice unless `--quiet` is set).

```bash
mu queue clear <renderer>
mu queue jump  <renderer> <index>
mu queue rm    <renderer> <index|queueEntryId>
mu queue mv    <renderer> <from> <to>
mu queue shuffle <renderer> [--seed <int>]
mu queue repeat  <renderer> off|all|one
```

### 5.3 Add items

```bash
mu queue add <renderer> <item...> [--at <index>|--next|--end] [--resolve=auto|yes|no]
```

`<item...>` can be:

* `mu:track:...` (canonical)
* `lib:<libraryAlias>:<libraryItemId>` (shorthand)
* URL (http/https) → treated as resolved source-only
* A playlist reference: `playlist:<playlistId>` (loads entries)

Default placement: `--end`.

`--resolve`:

* `auto` (default): if renderer caps say `queueResolve=true`, send refs; else resolve now.
* `yes`: resolve now via library
* `no`: send refs only (error if renderer can’t resolve)

### 5.4 Set queue atomically from file/stdin

```bash
mu queue set <renderer> --file <path>|- [--format muq|json] [--if-rev <n>]
```

* `muq` is a simple line format:

  * one item per line: `mu:track:...` or URL
  * comments start with `#`

`--if-rev` enables optimistic concurrency (exit 5 on mismatch).

---

## 6. Playlist server commands (required in v1)

### 6.1 List/get playlists

```bash
mu playlist ls [--server <playlistServer>] [--json]
mu playlist show <playlistId|name> [--server <playlistServer>] [--full] [--json]
```

### 6.2 Create / edit

```bash
mu playlist create "<name>" [--from-snapshot <snapshotId|name>] [--server <playlistServer>] [--json]
mu playlist add <playlistId|name> <item...> [--server <playlistServer>] [--json]
mu playlist rm  <playlistId|name> <entryId|index...> [--server <playlistServer>] [--json]
mu playlist delete <playlistId|name> [--server <playlistServer>] [--json]
mu playlist rename <playlistId|name> "<name>" [--server <playlistServer>]
```

Notes:
- `playlistId` is the full playlist URN from `mu playlist ls`, or the playlist name (unique within the playlist server).
- Human output resolves library metadata; use `--json` for raw entries.
- Use `--full` to show entry/item ids in the table.
- Items can be URLs, mu URNs, or library refs (`lib:<selector>:<itemId>`).
- `selector` may be a library alias, name, or full node id (URN).
- Container items (albums/artists) expand into their playable tracks when added to a playlist.
- `--from-snapshot` creates a new playlist seeded with snapshot items (by id or name).

### 6.3 Load playlist into renderer queue (lease required)

```bash
mu playlist load <renderer> <playlistId|name> [--mode replace|append|next] [--resolve=auto|yes|no] [--server <playlistServer>]
```

---

## 7. Snapshots (session queue save/restore)

### Save current queue (lease required to ensure correctness)

```bash
mu snapshot save <renderer> "<name>" [--server <playlistServer>] [--json]
```

### List / restore

```bash
mu snapshot ls [--server <playlistServer>] [--json]
mu snapshot rm <snapshotId|name> [--server <playlistServer>]
mu snapshot load <renderer> <snapshotId> [--mode replace|append|next] [--resolve=auto|yes|no] [--server <playlistServer>]
```

---

## 8. Library browsing/searching

### List libraries

```bash
mu lib ls [--json]
```

### Browse

```bash
mu lib browse [library] [containerId] [--start <n>] [--count <n>] [--json]
```

Notes:
- Omit `containerId` to browse the library root.
- Use `--container <id>` to disambiguate when relying on defaults.
- Library selectors can be an alias, the library name, or the full node id (URN).
- `containerId` values are library-specific (Jellyfin uses empty/root to list top-level folders).

### Search

```bash
mu lib search <library> "<query>" [--start <n>] [--count <n>] [--type <list>] [--json]
```

Library selectors can be an alias, the library name, or the full node id (URN).

`--type` is a comma-separated list of canonical types:

`Audio,MusicAlbum,MusicArtist,Movie,Series,Episode,Video,Playlist,Folder,Podcast,PodcastEpisode,Camera`

### Resolve

```bash
mu lib resolve <library> <itemId> [--json]
```

Convenience: `mu play <renderer> <item>` can accept `lib:<alias>:<id>` and auto-resolve/load.

---

## 9. Suggestions (advisor-driven, future-ready)

### List suggestions (read-only)

```bash
mu suggest ls [--server <playlistServer>] [--json]
mu suggest show <suggestionId> [--server <playlistServer>] [--json]
```

### Promote to playlist

```bash
mu suggest promote <suggestionId> "<playlist name>" [--server <playlistServer>] [--json]
```

### Apply to renderer (lease required)

```bash
mu suggest load <renderer> <suggestionId> [--mode replace|append|next] [--resolve=auto|yes|no] [--server <playlistServer>]
```

---

## 10. Configuration file (recommended)

Location (v1):

* `$XDG_CONFIG_HOME/mu/config.toml` or `~/.config/mu/config.toml`

Suggested keys:

```toml
broker = "mqtts://broker.local:8883"
identity = "mikey@pixel"
topic_base = "mu/v1"

[aliases]
livingroom = "mu:renderer:upnp:uuid:550e..."
kitchen    = "mu:renderer:kodi:main:player"

[defaults]
renderer = "livingroom"
playlist_server = "mu:playlist:plsrv:default:main"
library = "mu:library:jellyfin:home:server"
```

---

## 11. Command → MQTT mapping (normative)

`mu` MUST:

* subscribe to `mu/v1/node/+/presence` to resolve selectors
* for a target renderer:

  * publish to `mu/v1/node/<rendererId>/cmd`
  * read retained `mu/v1/node/<rendererId>/state`
  * optionally read `mu/v1/node/<rendererId>/evt`
* include:

  * `id` (uuid)
  * `type`
  * `ts`
  * `replyTo` topic unique to `mu` instance
  * `lease` object for mutations

---

## 12. Quality-of-life rules (like `mpc`, but better)

* `mu` SHOULD accept commands without specifying renderer if configured default exists:

  ```bash
  mu play
  mu pause
  mu queue list
  ```
* `mu` SHOULD print “lease needed” with hint:

  ```
  lease required: run 'mu acquire livingroom'
  ```
* `--watch` must be stable: reconnect, resubscribe, continue.

---
