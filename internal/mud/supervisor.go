package mud

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DefaultShutdownTimeout is the maximum time to wait for modules to stop.
const DefaultShutdownTimeout = 30 * time.Second

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

	for _, module := range modules {
		m := module
		wg.Add(1)
		go func() {
			defer wg.Done()
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

	select {
	case <-done:
		s.Logger.Info("all modules stopped")
	case <-time.After(shutdownTimeout):
		s.Logger.Warn("shutdown timeout exceeded, some modules may not have stopped gracefully",
			zap.Duration("timeout", shutdownTimeout))
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
