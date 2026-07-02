//go:build mpv

// Hand-rolled cgo binding for the libmpv client API. Only the ~dozen
// functions the driver needs are bound; every event is copied across the
// boundary so no shared object graph or refcount ever crosses cgo (see
// docs/design/renderer-mpv.md).
package renderermpv

/*
// Deliberately not `pkg-config: mpv` — Ubuntu's mpv.pc carries
// -fno-strict-overflow in Cflags, which cgo's flag allowlist rejects.
// libmpv's header and library live on default search paths.
#cgo LDFLAGS: -lmpv
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
