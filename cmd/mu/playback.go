package main

import (
	"context"

	"github.com/spf13/cobra"
)

func playCommand() *cobra.Command {
	var index int64

	cmd := &cobra.Command{
		Use:   "play [renderer]",
		Short: "Start playback",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			var idxPtr *int64
			if cmd.Flags().Changed("index") {
				idxPtr = &index
			}
			return app.service.PlaybackPlay(ctx, selector, idxPtr)
		},
	}

	cmd.Flags().Int64Var(&index, "index", 0, "queue index")

	return cmd
}

func pauseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pause [renderer]",
		Short: "Pause playback",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.service.PlaybackPause(ctx, selector)
		},
	}
}

func toggleCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "toggle [renderer]",
		Short: "Toggle playback",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.service.PlaybackToggle(ctx, selector)
		},
	}
}

func stopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [renderer]",
		Short: "Stop playback",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.service.PlaybackStop(ctx, selector)
		},
	}
}

func seekCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "seek [renderer] <+/-dur|ms>",
		Short: "Seek playback",
		Args:  cobra.RangeArgs(1, 2),
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
			return app.service.PlaybackSeek(ctx, selector, seekArg)
		},
	}
}

func nextCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "next [renderer]",
		Short: "Next track",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.service.PlaybackNext(ctx, selector)
		},
	}
}

func prevCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "prev [renderer]",
		Short: "Previous track",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.service.PlaybackPrev(ctx, selector)
		},
	}
}
