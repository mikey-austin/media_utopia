package mud

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DeployedStopTimeout mirrors the grace period the process supervisor gives
// mud between SIGTERM and SIGKILL. In the ansible deployment this is the mud
// container's stop_timeout (music-playbook-focusrite.yaml); systemd
// deployments would use TimeoutStopSec. It exists so DefaultShutdownTimeout
// can be checked against it rather than the two drifting independently.
const DeployedStopTimeout = 30 * time.Second

// DefaultShutdownTimeout is the maximum time to wait for modules to stop.
//
// This MUST stay strictly below DeployedStopTimeout with room to spare. When
// the two were both 30s (before 2026-08-12) a module that ignored context
// cancellation deadlocked the shutdown: mud's timeout and docker's SIGKILL
// deadline expired at the same instant, so the process was always killed
// before it could log which module hung, flush state, or exit cleanly. The
// remaining headroom covers unwinding and log flushing after Run returns.
const DefaultShutdownTimeout = 20 * time.Second

// ModuleRunner runs a module within the supervisor.
type ModuleRunner struct {
	Name string
	Run  func(ctx context.Context) error
}

// Supervisor manages module lifecycles.
type Supervisor struct {
	Logger          *zap.Logger
	ContinueOnError bool
	ShutdownTimeout time.Duration
}

// Run starts all module runners and waits for termination.
func (s Supervisor) Run(ctx context.Context, modules []ModuleRunner) error {
	if len(modules) == 0 {
		return fmt.Errorf("no modules enabled")
	}

	shutdownTimeout := s.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = DefaultShutdownTimeout
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(modules))

	// Track completion per module so a shutdown that overruns can name the
	// modules still running instead of reporting an anonymous timeout.
	// Indexed rather than keyed by name so duplicate module names can't mask
	// each other.
	var stoppedMu sync.Mutex
	stopped := make([]bool, len(modules))

	for i, module := range modules {
		idx, m := i, module
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				stoppedMu.Lock()
				stopped[idx] = true
				stoppedMu.Unlock()
			}()
			logger := s.Logger.With(zap.String("module", m.Name))
			logger.Info("starting module")
			defer func() {
				if r := recover(); r != nil {
					logger.Error("module panicked", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
					if !s.ContinueOnError {
						errCh <- fmt.Errorf("%s: panic: %v", m.Name, r)
					}
				}
			}()
			if err := m.Run(ctx); err != nil {
				logger.Error("module exited", zap.Error(err))
				if !s.ContinueOnError {
					errCh <- fmt.Errorf("%s: %w", m.Name, err)
				}
				return
			}
			logger.Info("module stopped")
		}()
	}

	var firstErr error
	select {
	case <-ctx.Done():
		s.Logger.Info("shutdown requested")
	case err := <-errCh:
		firstErr = err
		s.Logger.Error("module failure triggered shutdown", zap.Error(err))
		cancel()
	}

	// Wait for all modules to stop with timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	shutdownStart := time.Now()
	select {
	case <-done:
		s.Logger.Info("all modules stopped", zap.Duration("took", time.Since(shutdownStart)))
	case <-time.After(shutdownTimeout):
		// Name the offenders. Without this the process just disappears and
		// the only evidence is the supervisor's SIGKILL, which says nothing
		// about which module ignored cancellation.
		stoppedMu.Lock()
		var stuck []string
		for i, finished := range stopped {
			if !finished {
				stuck = append(stuck, modules[i].Name)
			}
		}
		stoppedMu.Unlock()

		s.Logger.Warn("shutdown timeout exceeded; exiting with modules still running",
			zap.Duration("timeout", shutdownTimeout),
			zap.Strings("stuck_modules", stuck))
	}

	// Drain any module errors that arrived during shutdown.
	for {
		select {
		case err := <-errCh:
			if firstErr == nil {
				firstErr = err
			}
		default:
			return firstErr
		}
	}
}
