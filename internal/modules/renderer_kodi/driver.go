package rendererkodi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

// Driver implements renderer_core.Driver for Kodi via JSON-RPC.
type Driver struct {
	baseURL      string
	http         *http.Client
	username     string
	password     string
	mu           sync.Mutex
	lastPlayerID *int
}

// NewDriver creates a Kodi JSON-RPC driver.
func NewDriver(baseURL string, username string, password string, timeout time.Duration) (*Driver, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("base_url required")
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(parsed.Path, "/jsonrpc") {
		parsed.Path = path.Join(parsed.Path, "/jsonrpc")
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &Driver{
		baseURL:  parsed.String(),
		http:     &http.Client{Timeout: timeout},
		username: username,
		password: password,
	}, nil
}

func (d *Driver) Play(url string, positionMS int64) error {
	if url == "" {
		return errors.New("url required")
	}
	if _, err := d.rpc("Player.Open", map[string]any{
		"item": map[string]any{"file": url},
	}); err != nil {
		return err
	}
	playerID, err := d.activePlayerID()
	if err != nil {
		return err
	}
	if positionMS > 0 {
		if err := d.seekPlayer(playerID, positionMS); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) Pause() error {
	playerID, err := d.activePlayerID()
	if err != nil {
		return err
	}
	_, err = d.rpc("Player.PlayPause", map[string]any{"playerid": playerID, "play": false})
	return err
}

func (d *Driver) Resume() error {
	playerID, err := d.activePlayerID()
	if err != nil {
		return err
	}
	_, err = d.rpc("Player.PlayPause", map[string]any{"playerid": playerID, "play": true})
	return err
}

func (d *Driver) Stop() error {
	playerID, err := d.activePlayerID()
	if err != nil {
		return err
	}
	_, err = d.rpc("Player.Stop", map[string]any{"playerid": playerID})
	return err
}

func (d *Driver) Seek(positionMS int64) error {
	playerID, err := d.activePlayerID()
	if err != nil {
		return err
	}
	return d.seekPlayer(playerID, positionMS)
}

func (d *Driver) seekPlayer(playerID int, positionMS int64) error {
	_, err := d.rpc("Player.Seek", map[string]any{
		"playerid": playerID,
		"value":    toTimeObject(positionMS),
	})
	return err
}

func (d *Driver) SetVolume(volume float64) error {
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	level := int(volume*100 + 0.5)
	_, err := d.rpc("Application.SetVolume", map[string]any{"volume": level})
	return err
}

func (d *Driver) SetMute(mute bool) error {
	_, err := d.rpc("Application.SetMute", map[string]any{"mute": mute})
	return err
}

func (d *Driver) Position() (int64, int64, bool) {
	playerID, err := d.activePlayerID()
	if err != nil {
		return 0, 0, false
	}
	raw, err := d.rpc("Player.GetProperties", map[string]any{
		"playerid":   playerID,
		"properties": []string{"time", "totaltime"},
	})
	if err != nil {
		return 0, 0, false
	}
	var props playerProperties
	if err := json.Unmarshal(raw, &props); err != nil {
		return 0, 0, false
	}
	return fromTimeObject(props.Time), fromTimeObject(props.TotalTime), true
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type activePlayer struct {
	PlayerID int    `json:"playerid"`
	Type     string `json:"type"`
}

type playerProperties struct {
	Time      timeObject `json:"time"`
	TotalTime timeObject `json:"totaltime"`
}

type timeObject struct {
	Hours        int `json:"hours"`
	Minutes      int `json:"minutes"`
	Seconds      int `json:"seconds"`
	Milliseconds int `json:"milliseconds"`
}

func (d *Driver) rpc(method string, params interface{}) (json.RawMessage, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      int(time.Now().UnixNano()),
		Method:  method,
		Params:  params,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest("POST", d.baseURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if d.username != "" || d.password != "" {
		httpReq.SetBasicAuth(d.username, d.password)
	}
	resp, err := d.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kodi error: %s", strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var rpcResp rpcResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("kodi error: %s", rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (d *Driver) activePlayerID() (int, error) {
	raw, err := d.rpc("Player.GetActivePlayers", nil)
	if err != nil {
		return 0, err
	}
	var players []activePlayer
	if err := json.Unmarshal(raw, &players); err != nil {
		return 0, err
	}
	if len(players) == 0 {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.lastPlayerID != nil {
			return *d.lastPlayerID, nil
		}
		return 0, errors.New("no active player")
	}
	playerID := players[0].PlayerID
	d.mu.Lock()
	d.lastPlayerID = &playerID
	d.mu.Unlock()
	return playerID, nil
}

func toTimeObject(ms int64) timeObject {
	if ms < 0 {
		ms = 0
	}
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	millis := ms % 1000
	return timeObject{
		Hours:        int(hours),
		Minutes:      int(minutes),
		Seconds:      int(seconds),
		Milliseconds: int(millis),
	}
}

func fromTimeObject(obj timeObject) int64 {
	return int64(obj.Hours)*3600*1000 +
		int64(obj.Minutes)*60*1000 +
		int64(obj.Seconds)*1000 +
		int64(obj.Milliseconds)
}
