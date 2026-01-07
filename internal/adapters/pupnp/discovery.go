//go:build upnp

package pupnp

// #cgo pkg-config: libupnp
// #cgo CFLAGS: -I/usr/include/upnp
// #include <upnp.h>
// #include <UpnpDiscovery.h>
// #include <stdlib.h>
//
// extern int goUPnPClientCallback(Upnp_EventType eventType, void *event, void *cookie);
// int upnpDispatchEvent(Upnp_EventType eventType, void *event, void *cookie);
import "C"

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime/cgo"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var (
	initMu     sync.Mutex
	initCount  int
	upnpInited bool
	initHost   string
)

// DiscoveryResult holds a single SSDP discovery entry.
type DiscoveryResult struct {
	DeviceID    string
	DeviceType  string
	ServiceType string
	ServiceVer  string
	Location    string
	OS          string
	Date        string
	Expires     int
}

// Client performs UPnP discovery using libupnp (pupnp).
type Client struct {
	handle C.UpnpClient_Handle
	token  cgo.Handle

	mu        sync.Mutex
	req       *discoveryRequest
	closed    bool
	iface     string
	boundPort uint16
}

type discoveryRequest struct {
	results chan DiscoveryResult
	done    chan struct{}
}

// NewClient initializes libupnp and registers a client callback.
// listenAddr is in host:port form; host selects the interface and port is optional (0 chooses a random port).
func NewClient(listenAddr string) (*Client, error) {
	host, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// Allow bare interface names or hosts without a port.
		host = listenAddr
	}
	var port uint16
	if portStr != "" {
		if parsed, perr := net.LookupPort("tcp", portStr); perr == nil {
			port = uint16(parsed)
		}
	}
	var cHost *C.char
	if strings.TrimSpace(host) != "" {
		cHost = C.CString(host)
		defer C.free(unsafe.Pointer(cHost))
	}
	if err := initUPnP(cHost, port); err != nil {
		return nil, err
	}
	var clientHandle C.UpnpClient_Handle
	client := &Client{
		handle:    clientHandle,
		iface:     host,
		boundPort: port,
	}
	client.token = cgo.NewHandle(client)
	if status := C.UpnpRegisterClient((C.Upnp_FunPtr)(unsafe.Pointer(C.upnpDispatchEvent)), unsafe.Pointer(client.token), &client.handle); status != C.UPNP_E_SUCCESS {
		client.token.Delete()
		C.UpnpFinish()
		return nil, fmt.Errorf("pupnp register client failed: %d", int(status))
	}
	return client, nil
}

// Close releases libupnp resources.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if c.req != nil {
		close(c.req.done)
	}
	C.UpnpUnRegisterClient(c.handle)
	c.token.Delete()
	finishUPnP()
}

// Discover searches for the provided search target (ST).
// MX controls the SSDP M-SEARCH wait time; defaults to 3 seconds if zero.
func (c *Client) Discover(ctx context.Context, searchTarget string, mx time.Duration) ([]DiscoveryResult, error) {
	if mx <= 0 {
		mx = 3 * time.Second
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("pupnp client closed")
	}
	if c.req != nil {
		c.mu.Unlock()
		return nil, errors.New("discovery already in progress")
	}
	req := &discoveryRequest{
		results: make(chan DiscoveryResult, 64),
		done:    make(chan struct{}),
	}
	c.req = req
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.req = nil
		c.mu.Unlock()
	}()

	cSearch := C.CString(searchTarget)
	defer C.free(unsafe.Pointer(cSearch))
	mxSeconds := C.int(mx / time.Second)
	status := C.UpnpSearchAsync(c.handle, mxSeconds, cSearch, unsafe.Pointer(c.token))
	if status != C.UPNP_E_SUCCESS {
		return nil, fmt.Errorf("pupnp search failed: %d", int(status))
	}

	results := []DiscoveryResult{}
	for {
		select {
		case res, ok := <-req.results:
			if !ok {
				return results, nil
			}
			results = append(results, res)
		case <-req.done:
			return results, nil
		case <-ctx.Done():
			return results, ctx.Err()
		}
	}
}

//export goUPnPClientCallback
func goUPnPClientCallback(eventType C.Upnp_EventType, event unsafe.Pointer, cookie unsafe.Pointer) C.int {
	handle := cgo.Handle(uintptr(cookie))
	raw := handle.Value()
	client, ok := raw.(*Client)
	if !ok || client == nil {
		return 0
	}
	switch eventType {
	case C.UPNP_DISCOVERY_SEARCH_RESULT:
		client.pushDiscovery(event)
	case C.UPNP_DISCOVERY_SEARCH_TIMEOUT:
		client.finishDiscovery()
	}
	return 0
}

//export upnpDispatchEvent
func upnpDispatchEvent(eventType C.Upnp_EventType, event unsafe.Pointer, cookie unsafe.Pointer) C.int {
	return goUPnPClientCallback(eventType, event, cookie)
}

func (c *Client) pushDiscovery(event unsafe.Pointer) {
	c.mu.Lock()
	req := c.req
	c.mu.Unlock()
	if req == nil {
		return
	}
	disc := (*C.UpnpDiscovery)(event)
	result := DiscoveryResult{
		DeviceID:    goCString(C.UpnpDiscovery_get_DeviceID_cstr(disc)),
		DeviceType:  goCString(C.UpnpDiscovery_get_DeviceType_cstr(disc)),
		ServiceType: goCString(C.UpnpDiscovery_get_ServiceType_cstr(disc)),
		ServiceVer:  goCString(C.UpnpDiscovery_get_ServiceVer_cstr(disc)),
		Location:    goCString(C.UpnpDiscovery_get_Location_cstr(disc)),
		OS:          goCString(C.UpnpDiscovery_get_Os_cstr(disc)),
		Date:        goCString(C.UpnpDiscovery_get_Date_cstr(disc)),
		Expires:     int(C.UpnpDiscovery_get_Expires(disc)),
	}
	select {
	case req.results <- result:
	default:
	}
}

func (c *Client) finishDiscovery() {
	c.mu.Lock()
	req := c.req
	c.mu.Unlock()
	if req == nil {
		return
	}
	close(req.results)
	select {
	case <-req.done:
	default:
		close(req.done)
	}
}

func goCString(str *C.char) string {
	if str == nil {
		return ""
	}
	return C.GoString(str)
}

func initUPnP(host *C.char, port uint16) error {
	initMu.Lock()
	defer initMu.Unlock()
	if upnpInited {
		reqHost := ""
		if host != nil {
			reqHost = C.GoString(host)
		}
		if initHost != "" && reqHost != "" && initHost != reqHost {
			return fmt.Errorf("pupnp already initialized on %s, requested %s; ensure all upnp modules share the same listen address", initHost, reqHost)
		}
		initCount++
		return nil
	}
	status := C.UpnpInit2(host, C.ushort(port))
	if status != C.UPNP_E_SUCCESS {
		// Fallback: let libupnp pick interface/port.
		status = C.UpnpInit2(nil, C.ushort(port))
		if status != C.UPNP_E_SUCCESS {
			return fmt.Errorf("pupnp init failed: %d", int(status))
		}
	}
	upnpInited = true
	initCount = 1
	if host != nil {
		initHost = C.GoString(host)
	} else {
		initHost = ""
	}
	return nil
}

func finishUPnP() {
	initMu.Lock()
	defer initMu.Unlock()
	if !upnpInited {
		return
	}
	initCount--
	if initCount <= 0 {
		C.UpnpFinish()
		upnpInited = false
		initCount = 0
	}
}
