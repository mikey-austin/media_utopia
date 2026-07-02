# mu CLI Usability & Aesthetics Overhaul

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Make `mu` world-class in usability and aesthetics: clean, consistent, width-aware output; forgiving selectors; helpful errors; script-friendly JSON and piping.

**Observed defects (from live use):**
- pterm tables emit doubled ANSI per cell and ` | ` separators; colors leak into pipes (no TTY detection); `|` in cell values is mangled to `/`.
- Fixed truncation widths (64/32/40) ignore the terminal: overflow-wrap on narrow, wasted space on wide; IDs shown full-width and shouty.
- Inconsistent visual language: box for status, pipe-tables for lists, bare printf blocks for session/resolve; footers differ ("Showing 1-3 of 3 items" vs "(rev 4)").
- Runtime errors dump the full cobra usage block ("container not found" → 10 lines of flags).
- Selectors need exact name match; "no match" errors don't list what exists; empty selector with several nodes says just "selector required".
- `lib search venus music warning sign` fails — first arg must be the library, rest must be ONE quoted arg.
- `--json` wraps payloads in a capitalized `{"Data": ...}` envelope; other results emit Go-cased keys.

## Design decisions

- **One table engine** (`internal/adapters/output/table.go`): columns declare min width, weight (flex shrink/grow), alignment, and style (e.g. dim for IDs, right-align durations). Width budget = terminal width (fallback 100, overridable for tests). Two-space gutters, no vertical separators. Headers dim+bold. Truncation uses `…` and never applies to ID or duration columns (copy-paste safety) — flexible text columns absorb the squeeze.
- **Color discipline** (`style.go`): colors only when stdout is a TTY AND NO_COLOR unset AND CLICOLOR != 0 AND !--no-color. Single palette: headers dim-bold, IDs dim, current-row green, status colors (playing green / paused yellow / stopped red), footers dim.
- **Detail views**: aligned `Label:` (dim) + value blocks for session/resolve/rescan.
- **Footers**: consistent `N–M of T · --offset M for more` (dim), only when relevant.
- **Errors**: `SilenceUsage`+`SilenceErrors` on root; print `mu: <message>` (red "mu:" when TTY) to stderr; usage shown only for cobra arg/flag parse errors. Not-found selector errors list available node names.
- **Resolver**: exact name/ID → alias → unique case-insensitive prefix → unique substring. Ambiguity lists candidates; empty selector with multiple candidates lists them too.
- **Search args**: `mu lib search [library] <query...>` — if the first arg resolves to a library, the rest is the query; otherwise ALL args join into the query. Multi-word queries need no quotes.
- **JSON**: `RawResult`/library payloads emit the payload itself (no `Data` envelope); core result structs get camelCase json tags.

## Tasks

1. `style.go` + `table.go` engine with unit tests (width allocation, truncation, alignment, color on/off).
2. Convert all human renderers (nodes, queue, playlists, snapshots, suggestions, library items, session, resolve, status box) to the engine; consistent footers/empty states. Golden-ish tests with color off + fixed width.
3. Error UX: silence cobra usage on runtime errors, styled stderr errors, keep exit codes; not-found errors carry candidate lists.
4. Resolver prefix/substring matching + candidate-listing errors + tests.
5. `lib search` greedy query args (+ same for `resolve`); tests.
6. JSON output: unwrap payload envelopes; add json tags to core results; tests.
7. Live verification against venus/mars (ls, status, queue, browse, search; piped vs TTY).
