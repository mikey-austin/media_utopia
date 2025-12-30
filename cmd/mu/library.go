package main

import (
	"context"

	"github.com/spf13/cobra"
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

	cmd := &cobra.Command{
		Use:   "browse [library] <containerId>",
		Short: "Browse library",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			containerID := ""
			if len(args) == 1 {
				containerID = args[0]
			} else {
				selector = args[0]
				containerID = args[1]
			}
			result, err := app.service.LibraryBrowse(ctx, selector, containerID, start, count)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().Int64Var(&start, "start", 0, "start offset")
	cmd.Flags().Int64Var(&count, "count", 50, "page size")
	return cmd
}

func libSearchCommand() *cobra.Command {
	var start int64
	var count int64

	cmd := &cobra.Command{
		Use:   "search [library] <query>",
		Short: "Search library",
		Args:  cobra.RangeArgs(1, 2),
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
