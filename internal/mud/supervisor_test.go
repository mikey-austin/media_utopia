package mud

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestModuleLoggerFactory(t *testing.T) {
	factory := NewModuleLoggerFactory(LogConfig{Level: "info"})
	if factory == nil {
		t.Fatalf("expected non-nil factory")
	}

	base := factory.Logger()
	if base == nil {
		t.Fatalf("expected non-nil base logger")
	}

	mod := factory.ModuleLogger("test_module")
	if mod == nil {
		t.Fatalf("expected non-nil module logger")
	}
}

func TestModuleLoggerLevelOverride(t *testing.T) {
	factory := NewModuleLoggerFactory(LogConfig{
		Level: "debug",
		ModuleLevels: map[string]string{
			"noisy": "error",
		},
	})

	// The base logger should allow info-level logging.
	base := factory.Logger()
	if !base.Core().Enabled(zapcore.InfoLevel) {
		t.Fatalf("base logger should allow info level")
	}

	// A module with no override should inherit the base (debug) level.
	normal := factory.ModuleLogger("normal")
	if !normal.Core().Enabled(zapcore.DebugLevel) {
		t.Fatalf("normal module should allow debug level")
	}

	// A module with an "error" override should NOT log info or debug.
	noisy := factory.ModuleLogger("noisy")
	if noisy.Core().Enabled(zapcore.InfoLevel) {
		t.Fatalf("noisy module with error override should not allow info level")
	}
	if noisy.Core().Enabled(zapcore.DebugLevel) {
		t.Fatalf("noisy module with error override should not allow debug level")
	}
	if !noisy.Core().Enabled(zapcore.ErrorLevel) {
		t.Fatalf("noisy module with error override should allow error level")
	}
}

func TestSupervisorRunsModules(t *testing.T) {
	logger := NewLogger(LogConfig{Level: "error"})
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
	logger := NewLogger(LogConfig{Level: "error"})
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
	logger := NewLogger(LogConfig{Level: "error"})
	supervisor := Supervisor{Logger: logger}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := supervisor.Run(ctx, nil); err == nil {
		t.Fatalf("expected error")
	}
}

// TestSupervisorDrainsErrorsOnShutdown verifies that when a module errors
// during shutdown (after ctx is canceled), the error is drained and returned.
// The previous bug returned nil when ctx was canceled, discarding any errors
// that arrived in errCh during the shutdown window.
func TestSupervisorDrainsErrorsOnShutdown(t *testing.T) {
	logger := NewLogger(LogConfig{Level: "error"})
	supervisor := Supervisor{
		Logger:          logger,
		ShutdownTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	moduleErr := errors.New("module failed")

	modules := []ModuleRunner{
		{
			Name: "failing",
			Run: func(ctx context.Context) error {
				// Wait for context cancellation, then return an error.
				// This simulates a module that fails during shutdown.
				<-ctx.Done()
				return moduleErr
			},
		},
		{
			Name: "slow-stopper",
			Run: func(ctx context.Context) error {
				// This module also waits for cancellation but takes
				// longer to stop, ensuring the supervisor enters the
				// wg.Wait/drain path (ctx.Done wins the select) and
				// the failing module's error lands in errCh before
				// the drain loop runs.
				<-ctx.Done()
				time.Sleep(100 * time.Millisecond)
				return nil
			},
		},
	}

	// Cancel the context after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := supervisor.Run(ctx, modules)
	if err == nil {
		t.Fatalf("expected error from failing module, got nil")
	}
	if !errors.Is(err, moduleErr) {
		t.Fatalf("expected wrapped module error, got: %v", err)
	}
}

// TestSupervisorContinueOnErrorKeepsRunning verifies that when ContinueOnError
// is true, a module failure is logged but the supervisor continues running
// other modules. The supervisor should only return when the context is
// cancelled, not when a single module fails.
func TestSupervisorContinueOnErrorKeepsRunning(t *testing.T) {
	logger := NewLogger(LogConfig{Level: "error"})
	supervisor := Supervisor{
		Logger:          logger,
		ContinueOnError: true,
		ShutdownTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	healthyStarted := make(chan struct{}, 1)
	healthyStopped := make(chan struct{}, 1)
	failedAt := make(chan time.Time, 1)
	runReturned := make(chan error, 1)

	modules := []ModuleRunner{
		{
			Name: "failing",
			Run: func(ctx context.Context) error {
				failedAt <- time.Now()
				return errors.New("module crash")
			},
		},
		{
			Name: "healthy",
			Run: func(ctx context.Context) error {
				healthyStarted <- struct{}{}
				<-ctx.Done()
				healthyStopped <- struct{}{}
				return nil
			},
		},
	}

	go func() {
		runReturned <- supervisor.Run(ctx, modules)
	}()

	// Wait for the failing module to have failed
	select {
	case <-failedAt:
	case <-time.After(time.Second):
		t.Fatalf("failing module did not run")
	}

	// Wait for the healthy module to have started
	select {
	case <-healthyStarted:
	case <-time.After(time.Second):
		t.Fatalf("healthy module did not start")
	}

	// Supervisor should NOT have returned yet -- the healthy module is still running.
	select {
	case err := <-runReturned:
		t.Fatalf("supervisor returned prematurely with err=%v", err)
	case <-time.After(100 * time.Millisecond):
		// Good: supervisor is still running
	}

	// Now cancel the context to shut everything down
	cancel()

	// The healthy module should stop
	select {
	case <-healthyStopped:
	case <-time.After(time.Second):
		t.Fatalf("healthy module did not stop after cancel")
	}

	// Supervisor should return without error (ContinueOnError swallows module errors)
	select {
	case err := <-runReturned:
		if err != nil {
			t.Fatalf("expected nil error with ContinueOnError, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("supervisor did not return after cancel")
	}
}

// TestDefaultShutdownTimeoutLeavesHeadroomUnderDockerStop pins the invariant
// that mud's own shutdown deadline fires strictly before its process
// supervisor gives up and SIGKILLs it.
//
// Regression test for the 2026-08-12 incident: the deployment sets docker
// stop_timeout=30 and DefaultShutdownTimeout was also exactly 30s, so the two
// deadlines expired simultaneously. mud could never reach its own "give up and
// exit" path — docker won the photo finish every time and SIGKILLed the
// process, discarding the in-memory renderer queue mid-stream.
func TestDefaultShutdownTimeoutLeavesHeadroomUnderDockerStop(t *testing.T) {
	if DefaultShutdownTimeout >= DeployedStopTimeout {
		t.Fatalf("DefaultShutdownTimeout (%v) must be strictly less than the deployed "+
			"stop timeout (%v), otherwise the process is SIGKILLed before it can "+
			"finish its own graceful shutdown", DefaultShutdownTimeout, DeployedStopTimeout)
	}

	// Headroom must be enough for the process to actually unwind and flush
	// after the supervisor returns, not merely non-zero.
	if headroom := DeployedStopTimeout - DefaultShutdownTimeout; headroom < 5*time.Second {
		t.Fatalf("only %v of headroom between DefaultShutdownTimeout (%v) and the "+
			"deployed stop timeout (%v); want at least 5s", headroom,
			DefaultShutdownTimeout, DeployedStopTimeout)
	}
}

// TestSupervisorNamesModulesStuckOnShutdown verifies that when the shutdown
// deadline expires, the supervisor names the specific modules that ignored
// context cancellation.
//
// The old warning ("some modules may not have stopped gracefully") identified
// nobody, which is why the 2026-08-12 SIGKILL left no evidence of which module
// hung. Diagnosing it required reading docker's json log as root; the log
// should name the culprit on its own.
func TestSupervisorNamesModulesStuckOnShutdown(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	supervisor := Supervisor{
		Logger:          zap.New(core),
		ShutdownTimeout: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release := make(chan struct{})
	defer close(release)
	obedientStopped := make(chan struct{})

	modules := []ModuleRunner{
		{
			Name: "obedient",
			Run: func(ctx context.Context) error {
				<-ctx.Done()
				close(obedientStopped)
				return nil
			},
		},
		{
			Name: "wedged",
			Run: func(ctx context.Context) error {
				// Deliberately ignores ctx — this is the behaviour the
				// supervisor must report by name.
				<-release
				return nil
			},
		},
	}

	runReturned := make(chan error, 1)
	go func() { runReturned <- supervisor.Run(ctx, modules) }()

	cancel()

	select {
	case <-obedientStopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("obedient module did not stop after cancel")
	}

	select {
	case err := <-runReturned:
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("supervisor did not return after shutdown timeout elapsed")
	}

	entries := logs.All()
	if len(entries) == 0 {
		t.Fatalf("expected a warning naming the stuck module, got no warnings")
	}

	// zap.Strings encodes as an ArrayMarshaler, so read it back through
	// ContextMap rather than asserting on the raw field type.
	var stuck []any
	found := false
	for _, entry := range entries {
		value, ok := entry.ContextMap()["stuck_modules"]
		if !ok {
			continue
		}
		names, ok := value.([]any)
		if !ok {
			t.Fatalf("stuck_modules is %T, want []any", value)
		}
		stuck, found = names, true
	}

	if !found {
		t.Fatalf("no warning carried a stuck_modules field; got %d warnings", len(entries))
	}
	if len(stuck) != 1 || stuck[0] != "wedged" {
		t.Fatalf("expected stuck_modules == [wedged], got %v", stuck)
	}
}

// TestSupervisorReportsNoStuckModulesOnCleanShutdown guards the other
// direction: a clean shutdown must not emit a spurious stuck-module warning,
// or the diagnostic becomes noise nobody reads.
func TestSupervisorReportsNoStuckModulesOnCleanShutdown(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	supervisor := Supervisor{
		Logger:          zap.New(core),
		ShutdownTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	modules := []ModuleRunner{
		{
			Name: "tidy",
			Run: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
		},
	}

	runReturned := make(chan error, 1)
	go func() { runReturned <- supervisor.Run(ctx, modules) }()

	cancel()

	select {
	case err := <-runReturned:
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("supervisor did not return after clean shutdown")
	}

	for _, entry := range logs.All() {
		for _, field := range entry.Context {
			if field.Key == "stuck_modules" {
				t.Fatalf("clean shutdown emitted a stuck-module warning: %v", entry.Message)
			}
		}
	}
}

// TestSupervisorGracefulShutdown verifies that when ContinueOnError is false
// (the default) and a module fails, the supervisor cancels the context so
// other running modules receive cancellation, waits for them to finish, and
// then returns the error. This tests the BUG-GM-4 fix ensuring graceful
// shutdown propagates cancellation to sibling modules.
func TestSupervisorGracefulShutdown(t *testing.T) {
	logger := NewLogger(LogConfig{Level: "error"})
	supervisor := Supervisor{
		Logger:          logger,
		ContinueOnError: false,
		ShutdownTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	moduleErr := errors.New("fatal crash")
	siblingCancelled := make(chan struct{}, 1)
	siblingStarted := make(chan struct{}, 1)

	modules := []ModuleRunner{
		{
			Name: "failing",
			Run: func(ctx context.Context) error {
				// Wait for sibling to start before failing, so we can
				// verify the cancellation propagates to the sibling.
				select {
				case <-siblingStarted:
				case <-time.After(time.Second):
					return errors.New("sibling never started")
				}
				return moduleErr
			},
		},
		{
			Name: "sibling",
			Run: func(ctx context.Context) error {
				siblingStarted <- struct{}{}
				<-ctx.Done()
				siblingCancelled <- struct{}{}
				return nil
			},
		},
	}

	runReturned := make(chan error, 1)
	go func() {
		runReturned <- supervisor.Run(ctx, modules)
	}()

	// The sibling should receive cancellation from the supervisor
	select {
	case <-siblingCancelled:
		// Good: the supervisor cancelled the context for sibling modules
	case <-time.After(2 * time.Second):
		t.Fatalf("sibling module did not receive cancellation after failing module exited")
	}

	// The supervisor should return the error from the failing module
	select {
	case err := <-runReturned:
		if err == nil {
			t.Fatalf("expected error from failing module, got nil")
		}
		if !errors.Is(err, moduleErr) {
			t.Fatalf("expected wrapped module error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("supervisor did not return after module failure")
	}
}
