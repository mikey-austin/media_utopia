package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func volumeCommand() *cobra.Command {
	var mute bool
	var unmute bool

	cmd := &cobra.Command{
		Use:   "vol [renderer] [<0..100>|<+/-n>]",
		Short: "Set volume",
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			if mute && unmute {
				return fmt.Errorf("use only --mute or --unmute")
			}
			var mutePtr *bool
			if mute || unmute {
				val := mute
				mutePtr = &val
			}

			selector := ""
			arg := ""
			switch len(args) {
			case 1:
				if looksLikeVolume(args[0]) && mutePtr == nil {
					arg = args[0]
				} else {
					selector = args[0]
				}
			case 2:
				selector = args[0]
				arg = args[1]
			}

			if arg == "" && mutePtr == nil {
				return fmt.Errorf("volume value required")
			}
			return app.runWithLeaseRetry(ctx, selector, func() error {
				return app.service.SetVolume(ctx, selector, arg, mutePtr)
			})
		},
	}

	cmd.Flags().BoolVar(&mute, "mute", false, "mute output")
	cmd.Flags().BoolVar(&unmute, "unmute", false, "unmute output")
	cmd.Flags().ParseErrorsWhitelist.UnknownFlags = true

	return cmd
}

func looksLikeVolume(arg string) bool {
	if arg == "" {
		return false
	}
	if strings.HasPrefix(arg, "+") || strings.HasPrefix(arg, "-") {
		return true
	}
	return arg[0] >= '0' && arg[0] <= '9'
}
