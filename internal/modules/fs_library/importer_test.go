package fslibrary

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func fakeStarter(fn func(args []string) (string, error)) ytDlpStarter {
	return func(ctx context.Context, args ...string) (io.ReadCloser, func() error, error) {
		out, err := fn(args)
		if err != nil {
			return nil, nil, err
		}
		return io.NopCloser(strings.NewReader(out)), func() error { return nil }, nil
	}
}

func TestResolveImportDir(t *testing.T) {
	roots := []string{"/data/music", "/data/other"}
	if got, err := resolveImportDir("youtube", roots); err != nil || got != "/data/music/youtube" {
		t.Fatalf("relative: %q %v", got, err)
	}
	if got, err := resolveImportDir("/data/other/yt", roots); err != nil || got != "/data/other/yt" {
		t.Fatalf("absolute inside root: %q %v", got, err)
	}
	if _, err := resolveImportDir("/tmp/elsewhere", roots); err == nil {
		t.Fatal("absolute outside roots must error (files would never be scanned)")
	}
	if _, err := resolveImportDir("../escape", roots); err == nil {
		t.Fatal("relative escaping the root must error")
	}
}

func TestImportJobHappyPath(t *testing.T) {
	dir := t.TempDir()
	rescanned := atomic.Bool{}
	probe := `{"title": "Best Playlist", "entries": [{"id":"a"},{"id":"b"},{"id":"c"}]}`
	mgr := newImportManager(importManagerConfig{
		ImportDir: dir,
		Log:       zap.NewNop(),
		Rescan:    func() { rescanned.Store(true) },
		Starter: fakeStarter(func(args []string) (string, error) {
			if hasArg(args, "--flat-playlist") {
				return probe, nil
			}
			// Download run: two downloads, one archive skip.
			return dir + "/Best Playlist/01 - One.flac\n" +
				"[download] a: has already been recorded in the archive\n" +
				dir + "/Best Playlist/03 - Three.flac\n", nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.start(ctx)

	job, err := mgr.enqueue("https://youtube.com/playlist?list=x")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitJobState(t, mgr, job.JobID, "done")

	got := findJob(t, mgr, job.JobID)
	if got.Playlist != "Best Playlist" || got.Total != 3 || got.Done != 2 || got.Skipped != 1 || got.Failed != 0 {
		t.Fatalf("job counts: %+v", got)
	}
	if !rescanned.Load() {
		t.Fatal("expected rescan after successful job")
	}
}

func TestImportJobProbeFailure(t *testing.T) {
	mgr := newImportManager(importManagerConfig{
		ImportDir: t.TempDir(),
		Log:       zap.NewNop(),
		Rescan:    func() {},
		Starter: fakeStarter(func(args []string) (string, error) {
			return "", fmt.Errorf("yt-dlp: ERROR: not a playlist")
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.start(ctx)

	job, err := mgr.enqueue("https://youtube.com/playlist?list=bad")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitJobState(t, mgr, job.JobID, "failed")
	got := findJob(t, mgr, job.JobID)
	if got.Error == "" {
		t.Fatal("failed job must carry the error")
	}
}

func TestImportJobsNewestFirstCapped(t *testing.T) {
	mgr := newImportManager(importManagerConfig{
		ImportDir: t.TempDir(),
		Log:       zap.NewNop(),
		Rescan:    func() {},
		Starter: fakeStarter(func(args []string) (string, error) {
			if hasArg(args, "--flat-playlist") {
				return `{"title": "P", "entries": []}`, nil
			}
			return "", nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.start(ctx)

	var last string
	for i := 0; i < 25; i++ {
		job, err := mgr.enqueue(fmt.Sprintf("https://youtube.com/playlist?list=%d", i))
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		last = job.JobID
		waitJobState(t, mgr, job.JobID, "done")
	}
	jobs := mgr.list()
	if len(jobs) > maxImportJobs {
		t.Fatalf("job list not capped: %d", len(jobs))
	}
	if jobs[0].JobID != last {
		t.Fatalf("jobs not newest-first: %s vs %s", jobs[0].JobID, last)
	}
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func waitJobState(t *testing.T, mgr *importManager, jobID string, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if j := findJob(t, mgr, jobID); j.State == want {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("job %s never reached %s (now %s, err %q)", jobID, want, j.State, j.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func findJob(t *testing.T, mgr *importManager, jobID string) importJob {
	t.Helper()
	for _, j := range mgr.list() {
		if j.JobID == jobID {
			return j
		}
	}
	t.Fatalf("job %s not found", jobID)
	return importJob{}
}

func TestCleanPlaylistTitle(t *testing.T) {
	cases := map[string]string{
		"Album - Components":                 "Components",
		"Album - Ravel: The Piano Concertos": "Ravel: The Piano Concertos",
		"EP - Small Thing":                   "Small Thing",
		"Single - One Track":                 "One Track",
		"Mix - All Naruto Openings":          "All Naruto Openings",
		"My Normal Playlist":                 "My Normal Playlist",
		"Album -":                            "Album -", // not a prefix match, keep
	}
	for in, want := range cases {
		if got := cleanPlaylistTitle(in); got != want {
			t.Errorf("cleanPlaylistTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
