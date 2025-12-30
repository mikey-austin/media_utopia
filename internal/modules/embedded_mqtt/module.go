package embeddedmqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// Config configures the embedded MQTT broker.
type Config struct {
	Listen         string
	AllowAnonymous bool
	Username       string
	Password       string
	TLSCA          string
	TLSCert        string
	TLSKey         string
}

// Module runs an embedded MQTT broker.
type Module struct {
	log    *slog.Logger
	server *mqtt.Server
	config Config
}

// NewModule creates a new embedded broker module.
func NewModule(log *slog.Logger, cfg Config) (*Module, error) {
	if strings.TrimSpace(cfg.Listen) == "" {
		cfg.Listen = "127.0.0.1:1883"
	}

	server, err := newServer(log, cfg)
	if err != nil {
		return nil, err
	}
	return &Module{log: log, server: server, config: cfg}, nil
}

// Run starts the embedded broker.
func (m *Module) Run(ctx context.Context) error {
	listenerConfig := listeners.Config{ID: "tcp-embedded", Address: m.config.Listen}
	if m.config.TLSCert != "" || m.config.TLSKey != "" || m.config.TLSCA != "" {
		tlsConfig, err := buildTLSConfig(m.config.TLSCA, m.config.TLSCert, m.config.TLSKey)
		if err != nil {
			return err
		}
		listenerConfig.TLSConfig = tlsConfig
	}

	listener := listeners.NewTCP(listenerConfig)
	if err := m.server.AddListener(listener); err != nil {
		return err
	}

	go func() {
		_ = m.server.Serve()
	}()

	<-ctx.Done()
	m.server.Close()
	return nil
}

func newServer(log *slog.Logger, cfg Config) (*mqtt.Server, error) {
	options := &mqtt.Options{InlineClient: true, Logger: log}
	server := mqtt.New(options)

	if cfg.AllowAnonymous {
		if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
			return nil, err
		}
	} else if cfg.Username != "" {
		ledger := &auth.Ledger{
			Auth: auth.AuthRules{{Username: auth.RString(cfg.Username), Password: auth.RString(cfg.Password), Allow: true}},
			ACL:  auth.ACLRules{{Username: auth.RString(cfg.Username), Filters: auth.Filters{auth.RString("#"): auth.ReadWrite}}},
		}
		if err := server.AddHook(new(auth.Hook), &auth.Options{Ledger: ledger}); err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("embedded mqtt requires allow_anonymous or username")
	}

	return server, nil
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

// BrokerURL returns the broker URL for a listen address.
func BrokerURL(listen string, tlsEnabled bool) string {
	scheme := "mqtt"
	if tlsEnabled {
		scheme = "mqtts"
	}
	return fmt.Sprintf("%s://%s", scheme, listen)
}
