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
