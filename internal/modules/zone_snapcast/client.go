package zonesnapcast

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// SnapcastClient provides JSON-RPC communication with Snapcast server.
type SnapcastClient struct {
	log       *zap.Logger
	serverURL string
	conn      *websocket.Conn
	tcpConn   net.Conn
	isTCP     bool
	mu        sync.Mutex
	reqID     atomic.Uint64
	pending   map[uint64]chan json.RawMessage
	pendingMu sync.Mutex
	onUpdate  func() // callback when server sends notification
}

// NewSnapcastClient creates a new Snapcast client.
func NewSnapcastClient(log *zap.Logger, serverURL string) *SnapcastClient {
	return &SnapcastClient{
		log:       log,
		serverURL: serverURL,
		pending:   make(map[uint64]chan json.RawMessage),
	}
}

// SetUpdateCallback sets the callback for server notifications.
func (c *SnapcastClient) SetUpdateCallback(cb func()) {
	c.onUpdate = cb
}

// Connect establishes a connection to the Snapcast server.
func (c *SnapcastClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil || c.tcpConn != nil {
		return nil // already connected
	}

	u, err := url.Parse(c.serverURL)
	if err != nil {
		return fmt.Errorf("invalid server url: %w", err)
	}

	if u.Scheme == "tcp" {
		d := net.Dialer{Timeout: 10 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", u.Host)
		if err != nil {
			return fmt.Errorf("tcp dial: %w", err)
		}
		c.tcpConn = conn
		c.isTCP = true
		go c.readLoopTCP()
		return nil
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	headers := http.Header{}
	// Some servers require Origin to be set
	// We'll set it same as the target URL or a generic one
	headers.Set("Origin", c.serverURL)

	conn, _, err := dialer.DialContext(ctx, c.serverURL, headers)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	c.conn = conn
	go c.readLoop()
	return nil
}

// Close closes the connection.
func (c *SnapcastClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var err error
	if c.conn != nil {
		err = c.conn.Close()
		c.conn = nil
	}
	if c.tcpConn != nil {
		if e := c.tcpConn.Close(); e != nil && err == nil {
			err = e
		}
		c.tcpConn = nil
	}
	return err
}

// readLoopTCP reads messages from the TCP connection.
func (c *SnapcastClient) readLoopTCP() {
	c.mu.Lock()
	conn := c.tcpConn
	c.mu.Unlock()
	if conn == nil {
		return
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		c.handleMessage(scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		c.log.Debug("tcp read error", zap.Error(err))
	}
	c.Close()
}

// readLoop reads messages from the WebSocket.
func (c *SnapcastClient) readLoop() {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			c.log.Debug("websocket read error", zap.Error(err))
			c.Close()
			return
		}

		c.handleMessage(message)
	}
}

// handleMessage dispatches incoming messages.
func (c *SnapcastClient) handleMessage(data []byte) {
	var msg struct {
		ID     *uint64         `json:"id,omitempty"`
		Method string          `json:"method,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// Response to a request
	if msg.ID != nil {
		c.pendingMu.Lock()
		ch, ok := c.pending[*msg.ID]
		if ok {
			delete(c.pending, *msg.ID)
		}
		c.pendingMu.Unlock()
		if ok && ch != nil {
			ch <- msg.Result
		}
		return
	}

	// Notification from server (e.g. Client.OnVolumeChanged)
	if msg.Method != "" && c.onUpdate != nil {
		c.onUpdate()
	}
}

// call sends a JSON-RPC request and waits for response.
func (c *SnapcastClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	conn := c.conn
	tcpConn := c.tcpConn
	isTCP := c.isTCP
	c.mu.Unlock()

	if conn == nil && tcpConn == nil {
		return nil, fmt.Errorf("not connected")
	}

	id := c.reqID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Create response channel
	respCh := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	// Send request
	c.mu.Lock()
	if isTCP {
		data = append(data, '\n')
		_, err = c.tcpConn.Write(data)
	} else if c.conn != nil {
		err = c.conn.WriteMessage(websocket.TextMessage, data)
	} else {
		err = fmt.Errorf("connection lost")
	}
	c.mu.Unlock()

	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	// Wait for response
	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case result := <-respCh:
		return result, nil
	}
}

// ServerStatus represents the Snapcast server status.
type ServerStatus struct {
	Server struct {
		Groups  []SnapGroup  `json:"groups"`
		Streams []SnapStream `json:"streams"`
	} `json:"server"`
}

// SnapGroup represents a Snapcast group.
type SnapGroup struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Stream  string       `json:"stream_id"`
	Muted   bool         `json:"muted"`
	Clients []SnapClient `json:"clients"`
}

// SnapClient represents a Snapcast client.
type SnapClient struct {
	ID        string `json:"id"`
	Connected bool   `json:"connected"`
	Config    struct {
		Name   string `json:"name"`
		Volume struct {
			Percent int  `json:"percent"`
			Muted   bool `json:"muted"`
		} `json:"volume"`
	} `json:"config"`
	Host struct {
		MAC  string `json:"mac"`
		IP   string `json:"ip"`
		Name string `json:"name"`
	} `json:"host"`
}

// SnapStream represents a Snapcast stream (audio source).
type SnapStream struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	URI    struct {
		Raw    string `json:"raw"`
		Scheme string `json:"scheme"`
		Host   string `json:"host"`
		Path   string `json:"path"`
		Query  struct {
			Name string `json:"name"`
		} `json:"query"`
	} `json:"uri"`
}

// GetStatus retrieves the current server status.
func (c *SnapcastClient) GetStatus(ctx context.Context) (*ServerStatus, error) {
	result, err := c.call(ctx, "Server.GetStatus", nil)
	if err != nil {
		return nil, err
	}
	var status ServerStatus
	if err := json.Unmarshal(result, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// SetClientVolume sets the volume for a client.
func (c *SnapcastClient) SetClientVolume(ctx context.Context, clientID string, percent int, muted bool) error {
	params := map[string]any{
		"id": clientID,
		"volume": map[string]any{
			"percent": percent,
			"muted":   muted,
		},
	}
	_, err := c.call(ctx, "Client.SetVolume", params)
	return err
}

// SetGroupStream sets the stream for a group.
func (c *SnapcastClient) SetGroupStream(ctx context.Context, groupID, streamID string) error {
	params := map[string]any{
		"id":        groupID,
		"stream_id": streamID,
	}
	_, err := c.call(ctx, "Group.SetStream", params)
	return err
}
