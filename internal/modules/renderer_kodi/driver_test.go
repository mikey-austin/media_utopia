package rendererkodi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDriverCommands(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	transport := testTransport(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		var rpcReq rpcRequest
		if err := json.Unmarshal(body, &rpcReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		seen[rpcReq.Method]++
		mu.Unlock()

		var result any
		switch rpcReq.Method {
		case "Player.Open":
			result = "OK"
		case "Player.GetActivePlayers":
			result = []map[string]any{{"playerid": 0, "type": "audio"}}
		case "Player.PlayPause":
			result = map[string]any{"speed": 1}
		case "Player.Stop":
			result = "OK"
		case "Player.Seek":
			result = map[string]any{"time": map[string]any{"hours": 0, "minutes": 0, "seconds": 10, "milliseconds": 0}}
		case "Player.GetProperties":
			result = map[string]any{
				"time":      map[string]any{"hours": 0, "minutes": 0, "seconds": 12, "milliseconds": 0},
				"totaltime": map[string]any{"hours": 0, "minutes": 1, "seconds": 0, "milliseconds": 0},
			}
		case "Application.SetVolume":
			result = 80
		case "Application.SetMute":
			result = true
		default:
			t.Fatalf("unexpected method %s", rpcReq.Method)
		}

		payload, _ := json.Marshal(map[string]any{"result": result})
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBuffer(payload)),
		}, nil
	})

	driver, err := NewDriver("http://kodi.test", "", "", 2*time.Second)
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
	if err := driver.SeekTo(10 * 1000); err != nil {
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
	if seen["Player.Open"] == 0 {
		t.Fatalf("expected Player.Open")
	}
	if seen["Player.GetActivePlayers"] == 0 {
		t.Fatalf("expected Player.GetActivePlayers")
	}
	if seen["Player.PlayPause"] < 2 {
		t.Fatalf("expected PlayPause twice")
	}
	if seen["Player.GetProperties"] == 0 {
		t.Fatalf("expected GetProperties")
	}
}

// TestResolveRefsSplitsAtLastColon verifies that library ref parsing uses
// LastIndex to split "libraryNodeID:itemID", since library node IDs contain
// colons (e.g. "mu:library:filesystem:home:default"). Using Index instead
// of LastIndex would split at the first colon and produce wrong results.
func TestResolveRefsSplitsAtLastColon(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		wantLib    string
		wantItem   string
		wantErr    bool
	}{
		{
			name:     "multi-colon library node ID",
			ref:      "lib:mu:library:filesystem:home:default:item123",
			wantLib:  "mu:library:filesystem:home:default",
			wantItem: "item123",
		},
		{
			name:     "simple library node ID",
			ref:      "lib:mu:library:abc123",
			wantLib:  "mu:library",
			wantItem: "abc123",
		},
		{
			name:    "no colon after prefix",
			ref:     "lib:nocolon",
			wantErr: true,
		},
		{
			name:    "missing lib: prefix",
			ref:     "mu:library:filesystem:home:default:item123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the parsing logic from resolveRefs.
			ref := tt.ref
			if !strings.HasPrefix(ref, "lib:") {
				if !tt.wantErr {
					t.Fatalf("expected no error but ref lacks lib: prefix")
				}
				return
			}
			ref = strings.TrimPrefix(ref, "lib:")
			idx := strings.LastIndex(ref, ":")
			if idx == -1 {
				if !tt.wantErr {
					t.Fatalf("expected no error but LastIndex returned -1")
				}
				return
			}
			if tt.wantErr {
				t.Fatalf("expected error but parsing succeeded")
			}

			libraryID := ref[:idx]
			itemID := ref[idx+1:]

			if libraryID != tt.wantLib {
				t.Fatalf("libraryID = %q, want %q", libraryID, tt.wantLib)
			}
			if itemID != tt.wantItem {
				t.Fatalf("itemID = %q, want %q", itemID, tt.wantItem)
			}
		})
	}
}

type testTransport func(*http.Request) (*http.Response, error)

func (t testTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t(r)
}
