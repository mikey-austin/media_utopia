package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func playlistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlist",
		Short: "Manage saved playlists",
		Long: `Manage saved playlists on a playlist server. Playlists persist across
sessions and can be loaded into any renderer's queue.`,
		GroupID: "content",
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
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List all playlists",
		Long:    "List all saved playlists on the server.",
		Example: `  mu playlist ls
  mu playlist ls --server myserver`,
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
		Use:     "show <playlistId|name>",
		Aliases: []string{"get", "info"},
		Short:   "Show playlist contents",
		Long: `Show the contents of a playlist with track details. Playlists can be
referenced by name or ID. Use --full to include entry IDs for scripting.`,
		Example: `  mu playlist show "Evening Jazz"
  mu playlist show abc-123 --full`,
		Args: cobra.ExactArgs(1),
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
		Short: "Create a new playlist",
		Long: `Create a new empty playlist. Use --from-snapshot to create a playlist
from an existing session snapshot.`,
		Example: `  mu playlist create "Road Trip"
  mu playlist create "Live Set" --from-snapshot my-snapshot`,
		Args: cobra.ExactArgs(1),
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
		Short: "Add items to a playlist",
		Long: "Add items to a playlist.\n" +
			"Items can be:\n" +
			"  - http(s) URLs\n" +
			"  - mu URNs (mu:...)\n" +
			"  - library refs (lib:<selector>:<itemId>)\n" +
			"    where selector can be a library alias or full nodeId\n" +
			"    container items (albums/artists) expand into playable tracks\n",
		Example: `  mu playlist add "Evening Jazz" https://example.com/song.mp3
  mu playlist add "Evening Jazz" lib:jellyfin:abc123 --resolve yes`,
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
		Short: "Remove entries from a playlist",
		Long: `Remove one or more entries from a playlist by entry ID.
Use 'mu playlist show --full' to see entry IDs.`,
		Example: "  mu playlist rm \"Evening Jazz\" entry-id-1 entry-id-2",
		Args:    cobra.MinimumNArgs(2),
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
		Aliases: []string{"del", "rm", "remove"},
		Short:   "Delete a playlist",
		Long:    "Permanently delete a playlist from the server.",
		Example: `  mu playlist delete "Old Playlist"
  mu playlist del abc-123`,
		Args: cobra.ExactArgs(1),
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
		Short: "Load a playlist into the renderer queue",
		Long: `Load a playlist into a renderer's queue.

Modes:
  replace - Clear the queue and load the playlist (default)
  append  - Add playlist tracks to the end of the queue
  next    - Insert playlist tracks after the currently playing track`,
		Example: `  mu playlist load "Evening Jazz"
  mu playlist load living-room "Evening Jazz"
  mu playlist load --mode append "Road Trip"`,
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
		Use:     "rename <playlistId|name> <name>",
		Short:   "Rename a playlist",
		Long:    "Rename an existing playlist.",
		Example: `  mu playlist rename "Old Name" "New Name"`,
		Args:    cobra.ExactArgs(2),
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
