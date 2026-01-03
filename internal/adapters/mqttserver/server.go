package mqttserver

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
)

// Options configures the MQTT server client.
type Options struct {
	BrokerURL               string
	ClientID                string
	Username                string
	Password                string
	TLSCA                   string
	TLSCert                 string
	TLSKey                  string
	Timeout                 time.Duration
	Logger                  *zap.Logger
	Debug                   bool
	BreakerEnabled          bool
	BreakerName             string
	BreakerTimeout          time.Duration
	BreakerInterval         time.Duration
	BreakerMaxRequests      uint32
	BreakerFailureThreshold uint32
}

// Client wraps an MQTT connection for server modules.
type Client struct {
	client          paho.Client
	log             *zap.Logger
	debug           bool
	timeout         time.Duration
	breakerEnabled  bool
	breakerSettings gobreaker.Settings
	breakers        map[string]*gobreaker.CircuitBreaker
	breakerMu       sync.Mutex
}

// ErrPublishTimeout indicates the publish operation timed out.
var ErrPublishTimeout = errors.New("mqtt publish timeout")

// ErrPublishBlocked indicates publish was skipped due to an open breaker.
var ErrPublishBlocked = errors.New("mqtt publish blocked by breaker")

// ErrSubscribeTimeout indicates the subscribe operation timed out.
var ErrSubscribeTimeout = errors.New("mqtt subscribe timeout")

// ErrUnsubscribeTimeout indicates the unsubscribe operation timed out.
var ErrUnsubscribeTimeout = errors.New("mqtt unsubscribe timeout")

// NewClient connects to MQTT.
func NewClient(opts Options) (*Client, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}

	clientOpts := paho.NewClientOptions().AddBroker(opts.BrokerURL)
	clientOpts.SetClientID(opts.ClientID)
	clientOpts.SetConnectTimeout(opts.Timeout)
	clientOpts.SetAutoReconnect(true)

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

	client := paho.NewClient(clientOpts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	mqttClient := &Client{
		client:   client,
		log:      opts.Logger,
		debug:    opts.Debug,
		timeout:  opts.Timeout,
		breakers: map[string]*gobreaker.CircuitBreaker{},
	}
	mqttClient.configureBreaker(opts)
	return mqttClient, nil
}

// Publish publishes a message.
func (c *Client) Publish(topic string, qos byte, retained bool, payload []byte) error {
	if c.debug {
		c.log.Debug("mqtt publish", zap.String("topic", topic), zap.Int("bytes", len(payload)), zap.String("payload", truncatePayload(payload)))
	}
	breaker := c.breakerForTopic(topic)
	if breaker == nil {
		return c.publishWithTimeout(topic, qos, retained, payload)
	}
	if breaker.State() == gobreaker.StateOpen {
		c.log.Warn("mqtt breaker open", zap.String("topic", topic))
		return ErrPublishBlocked
	}
	_, err := breaker.Execute(func() (any, error) {
		return nil, c.publishWithTimeout(topic, qos, retained, payload)
	})
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		c.log.Warn("mqtt breaker open", zap.String("topic", topic))
		return ErrPublishBlocked
	}
	return err
}

// Subscribe subscribes to a topic.
func (c *Client) Subscribe(topic string, qos byte, handler paho.MessageHandler) error {
	if c.debug {
		c.log.Debug("mqtt subscribe", zap.String("topic", topic))
	}
	wrapped := handler
	if c.debug {
		wrapped = func(client paho.Client, msg paho.Message) {
			c.log.Debug("mqtt message", zap.String("topic", msg.Topic()), zap.Int("bytes", len(msg.Payload())), zap.String("payload", truncatePayload(msg.Payload())))
			handler(client, msg)
		}
	}
	if breaker := c.breakerForTopic(topic); breaker != nil {
		original := wrapped
		wrapped = func(client paho.Client, msg paho.Message) {
			if breaker.State() == gobreaker.StateOpen {
				c.log.Warn("mqtt breaker open, dropping message", zap.String("topic", msg.Topic()))
				return
			}
			original(client, msg)
		}
	}
	token := c.client.Subscribe(topic, qos, wrapped)
	if c.timeout > 0 && !token.WaitTimeout(c.timeout) {
		c.log.Warn("mqtt subscribe timeout", zap.String("topic", topic))
		return ErrSubscribeTimeout
	}
	if c.timeout <= 0 {
		token.Wait()
	}
	return token.Error()
}

// Unsubscribe unsubscribes from a topic.
func (c *Client) Unsubscribe(topic string) error {
	if c.debug {
		c.log.Debug("mqtt unsubscribe", zap.String("topic", topic))
	}
	token := c.client.Unsubscribe(topic)
	if c.timeout > 0 && !token.WaitTimeout(c.timeout) {
		c.log.Warn("mqtt unsubscribe timeout", zap.String("topic", topic))
		return ErrUnsubscribeTimeout
	}
	if c.timeout <= 0 {
		token.Wait()
	}
	return token.Error()
}

func truncatePayload(payload []byte) string {
	const max = 2048
	if len(payload) <= max {
		return string(payload)
	}
	return string(payload[:max]) + "..."
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

func (c *Client) publishWithTimeout(topic string, qos byte, retained bool, payload []byte) error {
	token := c.client.Publish(topic, qos, retained, payload)
	if c.timeout > 0 && !token.WaitTimeout(c.timeout) {
		c.log.Warn("mqtt publish timeout", zap.String("topic", topic))
		return ErrPublishTimeout
	}
	if c.timeout <= 0 {
		token.Wait()
	}
	return token.Error()
}

func (c *Client) configureBreaker(opts Options) {
	if !opts.BreakerEnabled {
		return
	}
	if opts.BreakerFailureThreshold == 0 {
		opts.BreakerFailureThreshold = 5
	}
	if opts.BreakerTimeout == 0 {
		opts.BreakerTimeout = 2 * time.Second
	}
	name := opts.BreakerName
	if name == "" {
		name = "mqtt"
	}
	c.breakerEnabled = true
	c.breakerSettings = gobreaker.Settings{
		Name:        name,
		MaxRequests: opts.BreakerMaxRequests,
		Interval:    opts.BreakerInterval,
		Timeout:     opts.BreakerTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= opts.BreakerFailureThreshold
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			c.log.Warn("mqtt breaker state", zap.String("name", name), zap.String("from", from.String()), zap.String("to", to.String()))
		},
	}
}

func (c *Client) breakerForTopic(topic string) *gobreaker.CircuitBreaker {
	if !c.breakerEnabled {
		return nil
	}
	key := breakerKeyForTopic(topic)
	if key == "" {
		return nil
	}
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()
	if existing, ok := c.breakers[key]; ok {
		return existing
	}
	settings := c.breakerSettings
	settings.Name = key
	cb := gobreaker.NewCircuitBreaker(settings)
	c.breakers[key] = cb
	return cb
}

func breakerKeyForTopic(topic string) string {
	if topic == "" {
		return ""
	}
	if strings.Contains(topic, "/node/") {
		parts := strings.Split(topic, "/")
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "node" {
				return "node:" + parts[i+1]
			}
		}
	}
	if strings.Contains(topic, "/reply/") {
		return "reply"
	}
	return ""
}
