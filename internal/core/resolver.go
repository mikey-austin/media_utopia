package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mikey-austin/media_utopia/internal/ports"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Resolver resolves selectors to node presence.
type Resolver struct {
	Presence ports.Broker
	Config   Config
}

// ResolveRenderer resolves a renderer selector using config defaults.
func (r Resolver) ResolveRenderer(ctx context.Context, selector string) (mu.Presence, error) {
	return r.resolveByKind(ctx, selector, "renderer", r.Config.Defaults.Renderer)
}

// ResolveLibrary resolves a library selector using config defaults.
func (r Resolver) ResolveLibrary(ctx context.Context, selector string) (mu.Presence, error) {
	return r.resolveByKind(ctx, selector, "library", r.Config.Defaults.Library)
}

// ResolvePlaylistServer resolves a playlist server selector using config defaults.
func (r Resolver) ResolvePlaylistServer(ctx context.Context, selector string) (mu.Presence, error) {
	return r.resolveByKind(ctx, selector, "playlist", r.Config.Defaults.PlaylistServer)
}

func (r Resolver) resolveByKind(ctx context.Context, selector string, kind string, def string) (mu.Presence, error) {
	if selector == "" {
		selector = def
	}

	presence, err := r.Presence.ListPresence(ctx)
	if err != nil {
		return mu.Presence{}, WrapError(ExitRuntime, "list presence", err)
	}

	filtered := filterPresenceByKind(presence, kind)
	if selector == "" {
		if len(filtered) == 1 {
			return filtered[0], nil
		}
		return mu.Presence{}, &CLIError{Code: ExitUsage, Msg: "selector required"}
	}
	return resolveSelector(selector, filtered, r.Config.Aliases)
}

func filterPresenceByKind(presence []mu.Presence, kind string) []mu.Presence {
	if kind == "" {
		return presence
	}
	out := make([]mu.Presence, 0, len(presence))
	for _, p := range presence {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}

func resolveSelector(selector string, presence []mu.Presence, aliases map[string]string) (mu.Presence, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return mu.Presence{}, &CLIError{Code: ExitUsage, Msg: "selector required"}
	}

	if strings.HasPrefix(selector, "mu:") {
		return resolveExact(selector, presence)
	}

	if alias, ok := aliases[selector]; ok {
		if strings.HasPrefix(alias, "mu:") {
			return resolveExact(alias, presence)
		}
		selector = alias
	}

	matches := make([]mu.Presence, 0)
	for _, p := range presence {
		if strings.EqualFold(p.Name, selector) || strings.EqualFold(p.NodeID, selector) {
			matches = append(matches, p)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return mu.Presence{}, &CLIError{Code: ExitNotFound, Msg: fmt.Sprintf("no match for %q", selector)}
	}
	return mu.Presence{}, &CLIError{Code: ExitUsage, Msg: fmt.Sprintf("ambiguous selector %q: %s", selector, suggestionList(matches))}
}

func resolveExact(nodeID string, presence []mu.Presence) (mu.Presence, error) {
	for _, p := range presence {
		if p.NodeID == nodeID {
			return p, nil
		}
	}
	return mu.Presence{}, &CLIError{Code: ExitNotFound, Msg: fmt.Sprintf("node not found: %s", nodeID)}
}

func suggestionList(matches []mu.Presence) string {
	names := make([]string, 0, len(matches))
	for _, p := range matches {
		names = append(names, fmt.Sprintf("%s (%s)", p.Name, p.NodeID))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
