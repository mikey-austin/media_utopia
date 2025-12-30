package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/core"
)

func acquireCommand() *cobra.Command {
	var ttl time.Duration
	var wait bool

	cmd := &cobra.Command{
		Use:   "acquire [renderer]",
		Short: "Acquire a renderer lease",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = wait
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			result, err := app.service.AcquireLease(ctx, selector, ttl)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().DurationVar(&ttl, "ttl", 15*time.Second, "lease ttl")
	cmd.Flags().BoolVar(&wait, "wait", false, "wait for lease release")

	return cmd
}

func renewCommand() *cobra.Command {
	var ttl time.Duration

	cmd := &cobra.Command{
		Use:   "renew [renderer]",
		Short: "Renew a renderer lease",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.service.RenewLease(ctx, selector, ttl)
		},
	}

	cmd.Flags().DurationVar(&ttl, "ttl", 15*time.Second, "lease ttl")
	return cmd
}

func releaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release [renderer]",
		Short: "Release a renderer lease",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			return app.service.ReleaseLease(ctx, selector)
		},
	}
	return cmd
}

func ownerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "owner [renderer]",
		Short: "Show current renderer owner",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			owner, err := app.service.Owner(ctx, selector)
			if err != nil {
				return err
			}
			return app.printer.Print(core.RawResult{Data: owner})
		},
	}
	return cmd
}
