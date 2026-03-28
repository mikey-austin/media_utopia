# Roadmap

## v1.0 — Minimum Viable (COMPLETE)

- [x] Playlist server (durable playlists + snapshots)
- [x] GStreamer native renderer
- [x] `mu` CLI reference client
- [x] Home Assistant MQTT Discovery mapping
- [x] UPnP renderer bridge
- [x] UPnP library bridge

## v1.1 — Quality & Breadth (COMPLETE)

- [x] Jellyfin library bridge
- [x] Kodi renderer bridge
- [x] VLC renderer bridge
- [x] Queue paging and metadata enrichment
- [x] Filesystem library with metadata parsing
- [x] Podcast/RSS library
- [x] go2rtc camera library

## v1.2 — Intelligence & Multi-Room (COMPLETE)

- [x] Suggestion support in playlist server
- [x] Snapcast zone controller integration
- [x] Home Assistant custom panel (Lit web component)
- [x] Filesystem library enrichment (MusicBrainz, Discogs, Wikipedia)
- [x] Semantic search with embeddings (Ollama)
- [x] AcoustID fingerprint fallback for metadata
- [x] LLM-generated album summaries
- [x] Metadata repair and deduplication

## v1.3 — Robustness (IN PROGRESS)

- [x] MQTT command deduplication (QoS redelivery fix)
- [x] Playlist server failover (auto-switch on timeout)
- [x] WebSocket state subscriptions (replace polling)
- [x] Client-side position interpolation
- [x] Options flow for HA integration reconfiguration
- [x] Relevance-based search ranking
- [x] Incremental embedding updates
- [ ] Command idempotency in all receivers (ring buffer dedup)
- [ ] Health/liveness protocol (presence re-announce, LWT)
- [ ] Event type specification and implementation

## v2.0 — Future

- [ ] Multi-room sync primitives (prepare/startAt wall-clock)
- [ ] Playlist sharing and permissions
- [ ] Advisor node type (AI-driven suggestions)
- [ ] HNSW approximate nearest neighbor for 100K+ libraries
- [ ] Prometheus metrics export
- [ ] Per-module health endpoints
- [ ] TLS mutual authentication for MQTT
