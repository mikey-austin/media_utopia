# fs_library Stability, Speed & Search Overhaul

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox syntax.

**Goal:** Make fs_library (venus deployment) stable (no crashes/hangs), fast (cheap scans, fast metadata), with working search, dedup, and enrichment.

**Root causes (all verified against code + venus evidence):**

| # | Symptom | Root cause | Evidence |
|---|---------|-----------|----------|
| 1 | Crashes | `m.artURLCache` map written under `RLock` from 4 concurrent command workers → `fatal: concurrent map writes` | module.go:3206 vs RLock-only callers; cache wiped every scan widens window |
| 2 | Crash amplification | No `recover()` in any of the 6 detached goroutines | grep: zero recover() in package |
| 3 | Churn: 580 items re-parse + rehash every 15min, duplicate "original" flaps | Zero-byte files (exactly 580 on venus) never pass the `Size() > 0` reuse gate; all share one file-hash → giant phantom dup group, random original per scan (map order) | venus `find -size 0` = 580; scan logs `new: 580, removed: 580` every scan |
| 4 | Dedup "doesn't work" | `dedupe_policy = "prefer"` invalid but silently accepted (only logs); `best` policy defined but never implemented; `first` nondeterministic (map order) | repair.go policies vs scanInner:2539 |
| 5 | 34s startup rescan + full rehash I/O | `loadIndex` restores `m.index` but not `m.prevItems` (incremental-scan state) | module.go:3234; venus log `reused: 0` after restart |
| 6 | `scan_mode = "manual"` ignored; `genre_model` unusable | mud's FSLibraryConfig/wiring lacks ScanMode + GenreModel fields | internal/mud/config.go; cmd/mud/main.go:302 |
| 7 | Search: exact matches never surface | Keyword scorer (only place with exact/prefix boosts) unreachable — semantic early-returns when non-empty | module.go:1716-1722 |
| 8 | Search: poor semantic recall | mxbai-embed-large query embedded without its required retrieval-instruction prefix; docs embedded as structured cards | module.go:1865, embedding.go:711 |
| 9 | Search: 10s stalls | Per-keystroke uncached HTTP embed to titan, 10s ctx | module.go:1856 |
| 10 | Search: missing/unstable results | 1000-item cap applied before scoring over random map order; no diacritic folding; dupes never filtered; semantic total truncated by top-k | module.go:1731,1834; 2212; 1881 |
| 11 | Metadata "doesn't work": 949/2133 albums unenriched | mud-library image built CGO=0 without chromaprint → AcoustID fingerprint fallback compiled out (API key set but inert); MB-text-miss albums negative-cached | venus log warning; enrichment.go:1147 |
| 12 | Latent: failing LLM backfills retry every 15min forever | Sidecar only written on success; candidates recomputed each scan; serial 120s calls | enrichment.go:1479-1551 (currently idle on venus, candidates=0) |
| 13 | Steady-state waste | artURLCache wiped + full O(n) buildEmbeddings pass every scan even when library unchanged | module.go:2697, 2701 |

## Tasks

1. **Crash fix**: dedicated `artURLMu sync.Mutex` for artURLCache; race test (`go test -race` with concurrent browses during rescan).
2. **goSafe**: `m.goSafe(name, fn)` wrapper (deferred recover + error log) for all detached goroutines; panic-containment test.
3. **Zero-byte/unreadable guard**: skip `size == 0` in the walk's new-file branch; count + WARN once per scan with sample paths. Test with temp tree.
4. **Deterministic dedup + implement `best`**: sort dup group members (path asc) so original is stable; implement `best` (largest file wins, path tiebreak) as scan-time canonical selection: non-canonical dups hidden from browse/search (not deleted from index — resolvable by ID for existing playlists). `first` = path-asc first. Config validation: reject unknown dedupe/repair/index/scan-mode values at NewModule.
5. **Warm restarts**: loadIndex rebuilds `m.prevItems` from loaded items. Test: save → new module → load → scan reuses everything.
6. **Cheap no-op scans**: when `newCount == 0 && removedCount == 0`: keep artURLCache, skip buildEmbeddings launch (enrich/backfill paths still trigger their own rebuilds).
7. **Backfill cooldown**: persist `summary_attempted_at`/`genre_attempted_at` in sidecar on failure; skip candidates attempted < 24h ago.
8. **Search overhaul** (hybrid):
   - Always run keyword scorer; merge with semantic by ID (keyword exact/prefix boosts dominate; semantic fills tail), stable sort, paginate after merge.
   - Remove pre-score cap (collect all matches, then sort; 16k is cheap). Deterministic order.
   - mxbai query prefix (`Represent this sentence for searching relevant passages: `) applied in ollama provider for query embeds when model has "mxbai" prefix.
   - Query-embedding LRU cache (256) + 3s query-embed timeout (keyword results still return instantly when titan is slow).
   - ASCII diacritic folding (no new deps) in buildSearchText + query normalization.
   - Filter non-canonical duplicates from search + browse album tracks when dedupe active.
   - Fix enrich-key fallback mismatch ("Unknown Artist"/"Unknown Album") in scorer + buildEmbeddings.
   - Semantic searchLimit → `start+count+200`.
9. **mud config plumbing**: add `scan_mode`, `genre_model` to FSLibraryConfig + wiring + parse test.
10. **Packaging**: mud-library image gains chromaprint: build CGO=1, tags `chromaprint`; runtime = ubuntu:26.04 slim layer with `libchromaprint1` + ca-certificates (replaces distroless static for this target).
11. **Deploy venus**: push image; venus-playbook mud_image bump; template mud.toml: `dedupe_policy = "best"`, scan_mode → `"auto"` (scans are cheap now; manual was a workaround), keep acoustid key (now effective).
12. **Verify live**: no churn in scan logs, stable dup originals, warm restart < 2s, search returns exact matches first (mu CLI), fingerprint enrichment progresses on the 949 no-metadata albums. Report the 580 zero-byte files to Mikey for cleanup (list saved to venus:/var/lib/mud/zero-byte-files.txt).

Constraints: no new Go deps; all pure logic unit-tested; `-race` on fs_library tests; commit per task.
