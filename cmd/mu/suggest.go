package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mikey-austin/media_utopia/internal/adapters/output"
	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/spf13/cobra"
)

func suggestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "suggest",
		Short:   "Manage AI-generated suggestions",
		Long:    "Manage AI-generated playlist suggestions. Suggestions can be previewed, loaded into a renderer, or promoted to saved playlists.",
		GroupID: "content",
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
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List available suggestions",
		Long:    "List available suggestions from the server.",
		Example: `  mu suggest ls
  mu suggest ls --server myserver`,
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
		Use:     "show <suggestionId>",
		Short:   "Show suggestion details",
		Long:    "Show the details and track listing of a suggestion.",
		Example: "  mu suggest show abc-123",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.SuggestShow(ctx, args[0], server)
			if err != nil {
				return err
			}
			if !app.json {
				raw, _ := result.Data.(json.RawMessage)
				return app.printer.Print(output.SuggestShowOutput{Payload: raw})
			}
			return app.printer.Print(core.RawResult{Data: result.Data})
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func suggestPromoteCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:     "promote <suggestionId> <playlistName>",
		Short:   "Promote a suggestion to a saved playlist",
		Long:    "Promote a suggestion to a saved playlist, preserving it permanently.",
		Example: `  mu suggest promote abc-123 "New Discoveries"`,
		Args:    cobra.ExactArgs(2),
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
		Short: "Load a suggestion into the renderer queue",
		Long: `Load a suggestion into a renderer's queue.

Modes:
  replace - Clear the queue and load the suggestion (default)
  append  - Add suggestion tracks to the end of the queue
  next    - Insert suggestion tracks after the currently playing track`,
		Example: `  mu suggest load abc-123
  mu suggest load living-room abc-123
  mu suggest load --mode append abc-123`,
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
