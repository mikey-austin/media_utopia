package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mikey-austin/media_utopia/internal/adapters/output"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func libraryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lib",
		Short:   "Browse and search media libraries",
		GroupID: "library",
	}

	cmd.AddCommand(libListCommand())
	cmd.AddCommand(libBrowseCommand())
	cmd.AddCommand(libSearchCommand())
	cmd.AddCommand(libResolveCommand())
	cmd.AddCommand(libRescanCommand())
	cmd.AddCommand(libImportCommand())
	cmd.AddCommand(libImportsCommand())

	return cmd
}

func libListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List available libraries",
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
	var offset int64
	var count int64
	var container string

	cmd := &cobra.Command{
		Use:   "browse [library] [containerId]",
		Short: "Browse a library by container",
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
			result, err := app.service.LibraryBrowse(ctx, selector, containerID, offset, count)
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

	cmd.Flags().Int64Var(&offset, "offset", 0, "start offset")
	cmd.Flags().Int64Var(&count, "count", 50, "page size")
	cmd.Flags().StringVar(&container, "container", "", "container id (defaults to root)")
	return cmd
}

func libSearchCommand() *cobra.Command {
	var offset int64
	var count int64
	var types string

	cmd := &cobra.Command{
		Use:     "search [library] <query...>",
		Aliases: []string{"find", "query"},
		Short:   "Search a library for matching items",
		Long: "Search a library for matching items.\n" +
			"Library selectors can be a configured alias, the library name (or a\n" +
			"unique prefix of it), or a full node id (URN).\n\n" +
			"Multi-word queries need no quotes: if the first argument names a\n" +
			"library it is used as the selector, otherwise every argument joins\n" +
			"into the query.\n\n" +
			"Examples:\n" +
			"  mu lib search warning sign          # default library\n" +
			"  mu lib search venus warning sign    # library 'venus', query 'warning sign'",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector, query := splitSearchArgs(args, func(sel string) bool {
				p, err := app.service.Resolver.ResolveLibrary(ctx, sel)
				return err == nil && greedySelectorMatch(p, sel, app.service.Config.Aliases)
			})
			typeList, err := parseLibraryTypes(types)
			if err != nil {
				return err
			}
			result, err := app.service.LibrarySearch(ctx, selector, query, offset, count, typeList)
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

	cmd.Flags().Int64Var(&offset, "offset", 0, "start offset")
	cmd.Flags().Int64Var(&count, "count", 25, "page size")
	cmd.Flags().StringVar(&types, "type", "", "comma-separated types (Audio,MusicAlbum,MusicArtist,Movie,Series,Episode,Video,Playlist,Folder)")
	return cmd
}

func libResolveCommand() *cobra.Command {
	var includeSources bool

	cmd := &cobra.Command{
		Use:   "resolve [library] <itemId>",
		Short: "Show library item metadata; with --sources also fetch playable URLs",
		Long: `Show catalog metadata for a library item.

By default only metadata is fetched (library.getItem). Pass --sources to
additionally resolve playable URLs (library.resolveSources).

Pass either "<itemId>" (uses the default/configured library) or
"<library> <itemId>".`,
		Args: cobra.RangeArgs(1, 2),
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
			result, err := app.service.LibraryResolve(ctx, selector, strings.TrimSpace(itemID), includeSources)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
	cmd.Flags().BoolVar(&includeSources, "sources", false, "also resolve playable source URLs")
	return cmd
}

func libRescanCommand() *cobra.Command {
	var async bool
	var force bool

	cmd := &cobra.Command{
		Use:   "rescan [library]",
		Short: "Trigger a library rescan",
		Long: `Trigger a library rescan to index new, modified, or deleted files.

By default runs synchronously and reports the number of items found.
Use --async to start the scan in the background and return immediately.
Use --force to re-enrich all albums, including negative-cache sidecars
that would normally be skipped until they expire (30 days).

This command is only supported by libraries that implement the rescan
capability (e.g., fs_library). Other libraries may return an error.

Examples:
  mu lib rescan                    # rescan default library
  mu lib rescan filesystem         # rescan filesystem library
  mu lib rescan --async            # start rescan in background
  mu lib rescan --force            # force re-enrichment of all albums
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector := ""
			if len(args) > 0 {
				selector = args[0]
			}
			result, err := app.service.LibraryRescan(ctx, selector, async, force)
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}

	cmd.Flags().BoolVar(&async, "async", false, "run rescan in background")
	cmd.Flags().BoolVar(&force, "force", false, "force re-enrichment of all albums")
	return cmd
}

// greedySelectorMatch decides whether an implicit first argument really
// names the resolved node. Deliberately stricter than full resolution:
// exact name/ID/alias or a name prefix only — substring matches would
// steal query words ("me" resolving to the "Cameras" library turned
// 'search me at the zoo' into a camera search).
func greedySelectorMatch(p mu.Presence, sel string, aliases map[string]string) bool {
	if strings.EqualFold(p.Name, sel) || strings.EqualFold(p.NodeID, sel) {
		return true
	}
	if strings.HasPrefix(strings.ToLower(p.Name), strings.ToLower(sel)) {
		return true
	}
	if target, ok := aliases[sel]; ok && strings.EqualFold(target, p.NodeID) {
		return true
	}
	return false
}

// splitSearchArgs decides which arguments are the library selector and
// which are the query. A single argument is always the query; with more,
// the first is the selector only when it actually resolves to a library —
// otherwise everything joins into an unquoted multi-word query.
func splitSearchArgs(args []string, resolves func(string) bool) (selector string, query string) {
	if len(args) == 1 {
		return "", args[0]
	}
	if resolves(args[0]) {
		return args[0], strings.Join(args[1:], " ")
	}
	return "", strings.Join(args, " ")
}

func libImportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "import [library] <url>",
		Short: "Import a YouTube playlist into the library (async)",
		Long: `Download a YouTube playlist into the library as FLAC with artwork and
metadata. Runs asynchronously on the library host: the command returns a
job id immediately; watch progress with 'mu lib imports'.

Re-importing the same URL is safe — already-downloaded tracks are
skipped and only new playlist entries are fetched.`,
		Example: `  mu lib import https://www.youtube.com/playlist?list=PL123
  mu lib imports`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			selector, url := "", args[0]
			if len(args) == 2 {
				selector, url = args[0], args[1]
			}
			result, err := app.service.LibraryImport(ctx, selector, url)
			if err != nil {
				return err
			}
			if app.json {
				return app.printer.Print(result)
			}
			fmt.Printf("import %s: %s\n", result.Status, result.JobID)
			fmt.Println(output.Dim("watch progress with 'mu lib imports'"))
			return nil
		},
	}
}

func libImportsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "imports [library]",
		Short: "List import jobs and their progress",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := fromContext(cmd)
			ctx, cancel := withTimeout(context.Background(), app.timeout)
			defer cancel()

			result, err := app.service.LibraryImports(ctx, selectorArg(args))
			if err != nil {
				return err
			}
			return app.printer.Print(result)
		},
	}
}

func parseLibraryTypes(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	allowed := map[string]string{
		"audio":       "Audio",
		"musicalbum":  "MusicAlbum",
		"album":       "MusicAlbum",
		"musicartist": "MusicArtist",
		"artist":      "MusicArtist",
		"movie":       "Movie",
		"series":      "Series",
		"episode":     "Episode",
		"video":       "Video",
		"playlist":    "Playlist",
		"folder":      "Folder",
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		key := strings.ToLower(strings.TrimSpace(part))
		if key == "" {
			continue
		}
		canonical, ok := allowed[key]
		if !ok {
			return nil, errors.New("unknown type " + part + " (allowed: Audio,MusicAlbum,MusicArtist,Movie,Series,Episode,Video,Playlist,Folder)")
		}
		if !seen[canonical] {
			out = append(out, canonical)
			seen[canonical] = true
		}
	}
	return out, nil
}
