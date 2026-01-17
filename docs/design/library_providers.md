# Library Providers (Repair + Embeddings)

This document defines the pluggable provider interfaces for metadata repair
and semantic embeddings, with an Ollama embedding provider example.

## Provider Goals
- Keep the filesystem library core dependency-light.
- Allow optional provider integrations through clean interfaces.
- Support offline caching and retries.

## Metadata Repair Providers
Repair providers take extracted metadata and return canonical updates with
confidence scores and provenance.

Inputs:
- Raw tags (title, artist, album, duration, codec).
- Optional fingerprints (AcoustID, MBID).

Outputs:
- Canonical metadata and confidence per field.
- Provider provenance (source, URL, identifiers).

Policy:
- `strict`: accept only high confidence fields.
- `balanced`: accept medium confidence fields if no conflicts.
- `aggressive`: accept most fields, log conflicts.

Example (conceptual interface):
```go
type RepairProvider interface {
    Name() string
    Repair(ctx context.Context, item RepairItem) (RepairResult, error)
}
```

Example config:
```toml
[modules.fs_library.default.repair]
policy = "balanced"
providers = ["musicbrainz", "wikipedia"]

[modules.fs_library.default.repair.musicbrainz]
endpoint = "https://musicbrainz.org"
cache_path = "/var/lib/mud/library_fs/repair_mb"

[modules.fs_library.default.repair.wikipedia]
endpoint = "https://en.wikipedia.org"
cache_path = "/var/lib/mud/library_fs/repair_wiki"
```

## Embedding Providers
Embedding providers transform item text into vectors. The library module stores
and queries vectors for semantic search and similarity.

Inputs:
- Normalized text (title, artist, album, genre, description).
- Optional long-form descriptions (Wikipedia summary, lyrics).

Outputs:
- Vector[] with provider name + model version.

Example (conceptual interface):
```go
type EmbeddingProvider interface {
    Name() string
    Embed(ctx context.Context, inputs []EmbedInput) ([]EmbedVector, error)
}
```

## Ollama Embeddings Provider
Ollama exposes a local HTTP API for embeddings. This provider supports running
fully offline with minimal external dependencies.

Example config:
```toml
[modules.fs_library.default.embeddings]
provider = "ollama"
endpoint = "http://localhost:11434"
model = "nomic-embed-text"
timeout_ms = 10000
batch_size = 32
cache_path = "/var/lib/mud/library_fs/embeddings"
```

Ollama request (conceptual):
```
POST /api/embeddings
{"model":"nomic-embed-text","prompt":"<text>"}
```

## ANN Indexing
The module should keep the ANN index implementation optional. If no ANN index
is configured, semantic search can fall back to exact cosine similarity on a
bounded result set.

Example config:
```toml
[modules.fs_library.default.ann]
enabled = true
index_path = "/var/lib/mud/library_fs/ann"
ef_construction = 128
max_neighbors = 16
```
