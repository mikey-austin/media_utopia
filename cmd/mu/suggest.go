package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func suggestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggest",
		Short: "Suggestion commands",
	}

	cmd.AddCommand(suggestListCommand())
	cmd.AddCommand(suggestShowCommand())
	cmd.AddCommand(suggestPromoteCommand())
	cmd.AddCommand(suggestLoadCommand())

	return cmd
}

func suggestListCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List suggestions",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.SuggestList(ctx, server)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func suggestShowCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "show <suggestionId>",
		Short: "Show suggestion",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.SuggestShow(ctx, args[0], server)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func suggestPromoteCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "promote <suggestionId> <playlistName>",
		Short: "Promote suggestion to playlist",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			return app.service.SuggestPromote(ctx, args[0], args[1], server)
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func suggestLoadCommand() *cobra.Command {
	var mode string
	var resolve string
	var server string

	cmd := &cobra.Command{
		Use:   "load [renderer] <suggestionId>",
		Short: "Load suggestion into renderer queue",
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
			suggestionID := ""
			if len(args) == 1 {
				suggestionID = args[0]
			} else {
				selector = args[0]
				suggestionID = args[1]
			}
			return app.service.SuggestLoad(ctx, selector, suggestionID, modeValue, resolveValue, server)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "replace", "load mode (replace|append|next)")
	cmd.Flags().StringVar(&resolve, "resolve", "auto", "resolve mode (auto|yes|no)")
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}
