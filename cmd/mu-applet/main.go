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

	// Persistent reply subscription with correlation-based routing.
	pendingReplies := struct {
		sync.Mutex
		m map[string]chan mu.ReplyEnvelope
	}{m: make(map[string]chan mu.ReplyEnvelope)}

	client.Subscribe(replyTopic, 1, func(_ paho.Client, msg paho.Message) {
		var reply mu.ReplyEnvelope
		if err := json.Unmarshal(msg.Payload(), &reply); err != nil {
			return
		}
		pendingReplies.Lock()
		ch, ok := pendingReplies.m[reply.ID]
		if ok {
			delete(pendingReplies.m, reply.ID)
		}
		pendingReplies.Unlock()
		if ok {
			ch <- reply
		}
	})

	buildCmd := func(cmdType string, body interface{}) (mu.CommandEnvelope, error) {
		cmd, err := mu.NewCommand(cmdType, body)
		if err != nil {
			return cmd, err
		}
		cmd.ID = idGen.NewID()
		cmd.TS = time.Now().Unix()
		cmd.From = "mu-applet"
		return cmd, nil
	}

	sendMQTTCmd := func(targetTopic string, cmdType string, body interface{}) (*mu.ReplyEnvelope, error) {
		cmd, err := buildCmd(cmdType, body)
		if err != nil {
			return nil, err
		}
		cmd.ReplyTo = replyTopic
		ch := make(chan mu.ReplyEnvelope, 1)
		pendingReplies.Lock()
		pendingReplies.m[cmd.ID] = ch
		pendingReplies.Unlock()
		payload, _ := json.Marshal(cmd)
		client.Publish(targetTopic, 1, false, payload)
		select {
		case reply := <-ch:
			return &reply, nil
		case <-time.After(3 * time.Second):
			pendingReplies.Lock()
			delete(pendingReplies.m, cmd.ID)
			pendingReplies.Unlock()
			return nil, fmt.Errorf("timeout")
		}
	}

	// sendCmd publishes a command with the current lease (fire-and-forget).
	sendCmd := func(cmdType string, body interface{}) {
		cmd, err := buildCmd(cmdType, body)
		if err != nil {
			logger.Warn("failed to build command", zap.String("type", cmdType), zap.Error(err))
			return
		}
		leaseMu.Lock()
		cmd.Lease = lease
		leaseMu.Unlock()
		payload, _ := json.Marshal(cmd)
		if err := client.Publish(cmdTopic, 1, false, payload); err != nil {
			logger.Warn("failed to send command", zap.String("type", cmdType), zap.Error(err))
		}
	}

	acquireLease := func() bool {
		for attempt := 0; attempt < 10; attempt++ {
			time.Sleep(200 * time.Millisecond)
			reply, err := sendMQTTCmd(cmdTopic, "session.acquire", mu.SessionAcquireBody{TTLMS: 86400000})
			if err != nil || reply == nil || !reply.OK {
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
		cmd, _ := buildCmd("session.release", nil)
		cmd.Lease = l
		payload, _ := json.Marshal(cmd)
		client.Publish(cmdTopic, 1, false, payload)
		logger.Info("session released")
	}

	if !acquireLease() {
		logger.Fatal("failed to acquire session after retries")
	}

	// Lease UI callbacks — shared by popup and tray
	var popup *applet.Popup
	var tray *applet.TrayIcon
	onAcquireUI := func() {
		go func() {
			if acquireLease() {
				glib.IdleAdd(func() bool {
					tray.SetHasLease(true)
					popup.SetHasLease(true)
					return false
				})
			}
		}()
	}
	onReleaseUI := func() {
		releaseLease()
		tray.SetHasLease(false)
		popup.SetHasLease(false)
	}

	// Playlist server discovery and loading
	var playlistServerID string
	discoverPlaylistServer := func() {
		// Subscribe to presence to find playlist servers
		presenceTopic := cfg.Server.TopicBase + "/node/+/presence"
		found := make(chan string, 1)
		client.Subscribe(presenceTopic, 1, func(_ paho.Client, msg paho.Message) {
			var p mu.Presence
			if err := json.Unmarshal(msg.Payload(), &p); err == nil && p.Kind == "playlist" {
				select {
				case found <- p.NodeID:
				default:
				}
			}
		})
		select {
		case id := <-found:
			playlistServerID = id
			logger.Info("playlist server found", zap.String("nodeId", id))
		case <-time.After(3 * time.Second):
			logger.Debug("no playlist server found")
		}
		client.Unsubscribe(presenceTopic)
	}

	fetchPlaylists := func() {
		if playlistServerID == "" {
			discoverPlaylistServer()
		}
		if playlistServerID == "" {
			return
		}
		plTopic := mu.TopicCommands(cfg.Server.TopicBase, playlistServerID)
		reply, err := sendMQTTCmd(plTopic, "playlist.list", mu.PlaylistListBody{Owner: ""})
		if err != nil || reply == nil || !reply.OK {
			return
		}
		var body mu.PlaylistListReply
		if err := json.Unmarshal(reply.Body, &body); err != nil {
			return
		}
		glib.IdleAdd(func() bool {
			popup.SetPlaylists(body.Playlists)
			return false
		})
	}

	loadPlaylist := func(playlistID string) {
		if playlistServerID == "" {
			return
		}
		sendCmd("queue.loadPlaylist", mu.QueueLoadPlaylistBody{
			PlaylistServerID: playlistServerID,
			PlaylistID:       playlistID,
			Mode:             "replace",
			Resolve:          "auto",
		})
	}

	// Create popup
	popup, err = applet.NewPopup(sendCmd, onAcquireUI, onReleaseUI, loadPlaylist)
	if err != nil {
		logger.Fatal("popup init failed", zap.Error(err))
	}

	// Create tray icon
	tray, err = applet.NewTrayIcon(
		func() { cancel(); gtk.MainQuit() },
		onAcquireUI,
		onReleaseUI,
	)
	if err != nil {
		logger.Fatal("tray icon failed", zap.Error(err))
	}
	tray.SetPopup(popup)

	// Queue fetcher — entries already carry Display metadata from the renderer.
	fetchQueue := func() {
		reply, err := sendMQTTCmd(cmdTopic, "queue.get", mu.QueueGetBody{From: 0, Count: 1000})
		if err != nil || reply == nil || !reply.OK {
			return
		}
		var body mu.QueueGetReply
		if err := json.Unmarshal(reply.Body, &body); err != nil {
			return
		}
		glib.IdleAdd(func() bool {
			popup.SetQueueItems(body.Entries, body.Index)
			return false
		})
	}

	// State bridge — channels state to GTK.
	// Only re-fetch queue on actual queue changes (index/length), not position updates.
	var lastSeenIndex, lastSeenLength int64 = -1, -1
	bridge := applet.NewBridge(stateCh, func(state *mu.RendererState) {
		popup.UpdateState(state)
		if state.Playback != nil {
			tray.SetPlaybackState(state.Playback.Status)
		}
		if state.Queue != nil && (state.Queue.Index != lastSeenIndex || state.Queue.Length != lastSeenLength) {
			lastSeenIndex = state.Queue.Index
			lastSeenLength = state.Queue.Length
			go fetchQueue()
		}
	})
	go bridge.Start()

	// Fetch playlists in background
	go fetchPlaylists()

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
