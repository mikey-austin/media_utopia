# AcoustID Audio Fingerprint Integration

Audio fingerprinting via [AcoustID](https://acoustid.org/) provides a fallback identification mechanism for albums with poor or missing tags. When MusicBrainz text search returns no results, the enrichment pipeline can fingerprint a track and query AcoustID to recover a MusicBrainz release-group ID, which then feeds the existing metadata pipeline.

---

## Problem

Albums ripped without tags, or with corrupted/non-standard metadata, return zero results from MusicBrainz and Discogs text searches. The enrichment pipeline writes a negative-cache sidecar and moves on. These albums remain permanently un-enriched even though their audio content is identifiable.

---

## Solution

Insert an AcoustID fingerprint lookup as step 2b in the enrichment pipeline, between the MusicBrainz text search (step 1) and the Discogs release fetch (step 3). When the MB text search returns nil **and** AcoustID is configured, the pipeline:

1. Finds the first audio file in the album directory (alphabetically, for determinism)
2. Decodes the first 120 seconds to raw PCM via `ffmpeg`
3. Computes a Chromaprint fingerprint via CGo bindings to `libchromaprint`
4. Queries the AcoustID lookup API with the fingerprint and duration
5. If a result scores above 0.5, extracts the MusicBrainz release-group ID
6. Fetches the full release-group metadata via the existing `mbClient.fetchReleaseGroup()`

The resolved metadata populates the existing `MusicBrainz` field in the sidecar -- downstream steps (Discogs enrichment, artist info, Wikipedia, LLM summary, embeddings) proceed as normal.

---

## Architecture

### CGo Chromaprint Adapter

`internal/adapters/chromaprint/` follows the same build-tag-gated CGo pattern as `internal/adapters/pupnp/`:

| File                  | Build tag      | Purpose                                               |
|-----------------------|----------------|-------------------------------------------------------|
| `fingerprint.go`      | `chromaprint`  | CGo bindings to `libchromaprint`, `FingerprintFile()` |
| `fingerprint_stub.go` | `!chromaprint` | No-op stub returning `ErrDisabled`                    |

The adapter exports:

```go
const Enabled bool  // true when compiled with chromaprint tag

func FingerprintFile(path string) (fingerprint string, durationSec int, err error)
```

`FingerprintFile` shells out to `ffmpeg` for audio decoding (mono, 16-bit signed LE, 44100 Hz, max 120s), then feeds the PCM buffer through the chromaprint C API in-process. The `ffmpeg` binary is resolved on PATH once via `sync.Once`; if absent, `ErrFFmpegNotFound` is returned.

### AcoustID Client

The `acoustidClient` in `internal/modules/fs_library/enrichment.go` follows the same patterns as `mbClient` and `discogsClient`:

- Rate limiter: 334ms interval (3 req/sec per AcoustID docs)
- HTTP timeout: 15s, `MaxConnsPerHost: 2`
- `doWithRetry` for HTTP 429 responses

The `lookup()` method queries:

```
GET https://api.acoustid.org/v2/lookup?client=KEY&duration=DUR&fingerprint=FP&meta=releasegroups
```

It picks the result with the highest score (threshold >0.5) and returns the first release-group ID from its recordings, or `""` if no match.

---

## Configuration

A single config field controls the feature:

```toml
[modules.fs_library]
acoustid_api_key = "your-key-here"
```

The feature activates when **both** conditions are met:

1. `acoustid_api_key` is non-empty
2. The binary was compiled with the `chromaprint` build tag

If the key is set but the build tag is missing, a warning is logged at startup.

API keys are free to register at <https://acoustid.org/new-application>.

---

## Build

```bash
# Default build (no fingerprinting, stub compiled in):
go build ./cmd/mud

# With fingerprinting (requires libchromaprint-dev and pkg-config):
go build -tags=chromaprint ./cmd/mud
```

System dependencies for the `chromaprint` build tag:

- `libchromaprint-dev` (or equivalent for your distro)
- `pkg-config`
- `ffmpeg` on PATH at runtime

---

## Enrichment Pipeline Flow

```
for each album:
    1.  MusicBrainz text search (artist + album)
    2.  Discogs text search (artist + album)
   *2b. AcoustID fingerprint fallback (if MB returned nil and acoustid configured)*
    3.  Discogs main release fetch (fuller notes + credits)
   3b.  Extract instruments from Discogs credits
    4.  Artist info (MB + Discogs, cached)
    5.  Wikipedia summaries (album + artist)
    6.  LLM summary generation
    7.  Write sidecar
```

Step 2b only runs when `meta.MusicBrainz == nil` after step 1. If AcoustID finds a match, the resolved MB metadata flows into all subsequent steps identically to a successful text search.

---

## Sidecar Impact

None. AcoustID is purely a discovery mechanism that resolves to a MusicBrainz release-group ID. The resulting metadata populates the existing `MusicBrainz` field in the v3 sidecar schema. Downstream consumers (embeddings, search, browse) do not need to know how the data was discovered.

---

## Helper: `findFirstAudioFile`

Walks the album directory non-recursively and returns the first file matching `.mp3`, `.flac`, `.ogg`, or `.m4a` in alphabetical order. Returns `""` if no audio files are found. Alphabetical ordering ensures deterministic fingerprint results across runs.

---

## Testing

`TestAcoustIDLookup` and `TestAcoustIDLookupNoMatch` use `httptest` to mock the AcoustID API, verifying:

- Correct query parameters (`client`, `meta`, `duration`, `fingerprint`)
- Best-score selection (picks highest score above 0.5 threshold)
- Empty result handling (returns `""` with no error)

`TestFindFirstAudioFile` verifies alphabetical file selection and empty-directory handling.

---

## Files

| File                                                | Action                                                     |
|-----------------------------------------------------|------------------------------------------------------------|
| `internal/adapters/chromaprint/fingerprint.go`      | Created -- CGo bindings                                    |
| `internal/adapters/chromaprint/fingerprint_stub.go` | Created -- no-op stub                                      |
| `internal/modules/fs_library/enrichment.go`         | Modified -- acoustid client, step 2b, `findFirstAudioFile` |
| `internal/modules/fs_library/module.go`             | Modified -- `AcoustIDAPIKey` config field, startup warning |
| `internal/modules/fs_library/module_test.go`        | Modified -- acoustid + findFirstAudioFile tests            |
| `internal/mud/config.go`                            | Modified -- `acoustid_api_key` TOML field                  |
| `cmd/mud/main.go`                                   | Modified -- config wiring                                  |
