# fs_library: Manual Scan Mode, LLM Genre Classification, Search Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `scan_mode = "manual"` config option to disable automatic scans, replace the noisy MusicBrainz-derived genre browse with a 15-genre LLM classifier (Ollama-backed, sidecar-cached, with a static rollup-map fallback), and improve keyword search by indexing the new normalized genre and the composer/track-artist fields the file tags already provide.

**Architecture:** Each change is layered onto existing structures. `Config.ScanMode` gates the initial scan and ticker in `Run()`. A new `genre_classifier.go` file holds the classifier, prompt, parser, and rollup map; it slots in alongside the existing `OllamaGenerator` for summaries. `AlbumMetadata.LLMGenre` is an additive sidecar field. `buildGenreIndex`, `buildSearchText`, and the search scorer in `module.go` are modified to use the new fields.

**Tech Stack:** Go, `github.com/dhowden/tag` for embedded tag reading, existing `OllamaGenerator` pattern for the local LLM call, `go.uber.org/zap` for logging, standard `testing` package.

**Spec:** `docs/superpowers/specs/2026-05-01-fs-library-scan-genre-search-design.md`

**Conventions used in this plan:**
- All paths are absolute from repo root: `/home/mikey/Workspace/media_utopia/...`
- Test commands run with `go test ./internal/modules/fs_library/...`
- Each task ends with one commit. Commit messages use the project's `feat:`/`refactor:`/`fix:` prefix style observed in recent log.

---

## File Structure

| File | Status | Responsibility |
|------|--------|----------------|
| `internal/modules/fs_library/module.go` | Modify | Config field; Run() gate; mediaItem fields; buildSearchText; scorer; buildGenreIndex; tagMetadata struct; readTags()/buildItemFromInfo(); NewModule wiring; doc-comment header |
| `internal/modules/fs_library/enrichment.go` | Modify | AlbumMetadata.LLMGenre field |
| `internal/modules/fs_library/genre_classifier.go` | Create | GenreClassifier interface + Ollama implementation, prompt, response parser, allowlist, rollup map, backfill loop |
| `internal/modules/fs_library/genre_classifier_test.go` | Create | Unit tests for parser, rollup map, prompt builder |
| `internal/modules/fs_library/module_test.go` | Modify | Manual-mode scan test; LLMGenre indexing test; search-via-genre/composer test |
| `mud.toml/<host>.toml` (e.g. `integrations/home_assistant/mud_config/mud.toml`) | Modify (doc/sample) | Add commented `scan_mode` example |

---

## Task 1: Add `ScanMode` config field and gate `Run()`

**Files:**
- Modify: `internal/modules/fs_library/module.go` (Config struct ~line 263, Run() ~line 702)
- Test: `internal/modules/fs_library/module_test.go`

- [ ] **Step 1: Add the failing test**

Add to `internal/modules/fs_library/module_test.go` (append near other Module tests):

```go
func TestRunManualScanModeSkipsInitialScan(t *testing.T) {
    log := zap.NewNop()
    cfg := Config{
        NodeID:    "mu:library:filesystem:test:default",
        TopicBase: "mu",
        Name:      "test",
        Roots:     []string{t.TempDir()},
        ScanMode:  "manual",
        ScanIntervalMS: 100, // would fire fast in auto mode
    }
    m, err := NewModule(log, nil, cfg)
    if err != nil {
        t.Fatalf("NewModule: %v", err)
    }
    // Sentinel: we'll detect a scan by observing m.index.Items mutation timestamps.
    // In manual mode, scanCtx must NOT be invoked at startup.
    ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
    defer cancel()

    runErr := make(chan error, 1)
    go func() { runErr <- m.Run(ctx) }()

    <-ctx.Done()
    select {
    case <-runErr:
    case <-time.After(time.Second):
        t.Fatal("Run did not return after context cancel")
    }

    if got := m.scanCount.Load(); got != 0 {
        t.Fatalf("scanCount = %d, want 0 in manual mode", got)
    }
}
```

This test references a `scanCount atomic.Int64` field that does not yet exist on Module — adding it is part of the implementation. We use it only as a test sentinel; production callers won't touch it.

- [ ] **Step 2: Run test — confirm it fails to compile**

```
go test ./internal/modules/fs_library/ -run TestRunManualScanModeSkipsInitialScan
```

Expected: build error (`Config has no field ScanMode`, `Module has no field scanCount`).

- [ ] **Step 3: Add `ScanMode` to Config**

In `internal/modules/fs_library/module.go`, inside the `Config` struct (just before `MetadataMode` ~line 305), add:

```go
    // ScanMode controls automatic scanning:
    //   "auto"   - Initial scan at startup, then periodic rescans every ScanIntervalMS (default).
    //   "manual" - No initial scan and no periodic ticker. The persisted index loads as
    //              normal, and the user must invoke library.rescan to scan.
    ScanMode string
```

- [ ] **Step 4: Add `scanCount` sentinel to Module**

In `internal/modules/fs_library/module.go`, inside the `Module` struct (find it around line 380, near `scanMu`), add:

```go
    // scanCount is incremented whenever scanInner runs; used by tests to verify
    // that ScanMode="manual" actually suppresses scans.
    scanCount atomic.Int64
```

In `scanInner` (around line 2118), at the very top of the function (before `m.scanMu.Lock()`), add:

```go
    m.scanCount.Add(1)
```

- [ ] **Step 5: Gate `Run()` on ScanMode**

In `internal/modules/fs_library/module.go` `Run()` (around line 702), replace this block:

```go
    if err := m.scanCtx(ctx); err != nil && ctx.Err() == nil {
        m.log.Warn("initial scan failed", zap.Error(err))
    }
```

with:

```go
    if m.config.ScanMode != "manual" {
        if err := m.scanCtx(ctx); err != nil && ctx.Err() == nil {
            m.log.Warn("initial scan failed", zap.Error(err))
        }
    } else {
        m.log.Info("scan_mode=manual: skipping initial scan")
    }
```

And replace the ticker block lower down:

```go
    scanInterval := time.Duration(m.config.ScanIntervalMS) * time.Millisecond
    ticker := time.NewTicker(scanInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            m.shutdownHTTPServer()
            wg.Wait()
            return nil
        case <-ticker.C:
            if err := m.scanCtx(ctx); err != nil && ctx.Err() == nil {
                m.log.Warn("scan failed", zap.Error(err))
            }
        }
    }
```

with:

```go
    var tickerC <-chan time.Time
    if m.config.ScanMode != "manual" {
        scanInterval := time.Duration(m.config.ScanIntervalMS) * time.Millisecond
        ticker := time.NewTicker(scanInterval)
        defer ticker.Stop()
        tickerC = ticker.C
    }

    for {
        select {
        case <-ctx.Done():
            m.shutdownHTTPServer()
            wg.Wait()
            return nil
        case <-tickerC:
            if err := m.scanCtx(ctx); err != nil && ctx.Err() == nil {
                m.log.Warn("scan failed", zap.Error(err))
            }
        }
    }
```

(A nil receive channel in `select` blocks forever, which is the desired no-op behavior.)

- [ ] **Step 6: Run the test — confirm it passes**

```
go test ./internal/modules/fs_library/ -run TestRunManualScanModeSkipsInitialScan -v
```

Expected: PASS.

- [ ] **Step 7: Run the full module test suite to confirm no regressions**

```
go test ./internal/modules/fs_library/...
```

Expected: all existing tests still pass.

- [ ] **Step 8: Commit**

```bash
git add internal/modules/fs_library/module.go internal/modules/fs_library/module_test.go
git commit -m "feat(fs_library): add scan_mode=\"manual\" to disable auto-scanning"
```

---

## Task 2: Extract `Composer` and embedded `Genre` from file tags

**Files:**
- Modify: `internal/modules/fs_library/module.go` (`tagMetadata` ~line 2640, `readTags` ~line 2648, `buildItemFromInfo` ~line 2612, `mediaItem` ~line 509)
- Test: `internal/modules/fs_library/module_test.go`

The `dhowden/tag` library exposes `metadata.Composer()` and `metadata.Genre()` on its Metadata interface. We pull both through.

- [ ] **Step 1: Write the failing test**

Append to `internal/modules/fs_library/module_test.go`. (Skip via `t.Skip` if `testdata/` doesn't yet have a tagged sample — we'll exercise this path indirectly via the full-module test in Task 12. For now, test the pure plumbing on `tagMetadata`.)

```go
func TestTagMetadataCarriesComposerAndGenre(t *testing.T) {
    // Pure struct test - confirm the fields exist and round-trip through buildItemFromInfo
    info := &fakeFileInfo{name: "x.flac", size: 1, mtime: time.Now()}
    _ = info
    meta := tagMetadata{
        Title:         "Goldberg Variations",
        Artists:       []string{"Glenn Gould"},
        Album:         "Bach: Goldberg Variations",
        Composer:      "Johann Sebastian Bach",
        EmbeddedGenre: "Classical",
    }
    if meta.Composer != "Johann Sebastian Bach" {
        t.Fatalf("Composer = %q", meta.Composer)
    }
    if meta.EmbeddedGenre != "Classical" {
        t.Fatalf("EmbeddedGenre = %q", meta.EmbeddedGenre)
    }
}

func TestMediaItemCarriesComposerAndEmbeddedGenre(t *testing.T) {
    item := mediaItem{Composer: "Bach", EmbeddedGenre: "Baroque"}
    if item.Composer != "Bach" || item.EmbeddedGenre != "Baroque" {
        t.Fatalf("fields not carried: %+v", item)
    }
}
```

- [ ] **Step 2: Run test — confirm it fails to compile**

```
go test ./internal/modules/fs_library/ -run "TestTagMetadataCarriesComposerAndGenre|TestMediaItemCarriesComposerAndEmbeddedGenre"
```

Expected: build error (`tagMetadata has no field Composer`, `mediaItem has no field Composer`).

- [ ] **Step 3: Add fields to `tagMetadata`**

In `internal/modules/fs_library/module.go` ~line 2640, change `tagMetadata` to:

```go
type tagMetadata struct {
    Title         string
    Artists       []string
    Album         string
    Composer      string
    EmbeddedGenre string
    DurationMS    int64
    ArtExt        string
}
```

- [ ] **Step 4: Add fields to `mediaItem`**

In `internal/modules/fs_library/module.go` ~line 509, append two new fields after `Album` (keep them omitempty so existing index files still load cleanly):

```go
    // Composer is from the file's Composer tag (TCOM/COMPOSER), if present.
    Composer string `json:"composer,omitempty"`

    // EmbeddedGenre is the raw genre value from the file's tags. Used as input
    // to the LLM genre classifier and as the fallback signal when no LLM
    // classification has been cached.
    EmbeddedGenre string `json:"embeddedGenre,omitempty"`
```

- [ ] **Step 5: Populate in `readTags`**

In `internal/modules/fs_library/module.go` ~line 2648, replace the `return tagMetadata{...}` at the end of `readTags` with:

```go
    return tagMetadata{
        Title:         strings.TrimSpace(metadata.Title()),
        Artists:       artists,
        Album:         strings.TrimSpace(metadata.Album()),
        Composer:      strings.TrimSpace(metadata.Composer()),
        EmbeddedGenre: strings.TrimSpace(metadata.Genre()),
        DurationMS:    0,
        ArtExt:        artExt,
    }, nil
```

- [ ] **Step 6: Plumb into `buildItemFromInfo`**

In `internal/modules/fs_library/module.go` ~line 2626, expand the returned `mediaItem` literal to include the new fields:

```go
    return mediaItem{
        ID:             itemID,
        Path:           path,
        Name:           name,
        Title:          meta.Title,
        Artists:        meta.Artists,
        Album:          meta.Album,
        Composer:       meta.Composer,
        EmbeddedGenre:  meta.EmbeddedGenre,
        MediaType:      mediaType,
        DurationMS:     meta.DurationMS,
        Mtime:          info.ModTime(),
        EmbeddedArtExt: meta.ArtExt,
    }, nil
```

- [ ] **Step 7: Run tests — confirm they pass**

```
go test ./internal/modules/fs_library/ -run "TestTagMetadataCarriesComposerAndGenre|TestMediaItemCarriesComposerAndEmbeddedGenre" -v
```

Expected: PASS.

- [ ] **Step 8: Run full suite**

```
go test ./internal/modules/fs_library/...
```

Expected: PASS (no regressions).

- [ ] **Step 9: Commit**

```bash
git add internal/modules/fs_library/module.go internal/modules/fs_library/module_test.go
git commit -m "feat(fs_library): extract Composer and EmbeddedGenre from file tags"
```

---

## Task 3: Add `LLMGenre` field to `AlbumMetadata` sidecar

**Files:**
- Modify: `internal/modules/fs_library/enrichment.go` (`AlbumMetadata` ~line 25)
- Test: `internal/modules/fs_library/module_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/modules/fs_library/module_test.go`:

```go
func TestAlbumMetadataLLMGenreRoundTrip(t *testing.T) {
    dir := t.TempDir()
    in := &AlbumMetadata{
        Version:   currentSidecarVersion,
        Artist:    "Glenn Gould",
        Album:     "Bach: Goldberg Variations",
        LLMGenre:  "Classical",
    }
    if err := writeSidecar(dir, in); err != nil {
        t.Fatalf("writeSidecar: %v", err)
    }
    out, err := readSidecar(dir)
    if err != nil {
        t.Fatalf("readSidecar: %v", err)
    }
    if out.LLMGenre != "Classical" {
        t.Fatalf("LLMGenre = %q, want Classical", out.LLMGenre)
    }
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/modules/fs_library/ -run TestAlbumMetadataLLMGenreRoundTrip
```

Expected: build error (`AlbumMetadata has no field LLMGenre`).

- [ ] **Step 3: Add the field**

In `internal/modules/fs_library/enrichment.go` ~line 25, change `AlbumMetadata` to:

```go
type AlbumMetadata struct {
    Version     int               `json:"version"`
    FetchedAt   time.Time         `json:"fetched_at"`
    Artist      string            `json:"artist"`
    Album       string            `json:"album"`
    MusicBrainz *MBMetadata       `json:"musicbrainz"`
    Discogs     *DiscogsMetadata  `json:"discogs"`
    ArtistInfo  *ArtistInfo       `json:"artist_info,omitempty"`
    Description *AlbumDescription `json:"description,omitempty"`

    // LLMGenre is the locally-classified top-level genre, one of the strings in
    // genreAllowlist (genre_classifier.go). Populated by the genre classifier
    // backfill goroutine; absent in older sidecars.
    LLMGenre string `json:"llm_genre,omitempty"`
}
```

- [ ] **Step 4: Run test — confirm it passes**

```
go test ./internal/modules/fs_library/ -run TestAlbumMetadataLLMGenreRoundTrip -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/modules/fs_library/enrichment.go internal/modules/fs_library/module_test.go
git commit -m "feat(fs_library): add LLMGenre field to AlbumMetadata sidecar"
```

---

## Task 4: Genre classifier — allowlist and response parser

**Files:**
- Create: `internal/modules/fs_library/genre_classifier.go`
- Create: `internal/modules/fs_library/genre_classifier_test.go`

This task creates the file with the static allowlist and the parser only. Ollama call and rollup map come in subsequent tasks.

- [ ] **Step 1: Write the failing test**

Create `internal/modules/fs_library/genre_classifier_test.go`:

```go
package fslibrary

import "testing"

func TestParseGenreResponse(t *testing.T) {
    cases := []struct {
        name string
        in   string
        want string
    }{
        {"exact match", "Classical", "Classical"},
        {"trailing newline", "Jazz\n", "Jazz"},
        {"surrounding prose", "The genre is Rock.", "Rock"},
        {"lowercased", "electronic", "Electronic"},
        {"mixed case", "hip-hop", "Hip-Hop"},
        {"r&b/soul forms", "R&B", "R&B/Soul"},
        {"r&b/soul forms 2", "Soul", "R&B/Soul"},
        {"unknown text", "Vaporwave", "Other"},
        {"empty", "", "Other"},
        {"refusal", "I'm sorry, I can't help with that.", "Other"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := parseGenreResponse(tc.in)
            if got != tc.want {
                t.Fatalf("parseGenreResponse(%q) = %q, want %q", tc.in, got, tc.want)
            }
        })
    }
}

func TestGenreAllowlistContents(t *testing.T) {
    expected := []string{
        "Classical", "Jazz", "Rock", "Pop", "Hip-Hop", "Electronic",
        "Folk", "Country", "Metal", "R&B/Soul", "Blues", "Reggae",
        "World", "Soundtrack", "Other",
    }
    if len(genreAllowlist) != len(expected) {
        t.Fatalf("genreAllowlist length = %d, want %d", len(genreAllowlist), len(expected))
    }
    for i, g := range expected {
        if genreAllowlist[i] != g {
            t.Fatalf("genreAllowlist[%d] = %q, want %q", i, genreAllowlist[i], g)
        }
    }
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/modules/fs_library/ -run "TestParseGenreResponse|TestGenreAllowlistContents"
```

Expected: build error (`undefined: parseGenreResponse`, `undefined: genreAllowlist`).

- [ ] **Step 3: Create `genre_classifier.go` with allowlist + parser**

Create `internal/modules/fs_library/genre_classifier.go`:

```go
// Package fslibrary genre classifier.
//
// Maps fine-grained metadata (raw embedded tags, MusicBrainz/Discogs genres)
// to a fixed flat list of 15 top-level genres for browse-by-genre and search.
// The classifier prefers a local Ollama LLM for accuracy; when the LLM is
// unreachable, a static rollup map is used as a fallback at index-build time.
package fslibrary

import (
    "strings"
)

// genreAllowlist is the fixed taxonomy. Order is the order shown to the LLM
// in the prompt and the order asserted by tests.
var genreAllowlist = []string{
    "Classical",
    "Jazz",
    "Rock",
    "Pop",
    "Hip-Hop",
    "Electronic",
    "Folk",
    "Country",
    "Metal",
    "R&B/Soul",
    "Blues",
    "Reggae",
    "World",
    "Soundtrack",
    "Other",
}

// genreSynonyms maps lowercased free-form text to the canonical genreAllowlist
// entry. Used by parseGenreResponse to tolerate small variations in LLM output
// (e.g., "soul" → "R&B/Soul"). Does NOT cover the full embedded-tag fallback
// vocabulary — that lives in genreRollup (see Task 5).
var genreSynonyms = map[string]string{
    "classical":  "Classical",
    "jazz":       "Jazz",
    "rock":       "Rock",
    "pop":        "Pop",
    "hip-hop":    "Hip-Hop",
    "hip hop":    "Hip-Hop",
    "rap":        "Hip-Hop",
    "electronic": "Electronic",
    "folk":       "Folk",
    "country":    "Country",
    "metal":      "Metal",
    "r&b":        "R&B/Soul",
    "rnb":        "R&B/Soul",
    "soul":       "R&B/Soul",
    "r&b/soul":   "R&B/Soul",
    "blues":      "Blues",
    "reggae":     "Reggae",
    "world":      "World",
    "soundtrack": "Soundtrack",
    "score":      "Soundtrack",
    "other":      "Other",
}

// parseGenreResponse normalizes raw LLM output to one of genreAllowlist.
// Strategy:
//  1. Strip whitespace and surrounding punctuation.
//  2. Try an exact (case-insensitive) match against genreAllowlist.
//  3. Try a longest-prefix match against genreSynonyms keys.
//  4. Try a substring scan for any allowlist entry within the text.
//  5. Otherwise return "Other".
func parseGenreResponse(s string) string {
    s = strings.TrimSpace(s)
    if s == "" {
        return "Other"
    }
    // Drop trailing punctuation/quotes.
    s = strings.Trim(s, ".\"' \t\n\r,;:")
    lower := strings.ToLower(s)

    // (1) Exact match against allowlist.
    for _, g := range genreAllowlist {
        if strings.EqualFold(s, g) {
            return g
        }
    }

    // (2) Synonym match — try the whole string first, then per-word.
    if g, ok := genreSynonyms[lower]; ok {
        return g
    }
    for _, w := range strings.FieldsFunc(lower, func(r rune) bool {
        return r == ' ' || r == '\n' || r == '\t' || r == '.' || r == ',' || r == ';' || r == ':'
    }) {
        if g, ok := genreSynonyms[w]; ok {
            return g
        }
    }

    // (3) Substring scan: does any allowlist entry appear in the text?
    for _, g := range genreAllowlist {
        if g == "Other" {
            continue
        }
        if strings.Contains(lower, strings.ToLower(g)) {
            return g
        }
    }

    return "Other"
}
```

- [ ] **Step 4: Run test — confirm it passes**

```
go test ./internal/modules/fs_library/ -run "TestParseGenreResponse|TestGenreAllowlistContents" -v
```

Expected: PASS for all sub-cases.

- [ ] **Step 5: Commit**

```bash
git add internal/modules/fs_library/genre_classifier.go internal/modules/fs_library/genre_classifier_test.go
git commit -m "feat(fs_library): add genre classifier allowlist and response parser"
```

---

## Task 5: Genre classifier — static rollup map for fallback

**Files:**
- Modify: `internal/modules/fs_library/genre_classifier.go`
- Modify: `internal/modules/fs_library/genre_classifier_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/modules/fs_library/genre_classifier_test.go`:

```go
func TestRollupGenre(t *testing.T) {
    cases := []struct {
        in   string
        want string
    }{
        {"baroque", "Classical"},
        {"Romantic", "Classical"},
        {"Chamber Music", "Classical"},
        {"Symphony", "Classical"},
        {"early music", "Classical"},
        {"opera", "Classical"},
        {"bebop", "Jazz"},
        {"swing", "Jazz"},
        {"post-bop", "Jazz"},
        {"big band", "Jazz"},
        {"shoegaze", "Rock"},
        {"grunge", "Rock"},
        {"alternative rock", "Rock"},
        {"indie", "Rock"},
        {"trance", "Electronic"},
        {"techno", "Electronic"},
        {"deep house", "Electronic"},
        {"trap", "Hip-Hop"},
        {"gangsta rap", "Hip-Hop"},
        {"film score", "Soundtrack"},
        {"video game music", "Soundtrack"},
        {"unknown nonsense", ""}, // no match → empty
        {"", ""},
    }
    for _, tc := range cases {
        t.Run(tc.in, func(t *testing.T) {
            got := rollupGenre(tc.in)
            if got != tc.want {
                t.Fatalf("rollupGenre(%q) = %q, want %q", tc.in, got, tc.want)
            }
        })
    }
}

func TestRollupGenreFromCandidates(t *testing.T) {
    // Walks a list and returns the first match.
    if got := rollupGenreFromCandidates([]string{"", "baroque", "rock"}); got != "Classical" {
        t.Fatalf("got %q, want Classical", got)
    }
    if got := rollupGenreFromCandidates([]string{"", ""}); got != "" {
        t.Fatalf("empty candidates: got %q, want \"\"", got)
    }
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/modules/fs_library/ -run "TestRollupGenre|TestRollupGenreFromCandidates"
```

Expected: build error (`undefined: rollupGenre`).

- [ ] **Step 3: Add rollup map and helpers**

Append to `internal/modules/fs_library/genre_classifier.go`:

```go
// genreRollup maps lowercased fine-grained genre/tag strings to a top-level
// allowlist entry. Used when the LLM is unavailable. Each key is matched as a
// substring (case-insensitive), so e.g. "Alternative Rock" hits "rock".
var genreRollup = []struct {
    pattern string
    target  string
}{
    // Classical
    {"baroque", "Classical"},
    {"romantic", "Classical"},
    {"chamber", "Classical"},
    {"symphony", "Classical"},
    {"symphonic", "Classical"},
    {"opera", "Classical"},
    {"concerto", "Classical"},
    {"sonata", "Classical"},
    {"early music", "Classical"},
    {"medieval", "Classical"},
    {"renaissance", "Classical"},
    {"orchestral", "Classical"},
    {"choral", "Classical"},
    {"classical", "Classical"},

    // Jazz
    {"bebop", "Jazz"},
    {"post-bop", "Jazz"},
    {"hard bop", "Jazz"},
    {"swing", "Jazz"},
    {"big band", "Jazz"},
    {"fusion", "Jazz"},
    {"smooth jazz", "Jazz"},
    {"free jazz", "Jazz"},
    {"jazz", "Jazz"},

    // Rock
    {"shoegaze", "Rock"},
    {"grunge", "Rock"},
    {"punk", "Rock"},
    {"hardcore", "Rock"},
    {"emo", "Rock"},
    {"indie", "Rock"},
    {"alternative", "Rock"},
    {"prog", "Rock"},
    {"psychedelic", "Rock"},
    {"garage", "Rock"},
    {"rock", "Rock"},

    // Pop
    {"k-pop", "Pop"},
    {"j-pop", "Pop"},
    {"synth-pop", "Pop"},
    {"synthpop", "Pop"},
    {"dance-pop", "Pop"},
    {"electropop", "Pop"},
    {"pop", "Pop"},

    // Hip-Hop
    {"hip-hop", "Hip-Hop"},
    {"hip hop", "Hip-Hop"},
    {"trap", "Hip-Hop"},
    {"gangsta rap", "Hip-Hop"},
    {"rap", "Hip-Hop"},

    // Electronic
    {"trance", "Electronic"},
    {"techno", "Electronic"},
    {"deep house", "Electronic"},
    {"house music", "Electronic"},
    {"house", "Electronic"},
    {"drum and bass", "Electronic"},
    {"dubstep", "Electronic"},
    {"ambient", "Electronic"},
    {"idm", "Electronic"},
    {"breakbeat", "Electronic"},
    {"electronica", "Electronic"},
    {"electronic", "Electronic"},

    // Folk
    {"folk", "Folk"},
    {"singer-songwriter", "Folk"},
    {"acoustic", "Folk"},
    {"americana", "Folk"},
    {"bluegrass", "Folk"},

    // Country
    {"country", "Country"},
    {"honky-tonk", "Country"},

    // Metal
    {"black metal", "Metal"},
    {"death metal", "Metal"},
    {"doom metal", "Metal"},
    {"thrash", "Metal"},
    {"metalcore", "Metal"},
    {"metal", "Metal"},

    // R&B/Soul
    {"r&b", "R&B/Soul"},
    {"rnb", "R&B/Soul"},
    {"neo-soul", "R&B/Soul"},
    {"motown", "R&B/Soul"},
    {"soul", "R&B/Soul"},
    {"funk", "R&B/Soul"},

    // Blues
    {"delta blues", "Blues"},
    {"chicago blues", "Blues"},
    {"blues", "Blues"},

    // Reggae
    {"dub", "Reggae"},
    {"ska", "Reggae"},
    {"dancehall", "Reggae"},
    {"reggae", "Reggae"},

    // World
    {"latin", "World"},
    {"flamenco", "World"},
    {"afrobeat", "World"},
    {"bossa nova", "World"},
    {"world", "World"},
    {"celtic", "World"},

    // Soundtrack
    {"soundtrack", "Soundtrack"},
    {"film score", "Soundtrack"},
    {"video game", "Soundtrack"},
    {"score", "Soundtrack"},
}

// rollupGenre returns a genreAllowlist entry for the given raw text by
// substring-matching against genreRollup. Returns "" if no match.
func rollupGenre(raw string) string {
    s := strings.ToLower(strings.TrimSpace(raw))
    if s == "" {
        return ""
    }
    for _, e := range genreRollup {
        if strings.Contains(s, e.pattern) {
            return e.target
        }
    }
    return ""
}

// rollupGenreFromCandidates tries each candidate string in order, returning
// the first non-empty rollup match.
func rollupGenreFromCandidates(candidates []string) string {
    for _, c := range candidates {
        if g := rollupGenre(c); g != "" {
            return g
        }
    }
    return ""
}
```

- [ ] **Step 4: Run test — confirm it passes**

```
go test ./internal/modules/fs_library/ -run "TestRollupGenre|TestRollupGenreFromCandidates" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/modules/fs_library/genre_classifier.go internal/modules/fs_library/genre_classifier_test.go
git commit -m "feat(fs_library): add static genre rollup map for LLM fallback"
```

---

## Task 6: Genre classifier — Ollama-backed `Classify`

**Files:**
- Modify: `internal/modules/fs_library/genre_classifier.go`
- Modify: `internal/modules/fs_library/genre_classifier_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/modules/fs_library/genre_classifier_test.go`, replace the existing `import "testing"` line with:

```go
import (
    "context"
    "errors"
    "strings"
    "testing"
)
```

Then append:

```go
type stubGenerator struct {
    response string
    err      error
    lastPrompt string
}

func (s *stubGenerator) Generate(ctx context.Context, prompt string) (string, error) {
    s.lastPrompt = prompt
    return s.response, s.err
}

func TestBuildGenrePrompt(t *testing.T) {
    in := ClassifyInput{
        Artist:        "Glenn Gould",
        Album:         "Bach: Goldberg Variations",
        TrackTitles:   []string{"Aria", "Variation 1"},
        EmbeddedGenre: "Classical",
        MBGenres:      []string{"baroque", "early music"},
    }
    p := buildGenrePrompt(in)
    if !strings.Contains(p, "Glenn Gould") {
        t.Fatal("prompt missing artist")
    }
    if !strings.Contains(p, "Bach: Goldberg Variations") {
        t.Fatal("prompt missing album")
    }
    if !strings.Contains(p, "baroque") {
        t.Fatal("prompt missing MBGenres")
    }
    // All allowlist entries should appear so the model knows the valid set.
    for _, g := range genreAllowlist {
        if !strings.Contains(p, g) {
            t.Fatalf("prompt missing allowlist entry %q", g)
        }
    }
}

func TestOllamaClassifierClassifyHappyPath(t *testing.T) {
    gen := &stubGenerator{response: "Classical"}
    c := &ollamaGenreClassifier{gen: gen}
    got, err := c.Classify(context.Background(), ClassifyInput{Artist: "Bach", Album: "Mass in B Minor"})
    if err != nil {
        t.Fatalf("Classify: %v", err)
    }
    if got != "Classical" {
        t.Fatalf("got %q, want Classical", got)
    }
    if gen.lastPrompt == "" {
        t.Fatal("prompt was not sent")
    }
}

func TestOllamaClassifierClassifyError(t *testing.T) {
    gen := &stubGenerator{err: errors.New("boom")}
    c := &ollamaGenreClassifier{gen: gen}
    got, err := c.Classify(context.Background(), ClassifyInput{Artist: "X", Album: "Y"})
    if err == nil {
        t.Fatal("expected error, got nil")
    }
    if got != "" {
        t.Fatalf("got %q, want \"\"", got)
    }
}

func TestOllamaClassifierUnparseableResponse(t *testing.T) {
    gen := &stubGenerator{response: "I cannot determine that."}
    c := &ollamaGenreClassifier{gen: gen}
    got, err := c.Classify(context.Background(), ClassifyInput{Artist: "X", Album: "Y"})
    if err != nil {
        t.Fatalf("Classify: %v", err)
    }
    if got != "Other" {
        t.Fatalf("got %q, want Other (parser fallback)", got)
    }
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/modules/fs_library/ -run "TestBuildGenrePrompt|TestOllamaClassifier"
```

Expected: build error (`undefined: ClassifyInput`, `undefined: ollamaGenreClassifier`, etc.).

- [ ] **Step 3: Define interfaces and implementation**

Append to `internal/modules/fs_library/genre_classifier.go`:

```go
import (
    "context"
    "fmt"
)

// ClassifyInput is the per-album input to a GenreClassifier.
type ClassifyInput struct {
    Artist        string
    Album         string
    TrackTitles   []string
    EmbeddedGenre string
    MBGenres      []string
}

// GenreClassifier classifies an album into one of genreAllowlist.
// Implementations may use a local LLM, a rule-based fallback, or both.
type GenreClassifier interface {
    Classify(ctx context.Context, in ClassifyInput) (string, error)
}

// promptGenerator is the minimal surface ollamaGenreClassifier needs from
// OllamaGenerator (defined in embedding.go). Splitting it out lets tests use
// a stub.
type promptGenerator interface {
    Generate(ctx context.Context, prompt string) (string, error)
}

// ollamaGenreClassifier is a GenreClassifier backed by an Ollama text model.
type ollamaGenreClassifier struct {
    gen promptGenerator
}

// NewOllamaGenreClassifier wraps an OllamaGenerator. Returns nil if gen is nil.
func NewOllamaGenreClassifier(gen *OllamaGenerator) GenreClassifier {
    if gen == nil {
        return nil
    }
    return &ollamaGenreClassifier{gen: gen}
}

func (c *ollamaGenreClassifier) Classify(ctx context.Context, in ClassifyInput) (string, error) {
    prompt := buildGenrePrompt(in)
    raw, err := c.gen.Generate(ctx, prompt)
    if err != nil {
        return "", err
    }
    return parseGenreResponse(raw), nil
}

// buildGenrePrompt formats the classification request as a single prompt.
func buildGenrePrompt(in ClassifyInput) string {
    var trackList string
    if len(in.TrackTitles) > 0 {
        n := len(in.TrackTitles)
        if n > 10 {
            n = 10
        }
        trackList = strings.Join(in.TrackTitles[:n], "; ")
    }
    mbStr := strings.Join(in.MBGenres, ", ")

    return fmt.Sprintf(`You are a music classifier. Given an album, return the single best top-level genre.

Valid genres (return EXACTLY one of these, with nothing else):
%s

Album:
Artist: %s
Title: %s
Sample track titles: %s
Embedded genre tag: %s
Existing fine-grained genres: %s

Reply with just the genre name. No explanation.`,
        strings.Join(genreAllowlist, ", "),
        in.Artist, in.Album, trackList, in.EmbeddedGenre, mbStr)
}
```

Note: `OllamaGenerator` lives in `embedding.go` — confirm the type name with `grep -n "type OllamaGenerator" internal/modules/fs_library/embedding.go` before relying on it. If the constructor signature differs, adjust `NewOllamaGenreClassifier` accordingly.

- [ ] **Step 4: Consolidate the imports in `genre_classifier.go`**

If Step 3 inserted a second `import` block, merge them so the file has exactly one `import (...)` near the top:

```go
package fslibrary

import (
    "context"
    "fmt"
    "strings"
)
```

- [ ] **Step 5: Run tests — confirm they pass**

```
go test ./internal/modules/fs_library/ -run "TestBuildGenrePrompt|TestOllamaClassifier" -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/modules/fs_library/genre_classifier.go internal/modules/fs_library/genre_classifier_test.go
git commit -m "feat(fs_library): add Ollama-backed GenreClassifier with prompt + parse"
```

---

## Task 7: `backfillGenres` goroutine

**Files:**
- Modify: `internal/modules/fs_library/genre_classifier.go` (add `backfillGenres` method on Module — placed here for cohesion with the classifier)
- Modify: `internal/modules/fs_library/module.go` (Module struct: add `genreClassifier GenreClassifier` field)
- Test: `internal/modules/fs_library/genre_classifier_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/modules/fs_library/genre_classifier_test.go`:

```go
type recordingClassifier struct {
    calls   []ClassifyInput
    respond func(in ClassifyInput) (string, error)
}

func (r *recordingClassifier) Classify(_ context.Context, in ClassifyInput) (string, error) {
    r.calls = append(r.calls, in)
    if r.respond != nil {
        return r.respond(in)
    }
    return "Classical", nil
}

func TestBackfillGenresOnlyMissing(t *testing.T) {
    log := zap.NewNop()
    m := &Module{log: log}
    rc := &recordingClassifier{}
    m.genreClassifier = rc

    dir1 := t.TempDir()
    dir2 := t.TempDir()
    metas := map[string]*AlbumMetadata{
        "A|X": {Version: currentSidecarVersion, Artist: "A", Album: "X"},                  // missing -> classify
        "B|Y": {Version: currentSidecarVersion, Artist: "B", Album: "Y", LLMGenre: "Jazz"}, // skip
    }
    dirs := map[string]string{"A|X": dir1, "B|Y": dir2}

    m.backfillGenres(context.Background(), metas, dirs)

    if len(rc.calls) != 1 {
        t.Fatalf("Classify called %d times, want 1", len(rc.calls))
    }
    if rc.calls[0].Artist != "A" || rc.calls[0].Album != "X" {
        t.Fatalf("classified wrong album: %+v", rc.calls[0])
    }
    if metas["A|X"].LLMGenre != "Classical" {
        t.Fatalf("LLMGenre = %q, want Classical", metas["A|X"].LLMGenre)
    }
    // Sidecar should be persisted.
    out, err := readSidecar(dir1)
    if err != nil {
        t.Fatalf("readSidecar: %v", err)
    }
    if out.LLMGenre != "Classical" {
        t.Fatalf("persisted LLMGenre = %q, want Classical", out.LLMGenre)
    }
}

func TestBackfillGenresNoClassifierIsNoOp(t *testing.T) {
    log := zap.NewNop()
    m := &Module{log: log}
    metas := map[string]*AlbumMetadata{"A|X": {Artist: "A", Album: "X"}}
    m.backfillGenres(context.Background(), metas, map[string]string{"A|X": t.TempDir()})
    if metas["A|X"].LLMGenre != "" {
        t.Fatalf("classified without classifier: %q", metas["A|X"].LLMGenre)
    }
}
```

- [ ] **Step 2: Add the field to Module**

In `internal/modules/fs_library/module.go`, in the `Module` struct (~line 380), near `summaryGen`, add:

```go
    genreClassifier GenreClassifier
```

- [ ] **Step 3: Run test — confirm it fails to compile**

```
go test ./internal/modules/fs_library/ -run TestBackfillGenres
```

Expected: build error (`Module has no method backfillGenres`).

- [ ] **Step 4: Implement `backfillGenres`**

Append to `internal/modules/fs_library/genre_classifier.go`:

```go
import (
    // ... existing imports plus:
    "go.uber.org/zap"
)

// backfillGenres classifies every album in `metas` that does not yet have an
// LLMGenre. It mirrors backfillSummaries: snapshot candidates under RLock,
// classify outside the lock, write sidecars, and swap in updated metadata
// under write lock. No-op if Module.genreClassifier is nil.
func (m *Module) backfillGenres(ctx context.Context, metas map[string]*AlbumMetadata, dirs map[string]string) {
    if m.genreClassifier == nil {
        return
    }

    var candidates []string
    m.mu.RLock()
    for key, meta := range metas {
        if meta == nil || meta.LLMGenre != "" {
            continue
        }
        candidates = append(candidates, key)
    }
    m.mu.RUnlock()

    if len(candidates) == 0 {
        return
    }

    m.log.Info("genre backfill starting", zap.Int("albums", len(candidates)))
    classified := 0
    for _, key := range candidates {
        if ctx.Err() != nil {
            break
        }
        m.mu.RLock()
        meta := metas[key]
        m.mu.RUnlock()
        dir := dirs[key]

        in := ClassifyInput{
            Artist: meta.Artist,
            Album:  meta.Album,
        }
        if meta.MusicBrainz != nil {
            in.MBGenres = meta.MusicBrainz.Genres
        }

        genre, err := m.genreClassifier.Classify(ctx, in)
        if err != nil {
            m.log.Debug("genre classify failed",
                zap.String("artist", meta.Artist),
                zap.String("album", meta.Album),
                zap.Error(err))
            continue
        }
        if genre == "" {
            continue
        }

        // Copy before mutating to avoid races with concurrent readers.
        updated := *meta
        updated.LLMGenre = genre

        if dir != "" {
            if err := writeSidecar(dir, &updated); err != nil {
                m.log.Warn("genre backfill sidecar write failed",
                    zap.String("dir", dir),
                    zap.Error(err))
                continue
            }
        }

        m.mu.Lock()
        metas[key] = &updated
        m.mu.Unlock()
        classified++
    }
    m.log.Info("genre backfill complete",
        zap.Int("classified", classified),
        zap.Int("total", len(candidates)))
}
```

If the import block already has `"context"` and `"fmt"`, merge them rather than introducing a duplicate block.

- [ ] **Step 5: Run tests — confirm they pass**

```
go test ./internal/modules/fs_library/ -run "TestBackfillGenres" -v
```

Expected: PASS.

- [ ] **Step 6: Run full suite**

```
go test ./internal/modules/fs_library/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/modules/fs_library/genre_classifier.go internal/modules/fs_library/genre_classifier_test.go internal/modules/fs_library/module.go
git commit -m "feat(fs_library): backfill LLM genre into AlbumMetadata sidecars"
```

---

## Task 8: Rewrite `buildGenreIndex` to use `LLMGenre` with rollup-map fallback

**Files:**
- Modify: `internal/modules/fs_library/module.go` (`buildGenreIndex` ~line 3210)
- Test: `internal/modules/fs_library/module_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/modules/fs_library/module_test.go`:

```go
func TestBuildGenreIndexUsesLLMGenre(t *testing.T) {
    idx := &libraryIndex{
        Audio: map[string]artistEntry{
            "Glenn Gould": {Name: "Glenn Gould", Albums: map[string]albumEntry{
                "Goldberg Variations": {Name: "Goldberg Variations"},
            }},
            "Miles Davis": {Name: "Miles Davis", Albums: map[string]albumEntry{
                "Kind of Blue": {Name: "Kind of Blue"},
            }},
            "Unknown": {Name: "Unknown", Albums: map[string]albumEntry{
                "Mystery": {Name: "Mystery"},
            }},
        },
        GenreAlbums: map[string][]genreAlbumRef{},
        Containers:  map[string]containerInfo{},
    }
    enrich := map[string]*AlbumMetadata{
        "Glenn Gould|Goldberg Variations": {LLMGenre: "Classical"},
        "Miles Davis|Kind of Blue":        {LLMGenre: "Jazz"},
        // "Unknown|Mystery" — no entry, no LLMGenre, no rollup → "Unknown"
    }
    buildGenreIndex(idx, enrich)

    if got := idx.GenreAlbums["Classical"]; len(got) != 1 || got[0].Artist != "Glenn Gould" {
        t.Fatalf("Classical bucket = %+v", got)
    }
    if got := idx.GenreAlbums["Jazz"]; len(got) != 1 || got[0].Artist != "Miles Davis" {
        t.Fatalf("Jazz bucket = %+v", got)
    }
    if got := idx.GenreAlbums["Unknown"]; len(got) != 1 || got[0].Artist != "Unknown" {
        t.Fatalf("Unknown bucket = %+v", got)
    }
    if len(idx.GenreAlbums) != 3 {
        t.Fatalf("expected exactly 3 genre buckets, got %d: %v",
            len(idx.GenreAlbums), keysOf(idx.GenreAlbums))
    }
}

func TestBuildGenreIndexRollupFallback(t *testing.T) {
    idx := &libraryIndex{
        Audio: map[string]artistEntry{
            "X": {Name: "X", Albums: map[string]albumEntry{"A": {Name: "A"}}},
        },
        GenreAlbums: map[string][]genreAlbumRef{},
        Containers:  map[string]containerInfo{},
    }
    enrich := map[string]*AlbumMetadata{
        "X|A": {
            // No LLMGenre — fall back to rollup map on MB genres.
            MusicBrainz: &MBMetadata{Genres: []string{"baroque"}},
        },
    }
    buildGenreIndex(idx, enrich)
    if got := idx.GenreAlbums["Classical"]; len(got) != 1 {
        t.Fatalf("expected Classical bucket via rollup, got %v", idx.GenreAlbums)
    }
}

func keysOf[V any](m map[string]V) []string {
    ks := make([]string, 0, len(m))
    for k := range m {
        ks = append(ks, k)
    }
    return ks
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/modules/fs_library/ -run "TestBuildGenreIndexUsesLLMGenre|TestBuildGenreIndexRollupFallback"
```

Expected: existing test passes for some old shape but new tests fail because the function still groups by all the raw MB genres.

- [ ] **Step 3: Rewrite `buildGenreIndex`**

In `internal/modules/fs_library/module.go` ~line 3210, replace the entire `buildGenreIndex` function with:

```go
// buildGenreIndex populates idx.GenreAlbums by grouping albums under their
// classified top-level genre. Precedence per album:
//
//  1. AlbumMetadata.LLMGenre (cached classifier output) — preferred.
//  2. Rollup-map lookup on MB genres + ArtistInfo genres + ArtistInfo tags
//     (fallback when the LLM has not yet classified the album).
//  3. "Unknown".
//
// Each album lands in exactly one bucket, so browse-by-genre stays clean.
func buildGenreIndex(idx *libraryIndex, enrichMeta map[string]*AlbumMetadata) {
    idx.GenreAlbums = make(map[string][]genreAlbumRef)
    for artistName, artist := range idx.Audio {
        for albumName := range artist.Albums {
            key := artistName + "|" + albumName
            meta := enrichMeta[key]

            genre := classifyAlbumBucket(meta)
            ref := genreAlbumRef{Artist: artistName, Album: albumName}
            idx.GenreAlbums[genre] = append(idx.GenreAlbums[genre], ref)
        }
    }
    for genre, albums := range idx.GenreAlbums {
        sort.Slice(albums, func(i, j int) bool {
            if albums[i].Artist != albums[j].Artist {
                return albums[i].Artist < albums[j].Artist
            }
            return albums[i].Album < albums[j].Album
        })
        idx.GenreAlbums[genre] = albums
        hash := containerHash("genre", genre, "")
        idx.Containers[hash] = containerInfo{Type: "genre", Artist: genre}
    }
}

// classifyAlbumBucket picks the single bucket an album lives in for browse.
func classifyAlbumBucket(meta *AlbumMetadata) string {
    if meta == nil {
        return "Unknown"
    }
    if meta.LLMGenre != "" {
        return meta.LLMGenre
    }
    var candidates []string
    if meta.MusicBrainz != nil {
        candidates = append(candidates, meta.MusicBrainz.Genres...)
        candidates = append(candidates, meta.MusicBrainz.Tags...)
    }
    if meta.ArtistInfo != nil {
        candidates = append(candidates, meta.ArtistInfo.Genres...)
        candidates = append(candidates, meta.ArtistInfo.Tags...)
    }
    if g := rollupGenreFromCandidates(candidates); g != "" {
        return g
    }
    return "Unknown"
}
```

- [ ] **Step 4: Run tests — confirm they pass**

```
go test ./internal/modules/fs_library/ -run "TestBuildGenreIndex" -v
```

Expected: PASS for both new tests.

- [ ] **Step 5: Run full suite**

```
go test ./internal/modules/fs_library/...
```

Expected: PASS. If a pre-existing test asserts the old behavior (multiple buckets per album), update it to match the new contract — single bucket per album.

- [ ] **Step 6: Commit**

```bash
git add internal/modules/fs_library/module.go internal/modules/fs_library/module_test.go
git commit -m "refactor(fs_library): group browse-by-genre by LLMGenre with rollup fallback"
```

---

## Task 9: Extend `buildSearchText` with LLMGenre, Composer, track-artists

**Files:**
- Modify: `internal/modules/fs_library/module.go` (`buildSearchText` ~line 2064)
- Test: `internal/modules/fs_library/module_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/modules/fs_library/module_test.go`:

```go
func TestBuildSearchTextIncludesLLMGenreAndComposer(t *testing.T) {
    item := mediaItem{
        Name:          "Aria",
        Title:         "Aria",
        Album:         "Goldberg Variations",
        Artists:       []string{"Glenn Gould"},
        Composer:      "Johann Sebastian Bach",
        EmbeddedGenre: "Baroque",
    }
    enrich := &AlbumMetadata{
        LLMGenre: "Classical",
    }
    txt := buildSearchText(item, enrich)
    for _, want := range []string{"glenn gould", "goldberg", "classical", "bach", "baroque"} {
        if !strings.Contains(txt, want) {
            t.Fatalf("buildSearchText missing %q in %q", want, txt)
        }
    }
}

func TestBuildSearchTextNilEnrichmentStillIncludesLocalFields(t *testing.T) {
    item := mediaItem{
        Name:          "Aria",
        Title:         "Aria",
        Composer:      "Bach",
        EmbeddedGenre: "Classical",
    }
    txt := buildSearchText(item, nil)
    if !strings.Contains(txt, "bach") {
        t.Fatalf("missing composer: %q", txt)
    }
    if !strings.Contains(txt, "classical") {
        t.Fatalf("missing embedded genre: %q", txt)
    }
}
```

(Add `"strings"` to the test file's imports if not already present.)

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/modules/fs_library/ -run "TestBuildSearchTextIncludesLLMGenreAndComposer|TestBuildSearchTextNilEnrichmentStillIncludesLocalFields"
```

Expected: FAIL on the assertions about "classical", "bach", "baroque".

- [ ] **Step 3: Modify `buildSearchText`**

In `internal/modules/fs_library/module.go` ~line 2064, replace the function with:

```go
// buildSearchText creates the lowercased search text for a media item.
// Called once at scan time to precompute the searchable string.
func buildSearchText(item mediaItem, enrich *AlbumMetadata) string {
    parts := []string{item.Name, item.Title, item.Album, strings.Join(item.Artists, " ")}
    if item.Composer != "" {
        parts = append(parts, item.Composer)
    }
    if item.EmbeddedGenre != "" {
        parts = append(parts, item.EmbeddedGenre)
    }
    if enrich != nil {
        if enrich.LLMGenre != "" {
            parts = append(parts, enrich.LLMGenre)
        }
        if mb := enrich.MusicBrainz; mb != nil {
            parts = append(parts, strings.Join(mb.Genres, " "))
            parts = append(parts, strings.Join(mb.Tags, " "))
            if mb.Label != "" {
                parts = append(parts, mb.Label)
            }
        }
        if dc := enrich.Discogs; dc != nil {
            parts = append(parts, strings.Join(dc.Styles, " "))
        }
        if ai := enrich.ArtistInfo; ai != nil {
            parts = append(parts, ai.Name)
            if ai.Origin != "" {
                parts = append(parts, ai.Origin)
            }
            if ai.Type != "" {
                parts = append(parts, ai.Type)
            }
            if len(ai.Members) > 0 {
                parts = append(parts, strings.Join(ai.Members, " "))
            }
        }
    }
    return strings.ToLower(strings.Join(parts, " "))
}
```

- [ ] **Step 4: Run tests — confirm they pass**

```
go test ./internal/modules/fs_library/ -run "TestBuildSearchText" -v
```

Expected: PASS.

- [ ] **Step 5: Run full suite**

```
go test ./internal/modules/fs_library/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/modules/fs_library/module.go internal/modules/fs_library/module_test.go
git commit -m "feat(fs_library): index LLM genre, composer, and embedded genre in search text"
```

---

## Task 10: Search scorer — boost LLMGenre and composer matches

**Files:**
- Modify: `internal/modules/fs_library/module.go` (`search` scoring loop ~line 1681)
- Test: `internal/modules/fs_library/module_test.go`

The scorer currently only sees `libraryItem` (the result-shape struct), which does not carry composer/genre. We have two options: (a) add fields to `libraryItem`, or (b) carry the source `mediaItem` through the scoring loop. Option (b) is simpler and avoids leaking internals to MQTT clients.

- [ ] **Step 1: Write the failing test**

Append to `internal/modules/fs_library/module_test.go`:

```go
func TestSearchBoostsGenreAndComposerMatches(t *testing.T) {
    log := zap.NewNop()
    cfg := Config{
        NodeID: "mu:library:filesystem:test:default",
        TopicBase: "mu",
        Roots: []string{t.TempDir()},
        ScanMode: "manual",
    }
    m, err := NewModule(log, nil, cfg)
    if err != nil {
        t.Fatalf("NewModule: %v", err)
    }

    // Hand-build an index with two audio items that share artist but differ in
    // genre/composer. Searching "classical" should rank the Classical one first.
    bachID := "audio:bach"
    rockID := "audio:rock"
    m.index.Items[bachID] = mediaItem{
        ID: bachID, Name: "Aria", Title: "Aria", MediaType: "Audio",
        Artists: []string{"Glenn Gould"}, Album: "Goldberg",
        Composer: "Johann Sebastian Bach", EmbeddedGenre: "Baroque",
        SearchText: "aria glenn gould goldberg johann sebastian bach baroque classical",
    }
    m.index.Items[rockID] = mediaItem{
        ID: rockID, Name: "Aria Two", Title: "Aria Two", MediaType: "Audio",
        Artists: []string{"Glenn Gould"}, Album: "Other",
        SearchText: "aria two glenn gould other",
    }

    items, _ := m.search("classical", 0, 10)
    if len(items) == 0 || items[0].ItemID != bachID {
        t.Fatalf("expected Bach-tagged track first; got %+v", items)
    }

    items2, _ := m.search("bach", 0, 10)
    if len(items2) == 0 || items2[0].ItemID != bachID {
        t.Fatalf("expected composer-matched track first; got %+v", items2)
    }
}
```

- [ ] **Step 2: Run test — confirm it fails**

```
go test ./internal/modules/fs_library/ -run TestSearchBoostsGenreAndComposerMatches
```

Expected: FAIL — likely both items return but order is wrong, or only one matches due to limited search text.

- [ ] **Step 3: Modify the scorer**

In `internal/modules/fs_library/module.go` `search` (~line 1681), inside the per-item collection loop, carry the source mediaItem alongside the result. Replace the `for _, item := range m.index.Items` block and the subsequent scoring block (from "items := make(...)" through the end of the scoring `sort.Slice`) with:

```go
    type scoredItem struct {
        result libraryItem
        src    mediaItem
        score  int
    }

    scored := make([]scoredItem, 0)
    queryLower := strings.ToLower(query)

    for _, item := range m.index.Items {
        if !containsAllTerms(item, terms) {
            continue
        }
        artURL := ""
        if item.MediaType == "Audio" {
            artistName := firstOr(item.Artists, "Unknown Artist")
            albumName := item.Album
            if albumName == "" {
                albumName = "Unknown Album"
            }
            artURL = m.artURLUnlocked(containerHash("album", artistName, albumName))
        }
        result := libraryItem{
            ItemID:     item.ID,
            Name:       item.Name,
            Type:       item.MediaType,
            MediaType:  item.MediaType,
            Artists:    item.Artists,
            Album:      item.Album,
            DurationMS: item.DurationMS,
            ImageURL:   artURL,
        }

        s := 0
        nameLower := strings.ToLower(item.Name)
        if nameLower == queryLower {
            s += 100
        } else if strings.HasPrefix(nameLower, queryLower) {
            s += 80
        }
        allInName := true
        for _, term := range terms {
            if !strings.Contains(nameLower, term) {
                allInName = false
                break
            }
        }
        if allInName {
            s += 50
        }
        for _, artist := range item.Artists {
            artistLower := strings.ToLower(artist)
            for _, term := range terms {
                if strings.Contains(artistLower, term) {
                    s += 20
                    break
                }
            }
        }
        if item.Album != "" {
            albumLower := strings.ToLower(item.Album)
            for _, term := range terms {
                if strings.Contains(albumLower, term) {
                    s += 10
                    break
                }
            }
        }

        // New: LLM-genre boost. We don't store LLMGenre on mediaItem, but the
        // album's enrichment metadata carries it. Look it up by artist|album.
        if item.MediaType == "Audio" {
            artistName := firstOr(item.Artists, "")
            if enrich := m.enrichMeta[artistName+"|"+item.Album]; enrich != nil && enrich.LLMGenre != "" {
                lg := strings.ToLower(enrich.LLMGenre)
                for _, term := range terms {
                    if strings.Contains(lg, term) {
                        s += 30
                        break
                    }
                }
            }
        }

        // New: Composer boost.
        if item.Composer != "" {
            cl := strings.ToLower(item.Composer)
            for _, term := range terms {
                if strings.Contains(cl, term) {
                    s += 25
                    break
                }
            }
        }

        if len(nameLower) < 30 {
            s += 5
        }

        scored = append(scored, scoredItem{result: result, src: item, score: s})
        if len(scored) >= maxSearchResults {
            break
        }
    }

    sort.Slice(scored, func(i, j int) bool {
        if scored[i].score != scored[j].score {
            return scored[i].score > scored[j].score
        }
        return strings.ToLower(scored[i].result.Name) < strings.ToLower(scored[j].result.Name)
    })

    items := make([]libraryItem, len(scored))
    for i, si := range scored {
        items[i] = si.result
    }
    total = int64(len(items))
    return paginate(items, start, count), total
```

(`m.enrichMeta` is read under `m.mu.RLock()` — the surrounding function already holds it.)

- [ ] **Step 4: Run test — confirm it passes**

```
go test ./internal/modules/fs_library/ -run TestSearchBoostsGenreAndComposerMatches -v
```

Expected: PASS.

- [ ] **Step 5: Run full suite**

```
go test ./internal/modules/fs_library/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/modules/fs_library/module.go internal/modules/fs_library/module_test.go
git commit -m "feat(fs_library): boost search hits on LLM genre and composer fields"
```

---

## Task 11: Wire `GenreClassifier` and `backfillGenres` into the scan flow

**Files:**
- Modify: `internal/modules/fs_library/module.go` (Config struct: add `GenreModel`; NewModule constructor: build classifier; scanInner: launch backfill)

- [ ] **Step 1: Add `GenreModel` to Config**

In `internal/modules/fs_library/module.go` `Config` (~line 305, after `SummaryEndpoint`), add:

```go
    // GenreModel is the Ollama model used for top-level genre classification.
    // Defaults to SummaryModel when empty.
    GenreModel string
```

- [ ] **Step 2: Construct the classifier in `NewModule`**

In `internal/modules/fs_library/module.go` `NewModule` (~line 593), after the existing `summaryGen` is built and before the final `return &Module{...}`, add:

```go
    var genreClassifier GenreClassifier
    // Reuse the summary endpoint resolution: GenreModel falls back to
    // SummaryModel; SummaryEndpoint falls back to EmbeddingEndpoint.
    genreModel := cfg.GenreModel
    if genreModel == "" {
        genreModel = cfg.SummaryModel
    }
    if genreModel != "" {
        endpoint := cfg.SummaryEndpoint
        if endpoint == "" {
            endpoint = cfg.EmbeddingEndpoint
        }
        if endpoint != "" {
            gen := NewOllamaGenerator(endpoint, genreModel)
            genreClassifier = NewOllamaGenreClassifier(gen)
        }
    }
```

(Confirmed signature: `func NewOllamaGenerator(endpoint, model string) *OllamaGenerator` — no error return, no config struct.)

Then, in the returned `&Module{...}` literal at the end of `NewModule`, add:

```go
        genreClassifier: genreClassifier,
```

- [ ] **Step 3: Launch `backfillGenres` after each scan**

In `internal/modules/fs_library/module.go` `scanInner` (~line 2590, near the existing `go m.backfillSummaries(...)` call), add:

```go
    if m.genreClassifier != nil {
        go m.backfillGenres(ctx, enrichMeta, enrichDirs)
    }
```

- [ ] **Step 4: Build and run full test suite**

```
go build ./... && go test ./internal/modules/fs_library/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/modules/fs_library/module.go
git commit -m "feat(fs_library): wire GenreClassifier into scan flow with backfill"
```

---

## Task 12: Documentation — update module header and sample TOML

**Files:**
- Modify: `internal/modules/fs_library/module.go` (header doc-comment around line 1-215)
- Modify: `integrations/home_assistant/mud_config/mud.toml` (add commented examples)

- [ ] **Step 1: Update the module header doc-comment**

In `internal/modules/fs_library/module.go`, in the package-doc block (~lines 1-215), find the "Configuration Example" section. After the `summary_endpoint = ""` line (around line 201), add:

```
		genre_model = ""                      # Ollama model for top-level genre classification (default: summary_model)
		scan_mode = "auto"                    # "auto" (initial scan + periodic) or "manual" (no auto-scan)
```

Then in the "Commands" section (~line 203), no change needed — `library.rescan` already covers manual mode.

Above the Commands list, add a new short paragraph:

```
# Genre Classification

The fs_library module groups albums for browse-by-genre using a fixed flat
list of 15 top-level genres. Each album is classified by a local Ollama model
(see `genre_model`) and the result is cached in the album's
.mu_album_metadata.json sidecar (`llm_genre` field). When the LLM is
unreachable, a static rollup map maps fine-grained MusicBrainz/embedded tags
(e.g. "baroque", "shoegaze") to the top-level family at index-build time.

The 15 genres are: Classical, Jazz, Rock, Pop, Hip-Hop, Electronic, Folk,
Country, Metal, R&B/Soul, Blues, Reggae, World, Soundtrack, Other.

# Manual Scan Mode

Set `scan_mode = "manual"` to disable automatic scanning entirely — the
persisted index loads at startup, MQTT subscribers come up, but no
filesystem walk runs until the user invokes `library.rescan`. Use this on
low-power hosts where periodic scans interfere with playback.
```

- [ ] **Step 2: Update the sample TOML**

In `integrations/home_assistant/mud_config/mud.toml`, append two commented lines under the `[modules.fs_library]` block:

```toml
# scan_mode = "manual"     # disable automatic scanning; rescans are user-triggered
# genre_model = "gemma3:12b"  # local Ollama model for genre classification (defaults to summary_model)
```

- [ ] **Step 3: Verify the file still parses (build only, since this file is read at runtime)**

```
go build ./...
```

Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/modules/fs_library/module.go integrations/home_assistant/mud_config/mud.toml
git commit -m "docs(fs_library): document scan_mode and genre_model config options"
```

---

## Task 13: Final integration check

**Files:**
- None modified — verification only.

- [ ] **Step 1: Run the full repo test suite**

```
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run go vet and gofmt**

```
go vet ./internal/modules/fs_library/... && gofmt -l internal/modules/fs_library/
```

Expected: no output from either (vet is silent, `gofmt -l` lists files needing formatting — should be empty).

- [ ] **Step 3: Check that `Run()` behaves correctly under the new ScanMode without breaking auto mode**

Eyeball-confirm the existing `TestRun*` tests still pass. If any tests previously relied on `m.scanCount` not existing (none should — it's brand new), update them.

```
go test ./internal/modules/fs_library/... -run "TestRun" -v
```

Expected: PASS.

- [ ] **Step 4: Manual smoke test (optional but recommended)**

If the user has a music folder available locally, build the binary and run with `scan_mode = "manual"`:

```
go build -o /tmp/mud ./cmd/mud
/tmp/mud --config <path-to-toml-with-manual-mode>
```

Verify in the logs that "scan_mode=manual: skipping initial scan" appears at startup. Trigger a rescan via the `library.rescan` MQTT command and confirm it runs.

This step is optional — pass/fail does not gate the plan, but it's the cheapest way to catch wiring bugs.

---

## Summary of Test Coverage

| Spec section | Tests |
|--------------|-------|
| Manual scan mode | `TestRunManualScanModeSkipsInitialScan` |
| Composer/embedded-genre extraction | `TestTagMetadataCarriesComposerAndGenre`, `TestMediaItemCarriesComposerAndEmbeddedGenre` |
| Sidecar `LLMGenre` round-trip | `TestAlbumMetadataLLMGenreRoundTrip` |
| Allowlist + parser | `TestParseGenreResponse`, `TestGenreAllowlistContents` |
| Rollup map | `TestRollupGenre`, `TestRollupGenreFromCandidates` |
| Ollama classifier | `TestBuildGenrePrompt`, `TestOllamaClassifierClassifyHappyPath`, `TestOllamaClassifierClassifyError`, `TestOllamaClassifierUnparseableResponse` |
| Backfill | `TestBackfillGenresOnlyMissing`, `TestBackfillGenresNoClassifierIsNoOp` |
| Genre index by LLMGenre + rollup fallback | `TestBuildGenreIndexUsesLLMGenre`, `TestBuildGenreIndexRollupFallback` |
| Search text expansion | `TestBuildSearchTextIncludesLLMGenreAndComposer`, `TestBuildSearchTextNilEnrichmentStillIncludesLocalFields` |
| Search scorer boosts | `TestSearchBoostsGenreAndComposerMatches` |

---

## Open issues / follow-ups (deliberately deferred)

- Per-track genre classification (album-only for now).
- User-overridable taxonomy (the 15 genres are hardcoded).
- A UI for fixing individual albums' genres.
- Re-classifying when a user edits embedded tags — only triggered by force-enrich rescan today.
- (resolved during plan-writing) `NewOllamaGenerator` takes `(endpoint, model string)`, not a config struct.
