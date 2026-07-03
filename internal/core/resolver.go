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

// ResolveZone resolves a zone selector using config defaults.
func (r Resolver) ResolveZone(ctx context.Context, selector string) (mu.Presence, error) {
	return r.resolveByKind(ctx, selector, "zone", r.Config.Defaults.Zone)
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
		if len(filtered) == 0 {
			return mu.Presence{}, &CLIError{Code: ExitNotFound, Msg: fmt.Sprintf("no %s nodes found — is the broker reachable?", kind)}
		}
		return mu.Presence{}, &CLIError{Code: ExitUsage,
			Msg: fmt.Sprintf("several %ss available, pick one: %s (or set defaults.%s in config)",
				kind, suggestionList(filtered), kind)}
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

// ResolveSelectorIn resolves a selector against an explicit presence set
// (any kind) using the same forgiving matching as the kind-scoped resolvers.
func ResolveSelectorIn(selector string, presence []mu.Presence, aliases map[string]string) (mu.Presence, error) {
	return resolveSelector(selector, presence, aliases)
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

	// Progressively forgiving matching: exact name/ID, then unique
	// case-insensitive prefix, then unique substring. Each tier only
	// applies when the previous found nothing, so exact names always win.
	exact := make([]mu.Presence, 0)
	prefix := make([]mu.Presence, 0)
	substr := make([]mu.Presence, 0)
	needle := strings.ToLower(selector)
	for _, p := range presence {
		name := strings.ToLower(p.Name)
		switch {
		case strings.EqualFold(p.Name, selector) || strings.EqualFold(p.NodeID, selector):
			exact = append(exact, p)
		case strings.HasPrefix(name, needle):
			prefix = append(prefix, p)
		case strings.Contains(name, needle):
			substr = append(substr, p)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = prefix
	}
	if len(matches) == 0 {
		matches = substr
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		msg := fmt.Sprintf("no match for %q", selector)
		if len(presence) > 0 {
			msg += fmt.Sprintf(" — available: %s", suggestionList(presence))
		}
		return mu.Presence{}, &CLIError{Code: ExitNotFound, Msg: msg}
	}
	return mu.Presence{}, &CLIError{Code: ExitUsage, Msg: fmt.Sprintf("%q is ambiguous: %s", selector, suggestionList(matches))}
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
