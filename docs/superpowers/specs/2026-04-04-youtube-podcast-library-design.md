# YouTube Support for Podcast Library Module

## Context

The podcast library module (`internal/modules/podcast_library`) currently supports RSS feeds for browsing and playing podcast episodes. YouTube channels and playlists host podcast-style content that follows the same data model: a feed (channel/playlist) contains episodes (videos) with title, description, duration, author, thumbnail, and a playback URL.

Rather than creating a separate YouTube library module, we extend the existing podcast library to support YouTube as a feed type. The browse/search/resolve/caching infrastructure is reused as-is. Only the fetch mechanism (RSS parser vs yt-dlp) and resolve path (direct URL vs on-demand extraction) differ.

## Design

### Config Changes

The existing `feeds` field remains unchanged for RSS URLs. A new `youtube_playlists` field is added alongside it:

```toml
[modules.podcast.default]
enabled = true
name = "Podcasts"
feeds = [
    "https://example.com/feed.xml",
    "https://other.com/podcast.rss",
]
youtube_playlists = [
    "https://www.youtube.com/playlist?list=PLxyz",
    "https://www.youtube.com/@ChannelName",
]
yt_dlp_path = "yt-dlp"  # optional, defaults to "yt-dlp"
```

**Config structs:**

In `internal/mud/config.go`, two fields are added to `PodcastLibraryConfig`:

```go
YoutubePlayists []string `toml:"youtube_playlists"`
YtDlpPath       string   `toml:"yt_dlp_path"`
```

In `internal/modules/podcast_library/module.go`, two fields are added to `Config`:

```go
YoutubePlayists []string
YtDlpPath       string // default: "yt-dlp"
```

The existing `Feeds []string` field is untouched.

### YouTube Metadata Fetch

During periodic scan, for each URL in `YoutubePlaylists`, the module runs:

```
yt-dlp --flat-playlist --dump-json <url>
```

This outputs one JSON object per line with video metadata. The mapping to `cachedEpisode`:

| yt-dlp field | cachedEpisode field | Notes |
|---|---|---|
| `title` | `Title` | |
| `description` | `Description` | Truncated to 500 chars |
| `upload_date` | `Published` | Parsed from `YYYYMMDD` format |
| `duration` (seconds) | `DurationMS` | Multiplied by 1000 |
| `uploader` / `channel` | `Author` | |
| `thumbnail` | `ImageURL` | |
| `id` (video ID) | stored in `AudioURL` field | Prefixed with `ytid:` to distinguish from real URLs |

The channel/playlist title is extracted from the first entry's `playlist_title` or `channel` field. If neither is available, the feed title falls back to the URL.

The `AudioURL` field stores `ytid:<videoID>` during scan instead of a direct stream URL, since YouTube stream URLs expire.

The `cachedFeed` structure is reused without changes. The `FeedURL` stores the original YouTube URL.

### Feed Dispatch

The module iterates over both `config.Feeds` (RSS) and `config.YoutubePlaylists` (YouTube) when browsing, searching, and resolving. The `loadFeed` method is extended to accept a feed type parameter, dispatching to either the existing RSS parser or the new yt-dlp fetcher. Both produce the same `cachedFeed` structure, so all downstream browse/search/resolve logic is shared.

### YouTube Resolve (On-Demand Stream Extraction)

When `resolveItem` encounters an episode whose `AudioURL` starts with `ytid:`, it extracts the video ID and calls:

```
yt-dlp -f bestaudio -g --no-playlist "https://www.youtube.com/watch?v=<videoID>"
```

This returns a direct CDN URL for the best audio stream. The URL is returned as a `ResolvedSource` with `ByteRange: false`.

### Resolved URL Cache

To avoid redundant yt-dlp calls, resolved stream URLs are cached in memory with a 4-hour TTL (YouTube CDN URLs typically expire after 6 hours):

```go
type resolvedURLCache struct {
    mu      sync.Mutex
    entries map[string]resolvedURLEntry
}

type resolvedURLEntry struct {
    url       string
    expiresAt time.Time
}
```

On resolve, the cache is checked first. If a cached URL exists and hasn't expired, it's returned directly. Otherwise, yt-dlp is called and the result is cached.

Cache entries are evicted lazily (checked on access) — no background cleanup needed given the small number of entries.

### yt-dlp Execution

A helper function wraps yt-dlp calls:

```go
func (m *Module) runYtDlp(ctx context.Context, args ...string) ([]byte, error)
```

- Uses `exec.CommandContext` with the configured binary path
- Sets a timeout (default: 30s for metadata, 15s for URL extraction)
- Captures stdout and stderr separately
- Returns stdout on success, stderr in error message on failure

### Error Handling

- **yt-dlp not found:** At module startup, if `YoutubePlaylists` is non-empty, the module checks that the yt-dlp binary exists. If not, it logs an error and skips YouTube playlists (RSS feeds continue working).
- **Scan failure:** If yt-dlp fails during periodic metadata fetch (network, rate limit, etc.), the module falls back to the on-disk JSON cache — same pattern as existing RSS fetch failures.
- **Resolve failure:** If yt-dlp fails at resolve time, an error reply is sent to the renderer — same as the existing "episode has no audio url" path.
- **Timeout:** yt-dlp calls use context deadlines to prevent hanging.

### Backward Compatibility

Fully backward compatible. The existing `feeds` field is unchanged. The new `youtube_playlists` and `yt_dlp_path` fields are optional — existing configs work without modification.

### Files Modified

1. **`internal/mud/config.go`** — Add `YoutubePlaylists` and `YtDlpPath` fields to `PodcastLibraryConfig`
2. **`internal/modules/podcast_library/module.go`** — Add YouTube fetch logic, resolve dispatch for `ytid:` URLs, resolved URL cache, yt-dlp exec helper
3. **`cmd/mud/main.go`** — Pass new `YoutubePlaylists` and `YtDlpPath` config fields through to module
4. **`Dockerfile`** — Add `yt-dlp` and `python3` to the runtime image (yt-dlp requires Python)

### Verification

1. **Unit test:** Add test for YouTube metadata JSON parsing (mock yt-dlp output)
2. **Integration test:** Configure a module with a known public YouTube playlist, verify browse returns expected episodes
3. **End-to-end:** Configure in `mud.toml`, start mud, browse via MU applet or CLI, play a YouTube podcast episode through GStreamer renderer
4. **Error paths:** Test with yt-dlp missing from PATH, with an invalid YouTube URL, and with network disconnected (should fall back to cache)
