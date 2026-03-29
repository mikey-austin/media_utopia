// cmd/mu-applet/main.go
//go:build gtk

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"

	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/internal/applet"
	renderercore "github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	renderergstreamer "github.com/mikey-austin/media_utopia/internal/modules/renderer_gstreamer"
	"github.com/mikey-austin/media_utopia/internal/mud"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

func main() {
	var configPath string
	defaultConfig, err := mud.DefaultConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	flag.StringVar(&configPath, "config", defaultConfig, "config file path")
	flag.Parse()

	cfg, err := mud.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	// Find the first enabled GStreamer renderer
	renderers := cfg.Modules.RendererGStreamer.List()
	if len(renderers) == 0 {
		fmt.Fprintln(os.Stderr, "no renderer_gstreamer module configured")
		os.Exit(1)
	}
	rCfg := renderers[0].Config
	if !rCfg.Enabled {
		fmt.Fprintln(os.Stderr, "renderer_gstreamer is not enabled")
		os.Exit(1)
	}

	// Apply same defaults as mud
	if cfg.Server.TopicBase == "" {
		cfg.Server.TopicBase = mu.BaseTopic
	}
	if cfg.Server.Namespace == "" {
		cfg.Server.Namespace = cfg.Server.Identity
	}

	// Build node ID
	provider := rCfg.Provider
	if strings.TrimSpace(provider) == "" {
		provider = "gstreamer"
	}
	resource := rCfg.Resource
	if strings.TrimSpace(resource) == "" {
		resource = renderers[0].Name
		if resource == "" || resource == "default" {
			resource = "default"
		}
	}
	nodeID := fmt.Sprintf("mu:renderer:%s:%s:%s", provider, cfg.Server.Namespace, resource)

	// Logger
	logFactory := mud.NewModuleLoggerFactory(mud.LogConfig{
		Level:        cfg.Server.LogLevel,
		ModuleLevels: cfg.Server.LogLevels,
		Format:       cfg.Server.LogFormat,
		Output:       cfg.Server.LogOutput,
		AddSource:    cfg.Server.LogSource,
		UTC:          cfg.Server.LogUTC,
		Color:        cfg.Server.LogColor,
	})
	logger := logFactory.Logger()

	// MQTT client
	client, err := mqttserver.NewClient(mqttserver.Options{
		BrokerURL: cfg.Server.Broker,
		ClientID:  fmt.Sprintf("mu-applet-%d", time.Now().UnixNano()),
		Username:  cfg.Server.Auth.User,
		Password:  cfg.Server.Auth.Pass,
		TLSCA:     cfg.Server.TLS.CA,
		TLSCert:   cfg.Server.TLS.Cert,
		TLSKey:    cfg.Server.TLS.Key,
		Timeout:   2 * time.Second,
		Logger:    logger,
	})
	if err != nil {
		logger.Fatal("mqtt connection failed", zap.Error(err))
	}

	// State channel and publishers
	stateCh := make(chan *mu.RendererState, 1)
	stateTopic := mu.TopicState(cfg.Server.TopicBase, nodeID)
	presenceTopic := mu.TopicPresence(cfg.Server.TopicBase, nodeID)

	statePublisher := renderercore.NewMultiPublisher(
		renderercore.NewMQTTStatePublisher(client, stateTopic),
		renderercore.NewChannelStatePublisher(stateCh),
	)
	presencePublisher := renderercore.NewMQTTPresencePublisher(client, presenceTopic)

	// Create renderer module
	crossfade := time.Duration(rCfg.CrossfadeMS) * time.Millisecond
	mod, err := renderergstreamer.NewModule(logFactory.ModuleLogger("renderer_gstreamer"), client, renderergstreamer.Config{
		NodeID:            nodeID,
		TopicBase:         cfg.Server.TopicBase,
		Name:              rCfg.Name,
		Pipeline:          rCfg.Pipeline,
		Device:            rCfg.Device,
		Crossfade:         crossfade,
		Volume:            1.0,
		PublishState:      true,
		Source:            rCfg.Source,
		StatePublisher:    statePublisher,
		PresencePublisher: presencePublisher,
	})
	if err != nil {
		logger.Fatal("renderer init failed", zap.Error(err))
	}

	// GTK init — must happen before renderer starts (go-gst also initializes GLib)
	gtk.Init(nil)

	// Context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start renderer module in background
	go func() {
		if err := mod.Run(ctx); err != nil {
			logger.Error("renderer error", zap.Error(err))
			cancel()
		}
	}()

	// Lease management
	cmdTopic := mu.TopicCommands(cfg.Server.TopicBase, nodeID)
	replyTopic := mu.TopicReply(cfg.Server.TopicBase, "mu-applet")
	idGen := idgen.Generator{}

	var lease *mu.Lease
	var leaseMu sync.Mutex

	acquireLease := func() bool {
		replyCh := make(chan []byte, 1)
		client.Subscribe(replyTopic, 1, func(_ paho.Client, msg paho.Message) {
			select {
			case replyCh <- msg.Payload():
			default:
			}
		})
		defer client.Unsubscribe(replyTopic)

		for attempt := 0; attempt < 10; attempt++ {
			time.Sleep(200 * time.Millisecond)
			cmd, _ := mu.NewCommand("session.acquire", mu.SessionAcquireBody{TTLMS: 86400000})
			cmd.ID = idGen.NewID()
			cmd.TS = time.Now().Unix()
			cmd.From = "mu-applet"
			cmd.ReplyTo = replyTopic
			payload, _ := json.Marshal(cmd)
			if err := client.Publish(cmdTopic, 1, false, payload); err != nil {
				continue
			}
			select {
			case replyData := <-replyCh:
				var reply mu.ReplyEnvelope
				if err := json.Unmarshal(replyData, &reply); err != nil || !reply.OK {
					continue
				}
				var body mu.SessionReplyBody
				if err := json.Unmarshal(reply.Body, &body); err != nil {
					continue
				}
				leaseMu.Lock()
				lease = &mu.Lease{SessionID: body.Session.ID, Token: body.Session.Token}
				leaseMu.Unlock()
				logger.Info("session acquired", zap.String("session_id", body.Session.ID))
				return true
			case <-time.After(1 * time.Second):
				continue
			}
		}
		return false
	}

	releaseLease := func() {
		leaseMu.Lock()
		l := lease
		lease = nil
		leaseMu.Unlock()
		if l == nil {
			return
		}
		cmd, _ := mu.NewCommand("session.release", nil)
		cmd.ID = idGen.NewID()
		cmd.TS = time.Now().Unix()
		cmd.From = "mu-applet"
		cmd.Lease = l
		payload, _ := json.Marshal(cmd)
		client.Publish(cmdTopic, 1, false, payload)
		logger.Info("session released")
	}

	// Acquire initial lease (blocks until renderer module is ready)
	if !acquireLease() {
		logger.Fatal("failed to acquire session after retries")
	}

	// Command sender — publishes commands to MQTT with current lease
	sendCmd := func(cmdType string, body interface{}) {
		env, err := mu.NewCommand(cmdType, body)
		if err != nil {
			logger.Warn("failed to build command", zap.String("type", cmdType), zap.Error(err))
			return
		}
		env.ID = idGen.NewID()
		env.TS = time.Now().Unix()
		env.From = "mu-applet"
		leaseMu.Lock()
		env.Lease = lease
		leaseMu.Unlock()
		payload, _ := json.Marshal(env)
		if err := client.Publish(cmdTopic, 1, false, payload); err != nil {
			logger.Warn("failed to send command", zap.String("type", cmdType), zap.Error(err))
		}
	}

	// Create popup
	popup, err := applet.NewPopup(sendCmd)
	if err != nil {
		logger.Fatal("popup init failed", zap.Error(err))
	}

	// Create tray icon with lease management
	var tray *applet.TrayIcon
	tray, err = applet.NewTrayIcon(
		func() { // onQuit
			cancel()
			gtk.MainQuit()
		},
		func() { // onLeaseAcquire
			go func() {
				if acquireLease() {
					glib.IdleAdd(func() bool {
						tray.SetHasLease(true)
						return false
					})
				}
			}()
		},
		func() { // onLeaseRelease
			releaseLease()
			tray.SetHasLease(false)
		},
	)
	if err != nil {
		logger.Fatal("tray icon failed", zap.Error(err))
	}
	tray.SetPopup(popup)

	// State bridge — channels state to GTK
	bridge := applet.NewBridge(stateCh, func(state *mu.RendererState) {
		popup.UpdateState(state)
		if state.Playback != nil {
			tray.SetPlaybackState(state.Playback.Status)
		}
	})
	go bridge.Start()

	// Shutdown: when context is cancelled (signal), quit GTK
	go func() {
		<-ctx.Done()
		glib.IdleAdd(func() bool {
			gtk.MainQuit()
			return false
		})
	}()

	logger.Info("mu-applet started",
		zap.String("node_id", nodeID),
		zap.String("name", rCfg.Name),
		zap.String("broker", cfg.Server.Broker))

	gtk.Main()
	logger.Info("mu-applet stopped")
}
