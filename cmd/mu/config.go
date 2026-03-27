package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/adapters/config"
)

func configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Short:   "Show or manage configuration",
		Long:    "Show or manage the mu CLI configuration. Configuration is stored in a TOML file.",
		GroupID: "discovery",
	}
	cmd.AddCommand(configShowCommand())
	cmd.AddCommand(configPathCommand())
	cmd.AddCommand(configInitCommand())
	return cmd
}

func configShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the current configuration",
		Long:  "Display the current mu CLI configuration including defaults, aliases, and broker settings.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil // No MQTT needed
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Root().PersistentFlags().GetBool("json")

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Determine config path
			path := configFilePath()

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Path      string            `json:"path"`
					Broker    string            `json:"broker"`
					Identity  string            `json:"identity"`
					TopicBase string            `json:"topicBase"`
					Defaults  config.Defaults   `json:"defaults"`
					Aliases   map[string]string `json:"aliases"`
				}{
					Path:      path,
					Broker:    cfg.Broker,
					Identity:  cfg.Identity,
					TopicBase: cfg.TopicBase,
					Defaults:  cfg.Defaults,
					Aliases:   cfg.Aliases,
				})
			}

			// Human-readable output
			fmt.Printf("Config file: %s\n", path)
			fmt.Printf("Broker:      %s\n", valueOrDefault(cfg.Broker, "(not set)"))
			fmt.Printf("Identity:    %s\n", valueOrDefault(cfg.Identity, "(auto)"))
			fmt.Printf("Topic base:  %s\n", valueOrDefault(cfg.TopicBase, "(default)"))
			fmt.Println()
			fmt.Println("Defaults:")
			fmt.Printf("  Renderer:        %s\n", valueOrDefault(cfg.Defaults.Renderer, "(none)"))
			fmt.Printf("  Playlist server: %s\n", valueOrDefault(cfg.Defaults.PlaylistServer, "(none)"))
			fmt.Printf("  Library:         %s\n", valueOrDefault(cfg.Defaults.Library, "(none)"))
			if len(cfg.Aliases) > 0 {
				fmt.Println()
				fmt.Println("Aliases:")
				for alias, target := range cfg.Aliases {
					fmt.Printf("  %s -> %s\n", alias, target)
				}
			}
			return nil
		},
	}
}

func configPathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Show the config file path",
		Long:  "Print the path to the mu configuration file.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(configFilePath())
			return nil
		},
	}
}

func configFilePath() string {
	if p := os.Getenv("MU_CONFIG"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "mu", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/mu/config.toml"
	}
	return filepath.Join(home, ".config", "mu", "config.toml")
}

func configInitCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default configuration file",
		Long: `Create a default mu configuration file. The file is created at the
default config path unless MU_CONFIG is set. Use --force to overwrite an existing file.`,
		Example: `  mu config init
  mu config init --force`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			path := configFilePath()

			if !force {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("config file already exists at %s (use --force to overwrite)", path)
				}
			}

			// Ensure parent directory exists
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating config directory: %w", err)
			}

			defaultConfig := `# mu CLI configuration
# See 'mu config show' for current settings.

# MQTT broker URL (required)
# broker = "tcp://localhost:1883"

# Controller identity (defaults to user@hostname)
# identity = ""

# MQTT topic base
# topic_base = "mu/v1"

# Default selectors (used when no explicit selector is given)
[defaults]
# renderer = "living-room"
# playlist_server = ""
# library = ""

# Aliases map short names to full node IDs
[aliases]
# lr = "mu:renderer:living-room:ns:default"
# jf = "mu:library:jellyfin:ns:default"
`

			if err := os.WriteFile(path, []byte(defaultConfig), 0o644); err != nil {
				return fmt.Errorf("writing config file: %w", err)
			}

			fmt.Printf("Config file created at %s\n", path)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")
	return cmd
}

func valueOrDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
