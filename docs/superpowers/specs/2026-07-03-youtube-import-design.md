# YouTube Playlist Import for fs_library — Design

Give `mu lib import <playlist-url>` a YouTube playlist URL and the
fs_library server downloads every track as FLAC with artwork and metadata
into its own library root, asynchronously, then rescans so the tracks
appear in browse/search/enrichment like any other files.

## Decisions (from design discussion, 2026-07-03)

- **One-shot import**, idempotent: re-running the same URL downloads only
  entries added to the playlist since (yt-dlp `--download-archive`).
  Subscriptions/auto-sync are out of scope.
- **Layout**: `<import_dir>/<Playlist Title>/<NN - Track Title>.flac`.
  `import_dir` is a new fs_library config key; relative values resolve
  under the first library root, default `"youtube"`. Absolute values are
  allowed but must lie inside one of the configured roots (validated at
  NewModule; fail fast — files outside the roots would never be scanned).
- **Tags**: album = playlist title, artist = video channel/uploader,
  track number = playlist index, embedded thumbnail as art; the first
  video's thumbnail is also written as `cover.jpg` in the album dir.
- **Visibility**: job status command, no streaming. `library.import`
  returns a job id immediately; `library.imports` lists active/recent
  jobs with progress counts. Job state is in-memory (lost on restart —
  acceptable; the archive file keeps the downloads idempotent).
- **Placement**: inside fs_library (it owns roots, scanning, enrichment,
  embeddings — a rescan at job end wires everything up for free). Code
  lives in a new `internal/modules/fs_library/importer.go`.

## Architecture

```
mu lib import <url>            mu lib imports
      │                              │
      ▼                              ▼
library.import ──► job queue ──► worker goroutine (one at a time, goSafe)
  {url}   ◄─{jobId}                  │
                                     ├─ 1. yt-dlp --flat-playlist -J   → title, entry count
                                     ├─ 2. yt-dlp download run        → FLAC + tags + art
                                     │      (stdout filepaths stream → job.Done++)
                                     ├─ 3. first thumbnail → cover.jpg
                                     └─ 4. async rescan → items indexed, enrichment/embeddings follow
```

### Commands (MQTT, on the fs_library node)

- `library.import` body `{ "url": "<playlist or video url>" }` → reply
  `{ "jobId": "...", "status": "queued" }`. Validation: non-empty http(s)
  URL; yt-dlp binary present (clean error otherwise).
- `library.imports` body `{}` → reply `{ "jobs": [ { "jobId", "url",
  "playlist", "state", "done", "failed", "total", "startedAt",
  "finishedAt", "error" } ] }`, newest first, capped at the 20 most
  recent. States: `queued | running | done | failed`.

Both are lease-free (library commands, consistent with browse/search).
Presence gains cap `"import": true` so clients can discover support.

### Import worker

Single worker goroutine started by `Run` (serial queue: one yt-dlp
process at a time, polite to YouTube; additional `library.import` calls
queue behind it). Each job, under a 2-hour context bounded by the module
context (shutdown cancels cleanly, `goSafe`-wrapped):

1. **Probe**: `yt-dlp --flat-playlist -J <url>` → playlist title +
   entry count (job.Total). Missing/invalid playlist fails the job here
   with yt-dlp's stderr tail as the error.
2. **Download**: one yt-dlp run for the whole playlist:

   ```
   yt-dlp -x --audio-format flac --audio-quality 0
     --embed-metadata --embed-thumbnail
     --parse-metadata "playlist_index:%(track_number)s"
     --ignore-errors --no-overwrites
     --download-archive <albumDir>/.yt-archive
     --print after_move:filepath
     -o "<importDir>/%(playlist_title)s/%(playlist_index)02d - %(title)s.%(ext)s"
     <url>
   ```

   stdout is consumed line-by-line: each printed filepath increments
   `job.Done`, and each "has already been recorded in the archive" line
   increments `job.Skipped` (live progress for `mu lib imports`).
   `--download-archive` makes re-imports skip existing entries.
   Unavailable/region-locked videos are skipped by `--ignore-errors`; at
   the end `job.Failed = job.Total − job.Done − job.Skipped` (floored at
   0 — flat-playlist counts can include entries yt-dlp later merges).
3. **Cover art**: if the album dir has no `cover.*`, download the first
   entry's thumbnail as `cover.jpg` (embedded art already works; this
   feeds the album-grid/cover detection path).
4. **Rescan**: trigger the module's async rescan so the tracks index
   immediately (instead of waiting for the 15-minute tick). Enrichment,
   genre classification, and embeddings then run through the existing
   pipeline. YouTube albums will usually be MusicBrainz misses and get
   negative-cache sidecars — expected and fine; embedded tags carry
   title/artist/album for browse and search.

Path components from playlist/track titles are sanitized (slashes,
colons, NULs → `_`) before hitting the filesystem; yt-dlp's own
`--restrict-filenames` is NOT used (keeps human-readable unicode names,
which the library handles fine).

### Config (mud `[modules.fs_library.*]`)

- `import_dir` (string, default `"youtube"`): destination for imports.
  Relative → joined to the first root. Absolute → must be inside one of
  the roots (validated at NewModule, error otherwise).
- `yt_dlp_path` (string, default `"yt-dlp"`): binary override, mirrors
  the podcast module.

### CLI

- `mu lib import [library] <url>` → prints `import started: <jobId>
  (<playlist title if known>)`. Library selector follows the usual
  default/forgiving rules.
- `mu lib imports [library]` → table: `STATE  PROGRESS  PLAYLIST  URL
  JOB_ID(dim)`, e.g. `running  7/23`. Standard footer/empty states.

### Packaging

`mud-library` image adds yt-dlp the same way the full `mud` image does
(python3 + pip install yt-dlp). Note: yt-dlp needs periodic refreshes as
YouTube changes; image rebuilds pick up the latest.

### Quality note

YouTube serves lossy audio (Opus ~130k); `--audio-format flac` wraps it
losslessly but cannot restore what was never there. FLAC was chosen
deliberately (consistent library format, no further generation loss,
existing `include_exts` untouched).

## Error handling summary

| Failure | Behavior |
| --- | --- |
| yt-dlp missing | `library.import` errors immediately ("yt-dlp not found on library host") |
| Bad/unresolvable URL | job → `failed`, error = yt-dlp stderr tail |
| Individual video unavailable | skipped, counted in `failed`, job still `done` |
| mud restart mid-job | job state lost; re-running the URL resumes via `--download-archive` |
| import_dir outside roots | NewModule error at startup (fail fast) |

## Testing

- Unit: job table state transitions; output-line parsing (filepath →
  progress); import_dir validation (relative/absolute/outside-root);
  command handlers with a fake yt-dlp runner (the podcast module's
  `ytDlpRunner` injection pattern).
- Integration: fake yt-dlp script that "downloads" by copying fixture
  FLACs; assert files land in `<import_dir>/<playlist>/`, rescan indexes
  them, `library.imports` reports done counts.
- Live acceptance: real playlist import on venus; tracks browsable and
  searchable; re-import is a no-op; `mu lib imports` shows progress.

## Out of scope

Subscriptions/periodic re-sync, non-YouTube sources (anything yt-dlp
supports will mostly work, but only YouTube is tested), per-import
destination overrides, download rate limiting config, job persistence
across restarts.
