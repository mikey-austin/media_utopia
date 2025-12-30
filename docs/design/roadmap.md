# Roadmap (suggested)

This is a pragmatic implementation order that quickly yields a working system
with HA control and real audio endpoints.

## v1.0 (minimum viable, end-to-end)
1) Playlist server (required)
2) UPnP renderer bridge (unlocks existing renderers)
3) `mu` CLI reference client
4) Home Assistant MQTT Discovery mapping
5) UPnP library bridge (reuse Synology UPnP server without new media server)

## v1.1 (quality & breadth)
- Jellyfin library bridge
- Kodi renderer bridge
- richer queue listing (paging, metadata enrichment)
- improved UPnP end-of-track detection heuristics

## v1.2 (suggestions)
- advisor node type
- suggestion objects in playlist server
- HA entities for “suggestion list” + “apply/promote” actions

## v2 (optional futures)
- multiroom sync primitives (prepare/startAt wall-clock)
- richer permissions / sharing for playlists
- custom HA media browser integration
