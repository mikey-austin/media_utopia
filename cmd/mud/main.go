package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	embeddedmqtt "github.com/mikey-austin/media_utopia/internal/modules/embedded_mqtt"
	go2rtclibrary "github.com/mikey-austin/media_utopia/internal/modules/go2rtc_library"
	jellyfinlibrary "github.com/mikey-austin/media_utopia/internal/modules/jellyfin_library"
	"github.com/mikey-austin/media_utopia/internal/modules/playlist"
	podcastlibrary "github.com/mikey-austin/media_utopia/internal/modules/podcast_library"
	renderergstreamer "github.com/mikey-austin/media_utopia/internal/modules/renderer_gstreamer"
	rendererkodi "github.com/mikey-austin/media_utopia/internal/modules/renderer_kodi"
	renderervlc "github.com/mikey-austin/media_utopia/internal/modules/renderer_vlc"
	upnplibrary "github.com/mikey-austin/media_utopia/internal/modules/upnp_library"
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
		zap.String("namespace", cfg.Server.Namespace),
	)

	if daemonize {
		// TODO
		logger.Warn("daemonize flag is set; running in foreground (not implemented)")
	}

	var client *mqttserver.Client
	if moduleOnly != "embedded_mqtt" {
		var err error
		client, err = mqttserver.NewClient(mqttserver.Options{
			BrokerURL:               cfg.Server.Broker,
			ClientID:                fmt.Sprintf("mud-%d", time.Now().UnixNano()),
			Username:                cfg.Server.Auth.User,
			Password:                cfg.Server.Auth.Pass,
			TLSCA:                   cfg.Server.TLS.CA,
			TLSCert:                 cfg.Server.TLS.Cert,
			TLSKey:                  cfg.Server.TLS.Key,
			Timeout:                 2 * time.Second,
			Logger:                  logger,
			Debug:                   logger.Core().Enabled(zap.DebugLevel),
			BreakerEnabled:          cfg.Server.RPCBreakerEnabled,
			BreakerName:             "rpc",
			BreakerTimeout:          durationFromMS(cfg.Server.RPCBreakerTimeoutMS),
			BreakerInterval:         durationFromMS(cfg.Server.RPCBreakerIntervalMS),
			BreakerMaxRequests:      cfg.Server.RPCBreakerMaxRequests,
			BreakerFailureThreshold: cfg.Server.RPCBreakerFailureThreshold,
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

	supervisor := mud.Supervisor{
		Logger:          logger,
		ContinueOnError: cfg.Server.ContinueOnError,
	}
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
	if cfg.Server.Namespace == "" {
		cfg.Server.Namespace = cfg.Server.Identity
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
	nodeIDs := map[string]string{}
	ensureUnique := func(nodeID string, name string) error {
		if nodeID == "" {
			return nil
		}
		if existing, ok := nodeIDs[nodeID]; ok {
			return fmt.Errorf("node_id %s used by %s and %s", nodeID, existing, name)
		}
		nodeIDs[nodeID] = name
		return nil
	}
	resourceFor := func(key string, resource string) string {
		if strings.TrimSpace(resource) != "" {
			return resource
		}
		if key == "" || key == "default" {
			return "default"
		}
		return key
	}

	if moduleOnly == "" || moduleOnly == "playlist" {
		for _, item := range cfg.Modules.Playlist.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("playlist", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "playlist"); err != nil {
				return nil, err
			}
			pl, err := playlist.NewModule(logger.With(zap.String("module", "playlist")), client, playlist.Config{
				NodeID:      nodeID,
				TopicBase:   cfg.Server.TopicBase,
				StoragePath: cfgItem.StoragePath,
				Identity:    cfg.Server.Identity,
				Name:        cfgItem.Name,
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

	if moduleOnly == "" || moduleOnly == "bridge_jellyfin_library" {
		for _, item := range cfg.Modules.BridgeJellyfinLibrary.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			timeout := time.Duration(cfgItem.TimeoutMS) * time.Millisecond
			cacheTTL := time.Duration(cfgItem.CacheTTLMS) * time.Millisecond
			browseCacheTTL := time.Duration(cfgItem.BrowseCacheTTLMS) * time.Millisecond
			publishCooldown := time.Duration(cfgItem.PublishTimeoutCooldownMS) * time.Millisecond
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("library", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "bridge_jellyfin_library"); err != nil {
				return nil, err
			}
			jf, err := jellyfinlibrary.NewModule(logger.With(zap.String("module", "bridge_jellyfin_library")), client, jellyfinlibrary.Config{
				NodeID:                 nodeID,
				TopicBase:              cfg.Server.TopicBase,
				Name:                   cfgItem.Name,
				BaseURL:                cfgItem.BaseURL,
				StreamBaseURL:          cfgItem.StreamBaseURL,
				ArtworkBaseURL:         cfgItem.ArtworkBaseURL,
				APIKey:                 cfgItem.APIKey,
				UserID:                 cfgItem.UserID,
				Timeout:                timeout,
				CacheTTL:               cacheTTL,
				CacheSize:              cfgItem.CacheSize,
				CacheCompress:          cfgItem.CacheCompress,
				BrowseCacheTTL:         browseCacheTTL,
				BrowseCacheSize:        cfgItem.BrowseCacheSize,
				PublishTimeoutCooldown: publishCooldown,
				MaxConcurrentRequests:  cfgItem.MaxConcurrentRequests,
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

	if moduleOnly == "" || moduleOnly == "bridge_upnp_library" {
		for _, item := range cfg.Modules.BridgeUPNPLibrary.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			if !upnplibrary.Enabled {
				logger.Warn("bridge_upnp_library disabled at build time (missing upnp tag)")
				continue
			}
			timeout := time.Duration(cfgItem.TimeoutMS) * time.Millisecond
			cacheTTL := time.Duration(cfgItem.CacheTTLMS) * time.Millisecond
			browseCacheTTL := time.Duration(cfgItem.BrowseCacheTTLMS) * time.Millisecond
			publishCooldown := time.Duration(cfgItem.PublishTimeoutCooldownMS) * time.Millisecond
			discoveryInterval := time.Duration(cfgItem.DiscoveryIntervalMS) * time.Millisecond
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("library", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "bridge_upnp_library"); err != nil {
				return nil, err
			}
			mod, err := upnplibrary.NewModule(logger.With(zap.String("module", "bridge_upnp_library")), client, upnplibrary.Config{
				NodeID:                 nodeID,
				TopicBase:              cfg.Server.TopicBase,
				Name:                   cfgItem.Name,
				Listen:                 cfgItem.Listen,
				Timeout:                timeout,
				CacheTTL:               cacheTTL,
				CacheSize:              cfgItem.CacheSize,
				CacheCompress:          cfgItem.CacheCompress,
				BrowseCacheTTL:         browseCacheTTL,
				BrowseCacheSize:        cfgItem.BrowseCacheSize,
				MaxConcurrentRequests:  cfgItem.MaxConcurrentRequests,
				PublishTimeoutCooldown: publishCooldown,
				DiscoveryInterval:      discoveryInterval,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "bridge_upnp_library",
				Run:  mod.Run,
			})
		}
	}

	if moduleOnly == "" || moduleOnly == "podcast" {
		for _, item := range cfg.Modules.PodcastLibrary.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			timeout := time.Duration(cfgItem.TimeoutMS) * time.Millisecond
			refresh := time.Duration(cfgItem.RefreshIntervalMS) * time.Millisecond
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("library", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "podcast"); err != nil {
				return nil, err
			}
			mod, err := podcastlibrary.NewModule(logger.With(zap.String("module", "podcast")), client, podcastlibrary.Config{
				NodeID:            nodeID,
				TopicBase:         cfg.Server.TopicBase,
				Name:              cfgItem.Name,
				Feeds:             cfgItem.Feeds,
				RefreshInterval:   refresh,
				CacheDir:          cfgItem.CacheDir,
				Timeout:           timeout,
				ReverseSortByDate: cfgItem.ReverseSortByDate,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "podcast",
				Run:  mod.Run,
			})
		}
	}

	if moduleOnly == "" || moduleOnly == "go2rtc" {
		for _, item := range cfg.Modules.Go2RTCLibrary.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			timeout := time.Duration(cfgItem.TimeoutMS) * time.Millisecond
			refresh := time.Duration(cfgItem.RefreshIntervalMS) * time.Millisecond
			durations, err := parseDurations(cfgItem.Durations)
			if err != nil {
				return nil, err
			}
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("library", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "go2rtc"); err != nil {
				return nil, err
			}
			mod, err := go2rtclibrary.NewModule(logger.With(zap.String("module", "go2rtc")), client, go2rtclibrary.Config{
				NodeID:          nodeID,
				TopicBase:       cfg.Server.TopicBase,
				Name:            cfgItem.Name,
				BaseURL:         cfgItem.BaseURL,
				Username:        cfgItem.Username,
				Password:        cfgItem.Password,
				Durations:       durations,
				RefreshInterval: refresh,
				Timeout:         timeout,
				CacheTTL:        durationFromMS(cfgItem.CacheTTLMS),
				CacheSize:       cfgItem.CacheSize,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "go2rtc",
				Run:  mod.Run,
			})
		}
	}

	if moduleOnly == "" || moduleOnly == "renderer_gstreamer" {
		for _, item := range cfg.Modules.RendererGStreamer.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			crossfade := time.Duration(cfgItem.CrossfadeMS) * time.Millisecond
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("renderer", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "renderer_gstreamer"); err != nil {
				return nil, err
			}
			mod, err := renderergstreamer.NewModule(logger.With(zap.String("module", "renderer_gstreamer")), client, renderergstreamer.Config{
				NodeID:       nodeID,
				TopicBase:    cfg.Server.TopicBase,
				Name:         cfgItem.Name,
				Pipeline:     cfgItem.Pipeline,
				Device:       cfgItem.Device,
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

	if moduleOnly == "" || moduleOnly == "renderer_kodi" {
		for _, item := range cfg.Modules.RendererKodi.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			timeout := time.Duration(cfgItem.TimeoutMS) * time.Millisecond
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("renderer", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "renderer_kodi"); err != nil {
				return nil, err
			}
			mod, err := rendererkodi.NewModule(logger.With(zap.String("module", "renderer_kodi")), client, rendererkodi.Config{
				NodeID:       nodeID,
				TopicBase:    cfg.Server.TopicBase,
				Name:         cfgItem.Name,
				BaseURL:      cfgItem.BaseURL,
				Username:     cfgItem.Username,
				Password:     cfgItem.Password,
				Timeout:      timeout,
				Volume:       1.0,
				PublishState: true,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "renderer_kodi",
				Run:  mod.Run,
			})
		}
	}

	if moduleOnly == "" || moduleOnly == "renderer_vlc" {
		for _, item := range cfg.Modules.RendererVLC.List() {
			cfgItem := item.Config
			if !cfgItem.Enabled {
				continue
			}
			timeout := time.Duration(cfgItem.TimeoutMS) * time.Millisecond
			resource := resourceFor(item.Name, cfgItem.Resource)
			nodeID, err := buildNodeID("renderer", cfgItem.Provider, cfg.Server.Namespace, resource)
			if err != nil {
				return nil, err
			}
			if err := ensureUnique(nodeID, "renderer_vlc"); err != nil {
				return nil, err
			}
			mod, err := renderervlc.NewModule(logger.With(zap.String("module", "renderer_vlc")), client, renderervlc.Config{
				NodeID:       nodeID,
				TopicBase:    cfg.Server.TopicBase,
				Name:         cfgItem.Name,
				BaseURL:      cfgItem.BaseURL,
				Username:     cfgItem.Username,
				Password:     cfgItem.Password,
				Timeout:      timeout,
				Volume:       1.0,
				PublishState: true,
			})
			if err != nil {
				return nil, err
			}
			modules = append(modules, mud.ModuleRunner{
				Name: "renderer_vlc",
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
	for _, item := range cfg.Modules.Playlist.List() {
		if item.Config.Enabled {
			out = append(out, "playlist")
			break
		}
	}
	for _, item := range cfg.Modules.BridgeJellyfinLibrary.List() {
		if item.Config.Enabled {
			out = append(out, "bridge_jellyfin_library")
			break
		}
	}
	for _, item := range cfg.Modules.PodcastLibrary.List() {
		if item.Config.Enabled {
			out = append(out, "podcast")
			break
		}
	}
	for _, item := range cfg.Modules.Go2RTCLibrary.List() {
		if item.Config.Enabled {
			out = append(out, "go2rtc")
			break
		}
	}
	for _, item := range cfg.Modules.RendererGStreamer.List() {
		if item.Config.Enabled {
			out = append(out, "renderer_gstreamer")
			break
		}
	}
	for _, item := range cfg.Modules.RendererKodi.List() {
		if item.Config.Enabled {
			out = append(out, "renderer_kodi")
			break
		}
	}
	for _, item := range cfg.Modules.RendererVLC.List() {
		if item.Config.Enabled {
			out = append(out, "renderer_vlc")
			break
		}
	}
	for _, item := range cfg.Modules.BridgeUPNPLibrary.List() {
		if item.Config.Enabled {
			out = append(out, "bridge_upnp_library")
			break
		}
	}
	return out
}

func buildNodeID(kind string, provider string, namespace string, resource string) (string, error) {
	if strings.TrimSpace(provider) == "" {
		return "", fmt.Errorf("%s provider required", kind)
	}
	if strings.TrimSpace(namespace) == "" {
		return "", fmt.Errorf("%s namespace required", kind)
	}
	if strings.TrimSpace(resource) == "" {
		resource = "default"
	}
	return fmt.Sprintf("mu:%s:%s:%s:%s", kind, provider, namespace, resource), nil
}

func parseDurations(inputs []string) ([]time.Duration, error) {
	out := []time.Duration{}
	for _, raw := range inputs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parsed, err := time.ParseDuration(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid duration %q", trimmed)
		}
		out = append(out, parsed)
	}
	return out, nil
}

func durationFromMS(value int64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Millisecond
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
