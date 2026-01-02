package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func playlistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlist",
		Short: "Playlist commands",
	}

	cmd.AddCommand(playlistListCommand())
	cmd.AddCommand(playlistShowCommand())
	cmd.AddCommand(playlistCreateCommand())
	cmd.AddCommand(playlistAddCommand())
	cmd.AddCommand(playlistRemoveCommand())
	cmd.AddCommand(playlistDeleteCommand())
	cmd.AddCommand(playlistLoadCommand())
	cmd.AddCommand(playlistRenameCommand())

	return cmd
}

func playlistListCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List playlists",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.PlaylistList(ctx, server)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func playlistShowCommand() *cobra.Command {
	var server string
	var full bool

	cmd := &cobra.Command{
		Use:   "show <playlistId|name>",
		Short: "Show playlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			if !app.json {
				result, err := app.service.PlaylistShow(ctx, args[0], server, true, full)
				if err != nil {
					return err
				}
				return app.printer.Print(result)
			}
			result, err := app.service.PlaylistGet(ctx, args[0], server)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	cmd.Flags().BoolVar(&full, "full", false, "show full ids")
	return cmd
}

func playlistCreateCommand() *cobra.Command {
	var server string
	var fromSnapshot string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create playlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			return app.service.PlaylistCreate(ctx, args[0], fromSnapshot, server)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	cmd.Flags().StringVar(&fromSnapshot, "from-snapshot", "", "create from snapshot id or name")
	return cmd
}

func playlistAddCommand() *cobra.Command {
	var resolve string
	var server string

	cmd := &cobra.Command{
		Use:   "add <playlistId|name> <item...>",
		Short: "Add items to playlist",
		Long: "Add items to a playlist.\n" +
			"Items can be:\n" +
			"  - http(s) URLs\n" +
			"  - mu URNs (mu:...)\n" +
			"  - library refs (lib:<selector>:<itemId>)\n" +
			"    where selector can be a library alias or full nodeId\n" +
			"    container items (albums/artists) expand into playable tracks\n" +
			"\n" +
			"Examples:\n" +
			"  mu playlist add <playlistId> https://example.com/a.mp3\n" +
			"  mu playlist add \"Evening Miles\" https://example.com/a.mp3\n" +
			"  mu playlist add <playlistId> lib:jellyfin:ITEM_ID --resolve yes\n" +
			"  mu playlist add <playlistId> lib:mu:library:jellyfin:ns:default:ITEM_ID --resolve yes\n",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			resolveValue, err := normalizeResolve(resolve)
			if err != nil {
				return err
			}
			return app.service.PlaylistAdd(ctx, args[0], args[1:], resolveValue, server)
		},
	}

	cmd.Flags().StringVar(&resolve, "resolve", "auto", "resolve mode (auto|yes|no)")
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func playlistRemoveCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "rm <playlistId|name> <entryId...>",
		Short: "Remove items from playlist",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			return app.service.PlaylistRemove(ctx, args[0], args[1:], server)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func playlistDeleteCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:     "delete <playlistId|name>",
		Aliases: []string{"del"},
		Short:   "Delete playlist",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			return app.service.PlaylistDelete(ctx, args[0], server)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func playlistLoadCommand() *cobra.Command {
	var mode string
	var resolve string
	var server string

	cmd := &cobra.Command{
		Use:   "load [renderer] <playlistId|name>",
		Short: "Load playlist into renderer queue",
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
			playlistID := ""
			if len(args) == 1 {
				playlistID = args[0]
			} else {
				selector = args[0]
				playlistID = args[1]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueLoadPlaylist(ctx, selector, playlistID, modeValue, resolveValue, server)
			})
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "replace", "load mode (replace|append|next)")
	cmd.Flags().StringVar(&resolve, "resolve", "auto", "resolve mode (auto|yes|no)")
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}

func playlistRenameCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "rename <playlistId|name> <name>",
		Short: "Rename playlist",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			return app.service.PlaylistRename(ctx, args[0], args[1], server)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "playlist server selector")
	return cmd
}
