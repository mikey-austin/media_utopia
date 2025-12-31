package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	embeddedmqtt "github.com/mikey-austin/media_utopia/internal/modules/embedded_mqtt"
	jellyfinlibrary "github.com/mikey-austin/media_utopia/internal/modules/jellyfin_library"
	"github.com/mikey-austin/media_utopia/internal/modules/playlist"
	renderergstreamer "github.com/mikey-austin/media_utopia/internal/modules/renderer_gstreamer"
	"github.com/mikey-austin/media_utopia/internal/mud"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

func main() {
	var (
		configPath  string
		broker      string
		identity    string
		topicBase   string
		logLevel    string
		logFormat   string
		logOutput   string
		logSource   bool
		logUTC      bool
		logColor    bool
		daemonize   bool
		printConfig bool
		dryRun      bool
		moduleOnly  string
	)

	defaultConfig, err := mud.DefaultConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	flag.StringVar(&configPath, "config", defaultConfig, "config file path")
	flag.StringVar(&broker, "broker", "", "MQTT broker URL override")
	flag.StringVar(&identity, "identity", "", "server identity override")
	flag.StringVar(&topicBase, "topic-base", "", "topic base override")
	flag.StringVar(&logLevel, "log-level", "", "log level override")
	flag.StringVar(&logFormat, "log-format", "", "log format override (text|json)")
	flag.StringVar(&logOutput, "log-output", "", "log output override (stdout|stderr)")
	flag.BoolVar(&logSource, "log-source", false, "include source file in logs")
	flag.BoolVar(&logUTC, "log-utc", false, "use UTC timestamps in logs")
	flag.BoolVar(&logColor, "log-color", false, "enable colored log output (text only)")
	flag.BoolVar(&daemonize, "daemonize", false, "run as daemon")
	flag.StringVar(&moduleOnly, "module", "", "limit to a single module")
	flag.BoolVar(&printConfig, "print-config", false, "print resolved config and exit")
	flag.BoolVar(&dryRun, "dry-run", false, "validate config and exit")
	flag.Parse()

	cfg, err := mud.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	applyOverrides(&cfg, broker, identity, topicBase, logLevel, logFormat, logOutput, logSource, logUTC, logColor, daemonize)

	if printConfig {
		if err := printResolvedConfig(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if dryRun {
		return
	}

	logger := mud.NewLogger(mud.LogConfig{
		Level:     cfg.Server.LogLevel,
		Format:    cfg.Server.LogFormat,
		Output:    cfg.Server.LogOutput,
		AddSource: cfg.Server.LogSource,
		UTC:       cfg.Server.LogUTC,
		Color:     cfg.Server.LogColor,
	})
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	embeddedURL := embeddedBrokerURL(cfg)
	skipEmbedded := false

	if moduleOnly != "embedded_mqtt" && cfg.Modules.EmbeddedMQTT.Enabled && cfg.Server.Broker == embeddedURL {
		if err := startEmbeddedBroker(ctx, cfg, logger, cancel); err != nil {
			logger.Error("embedded mqtt failed", zap.Error(err))
			os.Exit(1)
		}
		skipEmbedded = true
	}

	if cfg.Server.Broker == "" && !(moduleOnly == "embedded_mqtt" && cfg.Modules.EmbeddedMQTT.Enabled) {
		logger.Error("broker is required")
		os.Exit(1)
	}
	logger.Info("mud starting",
		zap.String("broker", cfg.Server.Broker),
		zap.String("identity", cfg.Server.Identity),
		zap.String("topic_base", cfg.Server.TopicBase),
		zap.String("log_level", cfg.Server.LogLevel),
		zap.String("log_format", cfg.Server.LogFormat),
		zap.String("log_output", cfg.Server.LogOutput),
		zap.Bool("log_source", cfg.Server.LogSource),
		zap.Bool("log_utc", cfg.Server.LogUTC),
		zap.Bool("log_color", cfg.Server.LogColor),
		zap.Strings("modules", enabledModules(cfg)),
	)

	if daemonize {
		// TODO
		logger.Warn("daemonize flag is set; running in foreground (not implemented)")
	}

	var client *mqttserver.Client
	if moduleOnly != "embedded_mqtt" {
		var err error
		client, err = mqttserver.NewClient(mqttserver.Options{
			BrokerURL: cfg.Server.Broker,
			ClientID:  fmt.Sprintf("mud-%d", time.Now().UnixNano()),
			Username:  cfg.Server.Auth.User,
			Password:  cfg.Server.Auth.Pass,
			TLSCA:     cfg.Server.TLS.CA,
			TLSCert:   cfg.Server.TLS.Cert,
			TLSKey:    cfg.Server.TLS.Key,
			Timeout:   2 * time.Second,
		})
		if err != nil {
			logger.Error("mqtt connection failed", zap.Error(err))
			os.Exit(1)
		}
	}

	modules, err := buildModules(cfg, client, logger, moduleOnly, skipEmbedded)
	if err != nil {
		logger.Error("failed to build modules", zap.Error(err))
		os.Exit(1)
	}

	supervisor := mud.Supervisor{Logger: logger}
	if err := supervisor.Run(ctx, modules); err != nil {
		logger.Error("supervisor error", zap.Error(err))
		os.Exit(1)
	}
}

func applyOverrides(cfg *mud.Config, broker string, identity string, topicBase string, logLevel string, logFormat string, logOutput string, logSource bool, logUTC bool, logColor bool, daemonize bool) {
	if broker != "" {
		cfg.Server.Broker = broker
	}
	if identity != "" {
		cfg.Server.Identity = identity
	}
	if topicBase != "" {
		cfg.Server.TopicBase = topicBase
	}
	if logLevel != "" {
		cfg.Server.LogLevel = logLevel
	}
	if logFormat != "" {
		cfg.Server.LogFormat = logFormat
	}
	if logOutput != "" {
		cfg.Server.LogOutput = logOutput
	}
	if logSource {
		cfg.Server.LogSource = true
	}
	if logUTC {
		cfg.Server.LogUTC = true
	}
	if logColor {
		cfg.Server.LogColor = true
	}
	if daemonize {
		cfg.Server.Daemonize = true
	}
	if cfg.Server.TopicBase == "" {
		cfg.Server.TopicBase = mu.BaseTopic
	}
	if cfg.Server.Broker == "" && cfg.Modules.EmbeddedMQTT.Enabled {
		listen := cfg.Modules.EmbeddedMQTT.Listen
		if listen == "" {
			listen = "127.0.0.1:1883"
		}
		tlsEnabled := cfg.Modules.EmbeddedMQTT.TLSCert != "" || cfg.Modules.EmbeddedMQTT.TLSKey != "" || cfg.Modules.EmbeddedMQTT.TLSCA != ""
		cfg.Server.Broker = embeddedmqtt.BrokerURL(listen, tlsEnabled)
	}
}

func buildModules(cfg mud.Config, client *mqttserver.Client, logger *zap.Logger, moduleOnly string, skipEmbedded bool) ([]mud.ModuleRunner, error) {
	modules := []mud.ModuleRunner{}
	if cfg.Modules.EmbeddedMQTT.Enabled && !skipEmbedded {
		if moduleOnly == "" || moduleOnly == "embedded_mqtt" {
			mod, err := embeddedmqtt.NewModule(logger.With(zap.String("module", "embedded_mqtt")), embeddedmqtt.Config{
				Listen:         cfg.Modules.EmbeddedMQTT.Listen,
				AllowAnonymous: cfg.Modules.EmbeddedMQTT.AllowAnonymous,
				Username:       cfg.Modules.EmbeddedMQTT.Username,
				Password:       cfg.Modules.EmbeddedMQTT.Password,
				TLSCA:          cfg.Modules.EmbeddedMQTT.TLSCA,
				TLSCert:        cfg.Modules.EmbeddedMQTT.TLSCert,
				TLSKey:         cfg.Modules.EmbeddedMQTT.TLSKey,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "embedded_mqtt",
				Run:  mod.Run,
			})
		}
	}
	if cfg.Modules.Playlist.Enabled {
		if moduleOnly == "" || moduleOnly == "playlist" {
			pl, err := playlist.NewModule(logger.With(zap.String("module", "playlist")), client, playlist.Config{
				NodeID:      cfg.Modules.Playlist.NodeID,
				TopicBase:   cfg.Server.TopicBase,
				StoragePath: cfg.Modules.Playlist.StoragePath,
				Identity:    cfg.Server.Identity,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "playlist",
				Run:  pl.Run,
			})
		}
	}

	if cfg.Modules.BridgeJellyfinLibrary.Enabled {
		if moduleOnly == "" || moduleOnly == "bridge_jellyfin_library" {
			timeout := time.Duration(cfg.Modules.BridgeJellyfinLibrary.TimeoutMS) * time.Millisecond
			jf, err := jellyfinlibrary.NewModule(logger.With(zap.String("module", "bridge_jellyfin_library")), client, jellyfinlibrary.Config{
				NodeID:    cfg.Modules.BridgeJellyfinLibrary.NodeID,
				TopicBase: cfg.Server.TopicBase,
				BaseURL:   cfg.Modules.BridgeJellyfinLibrary.BaseURL,
				APIKey:    cfg.Modules.BridgeJellyfinLibrary.APIKey,
				UserID:    cfg.Modules.BridgeJellyfinLibrary.UserID,
				Timeout:   timeout,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "bridge_jellyfin_library",
				Run:  jf.Run,
			})
		}
	}

	if cfg.Modules.RendererGStreamer.Enabled {
		if moduleOnly == "" || moduleOnly == "renderer_gstreamer" {
			crossfade := time.Duration(cfg.Modules.RendererGStreamer.CrossfadeMS) * time.Millisecond
			mod, err := renderergstreamer.NewModule(logger.With(zap.String("module", "renderer_gstreamer")), client, renderergstreamer.Config{
				NodeID:       cfg.Modules.RendererGStreamer.NodeID,
				TopicBase:    cfg.Server.TopicBase,
				Name:         "GStreamer Renderer",
				Pipeline:     cfg.Modules.RendererGStreamer.Pipeline,
				Device:       cfg.Modules.RendererGStreamer.Device,
				Crossfade:    crossfade,
				Volume:       1.0,
				PublishState: true,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "renderer_gstreamer",
				Run:  mod.Run,
			})
		}
	}

	if moduleOnly != "" && len(modules) == 0 {
		return nil, errors.New("no modules enabled")
	}
	return modules, nil
}

func enabledModules(cfg mud.Config) []string {
	out := []string{}
	if cfg.Modules.EmbeddedMQTT.Enabled {
		out = append(out, "embedded_mqtt")
	}
	if cfg.Modules.Playlist.Enabled {
		out = append(out, "playlist")
	}
	if cfg.Modules.BridgeJellyfinLibrary.Enabled {
		out = append(out, "bridge_jellyfin_library")
	}
	if cfg.Modules.RendererGStreamer.Enabled {
		out = append(out, "renderer_gstreamer")
	}
	if cfg.Modules.BridgeUPNPLibrary.Enabled {
		out = append(out, "bridge_upnp_library")
	}
	return out
}

func printResolvedConfig(cfg mud.Config) error {
	fmt.Fprintf(os.Stdout,
		"broker=%s identity=%s topic_base=%s log_level=%s log_format=%s log_output=%s log_source=%t log_utc=%t log_color=%t daemonize=%t\n",
		cfg.Server.Broker,
		cfg.Server.Identity,
		cfg.Server.TopicBase,
		cfg.Server.LogLevel,
		cfg.Server.LogFormat,
		cfg.Server.LogOutput,
		cfg.Server.LogSource,
		cfg.Server.LogUTC,
		cfg.Server.LogColor,
		cfg.Server.Daemonize,
	)
	return nil
}

func embeddedBrokerURL(cfg mud.Config) string {
	listen := cfg.Modules.EmbeddedMQTT.Listen
	if listen == "" {
		listen = "127.0.0.1:1883"
	}
	tlsEnabled := cfg.Modules.EmbeddedMQTT.TLSCert != "" || cfg.Modules.EmbeddedMQTT.TLSKey != "" || cfg.Modules.EmbeddedMQTT.TLSCA != ""
	return embeddedmqtt.BrokerURL(listen, tlsEnabled)
}

func startEmbeddedBroker(ctx context.Context, cfg mud.Config, logger *zap.Logger, cancel context.CancelFunc) error {
	mod, err := embeddedmqtt.NewModule(logger.With(zap.String("module", "embedded_mqtt")), embeddedmqtt.Config{
		Listen:         cfg.Modules.EmbeddedMQTT.Listen,
		AllowAnonymous: cfg.Modules.EmbeddedMQTT.AllowAnonymous,
		Username:       cfg.Modules.EmbeddedMQTT.Username,
		Password:       cfg.Modules.EmbeddedMQTT.Password,
		TLSCA:          cfg.Modules.EmbeddedMQTT.TLSCA,
		TLSCert:        cfg.Modules.EmbeddedMQTT.TLSCert,
		TLSKey:         cfg.Modules.EmbeddedMQTT.TLSKey,
	})
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- mod.Run(ctx)
	}()
	go func() {
		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("embedded mqtt exited", zap.Error(err))
			cancel()
		}
	}()

	listen := cfg.Modules.EmbeddedMQTT.Listen
	if listen == "" {
		listen = "127.0.0.1:1883"
	}
	return waitForListen(listen, 3*time.Second)
}

func waitForListen(listen string, timeout time.Duration) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("embedded mqtt not ready at %s", addr)
}
