package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/core"
)

func acquireCommand() *cobra.Command {
	var ttl time.Duration
	var wait bool

	cmd := &cobra.Command{
		Use:   "acquire [renderer]",
		Short: "Acquire an exclusive renderer lease",
		Long: `Acquire an exclusive lease on a renderer. Only one controller can hold
a lease at a time. Mutating commands (play, stop, queue changes) require a lease,
which is auto-acquired when needed. Use this to explicitly lock a renderer.`,
		Example: `  mu acquire
  mu acquire living-room
  mu acquire living-room --ttl 10m`,
		GroupID:           "session",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = wait
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			result, err := app.service.AcquireLease(ctx, selector, ttl)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().DurationVar(&ttl, "ttl", 5*time.Minute, "lease ttl")
	cmd.Flags().BoolVar(&wait, "wait", false, "wait for lease release")

	return cmd
}

func renewCommand() *cobra.Command {
	var ttl time.Duration

	cmd := &cobra.Command{
		Use:     "renew [renderer]",
		Short:   "Renew an existing renderer lease",
		Long:    "Extend the TTL of an existing lease without releasing it.",
		Example: `  mu renew
  mu renew living-room --ttl 15m`,
		GroupID:           "session",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			if err := app.service.RenewLease(ctx, selector, ttl); err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Println("Lease renewed")
			}
			return nil
		},
	}

	cmd.Flags().DurationVar(&ttl, "ttl", 5*time.Minute, "lease ttl")
	return cmd
}

func releaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "release [renderer]",
		Short:   "Release a renderer lease",
		Long:    "Release a lease so other controllers can acquire it.",
		Example: `  mu release
  mu release living-room`,
		GroupID:           "session",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			if err := app.service.ReleaseLease(ctx, selector); err != nil {
				return err
			}
			if !app.quiet && !app.json {
				fmt.Println("Lease released")
			}
			return nil
		},
	}
	return cmd
}

func ownerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "owner [renderer]",
		Short:   "Show the current lease owner",
		Long:    "Show which controller currently holds the lease on a renderer.",
		Example: `  mu owner
  mu owner living-room`,
		GroupID:           "session",
		ValidArgsFunction: completeRenderers,
		Args:              cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := selectorArg(args)
			owner, err := app.service.Owner(ctx, selector)
			if err != nil {
				return err
			}
			return app.printer.Print(core.RawResult{Data: owner})
		},
	}
	return cmd
}
