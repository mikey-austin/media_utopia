package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func statusCommand() *cobra.Command {
	var watch bool

	cmd := &cobra.Command{
		Use:   "status [renderer]",
		Short: "Show renderer status",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			if watch {
				return watchStatus(cmd, app, selector)
			}
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()
			result, err := app.service.Status(ctx, selector)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "watch status updates")

	return cmd
}

func watchStatus(cmd *cobra.Command, app *app, selector string) error {
	ctx := context.Background()
	initial, err := app.service.Status(ctx, selector)
	if err != nil {
		return err
	}
	if err := app.printer.Print(initial); err != nil {
		return err
	}

	states, events, errs, err := app.service.WatchStatus(ctx, selector)
	if err != nil {
		return err
	}

	for {
		select {
		case state, ok := <-states:
			if !ok {
				return nil
			}
			result := coreStatusFromState(initial.Renderer, state)
			if err := app.printer.Print(result); err != nil {
				return err
			}
		case <-events:
			// ignored for now
		case err := <-errs:
			if err != nil {
				return err
			}
		}
	}
}

func coreStatusFromState(renderer mu.Presence, state mu.RendererState) core.StatusResult {
	return core.StatusResult{Renderer: renderer, State: state}
}
