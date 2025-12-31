package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func snapshotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot commands",
	}

	cmd.AddCommand(snapshotSaveCommand())
	cmd.AddCommand(snapshotLoadCommand())
	cmd.AddCommand(snapshotListCommand())

	return cmd
}

func snapshotSaveCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "save [renderer] <name>",
		Short: "Save session snapshot",
		Args:  cobra.RangeArgs(1, 2),
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
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.SnapshotSave(ctx, selector, name, server)
			})
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
		Short: "Load snapshot into renderer queue",
		Args:  cobra.RangeArgs(1, 2),
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
				return fmt.Errorf("mode must be replace|append|next")
			}
			selector := ""
			snapshotID := ""
			if len(args) == 1 {
				snapshotID = args[0]
			} else {
				selector = args[0]
				snapshotID = args[1]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueLoadSnapshot(ctx, selector, snapshotID, modeValue, resolveValue, server)
			})
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
		Use:   "ls",
		Short: "List snapshots",
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
