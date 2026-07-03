package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func playCommand() *cobra.Command {
	var index int64
	var library string

	cmd := &cobra.Command{
		Use:   "play [renderer] [item...]",
		Short: "Start or resume playback, or play items directly",
		Long: `Start or resume playback on a renderer. If the queue has entries,
playback begins at the current position unless --index is specified.

With item arguments — library item IDs from 'mu lib search'/'mu lib
browse', or direct URLs — the items are queued right after the current
track and playback jumps to the first of them. Bare item IDs use
--library, the configured default library, or the only discovered one.`,
		Example: `  mu play
  mu play living-room
  mu play living-room --index 5
  mu play 4c7ae19088ed6d95e9400ee3953cd2a5      # play this track now
  mu play living-room 4c7ae19088ed6d95e9400ee3953cd2a5
  mu play https://example.com/stream.mp3`,
		GroupID:           "playback",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			// The first argument is a renderer selector only when it
			// resolves to a renderer; everything else is items to play.
			selector := ""
			items := args
			if len(args) > 0 && !looksLikeItem(args[0]) {
				if _, err := app.service.Resolver.ResolveRenderer(ctx, args[0]); err == nil {
					selector = args[0]
					items = args[1:]
				}
			}
			if len(items) > 0 {
				if err := app.runWithLeaseRetry(ctx, selector, func() error {
					return app.service.PlayItems(ctx, selector, items, library)
				}); err != nil {
					return err
				}
				app.printPlaybackOutcome(ctx, selector, "Playback started")
				return nil
			}

			var idxPtr *int64
			if cmd.Flags().Changed("index") {
				idxPtr = &index
			}
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackPlay(ctx, selector, idxPtr)
			}); err != nil {
				return err
			}
			app.printPlaybackOutcome(ctx, selector, "Playback started")
			return nil
		},
	}

	cmd.Flags().Int64Var(&index, "index", 0, "queue index to start playback from")
	cmd.Flags().StringVarP(&library, "library", "l", "", "library the bare item IDs belong to (default: configured library)")

	return cmd
}

func pauseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pause [renderer]",
		Short: "Pause playback",
		Long:  "Pause playback on a renderer. Use 'mu toggle' to resume.",
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
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackPause(ctx, selector)
			}); err != nil {
				return err
			}
			app.printPlaybackOutcome(ctx, selector, "Playback paused")
			return nil
		},
	}
}

func toggleCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "toggle [renderer]",
		Short: "Toggle between play and pause",
		Long:  "Toggle between play and pause states. If playing, pauses. If paused, resumes.",
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
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackToggle(ctx, selector)
			}); err != nil {
				return err
			}
			app.printPlaybackOutcome(ctx, selector, "Playback toggled")
			return nil
		},
	}
}

func stopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [renderer]",
		Short: "Stop playback and reset position",
		Long:  "Stop playback and reset the position to the beginning of the current track.",
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
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackStop(ctx, selector)
			}); err != nil {
				return err
			}
			app.printPlaybackOutcome(ctx, selector, "Playback stopped")
			return nil
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
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackSeek(ctx, selector, seekArg)
			}); err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Println("Position updated")
			}
			return nil
		},
	}
	cmd.Flags().ParseErrorsWhitelist.UnknownFlags = true
	return cmd
}

func nextCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "next [renderer]",
		Short: "Skip to the next track",
		Long:  "Skip to the next track in the queue.",
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
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackNext(ctx, selector)
			}); err != nil {
				return err
			}
			app.printPlaybackOutcome(ctx, selector, "Skipped to next track")
			return nil
		},
	}
}

func prevCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "prev [renderer]",
		Short: "Go back to the previous track",
		Long:  "Go back to the previous track in the queue.",
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
			if err := app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.PlaybackPrev(ctx, selector)
			}); err != nil {
				return err
			}
			app.printPlaybackOutcome(ctx, selector, "Went to previous track")
			return nil
		},
	}
}
