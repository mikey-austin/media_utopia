package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/adapters/output"
)

func zoneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "zone",
		Short:   "Control speaker zones",
		GroupID: "playback",
		Long: `Control speaker zones: volumes, mutes, and which source plays where.

Zone selectors use the same forgiving matching as everywhere else (name,
unique prefix, or substring), and 'mu config set-default zone <name>'
makes the zone argument optional.`,
	}
	cmd.AddCommand(zoneListCommand())
	cmd.AddCommand(zoneVolumeCommand())
	cmd.AddCommand(zoneMuteCommand())
	cmd.AddCommand(zoneSourceCommand())
	cmd.AddCommand(zoneSourcesCommand())
	return cmd
}

func zoneListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List zones with volume, mute, and routed source",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.ZoneList(ctx)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
}

func zoneVolumeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vol [zone] <0..100|+/-n>",
		Short: "Set a zone's volume",
		Example: `  mu zone vol kitchen 40
  mu zone vol kitchen +5
  mu zone vol 25            # default zone`,
		ValidArgsFunction: completeZones,
		Args:              cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector, arg := "", args[0]
			if len(args) == 2 {
				selector, arg = args[0], args[1]
			} else if !looksLikeVolume(args[0]) {
				return fmt.Errorf("volume value required (0-100, +N, or -N)")
			}
			zone, vol, err := app.service.ZoneSetVolume(ctx, selector, arg)
			if err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Printf("%s volume %d%%\n", output.Bold(zone.Name), int(vol*100+0.5))
			}
			return nil
		},
	}
	// Let "-5" pass through as a relative volume rather than a flag: stop
	// flag parsing at the first positional arg, and ignore an unknown
	// leading "-N" (use 'mu zone vol -- -5' for the default zone).
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().ParseErrorsWhitelist.UnknownFlags = true
	return cmd
}

func zoneMuteCommand() *cobra.Command {
	var on, off bool

	cmd := &cobra.Command{
		Use:   "mute [zone]",
		Short: "Toggle (or set) a zone's mute",
		Example: `  mu zone mute kitchen        # toggle
  mu zone mute kitchen --on
  mu zone mute kitchen --off`,
		ValidArgsFunction: completeZones,
		Args:              cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			if on && off {
				return fmt.Errorf("--on and --off are mutually exclusive")
			}
			var mutePtr *bool
			if on || off {
				val := on
				mutePtr = &val
			}
			zone, muted, err := app.service.ZoneSetMute(ctx, selectorArg(args), mutePtr)
			if err != nil {
				return err
			}
			if !app.quiet && !app.json {
				state := "unmuted"
				if muted {
					state = "muted"
				}
				fmt.Printf("%s %s\n", output.Bold(zone.Name), state)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&on, "on", false, "mute")
	cmd.Flags().BoolVar(&off, "off", false, "unmute")
	return cmd
}

func zoneSourceCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "source [zone] <source>",
		Short: "Route a source to a zone",
		Long: `Route a source to a zone. Sources match by name (or unique prefix or
substring) against what the zone controller advertises — see
'mu zone sources'.`,
		Example: `  mu zone source kitchen mpv1
  mu zone source office "MPV 2"
  mu zone source gstreamer1        # default zone`,
		ValidArgsFunction: completeZones,
		Args:              cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector, source := "", args[0]
			if len(args) == 2 {
				selector, source = args[0], args[1]
			}
			zone, src, err := app.service.ZoneSelectSource(ctx, selector, source)
			if err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Printf("%s %s %s\n", output.Bold(zone.Name), output.Dim("←"), src.Name)
			}
			return nil
		},
	}
}

func zoneSourcesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "List the sources the zone controller offers",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.ZoneList(ctx)
			if err != nil {
				return err
			}
			return app.printer.Print(output.ZoneSourcesOutput{Sources: result.Sources})
		},
	}
}

// completeZones suggests zone names for the first argument.
func completeZones(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	app := fromContext(cmd)
	if app == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	result, err := app.service.ListNodes(ctx, "zone", true)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(result.Nodes))
	for _, node := range result.Nodes {
		names = append(names, node.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
