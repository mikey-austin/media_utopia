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
	"go.uber.org/zap"
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
	log    *zap.Logger
	server *mqtt.Server
	config Config
}

// NewModule creates a new embedded broker module.
func NewModule(log *zap.Logger, cfg Config) (*Module, error) {
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

func newServer(log *zap.Logger, cfg Config) (*mqtt.Server, error) {
	options := &mqtt.Options{InlineClient: true, Logger: newSlogLogger(log)}
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

func newSlogLogger(logger *zap.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return slog.New(&zapSlogHandler{logger: logger})
}

type zapSlogHandler struct {
	logger *zap.Logger
	attrs  []slog.Attr
}

func (h *zapSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return true
}

func (h *zapSlogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make([]zap.Field, 0, len(h.attrs)+record.NumAttrs())
	var errMsg string
	for _, attr := range h.attrs {
		fields = append(fields, slogAttrToField(attr))
	}
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "error" {
			switch attr.Value.Kind() {
			case slog.KindString:
				errMsg = attr.Value.String()
			case slog.KindAny:
				if v, ok := attr.Value.Any().(error); ok {
					errMsg = v.Error()
				}
			}
		}
		fields = append(fields, slogAttrToField(attr))
		return true
	})
	if errMsg != "" && (strings.Contains(errMsg, "read connection: EOF") || errMsg == "EOF") {
		fields = append(fields, zap.String("note", "harmless connection close"))
		h.logger.Debug("embedded mqtt connection closed", fields...)
		return nil
	}
	switch {
	case record.Level >= slog.LevelError:
		h.logger.Error(record.Message, fields...)
	case record.Level >= slog.LevelWarn:
		h.logger.Warn(record.Message, fields...)
	case record.Level >= slog.LevelInfo:
		h.logger.Info(record.Message, fields...)
	default:
		h.logger.Debug(record.Message, fields...)
	}
	return nil
}

func (h *zapSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	next = append(next, h.attrs...)
	next = append(next, attrs...)
	return &zapSlogHandler{logger: h.logger, attrs: next}
}

func (h *zapSlogHandler) WithGroup(_ string) slog.Handler {
	return h
}

func slogAttrToField(attr slog.Attr) zap.Field {
	switch attr.Value.Kind() {
	case slog.KindString:
		return zap.String(attr.Key, attr.Value.String())
	case slog.KindInt64:
		return zap.Int64(attr.Key, attr.Value.Int64())
	case slog.KindUint64:
		return zap.Uint64(attr.Key, attr.Value.Uint64())
	case slog.KindFloat64:
		return zap.Float64(attr.Key, attr.Value.Float64())
	case slog.KindBool:
		return zap.Bool(attr.Key, attr.Value.Bool())
	default:
		return zap.Any(attr.Key, attr.Value.Any())
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

// BrokerURL returns the broker URL for a listen address.
func BrokerURL(listen string, tlsEnabled bool) string {
	scheme := "mqtt"
	if tlsEnabled {
		scheme = "mqtts"
	}
	return fmt.Sprintf("%s://%s", scheme, listen)
}
