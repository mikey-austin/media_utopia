package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Options configures the MQTT client.
type Options struct {
	BrokerURL string
	ClientID  string
	Username  string
	Password  string
	TLSCA     string
	TLSCert   string
	TLSKey    string
	TopicBase string
	Timeout   time.Duration
}

// Client is an MQTT adapter implementing the Broker port.
type Client struct {
	client     paho.Client
	replyTopic string
	topicBase  string
	timeout    time.Duration

	mu            sync.Mutex
	replyHandlers map[string]chan mu.ReplyEnvelope
}

// NewClient creates and connects an MQTT client.
func NewClient(opts Options) (*Client, error) {
	if opts.TopicBase == "" {
		opts.TopicBase = mu.BaseTopic
	}
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Second
	}

	c := &Client{
		replyTopic:    mu.TopicReply(opts.TopicBase, opts.ClientID),
		topicBase:     opts.TopicBase,
		timeout:       opts.Timeout,
		replyHandlers: map[string]chan mu.ReplyEnvelope{},
	}

	clientOpts := paho.NewClientOptions().AddBroker(opts.BrokerURL)
	clientOpts.SetClientID(opts.ClientID)
	clientOpts.SetConnectTimeout(opts.Timeout)
	clientOpts.SetAutoReconnect(true)
	clientOpts.SetOnConnectHandler(func(client paho.Client) {
		topic := c.replyTopic
		token := client.Subscribe(topic, 1, c.handleReply)
		token.Wait()
	})

	if opts.Username != "" {
		clientOpts.SetUsername(opts.Username)
		clientOpts.SetPassword(opts.Password)
	}

	tlsConfig, err := buildTLSConfig(opts.TLSCA, opts.TLSCert, opts.TLSKey)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		clientOpts.SetTLSConfig(tlsConfig)
	}

	c.client = paho.NewClient(clientOpts)
	if token := c.client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	if token := c.client.Subscribe(c.replyTopic, 1, c.handleReply); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	return c, nil
}

// ReplyTopic returns the topic used for replies.
func (c *Client) ReplyTopic() string {
	return c.replyTopic
}

// PublishCommand publishes a command and waits for a reply.
func (c *Client) PublishCommand(ctx context.Context, nodeID string, cmd mu.CommandEnvelope) (mu.ReplyEnvelope, error) {
	req, err := json.Marshal(cmd)
	if err != nil {
		return mu.ReplyEnvelope{}, fmt.Errorf("marshal command: %w", err)
	}

	replyCh := make(chan mu.ReplyEnvelope, 1)
	c.mu.Lock()
	c.replyHandlers[cmd.ID] = replyCh
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.replyHandlers, cmd.ID)
		c.mu.Unlock()
	}()

	topic := mu.TopicCommands(c.topicBase, nodeID)
	if token := c.client.Publish(topic, 1, false, req); token.Wait() && token.Error() != nil {
		return mu.ReplyEnvelope{}, token.Error()
	}

	select {
	case <-ctx.Done():
		return mu.ReplyEnvelope{}, ctx.Err()
	case reply := <-replyCh:
		return reply, nil
	case <-time.After(c.timeout):
		return mu.ReplyEnvelope{}, errors.New("timeout waiting for reply")
	}
}

// ListPresence collects retained presence messages.
func (c *Client) ListPresence(ctx context.Context) ([]mu.Presence, error) {
	collect := make(map[string]mu.Presence)
	muLock := sync.Mutex{}

	handler := func(_ paho.Client, msg paho.Message) {
		var presence mu.Presence
		if err := json.Unmarshal(msg.Payload(), &presence); err != nil {
			return
		}
		muLock.Lock()
		collect[presence.NodeID] = presence
		muLock.Unlock()
	}

	topic := fmt.Sprintf("%s/node/+/presence", c.topicBase)
	if token := c.client.Subscribe(topic, 1, handler); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	defer func() {
		token := c.client.Unsubscribe(topic)
		token.Wait()
	}()

	wait := time.NewTimer(250 * time.Millisecond)
	select {
	case <-ctx.Done():
		wait.Stop()
	case <-wait.C:
	}

	muLock.Lock()
	defer muLock.Unlock()
	out := make([]mu.Presence, 0, len(collect))
	for _, presence := range collect {
		out = append(out, presence)
	}
	return out, nil
}

// GetRendererState returns the retained renderer state.
func (c *Client) GetRendererState(ctx context.Context, nodeID string) (mu.RendererState, error) {
	stateCh := make(chan mu.RendererState, 1)
	handler := func(_ paho.Client, msg paho.Message) {
		var state mu.RendererState
		if err := json.Unmarshal(msg.Payload(), &state); err != nil {
			return
		}
		select {
		case stateCh <- state:
		default:
		}
	}

	topic := mu.TopicState(c.topicBase, nodeID)
	if token := c.client.Subscribe(topic, 1, handler); token.Wait() && token.Error() != nil {
		return mu.RendererState{}, token.Error()
	}
	defer func() {
		token := c.client.Unsubscribe(topic)
		token.Wait()
	}()

	select {
	case <-ctx.Done():
		return mu.RendererState{}, ctx.Err()
	case state := <-stateCh:
		return state, nil
	case <-time.After(c.timeout):
		return mu.RendererState{}, errors.New("timeout waiting for state")
	}
}

// WatchRenderer streams state and events for a renderer.
func (c *Client) WatchRenderer(ctx context.Context, nodeID string) (<-chan mu.RendererState, <-chan mu.Event, <-chan error) {
	stateCh := make(chan mu.RendererState, 8)
	eventCh := make(chan mu.Event, 8)
	errCh := make(chan error, 1)

	stateHandler := func(_ paho.Client, msg paho.Message) {
		var state mu.RendererState
		if err := json.Unmarshal(msg.Payload(), &state); err != nil {
			return
		}
		select {
		case stateCh <- state:
		default:
		}
	}

	eventHandler := func(_ paho.Client, msg paho.Message) {
		var evt mu.Event
		if err := json.Unmarshal(msg.Payload(), &evt); err != nil {
			return
		}
		select {
		case eventCh <- evt:
		default:
		}
	}

	stateTopic := mu.TopicState(c.topicBase, nodeID)
	eventTopic := mu.TopicEvents(c.topicBase, nodeID)

	if token := c.client.Subscribe(stateTopic, 1, stateHandler); token.Wait() && token.Error() != nil {
		errCh <- token.Error()
		return stateCh, eventCh, errCh
	}
	if token := c.client.Subscribe(eventTopic, 1, eventHandler); token.Wait() && token.Error() != nil {
		errCh <- token.Error()
		return stateCh, eventCh, errCh
	}

	go func() {
		<-ctx.Done()
		c.client.Unsubscribe(stateTopic, eventTopic)
		close(stateCh)
		close(eventCh)
		close(errCh)
	}()

	return stateCh, eventCh, errCh
}

func (c *Client) handleReply(_ paho.Client, msg paho.Message) {
	var reply mu.ReplyEnvelope
	if err := json.Unmarshal(msg.Payload(), &reply); err != nil {
		return
	}

	c.mu.Lock()
	ch, ok := c.replyHandlers[reply.ID]
	c.mu.Unlock()
	if !ok {
		return
	}

	select {
	case ch <- reply:
	default:
	}
}

func buildTLSConfig(caPath, certPath, keyPath string) (*tls.Config, error) {
	if caPath == "" && certPath == "" && keyPath == "" {
		return nil, nil
	}

	config := &tls.Config{}
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("failed to parse CA bundle")
		}
		config.RootCAs = pool
	}

	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return nil, errors.New("both tls cert and key are required")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, err
		}
		config.Certificates = []tls.Certificate{cert}
	}

	return config, nil
}
