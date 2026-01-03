package mud

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"go.uber.org/zap"
)

// ModuleRunner runs a module within the supervisor.
type ModuleRunner struct {
	Name string
	Run  func(ctx context.Context) error
}

// Supervisor manages module lifecycles.
type Supervisor struct {
	Logger          *zap.Logger
	ContinueOnError bool
}

// Run starts all module runners and waits for termination.
func (s Supervisor) Run(ctx context.Context, modules []ModuleRunner) error {
	if len(modules) == 0 {
		return fmt.Errorf("no modules enabled")
	}

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

	select {
	case <-ctx.Done():
		s.Logger.Info("shutdown requested")
	case err := <-errCh:
		return err
	}

	wg.Wait()
	return nil
}
