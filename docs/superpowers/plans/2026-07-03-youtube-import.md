# YouTube Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.
> Spec: docs/superpowers/specs/2026-07-03-youtube-import-design.md

**Goal:** `mu lib import <url>` downloads a YouTube playlist as FLAC+art+tags into fs_library's `import_dir` asynchronously; `mu lib imports` shows job progress.

## Tasks

1. **Importer core** (`internal/modules/fs_library/importer.go` + `importer_test.go`):
   `importJob` struct + `importManager` (job table, serial queue channel, goSafe worker), yt-dlp probe (`--flat-playlist -J`) + download run with injectable runner `func(ctx, args...) (io.ReadCloser, func() error, error)`-style (streaming stdout), line parsing (filepath → Done++, "already been recorded" → Skipped++), cover.jpg fetch, rescan trigger via callback. Unit tests with fake runner covering: happy path counts, failed-probe job, archive-skip counts, import_dir validation (relative/absolute/outside-root).
2. **Module wiring**: Config `ImportDir`/`YtDlpPath` (+ validation in NewModule), dispatch cases `library.import`/`library.imports`, presence cap `import: true`, manager started in Run and stopped with ctx. Handler tests.
3. **mud plumbing**: FSLibraryConfig `import_dir`/`yt_dlp_path` + main.go passthrough + config test.
4. **CLI**: `core.Service.LibraryImport/LibraryImports` + results, `mu lib import` / `mu lib imports` commands, imports table renderer. Tests for renderer.
5. **Packaging**: mud-library image gains python3+pip yt-dlp; build+push.
6. **Deploy + acceptance**: venus playbook run (image pull), live import of a small real playlist, verify browse/search, idempotent re-run, `mu lib imports` progress; update memory.

Constraints: no new Go deps; importer code isolated in importer.go; all yt-dlp calls behind the injectable runner; commit per task.
