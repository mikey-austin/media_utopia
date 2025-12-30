package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/adapters/clock"
	"github.com/mikey-austin/media_utopia/internal/adapters/config"
	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"github.com/mikey-austin/media_utopia/internal/adapters/lease"
	"github.com/mikey-austin/media_utopia/internal/adapters/mqtt"
	"github.com/mikey-austin/media_utopia/internal/adapters/output"
	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

type app struct {
	service core.Service
	printer output.Printer
	quiet   bool
	json    bool
	timeout time.Duration
}

func main() {
	root := &cobra.Command{
		Use:   "mu",
		Short: "Media Utopia CLI",
	}

	var (
		broker    string
		topicBase string
		identity  string
		timeout   time.Duration
		quiet     bool
		jsonOut   bool
		noColor   bool
		verbose   bool
		tlsCA     string
		tlsCert   string
		tlsKey    string
		userOpt   string
		passOpt   string
	)

	root.PersistentFlags().StringVarP(&broker, "broker", "b", "", "MQTT broker URL")
	root.PersistentFlags().StringVar(&topicBase, "topic-base", mu.BaseTopic, "MQTT topic base")
	root.PersistentFlags().StringVarP(&identity, "identity", "i", "", "controller identity")
	root.PersistentFlags().DurationVarP(&timeout, "timeout", "t", 2*time.Second, "command timeout")
	root.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-essential output")
	root.PersistentFlags().BoolVarP(&jsonOut, "json", "j", false, "output json")
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable color")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")
	root.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA path")
	root.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS cert path")
	root.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS key path")
	root.PersistentFlags().StringVar(&userOpt, "user", "", "MQTT username")
	root.PersistentFlags().StringVar(&passOpt, "pass", "", "MQTT password")

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if noColor || verbose {
			_ = noColor
			_ = verbose
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		identity = defaultIdentity(identity, cfg.Identity)
		if broker == "" {
			broker = cfg.Broker
		}
		if topicBase == mu.BaseTopic && cfg.TopicBase != "" {
			topicBase = cfg.TopicBase
		}
		if broker == "" {
			return errors.New("broker is required (set --broker or config)")
		}
		if cfg.Aliases == nil {
			cfg.Aliases = map[string]string{}
		}

		leaseStore, err := lease.NewStore()
		if err != nil {
			return err
		}

		clientID := fmt.Sprintf("mu-%d", time.Now().UnixNano())
		mqttClient, err := mqtt.NewClient(mqtt.Options{
			BrokerURL: broker,
			ClientID:  clientID,
			Username:  userOpt,
			Password:  passOpt,
			TLSCA:     tlsCA,
			TLSCert:   tlsCert,
			TLSKey:    tlsKey,
			TopicBase: topicBase,
			Timeout:   timeout,
		})
		if err != nil {
			return err
		}

		coreCfg := core.Config{
			Broker:    broker,
			Identity:  identity,
			TopicBase: topicBase,
			Aliases:   cfg.Aliases,
			Defaults: core.Defaults{
				Renderer:       cfg.Defaults.Renderer,
				PlaylistServer: cfg.Defaults.PlaylistServer,
				Library:        cfg.Defaults.Library,
			},
		}

		resolver := core.Resolver{Presence: mqttClient, Config: coreCfg}
		service := core.Service{
			Broker:     mqttClient,
			Resolver:   resolver,
			Clock:      clock.Clock{},
			IDGen:      idgen.Generator{},
			LeaseStore: leaseStore,
			Config:     coreCfg,
		}

		var printer output.Printer
		if jsonOut {
			printer = output.JSONPrinter{}
		} else {
			printer = output.HumanPrinter{}
		}

		cmd.SetContext(context.WithValue(cmd.Context(), appKey{}, &app{
			service: service,
			printer: printer,
			quiet:   quiet,
			json:    jsonOut,
			timeout: timeout,
		}))
		return nil
	}

	root.AddCommand(lsCommand())
	root.AddCommand(statusCommand())
	root.AddCommand(acquireCommand())
	root.AddCommand(renewCommand())
	root.AddCommand(releaseCommand())
	root.AddCommand(ownerCommand())
	root.AddCommand(playCommand())
	root.AddCommand(pauseCommand())
	root.AddCommand(toggleCommand())
	root.AddCommand(stopCommand())
	root.AddCommand(seekCommand())
	root.AddCommand(nextCommand())
	root.AddCommand(prevCommand())
	root.AddCommand(volumeCommand())
	root.AddCommand(queueCommand())
	root.AddCommand(playlistCommand())
	root.AddCommand(snapshotCommand())
	root.AddCommand(libraryCommand())
	root.AddCommand(suggestCommand())

	if err := root.Execute(); err != nil {
		os.Exit(core.ExitCode(err))
	}
}

type appKey struct{}

func fromContext(cmd *cobra.Command) *app {
	val := cmd.Context().Value(appKey{})
	if val == nil {
		return nil
	}
	return val.(*app)
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}

func defaultIdentity(flagVal string, cfgVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if cfgVal != "" {
		return cfgVal
	}
	usr, _ := user.Current()
	host, _ := os.Hostname()
	if usr != nil && host != "" {
		return fmt.Sprintf("%s@%s", usr.Username, host)
	}
	if host != "" {
		return host
	}
	return "mu-unknown"
}

func readFileOrStdin(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func normalizeResolve(arg string) (string, error) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	switch arg {
	case "", "auto", "yes", "no":
		if arg == "" {
			return "auto", nil
		}
		return arg, nil
	default:
		return "", fmt.Errorf("resolve must be auto|yes|no")
	}
}
