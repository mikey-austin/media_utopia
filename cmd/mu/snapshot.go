package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func snapshotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage session snapshots",
		Long: `Manage session snapshots. A snapshot captures the current queue state
and can be restored later or promoted to a saved playlist.`,
		GroupID: "content",
	}

	cmd.AddCommand(snapshotSaveCommand())
	cmd.AddCommand(snapshotLoadCommand())
	cmd.AddCommand(snapshotListCommand())
	cmd.AddCommand(snapshotRemoveCommand())

	return cmd
}

func snapshotSaveCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:     "save [renderer] <name>",
		Short:   "Save the current session as a snapshot",
		Long:    "Save the current renderer session (queue, position, volume) as a named snapshot.",
		Example: `  mu snapshot save "Friday Night"
  mu snapshot save living-room "Party Mix"`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else {
				selector = args[0]
				name = args[1]
			}
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.SnapshotSave(ctx, selector, name, server)
			}); err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Printf("Snapshot %q saved\n", name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func snapshotLoadCommand() *cobra.Command {
	var mode string
	var resolve string
	var server string

	cmd := &cobra.Command{
		Use:   "load [renderer] <snapshotId>",
		Short: "Restore a snapshot into the renderer queue",
		Long: `Restore a snapshot into a renderer's queue.

Modes:
  replace - Clear the queue and load the snapshot (default)
  append  - Add snapshot tracks to the end of the queue
  next    - Insert snapshot tracks after the currently playing track`,
		Example: `  mu snapshot load "Friday Night"
  mu snapshot load living-room abc-123
  mu snapshot load --mode append "Party Mix"`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			resolveValue, err := normalizeResolve(resolve)
			if err != nil {
				return err
			}
			modeValue := mode
			if modeValue == "" {
				modeValue = "replace"
			}
			switch modeValue {
			case "replace", "append", "next":
			default:
				return fmt.Errorf("invalid mode %q: must be replace, append, or next", modeValue)
			}
			selector := ""
			snapshotID := ""
			if len(args) == 1 {
				snapshotID = args[0]
			} else {
				selector = args[0]
				snapshotID = args[1]
			}
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueLoadSnapshot(ctx, selector, snapshotID, modeValue, resolveValue, server)
			}); err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Println("Snapshot loaded")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "replace", "load mode (replace|append|next)")
	cmd.Flags().StringVar(&resolve, "resolve", "auto", "resolve mode (auto|yes|no)")
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func snapshotListCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List saved snapshots",
		Long:    "List all saved snapshots on the server.",
		Example: `  mu snapshot ls
  mu snapshot ls --server myserver`,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.SnapshotList(ctx, server)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func snapshotRemoveCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:     "rm <snapshotId|name>",
		Aliases: []string{"remove", "del", "delete"},
		Short:   "Delete a snapshot",
		Long:    "Delete a snapshot from the server.",
		Example: `  mu snapshot rm "Old Snapshot"
  mu snapshot rm abc-123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			if err := app.service.SnapshotRemove(ctx, args[0], server); err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Println("Snapshot deleted")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}
