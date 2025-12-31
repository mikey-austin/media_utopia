package renderergstreamer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Config configures the GStreamer renderer module.
type Config struct {
	NodeID       string
	TopicBase    string
	Name         string
	Pipeline     string
	Device       string
	Crossfade    time.Duration
	Volume       float64
	PublishState bool
}

// Module implements a GStreamer renderer.
type Module struct {
	log      *zap.Logger
	client   *mqttserver.Client
	engine   *renderercore.Engine
	config   Config
	cmdTopic string
}

// NewModule creates a renderer module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("node_id required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "GStreamer Renderer"
	}
	if strings.TrimSpace(cfg.Pipeline) == "" {
		return nil, errors.New("pipeline required")
	}

	driver, err := NewDriver(cfg.Pipeline, cfg.Device, cfg.Crossfade)
	if err != nil {
		return nil, err
	}
	engine := renderercore.NewEngine(cfg.NodeID, cfg.Name, driver)
	if cfg.Volume > 0 {
		engine.State.Playback.Volume = cfg.Volume
	}

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	return &Module{log: log, client: client, engine: engine, config: cfg, cmdTopic: cmdTopic}, nil
}

// Run starts the renderer module.
func (m *Module) Run(ctx context.Context) error {
	if err := m.publishPresence(); err != nil {
		return err
	}
	if m.config.PublishState {
		if err := m.publishState(); err != nil {
			return err
		}
	}

	handler := func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}
	if err := m.client.Subscribe(m.cmdTopic, 1, handler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.cmdTopic)

	<-ctx.Done()
	return nil
}

func (m *Module) publishPresence() error {
	presence := mu.Presence{
		NodeID: m.config.NodeID,
		Kind:   "renderer",
		Name:   m.config.Name,
		Caps: map[string]any{
			"queueResolve": false,
			"seek":         true,
			"volume":       true,
		},
		TS: time.Now().Unix(),
	}
	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicPresence(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

func (m *Module) publishState() error {
	payload, err := json.Marshal(m.engine.State)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicState(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

func (m *Module) handleMessage(msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid command", zap.Error(err))
		return
	}

	reply := m.engine.HandleCommand(cmd)
	if cmd.ReplyTo != "" {
		payload, err := json.Marshal(reply)
		if err == nil {
			_ = m.client.Publish(cmd.ReplyTo, 1, false, payload)
		}
	}
	if m.config.PublishState {
		_ = m.publishState()
	}
}
