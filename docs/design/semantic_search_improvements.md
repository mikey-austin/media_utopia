# Semantic Search Embeddings Spec (Album + Track) with Sidecar JSON

This spec defines how to generate **album-level** and **track-level** embeddings (and optional summaries) from **sidecar JSON files stored in each album directory**, optimized for semantic search.

---

## Goals

* Provide high-quality semantic search over:

  * **Tracks** (primary): “spacious modal jazz with trumpet, cool feel”
  * **Albums**: “late 50s Columbia jazz, restrained, airy arrangements”
* Keep embedding inputs:

  * **Consistent** across items (labels + stable ordering)
  * **Compact** (avoid drowning signal in repeated credits)
  * **Search-friendly** (supports both “vibe” and “facet-ish” queries)
* Generate short **natural-language summaries** using **Ollama**, then embed:

  * **Structured card** (labeled fields)
  * **Descriptive summary** (1–2 sentences)

---

## Directory Layout

Each album directory contains audio + a sidecar JSON:

```
Music/
  Artist - Album (Year)/
    album.json
    01 - Track Title.flac
    02 - Track Title.flac
    ...
```

You may also store derived artifacts:

```
Artist - Album (Year)/
  album.json
  .semantic/
    embeddings.json
    summaries.json
    manifest.json
```

---

## Sidecar JSON Schema

### `album.json` (recommended shape)

```json
{
  "album_id": "mbid-or-stable-hash",
  "album_title": "Kind of Blue",
  "album_artist": "Miles Davis",
  "year": 1959,
  "genres": ["modal jazz", "cool jazz"],
  "moods": ["spacious", "cool", "restrained"],
  "label": "Columbia",
  "recording_type": "studio",
  "recording_locations": ["New York"],
  "tags": ["modal", "minimal harmony"],

  "personnel": [
    {"name": "Miles Davis", "role": "trumpet"},
    {"name": "John Coltrane", "role": "tenor sax"},
    {"name": "Bill Evans", "role": "piano"}
  ],

  "tracks": [
    {
      "track_id": "mbid-or-stable-hash",
      "disc_number": 1,
      "track_number": 1,
      "track_title": "So What",
      "track_artists": ["Miles Davis"],
      "featured_artists": [],
      "duration_sec": 545,

      "genres": ["modal jazz"],
      "moods": ["spacious", "cool"],
      "instruments": ["trumpet", "tenor sax", "alto sax", "piano", "bass", "drums"],

      "tempo": "medium",
      "energy": "low-to-medium",
      "language": null,
      "tags": ["Dorian vamp", "head-solos-head", "AABA"],

      "lyrics_themes": [],
      "notes": ""
    }
  ]
}
```

### Normalization requirements

To improve embedding quality, normalize into controlled vocab where possible:

* `genres`: consistent taxonomy (e.g., `modal jazz` not `Modal Jazz`)
* `instruments`: consistent names (`tenor sax`, not sometimes `tenor saxophone`)
* `moods`: consistent set; prefer 3–8 moods max
* `tempo`, `energy`: from a small enum set

Recommended enums:

* `tempo`: `very slow | slow | medium | fast | very fast | variable`
* `energy`: `very low | low | low-to-medium | medium | medium-to-high | high | very high`

---

## IDs and Stability

Embeddings must be reproducible and stable across re-runs.

* Prefer MusicBrainz IDs if available.
* Otherwise generate stable IDs:

  * Album: hash of `album_artist + album_title + year`
  * Track: hash of `album_id + disc + track_number + track_title + duration_sec`

Store these IDs in `album.json` so they don’t change.

---

## Outputs

### `.semantic/manifest.json`

Tracks what was generated with which settings.

```json
{
  "spec_version": "1.0",
  "generated_at": "2026-02-08T07:00:00+01:00",
  "ollama_model": "llama3.1:8b",
  "ollama_params": {"temperature": 0.2, "top_p": 0.9, "repeat_penalty": 1.1},
  "embed_model": "your-embedding-model-name",
  "embed_card_version": "card-v1",
  "embed_summary_version": "summary-v1"
}
```

### `.semantic/summaries.json`

```json
{
  "album": {
    "album_id": "...",
    "summary": "..."
  },
  "tracks": {
    "track_id_1": {"summary": "...", "keywords": ["..."]},
    "track_id_2": {"summary": "...", "keywords": ["..."]}
  }
}
```

### `.semantic/embeddings.json`

Store one or more vectors per entity (recommended: 2 each).

```json
{
  "album": {
    "album_id": "...",
    "vectors": {
      "card": [/* floats */],
      "summary": [/* floats */]
    }
  },
  "tracks": {
    "track_id_1": {
      "vectors": {
        "card": [/* floats */],
        "summary": [/* floats */]
      }
    }
  }
}
```

---

## Embedding Strategy

Generate **two embeddings per entity**:

1. **Card embedding** (structured, labeled, normalized)
2. **Summary embedding** (short natural language)

This gives strong results for:

* facet-ish search (“trumpet modal jazz medium tempo”)
* vibe search (“airy, spacious, cool, minimal harmony”)

If you must use only one embedding, prefer:

* Track: `card + summary` combined into one string (see below)
* Album: same

---

## Text Construction

### Card format rules

* Always use **labels**
* Fixed ordering
* Use `; ` to separate list values
* Keep it compact
* Prefer track-specific fields for track cards; keep album context short

### Track Card (card-v1)

```
type: track
track_title: {track_title}
track_number: {disc_number}-{track_number}
artist: {album_artist}
primary_artists: {track_artists}
featured_artists: {featured_artists}
album: {album_title}
year: {year}
genres: {track.genres OR album.genres}
moods: {track.moods OR album.moods}
instruments: {track.instruments}
tempo: {track.tempo}
energy: {track.energy}
recording_type: {album.recording_type}
label: {album.label}
tags: {track.tags + album.tags (dedup, short)}
album_context: {album_title} ({year}, {label}) — {top 1-2 genres}
```

**Notes**

* `genres/moods` fall back to album values when missing.
* `album_context` should be **one line** only.
* Avoid full personnel lists here unless you *don’t have instruments* and need something.

### Album Card (card-v1)

```
type: album
album_title: {album_title}
artist: {album_artist}
year: {year}
genres: {album.genres}
moods: {album.moods}
recording_type: {recording_type}
label: {label}
tags: {album.tags}
personnel: {top-billed names + roles (optional, short)}
```

Keep `personnel` short (e.g., 3–8 entries max).

---

## Summary Generation (Ollama)

Generate summaries **before** embeddings, store in `.semantic/summaries.json`, then embed the summary separately.

### Track summary prompt (summary-v1)

**Constraints**

* 1–2 sentences
* 30–45 words total
* Describe sound (mood/groove/texture) + 1–2 anchors (instrumentation/structure/genre/era)
* No hype words, no quotes, no bullet points
* Do not mention “metadata” or labels

**Prompt template**

```text
You write concise music track summaries for semantic search.

Rules:
- Output exactly 1–2 sentences.
- 30–45 words total.
- Describe how it sounds (mood, groove, texture), plus 1–2 concrete anchors (instrumentation, structure, era/genre).
- Do NOT hype (no “iconic”, “masterpiece”), do NOT mention “this track”, do NOT mention metadata labels.
- No quotes, no bullet points.

Track:
Title: {track_title}
Artist: {album_artist}
Album: {album_title}
Year: {year}
Genres: {genres}
Moods: {moods}
Instruments: {instruments}
Tempo: {tempo}
Energy: {energy}
Tags/notes: {tags}
Live/Studio: {recording_type}

Summary:
```

### Track summary JSON output (recommended)

Use this when possible to avoid cleanup:

```text
Return ONLY valid JSON:
{
  "summary": "string (1–2 sentences, 30–45 words)",
  "keywords": ["5–10 short strings"]
}
Same rules as before (no hype, no bullets, no quotes).
```

### Album summary prompt (summary-v1)

**Constraints**

* 1–3 sentences
* 45–70 words total
* Describe overall sound + recurring traits; optionally mention era/scene/label/recording type

```text
You write concise album summaries for semantic search.

Rules:
- Output 1–3 sentences.
- 45–70 words total.
- Describe overall sound and mood, instrumentation tendencies, and genre/era anchors.
- No hype words, no quotes, no bullet points.

Album:
Title: {album_title}
Artist: {album_artist}
Year: {year}
Genres: {genres}
Moods: {moods}
Label: {label}
Recording type: {recording_type}
Tags/notes: {tags}
Personnel (optional): {short_personnel}

Summary:
```

### Ollama runtime recommendations

* `temperature`: ~0.2
* `top_p`: ~0.9
* `repeat_penalty`: ~1.1
* Keep prompts short; don’t feed massive credits.

---

## Embedding Input Strings

### Summary embedding input

For tracks:

```
type: track summary
track_title: {track_title}
artist: {album_artist}
album: {album_title}
year: {year}
summary: {generated_summary}
keywords: {keywords joined by ; }
```

For albums:

```
type: album summary
album_title: {album_title}
artist: {album_artist}
year: {year}
summary: {generated_summary}
keywords: {keywords}
```

### Single-vector fallback (if you only store one vector)

Concatenate card + summary with a hard separator:

```
{TRACK_CARD}

--- summary ---
{TRACK_SUMMARY}
keywords: ...
```

---

## Indexing and Retrieval

### Index entities

* Track documents:

  * `id`: `track_id`
  * metadata: album/artist/year/track_number/duration/etc.
  * vectors: `card`, `summary` (or single)
* Album documents:

  * `id`: `album_id`
  * metadata
  * vectors: `card`, `summary`

### Query strategy (recommended)

Compute query embedding(s) and combine scores:

* `score = 0.6 * cos_sim(track.card, q) + 0.4 * cos_sim(track.summary, q)`

  * If user query is “vibe-y”, increase summary weight.
  * If user query is “faceted”, increase card weight.

If you support explicit labeled queries (optional), you can:

* parse out fields (e.g., `moods:` `genres:` `instruments:`)
* build a query-card string in the same schema, and embed that too

---

## Update Logic

Track generation should be incremental:

1. Load `album.json`
2. Compute a **content hash** for:

   * album card inputs (album)
   * each track card inputs
3. If hash changed or missing artifacts:

   * regenerate summary (if summary inputs changed)
   * regenerate embeddings (if card/summary changed)
4. Update `.semantic/manifest.json` with models + versions

Store per-entity hashes in `.semantic/manifest.json` or a separate `.semantic/state.json`.

---

## Quality Checklist

* ✅ Labels on every field
* ✅ Stable ordering
* ✅ Normalized tags
* ✅ Track cards emphasize track-specific info
* ✅ Album context included but short
* ✅ Summaries short, non-hype, “how it sounds”
* ✅ Two vectors per entity if possible
* ✅ Versioned prompts / card formats to allow reindexing

---

## Implementation Status

### Phase 1: Structured Card Text + Normalization — DONE

Replaced the flat `" - "` joined `buildEmbedText` output with a labeled card format. All fields use `label: value` lines with `"; "` list separators. Added normalization helpers (`normalizeStringList`, `normalizeGenres`, `normalizeTags`, `normalizeStyles`) that lowercase, trim, dedup, sort, and apply synonym mappings. Helper functions `collectGenres`, `collectTags`, `uniqueNames`, and `buildAlbumContext` consolidate enrichment data from multiple sources.

### Phase 2: Dual Vectors + Weighted Scoring — DONE

- `buildEmbeddings` now generates two vectors per item: `{id}:card` (always) and `{id}:summary` (when Wikipedia/MBAnnotation data exists)
- `SearchDual` performs weighted scoring: `0.6 * card_score + 0.4 * summary_score`
- Items without summary vectors use card score at full weight (no penalty)
- `semanticSearch` uses `SearchDual` with automatic fallback to `Search` for legacy indexes
- Replaced O(n²) bubble sort with `sort.Slice` in `VectorIndex.Search`

### Not Yet Implemented

- **LLM-generated summaries** (Phase 3): Ollama generate for track/album summaries — currently using Wikipedia/MBAnnotation text directly
- **Track-specific fields**: `moods`, `instruments`, `tempo`, `energy`, `track_number`, `disc_number` — not available in current sidecar schema
- **Separate `.semantic/` output directory**: Embeddings remain in the existing `EmbeddingCache` + `VectorIndex` system

---

## Actual Card Format

The implemented card format uses fields available from `.mu_album_metadata.json` sidecar data (MusicBrainz + Discogs + Wikipedia):

```
type: audio
title: So What
artist: Miles Davis
album: Kind of Blue
year: 1959
genres: cool jazz; modal jazz
styles: cool jazz; post-bop
tags: modal; minimal harmony
label: Columbia
recording_type: Album
personnel: John Coltrane; Bill Evans; Cannonball Adderley
producers: Teo Macero
artist_type: Group
artist_origin: US
artist_active: 1944
members: Miles Davis; John Coltrane
biography: <200 char cap>
description: <200 char cap>
album_context: Kind of Blue (1959, Columbia) -- cool jazz, modal jazz
```

**Fields not yet available** (require sidecar schema additions or LLM analysis):
- `moods`, `instruments`, `tempo`, `energy` — track-level audio features
- `track_number`, `disc_number` — available in filesystem but not passed through to embedding
- `featured_artists` — not extracted separately

---

## Normalization Approach

- All genre, style, and tag lists are lowercased, trimmed, deduplicated, and sorted
- A small synonym map handles common variants:
  - `"hip hop"` → `"hip-hop"`, `"r&b"` → `"rhythm and blues"`, `"synth pop"` → `"synthpop"`, etc.
- Genres are merged from MusicBrainz album genres + artist genres before normalization
- Tags are merged from MusicBrainz album tags (top 5) + artist tags (top 5) before normalization

---

## Data Source

Enrichment data comes from `.mu_album_metadata.json` sidecar files (not a separate `album.json`). These contain:

- **MusicBrainz**: genres, tags, year, label, release type, annotation, artist IDs
- **Discogs**: styles, credits (personnel), release credits (producers), notes
- **Artist info**: type, origin, active years, members, genres, tags, biography
- **Description**: Wikipedia summary, MusicBrainz annotation

The sidecar is populated by the existing enrichment pipeline (`enrichment.go`).

---

## Updated Roadmap

- **Phase 3: LLM Summaries via Ollama Generate** — Use Ollama to generate concise 1–2 sentence summaries per track/album, replacing raw Wikipedia text. Store in sidecar or `.semantic/summaries.json`.
- **Phase 4: Track-Specific Fields** — Extend sidecar schema with moods, instruments, tempo, energy. Potentially use audio analysis or LLM inference from track titles and album context.
- **Phase 5: Query Classification** — Detect "vibe" vs "facet" queries and adjust card/summary weights dynamically.
