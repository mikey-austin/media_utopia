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

func valueOrDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
