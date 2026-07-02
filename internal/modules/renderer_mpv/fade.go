package renderermpv

import (
	"context"
	"sync"
	"time"
)

// fadeSteps is the number of discrete volume steps per fade ramp.
const fadeSteps = 20

// fadeJob holds the cancel handle for one in-flight fade goroutine. The
// driver tracks the set of live jobs so a new Play/Stop/Close can cancel
// all of them, not just the most recent one.
type fadeJob struct {
	cancel context.CancelFunc
}

// fadeSet tracks in-flight fade goroutines.
type fadeSet struct {
	mu   sync.Mutex
	jobs map[*fadeJob]struct{}
	wg   sync.WaitGroup
}

// start registers a new fade job and returns its cancellation context.
func (s *fadeSet) start() (context.Context, *fadeJob) {
	ctx, cancel := context.WithCancel(context.Background())
	job := &fadeJob{cancel: cancel}
	s.mu.Lock()
	if s.jobs == nil {
		s.jobs = make(map[*fadeJob]struct{})
	}
	s.jobs[job] = struct{}{}
	s.mu.Unlock()
	s.wg.Add(1)
	return ctx, job
}

// finish removes a fade job from the live set. Must be called exactly once
// per start(), from the fade goroutine.
func (s *fadeSet) finish(job *fadeJob) {
	s.mu.Lock()
	delete(s.jobs, job)
	s.mu.Unlock()
	job.cancel() // release ctx resources
	s.wg.Done()
}

// cancelAll signals every in-flight fade to wind up. Non-blocking.
func (s *fadeSet) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for job := range s.jobs {
		job.cancel()
	}
}

// wait blocks until all started fades have finished.
func (s *fadeSet) wait() {
	s.wg.Wait()
}

// runFade ramps from `from` to `to` over `duration`, calling set() for each
// step. On cancellation the final gain is applied immediately and runFade
// returns. Blocking; run it from a dedicated goroutine.
func runFade(ctx context.Context, duration time.Duration, from, to float64, set func(float64)) {
	if duration <= 0 {
		set(to)
		return
	}
	ticker := time.NewTicker(duration / fadeSteps)
	defer ticker.Stop()
	for i := 1; i <= fadeSteps; i++ {
		select {
		case <-ctx.Done():
			set(to)
			return
		case <-ticker.C:
		}
		set(from + (to-from)*(float64(i)/fadeSteps))
	}
}
