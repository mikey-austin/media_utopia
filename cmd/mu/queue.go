package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func queueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Queue commands",
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
	var from int64
	var count int64

	cmd := &cobra.Command{
		Use:   "list [renderer]",
		Short: "List queue entries",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			result, err := app.service.QueueList(ctx, selector, from, count)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().Int64Var(&from, "from", 0, "start index")
	cmd.Flags().Int64Var(&count, "count", 50, "number of entries")
	return cmd
}

func queueNowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "now [renderer]",
		Short: "Show current queue item",
		Args:  cobra.RangeArgs(0, 1),
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
		Use:   "clear [renderer]",
		Short: "Clear the queue",
		Args:  cobra.RangeArgs(0, 1),
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
		Use:   "jump [renderer] <index>",
		Short: "Jump to index",
		Args:  cobra.RangeArgs(1, 2),
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
				return fmt.Errorf("invalid index")
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
		Use:   "rm [renderer] <index|queueEntryId>",
		Short: "Remove entry",
		Args:  cobra.RangeArgs(1, 2),
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
		Use:   "mv [renderer] <from> <to>",
		Short: "Move entry",
		Args:  cobra.RangeArgs(2, 3),
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
				return fmt.Errorf("invalid from index")
			}
			to, err := strconv.ParseInt(toArg, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid to index")
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
		Use:   "shuffle [renderer]",
		Short: "Shuffle queue",
		Args:  cobra.RangeArgs(0, 1),
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
		Use:   "repeat [renderer] on|off",
		Short: "Toggle repeat",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := ""
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			} else {
				selector = args[0]
				arg = args[1]
			}
			repeat := false
			switch arg {
			case "on":
				repeat = true
			case "off":
				repeat = false
			default:
				return fmt.Errorf("repeat must be on|off")
			}
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.QueueRepeat(ctx, selector, repeat)
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
		Short: "Add items to queue",
		Args:  cobra.MinimumNArgs(1),
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
		Short: "Replace queue",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			if file == "" {
				return fmt.Errorf("--file is required")
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
