package renderervlc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Driver implements renderer_core.Driver for VLC via HTTP RC.
type Driver struct {
	baseURL    string
	http       *http.Client
	username   string
	password   string
	lastVolume int
}

// NewDriver creates a VLC HTTP RC driver.
func NewDriver(baseURL string, username string, password string, timeout time.Duration) (*Driver, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("base_url required")
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &Driver{
		baseURL:    strings.TrimRight(baseURL, "/"),
		http:       &http.Client{Timeout: timeout},
		username:   username,
		password:   password,
		lastVolume: 256,
	}, nil
}

func (d *Driver) Play(streamURL string, positionMS int64) error {
	if streamURL == "" {
		return errors.New("url required")
	}
	_, _ = d.request(url.Values{"command": []string{"pl_stop"}})
	_, _ = d.request(url.Values{"command": []string{"pl_empty"}})
	if _, err := d.request(url.Values{
		"command": []string{"in_play"},
		"input":   []string{streamURL},
	}); err != nil {
		return err
	}
	_, _ = d.request(url.Values{"command": []string{"pl_play"}})
	if positionMS > 0 {
		return d.Seek(positionMS)
	}
	return nil
}

func (d *Driver) Pause() error {
	_, err := d.request(url.Values{"command": []string{"pl_pause"}})
	return err
}

func (d *Driver) Resume() error {
	_, err := d.request(url.Values{"command": []string{"pl_play"}})
	return err
}

func (d *Driver) Stop() error {
	_, err := d.request(url.Values{"command": []string{"pl_stop"}})
	return err
}

func (d *Driver) Seek(positionMS int64) error {
	seconds := int64(0)
	if positionMS > 0 {
		seconds = positionMS / 1000
	}
	_, err := d.request(url.Values{
		"command": []string{"seek"},
		"val":     []string{strconv.FormatInt(seconds, 10)},
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
	level := int(volume*256 + 0.5)
	if level > 0 {
		d.lastVolume = level
	}
	_, err := d.request(url.Values{
		"command": []string{"volume"},
		"val":     []string{strconv.Itoa(level)},
	})
	return err
}

func (d *Driver) SetMute(mute bool) error {
	level := 0
	if !mute {
		level = d.lastVolume
		if level <= 0 {
			level = 256
		}
	}
	_, err := d.request(url.Values{
		"command": []string{"volume"},
		"val":     []string{strconv.Itoa(level)},
	})
	return err
}

func (d *Driver) Position() (int64, int64, bool) {
	payload, err := d.request(nil)
	if err != nil {
		return 0, 0, false
	}
	var status vlcStatus
	if err := json.Unmarshal(payload, &status); err != nil {
		return 0, 0, false
	}
	pos := int64(status.Time) * 1000
	dur := int64(status.Length) * 1000
	return pos, dur, true
}

type vlcStatus struct {
	State  string `json:"state"`
	Time   int64  `json:"time"`
	Length int64  `json:"length"`
}

func (d *Driver) request(values url.Values) ([]byte, error) {
	endpoint := d.baseURL + "/requests/status.json"
	if values != nil && len(values) > 0 {
		endpoint = endpoint + "?" + values.Encode()
	}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	if d.username != "" || d.password != "" {
		req.SetBasicAuth(d.username, d.password)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("vlc error: %s", msg)
	}
	return body, nil
}
