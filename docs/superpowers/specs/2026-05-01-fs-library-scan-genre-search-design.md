# fs_library: manual scan mode, LLM genre classification, search fixes

**Status:** Draft — 2026-05-01
**Module:** `internal/modules/fs_library`

## Problem

Three related complaints with the filesystem library module:

1. **Async scan operations bog down the host.** The module always runs an
   initial filesystem scan at startup and continues scanning every
   `scan_interval_ms` (default 15 min). On a low-power Atom NAS this competes
   with playback and metadata enrichment work, requiring a daemon restart to
   recover.
2. **Genre tagging is fine-grained and miscategorized.** Browse-by-genre
   surfaces raw MusicBrainz tags ("baroque", "early music", "chamber music")
   instead of broad families. Bach albums whose performer-derived genres
   contain none of "classical" land under unhelpful buckets.
3. **Search works poorly.** Probably downstream of (2) — the search index
   contains noisy/missing genre data, and composer-only queries miss albums
   whose album-artist is the performer rather than the composer.

## Goals

- Provide a config option to disable automatic scanning so the user can run
  the library in manual-only mode.
- Replace browse-by-genre with a normalized 15-genre taxonomy classified by a
  local LLM (Ollama), cached per album.
- Improve keyword search relevance for genre and composer queries.
- No breaking change for users who don't change config.

## Non-goals

- Per-track genre classification (album-level only).
- User-configurable taxonomy beyond the 15 baked-in genres.
- A UI for manually correcting individual albums' genres.
- Re-classification triggered by file-tag edits (next manual rescan handles
  it).

## Design

### 1. Manual scan mode

**New config field:** `scan_mode` (string, default `"auto"`). Values:

- `"auto"` — current behavior. Initial scan at startup, periodic ticker
  every `scan_interval_ms`.
- `"manual"` — skip initial scan in `Run()`, never start the periodic
  ticker. The persisted index loads from disk as usual; MQTT
  subscribers still come up; `library.rescan` continues to trigger scans
  on demand.

`scan_interval_ms` semantics are unchanged.

**Implementation site:** `module.go` `Run()` around line 712. Wrap the
initial `m.scanCtx(ctx)` call and the `time.NewTicker` block in
`if m.config.ScanMode != "manual"` (or equivalent normalized check).

Background enrichment, embedding, and summary goroutines keep their existing
hooks — they only run as a side effect of a scan, so manual mode means they
only run when the user invokes `library.rescan`.

### 2. LLM genre classification

#### Taxonomy

Fixed flat list of 15 top-level genres, baked into the module:

```
Classical, Jazz, Rock, Pop, Hip-Hop, Electronic, Folk, Country,
Metal, R&B/Soul, Blues, Reggae, World, Soundtrack, Other
```

#### Classifier component

New file `internal/modules/fs_library/genre_classifier.go`. Exposes:

```go
type GenreClassifier interface {
    Classify(ctx context.Context, in ClassifyInput) (string, error)
}

type ClassifyInput struct {
    Artist        string
    Album         string
    TrackTitles   []string // first ~10
    EmbeddedGenre string   // from FLAC/ID3/Vorbis tags, may be empty
    MBGenres      []string // existing MusicBrainz genres if cached, else nil
}
```

Constructor uses the existing Ollama endpoint resolution (defaults to
`SummaryEndpoint`, which itself defaults to `EmbeddingEndpoint`). New config
field `genre_model` (default falls back to `summary_model`, default
`gemma3:12b`).

#### Prompt

Single-shot prompt that:

- Lists the 15 valid genres explicitly.
- Provides artist/album/track samples and embedded-tag/MB hints.
- Instructs the model to return exactly one of the 15 strings, no extra
  prose.

Response is trimmed, case-folded, and matched against the allowlist. Anything
unparseable becomes `"Other"`.

#### Caching

Stored in the existing per-album sidecar (`AlbumMetadata`, written by
`enrichment.go`) under a new field `LLMGenre string`. Additive field — no
sidecar version bump needed; older sidecars without it are still valid.

The classifier runs in a background goroutine kicked off after a scan
completes, parallel to `backfillSummaries`. Iterates albums whose sidecar
lacks `LLMGenre`, calls `Classify`, writes the result back to the sidecar.
One LLM call per album, ever, unless the user runs a force-enrich rescan.

#### Fallback when LLM unavailable

If the Ollama endpoint is unreachable or the classifier construction failed,
a static rollup map is consulted at index-build time:

- ~80–100 entries mapping common embedded-tag genre strings to one of the
  15 (e.g., `baroque|romantic|chamber|symphony|opera|concerto|early music`
  → Classical; `bebop|swing|fusion|post-bop|big band` → Jazz;
  `shoegaze|grunge|punk|indie|alternative` → Rock; etc.).
- Map lives in a single file alongside the classifier; easy to extend.
- Result is **not** cached to the sidecar (so the LLM still gets a chance
  next scan). Used purely for live indexing when no cached value exists and
  no LLM is reachable.

#### Precedence

1. Cached `LLMGenre` in sidecar — use it.
2. Live LLM call (if reachable) — cache and use.
3. LLM unreachable + embedded tag matches the rollup map — use mapped
   value (uncached, retried next scan).
4. None of the above — `"Unknown"`.

#### Browse-by-genre

`buildGenreIndex` (`module.go` ~line 3210) is rewritten to group albums by
`LLMGenre` only. Raw MusicBrainz/Discogs genre and tag values remain in
the album sidecar and the search text but no longer create top-level browse
buckets. Albums without a resolved genre fall under `"Unknown"`.

### 3. Search improvements

Three changes in `module.go`.

#### `buildSearchText` (~line 2064)

Add to the searchable string:

- `LLMGenre` (so a `classical` query hits Bach albums even when MB tagged
  them only as `baroque`).
- Track-level `Artist` values where they differ from the album-level
  artists (so a `bach` query hits Glenn Gould's Bach albums where the
  composer lives in track tags).
- `Composer` field where present in the file tags.

The file-tag reader already extracts these via `dhowden/tag`; they just
need to be carried through `mediaItem` and joined into the precomputed
search text.

#### Keyword scorer (~line 1681)

Two new dimensions added to the existing scoring loop:

- `+30` if any query term appears in `LLMGenre` (exact substring, lowered).
- `+25` if any query term appears in any composer/track-artist field.

Existing scoring (name/artist/album boosts, length penalty) unchanged.

#### Semantic search

The embedding text is built from `buildSearchText`'s output indirectly. The
expanded search text means affected albums will produce different
embedding inputs and get re-embedded automatically on next scan (the
embedding cache keys on the text).

#### MQTT contract

No changes to the `library.search` request or response shape.

## Implementation plan summary

| File | Change |
|------|--------|
| `module.go` | Add `ScanMode`, `GenreModel` to `Config`; gate initial scan + ticker on `ScanMode != "manual"`; carry `LLMGenre`/`Composer`/track-artists through `mediaItem`; update `buildSearchText` and scorer; rewrite `buildGenreIndex` to use `LLMGenre`; update header doc. |
| `enrichment.go` | Add `LLMGenre string` to `AlbumMetadata`; surface tag-extracted composer/track-artist in the data carried back to the index builder. |
| `genre_classifier.go` (new) | `GenreClassifier` type, prompt, parser, allowlist, rollup map, ~250 lines incl. data. |
| `genre_classifier_test.go` (new) | Unit tests for prompt construction, response parsing, rollup-map fallback. LLM is interface-mocked. |
| `module.go` (backfill) | New `backfillGenres` goroutine sibling to `backfillSummaries`. |
| `module_test.go` | Cases for `scan_mode=manual` skipping initial scan + ticker; `buildGenreIndex` driven by `LLMGenre`; search hits via `LLMGenre` and composer. |

`NewModule` constructs the `GenreClassifier` using the existing summary
endpoint resolution. If construction fails, the field is left nil — the
backfill goroutine no-ops and the rollup-map fallback still runs at
index-build time.

## Testing

- **Unit:** classifier prompt/parse/rollup tests, no network.
- **Integration (in-package):** stub `GenreClassifier` interface; assert
  albums get tagged, indexed under their `LLMGenre`, and surfaced by
  search.
- **Manual:** point at the user's music folder with a known Bach album,
  run `library.rescan force_enrich=true`, confirm browse-by-genre shows it
  under "Classical" and `library.search bach` returns it.

## Rollout / migration

- Default `scan_mode = "auto"` preserves current behavior.
- Sidecars without `llm_genre` are valid; backfill fills them on next
  scan.
- No sidecar schema-version bump.
- No MQTT contract changes.

## Risks

- **Ollama latency/availability.** Classification runs in the background
  off the scan critical path; if the model is slow or absent the rollup
  fallback keeps browse-by-genre populated for the common cases. Worst
  case: albums sit under "Unknown" until the LLM returns.
- **Rollup-map drift.** A static map will miss new tag conventions. It is
  intentionally a fallback — the LLM is the primary classifier and the
  cache supersedes the map once it lands. The map needs ~yearly review at
  most.
- **Prompt drift between models.** The 15-genre allowlist + strict parse
  + "Other" sink protect against malformed responses; switching
  `genre_model` should not regress browse correctness.
