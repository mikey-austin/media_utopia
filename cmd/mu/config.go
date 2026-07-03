package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mikey-austin/media_utopia/internal/adapters/config"
	"github.com/mikey-austin/media_utopia/internal/adapters/output"
	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
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
	cmd.AddCommand(configSetDefaultCommand())
	cmd.AddCommand(configSwitchCommand())
	cmd.AddCommand(configProfileCommand())
	cmd.AddCommand(configAliasCommand())
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
					Path          string                     `json:"path"`
					Broker        string                     `json:"broker"`
					Identity      string                     `json:"identity"`
					TopicBase     string                     `json:"topicBase"`
					ActiveProfile string                     `json:"activeProfile,omitempty"`
					Defaults      config.Defaults            `json:"defaults"`
					Effective     config.Defaults            `json:"effectiveDefaults"`
					Profiles      map[string]config.Defaults `json:"profiles,omitempty"`
					Aliases       map[string]string          `json:"aliases,omitempty"`
				}{
					Path:          path,
					Broker:        cfg.Broker,
					Identity:      cfg.Identity,
					TopicBase:     cfg.TopicBase,
					ActiveProfile: cfg.ActiveProfile,
					Defaults:      cfg.Defaults,
					Effective:     cfg.EffectiveDefaults(),
					Profiles:      cfg.Profiles,
					Aliases:       cfg.Aliases,
				})
			}

			// Human-readable output
			fmt.Print(output.RenderDetails([][2]string{
				{"Config file", path},
				{"Broker", valueOrDefault(cfg.Broker, "(not set)")},
				{"Identity", valueOrDefault(cfg.Identity, "(auto)")},
				{"Topic base", valueOrDefault(cfg.TopicBase, "(default)")},
				{"Profile", valueOrDefault(cfg.ActiveProfile, "(none)")},
			}))
			eff := cfg.EffectiveDefaults()
			fmt.Println()
			if eff == (config.Defaults{}) {
				fmt.Println("No defaults set. Try: mu config set-default renderer")
			} else {
				fmt.Println("Effective defaults:")
				printDefaults(eff)
			}
			if len(cfg.Profiles) > 0 {
				fmt.Println()
				fmt.Println("Profiles:")
				_ = printProfiles(cfg)
			}
			if len(cfg.Aliases) > 0 {
				fmt.Println()
				fmt.Println("Aliases:")
				names := make([]string, 0, len(cfg.Aliases))
				for alias := range cfg.Aliases {
					names = append(names, alias)
				}
				sort.Strings(names)
				for _, alias := range names {
					fmt.Printf("  %s %s %s\n", output.Bold(alias), output.Dim("\u2192"), cfg.Aliases[alias])
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

# Default selectors (used when no explicit selector is given).
# Prefer 'mu config set-default renderer' — it resolves names against the
# network for you, no URNs needed.
[defaults]
# renderer = ""
# playlist_server = ""
# library = ""

# Profiles are named sets of defaults; switch with 'mu config switch <name>'.
# [profiles.office]
# renderer = ""

# Aliases map short names to nodes ('mu config alias set lr "living room"').
[aliases]
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

// defaultKinds maps user-facing kind spellings to the presence kind and the
// config field writer.
var defaultKinds = map[string]string{
	"renderer":        "renderer",
	"library":         "library",
	"lib":             "library",
	"playlist":        "playlist",
	"playlist-server": "playlist",
	"playlist_server": "playlist",
	"zone":            "zone",
}

func setDefaultField(d *config.Defaults, kind string, nodeID string) {
	switch kind {
	case "renderer":
		d.Renderer = nodeID
	case "library":
		d.Library = nodeID
	case "playlist":
		d.PlaylistServer = nodeID
	case "zone":
		d.Zone = nodeID
	}
}

func configSetDefaultCommand() *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:   "set-default <renderer|library|playlist> [selector]",
		Short: "Set a default node from the discovered set",
		Long: `Set a default node, resolved against what is currently discoverable on
the network — no URNs needed. With no selector and an interactive
terminal, presents a numbered picker of the discovered nodes.

The resolved node ID is stored, so the default keeps working even when
node names change or several nodes share a prefix.

With --profile the default is written into that profile (created on
first use) instead of the top-level defaults; activate profiles with
'mu config switch'.`,
		Example: `  mu config set-default renderer            # pick interactively
  mu config set-default renderer "mpv 1"    # forgiving name matching
  mu config set-default library venus
  mu config set-default renderer office-spk --profile office`,
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeDefaultKinds,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			kind, ok := defaultKinds[strings.ToLower(args[0])]
			if !ok {
				return core.NewUsageError("unknown default kind " + args[0] + " (renderer|library|playlist)")
			}

			var node mu.Presence
			var err error
			if len(args) == 2 {
				switch kind {
				case "renderer":
					node, err = app.service.Resolver.ResolveRenderer(ctx, args[1])
				case "library":
					node, err = app.service.Resolver.ResolveLibrary(ctx, args[1])
				case "playlist":
					node, err = app.service.Resolver.ResolvePlaylistServer(ctx, args[1])
				case "zone":
					node, err = app.service.Resolver.ResolveZone(ctx, args[1])
				}
			} else {
				node, err = pickNode(ctx, app, kind)
			}
			if err != nil {
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			target := "defaults"
			if profile != "" {
				if cfg.Profiles == nil {
					cfg.Profiles = map[string]config.Defaults{}
				}
				p := cfg.Profiles[profile]
				setDefaultField(&p, kind, node.NodeID)
				cfg.Profiles[profile] = p
				target = "profile " + profile
			} else {
				setDefaultField(&cfg.Defaults, kind, node.NodeID)
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("default %s = %s (%s) in %s\n", kind, output.Bold(node.Name), output.Dim(node.NodeID), target)
			return nil
		},
	}
	cmd.Flags().StringVarP(&profile, "profile", "p", "", "write into this profile instead of the top-level defaults")
	return cmd
}

// pickNode presents an interactive numbered picker over the discovered
// nodes of the given kind. Falls back to a helpful error when stdin is not
// a terminal.
func pickNode(ctx context.Context, app *app, kind string) (mu.Presence, error) {
	result, err := app.service.ListNodes(ctx, kind, true)
	if err != nil {
		return mu.Presence{}, err
	}
	nodes := result.Nodes
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	switch len(nodes) {
	case 0:
		return mu.Presence{}, core.NewNotFoundError("no " + kind + " nodes discovered")
	case 1:
		return nodes[0], nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		names := make([]string, 0, len(nodes))
		for _, n := range nodes {
			names = append(names, n.Name)
		}
		return mu.Presence{}, core.NewUsageError("selector required (available: " + strings.Join(names, ", ") + ")")
	}
	for i, n := range nodes {
		fmt.Printf("  %s %s %s\n", output.Dim(fmt.Sprintf("%2d)", i+1)), n.Name, output.Dim(n.NodeID))
	}
	fmt.Printf("Pick a %s [1-%d]: ", kind, len(nodes))
	var choice int
	if _, err := fmt.Fscanln(os.Stdin, &choice); err != nil || choice < 1 || choice > len(nodes) {
		return mu.Presence{}, core.NewUsageError("invalid choice")
	}
	return nodes[choice-1], nil
}

func completeDefaultKinds(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return []string{"renderer", "library", "playlist", "zone"}, cobra.ShellCompDirectiveNoFileComp
}

func configSwitchCommand() *cobra.Command {
	var clear bool

	cmd := &cobra.Command{
		Use:   "switch [profile]",
		Short: "Switch the active defaults profile",
		Long: `Switch which profile of defaults is active. A profile overrides the
top-level defaults for the fields it sets; unset fields fall through.

With no arguments, lists the profiles and marks the active one.`,
		Example: `  mu config switch            # list profiles
  mu config switch office     # activate 'office'
  mu config switch --clear    # back to top-level defaults`,
		Args: cobra.MaximumNArgs(1),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil // config-file only, no MQTT
		},
		ValidArgsFunction: completeProfiles,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			switch {
			case clear:
				cfg.ActiveProfile = ""
				if err := config.Save(cfg); err != nil {
					return err
				}
				fmt.Println("using top-level defaults")
				return nil
			case len(args) == 0:
				return printProfiles(cfg)
			default:
				name := args[0]
				if _, ok := cfg.Profiles[name]; !ok {
					return core.NewNotFoundError(fmt.Sprintf("no profile %q (create one: mu config set-default renderer <name> --profile %s)", name, name))
				}
				cfg.ActiveProfile = name
				if err := config.Save(cfg); err != nil {
					return err
				}
				d := cfg.EffectiveDefaults()
				fmt.Printf("switched to profile %s\n", output.Bold(name))
				printDefaults(d)
				return nil
			}
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "deactivate the profile and use top-level defaults")
	return cmd
}

func printProfiles(cfg config.Config) error {
	if len(cfg.Profiles) == 0 {
		fmt.Println("No profiles. Create one: mu config set-default renderer <name> --profile <profile>")
		return nil
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		marker := " "
		if name == cfg.ActiveProfile {
			marker = output.Green("*")
		}
		p := cfg.Profiles[name]
		parts := []string{}
		if p.Renderer != "" {
			parts = append(parts, "renderer="+shortNodeID(p.Renderer))
		}
		if p.Library != "" {
			parts = append(parts, "library="+shortNodeID(p.Library))
		}
		if p.PlaylistServer != "" {
			parts = append(parts, "playlist="+shortNodeID(p.PlaylistServer))
		}
		if p.Zone != "" {
			parts = append(parts, "zone="+shortNodeID(p.Zone))
		}
		fmt.Printf("%s %s  %s\n", marker, output.Bold(name), output.Dim(strings.Join(parts, " ")))
	}
	return nil
}

// shortNodeID renders a URN by its most meaningful tail segments.
func shortNodeID(id string) string {
	parts := strings.Split(id, ":")
	if len(parts) >= 2 && strings.HasPrefix(id, "mu:") {
		return strings.Join(parts[len(parts)-2:], ":")
	}
	return id
}

func printDefaults(d config.Defaults) {
	if d.Renderer != "" {
		fmt.Printf("  renderer  %s\n", output.Dim(d.Renderer))
	}
	if d.Library != "" {
		fmt.Printf("  library   %s\n", output.Dim(d.Library))
	}
	if d.PlaylistServer != "" {
		fmt.Printf("  playlist  %s\n", output.Dim(d.PlaylistServer))
	}
	if d.Zone != "" {
		fmt.Printf("  zone      %s\n", output.Dim(d.Zone))
	}
}

func configProfileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage defaults profiles",
	}

	ls := &cobra.Command{
		Use:   "ls",
		Short: "List profiles",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return printProfiles(cfg)
		},
	}

	save := &cobra.Command{
		Use:   "save <name>",
		Short: "Save the current effective defaults as a profile",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.Profiles == nil {
				cfg.Profiles = map[string]config.Defaults{}
			}
			cfg.Profiles[args[0]] = cfg.EffectiveDefaults()
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("profile %s saved\n", output.Bold(args[0]))
			printDefaults(cfg.Profiles[args[0]])
			return nil
		},
	}

	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a profile",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeProfiles,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[args[0]]; !ok {
				return core.NewNotFoundError("no profile " + args[0])
			}
			delete(cfg.Profiles, args[0])
			if cfg.ActiveProfile == args[0] {
				cfg.ActiveProfile = ""
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("profile %s removed\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(ls, save, rm)
	return cmd
}

func completeProfiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

func configAliasCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage selector aliases",
		Long: `Aliases map a short name of your choosing to a node. They work anywhere
a selector does: 'mu play lr', 'mu lib browse tunes', …`,
	}

	set := &cobra.Command{
		Use:   "set <alias> <selector>",
		Short: "Create or update an alias (selector resolved against the network)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			// Resolve against any node kind so aliases can point anywhere.
			nodes, err := app.service.ListNodes(ctx, "", true)
			if err != nil {
				return err
			}
			node, err := core.ResolveSelectorIn(args[1], nodes.Nodes, nil)
			if err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.Aliases == nil {
				cfg.Aliases = map[string]string{}
			}
			cfg.Aliases[args[0]] = node.NodeID
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("alias %s = %s (%s)\n", output.Bold(args[0]), node.Name, output.Dim(node.NodeID))
			return nil
		},
	}

	rm := &cobra.Command{
		Use:   "rm <alias>",
		Short: "Remove an alias",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Aliases[args[0]]; !ok {
				return core.NewNotFoundError("no alias " + args[0])
			}
			delete(cfg.Aliases, args[0])
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("alias %s removed\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(set, rm)
	return cmd
}
