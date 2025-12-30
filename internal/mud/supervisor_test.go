package mud

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSupervisorRunsModules(t *testing.T) {
	logger := NewLogger("error")
	supervisor := Supervisor{Logger: logger}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	modules := []ModuleRunner{
		{
			Name: "test",
			Run: func(ctx context.Context) error {
				started <- struct{}{}
				<-ctx.Done()
				return nil
			},
		},
	}

	go func() {
		<-started
		cancel()
	}()

	if err := supervisor.Run(ctx, modules); err != nil {
		t.Fatalf("supervisor run: %v", err)
	}
}

func TestSupervisorPropagatesErrors(t *testing.T) {
	logger := NewLogger("error")
	supervisor := Supervisor{Logger: logger}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	modules := []ModuleRunner{
		{
			Name: "fail",
			Run: func(ctx context.Context) error {
				return errors.New("boom")
			},
		},
	}

	err := supervisor.Run(ctx, modules)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestSupervisorNoModules(t *testing.T) {
	logger := NewLogger("error")
	supervisor := Supervisor{Logger: logger}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := supervisor.Run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}
