package renderermpv

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRunFadeReachesTarget(t *testing.T) {
	var mu sync.Mutex
	var got []float64
	runFade(context.Background(), 50*time.Millisecond, 0, 1, func(g float64) {
		mu.Lock()
		got = append(got, g)
		mu.Unlock()
	})
	mu.Lock()
	defer mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("expected multiple steps, got %d", len(got))
	}
	if got[len(got)-1] != 1 {
		t.Fatalf("final gain = %v, want 1", got[len(got)-1])
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("gain not monotonic: %v", got)
		}
	}
}

func TestRunFadeCancelAppliesFinal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var last float64 = -1
	start := time.Now()
	runFade(ctx, 10*time.Second, 1, 0, func(g float64) { last = g })
	if time.Since(start) > time.Second {
		t.Fatal("cancelled fade did not return promptly")
	}
	if last != 0 {
		t.Fatalf("cancelled fade must apply final gain, got %v", last)
	}
}

func TestRunFadeZeroDuration(t *testing.T) {
	var last float64 = -1
	runFade(context.Background(), 0, 0, 1, func(g float64) { last = g })
	if last != 1 {
		t.Fatalf("zero-duration fade must apply final gain, got %v", last)
	}
}

func TestFadeSetCancelAll(t *testing.T) {
	var fs fadeSet
	release := make(chan struct{})
	for i := 0; i < 3; i++ {
		ctx, job := fs.start()
		go func() {
			defer fs.finish(job)
			select {
			case <-ctx.Done():
			case <-release:
			}
		}()
	}
	fs.cancelAll()
	done := make(chan struct{})
	go func() { fs.wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fadeSet.wait did not return after cancelAll")
	}
	close(release)
}
