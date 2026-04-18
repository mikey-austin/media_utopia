"""MQTT bridge for Mud."""

from __future__ import annotations

import asyncio
import json
import logging
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from collections.abc import Callable
from typing import Any
from urllib.parse import quote, urljoin, urlparse, urlunparse

from homeassistant.components import mqtt
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.network import get_url
from homeassistant.helpers.storage import Store
import voluptuous as vol

from .const import (
    CONF_DISCOVERY_PREFIX,
    CONF_ENTITY_PREFIX,
    CONF_IDENTITY,
    CONF_ARTWORK_BASE_URL,
    CONF_PLAYLIST_REFRESH,
    CONF_TOPIC_BASE,
    DEFAULT_DISCOVERY_PREFIX,
    DEFAULT_ENTITY_PREFIX,
    DEFAULT_IDENTITY,
    DEFAULT_ARTWORK_BASE_URL,
    DEFAULT_PLAYLIST_REFRESH,
    DEFAULT_TOPIC_BASE,
    DOMAIN,
    LEASE_RENEW_THRESHOLD_SECONDS,
    LEASE_TTL_MS,
    RENDERER_LOAD_TIMEOUT_SECONDS,
    REPLY_TIMEOUT_SECONDS,
)
from .views import ARTWORK_PROXY_PATH

_LOGGER = logging.getLogger(__name__)

try:
    from homeassistant.components.image_proxy import async_image_proxy_url as _image_proxy_url
except Exception:  # pragma: no cover - fallback for older HA
    try:
        from homeassistant.helpers.image_proxy import (  # type: ignore[no-redef]
            async_image_proxy_url as _image_proxy_url,
        )
    except Exception:  # pragma: no cover - fallback if helper missing
        _image_proxy_url = None


@dataclass
class Lease:
    session_id: str
    token: str
    expires_at: int


class MudBridge:
    """Bridge between Mud MQTT and Home Assistant MQTT discovery."""

    def __init__(self, hass: HomeAssistant, entry: ConfigEntry) -> None:
        self.hass = hass
        self.entry = entry
        data = entry.data
        self.topic_base = data.get(CONF_TOPIC_BASE, DEFAULT_TOPIC_BASE)
        self.discovery_prefix = data.get(CONF_DISCOVERY_PREFIX, DEFAULT_DISCOVERY_PREFIX)
        self.entity_prefix = data.get(CONF_ENTITY_PREFIX, DEFAULT_ENTITY_PREFIX)
        self.identity = data.get(CONF_IDENTITY, DEFAULT_IDENTITY)
        self.artwork_base_url = data.get(
            CONF_ARTWORK_BASE_URL, DEFAULT_ARTWORK_BASE_URL
        ).strip()
        self.playlist_refresh = int(
            data.get(CONF_PLAYLIST_REFRESH, DEFAULT_PLAYLIST_REFRESH)
        )

        self.reply_topic = f"{self.topic_base}/reply/ha-{uuid.uuid4().hex}"
        self._pending: dict[str, asyncio.Future] = {}
        self._unsubscribes: list[Callable] = []
        self._playlist_task: asyncio.Task | None = None
        self._discovery_topics: set[str] = set()
        self._discovery_store = Store(hass, 1, f"{DOMAIN}_discovery_topics")

        self._renderers: dict[str, dict[str, Any]] = {}
        self._renderer_topics: dict[str, dict[str, str]] = {}
        self._renderer_listeners: list[Callable] = []
        self._renderer_state_listeners: list[Callable] = []
        self._metadata_cache: dict[str, dict[str, Any]] = {}
        self._metadata_failures: dict[str, float] = {}
        self._request_sema = asyncio.Semaphore(4)
        self._metadata_sema = asyncio.Semaphore(2)
        self._libraries: dict[str, dict[str, Any]] = {}
        self._playlist_servers: dict[str, dict[str, Any]] = {}  # All servers by nodeId
        self._selected_playlist_server: str | None = None  # Currently selected server
        self._playlists: dict[str, dict[str, Any]] = {}
        self._leases: dict[str, Lease] = {}
        self._snapshot_names: dict[str, str] = {}
        self._playlist_listeners: list[Callable] = []
        self._zone_controllers: dict[str, dict[str, Any]] = {}
        self._zone_controller_listeners: list[Callable] = []
        self._zones: dict[str, dict[str, Any]] = {}
        self._zone_listeners: list[Callable] = []
        self._zone_state_listeners: list[Callable] = []
        self._image_proxy_url = _image_proxy_url

    async def async_start(self) -> None:
        """Start the bridge."""
        self._ready = False
        await mqtt.async_wait_for_mqtt_client(self.hass)

        await self._cleanup_discovery_topics()

        self._unsubscribes.append(
            await mqtt.async_subscribe(self.hass, self.reply_topic, self._on_reply)
        )
        self._unsubscribes.append(
            await mqtt.async_subscribe(
                self.hass,
                f"{self.topic_base}/node/+/presence",
                self._on_presence,
            )
        )
        self._unsubscribes.append(
            await mqtt.async_subscribe(
                self.hass,
                f"{self.topic_base}/node/+/state",
                self._on_state,
            )
        )

        self._register_services()

        # Wait for MQTT subscriptions to be confirmed and retained messages to arrive
        await asyncio.sleep(0.5)
        self._ready = True
        _LOGGER.debug("mu bridge started with topic_base=%s", self.topic_base)

        self._playlist_task = asyncio.create_task(self._playlist_loop())

    async def async_stop(self) -> None:
        """Stop the bridge."""
        for unsub in self._unsubscribes:
            try:
                unsub()
            except Exception:
                pass
        self._unsubscribes.clear()
        if self._playlist_task:
            self._playlist_task.cancel()
            self._playlist_task = None
        await self._save_discovery_topics()

    async def _cleanup_discovery_topics(self) -> None:
        data = await self._discovery_store.async_load() or {}
        topics = data.get("topics") or []
        if not isinstance(topics, list):
            topics = []
        for topic in topics:
            if not isinstance(topic, str):
                continue
            await mqtt.async_publish(self.hass, topic, "", qos=1, retain=True)
        self._discovery_topics.clear()
        await self._save_discovery_topics()

    async def _save_discovery_topics(self) -> None:
        await self._discovery_store.async_save({"topics": sorted(self._discovery_topics)})

    def _register_services(self) -> None:
        async def _load_playlist(call):
            renderer = call.data.get("renderer")
            playlist = call.data.get("playlist")
            mode = call.data.get("mode", "replace")
            resolve = call.data.get("resolve", "auto")
            if not renderer or not playlist:
                _LOGGER.warning("load_playlist: renderer and playlist are required")
                return
            renderer_id = self._resolve_renderer(renderer)
            if renderer_id is None:
                _LOGGER.warning("load_playlist: renderer %r not found", renderer)
                return
            playlist_id = await self._resolve_playlist_id(playlist)
            if playlist_id is None:
                _LOGGER.warning("load_playlist: playlist %r not found", playlist)
                return
            await self.async_load_playlist(renderer_id, playlist_id, mode, resolve)

        async def _clear_queue(call):
            renderer = call.data.get("renderer")
            if not renderer:
                _LOGGER.warning("clear_queue: renderer is required")
                return
            renderer_id = self._resolve_renderer(renderer)
            if renderer_id is None:
                _LOGGER.warning("clear_queue: renderer %r not found", renderer)
                return
            await self._send_renderer_command(renderer_id, "queue.clear", {})

        async def _shuffle_queue(call):
            renderer = call.data.get("renderer")
            if not renderer:
                _LOGGER.warning("shuffle_queue: renderer is required")
                return
            renderer_id = self._resolve_renderer(renderer)
            if renderer_id is None:
                _LOGGER.warning("shuffle_queue: renderer %r not found", renderer)
                return
            await self._send_renderer_command(
                renderer_id, "queue.shuffle", {"seed": int(time.time())}
            )

        schema = vol.Schema(
            {
                vol.Required("renderer"): str,
                vol.Required("playlist"): str,
                vol.Optional("mode", default="replace"): vol.In(
                    ["replace", "append", "next"]
                ),
                vol.Optional("resolve", default="auto"): vol.In(
                    ["auto", "yes", "no"]
                ),
            }
        )
        self.hass.services.async_register(
            DOMAIN, "load_playlist", _load_playlist, schema=schema
        )

        renderer_schema = vol.Schema({vol.Required("renderer"): str})
        self.hass.services.async_register(
            DOMAIN, "clear_queue", _clear_queue, schema=renderer_schema
        )
        self.hass.services.async_register(
            DOMAIN, "shuffle_queue", _shuffle_queue, schema=renderer_schema
        )

        async def _save_snapshot(call):
            renderer = call.data.get("renderer")
            name = call.data.get("name")
            if not renderer or not name:
                _LOGGER.warning("save_snapshot: renderer and name are required")
                return
            renderer_id = self._resolve_renderer(renderer)
            if renderer_id is None:
                _LOGGER.warning("save_snapshot: renderer %r not found", renderer)
                return
            success = await self.async_save_snapshot(renderer_id, name)
            if not success:
                _LOGGER.warning("save_snapshot: failed to save %r for %s", name, renderer_id)

        snapshot_schema = vol.Schema({
            vol.Required("renderer"): str,
            vol.Required("name"): str,
        })
        self.hass.services.async_register(
            DOMAIN, "save_snapshot", _save_snapshot, schema=snapshot_schema
        )

    async def _playlist_loop(self) -> None:
        # Immediate refresh on startup if playlist server already known
        if self._playlist_server is not None:
            for attempt in range(3):
                try:
                    await self._refresh_playlists()
                    break
                except Exception:
                    _LOGGER.debug("playlist refresh attempt %d failed, retrying", attempt + 1)
                    await asyncio.sleep(1)

        while True:
            try:
                await asyncio.sleep(self.playlist_refresh)
                if self._selected_playlist_server is not None:
                    await self._refresh_playlists()
            except asyncio.CancelledError:
                return
            except Exception:
                _LOGGER.debug("playlist refresh failed", exc_info=True)

    async def _on_reply(self, msg) -> None:
        try:
            payload = json.loads(msg.payload)
        except Exception:
            return
        cmd_id = payload.get("id")
        if not cmd_id:
            return
        reply_type = payload.get("type", "?")
        err = payload.get("err") or {}
        err_info = f" err={err.get('code')}:{err.get('message','')}" if err else ""
        _LOGGER.warning(
            "DIAG reply cmd_id=%s type=%s ok=%s%s",
            cmd_id[:12] if cmd_id else "?", reply_type, payload.get("ok"), err_info,
        )
        future = self._pending.pop(cmd_id, None)
        if future and not future.done():
            future.set_result(payload)

    async def _on_presence(self, msg) -> None:
        try:
            payload = json.loads(msg.payload)
        except Exception:
            return
        node_id = payload.get("nodeId")
        kind = payload.get("kind")
        if not node_id or not kind:
            return
        _LOGGER.debug("presence %s kind=%s", node_id, kind)
        if kind == "renderer":
            payload["online"] = True
            self._renderers[node_id] = payload
            self._notify_renderer_listeners(node_id)
        elif kind == "library":
            self._libraries[node_id] = payload
            # Invalidate cached metadata from this library so artwork URLs
            # are refreshed (the library's HTTP port may have changed).
            self._invalidate_library_cache(node_id)
        elif kind == "playlist":
            self._playlist_servers[node_id] = payload
            await self._publish_playlist_server_availability(node_id, "online")
            if self._selected_playlist_server is None:
                # Try to verify this server responds before auto-selecting
                self._selected_playlist_server = node_id
                _LOGGER.info("auto-selected playlist server: %s", node_id)
            elif self._selected_playlist_server not in self._playlist_servers:
                # Previously selected server disappeared, switch to this one
                self._selected_playlist_server = node_id
                _LOGGER.info("switched playlist server to %s (previous not found)", node_id)
            # Only refresh immediately if bridge is ready and this is the selected server
            if getattr(self, "_ready", False) and node_id == self._selected_playlist_server:
                await self._refresh_playlists()
        elif kind == "zone_controller":
            self._zone_controllers[node_id] = payload
            self._notify_zone_controller_listeners(node_id)
        elif kind == "zone":
            self._zones[node_id] = payload
            self._notify_zone_listeners(node_id)

    def _cache_metadata(self, key: str, metadata: dict[str, Any]) -> None:
        """Cache metadata with size limit."""
        if len(self._metadata_cache) > 10000:
            # Evict oldest entries (dict preserves insertion order in Python 3.7+)
            to_remove = list(self._metadata_cache.keys())[:2000]
            for k in to_remove:
                del self._metadata_cache[k]
        self._metadata_cache[key] = metadata

    def _invalidate_library_cache(self, library_id: str) -> None:
        """Remove cached metadata for items belonging to a library.

        Called when a library re-announces presence (e.g. after restart),
        since its HTTP port may have changed making cached artwork URLs stale.
        """
        prefix = f"{library_id}|"
        stale = [k for k in self._metadata_cache if k.startswith(prefix)]
        for k in stale:
            del self._metadata_cache[k]
        stale_failures = [k for k in self._metadata_failures if k.startswith(prefix)]
        for k in stale_failures:
            del self._metadata_failures[k]
        if stale:
            _LOGGER.debug(
                "invalidated %d cached metadata entries for library %s",
                len(stale),
                library_id,
            )

    async def _on_state(self, msg) -> None:
        try:
            payload = json.loads(msg.payload)
        except Exception:
            return
        node_id = self._node_id_from_topic(msg.topic)
        if node_id is None:
            return
        if node_id in self._renderers:
            _LOGGER.debug("state %s status=%s", node_id, (payload.get("playback") or {}).get("status"))
            self._renderers[node_id]["state"] = payload
            self._maybe_resolve_metadata(node_id, payload)
            self._notify_renderer_state_listeners(node_id, payload)
            await self._publish_renderer_state(node_id, payload)

        if node_id in self._zones:
            self._zones[node_id]["state"] = payload
            self._notify_zone_state_listeners(node_id, payload)

    def register_renderer_listener(self, callback) -> None:
        self._renderer_listeners.append(callback)
        for node_id in list(self._renderers.keys()):
            self._notify_renderer_listener(callback, node_id)

    def register_renderer_state_listener(self, callback) -> Callable:
        """Register a state listener. Returns an unregister function."""
        self._renderer_state_listeners.append(callback)
        def unregister():
            try:
                self._renderer_state_listeners.remove(callback)
            except ValueError:
                pass
        return unregister

    def list_libraries(self) -> list[tuple[str, str]]:
        items = []
        for node_id, info in self._libraries.items():
            name = info.get("name") or node_id
            items.append((node_id, name))
        items.sort(key=lambda item: item[1].lower())
        return items

    def get_library(self, node_id: str) -> dict[str, Any] | None:
        return self._libraries.get(node_id)

    def register_playlist_listener(self, callback) -> None:
        self._playlist_listeners.append(callback)
        for playlist_id in list(self._playlists.keys()):
            self._notify_playlist_listener(callback, playlist_id)

    def _notify_renderer_listeners(self, node_id: str) -> None:
        for callback in self._renderer_listeners:
            self._notify_renderer_listener(callback, node_id)

    def _notify_renderer_listener(self, callback, node_id: str) -> None:
        try:
            callback(node_id)
        except Exception:
            pass

    def _notify_renderer_state_listeners(self, node_id: str, state: dict[str, Any]) -> None:
        for callback in self._renderer_state_listeners:
            try:
                callback(node_id, state)
            except Exception:
                continue

    def _notify_playlist_listener(self, callback, playlist_id: str) -> None:
        try:
            callback(playlist_id)
        except Exception:
            pass

    def _notify_playlist_listeners(self, playlist_id: str) -> None:
        for callback in self._playlist_listeners:
            self._notify_playlist_listener(callback, playlist_id)

    def get_renderer(self, node_id: str) -> dict[str, Any] | None:
        return self._renderers.get(node_id)

    def get_renderer_state(self, node_id: str) -> dict[str, Any]:
        return self._renderers.get(node_id, {}).get("state") or {}

    def list_renderers(self) -> list[tuple[str, str]]:
        out: list[tuple[str, str]] = []
        for node_id, info in self._renderers.items():
            out.append((node_id, info.get("name", node_id)))
        return out

    def list_renderers_full(self) -> list[dict[str, Any]]:
        """List all known renderers with their online status and capabilities."""
        result = []
        for node_id, info in self._renderers.items():
            result.append({
                "rendererId": node_id,
                "name": info.get("name", node_id),
                "online": info.get("online", False),
                "caps": info.get("caps", {}),
            })
        return result

    def get_renderer_info(self, node_id: str) -> dict[str, Any] | None:
        """Get the full info dict for a renderer."""
        return self._renderers.get(node_id)

    def _mark_all_offline(self) -> None:
        """Mark all renderers as offline (e.g., on MQTT disconnect)."""
        for node_id, info in self._renderers.items():
            info["online"] = False
        _LOGGER.warning("marked all renderers offline")

    def register_zone_listener(self, callback) -> None:
        if callback not in self._zone_listeners:
            self._zone_listeners.append(callback)

    def register_zone_state_listener(self, callback) -> None:
        if callback not in self._zone_state_listeners:
            self._zone_state_listeners.append(callback)

    def register_zone_controller_listener(self, callback) -> None:
        if callback not in self._zone_controller_listeners:
            self._zone_controller_listeners.append(callback)

    def _notify_zone_listeners(self, node_id: str) -> None:
        for callback in self._zone_listeners:
            try:
                callback(node_id)
            except Exception:
                pass

    def _notify_zone_state_listeners(self, node_id: str, state: dict[str, Any]) -> None:
        for callback in self._zone_state_listeners:
            try:
                callback(node_id, state)
            except Exception:
                pass

    def _notify_zone_controller_listeners(self, node_id: str) -> None:
        for callback in self._zone_controller_listeners:
            try:
                callback(node_id)
            except Exception:
                pass

    def get_zone_controller(self, node_id: str) -> dict[str, Any] | None:
        return self._zone_controllers.get(node_id)

    def list_zone_controllers(self) -> list[str]:
        return list(self._zone_controllers.keys())

    def get_zone(self, node_id: str) -> dict[str, Any] | None:
        return self._zones.get(node_id)

    def list_zones(self) -> list[str]:
        return list(self._zones.keys())

    def _zone_controller_id(self, zone_id: str) -> str | None:
        zone = self._zones.get(zone_id) or {}
        controller_id = zone.get("controllerId")
        if not controller_id:
            return None
        return str(controller_id)

    async def async_zone_set_volume(self, node_id: str, volume: float) -> None:
        await self._send_zone_command(node_id, "zone.setVolume", {"volume": volume})

    async def async_zone_set_mute(self, node_id: str, mute: bool) -> None:
        await self._send_zone_command(node_id, "zone.setMute", {"mute": mute})

    async def async_zone_select_source(self, node_id: str, source_id: str) -> None:
        await self._send_zone_command(
            node_id, "zone.selectSource", {"sourceId": source_id}
        )

    async def _send_zone_command(
        self, zone_id: str, cmd_type: str, body: dict[str, Any]
    ) -> None:
        controller_id = self._zone_controller_id(zone_id)
        if controller_id is None:
            _LOGGER.warning("zone controller missing for %s cmd=%s", zone_id, cmd_type)
            return
        payload = {"zoneId": zone_id, **body}
        _LOGGER.debug(
            "send zone cmd=%s controller=%s zone=%s body=%s",
            cmd_type,
            controller_id,
            zone_id,
            body,
        )
        await self._publish_command(
            controller_id,
            cmd_type,
            payload,
            need_lease=False,
        )

    @property
    def _playlist_server(self) -> dict[str, Any] | None:
        """Get currently selected playlist server (backward compatibility)."""
        if self._selected_playlist_server is None:
            return None
        return self._playlist_servers.get(self._selected_playlist_server)

    def list_playlist_servers(self) -> list[tuple[str, str]]:
        """Return list of (node_id, name) for all discovered playlist servers."""
        out: list[tuple[str, str]] = []
        for node_id, info in self._playlist_servers.items():
            out.append((node_id, info.get("name", node_id)))
        return out

    def get_selected_playlist_server(self) -> str | None:
        """Get the currently selected playlist server node ID."""
        return self._selected_playlist_server

    async def select_playlist_server(self, node_id: str) -> bool:
        """Select a playlist server by node_id. Returns True if successful."""
        if node_id not in self._playlist_servers:
            return False
        self._selected_playlist_server = node_id
        _LOGGER.debug("selected playlist server: %s", node_id)
        # Clear cached playlists and refresh from new server
        self._playlists.clear()
        await self._refresh_playlists()
        return True

    def get_playlist(self, playlist_id: str) -> dict[str, Any] | None:
        return self._playlists.get(playlist_id)

    def set_snapshot_name(self, node_id: str, name: str) -> None:
        self._snapshot_names[node_id] = name

    def get_snapshot_name(self, node_id: str) -> str:
        return self._snapshot_names.get(node_id, "")

    def list_playlists(self) -> list[tuple[str, str]]:
        out: list[tuple[str, str]] = []
        for playlist_id, info in self._playlists.items():
            out.append((playlist_id, info.get("name", playlist_id)))
        return out

    def list_playlists_full(self) -> list[tuple[str, dict[str, Any]]]:
        """Return list of (playlist_id, info_dict)."""
        return list(self._playlists.items())

    @staticmethod
    def _make_library_ref(library_id: str, item_id: str) -> dict[str, str]:
        return {"kind": "libraryItem", "libraryId": library_id, "itemId": item_id}

    def _current_display(self, current: dict[str, Any] | None) -> dict[str, Any]:
        """Read the renderer-published display block off `current`.

        Prefers the wire `display` field; falls back to the legacy `metadata`
        slot when the renderer hasn't sent display (and we may have already
        folded a cached display into metadata via `_maybe_resolve_metadata`).
        """
        if not isinstance(current, dict):
            return {}
        display = current.get("display")
        if isinstance(display, dict) and display:
            return self._normalize_display(display)
        legacy = current.get("metadata")
        if isinstance(legacy, dict) and legacy:
            return legacy
        return {}

    def display_for_entry(self, entry: dict[str, Any]) -> dict[str, Any]:
        """Public helper: read the display block off a queue/playlist entry."""
        if not isinstance(entry, dict):
            return {}
        display = entry.get("display")
        if isinstance(display, dict) and display:
            return self._normalize_display(display)
        return {}

    @staticmethod
    def _ref_cache_key(ref: dict[str, Any] | None) -> str | None:
        if not isinstance(ref, dict):
            return None
        library_id = ref.get("libraryId")
        item_id = ref.get("itemId")
        if not library_id or not item_id:
            return None
        return f"{library_id}|{item_id}"

    @staticmethod
    def _ref_from_anything(value: Any) -> dict[str, str] | None:
        """Build a structured library ref from a dict input.

        Accepts {kind, libraryId, itemId} (canonical) or {libraryId, itemId}.
        Returns None for anything else.
        """
        if not isinstance(value, dict):
            return None
        library_id = value.get("libraryId")
        item_id = value.get("itemId")
        if not library_id or not item_id:
            return None
        return {
            "kind": value.get("kind") or "libraryItem",
            "libraryId": str(library_id),
            "itemId": str(item_id),
        }

    def _maybe_resolve_metadata(self, node_id: str, payload: dict[str, Any]) -> None:
        """Hot path: copy structured display into metadata for legacy consumers.

        After the protocol reset the renderer publishes display fields inline on
        `current.display`.  No catalog lookups happen here.
        """
        current = payload.get("current") or {}
        if not isinstance(current, dict):
            return
        display = current.get("display")
        if isinstance(display, dict) and display:
            normalized = self._normalize_display(display)
            current["metadata"] = normalized
            ref = current.get("ref")
            cache_key = self._ref_cache_key(ref)
            if cache_key:
                self._cache_metadata(cache_key, normalized)
            return
        # Display absent — leave metadata alone; the renderer is the source of
        # truth for what's playing and we do NOT issue follow-up library calls
        # from the state listener.
        cache_key = self._ref_cache_key(current.get("ref"))
        if cache_key:
            cached = self._metadata_cache.get(cache_key)
            if cached:
                current["metadata"] = cached

    async def async_get_item_displays(
        self, refs: list[dict[str, Any]]
    ) -> dict[str, dict[str, Any]]:
        """Slow-path display backfill via library.getItems.

        Keyed by `<libraryId>|<itemId>`.  Use only for explicit "refresh details"
        UX flows; do NOT call from the state listener.
        """
        results: dict[str, dict[str, Any]] = {}
        if not refs:
            return results
        groups: dict[str, list[dict[str, str]]] = {}
        for raw in refs:
            ref = self._ref_from_anything(raw)
            if not ref:
                continue
            key = self._ref_cache_key(ref)
            if not key:
                continue
            cached = self._metadata_cache.get(key)
            if cached:
                results[key] = self._normalize_display(cached)
                continue
            last_fail = self._metadata_failures.get(key)
            if last_fail and time.time() - last_fail < 60:
                continue
            groups.setdefault(ref["libraryId"], []).append(ref)

        for library_id, library_refs in groups.items():
            async with self._metadata_sema:
                reply = await self._request(
                    library_id,
                    "library.getItems",
                    {"refs": library_refs},
                    need_lease=False,
                    timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
                )
            if reply is None or reply.get("type") != "ack":
                for ref in library_refs:
                    key = self._ref_cache_key(ref)
                    if key:
                        self._metadata_failures[key] = time.time()
                continue
            items = (reply.get("body") or {}).get("items") or []
            for item in items:
                ref = item.get("ref") or {}
                key = self._ref_cache_key(ref)
                if not key:
                    continue
                if item.get("err"):
                    self._metadata_failures[key] = time.time()
                    continue
                display = item.get("display") or {}
                if not display:
                    continue
                normalized = self._normalize_display(display)
                self._cache_metadata(key, normalized)
                results[key] = normalized
        return results

    def _normalize_display(self, display: dict[str, Any]) -> dict[str, Any]:
        """Normalize a display block.

        Folds `artists` into `artist`, normalizes duration field name, and
        rewrites the artwork URL via the configured base.  Used both for
        renderer-published display blocks and for backfill from
        `library.getItems`.
        """
        if not display:
            return display
        result = dict(display)
        if "artists" in result and "artist" not in result:
            artists = result.get("artists", [])
            if isinstance(artists, list) and artists:
                result["artist"] = ", ".join(str(a) for a in artists)
        if "durationMs" not in result and "duration_ms" in result:
            result["durationMs"] = result.pop("duration_ms")
        art = result.get("artworkUrl")
        if art:
            result["artworkUrl"] = self._rewrite_artwork_base(art)
        return result

    # Back-compat shim — older callers still use `_normalize_metadata`.  The
    # display block and the legacy metadata block share field names so the
    # implementation is identical.
    _normalize_metadata = _normalize_display

    async def _resolve_sources_for_refs(
        self, refs: list[dict[str, Any]]
    ) -> dict[str, list[dict[str, Any]]]:
        """Resolve playback sources for a list of structured refs.

        Returns a mapping `<libraryId>|<itemId>` -> list of source dicts.
        """
        results: dict[str, list[dict[str, Any]]] = {}
        if not refs:
            return results
        groups: dict[str, list[dict[str, str]]] = {}
        for raw in refs:
            ref = self._ref_from_anything(raw)
            if not ref:
                continue
            groups.setdefault(ref["libraryId"], []).append(ref)

        for library_id, library_refs in groups.items():
            reply = await self._request(
                library_id,
                "library.resolveSourcesBatch",
                {"refs": library_refs},
                need_lease=False,
                timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
            )
            if reply is None or reply.get("type") != "ack":
                continue
            items = (reply.get("body") or {}).get("items") or []
            for item in items:
                ref = item.get("ref") or {}
                key = self._ref_cache_key(ref)
                if not key:
                    continue
                if item.get("err"):
                    continue
                sources = item.get("sources") or []
                if sources:
                    results[key] = sources
        return results

    async def _resolve_sources_for_ref(
        self, ref: dict[str, Any]
    ) -> list[dict[str, Any]]:
        canonical = self._ref_from_anything(ref)
        if not canonical:
            return []
        reply = await self._request(
            canonical["libraryId"],
            "library.resolveSources",
            {"ref": canonical},
            need_lease=False,
            timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
        )
        if reply is None or reply.get("type") != "ack":
            return []
        return (reply.get("body") or {}).get("sources") or []

    async def _publish_renderer_state(self, node_id: str, state: dict[str, Any]) -> None:
        topics = self._renderer_topics.get(node_id)
        if not topics:
            return
        playback = state.get("playback") or {}
        current = state.get("current") or {}
        display = self._current_display(current)

        status = playback.get("status") or "idle"
        if status == "stopped":
            status = "idle"

        title = display.get("title")
        artist = display.get("artist")
        album = display.get("album")
        artwork = self.rewrite_artwork_url(display.get("artworkUrl"))

        payload = {
            "state": status,
            "media_title": title,
            "media_artist": artist,
            "media_album_name": album,
            "entity_picture": artwork,
            "media_duration": int((playback.get("durationMs") or 0) / 1000),
            "media_position": int((playback.get("positionMs") or 0) / 1000),
            "media_position_updated_at": datetime.now(timezone.utc).isoformat(),
            "volume_level": playback.get("volume", 1.0),
            "is_volume_muted": playback.get("mute", False),
        }
        await self._publish(topics["state"], payload, retain=True)

    async def _publish_availability(self, node_id: str, status: str) -> None:
        topics = self._renderer_topics.get(node_id)
        if not topics:
            return
        await self._publish(topics["availability"], status, retain=True)

    async def _publish_playlist_server_availability(self, node_id: str, status: str) -> None:
        topic = self._playlist_availability_topic(node_id)
        await self._publish(topic, status, retain=True)

    async def _ensure_renderer_discovery(self, node_id: str) -> None:
        if node_id in self._renderer_topics:
            return
        renderer = self._renderers[node_id]
        unique = self._unique_id("renderer", node_id)
        topics = self._renderer_topics_for(node_id, unique)
        self._renderer_topics[node_id] = topics
        _ = renderer

    def rewrite_artwork_url(self, url: str | None, for_internal: bool = False) -> str | None:
        """Rewrite artwork URL for proxying.

        Args:
            url: The original artwork URL.
            for_internal: If True, return a URL that HA can fetch internally
                          (direct upstream URL). If False, return a proxied URL
                          for browser access.
        """
        if not url:
            return url
        if url.startswith("/api/image_proxy") or url.startswith(ARTWORK_PROXY_PATH):
            return url
        try:
            parsed = urlparse(url)
            if parsed.path == ARTWORK_PROXY_PATH or parsed.path.startswith("/api/image_proxy"):
                return url
        except Exception:
            pass
        try:
            rewritten = self._rewrite_artwork_base(url)
            # For internal HA fetching, return the direct URL (no proxy)
            if for_internal:
                return rewritten
            proxied = self._proxy_artwork_url(rewritten)
            if proxied:
                return proxied
            if self._image_proxy_url:
                try:
                    return self._image_proxy_url(self.hass, rewritten)
                except Exception:
                    return rewritten
            return rewritten
        except Exception:
            return url

    def _rewrite_artwork_base(self, url: str) -> str:
        if not self.artwork_base_url:
            return url
        parsed = urlparse(url)
        base = urlparse(self.artwork_base_url)
        if not parsed.scheme:
            return urljoin(self.artwork_base_url, url)
        rebuilt = parsed._replace(
            scheme=base.scheme or parsed.scheme,
            netloc=base.netloc or parsed.netloc,
        )
        return urlunparse(rebuilt)

    def _proxy_artwork_url(self, url: str) -> str | None:
        if not url:
            return None
        base_url = ""
        # Try to get external URL first (for reverse proxy setups)
        try:
            base_url = get_url(self.hass, prefer_external=True, allow_internal=False)
        except Exception:
            pass
        # Fall back to internal URL if external not available
        if not base_url:
            try:
                base_url = get_url(self.hass, prefer_external=True, allow_internal=True)
            except Exception:
                base_url = self.hass.config.external_url or ""
        encoded = quote(url, safe="")
        if not base_url:
            return f"{ARTWORK_PROXY_PATH}?url={encoded}"
        return f"{base_url.rstrip('/')}{ARTWORK_PROXY_PATH}?url={encoded}"

    async def _handle_renderer_command(self, msg) -> None:
        node_id = self._node_id_from_topic(msg.topic)
        if node_id is None:
            return
        payload_raw = msg.payload
        if isinstance(payload_raw, bytes):
            payload = payload_raw.decode("utf-8", errors="ignore")
        else:
            payload = str(payload_raw)

        topics = self._renderer_topics.get(node_id)
        if not topics:
            return

        if msg.topic == topics["command"]:
            await self._handle_command_topic(node_id, payload)
        elif msg.topic == topics["volume"]:
            await self._handle_volume(node_id, payload)
        elif msg.topic == topics["mute"]:
            await self._handle_mute(node_id, payload)
        elif msg.topic == topics["seek"]:
            await self._handle_seek(node_id, payload)
        elif msg.topic == topics["shuffle"]:
            await self._handle_shuffle(node_id, payload)
        elif msg.topic == topics["repeat"]:
            await self._handle_repeat(node_id, payload)
        elif msg.topic == topics["play_media"]:
            await self._handle_play_media(node_id, payload)

    async def _handle_command_topic(self, node_id: str, payload: str) -> None:
        payload = payload.lower()
        command = None
        if payload in ["play", "pause", "stop", "next", "previous"]:
            command = payload
        elif payload == "toggle":
            state = self._renderers.get(node_id, {}).get("state", {})
            status = (state.get("playback") or {}).get("status")
            command = "pause" if status == "playing" else "play"
        if command is None:
            return
        cmd_map = {
            "play": "playback.play",
            "pause": "playback.pause",
            "stop": "playback.stop",
            "next": "playback.next",
            "previous": "playback.prev",
        }
        await self._send_renderer_command(node_id, cmd_map[command], {})

    async def _handle_volume(self, node_id: str, payload: str) -> None:
        try:
            volume = float(payload)
        except ValueError:
            return
        volume = max(0.0, min(1.0, volume))
        await self._send_renderer_command(node_id, "playback.setVolume", {"volume": volume})

    async def _handle_mute(self, node_id: str, payload: str) -> None:
        muted = payload.strip().upper() == "ON"
        await self._send_renderer_command(node_id, "playback.setMute", {"mute": muted})

    async def _handle_seek(self, node_id: str, payload: str) -> None:
        position_s = None
        try:
            if payload.strip().startswith("{"):
                data = json.loads(payload)
                position_s = float(data.get("position", 0))
            else:
                position_s = float(payload)
        except ValueError:
            return
        await self._send_renderer_command(
            node_id, "playback.seek", {"positionMs": int(position_s * 1000)}
        )

    async def _handle_shuffle(self, node_id: str, payload: str) -> None:
        if payload.strip().upper() != "ON":
            return
        await self._send_renderer_command(node_id, "queue.shuffle", {"seed": int(time.time())})

    async def _handle_repeat(self, node_id: str, payload: str) -> None:
        mode = payload.strip().lower()
        repeat = mode in ["all", "on", "true"]
        await self._send_renderer_command(node_id, "queue.setRepeat", {"repeat": repeat})

    async def _handle_play_media(self, node_id: str, payload: str) -> None:
        try:
            data = json.loads(payload)
        except Exception:
            return
        media_id = data.get("media_content_id") or data.get("ref")
        if not media_id:
            return
        try:
            entries = await self._resolve_media_entries(media_id)
        except ValueError as exc:
            _LOGGER.warning("play_media rejected: %s", exc)
            return
        await self._queue_set_and_fill(node_id, entries)

    async def async_play(self, node_id: str) -> None:
        await self._send_renderer_command(node_id, "playback.play", {})

    async def async_pause(self, node_id: str) -> None:
        await self._send_renderer_command(node_id, "playback.pause", {})

    async def async_stop(self, node_id: str) -> None:
        await self._send_renderer_command(node_id, "playback.stop", {})

    async def async_next(self, node_id: str) -> None:
        state = self.get_renderer_state(node_id)
        queue = state.get("queue") or {}
        index = queue.get("index")
        length = queue.get("length")
        repeat = queue.get("repeat")
        if isinstance(index, int) and isinstance(length, int) and length > 0:
            if index >= length-1 and not repeat:
                await self._send_renderer_command(node_id, "playback.stop", {})
                return
        await self._send_renderer_command(node_id, "playback.next", {})

    async def async_previous(self, node_id: str) -> None:
        state = self.get_renderer_state(node_id)
        queue = state.get("queue") or {}
        index = queue.get("index")
        repeat = queue.get("repeat")
        if isinstance(index, int) and index <= 0 and not repeat:
            await self._send_renderer_command(node_id, "playback.play", {"index": 0})
            return
        await self._send_renderer_command(node_id, "playback.prev", {})

    async def async_set_volume(self, node_id: str, volume: float) -> None:
        await self._send_renderer_command(node_id, "playback.setVolume", {"volume": volume})

    async def async_mute(self, node_id: str, muted: bool) -> None:
        await self._send_renderer_command(node_id, "playback.setMute", {"mute": muted})

    async def async_seek(self, node_id: str, position: float) -> None:
        await self._send_renderer_command(
            node_id, "playback.seek", {"positionMs": int(position * 1000)}
        )

    async def async_get_queue(self, node_id: str) -> list[dict[str, Any]]:
        """Fetch the current queue entries for a renderer.

        Each entry carries its own `display` block (and optional `ref`/
        `resolved`) — controllers do NOT need to follow up with library
        lookups for ordinary rendering.
        """
        state = self.get_renderer_state(node_id)
        queue = state.get("queue") or {}
        length = queue.get("length") or 0
        if length == 0:
            return []
        entries: list[dict[str, Any]] = []
        from_index = 0
        page_size = 100
        while from_index < length:
            reply = await self._request(
                node_id,
                "queue.get",
                {"from": from_index, "count": page_size},
                need_lease=False,
            )
            if reply is None or reply.get("type") != "ack":
                break
            body = reply.get("body") or {}
            for entry in body.get("entries") or []:
                entries.append(entry)
            from_index += page_size
        return entries

    async def async_queue_jump(self, node_id: str, index: int) -> None:
        """Jump to a specific queue index and start playback."""
        await self._send_renderer_command(node_id, "playback.play", {"index": index})

    async def async_queue_add(
        self, node_id: str, items: list[Any], mode: str = "append"
    ) -> bool:
        """Add items to queue with mode: replace, next, or append.

        Each item must be either a structured library ref dict
        (`{kind, libraryId, itemId}` or `{libraryId, itemId}`) or one of the
        recognised string forms (a `mu:item:` internal browse id, a direct
        http(s):// URL, or a `playlist:`/`snapshot:` selector handled
        elsewhere).
        """
        if not items:
            return True
        # Bulk-resolve all structured refs with one batch call before falling
        # back to per-item resolution for non-ref forms.
        ref_inputs: list[dict[str, Any]] = []
        misc_inputs: list[Any] = []
        for raw in items:
            ref = self._ref_from_anything(raw)
            if ref is not None:
                ref_inputs.append(ref)
            else:
                misc_inputs.append(raw)

        entries: list[dict[str, Any]] = []
        if ref_inputs:
            entries.extend(await self._entries_for_refs(ref_inputs))
        for raw in misc_inputs:
            resolved = await self._resolve_media_entries(raw)
            entries.extend(resolved)
        _LOGGER.debug(
            "queue_add: node=%s mode=%s items=%d entries=%d",
            node_id,
            mode,
            len(items),
            len(entries),
        )
        if not entries:
            _LOGGER.warning("queue_add: no entries resolved")
            return False

        if mode == "replace":
            await self._queue_set_and_fill(node_id, entries)
            return True

        position = "next" if mode == "next" else "end"
        reply = await self._request_with_lease(
            node_id,
            "queue.add",
            {"position": position, "entries": entries},
        )
        return reply is not None and reply.get("type") == "ack"

    async def async_queue_remove(
        self, node_id: str, entry_id: str | None = None, index: int | None = None
    ) -> bool:
        """Remove item from queue by entry ID or index."""
        body: dict[str, Any] = {}
        if entry_id:
            body["queueEntryId"] = entry_id
        elif index is not None:
            body["index"] = index
        else:
            return False
        reply = await self._request_with_lease(node_id, "queue.remove", body)
        return reply is not None and reply.get("type") == "ack"

    async def async_queue_move(
        self, node_id: str, from_index: int, to_index: int, if_revision: int | None = None
    ) -> bool:
        """Move queue entry from one index to another."""
        body: dict[str, Any] = {"fromIndex": from_index, "toIndex": to_index}
        if if_revision is not None:
            # Add revision guard to request envelope, not body
            pass  # ifRevision is handled at envelope level in _request
        reply = await self._request_with_lease(node_id, "queue.move", body)
        return reply is not None and reply.get("type") == "ack"

    async def async_queue_clear(self, node_id: str) -> bool:
        """Clear the queue."""
        reply = await self._request_with_lease(node_id, "queue.clear", {})
        return reply is not None and reply.get("type") == "ack"

    async def async_queue_shuffle(self, node_id: str) -> bool:
        """Shuffle the queue."""
        reply = await self._request_with_lease(
            node_id, "queue.shuffle", {"seed": int(time.time())}
        )
        return reply is not None and reply.get("type") == "ack"

    async def async_playlist_create(self, name: str) -> str | None:
        """Create a new playlist and return its ID."""
        if self._playlist_server is None:
            return None
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.create",
            {"name": name},
        )
        if reply is None or reply.get("type") != "ack":
            return None
        body = reply.get("body") or {}
        return body.get("playlistId")

    async def async_playlist_create_from_snapshot(self, snapshot_id: str, name: str) -> str | None:
        """Create a new playlist from a snapshot and return its ID."""
        if self._playlist_server is None:
            return None
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.create",
            {"name": name, "snapshotId": snapshot_id},
        )
        if reply is None or reply.get("type") != "ack":
            return None
        body = reply.get("body") or {}
        return body.get("playlistId")

    async def async_playlist_rename(self, playlist_id: str, name: str) -> bool:
        """Rename a playlist."""
        if self._playlist_server is None:
            return False
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.rename",
            {"playlistId": playlist_id, "name": name},
        )
        return reply is not None and reply.get("type") == "ack"

    async def async_delete_playlist(self, playlist_id: str) -> bool:
        """Delete a playlist."""
        if self._playlist_server is None:
            return False
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.delete",
            {"playlistId": playlist_id},
        )
        success = reply is not None and reply.get("type") == "ack"
        if success:
            self._playlists.pop(playlist_id, None)
        return success

    async def async_playlist_add_item(
        self,
        playlist_id: str,
        ref: dict[str, Any],
        mode: str = "append",
        at_index: int | None = None,
    ) -> bool:
        """Add a single library item to a playlist using a structured ref."""
        if self._playlist_server is None:
            return False
        canonical = self._ref_from_anything(ref)
        if canonical is None:
            _LOGGER.warning("playlist.addItems rejected: invalid ref %r", ref)
            return False
        body: dict[str, Any] = {
            "playlistId": playlist_id,
            "entries": [{"ref": canonical}],
        }
        if mode == "insert" and at_index is not None:
            body["atIndex"] = at_index
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.addItems",
            body,
        )
        return reply is not None and reply.get("type") == "ack"

    async def async_playlist_add_items(
        self, playlist_id: str, refs: list[dict[str, Any]]
    ) -> bool:
        """Add items to an existing playlist via structured refs."""
        if self._playlist_server is None or not refs:
            return False
        entries: list[dict[str, Any]] = []
        for raw in refs:
            canonical = self._ref_from_anything(raw)
            if canonical is None:
                _LOGGER.warning("playlist.addItems skip invalid ref %r", raw)
                continue
            entries.append({"ref": canonical})
        if not entries:
            return False
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.addItems",
            {"playlistId": playlist_id, "entries": entries},
        )
        return reply is not None and reply.get("type") == "ack"

    async def async_playlist_remove_item(self, playlist_id: str, entry_id: str) -> bool:
        """Remove an item from a playlist."""
        if self._playlist_server is None:
            return False
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.removeItems",
            {"playlistId": playlist_id, "entryIds": [entry_id]},
        )
        return reply is not None and reply.get("type") == "ack"

    async def async_playlist_replace_items(
        self, playlist_id: str, entries: list[dict[str, Any]]
    ) -> bool:
        """Replace all items in a playlist with the given queue-shape entries."""
        if self._playlist_server is None:
            return False
        normalized: list[dict[str, Any]] = []
        for raw in entries:
            entry = self._normalize_queue_entry_for_save(raw)
            if entry is not None:
                normalized.append(entry)
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.replaceItems",
            {"playlistId": playlist_id, "entries": normalized},
        )
        if reply is not None and reply.get("type") == "ack":
            if playlist_id in self._playlists:
                self._playlists[playlist_id]["size"] = len(normalized)
            return True
        return False

    async def async_playlist_move(
        self, playlist_id: str, from_index: int, to_index: int, if_revision: int | None = None
    ) -> bool:
        """Move playlist entry from one index to another."""
        if self._playlist_server is None:
            return False
        body: dict[str, Any] = {
            "playlistId": playlist_id,
            "fromIndex": from_index,
            "toIndex": to_index,
        }
        if if_revision is not None:
            body["ifRevision"] = if_revision
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.move",
            body,
        )
        return reply is not None and reply.get("type") == "ack"

    async def async_shuffle(self, node_id: str, shuffle: bool) -> None:
        if shuffle:
            await self._send_renderer_command(node_id, "queue.shuffle", {"seed": int(time.time())})
            return
        await self._send_renderer_command(node_id, "queue.setShuffle", {"shuffle": False})

    async def async_repeat(self, node_id: str, repeat: bool) -> None:
        mode = "all" if repeat else "off"
        await self._send_renderer_command(node_id, "queue.setRepeat", {"repeat": repeat, "mode": mode})

    async def async_repeat_mode(self, node_id: str, mode: str) -> None:
        mode = (mode or "").lower()
        repeat = mode in {"all", "one", "single", "on", "true"}
        await self._send_renderer_command(
            node_id,
            "queue.setRepeat",
            {"repeat": repeat, "mode": mode},
        )

    async def async_play_media(self, node_id: str, media_id: Any) -> None:
        if isinstance(media_id, dict):
            entries = await self._resolve_media_entries(media_id)
            await self._queue_set_and_fill(node_id, entries)
            return
        text = str(media_id)
        if text.startswith("playlist:"):
            playlist_id = text[len("playlist:") :]
            if "?page=" in playlist_id:
                playlist_id = playlist_id.split("?page=", 1)[0]
            playlist_id = playlist_id.strip()
            if playlist_id:
                await self.async_load_playlist(node_id, playlist_id)
            return
        if text.startswith("snapshot:"):
            snapshot_id = text[len("snapshot:") :]
            if "?page=" in snapshot_id:
                snapshot_id = snapshot_id.split("?page=", 1)[0]
            snapshot_id = snapshot_id.strip()
            if snapshot_id:
                await self.async_load_snapshot(node_id, snapshot_id)
            return
        if text.startswith("library:"):
            await self.async_play_library_container(node_id, text)
            return
        entries = await self._resolve_media_entries(text)
        await self._queue_set_and_fill(node_id, entries)

    async def async_fetch_playlist(self, playlist_id: str) -> dict[str, Any] | None:
        if self._playlist_server is None:
            return None
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.get",
            {"playlistId": playlist_id},
        )
        if reply is None or reply.get("type") != "ack":
            return None
        body = reply.get("body") or {}
        return body

    async def async_list_snapshots(self) -> list[tuple[str, str]]:
        if self._playlist_server is None:
            return []
        reply = await self._request(
            self._playlist_server["nodeId"],
            "snapshot.list",
            {"owner": self.identity},
        )
        if reply is None or reply.get("type") != "ack":
            return []
        body = reply.get("body") or {}
        snapshots = body.get("snapshots") or []
        items = []
        for snap in snapshots:
            snapshot_id = snap.get("snapshotId")
            name = snap.get("name") or snapshot_id
            if snapshot_id:
                items.append((snapshot_id, name))
        items.sort(key=lambda item: item[1].lower())
        return items

    async def async_get_snapshot(self, snapshot_id: str) -> dict[str, Any] | None:
        if self._playlist_server is None or not snapshot_id:
            return None
        reply = await self._request(
            self._playlist_server["nodeId"],
            "snapshot.get",
            {"snapshotId": snapshot_id},
        )
        if reply is None or reply.get("type") != "ack":
            return None
        return reply.get("body") or {}

    async def async_remove_snapshot(self, snapshot_id: str) -> bool:
        if self._playlist_server is None or not snapshot_id:
            return False
        reply = await self._request(
            self._playlist_server["nodeId"],
            "snapshot.remove",
            {"snapshotId": snapshot_id},
        )
        return bool(reply and reply.get("type") == "ack")

    async def async_load_snapshot(self, renderer_id: str, snapshot_id: str) -> None:
        # Fetch + hydrate client-side rather than going through queue.loadSnapshot,
        # so legacy snapshots that lack `display` get backfilled here. The
        # renderer's own queue.loadSnapshot handler resolves sources but cannot
        # fetch catalog metadata, which is the controller's responsibility.
        if self._playlist_server is None:
            return
        snapshot = await self._request(
            self._playlist_server["nodeId"],
            "snapshot.get",
            {"snapshotId": snapshot_id},
            need_lease=False,
        )
        if snapshot is None or snapshot.get("type") != "ack":
            return
        body = snapshot.get("body") or {}
        entries = body.get("entries") or []
        playable: list[dict[str, Any]] = []
        for entry in entries:
            normalized = self._normalize_queue_entry_for_save(entry)
            if normalized is not None:
                playable.append(normalized)
        playable = await self._hydrate_entries(playable)
        await self._queue_set_and_fill_from_playlist_entries(renderer_id, playable)

    async def async_save_snapshot(self, renderer_id: str, name: str) -> bool:
        name = (name or "").strip()
        if self._playlist_server is None or not name:
            _LOGGER.warning("snapshot save skipped: name required")
            return False
        snapshots = await self.async_list_snapshots()
        for _, existing in snapshots:
            if existing.lower() == name.lower():
                _LOGGER.warning("snapshot save skipped: name already exists")
                return False
        state = self.get_renderer_state(renderer_id)
        queue = state.get("queue") or {}
        playback = state.get("playback") or {}
        session = state.get("session") or {}
        entries = await self.async_get_queue(renderer_id)
        snapshot_entries: list[dict[str, Any]] = []
        for entry in entries:
            normalized = self._normalize_queue_entry_for_save(entry)
            if normalized is not None:
                snapshot_entries.append(normalized)
        capture = {
            "queueRevision": queue.get("revision", 0),
            "index": queue.get("index", 0),
            "positionMs": playback.get("positionMs", 0),
            "repeat": queue.get("repeat", False),
            "repeatMode": queue.get("repeatMode", ""),
            "shuffle": queue.get("shuffle", False),
        }
        reply = await self._request(
            self._playlist_server["nodeId"],
            "snapshot.save",
            {
                "name": name,
                "rendererId": renderer_id,
                "sessionId": session.get("id"),
                "capture": capture,
                "entries": snapshot_entries,
            },
        )
        return bool(reply and reply.get("type") == "ack")

    @staticmethod
    def _normalize_queue_entry_for_save(entry: dict[str, Any]) -> dict[str, Any] | None:
        """Strip a runtime queue entry down to the persisted-entry shape."""
        if not isinstance(entry, dict):
            return None
        out: dict[str, Any] = {}
        ref = entry.get("ref")
        if isinstance(ref, dict) and ref.get("libraryId") and ref.get("itemId"):
            out["ref"] = {
                "kind": ref.get("kind") or "libraryItem",
                "libraryId": ref["libraryId"],
                "itemId": ref["itemId"],
            }
        resolved = entry.get("resolved")
        if isinstance(resolved, dict) and resolved.get("url"):
            out["resolved"] = resolved
        display = entry.get("display")
        if isinstance(display, dict) and display:
            out["display"] = display
        if not out.get("ref") and not out.get("resolved"):
            return None
        return out

    async def async_browse_library(
        self, library_id: str, container_id: str, start: int, count: int
    ) -> dict[str, Any] | None:
        if not library_id:
            return None
        body = {"containerId": container_id, "start": start, "count": count}
        reply = await self._request(
            library_id,
            "library.browse",
            body,
            need_lease=False,
        )
        if reply is None or reply.get("type") != "ack":
            return None
        return reply.get("body") or {}

    async def async_search_library(
        self, library_id: str, query: str, start: int = 0, count: int = 50
    ) -> dict[str, Any] | None:
        if not library_id or not query:
            return None
        body = {"query": query, "start": start, "count": count}
        reply = await self._request(
            library_id,
            "library.search",
            body,
            need_lease=False,
        )
        if reply is None or reply.get("type") != "ack":
            return None
        return reply.get("body") or {}

    async def async_play_library_container(self, node_id: str, media_id: str) -> None:
        library_id, container_id, page = self._parse_library_media_id(media_id)
        if not library_id:
            return
        if page <= 0:
            page = 1
        page_size = 200
        start = (page - 1) * page_size
        payload = await self.async_browse_library(library_id, container_id, start, page_size)
        if not payload:
            return
        items = payload.get("items") or []
        item_ids: list[str] = []
        for item in items:
            item_id = item.get("itemId")
            if not item_id:
                continue
            media_type = (item.get("mediaType") or "").lower()
            item_type = (item.get("type") or "").lower()
            playable = media_type in {"audio", "video"} or item_type in {
                "audio",
                "video",
                "movie",
                "episode",
                "musicvideo",
            }
            if playable:
                item_ids.append(item_id)
        if not item_ids:
            return
        await self._queue_set_and_fill_from_library_items(node_id, library_id, item_ids)

    def _parse_library_media_id(self, media_id: str) -> tuple[str, str, int]:
        rest = media_id[len("library:") :]
        node_id = rest
        container_id = ""
        page = 1
        if "?" in rest:
            node_id, query = rest.split("?", 1)
            params = dict(pair.split("=", 1) for pair in query.split("&") if "=" in pair)
            container_id = params.get("container", "")
            try:
                page = max(1, int(params.get("page", "1")))
            except ValueError:
                page = 1
        return node_id.strip(), container_id, page

    async def async_fetch_displays(
        self, refs: list[dict[str, Any]]
    ) -> dict[str, dict[str, Any]]:
        """Slow-path display backfill keyed by `<libraryId>|<itemId>`."""
        return await self.async_get_item_displays(refs)

    async def _hydrate_entries(
        self, entries: list[dict[str, Any]]
    ) -> list[dict[str, Any]]:
        """Backfill `display` and `resolved` on queue entries that have only `ref`.

        Renderers preserve display when supplied but cannot fetch it themselves
        (per the protocol contract).  `playCurrent` also needs a resolved source
        URL.  Doing this once at the controller before queue.set/add means the
        renderer never sees a ref-only entry on its hot path.
        """
        if not entries:
            return entries
        refs_for_display: list[dict[str, Any]] = []
        refs_for_sources: list[dict[str, Any]] = []
        for entry in entries:
            ref = entry.get("ref") if isinstance(entry, dict) else None
            if not isinstance(ref, dict):
                continue
            canonical = self._ref_from_anything(ref)
            if canonical is None:
                continue
            if not entry.get("display"):
                refs_for_display.append(canonical)
            if not entry.get("resolved"):
                refs_for_sources.append(canonical)
        if not refs_for_display and not refs_for_sources:
            return entries
        displays_task = (
            self.async_get_item_displays(refs_for_display)
            if refs_for_display
            else None
        )
        sources_task = (
            self._resolve_sources_for_refs(refs_for_sources)
            if refs_for_sources
            else None
        )
        displays: dict[str, dict[str, Any]] = {}
        sources: dict[str, list[dict[str, Any]]] = {}
        if displays_task and sources_task:
            displays, sources = await asyncio.gather(displays_task, sources_task)
        elif displays_task:
            displays = await displays_task
        elif sources_task:
            sources = await sources_task
        out: list[dict[str, Any]] = []
        for entry in entries:
            if not isinstance(entry, dict):
                continue
            ref = entry.get("ref")
            if not isinstance(ref, dict):
                out.append(entry)
                continue
            canonical = self._ref_from_anything(ref)
            if canonical is None:
                out.append(entry)
                continue
            key = self._ref_cache_key(canonical)
            merged = dict(entry)
            if not merged.get("display") and key in displays:
                merged["display"] = displays[key]
            if not merged.get("resolved") and key in sources and sources[key]:
                merged["resolved"] = sources[key][0]
            out.append(merged)
        return out

    async def async_load_playlist(
        self, renderer_id: str, playlist_id: str, mode: str = "replace", resolve: str = "auto"
    ) -> None:
        if self._playlist_server is None:
            return
        if mode != "replace":
            body = {
                "playlistServerId": self._playlist_server["nodeId"],
                "playlistId": playlist_id,
                "mode": mode,
                "resolve": resolve,
            }
            reply = await self._request_with_lease(
                renderer_id,
                "queue.loadPlaylist",
                body,
                timeout_seconds=RENDERER_LOAD_TIMEOUT_SECONDS,
            )
            if reply is None or reply.get("type") != "ack":
                return
            await self._send_renderer_command(renderer_id, "playback.play", {})
            return
        playlist = await self._request(
            self._playlist_server["nodeId"],
            "playlist.get",
            {"playlistId": playlist_id},
            need_lease=False,
        )
        if playlist is None or playlist.get("type") != "ack":
            return
        entries = (playlist.get("body") or {}).get("entries") or []
        playable_entries: list[dict[str, Any]] = []
        for entry in entries:
            normalized = self._normalize_queue_entry_for_save(entry)
            if normalized is not None:
                playable_entries.append(normalized)
        playable_entries = await self._hydrate_entries(playable_entries)
        await self._queue_set_and_fill_from_playlist_entries(renderer_id, playable_entries)

    async def _queue_set_and_fill(self, node_id: str, entries: list[dict[str, Any]]) -> None:
        if not entries:
            return
        first = entries[:1]
        reply = await self._request_with_lease(
            node_id, "queue.set", {"startIndex": 0, "entries": first}
        )
        if reply is None or reply.get("type") != "ack":
            return
        await self._send_renderer_command(node_id, "playback.play", {"index": 0})
        if len(entries) > 1:
            self.hass.async_create_task(self._queue_add_entries(node_id, entries[1:]))

    async def _queue_add_entries(self, node_id: str, entries: list[dict[str, Any]]) -> None:
        chunk_size = 50
        for start in range(0, len(entries), chunk_size):
            chunk = entries[start : start + chunk_size]
            chunk = await self._hydrate_entries(chunk)
            reply = await self._request_with_lease(
                node_id, "queue.add", {"position": "end", "entries": chunk}
            )
            if reply is None or reply.get("type") != "ack":
                _LOGGER.warning("queue.add failed for %s", node_id)
                return

    async def _queue_set_and_fill_from_library_items(
        self, node_id: str, library_id: str, item_ids: list[str]
    ) -> None:
        if not item_ids:
            return
        refs = [self._make_library_ref(library_id, item_id) for item_id in item_ids]
        await self._queue_set_and_fill_from_refs(node_id, refs)

    async def _queue_set_and_fill_from_refs(
        self, node_id: str, refs: list[dict[str, Any]]
    ) -> None:
        if not refs:
            return
        first_entries = await self._entries_for_refs(refs[:1])
        if not first_entries:
            return
        reply = await self._request_with_lease(
            node_id, "queue.set", {"startIndex": 0, "entries": first_entries}
        )
        if reply is None or reply.get("type") != "ack":
            return
        await self._send_renderer_command(node_id, "playback.play", {"index": 0})
        if len(refs) > 1:
            self.hass.async_create_task(self._queue_add_refs(node_id, refs[1:]))

    async def _queue_set_and_fill_from_playlist_entries(
        self, node_id: str, entries: list[dict[str, Any]]
    ) -> None:
        if not entries:
            return
        reply = await self._request_with_lease(
            node_id, "queue.set", {"startIndex": 0, "entries": entries[:1]}
        )
        if reply is None or reply.get("type") != "ack":
            return
        await self._send_renderer_command(node_id, "playback.play", {"index": 0})
        if len(entries) > 1:
            self.hass.async_create_task(
                self._queue_add_entries(node_id, entries[1:])
            )

    async def _queue_add_refs(
        self, node_id: str, refs: list[dict[str, Any]]
    ) -> None:
        chunk_size = 50
        for start in range(0, len(refs), chunk_size):
            chunk = refs[start : start + chunk_size]
            try:
                entries = await self._entries_for_refs(chunk)
                if not entries:
                    _LOGGER.warning(
                        "queue_add_refs: no entries resolved for chunk start=%d count=%d",
                        start,
                        len(chunk),
                    )
                    continue
                reply = await self._request_with_lease(
                    node_id, "queue.add", {"position": "end", "entries": entries}
                )
                if reply is None or reply.get("type") != "ack":
                    _LOGGER.warning("queue.add failed for %s", node_id)
            except Exception as e:
                _LOGGER.exception("queue_add_refs failed: %s", e)

    async def _entries_for_refs(
        self, refs: list[dict[str, Any]]
    ) -> list[dict[str, Any]]:
        if not refs:
            return []
        canonical: list[dict[str, str]] = []
        for raw in refs:
            ref = self._ref_from_anything(raw)
            if ref is not None:
                canonical.append(ref)
        sources_by_key, displays_by_key = await asyncio.gather(
            self._resolve_sources_for_refs(canonical),
            self.async_get_item_displays(canonical),
        )
        entries: list[dict[str, Any]] = []
        for ref in canonical:
            key = self._ref_cache_key(ref)
            if not key:
                continue
            sources = sources_by_key.get(key)
            if not sources:
                continue
            entry: dict[str, Any] = {"ref": ref, "resolved": sources[0]}
            display = displays_by_key.get(key)
            if display:
                entry["display"] = display
            entries.append(entry)
        return entries

    async def async_acquire_lease(self, node_id: str) -> bool:
        return await self._acquire_lease(node_id) is not None

    async def async_renew_lease(self, node_id: str) -> bool:
        lease = self._leases.get(node_id)
        if lease is None:
            return await self._acquire_lease(node_id) is not None
        return await self._renew_lease(node_id, lease) is not None

    async def async_release_lease(self, node_id: str) -> bool:
        lease = self._leases.get(node_id)
        if lease is None:
            return False
        reply = await self._request(
            node_id,
            "session.release",
            {},
            need_lease=True,
            lease=lease,
        )
        if reply and reply.get("type") == "ack":
            self._leases.pop(node_id, None)
            return True
        return False


    INTERNAL_ITEM_PREFIX = "mu:item:"

    def _parse_internal_item_ref(
        self, media_id: str
    ) -> tuple[str | None, str | None]:
        """Parse the internal browse-tree content_id `mu:item:<libId>:<itemId>`.

        This is NOT a wire format and not user-facing — it is only emitted by
        the integration's own BrowseMedia tree.  The library ID here uses the
        canonical `mu:library:<provider>:<namespace>:<resource>` form, so we
        slice off the first six colon parts.
        """
        if not media_id.startswith(self.INTERNAL_ITEM_PREFIX):
            return None, None
        rest = media_id[len(self.INTERNAL_ITEM_PREFIX):]
        parts = rest.split(":", 5)
        if len(parts) < 6:
            return None, None
        library_id = ":".join(parts[:5])
        item_id = parts[5]
        if not library_id or not item_id:
            return None, None
        return library_id, item_id

    async def _resolve_media_entries(self, media_id: Any) -> list[dict[str, Any]]:
        # Structured ref dict — preferred path from new callers.
        if isinstance(media_id, dict):
            ref = self._ref_from_anything(media_id)
            if ref is None:
                return []
            return await self._entries_for_refs([ref])
        text = str(media_id).strip()
        if text.startswith("http://") or text.startswith("https://"):
            return [{"resolved": {"url": text, "byteRange": False}}]
        if text.startswith(self.INTERNAL_ITEM_PREFIX):
            library_id, item_id = self._parse_internal_item_ref(text)
            if not library_id or not item_id:
                return []
            ref = self._make_library_ref(library_id, item_id)
            sources = await self._resolve_sources_for_ref(ref)
            if not sources:
                return []
            distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
            is_container = len(distinct_ids) > 1
            if is_container:
                entries: list[dict[str, Any]] = []
                for source in sources:
                    sid = source.get("itemId") or item_id
                    entries.append({
                        "ref": self._make_library_ref(library_id, sid),
                        "resolved": source,
                    })
                return entries
            source = sources[0]
            sid = source.get("itemId") or item_id
            return [{
                "ref": self._make_library_ref(library_id, sid),
                "resolved": source,
            }]
        return []

    def _resolve_renderer(self, selector: str) -> str | None:
        selector = selector.strip()
        if selector in self._renderers:
            return selector
        matches = [
            rid
            for rid, info in self._renderers.items()
            if info.get("name", "").lower() == selector.lower()
        ]
        if len(matches) == 1:
            return matches[0]
        return None

    def _resolve_library(self, selector: str) -> str | None:
        selector = selector.strip()
        if selector.startswith("mu:library:"):
            return selector
        matches = [
            node_id
            for node_id, info in self._libraries.items()
            if info.get("name", "").lower() == selector.lower()
        ]
        if len(matches) == 1:
            return matches[0]
        return None

    async def _resolve_playlist_id(self, selector: str) -> str | None:
        if self._playlist_server is None:
            return None
        selector = selector.strip()
        if selector.startswith("mu:"):
            return selector
        for snap in self._playlists.values():
            if snap.get("name", "").lower() == selector.lower():
                return snap.get("playlistId")
        await self._refresh_playlists()
        for snap in self._playlists.values():
            if snap.get("name", "").lower() == selector.lower():
                return snap.get("playlistId")
        return None

    async def _refresh_playlists(self) -> None:
        if self._playlist_server is None:
            return
        if getattr(self, "_refreshing_playlists", False):
            return
        self._refreshing_playlists = True
        try:
            await self._do_refresh_playlists()
        finally:
            self._refreshing_playlists = False

    async def _do_refresh_playlists(self) -> None:
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.list",
            {"owner": self.identity},
        )
        if reply is None or reply.get("type") != "ack":
            # Try to switch to another playlist server
            await self._try_switch_playlist_server()
            return
        body = reply.get("body") or {}
        playlists = body.get("playlists") or []
        for pl in playlists:
            playlist_id = pl.get("playlistId")
            if not playlist_id:
                continue
            if pl.get("size") is None:
                playlist = await self.async_fetch_playlist(playlist_id)
                if playlist:
                    entries = playlist.get("entries") or []
                    pl["size"] = len(entries)
            self._playlists[playlist_id] = pl
            await self._ensure_playlist_discovery(playlist_id, pl)

    async def _try_switch_playlist_server(self) -> None:
        """Try switching to a different playlist server if the current one is unresponsive."""
        current = self._selected_playlist_server
        for node_id in self._playlist_servers:
            if node_id == current:
                continue
            _LOGGER.info("trying playlist server %s (current %s unresponsive)", node_id, current)
            self._selected_playlist_server = node_id
            reply = await self._request(
                node_id, "playlist.list", {"owner": self.identity},
                timeout_seconds=5,
            )
            if reply is not None and reply.get("type") == "ack":
                _LOGGER.info("switched to playlist server %s", node_id)
                return
        # No server responded, restore original
        self._selected_playlist_server = current
        _LOGGER.warning("no playlist servers responding")

    async def _ensure_playlist_discovery(
        self, playlist_id: str, playlist: dict[str, Any]
    ) -> None:
        unique = self._unique_id("playlist", playlist_id)
        state_topic = f"{self.entity_prefix}/playlist/{unique}/state"
        availability_topic = self._playlist_availability_topic(
            self._playlist_server["nodeId"]
        )
        discovery_topic = f"{self.discovery_prefix}/sensor/{unique}/config"
        payload = {
            "name": f"Playlist {playlist.get('name', playlist_id)}",
            "unique_id": unique,
            "state_topic": state_topic,
            "availability_topic": availability_topic,
            "payload_available": "online",
            "payload_not_available": "offline",
            "enabled_by_default": True,
            "suggested_area": "Media Utopia",
            "value_template": "{{ value_json.size }}",
            "json_attributes_topic": state_topic,
            "icon": "mdi:playlist-music",
        }
        await self._publish_discovery(discovery_topic, payload)
        state_payload = {
            "name": playlist.get("name"),
            "playlistId": playlist_id,
            "revision": playlist.get("revision"),
            "size": playlist.get("size"),
        }
        await self._publish(state_topic, state_payload, retain=True)
        self._notify_playlist_listeners(playlist_id)

    async def _send_renderer_command(
        self, node_id: str, cmd_type: str, body: dict[str, Any]
    ) -> None:
        lease = await self._ensure_lease(node_id)
        if lease is None:
            _LOGGER.warning("lease unavailable for renderer %s cmd=%s", node_id, cmd_type)
            return
        _LOGGER.debug("send renderer cmd=%s node=%s body=%s", cmd_type, node_id, body)
        await self._publish_command(node_id, cmd_type, body, lease=lease)

    async def _request_with_lease(
        self,
        node_id: str,
        cmd_type: str,
        body: dict[str, Any],
        timeout_seconds: int | None = None,
    ) -> dict[str, Any] | None:
        lease = await self._ensure_lease(node_id)
        if lease is None:
            _LOGGER.warning("lease unavailable for renderer %s cmd=%s", node_id, cmd_type)
            return None
        reply = await self._request(
            node_id,
            cmd_type,
            body,
            need_lease=True,
            lease=lease,
            timeout_seconds=timeout_seconds,
        )
        if reply is None:
            return None
        if reply.get("type") == "error":
            code = ((reply.get("err") or {}).get("code") or "").upper()
            if code == "LEASE_MISMATCH":
                _LOGGER.info("lease mismatch for %s, reacquiring", node_id)
                lease = await self._acquire_lease(node_id)
                if lease is None:
                    return reply
                return await self._request(
                    node_id,
                    cmd_type,
                    body,
                    need_lease=True,
                    lease=lease,
                    timeout_seconds=timeout_seconds,
                )
        return reply

    async def _ensure_lease(self, node_id: str) -> Lease | None:
        lease = self._leases.get(node_id)
        now = int(time.time())
        if lease is None:
            _LOGGER.debug("no lease for %s, acquiring", node_id)
            return await self._acquire_lease(node_id)
        remaining = lease.expires_at - now
        if remaining >= LEASE_RENEW_THRESHOLD_SECONDS:
            return lease
        # If the lease already expired, skip the renew (it will fail with
        # LEASE_MISMATCH anyway) and go straight to acquire.
        if remaining <= 0:
            _LOGGER.debug("lease for %s already expired, reacquiring", node_id)
            return await self._acquire_lease(node_id)
        _LOGGER.debug("lease for %s expires in %ds, renewing", node_id, remaining)
        return await self._renew_lease(node_id, lease)

    async def _acquire_lease(self, node_id: str) -> Lease | None:
        reply = await self._request(
            node_id,
            "session.acquire",
            {"ttlMs": LEASE_TTL_MS},
            need_lease=False,
        )
        if reply is None or reply.get("type") != "ack":
            _LOGGER.warning("lease acquire failed for %s reply=%s", node_id, reply)
            return None
        body = reply.get("body") or {}
        session = body.get("session") or {}
        lease = Lease(
            session_id=session.get("id"),
            token=session.get("token"),
            expires_at=session.get("leaseExpiresAt"),
        )
        self._leases[node_id] = lease
        return lease

    async def _renew_lease(self, node_id: str, lease: Lease) -> Lease | None:
        reply = await self._request(
            node_id,
            "session.renew",
            {"ttlMs": LEASE_TTL_MS},
            need_lease=True,
            lease=lease,
        )
        if reply is None:
            # Timeout — the renderer may be blocked.  Discard the stale
            # lease so the next attempt reacquires rather than sending
            # commands with an expired token.
            _LOGGER.warning("lease renew timed out for %s, discarding lease", node_id)
            self._leases.pop(node_id, None)
            return None
        if reply.get("type") == "error":
            code = ((reply.get("err") or {}).get("code") or "").upper()
            if code == "LEASE_MISMATCH":
                _LOGGER.info("lease mismatch for %s, reacquiring", node_id)
                return await self._acquire_lease(node_id)
            _LOGGER.warning("lease renew failed for %s reply=%s", node_id, reply)
            # Unknown error — discard potentially-expired lease.
            self._leases.pop(node_id, None)
            return None
        if reply.get("type") != "ack":
            _LOGGER.warning("lease renew unexpected reply for %s reply=%s", node_id, reply)
            self._leases.pop(node_id, None)
            return None
        body = reply.get("body") or {}
        session = body.get("session") or {}
        lease.expires_at = session.get("leaseExpiresAt", lease.expires_at)
        return lease

    async def _request(
        self,
        node_id: str,
        cmd_type: str,
        body: dict[str, Any],
        need_lease: bool = False,
        lease: Lease | None = None,
        timeout_seconds: int | None = None,
    ) -> dict[str, Any] | None:
        async with self._request_sema:
            cmd_id = uuid.uuid4().hex
            future = self.hass.loop.create_future()
            self._pending[cmd_id] = future
            await self._publish_command(node_id, cmd_type, body, cmd_id, need_lease, lease)
            try:
                timeout = timeout_seconds or REPLY_TIMEOUT_SECONDS
                payload = await asyncio.wait_for(future, timeout=timeout)
                return payload
            except asyncio.TimeoutError:
                _LOGGER.warning("command timeout node=%s type=%s", node_id, cmd_type)
                return None
            finally:
                self._pending.pop(cmd_id, None)

    async def _publish_command(
        self,
        node_id: str,
        cmd_type: str,
        body: dict[str, Any],
        cmd_id: str | None = None,
        need_lease: bool = True,
        lease: Lease | None = None,
    ) -> None:
        cmd_id = cmd_id or uuid.uuid4().hex
        payload = {
            "id": cmd_id,
            "type": cmd_type,
            "ts": int(time.time()),
            "from": self.identity,
            "replyTo": self.reply_topic,
            "body": body,
        }
        if need_lease:
            if lease is None:
                return
            payload["lease"] = {
                "sessionId": lease.session_id,
                "token": lease.token,
            }
        topic = f"{self.topic_base}/node/{node_id}/cmd"
        _LOGGER.debug(
            "publish cmd_id=%s type=%s node=%s entries=%d",
            cmd_id,
            cmd_type,
            node_id.split(":")[-1],
            len(body.get("entries", [])),
        )
        await self._publish(topic, payload, retain=False)

    async def _publish(self, topic: str, payload: Any, retain: bool) -> None:
        if isinstance(payload, (dict, list)):
            payload = json.dumps(payload)
        # Use QoS 0 for commands (non-retained) to prevent MQTT redelivery
        # causing duplicate processing. Commands have timeout-based retry.
        # Use QoS 1 only for retained messages (discovery, state) where
        # guaranteed delivery matters and duplicates are idempotent.
        qos = 1 if retain else 0
        await mqtt.async_publish(self.hass, topic, payload, qos=qos, retain=retain)

    async def _publish_discovery(self, topic: str, payload: Any) -> None:
        self._discovery_topics.add(topic)
        await self._save_discovery_topics()
        await self._publish(topic, payload, retain=True)

    def _unique_id(self, kind: str, node_id: str) -> str:
        safe = node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_{kind}_{safe}"

    def _renderer_topics_for(self, node_id: str, unique: str) -> dict[str, str]:
        base = f"{self.entity_prefix}/renderer/{unique}"
        return {
            "state": f"{base}/state",
            "availability": f"{base}/availability",
            "command": f"{base}/command",
            "volume": f"{base}/volume",
            "mute": f"{base}/mute",
            "seek": f"{base}/seek",
            "shuffle": f"{base}/shuffle",
            "repeat": f"{base}/repeat",
            "play_media": f"{base}/play_media",
        }

    def _playlist_availability_topic(self, node_id: str) -> str:
        safe = node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"{self.entity_prefix}/playlist_server/{safe}/availability"

    def _node_id_from_topic(self, topic: str) -> str | None:
        parts = topic.split("/")
        if len(parts) >= 4 and parts[2] == "node":
            return parts[3]
        for node_id, topics in self._renderer_topics.items():
            if topic in topics.values():
                return node_id
        return None

