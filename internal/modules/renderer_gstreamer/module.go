package renderergstreamer

import (
	"errors"
	"strings"
	"time"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	renderercore "github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	"go.uber.org/zap"
)

// Config configures the GStreamer renderer module. Everything except the
// driver parameters (Pipeline/Device/Crossfade) is handled by the shared
// renderer_core module scaffolding.
type Config struct {
	NodeID            string
	TopicBase         string
	Name              string
	Pipeline          string
	Device            string
	Crossfade         time.Duration
	Volume            float64
	PublishState      bool
	Source            string
	StatePublisher    renderercore.StatePublisher
	PresencePublisher renderercore.PresencePublisher
}

// NewModule creates a GStreamer renderer module: it constructs the
// GStreamer driver and delegates command handling, state publishing, and
// driver-event consumption to renderer_core.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*renderercore.Module, error) {
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
	return renderercore.NewModule(log, client, renderercore.ModuleConfig{
		NodeID:            cfg.NodeID,
		TopicBase:         cfg.TopicBase,
		Name:              cfg.Name,
		Volume:            cfg.Volume,
		PublishState:      cfg.PublishState,
		Source:            cfg.Source,
		StatePublisher:    cfg.StatePublisher,
		PresencePublisher: cfg.PresencePublisher,
	}, driver)
}
