# Media Utopia Documentation

## Getting Started

- [Getting Started Guide](getting-started.md) — Install, configure, and play your first track

## Specifications (normative)

- [Core Concepts](spec/overview.md) — Nodes, IDs, leases, capabilities, errors, events
- [MQTT Messages](spec/messages.md) — Wire protocol, envelopes, command bodies, QoS rules
- [CLI Reference](spec/cli.md) — `mu` command-line interface
- [Library Search Grammar](spec/library_filesystem.md) — Semantic search query syntax

## Design Documents

- [Motivation](design/motivation.md) — Why Media Utopia exists
- [Architecture](design/architecture.md) — System shape, control/data planes, state model
- [Design Decisions](design/decisions.md) — Locked v1 decisions
- [mud Architecture](design/mud-architecture.md) — Daemon, modules, configuration
- [HA Integration](design/ha-integration.md) — Home Assistant bridge, panel, WebSocket API
- [Integration Strategy](design/integrations.md) — Bridge-first approach
- [Filesystem Library](design/library_filesystem.md) — Indexing, browsing, metadata
- [Library Providers](design/library_providers.md) — Enrichment, repair, embeddings
- [AcoustID Fingerprinting](design/acoustid_fingerprinting.md) — Audio fingerprint fallback
- [Semantic Search](design/semantic_search_improvements.md) — Embeddings, summaries, dual search
- [Roadmap](design/roadmap.md) — Version milestones and progress
