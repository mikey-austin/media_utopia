# Filesystem Library Module (Design)

This document proposes a local filesystem library module for mud that exposes
audio and video libraries to mu renderers, including advanced metadata repair,
semantic search, and duplicate management. The module follows the standard
library command contract: `library.browse`, `library.search`, `library.resolve`,
and `library.resolveBatch`.

## Goals
- Expose local audio+video collections to any mu renderer using HTTP URLs.
- Provide full metadata from embedded tags, plus repair/enrichment pipelines.
- Support fuzzy search, semantic search, and similarity recommendations.
- Detect and manage duplicates with clear, observable policies.
- Minimize dependencies in the core Go module.

## Non-goals
- DRM-protected media handling.
- Full media transcoding (out of scope; can be added via a resolver).

## Capabilities
- Browse: hierarchical containers (Artist/Album/Track, Series/Season/Episode).
- Search: fuzzy, keyword, and semantic similarity queries.
- Resolve: return a playable HTTP URL and canonical metadata.
- Duplicate detection: grouping, scoring, and optional auto-actions.
- Observability: structured logging, timing, counters, and per-item tracing.

## Pipeline Overview
1) Ingest / Scan
   - Walk configured roots and file extensions.
   - Skip unchanged files by size+mtime cache.
2) Tag Extraction
   - Read embedded tags (audio/video containers).
   - Extract title, artist, album, track, duration, codecs, etc.
3) Fingerprint / Signature
   - Compute fast file hash and optional full hash.
   - Optional audio/video fingerprint providers.
4) Metadata Repair / Enrichment
   - Pluggable providers (MusicBrainz, Wikipedia, others).
   - Confidence scoring and policy-driven acceptance.
5) Normalization
   - Canonicalize fields, normalize names, build search text.
6) Library Model Build
   - Items + containers + browse tree.
7) Search Index
   - Trigram and token index for fuzzy search.
8) Embeddings
   - Semantic vectors for items (provider-based).
9) ANN Index
   - Build approximate nearest neighbor index (optional).
10) Dedupe
   - Group duplicates, choose preferred item, apply policy.

## Data Model
- Item: track/episode/movie with stable ID, metadata, sources.
- Container: album/series/artist nodes for browse.
- Source: one or more resolvable URLs (HTTP by default).
- Provenance: metadata sources + confidence.

## Command Semantics
- `library.browse`: returns containers + items under a container ID.
- `library.search`: supports query strings and optional type filters.
- `library.resolve`: returns metadata + source URLs for one item.
- `library.resolveBatch`: resolves multiple IDs.

Query patterns for `library.search`:
- `genre:jazz year:1959` (structured filters)
- `similar:<itemId>` (semantic similarity to item)
- `mood:ambient` (semantic term, if embeddings enabled)

## Duplicate Detection
Stages:
- Hard matches: full hash, MBID, AcoustID.
- Soft matches: duration + tag similarity + fingerprint similarity.

Actions:
- `hide`: mark duplicates hidden in browse/search.
- `merge`: keep best item, merge metadata sources.
- `prefer`: select best-quality item and return only that in search.

Quality scoring:
- Prefer lossless, higher bitrate, higher resolution, complete tags.

## Observability
Log structure:
- `module=fs_library`
- `stage=scan|tags|repair|embed|index|dedupe`
- `item_id`, `path`, `provider`, `duration_ms`, `status`

Metrics to emit (logging or counters):
- `scan.items_total`
- `scan.items_skipped`
- `tags.duration_ms`
- `repair.duration_ms`
- `embed.duration_ms`
- `index.build_ms`
- `dedupe.groups_total`

## Performance Notes
- Incremental scans use mtime+size cache to avoid re-reading files.
- Heavy stages (repair, embeddings) run async with bounded workers.
- ANN build can be scheduled or incremental.

## Configuration (draft)
```toml
[modules.fs_library.default]
enabled = true
name = "Local Library"
provider = "filesystem"
resource = "default"
roots = ["/media/music", "/media/video"]
include_exts = [".flac", ".mp3", ".m4a", ".ogg", ".wav", ".mp4", ".mkv"]
http_listen = "0.0.0.0:0"
index_path = "/var/lib/mud/library_fs"
index_mode = "separate" # or "near" to store alongside media roots
scan_interval_ms = 900000
metadata_mode = "tags"
repair_policy = "balanced"
dedupe_policy = "prefer"

[modules.fs_library.default.embeddings]
provider = "ollama"
model = "nomic-embed-text"
endpoint = "http://localhost:11434"
cache_path = "/var/lib/mud/library_fs/embeddings"
```

## Examples
Browse:
```
mu browse lib:mu:library:filesystem:mud@home:default
mu browse lib:mu:library:filesystem:mud@home:default --container "artist:radiohead"
```

Search:
```
mu search lib:mu:library:filesystem:mud@home:default "late night jazz trumpet"
mu search lib:mu:library:filesystem:mud@home:default "similar:track:abc123"
```

Resolve:
```
mu resolve lib:mu:library:filesystem:mud@home:default:track:abc123
```

Duplicates:
```
mu library dupes lib:mu:library:filesystem:mud@home:default
mu library dupes lib:mu:library:filesystem:mud@home:default --action=hide
```

## Glossary
- **Container**: A browse node like artist, album, series, or season.
- **Item**: A playable media entity (track, episode, movie).
- **Repair policy**: Rules for accepting metadata from providers.
- **Dedupe policy**: How duplicate groups are surfaced or collapsed.
- **Semantic search**: Vector-based search over item text meaning.
