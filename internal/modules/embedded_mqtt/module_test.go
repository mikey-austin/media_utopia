package embeddedmqtt

import (
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"go.uber.org/zap"
)

func TestNewServerAllowAnonymous(t *testing.T) {
	server, err := newServer(zap.NewNop(), Config{AllowAnonymous: true})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	if server == nil {
		t.Fatalf("expected server")
	}
}

func TestNewServerRequiresAuthConfig(t *testing.T) {
	_, err := newServer(zap.NewNop(), Config{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestInlinePublishSubscribe(t *testing.T) {
	server, err := newServer(zap.NewNop(), Config{AllowAnonymous: true})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	received := make(chan packets.Packet, 1)
	handler := func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		received <- pk
	}
	if err := server.Subscribe("test/#", 1, handler); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := server.Publish("test/topic", []byte("payload"), false, 0); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case pk := <-received:
		if string(pk.Payload) != "payload" {
			t.Fatalf("unexpected payload")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timeout waiting for message")
	}
}

func TestBrokerURL(t *testing.T) {
	if BrokerURL("127.0.0.1:1883", false) != "mqtt://127.0.0.1:1883" {
		t.Fatalf("expected mqtt scheme")
	}
	if BrokerURL("127.0.0.1:8883", true) != "mqtts://127.0.0.1:8883" {
		t.Fatalf("expected mqtts scheme")
	}
}

func TestNewModuleValidation(t *testing.T) {
	t.Run("nil logger falls back to slog default", func(t *testing.T) {
		// NewModule with nil logger should still succeed since newSlogLogger
		// falls back to slog.Default().
		m, err := NewModule(nil, Config{AllowAnonymous: true})
		if err != nil {
			t.Fatalf("expected nil logger to be accepted: %v", err)
		}
		if m == nil {
			t.Fatalf("expected non-nil module")
		}
	})

	t.Run("missing auth config returns error", func(t *testing.T) {
		_, err := NewModule(zap.NewNop(), Config{})
		if err == nil {
			t.Fatalf("expected error when no auth is configured")
		}
	})

	t.Run("empty listen defaults to localhost", func(t *testing.T) {
		m, err := NewModule(zap.NewNop(), Config{AllowAnonymous: true})
		if err != nil {
			t.Fatalf("NewModule: %v", err)
		}
		if m.config.Listen != "127.0.0.1:1883" {
			t.Fatalf("expected default listen, got %q", m.config.Listen)
		}
	})

	t.Run("whitespace-only listen defaults to localhost", func(t *testing.T) {
		m, err := NewModule(zap.NewNop(), Config{AllowAnonymous: true, Listen: "   "})
		if err != nil {
			t.Fatalf("NewModule: %v", err)
		}
		if m.config.Listen != "127.0.0.1:1883" {
			t.Fatalf("expected default listen, got %q", m.config.Listen)
		}
	})

	t.Run("explicit listen is preserved", func(t *testing.T) {
		m, err := NewModule(zap.NewNop(), Config{AllowAnonymous: true, Listen: "0.0.0.0:9999"})
		if err != nil {
			t.Fatalf("NewModule: %v", err)
		}
		if m.config.Listen != "0.0.0.0:9999" {
			t.Fatalf("expected custom listen, got %q", m.config.Listen)
		}
	})

	t.Run("username auth without password succeeds", func(t *testing.T) {
		m, err := NewModule(zap.NewNop(), Config{Username: "admin"})
		if err != nil {
			t.Fatalf("NewModule: %v", err)
		}
		if m == nil {
			t.Fatalf("expected non-nil module")
		}
	})
}

func TestBrokerURLFormats(t *testing.T) {
	tests := []struct {
		name       string
		listen     string
		tlsEnabled bool
		want       string
	}{
		{"localhost plain", "127.0.0.1:1883", false, "mqtt://127.0.0.1:1883"},
		{"localhost tls", "127.0.0.1:8883", true, "mqtts://127.0.0.1:8883"},
		{"all interfaces plain", "0.0.0.0:1883", false, "mqtt://0.0.0.0:1883"},
		{"all interfaces tls", "0.0.0.0:8883", true, "mqtts://0.0.0.0:8883"},
		{"ipv6 loopback plain", "[::1]:1883", false, "mqtt://[::1]:1883"},
		{"ipv6 loopback tls", "[::1]:8883", true, "mqtts://[::1]:8883"},
		{"hostname plain", "broker.local:1883", false, "mqtt://broker.local:1883"},
		{"hostname tls", "broker.local:8883", true, "mqtts://broker.local:8883"},
		{"non-standard port plain", "10.0.0.5:9999", false, "mqtt://10.0.0.5:9999"},
		{"non-standard port tls", "10.0.0.5:9999", true, "mqtts://10.0.0.5:9999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BrokerURL(tt.listen, tt.tlsEnabled)
			if got != tt.want {
				t.Fatalf("BrokerURL(%q, %v) = %q, want %q", tt.listen, tt.tlsEnabled, got, tt.want)
			}
		})
	}
}

func TestRetainedMessages(t *testing.T) {
	server, err := newServer(zap.NewNop(), Config{AllowAnonymous: true})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	// Publish a retained message before any subscriber exists.
	if err := server.Publish("retain/topic", []byte("retained-value"), true, 0); err != nil {
		t.Fatalf("publish retained: %v", err)
	}

	// Subscribe after the retained message was published.
	received := make(chan packets.Packet, 1)
	handler := func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		received <- pk
	}
	if err := server.Subscribe("retain/topic", 1, handler); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case pk := <-received:
		if string(pk.Payload) != "retained-value" {
			t.Fatalf("unexpected payload: %q", string(pk.Payload))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for retained message")
	}

	// A second subscriber should also receive the retained message.
	received2 := make(chan packets.Packet, 1)
	handler2 := func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		received2 <- pk
	}
	if err := server.Subscribe("retain/topic", 2, handler2); err != nil {
		t.Fatalf("subscribe second: %v", err)
	}

	select {
	case pk := <-received2:
		if string(pk.Payload) != "retained-value" {
			t.Fatalf("unexpected payload for second subscriber: %q", string(pk.Payload))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for retained message on second subscriber")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	server, err := newServer(zap.NewNop(), Config{AllowAnonymous: true})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	const subscriberCount = 5
	channels := make([]chan packets.Packet, subscriberCount)

	for i := 0; i < subscriberCount; i++ {
		ch := make(chan packets.Packet, 1)
		channels[i] = ch
		subID := i + 1
		handler := func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
			ch <- pk
		}
		if err := server.Subscribe("multi/topic", subID, handler); err != nil {
			t.Fatalf("subscribe[%d]: %v", i, err)
		}
	}

	if err := server.Publish("multi/topic", []byte("broadcast"), false, 0); err != nil {
		t.Fatalf("publish: %v", err)
	}

	for i, ch := range channels {
		select {
		case pk := <-ch:
			if string(pk.Payload) != "broadcast" {
				t.Fatalf("subscriber %d: unexpected payload %q", i, string(pk.Payload))
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("subscriber %d: timeout waiting for message", i)
		}
	}
}
