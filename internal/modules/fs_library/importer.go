package fslibrary

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"go.uber.org/zap"
)

// YouTube playlist import: one-shot, idempotent downloads of a playlist as
// FLAC + embedded art + tags into the library's import_dir, tracked by an
// in-memory job table. See docs/superpowers/specs/2026-07-03-youtube-import-design.md.

const (
	// maxImportJobs bounds the in-memory job history.
	maxImportJobs = 20
	// importProbeTimeout bounds the playlist metadata probe.
	importProbeTimeout = 2 * time.Minute
	// importJobTimeout bounds one full playlist download.
	importJobTimeout = 2 * time.Hour
	// importQueueDepth bounds queued (not yet started) jobs.
	importQueueDepth = 16
)

// importJob is one import's public state, embedded verbatim in the
// library.imports reply.
type importJob struct {
	JobID      string `json:"jobId"`
	URL        string `json:"url"`
	Playlist   string `json:"playlist,omitempty"`
	State      string `json:"state"` // queued | running | done | failed
	Done       int    `json:"done"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	Total      int    `json:"total"`
	StartedAt  int64  `json:"startedAt,omitempty"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ytDlpStarter launches yt-dlp and returns its stdout stream plus a wait
// function reporting the exit result. Tests substitute fakes; production
// uses execYtDlp.
type ytDlpStarter func(ctx context.Context, args ...string) (io.ReadCloser, func() error, error)

// importManagerConfig wires an importManager.
type importManagerConfig struct {
	ImportDir string // absolute destination (already validated)
	Log       *zap.Logger
	Rescan    func()       // called after a job lands new files
	Starter   ytDlpStarter // yt-dlp launcher
}

// importManager owns the job table and the single worker goroutine that
// runs downloads serially (one yt-dlp process at a time).
type importManager struct {
	cfg   importManagerConfig
	idGen idgen.Generator

	mu   sync.Mutex
	jobs []*importJob // newest first

	queue chan *importJob
}

func newImportManager(cfg importManagerConfig) *importManager {
	if cfg.Log == nil {
		cfg.Log = zap.NewNop()
	}
	return &importManager{cfg: cfg, queue: make(chan *importJob, importQueueDepth)}
}

// start launches the worker goroutine; it exits when ctx is cancelled.
func (m *importManager) start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-m.queue:
				m.runJob(ctx, job)
			}
		}
	}()
}

// enqueue registers a new job and queues it for the worker.
func (m *importManager) enqueue(url string) (importJob, error) {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return importJob{}, errors.New("url must be http(s)")
	}
	job := &importJob{
		JobID: m.idGen.NewID(),
		URL:   url,
		State: "queued",
	}
	m.mu.Lock()
	m.jobs = append([]*importJob{job}, m.jobs...)
	if len(m.jobs) > maxImportJobs {
		m.jobs = m.jobs[:maxImportJobs]
	}
	m.mu.Unlock()

	select {
	case m.queue <- job:
		return *job, nil
	default:
		m.update(job, func(j *importJob) {
			j.State = "failed"
			j.Error = "import queue full"
		})
		return importJob{}, errors.New("import queue full")
	}
}

// list returns a snapshot of the job table, newest first.
func (m *importManager) list() []importJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]importJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, *j)
	}
	return out
}

func (m *importManager) update(job *importJob, fn func(*importJob)) {
	m.mu.Lock()
	fn(job)
	m.mu.Unlock()
}

// runJob executes one import: probe, download, cover art, rescan.
func (m *importManager) runJob(ctx context.Context, job *importJob) {
	defer func() {
		if r := recover(); r != nil {
			m.cfg.Log.Error("import job panicked", zap.Any("panic", r))
			m.update(job, func(j *importJob) {
				j.State = "failed"
				j.Error = fmt.Sprintf("internal error: %v", r)
				j.FinishedAt = time.Now().Unix()
			})
		}
	}()
	m.update(job, func(j *importJob) {
		j.State = "running"
		j.StartedAt = time.Now().Unix()
	})
	fail := func(err error) {
		m.cfg.Log.Warn("import failed", zap.String("url", job.URL), zap.Error(err))
		m.update(job, func(j *importJob) {
			j.State = "failed"
			j.Error = err.Error()
			j.FinishedAt = time.Now().Unix()
		})
	}

	title, total, thumbnail, err := m.probe(ctx, job.URL)
	if err != nil {
		fail(err)
		return
	}
	if title == "" {
		title = "Unknown Playlist"
	}
	m.update(job, func(j *importJob) {
		j.Playlist = title
		j.Total = total
	})

	albumDir := filepath.Join(m.cfg.ImportDir, safeImportName(title))
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		fail(fmt.Errorf("create %s: %w", albumDir, err))
		return
	}

	done, skipped, err := m.download(ctx, job, albumDir)
	m.update(job, func(j *importJob) {
		j.Done = done
		j.Skipped = skipped
		j.Failed = max(0, j.Total-done-skipped)
	})
	if err != nil && done == 0 && skipped == 0 {
		fail(err)
		return
	}

	m.fetchCover(ctx, albumDir, thumbnail)
	cleanThumbnailSidecars(albumDir)

	m.update(job, func(j *importJob) {
		j.State = "done"
		j.FinishedAt = time.Now().Unix()
	})
	m.cfg.Log.Info("import complete",
		zap.String("playlist", title),
		zap.Int("downloaded", done),
		zap.Int("skipped", skipped),
		zap.Int("failed", job.Failed))
	if done > 0 && m.cfg.Rescan != nil {
		m.cfg.Rescan()
	}
}

// probe fetches the playlist title, entry count, and a representative
// thumbnail without downloading anything.
func (m *importManager) probe(ctx context.Context, url string) (title string, total int, thumbnail string, err error) {
	ctx, cancel := context.WithTimeout(ctx, importProbeTimeout)
	defer cancel()
	stdout, wait, err := m.cfg.Starter(ctx, "--flat-playlist", "-J", "--no-warnings", url)
	if err != nil {
		return "", 0, "", err
	}
	data, readErr := io.ReadAll(stdout)
	_ = stdout.Close()
	if wait != nil {
		if werr := wait(); werr != nil && len(bytes.TrimSpace(data)) == 0 {
			return "", 0, "", werr
		}
	}
	if readErr != nil && len(bytes.TrimSpace(data)) == 0 {
		return "", 0, "", readErr
	}

	var payload struct {
		Title      string `json:"title"`
		Thumbnail  string `json:"thumbnail"`
		Thumbnails []struct {
			URL string `json:"url"`
		} `json:"thumbnails"`
		Entries []struct {
			Thumbnails []struct {
				URL string `json:"url"`
			} `json:"thumbnails"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &payload); err != nil {
		return "", 0, "", fmt.Errorf("parse playlist metadata: %w", err)
	}
	title = payload.Title
	total = len(payload.Entries)
	if total == 0 {
		// A single-video URL probes as an object without entries.
		total = 1
	}
	thumbnail = payload.Thumbnail
	if thumbnail == "" && len(payload.Thumbnails) > 0 {
		thumbnail = payload.Thumbnails[len(payload.Thumbnails)-1].URL
	}
	if thumbnail == "" && len(payload.Entries) > 0 && len(payload.Entries[0].Thumbnails) > 0 {
		ts := payload.Entries[0].Thumbnails
		thumbnail = ts[len(ts)-1].URL
	}
	return title, total, thumbnail, nil
}

// download runs the yt-dlp download pass, streaming stdout to count
// completed files and archive skips as they happen.
func (m *importManager) download(ctx context.Context, job *importJob, albumDir string) (done int, skipped int, err error) {
	ctx, cancel := context.WithTimeout(ctx, importJobTimeout)
	defer cancel()
	args := []string{
		"-x", "--audio-format", "flac", "--audio-quality", "0",
		"--embed-metadata", "--embed-thumbnail",
		"--parse-metadata", "playlist_index:%(track_number)s",
		// Album = playlist title, falling back to the video title for
		// single-video URLs (otherwise those land in "Unknown Album").
		"--parse-metadata", "%(playlist_title,title)s:%(meta_album)s",
		"--ignore-errors", "--no-overwrites", "--no-progress", "--no-warnings",
		// --print implies --quiet; without --no-quiet the "already recorded
		// in the archive" lines never reach stdout and skips read as failures.
		"--no-quiet",
		"--download-archive", filepath.Join(albumDir, ".yt-archive"),
		"--print", "after_move:filepath",
		"-o", filepath.Join(albumDir, "%(playlist_index|0)02d - %(title)s.%(ext)s"),
		job.URL,
	}
	stdout, wait, err := m.cfg.Starter(ctx, args...)
	if err != nil {
		return 0, 0, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, string(filepath.Separator)):
			done++
			m.update(job, func(j *importJob) { j.Done = done })
		case strings.Contains(line, "has already been recorded in the archive"):
			skipped++
			m.update(job, func(j *importJob) { j.Skipped = skipped })
		}
	}
	_ = stdout.Close()
	if wait != nil {
		if werr := wait(); werr != nil {
			return done, skipped, werr
		}
	}
	return done, skipped, scanner.Err()
}

// fetchCover writes cover.jpg from the playlist thumbnail when the album
// directory has no cover art yet. Best-effort: failures are logged only.
func (m *importManager) fetchCover(ctx context.Context, albumDir string, thumbnail string) {
	if thumbnail == "" || findCoverArt(albumDir) != "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, thumbnail, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		m.cfg.Log.Debug("cover fetch failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(albumDir, "cover.jpg"), data, 0o644); err != nil {
		m.cfg.Log.Debug("cover write failed", zap.Error(err))
	}
}

// execYtDlp is the production ytDlpStarter: it launches the binary with
// stderr captured (tail surfaces in error messages).
func execYtDlp(binary string) ytDlpStarter {
	return func(ctx context.Context, args ...string) (io.ReadCloser, func() error, error) {
		cmd := exec.CommandContext(ctx, binary, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		if err := cmd.Start(); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return nil, nil, fmt.Errorf("yt-dlp not found on library host (%s)", binary)
			}
			return nil, nil, err
		}
		wait := func() error {
			if err := cmd.Wait(); err != nil {
				return fmt.Errorf("yt-dlp: %w: %s", err, stderrTail(stderr.String()))
			}
			return nil
		}
		return stdout, wait, nil
	}
}

// stderrTail keeps error messages readable: the last few non-empty lines.
func stderrTail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	keep := make([]string, 0, 3)
	for i := len(lines) - 1; i >= 0 && len(keep) < 3; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			keep = append([]string{l}, keep...)
		}
	}
	return strings.Join(keep, " | ")
}

// safeImportName sanitizes a playlist title into a directory component.
func safeImportName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.Trim(name, "."))
	if name == "" {
		return "Unknown Playlist"
	}
	return name
}

// cleanThumbnailSidecars removes the intermediate thumbnail files yt-dlp
// leaves next to tracks after embedding (keeping cover.*, which the album
// grid uses).
func cleanThumbnailSidecars(albumDir string) {
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		return
	}
	flacs := map[string]bool{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".flac") {
			flacs[strings.TrimSuffix(e.Name(), ".flac")] = true
		}
	}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".png" && ext != ".webp" && ext != ".jpg" && ext != ".jpeg" {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if strings.HasPrefix(strings.ToLower(e.Name()), "cover.") {
			continue
		}
		if flacs[base] {
			_ = os.Remove(filepath.Join(albumDir, e.Name()))
		}
	}
}

// resolveImportDir validates the import_dir config value and returns the
// absolute destination. Relative values resolve under the first root;
// absolute values must lie inside one of the roots — anything else would
// download files the scanner can never see.
func resolveImportDir(importDir string, roots []string) (string, error) {
	importDir = strings.TrimSpace(importDir)
	if importDir == "" {
		importDir = "youtube"
	}
	if len(roots) == 0 || strings.TrimSpace(roots[0]) == "" {
		return "", errors.New("import_dir requires at least one library root")
	}
	var abs string
	if filepath.IsAbs(importDir) {
		abs = filepath.Clean(importDir)
	} else {
		abs = filepath.Clean(filepath.Join(strings.TrimSpace(roots[0]), importDir))
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("import_dir %q is outside the library roots %v (imported files would never be scanned)", importDir, roots)
}
