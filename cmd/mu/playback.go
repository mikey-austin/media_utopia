package main

import (
	"context"

	"github.com/spf13/cobra"
)

func playCommand() *cobra.Command {
	var index int64

	cmd := &cobra.Command{
		Use:   "play [renderer]",
		Short: "Start or resume playback",
		Long: `Start or resume playback on a renderer. If the queue has entries,
playback begins at the current position unless --index is specified.`,
		Example: `  mu play
  mu play living-room
  mu play living-room --index 5`,
		GroupID:           "playback",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			var idxPtr *int64
			if cmd.Flags().Changed("index") {
				idxPtr = &index
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackPlay(ctx, selector, idxPtr)
			})
		},
	}

	cmd.Flags().Int64Var(&index, "index", 0, "queue index")

	return cmd
}

func pauseCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "pause [renderer]",
		Short:   "Pause playback",
		Long:    "Pause playback on a renderer. Use 'mu toggle' to resume.",
		Example: `  mu pause
  mu pause living-room`,
		GroupID:           "playback",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackPause(ctx, selector)
			})
		},
	}
}

func toggleCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "toggle [renderer]",
		Short:   "Toggle between play and pause",
		Long:    "Toggle between play and pause states. If playing, pauses. If paused, resumes.",
		Example: `  mu toggle
  mu toggle living-room`,
		GroupID:           "playback",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackToggle(ctx, selector)
			})
		},
	}
}

func stopCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "stop [renderer]",
		Short:   "Stop playback and reset position",
		Long:    "Stop playback and reset the position to the beginning of the current track.",
		Example: `  mu stop
  mu stop living-room`,
		GroupID:           "playback",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackStop(ctx, selector)
			})
		},
	}
}

func seekCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seek [renderer] <+/-dur|ms>",
		Short: "Seek to a position or offset",
		Long: `Seek to an absolute position or a relative offset.

Accepts milliseconds, or a relative offset with +/- prefix.
Duration suffixes like "s" and "m" are also supported.`,
		Example: `  mu seek +30s
  mu seek -10s
  mu seek 120000
  mu seek living-room +1m`,
		GroupID: "playback",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			seekArg := ""
			if len(args) == 1 {
				seekArg = args[0]
			} else {
				selector = args[0]
				seekArg = args[1]
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackSeek(ctx, selector, seekArg)
			})
		},
	}
	cmd.Flags().ParseErrorsWhitelist.UnknownFlags = true
	return cmd
}

func nextCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "next [renderer]",
		Short:   "Skip to the next track",
		Long:    "Skip to the next track in the queue.",
		Example: `  mu next
  mu next living-room`,
		GroupID:           "playback",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackNext(ctx, selector)
			})
		},
	}
}

func prevCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "prev [renderer]",
		Short:   "Go back to the previous track",
		Long:    "Go back to the previous track in the queue.",
		Example: `  mu prev
  mu prev living-room`,
		GroupID:           "playback",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackPrev(ctx, selector)
			})
		},
	}
}
