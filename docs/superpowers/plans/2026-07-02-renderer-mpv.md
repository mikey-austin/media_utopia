# renderer_mpv Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `renderer_mpv`, a mud renderer module driving playback through hand-rolled cgo libmpv bindings, as a drop-in replacement for `renderer_gstreamer`, per `docs/design/renderer-mpv.md`.

**Architecture:** One `mpv_handle` per active track; a dedicated goroutine per handle blocks on `mpv_wait_event` and translates events. `renderer_core.Engine` and the module layer are reused verbatim (module.go is a near-copy of the GStreamer module). Crossfade is implemented in the driver via dual handles and cancellable fade jobs. Pure logic (fades, volume math, option assembly) lives in untagged files so unit tests run without libmpv; cgo code is behind `//go:build mpv`.

**Tech Stack:** Go 1.25, cgo + libmpv (client API, `pkg-config: mpv`), no third-party mpv wrapper. Ubuntu 26.04 Docker images (`libmpv-dev` build / `libmpv2` runtime).

## Global Constraints

- Hand-rolled cgo binding only — do NOT add `gen2brain/go-mpv` or any other dependency to go.mod.
- Build tag is `mpv`; without it a stub driver returns "mpv build tag not enabled" (mirrors `gstreamer` tag pattern).
- Same `renderer_core.Driver` contract, same MQTT surface, same module scaffolding as `renderer_gstreamer`.
- No seek debounce, no serialized teardown queue, no shutdown-budget machinery ported from the GStreamer driver (their cause is gone). Fade-job cancellation IS ported.
- mpv volume scale is 0–100; engine volume is 0.0–1.0; fade gain composes multiplicatively: `mpvVolume = user * gain * 100`.
- Local machine has NO libmpv-dev (no sudo): `mpv`-tagged code is compile-verified via the Docker build stage; untagged tests run locally with `go test ./internal/modules/renderer_mpv/...`.
- `libmpv-dev` (build) / `libmpv2` (runtime) are the only new packages.
- Existing node IDs preserved via config: optional `node_id` TOML key overrides the generated `mu:renderer:<provider>:<ns>:<resource>` ID.
- Commit after each task. Run `gofmt -w` on touched Go files before committing.

## File Structure

```
internal/modules/renderer_mpv/
  events.go          # no tag — EventEOS/EventError/EventWarning/EventAudioDown
  options.go         # no tag — handle option assembly + effectiveVolume
  options_test.go    # no tag
  fade.go            # no tag — fadeJob/fadeSet/runFade (cancellable ramps)
  fade_test.go       # no tag
  binding.go         # //go:build mpv — hand-rolled cgo libmpv binding
  driver_mpv.go      # //go:build mpv — Driver (handles, event loops, crossfade, health probe)
  driver_stub.go     # //go:build !mpv — stub Driver
  module.go          # no tag — near-copy of renderer_gstreamer/module.go
  module_test.go     # no tag — copied module tests
  soak_test.go       # //go:build mpv && integration — real-stream soak + local-file EOS test
internal/mud/config.go       # + RendererMPV config set
internal/mud/config_test.go  # + TOML parse test
cmd/mud/main.go              # + renderer_mpv wiring + enabledModules entry
Makefile                     # + mpv tags in docker; build-mpv/test-mpv/integration-mpv targets
Dockerfile                   # + libmpv-dev (build stage), libmpv2 (mud runtime stage)
```

---

### Task 1: Package scaffold — events + options + volume math

**Files:**
- Create: `internal/modules/renderer_mpv/events.go`
- Create: `internal/modules/renderer_mpv/options.go`
- Test: `internal/modules/renderer_mpv/options_test.go`

**Interfaces:**
- Produces: `package renderermpv`; `Event{Kind EventKind, Message string}`; `EventEOS/EventError/EventWarning/EventAudioDown`; `handleOptions(ao, device string, extra map[string]string, positionMS int64) [][2]string`; `effectiveVolume(user, gain float64) float64`.

- [ ] **Step 1: Write failing tests** (`options_test.go`)

```go
package renderermpv

import "testing"

func TestEffectiveVolume(t *testing.T) {
	cases := []struct {
		name       string
		user, gain float64
		want       float64
	}{
		{"full", 1.0, 1.0, 100},
		{"half user", 0.5, 1.0, 50},
		{"composed", 0.8, 0.5, 40},
		{"zero gain", 1.0, 0.0, 0},
		{"clamped high", 1.5, 2.0, 100},
		{"clamped low", -0.2, 1.0, 0},
	}
	for _, tc := range cases {
		if got := effectiveVolume(tc.user, tc.gain); got != tc.want {
			t.Errorf("%s: effectiveVolume(%v,%v)=%v want %v", tc.name, tc.user, tc.gain, got, tc.want)
		}
	}
}

func TestHandleOptions(t *testing.T) {
	opts := handleOptions("pipewire", "sink-1", map[string]string{"network-timeout": "10"}, 42000)
	get := func(name string) (string, bool) {
		var val string
		found := false
		for _, kv := range opts { // last write wins, like mpv option application
			if kv[0] == name {
				val = kv[1]
				found = true
			}
		}
		return val, found
	}
	for name, want := range map[string]string{
		"vid":           "no",
		"audio-display": "no",
		"terminal":      "no",
		"idle":          "yes",
		"cache":         "yes",
		"ao":            "pipewire",
		"audio-device":  "sink-1",
		"start":         "42.000",
		"network-timeout": "10",
	} {
		if got, ok := get(name); !ok || got != want {
			t.Errorf("option %s = %q (found=%v), want %q", name, got, ok, want)
		}
	}

	// No device / no start → options absent.
	opts = handleOptions("alsa", "", nil, 0)
	if _, ok := get("x"); ok {
		t.Fatal("bad accessor")
	}
	for _, kv := range opts {
		if kv[0] == "audio-device" || kv[0] == "start" {
			t.Errorf("unexpected option %s", kv[0])
		}
	}

	// Extra opts override base opts (escape hatch wins).
	opts = handleOptions("pipewire", "", map[string]string{"cache": "no"}, 0)
	last := ""
	for _, kv := range opts {
		if kv[0] == "cache" {
			last = kv[1]
		}
	}
	if last != "no" {
		t.Errorf("extra opts must override base: cache=%q want no", last)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/modules/renderer_mpv/`
Expected: FAIL (package doesn't compile — functions undefined)

- [ ] **Step 3: Implement** `events.go`:

```go
package renderermpv

// EventKind classifies asynchronous events surfaced by the driver.
type EventKind int

const (
	// EventEOS is emitted when the current track reaches end-of-file.
	// Consumers should advance the queue.
	EventEOS EventKind = iota
	// EventError is emitted when the current track ends with a playback
	// error (demux/decode/network failure). The handle is torn down before
	// the event is delivered.
	EventError
	// EventWarning carries mpv log messages at warn level. Worth logging
	// but not fatal.
	EventWarning
	// EventAudioDown is emitted when the healthcheck loop can't reach the
	// pipewire socket for several consecutive probes. It distinguishes
	// "stream ended" from "audio server gone" in published state.
	EventAudioDown
)

// Event carries one asynchronous notification from the driver.
type Event struct {
	Kind    EventKind
	Message string
}
```

`options.go` (sorted extras for determinism):

```go
package renderermpv

import (
	"fmt"
	"sort"
)

// handleOptions assembles the option list applied to a fresh mpv handle
// before initialization. Base audio-only options come first; per-config
// extras are appended last so they can override anything (the
// stream-compatibility escape hatch from the design doc).
func handleOptions(ao, device string, extra map[string]string, positionMS int64) [][2]string {
	opts := [][2]string{
		{"vid", "no"},
		{"audio-display", "no"},
		{"terminal", "no"},
		{"idle", "yes"},
		{"keep-open", "no"},
		{"cache", "yes"},
		{"ao", ao},
	}
	if device != "" {
		opts = append(opts, [2]string{"audio-device", device})
	}
	if positionMS > 0 {
		opts = append(opts, [2]string{"start", fmt.Sprintf("%.3f", float64(positionMS)/1000)})
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		opts = append(opts, [2]string{k, extra[k]})
	}
	return opts
}

// effectiveVolume composes the engine's 0.0–1.0 user volume with a 0.0–1.0
// crossfade gain into mpv's 0–100 softvol scale.
func effectiveVolume(user, gain float64) float64 {
	user = clamp01(user)
	gain = clamp01(gain)
	return user * gain * 100
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/modules/renderer_mpv/`
Expected: PASS

- [ ] **Step 5: Commit** — `feat(renderer_mpv): package scaffold — events, handle options, volume math`

---

### Task 2: Cancellable fade jobs

**Files:**
- Create: `internal/modules/renderer_mpv/fade.go`
- Test: `internal/modules/renderer_mpv/fade_test.go`

**Interfaces:**
- Produces: `fadeSet` with `start() (context.Context, *fadeJob)`, `finish(*fadeJob)`, `cancelAll()`, `wait()`; `runFade(ctx context.Context, d time.Duration, from, to float64, set func(float64))` — blocking ramp; on cancel it applies `to` immediately and returns.

- [ ] **Step 1: Write failing tests** (`fade_test.go`)

```go
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
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/modules/renderer_mpv/` → FAIL (undefined)

- [ ] **Step 3: Implement** `fade.go`:

```go
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
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/modules/renderer_mpv/` → PASS
- [ ] **Step 5: Commit** — `feat(renderer_mpv): cancellable fade jobs`

---

### Task 3: Hand-rolled cgo libmpv binding

**Files:**
- Create: `internal/modules/renderer_mpv/binding.go` (`//go:build mpv`)

**Interfaces:**
- Produces (all `mpv`-tagged, package-private):
  - `newMPVHandle() (*mpvHandle, error)`
  - `(*mpvHandle) setOptionString(name, value string) error`
  - `(*mpvHandle) initialize() error`
  - `(*mpvHandle) command(args ...string) error`
  - `(*mpvHandle) setPropertyDouble(name string, v float64) error`
  - `(*mpvHandle) setPropertyFlag(name string, v bool) error`
  - `(*mpvHandle) observeDouble(name string) error`
  - `(*mpvHandle) requestLogMessages(level string) error`
  - `(*mpvHandle) waitEvent(timeoutSec float64) mpvEvent`
  - `(*mpvHandle) terminateDestroy()`
  - `mpvEvent{kind mpvEventKind, endReason mpvEndReason, message, propName string, propDouble float64, propOK bool}`
  - kinds: `evNone, evShutdown, evEndFile, evLogMessage, evPropertyChange`; reasons: `endEOF, endError, endOther`

- [ ] **Step 1: Write** `binding.go`. This is the entire binding — no wrapper dependency:

```go
//go:build mpv

// Hand-rolled cgo binding for the libmpv client API. Only the ~dozen
// functions the driver needs are bound; everything is copied across the
// boundary (no shared object graph — see docs/design/renderer-mpv.md).
package renderermpv

/*
#cgo pkg-config: mpv
#include <mpv/client.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// mpvHandle wraps one opaque mpv_handle. All methods except waitEvent are
// safe to call from any goroutine; waitEvent must only be called from the
// single goroutine that owns the handle's event queue. terminateDestroy
// must not race a concurrent waitEvent — the event goroutine calls it
// after observing evShutdown (triggered by the `quit` command).
type mpvHandle struct {
	h *C.mpv_handle
}

func newMPVHandle() (*mpvHandle, error) {
	h := C.mpv_create()
	if h == nil {
		return nil, fmt.Errorf("mpv_create failed")
	}
	return &mpvHandle{h: h}, nil
}

func mpvErr(code C.int, op string) error {
	if code >= 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", op, C.GoString(C.mpv_error_string(code)))
}

func (m *mpvHandle) setOptionString(name, value string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	cvalue := C.CString(value)
	defer C.free(unsafe.Pointer(cvalue))
	return mpvErr(C.mpv_set_option_string(m.h, cname, cvalue), "set option "+name)
}

func (m *mpvHandle) initialize() error {
	return mpvErr(C.mpv_initialize(m.h), "initialize")
}

func (m *mpvHandle) command(args ...string) error {
	cargs := make([]*C.char, len(args)+1) // NULL-terminated argv
	for i, a := range args {
		cargs[i] = C.CString(a)
	}
	defer func() {
		for _, c := range cargs {
			if c != nil {
				C.free(unsafe.Pointer(c))
			}
		}
	}()
	return mpvErr(C.mpv_command(m.h, &cargs[0]), "command "+args[0])
}

func (m *mpvHandle) setPropertyDouble(name string, v float64) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	cv := C.double(v)
	return mpvErr(C.mpv_set_property(m.h, cname, C.MPV_FORMAT_DOUBLE, unsafe.Pointer(&cv)), "set property "+name)
}

func (m *mpvHandle) setPropertyFlag(name string, v bool) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	cv := C.int(0)
	if v {
		cv = 1
	}
	return mpvErr(C.mpv_set_property(m.h, cname, C.MPV_FORMAT_FLAG, unsafe.Pointer(&cv)), "set property "+name)
}

// observeDouble subscribes to property-change events for a double-typed
// property. Updates arrive via waitEvent as evPropertyChange.
func (m *mpvHandle) observeDouble(name string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	return mpvErr(C.mpv_observe_property(m.h, 0, cname, C.MPV_FORMAT_DOUBLE), "observe "+name)
}

func (m *mpvHandle) requestLogMessages(level string) error {
	clevel := C.CString(level)
	defer C.free(unsafe.Pointer(clevel))
	return mpvErr(C.mpv_request_log_messages(m.h, clevel), "request log messages")
}

// terminateDestroy synchronously brings the player down and frees the
// handle. Callers must guarantee no concurrent mpv call on this handle is
// in flight (the driver's event goroutine calls this after evShutdown).
func (m *mpvHandle) terminateDestroy() {
	C.mpv_terminate_destroy(m.h)
	m.h = nil
}

type mpvEventKind int

const (
	evNone mpvEventKind = iota
	evShutdown
	evEndFile
	evLogMessage
	evPropertyChange
)

type mpvEndReason int

const (
	endEOF mpvEndReason = iota
	endError
	endOther // stop/quit/redirect — not surfaced as driver events
)

// mpvEvent is a fully-copied snapshot of one mpv_event; nothing references
// mpv-owned memory after waitEvent returns.
type mpvEvent struct {
	kind       mpvEventKind
	endReason  mpvEndReason
	message    string  // error text (end-file) or log line (log-message)
	propName   string  // property-change
	propDouble float64 // property-change payload
	propOK     bool    // property-change had a double payload
}

// waitEvent blocks up to timeoutSec for the next event and translates it.
// Unhandled event kinds come back as evNone (callers loop).
func (m *mpvHandle) waitEvent(timeoutSec float64) mpvEvent {
	ev := C.mpv_wait_event(m.h, C.double(timeoutSec))
	switch ev.event_id {
	case C.MPV_EVENT_SHUTDOWN:
		return mpvEvent{kind: evShutdown}
	case C.MPV_EVENT_END_FILE:
		ef := (*C.mpv_event_end_file)(ev.data)
		out := mpvEvent{kind: evEndFile, endReason: endOther}
		switch ef.reason {
		case C.MPV_END_FILE_REASON_EOF:
			out.endReason = endEOF
		case C.MPV_END_FILE_REASON_ERROR:
			out.endReason = endError
			out.message = C.GoString(C.mpv_error_string(ef.error))
		}
		return out
	case C.MPV_EVENT_LOG_MESSAGE:
		lm := (*C.mpv_event_log_message)(ev.data)
		return mpvEvent{
			kind:    evLogMessage,
			message: C.GoString(lm.prefix) + ": " + C.GoString(lm.text),
		}
	case C.MPV_EVENT_PROPERTY_CHANGE:
		pc := (*C.mpv_event_property)(ev.data)
		out := mpvEvent{kind: evPropertyChange, propName: C.GoString(pc.name)}
		if pc.format == C.MPV_FORMAT_DOUBLE && pc.data != nil {
			out.propDouble = float64(*(*C.double)(pc.data))
			out.propOK = true
		}
		return out
	default:
		return mpvEvent{kind: evNone}
	}
}
```

- [ ] **Step 2: Verify untagged build still compiles** — `go build ./...` → PASS (binding is tag-gated)
- [ ] **Step 3: Verify vet on untagged tree** — `go vet ./internal/modules/renderer_mpv/` → PASS. (Tagged compile verification happens in Task 7 via Docker; do not attempt `go build -tags mpv` locally — libmpv-dev is absent.)
- [ ] **Step 4: Commit** — `feat(renderer_mpv): hand-rolled cgo libmpv binding`

---

### Task 4: mpv Driver + stub

**Files:**
- Create: `internal/modules/renderer_mpv/driver_mpv.go` (`//go:build mpv`)
- Create: `internal/modules/renderer_mpv/driver_stub.go` (`//go:build !mpv`)

**Interfaces:**
- Produces: `NewDriver(ao, device string, crossfade time.Duration, mpvOptions map[string]string) (*Driver, error)`; `*Driver` implements `renderercore.Driver` plus `Close() error` and `Events() <-chan Event`.

- [ ] **Step 1: Write** `driver_stub.go`:

```go
//go:build !mpv

package renderermpv

import (
	"errors"
	"time"
)

// Driver is a stub when the mpv build tag is not enabled.
type Driver struct{}

var errNoMPV = errors.New("mpv build tag not enabled")

// NewDriver returns an error when the mpv build tag is missing.
func NewDriver(ao string, device string, crossfade time.Duration, mpvOptions map[string]string) (*Driver, error) {
	return nil, errNoMPV
}

func (d *Driver) Play(url string, positionMS int64) error { return errNoMPV }
func (d *Driver) Pause() error                            { return errNoMPV }
func (d *Driver) Resume() error                           { return errNoMPV }
func (d *Driver) Stop() error                             { return errNoMPV }
func (d *Driver) SeekTo(positionMS int64) error           { return errNoMPV }
func (d *Driver) SetVolume(volume float64) error          { return errNoMPV }
func (d *Driver) SetMute(mute bool) error                 { return errNoMPV }
func (d *Driver) Position() (int64, int64, bool)          { return 0, 0, false }
```

- [ ] **Step 2: Write** `driver_mpv.go`:

```go
//go:build mpv

package renderermpv

import (
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// eventWaitTimeout bounds each mpv_wait_event call so the event
	// goroutine can never hang forever on a wedged handle.
	eventWaitTimeout = 1.0 // seconds

	healthcheckInterval         = 30 * time.Second
	healthcheckFailureThreshold = 2 // ~60s of unhealth before we react

	// closeBudget bounds Close's wait for handle teardown. mpv's quit +
	// terminate_destroy is synchronous and fast for audio-only handles;
	// the budget is a backstop that keeps shutdown well inside docker's
	// 30s stop grace.
	closeBudget = 8 * time.Second
)

// track is one active mpv handle: the playing (or fading-out) instance of
// one URL. Position/duration arrive via property observation and are
// cached in atomics so Position() never crosses cgo on the hot path.
type track struct {
	h        *mpvHandle
	posMS    atomic.Int64
	durMS    atomic.Int64
	posSeen  atomic.Bool
	gainBits atomic.Uint64 // math.Float64bits of the crossfade gain (0..1)
	done     chan struct{} // closed after terminateDestroy
	quitOnce sync.Once
}

func (t *track) gain() float64        { return math.Float64frombits(t.gainBits.Load()) }
func (t *track) storeGain(g float64)  { t.gainBits.Store(math.Float64bits(g)) }

// quit asks the handle's core to shut down. The event goroutine observes
// evShutdown and performs terminateDestroy — quit itself never blocks and
// is safe from any goroutine.
func (t *track) quit() {
	t.quitOnce.Do(func() {
		_ = t.h.command("quit")
	})
}

// Driver implements renderer_core.Driver on libmpv, one handle per track.
type Driver struct {
	mu        sync.Mutex
	ao        string
	device    string
	crossfade time.Duration
	extraOpts map[string]string

	volume  float64
	muted   bool
	current *track

	fades fadeSet

	// events carries EOS / error / warning / audio-down notifications up to
	// the Module. Buffered; emit drops rather than blocking event loops.
	events chan Event

	// wg tracks per-track event goroutines and the healthcheck loop.
	wg sync.WaitGroup

	healthCancel chan struct{}
	closeOnce    sync.Once
	closed       bool
}

// NewDriver creates an mpv driver. ao selects the audio output backend
// (pipewire, alsa, pulse, null, ...); mpvOptions are applied verbatim to
// every handle as the stream-tuning escape hatch.
func NewDriver(ao string, device string, crossfade time.Duration, mpvOptions map[string]string) (*Driver, error) {
	if ao == "" {
		ao = "pipewire"
	}
	d := &Driver{
		ao:        ao,
		device:    device,
		crossfade: crossfade,
		extraOpts: mpvOptions,
		volume:    1.0,
		events:    make(chan Event, 16),
	}
	if ao == "pipewire" {
		d.healthCancel = make(chan struct{})
		d.wg.Add(1)
		go d.healthcheckLoop()
	}
	return d, nil
}

// Events returns the async notification channel. Events may be dropped
// (with a stderr warning) if the consumer falls behind.
func (d *Driver) Events() <-chan Event {
	return d.events
}

func (d *Driver) emit(ev Event) {
	select {
	case d.events <- ev:
	default:
		fmt.Fprintf(os.Stderr, "mpv: dropped event kind=%d msg=%q (consumer slow)\n", ev.Kind, ev.Message)
	}
}

// newTrack builds and starts a handle for url. On any error the handle is
// destroyed before returning (no event goroutine is running yet, so a
// direct terminateDestroy is safe).
func (d *Driver) newTrack(url string, positionMS int64, initialGain float64) (*track, error) {
	h, err := newMPVHandle()
	if err != nil {
		return nil, err
	}
	t := &track{h: h, done: make(chan struct{})}
	t.storeGain(initialGain)
	fail := func(err error) (*track, error) {
		h.terminateDestroy()
		return nil, err
	}
	for _, kv := range handleOptions(d.ao, d.device, d.extraOpts, positionMS) {
		if err := h.setOptionString(kv[0], kv[1]); err != nil {
			return fail(err)
		}
	}
	if err := h.initialize(); err != nil {
		return fail(err)
	}
	if err := h.requestLogMessages("warn"); err != nil {
		return fail(err)
	}
	if err := h.observeDouble("time-pos"); err != nil {
		return fail(err)
	}
	if err := h.observeDouble("duration"); err != nil {
		return fail(err)
	}
	// Volume/mute are applied before loadfile so playback never starts at
	// the wrong level (avoids the audible pop at crossfade start).
	if err := h.setPropertyDouble("volume", effectiveVolume(d.volume, initialGain)); err != nil {
		return fail(err)
	}
	if err := h.setPropertyFlag("mute", d.muted); err != nil {
		return fail(err)
	}
	if err := h.command("loadfile", url); err != nil {
		return fail(err)
	}
	d.wg.Add(1)
	go d.eventLoop(t)
	return t, nil
}

// eventLoop owns t's event queue: it caches position/duration, forwards
// EOS/error/warning events (only while t is the current track — an
// outgoing crossfade handle finishing must not double-advance the queue),
// and performs the final terminateDestroy after quit.
func (d *Driver) eventLoop(t *track) {
	defer d.wg.Done()
	for {
		ev := t.h.waitEvent(eventWaitTimeout)
		switch ev.kind {
		case evShutdown:
			t.h.terminateDestroy()
			close(t.done)
			return
		case evPropertyChange:
			if !ev.propOK {
				continue
			}
			switch ev.propName {
			case "time-pos":
				t.posMS.Store(int64(ev.propDouble * 1000))
				t.posSeen.Store(true)
			case "duration":
				t.durMS.Store(int64(ev.propDouble * 1000))
			}
		case evEndFile:
			switch ev.endReason {
			case endEOF:
				if d.isCurrent(t) {
					d.emit(Event{Kind: EventEOS})
				}
			case endError:
				if d.isCurrent(t) {
					d.emit(Event{Kind: EventError, Message: ev.message})
				}
			}
		case evLogMessage:
			d.emit(Event{Kind: EventWarning, Message: ev.message})
		}
	}
}

func (d *Driver) isCurrent(t *track) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current == t
}

// applyGain is the fade goroutines' volume sink: it composes the fade gain
// with the user volume and pushes the result to the handle.
func (d *Driver) applyGain(t *track, gain float64) {
	t.storeGain(gain)
	d.mu.Lock()
	user := d.volume
	d.mu.Unlock()
	_ = t.h.setPropertyDouble("volume", effectiveVolume(user, gain))
}

// Play starts playback of url. With crossfade configured and a track
// already playing, the outgoing handle fades down while the incoming one
// fades up; pipewire mixes the two.
func (d *Driver) Play(url string, positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errors.New("driver closed")
	}
	d.fades.cancelAll()

	old := d.current
	crossfading := d.crossfade > 0 && old != nil
	initialGain := 1.0
	if crossfading {
		initialGain = 0.0
	}
	t, err := d.newTrack(url, positionMS, initialGain)
	if err != nil {
		return err
	}
	d.current = t

	if crossfading {
		inCtx, inJob := d.fades.start()
		go func() {
			defer d.fades.finish(inJob)
			runFade(inCtx, d.crossfade, 0, 1, func(g float64) { d.applyGain(t, g) })
		}()
		outCtx, outJob := d.fades.start()
		go func() {
			defer d.fades.finish(outJob)
			runFade(outCtx, d.crossfade, old.gain(), 0, func(g float64) { d.applyGain(old, g) })
			old.quit()
		}()
	} else if old != nil {
		old.quit()
	}
	return nil
}

// Pause pauses playback. During a crossfade the fade is cancelled first
// (ramps snap to their targets) so pause applies to the incoming track.
func (d *Driver) Pause() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return errors.New("not playing")
	}
	d.fades.cancelAll()
	return d.current.h.setPropertyFlag("pause", true)
}

// Resume resumes playback.
func (d *Driver) Resume() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return errors.New("not playing")
	}
	return d.current.h.setPropertyFlag("pause", false)
}

// Stop stops playback and discards the current handle.
func (d *Driver) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.current == nil {
		return nil
	}
	d.fades.cancelAll()
	d.current.quit()
	d.current = nil
	return nil
}

// SeekTo seeks to an absolute position. mpv coalesces stacked seeks
// internally, so no debounce is needed (the GStreamer workaround is
// deliberately not ported).
func (d *Driver) SeekTo(positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return errors.New("not playing")
	}
	return d.current.h.command("seek", strconv.FormatFloat(float64(positionMS)/1000, 'f', 3, 64), "absolute")
}

// SetVolume sets the user volume (0..1), composed with any in-flight fade
// gain.
func (d *Driver) SetVolume(volume float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.volume = volume
	if d.current != nil {
		return d.current.h.setPropertyDouble("volume", effectiveVolume(volume, d.current.gain()))
	}
	return nil
}

// SetMute toggles mpv's softvol mute; it is independent of the volume
// property so fades and user volume compose cleanly around it.
func (d *Driver) SetMute(mute bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.muted = mute
	if d.current != nil {
		return d.current.h.setPropertyFlag("mute", mute)
	}
	return nil
}

// Position returns cached position/duration for the current track.
func (d *Driver) Position() (int64, int64, bool) {
	d.mu.Lock()
	t := d.current
	d.mu.Unlock()
	if t == nil || !t.posSeen.Load() {
		return 0, 0, false
	}
	return t.posMS.Load(), t.durMS.Load(), true
}

// Close shuts the driver down: fades are cancelled and awaited, every
// outstanding handle is quit, and their event goroutines perform the
// bounded synchronous terminate_destroy. After Close, driver calls fail.
func (d *Driver) Close() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		survivor := d.current
		d.current = nil
		d.fades.cancelAll()
		d.mu.Unlock()

		if d.healthCancel != nil {
			close(d.healthCancel)
		}
		// Cancelled fade-outs quit their tracks on exit; wait for that so
		// every handle has a quit in flight before we wait on teardown.
		d.fades.wait()
		if survivor != nil {
			survivor.quit()
		}
		done := make(chan struct{})
		go func() {
			d.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			close(d.events)
		case <-time.After(closeBudget):
			// Event goroutines still alive — leave events open so nothing
			// sends on a closed channel; the process is exiting anyway.
			fmt.Fprintf(os.Stderr, "mpv: close budget exceeded waiting for handle teardown\n")
		}
	})
	return nil
}

// healthcheckLoop probes the pipewire socket and emits EventAudioDown after
// consecutive failures — distinguishing "stream ended" from "audio server
// gone" in published state. mpv itself surfaces in-stream AO failures as
// end-file errors, so no teardown is performed here.
func (d *Driver) healthcheckLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(healthcheckInterval)
	defer ticker.Stop()
	failures := 0
	notified := false
	for {
		select {
		case <-d.healthCancel:
			return
		case <-ticker.C:
		}
		if probePipewire() {
			failures = 0
			notified = false
			continue
		}
		failures++
		if failures >= healthcheckFailureThreshold && !notified {
			notified = true
			d.emit(Event{Kind: EventAudioDown, Message: "pipewire socket unreachable"})
		}
	}
}

// probePipewire returns true if the pipewire control socket is connectable.
// Same path resolution libpipewire uses (PIPEWIRE_RUNTIME_DIR/PIPEWIRE_REMOTE
// falling back to XDG_RUNTIME_DIR/pipewire-0).
func probePipewire() bool {
	socket := os.Getenv("PIPEWIRE_REMOTE")
	if socket == "" {
		socket = "pipewire-0"
	}
	runtime := os.Getenv("PIPEWIRE_RUNTIME_DIR")
	if runtime == "" {
		runtime = os.Getenv("XDG_RUNTIME_DIR")
	}
	if runtime == "" {
		runtime = "/run/pipewire"
	}
	conn, err := net.DialTimeout("unix", filepath.Join(runtime, socket), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
```

- [ ] **Step 3: Verify untagged build + tests** — `go build ./... && go test ./internal/modules/renderer_mpv/` → PASS (stub compiles; tagged file checked in Task 7)
- [ ] **Step 4: Commit** — `feat(renderer_mpv): libmpv driver with crossfade and pipewire health probe`

---

> **Revision (2026-07-02, per user feedback):** Task 5 was reworked after
> initial execution. Instead of copying `renderer_gstreamer/module.go`
> wholesale, the shared module scaffolding was extracted into
> `renderer_core` (`DriverEvent`/`DriverEventKind`/`DriverEventSource` in
> `driver_events.go`; `Module`/`ModuleConfig`/`NewModule(log, client, cfg,
> driver)` in `module.go`; module tests moved to
> `renderer_core/module_test.go`). Both `renderer_gstreamer` and
> `renderer_mpv` are now ~50-line wrappers that construct their driver and
> delegate; their `events.go` files alias the shared event types.
> `renderer_kodi`/`renderer_vlc` still carry older module copies
> (polling-based EOS detection) — follow-up candidates.

### Task 5: Module + module tests

**Files:**
- Create: `internal/modules/renderer_mpv/module.go`
- Test: `internal/modules/renderer_mpv/module_test.go`

**Interfaces:**
- Consumes: `NewDriver(ao, device, crossfade, mpvOptions)` from Task 4; events from Task 1.
- Produces: `NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error)`; `Config{NodeID, TopicBase, Name, AO, Device string, Crossfade time.Duration, Volume float64, PublishState bool, Source string, MPVOptions map[string]string, StatePublisher renderercore.StatePublisher, PresencePublisher renderercore.PresencePublisher}`; `(*Module).Run(ctx) error`.

- [ ] **Step 1: Copy** `internal/modules/renderer_gstreamer/module.go` → `internal/modules/renderer_mpv/module.go` and apply exactly these diffs:
  1. `package renderergstreamer` → `package renderermpv`.
  2. Doc comments: "GStreamer renderer" → "mpv renderer"; `// Module implements a GStreamer renderer.` → `// Module implements an mpv (libmpv) renderer.`
  3. `Config` struct: replace field `Pipeline string` with `AO string`; add `MPVOptions map[string]string` after `Volume float64`. (Other fields unchanged.)
  4. In `NewModule`: default name `"GStreamer Renderer"` → `"MPV Renderer"`; delete the `if strings.TrimSpace(cfg.Pipeline) == "" { return nil, errors.New("pipeline required") }` block (ao has a driver-side default); change driver construction to `driver, err := NewDriver(cfg.AO, cfg.Device, cfg.Crossfade, cfg.MPVOptions)`.
  5. In `consumeDriverEvents`: log strings `"gst EOS — advancing queue"` → `"mpv EOS — advancing queue"`, `"gst pipeline error"` → `"mpv playback error"`, `"gst pipeline warning"` → `"mpv warning"`; replace the `case EventPipewireDown:` branch with:

```go
			case EventAudioDown:
				m.log.Error("audio backend unreachable — playback will fail until it returns",
					zap.String("message", ev.Message))
				if m.config.PublishState {
					m.scheduleStatePublish()
				}
```

  6. The `Run` teardown comment referencing GStreamer SetState(Null) → reword: mpv's terminate_destroy gives pipewire a clean client disconnect before exit (same rationale: avoid stale daemon state after SIGKILL).

- [ ] **Step 2: Copy** `internal/modules/renderer_gstreamer/module_test.go` → `internal/modules/renderer_mpv/module_test.go`; change only the package clause to `package renderermpv`. All three tests (loadPlaylist, loadSnapshot, load-error-no-corruption) run against the stub-driver-in-test pattern and are renderer-agnostic.

- [ ] **Step 3: Run** — `go test ./internal/modules/renderer_mpv/` → PASS (5 tests + fade/options)
- [ ] **Step 4: Commit** — `feat(renderer_mpv): module layer (copy of renderer_gstreamer wiring)`

---

### Task 6: mud config + wiring

**Files:**
- Modify: `internal/mud/config.go` (ModulesConfig ~line 62; new struct after RendererGStreamerConfig ~line 121)
- Modify: `cmd/mud/main.go` (import block; new wiring block after the renderer_gstreamer block ending ~line 549; enabledModules loop after the RendererGStreamer loop ~line 727)
- Test: `internal/mud/config_test.go`

**Interfaces:**
- Consumes: `renderermpv.NewModule`, `renderermpv.Config` from Task 5.
- Produces: TOML section `[modules.renderer_mpv.<name>]` with keys `enabled, name, provider, resource, node_id, ao, device, crossfade_ms, volume, source` and subtable `mpv_options`.

- [ ] **Step 1: Write failing config test** (append to `internal/mud/config_test.go`, matching that file's existing style):

```go
func TestRendererMPVConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mud.toml")
	body := `
[server]
identity = "test"

[modules.renderer_mpv.living_room]
enabled = true
name = "Living Room"
provider = "gstreamer"
resource = "default"
node_id = "mu:renderer:gstreamer:mud@livingroom:default"
ao = "pipewire"
device = "sink-1"
crossfade_ms = 3000
volume = 0.8
[modules.renderer_mpv.living_room.mpv_options]
network-timeout = "10"
demuxer-max-bytes = "32MiB"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	items := cfg.Modules.RendererMPV.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 renderer_mpv item, got %d", len(items))
	}
	item := items[0].Config
	if !item.Enabled || item.AO != "pipewire" || item.Device != "sink-1" ||
		item.CrossfadeMS != 3000 || item.Volume != 0.8 ||
		item.NodeID != "mu:renderer:gstreamer:mud@livingroom:default" {
		t.Fatalf("unexpected config: %+v", item)
	}
	if item.MPVOptions["network-timeout"] != "10" || item.MPVOptions["demuxer-max-bytes"] != "32MiB" {
		t.Fatalf("mpv_options not parsed: %+v", item.MPVOptions)
	}
}
```

(Adjust `LoadConfig` call to the file's actual loader function name; check imports `os`, `path/filepath` are present.)

- [ ] **Step 2: Run to verify failure** — `go test ./internal/mud/` → FAIL (RendererMPV undefined)

- [ ] **Step 3: Implement config.** In `ModulesConfig` add after RendererGStreamer:

```go
	RendererMPV           ModuleConfigSet[RendererMPVConfig]       `toml:"renderer_mpv"`
```

After `RendererGStreamerConfig`:

```go
// RendererMPVConfig configures the mpv (libmpv) renderer module. NodeID
// optionally pins the full node ID so cutover from renderer_gstreamer
// preserves retained MQTT state, queue snapshots, and zone wiring.
type RendererMPVConfig struct {
	Enabled     bool              `toml:"enabled"`
	Name        string            `toml:"name"`
	Provider    string            `toml:"provider"`
	Resource    string            `toml:"resource"`
	NodeID      string            `toml:"node_id"`
	AO          string            `toml:"ao"`
	Device      string            `toml:"device"`
	CrossfadeMS int64             `toml:"crossfade_ms"`
	Volume      float64           `toml:"volume"`
	Source      string            `toml:"source"`
	MPVOptions  map[string]string `toml:"mpv_options"`
}
```

- [ ] **Step 4: Wire in** `cmd/mud/main.go`. Import `renderermpv "github.com/mikey-austin/media_utopia/internal/modules/renderer_mpv"`. After the renderer_gstreamer block:

```go
	if moduleOnly == "" || moduleOnly == "renderer_mpv" {
		for _, item := range cfg.Modules.RendererMPV.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			crossfade := time.Duration(cfgItem.CrossfadeMS) * time.Millisecond
			nodeID := strings.TrimSpace(cfgItem.NodeID)
			if nodeID == "" {
				resource := resourceFor(item.Name, cfgItem.Resource)
				var err error
				nodeID, err = buildNodeID("renderer", cfgItem.Provider, cfg.Server.Namespace, resource)
				if err != nil {
					return nil, err
				}
			}
			if err := ensureUnique(nodeID, "renderer_mpv"); err != nil {
				return nil, err
			}
			volume := cfgItem.Volume
			if volume <= 0 {
				volume = 1.0
			}
			stateTopic := mu.TopicState(cfg.Server.TopicBase, nodeID)
			presenceTopic := mu.TopicPresence(cfg.Server.TopicBase, nodeID)
			mod, err := renderermpv.NewModule(logFactory.ModuleLogger("renderer_mpv"), client, renderermpv.Config{
				NodeID:            nodeID,
				TopicBase:         cfg.Server.TopicBase,
				Name:              cfgItem.Name,
				AO:                cfgItem.AO,
				Device:            cfgItem.Device,
				Crossfade:         crossfade,
				Volume:            volume,
				PublishState:      true,
				Source:            cfgItem.Source,
				MPVOptions:        cfgItem.MPVOptions,
				StatePublisher:    renderercore.NewMQTTStatePublisher(client, stateTopic),
				PresencePublisher: renderercore.NewMQTTPresencePublisher(client, presenceTopic),
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "renderer_mpv",
				Run:  mod.Run,
			})
		}
	}
```

In the enabled-modules list function, after the RendererGStreamer loop:

```go
	for _, item := range cfg.Modules.RendererMPV.List() {
		if item.Config.Enabled {
			out = append(out, "renderer_mpv")
			break
		}
	}
```

- [ ] **Step 5: Run** — `go test ./internal/mud/ ./cmd/mud/ && go build ./...` → PASS
- [ ] **Step 6: Commit** — `feat(mud): wire renderer_mpv module config`

---

### Task 7: Makefile + Dockerfile

**Files:**
- Modify: `Makefile`
- Modify: `Dockerfile`

- [ ] **Step 1: Makefile.** Add `mpv` to the docker image tags, plus local targets for machines with libmpv-dev:

```makefile
build-mpv:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/mu ./cmd/mu
	go build -tags "gstreamer upnp chromaprint mpv" -o $(BIN_DIR)/mud ./cmd/mud

test-mpv:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags mpv ./internal/modules/renderer_mpv/...

integration-mpv:
	GOCACHE=$(GOCACHE) go test -count=1 -v -tags "mpv integration" ./internal/modules/renderer_mpv/...
```

Change the `docker` target's BUILD_TAGS to `"upnp gstreamer chromaprint mpv"`. Add the three new targets to `.PHONY`.

- [ ] **Step 2: Dockerfile.** Build stage: add `libmpv-dev \` to the apt list; update `ARG BUILD_TAGS` default to `"gstreamer upnp mpv"` and header comments. `mud` runtime stage: add `libmpv2 \` to its apt list (FFmpeg libs come in as its dependencies).

- [ ] **Step 3: Verify tagged compile via Docker** (this is the compile gate for binding.go/driver_mpv.go):

Run: `docker build --target build --build-arg BUILD_TAGS="upnp gstreamer chromaprint mpv" -t mud-build-check .`
Expected: builds to completion. Fix any cgo compile errors now.

- [ ] **Step 4: Full image builds** — `make docker` and `make docker-library` → both succeed.
- [ ] **Step 5: Commit** — `build: mpv build tag in Makefile and Docker images`

---

### Task 8: Integration soak harness

**Files:**
- Create: `internal/modules/renderer_mpv/soak_test.go` (`//go:build mpv && integration`)

Two pieces: a hermetic local-file test (generated WAV → play with `ao=null` → expect EOS, seek, pause/resume), and an opt-in real-stream soak driven by `MU_SOAK_STREAMS` (comma-separated URLs; skipped when unset). `ao=null` means no audio stack is needed.

- [ ] **Step 1: Write** `soak_test.go`:

```go
//go:build mpv && integration

package renderermpv

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestWAV writes a mono 16-bit 8kHz PCM wav of the given duration.
func writeTestWAV(t *testing.T, path string, dur time.Duration) {
	t.Helper()
	const rate = 8000
	n := int(float64(rate) * dur.Seconds())
	data := make([]byte, 44+2*n)
	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+2*n))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:], 1) // mono
	binary.LittleEndian.PutUint32(data[24:], rate)
	binary.LittleEndian.PutUint32(data[28:], rate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(2*n))
	// silence samples are fine
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
}

func newNullDriver(t *testing.T, crossfade time.Duration) *Driver {
	t.Helper()
	d, err := NewDriver("null", "", crossfade, nil)
	if err != nil {
		t.Fatalf("NewDriver: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func waitEvent(t *testing.T, d *Driver, kind EventKind, timeout time.Duration) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-d.Events():
			if ev.Kind == kind {
				return ev
			}
			t.Logf("event kind=%d msg=%s", ev.Kind, ev.Message)
		case <-deadline:
			t.Fatalf("timed out waiting for event kind=%d", kind)
		}
	}
}

func TestPlayLocalFileToEOS(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeTestWAV(t, wav, 2*time.Second)
	d := newNullDriver(t, 0)
	if err := d.Play(wav, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitEvent(t, d, EventEOS, 15*time.Second)
}

func TestPositionSeekPauseResume(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeTestWAV(t, wav, 30*time.Second)
	d := newNullDriver(t, 0)
	if err := d.Play(wav, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if pos, dur, ok := d.Position(); ok && dur > 25000 && pos >= 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("position/duration never observed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := d.SeekTo(20000); err != nil {
		t.Fatalf("SeekTo: %v", err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if pos, _, ok := d.Position(); ok && pos >= 19000 {
			break
		}
		if time.Now().After(deadline) {
			pos, _, _ := d.Position()
			t.Fatalf("seek not reflected, pos=%d", pos)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := d.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := d.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestCrossfadePlaySwitch(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.wav")
	b := filepath.Join(dir, "b.wav")
	writeTestWAV(t, a, 30*time.Second)
	writeTestWAV(t, b, 5*time.Second)
	d := newNullDriver(t, 1*time.Second)
	if err := d.Play(a, 0); err != nil {
		t.Fatalf("Play a: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := d.Play(b, 0); err != nil {
		t.Fatalf("Play b (crossfade): %v", err)
	}
	// b (5s) should reach EOS while a's fade-out handle is long gone.
	waitEvent(t, d, EventEOS, 20*time.Second)
}

// TestStreamSoak exercises the real station list. Opt-in:
//
//	MU_SOAK_STREAMS="http://...,http://..." make integration-mpv
func TestStreamSoak(t *testing.T) {
	raw := os.Getenv("MU_SOAK_STREAMS")
	if raw == "" {
		t.Skip("MU_SOAK_STREAMS not set")
	}
	for _, url := range strings.Split(raw, ",") {
		url := strings.TrimSpace(url)
		if url == "" {
			continue
		}
		t.Run(url, func(t *testing.T) {
			d := newNullDriver(t, 0)
			if err := d.Play(url, 0); err != nil {
				t.Fatalf("Play: %v", err)
			}
			// A live stream never EOSes; assert playback position advances
			// and no error event arrives within the probe window.
			deadline := time.After(20 * time.Second)
			for {
				select {
				case ev := <-d.Events():
					if ev.Kind == EventError {
						t.Fatalf("playback error: %s", ev.Message)
					}
					t.Logf("event kind=%d msg=%s", ev.Kind, ev.Message)
				case <-time.After(500 * time.Millisecond):
					if pos, _, ok := d.Position(); ok && pos > 2000 {
						return // played >2s of stream — success
					}
				case <-deadline:
					t.Fatal("stream never reached 2s of playback")
				}
			}
		})
	}
}
```

- [ ] **Step 2: Verify untagged tests still pass** — `go test ./internal/modules/renderer_mpv/` → PASS
- [ ] **Step 3: Run the integration tests inside the Docker build container** (has libmpv-dev):

Run:
```bash
docker build --target build --build-arg BUILD_TAGS="mpv" -t mud-build-check . \
  && docker run --rm mud-build-check sh -c 'cd /src && go test -count=1 -v -tags "mpv integration" ./internal/modules/renderer_mpv/'
```
Expected: local-file tests PASS (soak skipped without MU_SOAK_STREAMS). Iterate on driver bugs here — this is the acceptance gate for the driver's real behavior.

- [ ] **Step 4: Commit** — `test(renderer_mpv): integration soak harness (ao=null)`

---

## Self-Review Notes

- Spec coverage: binding (T3), driver+events+health (T4), crossfade (T2+T4), module/config/stub (T1,T4,T5,T6), packaging (T7), testing incl. soak (T8). Migration steps 2–3 (zone cutover, gstreamer deletion) are runtime/user actions, out of scope per design.
- Open questions resolved per design's proposals: hand-rolled binding; Pause during crossfade cancels fades and pauses incoming; gapless deferred.
- Type consistency: `NewDriver(ao, device string, crossfade time.Duration, mpvOptions map[string]string)` used identically in stub, mpv driver, and module. `Event`/`EventKind` names match module's consumeDriverEvents.
