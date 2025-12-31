package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/adapters/output"
)

func libraryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lib",
		Short: "Library commands",
	}

	cmd.AddCommand(libListCommand())
	cmd.AddCommand(libBrowseCommand())
	cmd.AddCommand(libSearchCommand())
	cmd.AddCommand(libResolveCommand())

	return cmd
}

func libListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List libraries",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.LibraryList(ctx)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
}

func libBrowseCommand() *cobra.Command {
	var start int64
	var count int64
	var container string

	cmd := &cobra.Command{
		Use:   "browse [library] [containerId]",
		Short: "Browse library",
		Long: "Browse a library by container. Omit containerId to browse the root.\n" +
			"Library selectors can be a configured alias, the library name, or a full node id (URN).\n" +
			"Container ids are library-specific; for Jellyfin, use empty to list the root folders.\n" +
			"Examples:\n" +
			"  mu lib browse               # root of default library\n" +
			"  mu lib browse jellyfin      # root of jellyfin library\n" +
			"  mu lib browse jellyfin abc  # container abc in jellyfin\n" +
			"  mu lib browse --container abc\n",
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			containerID := container
			switch len(args) {
			case 0:
				// Use defaults and optional --container.
			case 1:
				selector = args[0]
			case 2:
				if container != "" {
					return errors.New("use either [containerId] or --container, not both")
				}
				selector = args[0]
				containerID = args[1]
			}
			result, err := app.service.LibraryBrowse(ctx, selector, containerID, start, count)
			if err != nil {
				return err
			}
			if !app.json {
				library, err := app.service.Resolver.ResolveLibrary(ctx, selector)
				if err != nil {
					return err
				}
				if payload, ok := result.Data.(json.RawMessage); ok {
					return app.printer.Print(output.LibraryItemsOutput{LibraryID: library.NodeID, Payload: payload})
				}
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().Int64Var(&start, "start", 0, "start offset")
	cmd.Flags().Int64Var(&count, "count", 50, "page size")
	cmd.Flags().StringVar(&container, "container", "", "container id (defaults to root)")
	return cmd
}

func libSearchCommand() *cobra.Command {
	var start int64
	var count int64

	cmd := &cobra.Command{
		Use:   "search [library] <query>",
		Short: "Search library",
		Long: "Search a library for matching items.\n" +
			"Library selectors can be a configured alias, the library name, or a full node id (URN).",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			query := ""
			if len(args) == 1 {
				query = args[0]
			} else {
				selector = args[0]
				query = args[1]
			}
			result, err := app.service.LibrarySearch(ctx, selector, query, start, count)
			if err != nil {
				return err
			}
			if !app.json {
				library, err := app.service.Resolver.ResolveLibrary(ctx, selector)
				if err != nil {
					return err
				}
				if payload, ok := result.Data.(json.RawMessage); ok {
					return app.printer.Print(output.LibraryItemsOutput{LibraryID: library.NodeID, Payload: payload})
				}
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().Int64Var(&start, "start", 0, "start offset")
	cmd.Flags().Int64Var(&count, "count", 25, "page size")
	return cmd
}

func libResolveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resolve [library] <itemId>",
		Short: "Resolve library item",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			itemID := ""
			if len(args) == 1 {
				itemID = args[0]
			} else {
				selector = args[0]
				itemID = args[1]
			}
			result, err := app.service.LibraryResolve(ctx, selector, itemID)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
}
