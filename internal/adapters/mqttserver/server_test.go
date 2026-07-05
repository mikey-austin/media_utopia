package mqttserver

import (
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

type fakeToken struct{}

func (t *fakeToken) Wait() bool                     { return true }
func (t *fakeToken) WaitTimeout(time.Duration) bool { return true }
func (t *fakeToken) Error() error                   { return nil }
func (t *fakeToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

type fakePahoClient struct {
	mu         sync.Mutex
	subscribed map[string]byte
}

func newFakePahoClient() *fakePahoClient {
	return &fakePahoClient{subscribed: map[string]byte{}}
}

func (f *fakePahoClient) IsConnected() bool      { return true }
func (f *fakePahoClient) IsConnectionOpen() bool { return true }
func (f *fakePahoClient) Connect() paho.Token    { return &fakeToken{} }
func (f *fakePahoClient) Disconnect(uint)        {}

func (f *fakePahoClient) Publish(string, byte, bool, interface{}) paho.Token {
	return &fakeToken{}
}

func (f *fakePahoClient) Subscribe(topic string, qos byte, _ paho.MessageHandler) paho.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed[topic] = qos
	return &fakeToken{}
}

func (f *fakePahoClient) SubscribeMultiple(filters map[string]byte, _ paho.MessageHandler) paho.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	for topic, qos := range filters {
		f.subscribed[topic] = qos
	}
	return &fakeToken{}
}

func (f *fakePahoClient) Unsubscribe(topics ...string) paho.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, topic := range topics {
		delete(f.subscribed, topic)
	}
	return &fakeToken{}
}

func (f *fakePahoClient) AddRoute(string, paho.MessageHandler) {}

func (f *fakePahoClient) OptionsReader() paho.ClientOptionsReader {
	return paho.ClientOptionsReader{}
}

// dropSubscriptions simulates a broker reconnect with clean session: the
// broker forgets everything the client subscribed to.
func (f *fakePahoClient) dropSubscriptions() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed = map[string]byte{}
}

func (f *fakePahoClient) snapshot() map[string]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]byte, len(f.subscribed))
	for topic, qos := range f.subscribed {
		out[topic] = qos
	}
	return out
}

func newTestClient(fake *fakePahoClient) *Client {
	return &Client{
		client:        fake,
		log:           zap.NewNop(),
		timeout:       time.Second,
		subscriptions: map[string]subscription{},
	}
}

func TestOnConnectResubscribes(t *testing.T) {
	fake := newFakePahoClient()
	client := newTestClient(fake)

	handler := func(paho.Client, paho.Message) {}
	if err := client.Subscribe("mu/v1/node/renderer/cmd", 1, handler); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if err := client.Subscribe("mu/v1/node/library/cmd", 0, handler); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	// Simulate a broker reconnect that dropped all subscriptions.
	fake.dropSubscriptions()
	client.onConnect(fake)

	subs := fake.snapshot()
	if len(subs) != 2 {
		t.Fatalf("expected 2 restored subscriptions, got %d", len(subs))
	}
	if qos, ok := subs["mu/v1/node/renderer/cmd"]; !ok || qos != 1 {
		t.Fatalf("expected renderer cmd resubscribed at qos 1, got qos=%d ok=%v", qos, ok)
	}
	if qos, ok := subs["mu/v1/node/library/cmd"]; !ok || qos != 0 {
		t.Fatalf("expected library cmd resubscribed at qos 0, got qos=%d ok=%v", qos, ok)
	}
}

func TestOnConnectSkipsUnsubscribedTopics(t *testing.T) {
	fake := newFakePahoClient()
	client := newTestClient(fake)

	handler := func(paho.Client, paho.Message) {}
	if err := client.Subscribe("mu/v1/node/renderer/cmd", 1, handler); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if err := client.Subscribe("mu/v1/node/library/cmd", 1, handler); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	if err := client.Unsubscribe("mu/v1/node/library/cmd"); err != nil {
		t.Fatalf("unsubscribe failed: %v", err)
	}

	fake.dropSubscriptions()
	client.onConnect(fake)

	subs := fake.snapshot()
	if len(subs) != 1 {
		t.Fatalf("expected 1 restored subscription, got %d", len(subs))
	}
	if _, ok := subs["mu/v1/node/renderer/cmd"]; !ok {
		t.Fatalf("expected renderer cmd resubscribed")
	}
}

func TestOnConnectFirstConnectIsNoop(t *testing.T) {
	fake := newFakePahoClient()
	client := newTestClient(fake)

	// No subscriptions yet: the initial connect must not subscribe anything.
	client.onConnect(fake)

	if subs := fake.snapshot(); len(subs) != 0 {
		t.Fatalf("expected no subscriptions on first connect, got %d", len(subs))
	}
}
