"""WebSocket API for the MU panel."""

from __future__ import annotations

import logging
from typing import Any

import voluptuous as vol

from homeassistant.components import websocket_api
from homeassistant.core import HomeAssistant, callback

from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)


def async_register_websocket_api(hass: HomeAssistant) -> None:
    """Register WebSocket commands for the MU panel."""
    websocket_api.async_register_command(hass, ws_renderers)
    websocket_api.async_register_command(hass, ws_renderer_state)
    websocket_api.async_register_command(hass, ws_lease_status)
    websocket_api.async_register_command(hass, ws_lease_acquire)
    websocket_api.async_register_command(hass, ws_lease_release)
    websocket_api.async_register_command(hass, ws_transport)
    websocket_api.async_register_command(hass, ws_seek)
    websocket_api.async_register_command(hass, ws_volume)
    websocket_api.async_register_command(hass, ws_shuffle)
    websocket_api.async_register_command(hass, ws_repeat_mode)
    websocket_api.async_register_command(hass, ws_queue_get)
    websocket_api.async_register_command(hass, ws_queue_add)
    websocket_api.async_register_command(hass, ws_queue_remove)
    websocket_api.async_register_command(hass, ws_queue_move)
    websocket_api.async_register_command(hass, ws_queue_clear)
    websocket_api.async_register_command(hass, ws_queue_jump)
    websocket_api.async_register_command(hass, ws_playlists_list)
    websocket_api.async_register_command(hass, ws_playlist_get)
    websocket_api.async_register_command(hass, ws_playlist_create)
    websocket_api.async_register_command(hass, ws_playlist_rename)
    websocket_api.async_register_command(hass, ws_playlist_add)
    websocket_api.async_register_command(hass, ws_playlist_remove)
    websocket_api.async_register_command(hass, ws_playlist_move)
    websocket_api.async_register_command(hass, ws_playlist_delete)
    websocket_api.async_register_command(hass, ws_queue_load_playlist)
    websocket_api.async_register_command(hass, ws_snapshots_list)
    websocket_api.async_register_command(hass, ws_snapshot_save)
    websocket_api.async_register_command(hass, ws_queue_load_snapshot)
    websocket_api.async_register_command(hass, ws_snapshot_delete)
    websocket_api.async_register_command(hass, ws_playlist_from_snapshot)
    websocket_api.async_register_command(hass, ws_libraries_list)
    websocket_api.async_register_command(hass, ws_library_browse)
    websocket_api.async_register_command(hass, ws_zone_controllers_list)
    websocket_api.async_register_command(hass, ws_zones_list)
    websocket_api.async_register_command(hass, ws_zone_set_volume)
    websocket_api.async_register_command(hass, ws_zone_set_mute)
    websocket_api.async_register_command(hass, ws_zone_select_source)
    websocket_api.async_register_command(hass, ws_playlist_servers_list)
    websocket_api.async_register_command(hass, ws_playlist_server_select)
    websocket_api.async_register_command(hass, ws_playlist_save_from_queue)


def _get_bridge(hass: HomeAssistant):
    """Get the MudBridge instance."""
    domain_data = hass.data.get(DOMAIN, {})
    for entry_data in domain_data.values():
        if isinstance(entry_data, dict) and "bridge" in entry_data:
            return entry_data["bridge"]
    return None


# --- Renderer Discovery & State ---


@websocket_api.websocket_command({vol.Required("type"): "mu/renderers"})
@websocket_api.async_response
async def ws_renderers(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """List all discovered MU renderers."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderers = []
    for node_id, name in bridge.list_renderers():
        # Get full renderer info for capabilities
        info = bridge.get_renderer_info(node_id) or {}
        caps = info.get("caps") or {}
        renderers.append({
            "rendererId": node_id,
            "name": name,
            "online": True,
            "caps": {
                "seek": caps.get("seek", True),
                "volume": caps.get("volume", True),
                "queue": caps.get("queue", True),
                "queueResolve": caps.get("queueResolve", False),
            },
        })
    connection.send_result(msg["id"], renderers)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/renderer_state",
    vol.Required("renderer_id"): str,
})
@websocket_api.async_response
async def ws_renderer_state(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Get state for a specific renderer."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    state = bridge.get_renderer_state(renderer_id)
    if not state:
        connection.send_error(msg["id"], "not_found", f"Renderer {renderer_id} not found")
        return

    session = state.get("session") or {}
    playback = state.get("playback") or {}
    queue = state.get("queue") or {}
    current = state.get("current") or {}
    metadata = current.get("metadata") or {}

    result = {
        "session": {
            "owned": session.get("owner") == bridge.identity,
            "owner": session.get("owner", ""),
            "expires_in_s": max(0, (session.get("leaseExpiresAt") or 0) - int(__import__("time").time())),
        },
        "playback": {
            "status": playback.get("status", "idle"),
            "position_ms": playback.get("positionMs", 0),
            "duration_ms": playback.get("durationMs", 0),
            "volume": playback.get("volume", 1.0),
            "mute": playback.get("mute", False),
        },
        "current": {
            "title": metadata.get("title", ""),
            "artist": metadata.get("artist", ""),
            "album": metadata.get("album", ""),
            "art_url": bridge.rewrite_artwork_url(metadata.get("artworkUrl")),
        },
        "queue": {
            "revision": queue.get("revision", 0),
            "length": queue.get("length", 0),
            "index": queue.get("index", 0),
            "shuffle": queue.get("shuffle", False),
            "repeat": queue.get("repeat", False),
            "repeatMode": queue.get("repeatMode", "off"),
        },
    }
    connection.send_result(msg["id"], result)


# --- Lease Management ---


@websocket_api.websocket_command({
    vol.Required("type"): "mu/lease_status",
    vol.Required("renderer_id"): str,
})
@websocket_api.async_response
async def ws_lease_status(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Get lease status for a renderer."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    state = bridge.get_renderer_state(renderer_id)
    session = (state or {}).get("session") or {}
    import time
    result = {
        "owned": session.get("owner") == bridge.identity,
        "owner": session.get("owner", ""),
        "expires_in_s": max(0, (session.get("leaseExpiresAt") or 0) - int(time.time())),
    }
    connection.send_result(msg["id"], result)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/lease_acquire",
    vol.Required("renderer_id"): str,
    vol.Optional("ttl_ms", default=15000): int,
})
@websocket_api.async_response
async def ws_lease_acquire(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Acquire lease for a renderer."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    success = await bridge.async_acquire_lease(renderer_id)
    if not success:
        connection.send_error(msg["id"], "lease_failed", "Failed to acquire lease")
        return
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/lease_release",
    vol.Required("renderer_id"): str,
})
@websocket_api.async_response
async def ws_lease_release(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Release lease for a renderer."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    success = await bridge.async_release_lease(renderer_id)
    connection.send_result(msg["id"], {"success": success})


# --- Transport Controls ---


@websocket_api.websocket_command({
    vol.Required("type"): "mu/transport",
    vol.Required("renderer_id"): str,
    vol.Required("action"): vol.In(["play", "pause", "toggle", "stop", "next", "prev"]),
})
@websocket_api.async_response
async def ws_transport(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Execute transport command."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    action = msg["action"]

    if action == "play":
        await bridge.async_play(renderer_id)
    elif action == "pause":
        await bridge.async_pause(renderer_id)
    elif action == "toggle":
        state = bridge.get_renderer_state(renderer_id)
        status = ((state or {}).get("playback") or {}).get("status")
        if status == "playing":
            await bridge.async_pause(renderer_id)
        else:
            await bridge.async_play(renderer_id)
    elif action == "stop":
        await bridge.async_stop(renderer_id)
    elif action == "next":
        await bridge.async_next(renderer_id)
    elif action == "prev":
        await bridge.async_previous(renderer_id)

    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/seek",
    vol.Required("renderer_id"): str,
    vol.Required("position_ms"): int,
})
@websocket_api.async_response
async def ws_seek(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Seek to position."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_seek(msg["renderer_id"], msg["position_ms"] / 1000)
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/volume",
    vol.Required("renderer_id"): str,
    vol.Optional("volume"): vol.Coerce(float),
    vol.Optional("mute"): bool,
})
@websocket_api.async_response
async def ws_volume(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Set volume or mute state."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    if "volume" in msg:
        await bridge.async_set_volume(renderer_id, msg["volume"])
    if "mute" in msg:
        await bridge.async_mute(renderer_id, msg["mute"])
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/shuffle",
    vol.Required("renderer_id"): str,
    vol.Required("shuffle"): bool,
})
@websocket_api.async_response
async def ws_shuffle(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Set shuffle mode."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_shuffle(msg["renderer_id"], msg["shuffle"])
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/repeat_mode",
    vol.Required("renderer_id"): str,
    vol.Required("mode"): vol.In(["off", "all", "one"]),
})
@websocket_api.async_response
async def ws_repeat_mode(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Set repeat mode (off, all, one)."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_repeat_mode(msg["renderer_id"], msg["mode"])
    connection.send_result(msg["id"], {"success": True})


# --- Queue Operations ---


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_get",
    vol.Required("renderer_id"): str,
})
@websocket_api.async_response
async def ws_queue_get(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Get queue entries for a renderer."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    state = bridge.get_renderer_state(renderer_id)
    queue_info = (state or {}).get("queue") or {}

    entries = await bridge.async_get_queue(renderer_id)
    
    # Collect item IDs that need metadata resolution
    # Use a dict to deduplicate while preserving order
    items_needing_metadata = {}
    for entry in entries:
        metadata = entry.get("metadata") or {}
        if not metadata.get("title") and not metadata.get("name"):
            item_id = entry.get("itemId")
            if item_id and item_id.startswith("lib:") and item_id not in items_needing_metadata:
                items_needing_metadata[item_id] = True
    
    # Batch fetch metadata for items without inline metadata
    resolved_metadata = {}
    if items_needing_metadata:
        resolved_metadata = await bridge.async_fetch_metadata_batch(list(items_needing_metadata.keys()))
        _LOGGER.debug("Queue metadata resolution: requested=%d resolved=%d", 
                      len(items_needing_metadata), len(resolved_metadata))
    
    formatted = []
    for entry in entries:
        metadata = entry.get("metadata") or {}
        item_id = entry.get("itemId", "")
        
        # If no inline metadata, try resolved metadata
        if not metadata.get("title") and not metadata.get("name"):
            if item_id in resolved_metadata:
                metadata = resolved_metadata[item_id]
                _LOGGER.debug("Using resolved metadata for %s: title=%s", 
                              item_id, metadata.get("title") or metadata.get("name"))
        
        # Queue metadata uses 'title' but may have 'name' as fallback
        title = metadata.get("title") or metadata.get("name") or "Unknown"
        # Handle both single artist string and artists array
        artist = metadata.get("artist") or ""
        if not artist:
            artists = metadata.get("artists")
            if isinstance(artists, list) and artists:
                artist = ", ".join(artists)
        formatted.append({
            "queueEntryId": entry.get("queueEntryId", ""),
            "itemId": item_id,
            "title": title,
            "artist": artist,
            "album": metadata.get("album", ""),
            "duration_ms": metadata.get("durationMs", 0),
            "art_url": bridge.rewrite_artwork_url(metadata.get("artworkUrl") or metadata.get("imageUrl")),
        })

    result = {
        "revision": queue_info.get("revision", 0),
        "index": queue_info.get("index", 0),
        "entries": formatted,
    }
    connection.send_result(msg["id"], result)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_add",
    vol.Required("renderer_id"): str,
    vol.Required("mode"): vol.In(["replace", "next", "append"]),
    vol.Required("items"): [str],
})
@websocket_api.async_response
async def ws_queue_add(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Add items to queue."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    mode = msg["mode"]
    items = msg["items"]

    success = await bridge.async_queue_add(renderer_id, items, mode)
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_remove",
    vol.Required("renderer_id"): str,
    vol.Exclusive("queue_entry_id", "target"): str,
    vol.Exclusive("index", "target"): int,
})
@websocket_api.async_response
async def ws_queue_remove(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Remove item from queue."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    entry_id = msg.get("queue_entry_id")
    index = msg.get("index")

    success = await bridge.async_queue_remove(renderer_id, entry_id=entry_id, index=index)
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_move",
    vol.Required("renderer_id"): str,
    vol.Required("from_index"): int,
    vol.Required("to_index"): int,
    vol.Optional("if_revision"): int,
})
@websocket_api.async_response
async def ws_queue_move(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Move queue entry (drag-drop reorder)."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    from_idx = msg["from_index"]
    to_idx = msg["to_index"]
    if_revision = msg.get("if_revision")

    success = await bridge.async_queue_move(renderer_id, from_idx, to_idx, if_revision)
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_clear",
    vol.Required("renderer_id"): str,
})
@websocket_api.async_response
async def ws_queue_clear(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Clear queue."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_queue_clear(msg["renderer_id"])
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_jump",
    vol.Required("renderer_id"): str,
    vol.Required("index"): int,
})
@websocket_api.async_response
async def ws_queue_jump(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Jump to queue index."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_queue_jump(msg["renderer_id"], msg["index"])
    connection.send_result(msg["id"], {"success": True})


# --- Playlists ---


@websocket_api.websocket_command({vol.Required("type"): "mu/playlists_list"})
@websocket_api.async_response
async def ws_playlists_list(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """List all playlists."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    playlists = []
    for playlist_id, info in bridge.list_playlists_full():
        playlists.append({
            "playlistId": playlist_id,
            "name": info.get("name", playlist_id),
            "size": info.get("size", 0),
            "revision": info.get("revision", 0),
        })
    connection.send_result(msg["id"], playlists)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_get",
    vol.Required("playlist_id"): str,
})
@websocket_api.async_response
async def ws_playlist_get(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Get playlist contents."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    playlist = await bridge.async_fetch_playlist(msg["playlist_id"])
    if playlist is None:
        connection.send_error(msg["id"], "not_found", "Playlist not found")
        return

    entries = []
    for entry in playlist.get("entries") or []:
        ref = entry.get("ref") or {}
        metadata = entry.get("metadata") or {}
        entries.append({
            "entryId": entry.get("entryId", ""),
            "itemId": ref.get("id", ""),
            "title": metadata.get("title", "Unknown"),
            "artist": metadata.get("artist", ""),
            "album": metadata.get("album", ""),
            "art_url": bridge.rewrite_artwork_url(metadata.get("artworkUrl")),
        })

    result = {
        "playlistId": msg["playlist_id"],
        "name": playlist.get("name", ""),
        "revision": playlist.get("revision", 0),
        "entries": entries,
    }
    connection.send_result(msg["id"], result)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_create",
    vol.Required("name"): str,
})
@websocket_api.async_response
async def ws_playlist_create(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Create a new playlist."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    playlist_id = await bridge.async_playlist_create(msg["name"])
    if playlist_id is None:
        connection.send_error(msg["id"], "create_failed", "Failed to create playlist")
        return
    connection.send_result(msg["id"], {"playlistId": playlist_id})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_rename",
    vol.Required("playlist_id"): str,
    vol.Required("name"): str,
})
@websocket_api.async_response
async def ws_playlist_rename(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Rename a playlist."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_playlist_rename(msg["playlist_id"], msg["name"])
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_add",
    vol.Required("playlist_id"): str,
    vol.Required("item_id"): str,
    vol.Optional("mode", default="append"): vol.In(["append", "insert"]),
    vol.Optional("at_index"): int,
})
@websocket_api.async_response
async def ws_playlist_add(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Add item to playlist."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_playlist_add_item(
        msg["playlist_id"],
        msg["item_id"],
        msg["mode"],
        msg.get("at_index"),
    )
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_remove",
    vol.Required("playlist_id"): str,
    vol.Required("entry_id"): str,
})
@websocket_api.async_response
async def ws_playlist_remove(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Remove item from playlist."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_playlist_remove_item(msg["playlist_id"], msg["entry_id"])
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_move",
    vol.Required("playlist_id"): str,
    vol.Required("from_index"): int,
    vol.Required("to_index"): int,
    vol.Optional("if_revision"): int,
})
@websocket_api.async_response
async def ws_playlist_move(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Move playlist entry (reorder)."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_playlist_move(
        msg["playlist_id"],
        msg["from_index"],
        msg["to_index"],
        msg.get("if_revision"),
    )
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_delete",
    vol.Required("playlist_id"): str,
})
@websocket_api.async_response
async def ws_playlist_delete(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Delete a playlist."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_delete_playlist(msg["playlist_id"])
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_load_playlist",
    vol.Required("renderer_id"): str,
    vol.Required("playlist_id"): str,
    vol.Optional("mode", default="replace"): vol.In(["replace", "next", "append"]),
})
@websocket_api.async_response
async def ws_queue_load_playlist(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Load playlist into renderer queue."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_load_playlist(
        msg["renderer_id"],
        msg["playlist_id"],
        msg["mode"],
    )
    connection.send_result(msg["id"], {"success": True})


# --- Snapshots ---


@websocket_api.websocket_command({vol.Required("type"): "mu/snapshots_list"})
@websocket_api.async_response
async def ws_snapshots_list(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """List all snapshots."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    snapshots_raw = await bridge.async_list_snapshots()
    snapshots = [{"snapshotId": sid, "name": name} for sid, name in snapshots_raw]
    connection.send_result(msg["id"], snapshots)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/snapshot_save",
    vol.Required("renderer_id"): str,
    vol.Required("name"): str,
})
@websocket_api.async_response
async def ws_snapshot_save(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Save current queue as snapshot."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_save_snapshot(msg["renderer_id"], msg["name"])
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/queue_load_snapshot",
    vol.Required("renderer_id"): str,
    vol.Required("snapshot_id"): str,
    vol.Optional("mode", default="replace"): vol.In(["replace", "next", "append"]),
})
@websocket_api.async_response
async def ws_queue_load_snapshot(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Load snapshot into renderer queue."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_load_snapshot(msg["renderer_id"], msg["snapshot_id"])
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/snapshot_delete",
    vol.Required("snapshot_id"): str,
})
@websocket_api.async_response
async def ws_snapshot_delete(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Delete a snapshot."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.async_remove_snapshot(msg["snapshot_id"])
    connection.send_result(msg["id"], {"success": success})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_from_snapshot",
    vol.Required("snapshot_id"): str,
    vol.Required("name"): str,
})
@websocket_api.async_response
async def ws_playlist_from_snapshot(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Create a playlist from a snapshot."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    # Use the bridge's playlist.create with snapshotId
    playlist_id = await bridge.async_playlist_create_from_snapshot(msg["snapshot_id"], msg["name"])
    if playlist_id:
        connection.send_result(msg["id"], {"success": True, "playlistId": playlist_id})
    else:
        connection.send_result(msg["id"], {"success": False})

# --- Libraries ---


@websocket_api.websocket_command({vol.Required("type"): "mu/libraries_list"})
@websocket_api.async_response
async def ws_libraries_list(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """List all discovered libraries."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    libraries = []
    for node_id, name in bridge.list_libraries():
        libraries.append({
            "libraryId": node_id,
            "name": name,
        })
    connection.send_result(msg["id"], libraries)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/library_browse",
    vol.Required("library_id"): str,
    vol.Optional("container_id", default=""): str,
    vol.Optional("start", default=0): int,
    vol.Optional("count", default=100): int,
})
@websocket_api.async_response
async def ws_library_browse(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Browse library container."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    result = await bridge.async_browse_library(
        msg["library_id"],
        msg["container_id"],
        msg["start"],
        msg["count"],
    )
    if result is None:
        connection.send_error(msg["id"], "browse_failed", "Failed to browse library")
        return

    items = []
    for item in result.get("items") or []:
        item_type = (item.get("type") or "").lower()
        media_type = (item.get("mediaType") or "").lower()
        
        # Match media_player.py logic: playable items are explicit, everything else is a container
        is_playable = media_type in {"audio", "video"} or item_type in {
            "audio", "video", "movie", "episode", "musicvideo", "audiobook"
        }
        # Non-playable items are treated as containers (can browse into them)
        is_container = not is_playable

        # Handle artists array
        artists = item.get("artists") or []
        artist_str = ", ".join(artists) if isinstance(artists, list) else str(artists) if artists else ""

        items.append({
            "itemId": item.get("itemId", ""),
            "title": item.get("name") or item.get("title") or "Unknown",
            "subtitle": artist_str or item.get("album") or "",
            "type": item_type,
            "mediaType": media_type,
            "isContainer": is_container,
            "isPlayable": is_playable,
            # Containers can also be played (enqueued)
            "canEnqueue": True,
            "art_url": bridge.rewrite_artwork_url(item.get("imageUrl") or item.get("artworkUrl")),
            "childCount": item.get("childCount"),
        })

    connection.send_result(msg["id"], {
        "containerId": msg["container_id"],
        "totalCount": result.get("total") or result.get("totalCount") or len(items),
        "items": items,
    })


# --- Zone Controllers & Zones ---


@websocket_api.websocket_command({vol.Required("type"): "mu/zone_controllers_list"})
@websocket_api.async_response
async def ws_zone_controllers_list(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """List zone controllers with sources and zones."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    controllers = []
    for controller_id in bridge.list_zone_controllers():
        controller = bridge.get_zone_controller(controller_id)
        if controller:
            controllers.append({
                "controllerId": controller_id,
                "name": controller.get("name", controller_id),
                "sources": controller.get("sources", []),
                "zones": controller.get("zones", []),
            })

    connection.send_result(msg["id"], controllers)


@websocket_api.websocket_command({vol.Required("type"): "mu/zones_list"})
@websocket_api.async_response
async def ws_zones_list(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """List all zones with current state."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    zones = []
    for zone_id in bridge.list_zones():
        zone = bridge.get_zone(zone_id)
        if zone:
            state = zone.get("state", {})
            zones.append({
                "zoneId": zone_id,
                "name": zone.get("name", zone_id),
                "controllerId": zone.get("controllerId", ""),
                "volume": state.get("volume", 0),
                "mute": state.get("mute", False),
                "sourceId": state.get("sourceId", ""),
                "connected": state.get("connected", False),
            })

    connection.send_result(msg["id"], zones)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/zone_set_volume",
    vol.Required("zone_id"): str,
    vol.Required("volume"): vol.Coerce(float),
})
@websocket_api.async_response
async def ws_zone_set_volume(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Set volume for a zone."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_zone_set_volume(msg["zone_id"], msg["volume"])
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/zone_set_mute",
    vol.Required("zone_id"): str,
    vol.Required("mute"): bool,
})
@websocket_api.async_response
async def ws_zone_set_mute(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Set mute for a zone."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_zone_set_mute(msg["zone_id"], msg["mute"])
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/zone_select_source",
    vol.Required("zone_id"): str,
    vol.Required("source_id"): str,
})
@websocket_api.async_response
async def ws_zone_select_source(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Select source for a zone."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    await bridge.async_zone_select_source(msg["zone_id"], msg["source_id"])
    connection.send_result(msg["id"], {"success": True})


# --- Playlist Servers ---


@websocket_api.websocket_command({vol.Required("type"): "mu/playlist_servers_list"})
@websocket_api.async_response
async def ws_playlist_servers_list(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """List all discovered playlist servers."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    servers = []
    selected = bridge.get_selected_playlist_server()
    for node_id, name in bridge.list_playlist_servers():
        servers.append({
            "nodeId": node_id,
            "name": name,
            "selected": node_id == selected,
        })
    connection.send_result(msg["id"], servers)


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_server_select",
    vol.Required("node_id"): str,
})
@websocket_api.async_response
async def ws_playlist_server_select(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Select which playlist server to use."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    success = await bridge.select_playlist_server(msg["node_id"])
    if not success:
        connection.send_error(msg["id"], "not_found", f"Playlist server {msg['node_id']} not found")
        return
    connection.send_result(msg["id"], {"success": True})


@websocket_api.websocket_command({
    vol.Required("type"): "mu/playlist_save_from_queue",
    vol.Required("renderer_id"): str,
    vol.Required("playlist_id"): str,
})
@websocket_api.async_response
async def ws_playlist_save_from_queue(
    hass: HomeAssistant,
    connection: websocket_api.ActiveConnection,
    msg: dict[str, Any],
) -> None:
    """Save current queue contents to a playlist (replaces all items)."""
    bridge = _get_bridge(hass)
    if bridge is None:
        connection.send_error(msg["id"], "not_ready", "MU integration not ready")
        return

    renderer_id = msg["renderer_id"]
    playlist_id = msg["playlist_id"]

    # Get current queue entries
    entries = await bridge.async_get_queue(renderer_id)
    if entries is None:
        connection.send_error(msg["id"], "queue_error", "Failed to get queue")
        return

    # Extract item IDs from queue entries
    item_ids = [entry.get("itemId") for entry in entries if entry.get("itemId")]

    # Replace playlist items
    success = await bridge.async_playlist_replace_items(playlist_id, item_ids)
    if not success:
        connection.send_error(msg["id"], "save_failed", "Failed to save queue to playlist")
        return

    connection.send_result(msg["id"], {"success": True, "itemCount": len(item_ids)})


