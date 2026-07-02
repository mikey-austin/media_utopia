package renderermpv

import (
	"strings"
	"time"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	renderercore "github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	"go.uber.org/zap"
)

// Config configures the mpv renderer module. Everything except the driver
// parameters (AO/Device/Crossfade/MPVOptions) is handled by the shared
// renderer_core module scaffolding.
type Config struct {
	NodeID            string
	TopicBase         string
	Name              string
	AO                string
	Device            string
	Crossfade         time.Duration
	Volume            float64
	PublishState      bool
	Source            string
	MPVOptions        map[string]string
	StatePublisher    renderercore.StatePublisher
	PresencePublisher renderercore.PresencePublisher
}

// NewModule creates an mpv renderer module: it constructs the libmpv
// driver and delegates command handling, state publishing, and
// driver-event consumption to renderer_core.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*renderercore.Module, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "MPV Renderer"
	}
	driver, err := NewDriver(cfg.AO, cfg.Device, cfg.Crossfade, cfg.MPVOptions)
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
