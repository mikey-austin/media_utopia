package mqttserver

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

// Options configures the MQTT server client.
type Options struct {
	BrokerURL string
	ClientID  string
	Username  string
	Password  string
	TLSCA     string
	TLSCert   string
	TLSKey    string
	Timeout   time.Duration
	Logger    *zap.Logger
	Debug     bool
}

// Client wraps an MQTT connection for server modules.
type Client struct {
	client paho.Client
	log    *zap.Logger
	debug  bool
}

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

	return &Client{client: client, log: opts.Logger, debug: opts.Debug}, nil
}

// Publish publishes a message.
func (c *Client) Publish(topic string, qos byte, retained bool, payload []byte) error {
	if c.debug {
		c.log.Debug("mqtt publish", zap.String("topic", topic), zap.Int("bytes", len(payload)), zap.String("payload", truncatePayload(payload)))
	}
	token := c.client.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
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
	token := c.client.Subscribe(topic, qos, wrapped)
	token.Wait()
	return token.Error()
}

// Unsubscribe unsubscribes from a topic.
func (c *Client) Unsubscribe(topic string) error {
	if c.debug {
		c.log.Debug("mqtt unsubscribe", zap.String("topic", topic))
	}
	token := c.client.Unsubscribe(topic)
	token.Wait()
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
