package renderervlc

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDriverCommands(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		cmd := query.Get("command")
		mu.Lock()
		if cmd != "" {
			seen[cmd]++
		}
		mu.Unlock()

		status := map[string]any{
			"state":  "playing",
			"time":   12,
			"length": 60,
		}
		payload, _ := json.Marshal(status)
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBuffer(payload)),
		}, nil
	})

	driver, err := NewDriver("http://vlc.test:8080", "", "", 2*time.Second)
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	driver.http = &http.Client{Transport: transport}

	if err := driver.Play("http://example.com/track.mp3", 0); err != nil {
		t.Fatalf("play: %v", err)
	}
	if err := driver.Pause(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := driver.Resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := driver.Seek(10 * 1000); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if err := driver.SetVolume(0.8); err != nil {
		t.Fatalf("volume: %v", err)
	}
	if err := driver.SetMute(true); err != nil {
		t.Fatalf("mute: %v", err)
	}
	pos, dur, ok := driver.Position()
	if !ok || pos == 0 || dur == 0 {
		t.Fatalf("expected position and duration")
	}
	if err := driver.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if seen["in_play"] == 0 {
		t.Fatalf("expected in_play")
	}
	if seen["pl_pause"] == 0 {
		t.Fatalf("expected pl_pause")
	}
	if seen["pl_play"] == 0 {
		t.Fatalf("expected pl_play")
	}
	if seen["seek"] == 0 {
		t.Fatalf("expected seek")
	}
	if seen["volume"] < 2 {
		t.Fatalf("expected volume commands")
	}
	if seen["pl_stop"] == 0 {
		t.Fatalf("expected pl_stop")
	}
}

type testTransport func(*http.Request) (*http.Response, error)

func (t testTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t(r)
}

func TestDriverQueryEncoding(t *testing.T) {
	var seen url.Values
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		seen = req.URL.Query()
		payload := []byte(`{"state":"playing","time":0,"length":0}`)
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBuffer(payload)),
		}, nil
	})

	driver, err := NewDriver("http://vlc.test", "", "", 2*time.Second)
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	driver.http = &http.Client{Transport: transport}

	urlWithSpace := "http://example.com/track name.mp3"
	if err := driver.Play(urlWithSpace, 0); err != nil {
		t.Fatalf("play: %v", err)
	}
	if strings.Contains(seen.Encode(), " ") {
		t.Fatalf("expected encoded query")
	}
}
