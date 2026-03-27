package main

import (
	"context"

	"github.com/spf13/cobra"
)

func lsCommand() *cobra.Command {
	var kind string
	var online bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List discovered nodes on the network",
		Long: `List all discovered nodes on the network. Nodes include renderers,
libraries, and playlist servers. Use --kind to filter by type and --online
to show only currently connected nodes.`,
		Example: `  mu ls
  mu ls --kind renderer
  mu ls --kind library --online`,
		GroupID: "discovery",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.ListNodes(ctx, kind, online)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind")
	cmd.Flags().BoolVar(&online, "online", false, "show only online nodes")

	return cmd
}
