package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/adapters/output"
)

func queueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Manage the playback queue",
		Long: `Manage the playback queue on a renderer. The queue holds the list of
items to play. Most queue commands auto-acquire a lease when needed.`,
		GroupID: "queue",
	}

	cmd.AddCommand(queueListCommand())
	cmd.AddCommand(queueNowCommand())
	cmd.AddCommand(queueClearCommand())
	cmd.AddCommand(queueJumpCommand())
	cmd.AddCommand(queueRemoveCommand())
	cmd.AddCommand(queueMoveCommand())
	cmd.AddCommand(queueShuffleCommand())
	cmd.AddCommand(queueRepeatCommand())
	cmd.AddCommand(queueAddCommand())
	cmd.AddCommand(queueSetCommand())

	return cmd
}

func queueListCommand() *cobra.Command {
	var offset int64
	var count int64
	var full bool

	cmd := &cobra.Command{
		Use:     "list [renderer]",
		Aliases: []string{"ls"},
		Short:   "List queue entries",
		Long: `List entries in the playback queue with title, artist, album, and duration.
Use --full to include queue entry IDs and item IDs for scripting.`,
		Example: `  mu queue list
  mu queue list living-room
  mu queue list --from 10 --count 20
  mu queue list --full`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			result, err := app.service.QueueList(ctx, selector, offset, count, !app.json, full)
			if err != nil {
				return err
			}
			if !app.json {
				return app.printer.Print(output.QueueListOutput{
					Result: result,
					Offset: offset,
					Count:  count,
				})
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().Int64Var(&offset, "offset", 0, "start index")
	cmd.Flags().Int64Var(&count, "count", 50, "number of entries")
	cmd.Flags().BoolVar(&full, "full", false, "show full ids")
	return cmd
}

func queueNowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "now [renderer]",
		Short:   "Show the currently playing item",
		Long:    "Show the currently playing item from the queue.",
		Example: `  mu queue now
  mu queue now living-room`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			result, err := app.service.QueueNow(ctx, selector)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
	return cmd
}

func queueClearCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "clear [renderer]",
		Short:   "Clear all entries from the queue",
		Long:    "Remove all entries from the playback queue.",
		Example: `  mu queue clear
  mu queue clear living-room`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueClear(ctx, selector)
			})
		},
	}
}

func queueJumpCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "jump [renderer] <index>",
		Short:   "Jump to a specific queue index",
		Long:    "Jump to a specific index in the queue and start playback from there.",
		Example: `  mu queue jump 5
  mu queue jump living-room 10`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := ""
			indexArg := ""
			if len(args) == 1 {
				indexArg = args[0]
			} else {
				selector = args[0]
				indexArg = args[1]
			}
			index, err := strconv.ParseInt(indexArg, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid index %q: expected an integer", indexArg)
			}
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueJump(ctx, selector, index)
			})
		},
	}
}

func queueRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rm [renderer] <index|queueEntryId>",
		Aliases: []string{"remove", "del", "delete"},
		Short:   "Remove an entry from the queue",
		Long: `Remove an entry from the queue by index or queue entry ID.
Use 'mu queue list --full' to see queue entry IDs.`,
		Example: `  mu queue rm 3
  mu queue rm living-room 0
  mu queue rm living-room abc-123-def`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			selector := ""
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			} else {
				selector = args[0]
				arg = args[1]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueRemove(ctx, selector, arg)
			})
		},
	}
}

func queueMoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "mv [renderer] <from> <to>",
		Aliases: []string{"move"},
		Short:   "Move a queue entry to a new position",
		Long:    "Move a queue entry from one position to another.",
		Example: `  mu queue mv 5 0
  mu queue mv living-room 3 7`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := ""
			fromArg := ""
			toArg := ""
			if len(args) == 2 {
				fromArg = args[0]
				toArg = args[1]
			} else {
				selector = args[0]
				fromArg = args[1]
				toArg = args[2]
			}
			from, err := strconv.ParseInt(fromArg, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid source index %q: expected an integer", fromArg)
			}
			to, err := strconv.ParseInt(toArg, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid destination index %q: expected an integer", toArg)
			}
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueMove(ctx, selector, from, to)
			})
		},
	}
}

func queueShuffleCommand() *cobra.Command {
	var seed int64

	cmd := &cobra.Command{
		Use:     "shuffle [renderer]",
		Short:   "Shuffle the queue order",
		Long:    "Randomly reorder the entries in the queue. Use --seed for reproducible results.",
		Example: `  mu queue shuffle
  mu queue shuffle living-room
  mu queue shuffle --seed 42`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueShuffle(ctx, selector, seed)
			})
		},
	}

	cmd.Flags().Int64Var(&seed, "seed", 0, "shuffle seed")
	return cmd
}

func queueRepeatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "repeat [renderer] off|all|one",
		Short: "Set the repeat mode",
		Long: `Set the repeat mode for the queue.

Modes:
  off  - No repeat (stop after last track)
  all  - Repeat the entire queue
  one  - Repeat the current track`,
		Example: `  mu queue repeat all
  mu queue repeat one
  mu queue repeat living-room off`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := ""
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			} else {
				selector = args[0]
				arg = args[1]
			}
			mode := strings.ToLower(strings.TrimSpace(arg))
			switch mode {
			case "off", "all", "one":
			default:
				return fmt.Errorf("invalid repeat mode %q: must be off, all, or one", mode)
			}
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueRepeat(ctx, selector, mode)
			})
		},
	}
}

func queueAddCommand() *cobra.Command {
	var atIndex int64
	var atSet bool
	var next bool
	var end bool
	var resolve string

	cmd := &cobra.Command{
		Use:   "add [renderer] <item...>",
		Short: "Add items to the queue",
		Long: `Add one or more items to the queue.

Items can be URLs, mu URNs (mu:...), or library references (lib:<library>:<itemId>).
By default items are appended to the end. Use --next to insert after the current
track, or --at to insert at a specific position.`,
		Example: `  mu queue add https://example.com/song.mp3
  mu queue add lib:jellyfin:abc123
  mu queue add --next lib:jellyfin:abc123
  mu queue add --at 0 https://example.com/song.mp3
  mu queue add living-room lib:jellyfin:abc123 lib:jellyfin:def456`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			resolveValue, err := normalizeResolve(resolve)
			if err != nil {
				return err
			}

			position := "end"
			var indexPtr *int64
			switch {
			case next:
				position = "next"
			case atSet:
				position = "at"
				indexPtr = &atIndex
			case end:
				position = "end"
			}

			selector := ""
			items := args
			if len(args) > 1 && !looksLikeItem(args[0]) {
				selector = args[0]
				items = args[1:]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueAdd(ctx, selector, items, position, indexPtr, resolveValue)
			})
		},
	}

	cmd.Flags().Int64Var(&atIndex, "at", 0, "insert at index")
	cmd.Flags().BoolVar(&next, "next", false, "insert next")
	cmd.Flags().BoolVar(&end, "end", false, "append at end")
	cmd.Flags().StringVar(&resolve, "resolve", "auto", "resolve mode (auto|yes|no)")

	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("at") {
			atSet = true
		}
		return nil
	}

	return cmd
}

func looksLikeItem(arg string) bool {
	return strings.HasPrefix(arg, "mu:") ||
		strings.HasPrefix(arg, "lib:") ||
		strings.HasPrefix(arg, "playlist:") ||
		strings.HasPrefix(arg, "http://") ||
		strings.HasPrefix(arg, "https://")
}

func queueSetCommand() *cobra.Command {
	var file string
	var format string
	var ifRev int64
	var ifRevSet bool

	cmd := &cobra.Command{
		Use:   "set [renderer] --file <path>|-",
		Short: "Replace the entire queue from a file",
		Example: `  mu queue set --file playlist.muq
  mu queue set --file playlist.json --format json
  cat tracks.muq | mu queue set --file -`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			if file == "" {
				return fmt.Errorf("--file is required: specify a file path or - for stdin")
			}
			data, err := readFileOrStdin(file)
			if err != nil {
				return err
			}

			entries, err := app.service.QueueEntriesFromFile(format, data)
			if err != nil {
				return err
			}
			var revPtr *int64
			if ifRevSet {
				revPtr = &ifRev
			}
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueSet(ctx, selector, entries, revPtr)
			})
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "queue file path or - for stdin")
	cmd.Flags().StringVar(&format, "format", "muq", "queue file format (muq|json)")
	cmd.Flags().Int64Var(&ifRev, "if-rev", 0, "revision guard")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("if-rev") {
			ifRevSet = true
		}
		return nil
	}

	return cmd
}
