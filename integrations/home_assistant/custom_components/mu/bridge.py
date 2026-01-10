"""MQTT bridge for Mud."""

from __future__ import annotations

import asyncio
import json
import logging
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
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
        self._unsubscribes: list[callable] = []
        self._playlist_task: asyncio.Task | None = None
        self._discovery_topics: set[str] = set()
        self._discovery_store = Store(hass, 1, f"{DOMAIN}_discovery_topics")

        self._renderers: dict[str, dict[str, Any]] = {}
        self._renderer_topics: dict[str, dict[str, str]] = {}
        self._renderer_listeners: list[callable] = []
        self._renderer_state_listeners: list[callable] = []
        self._metadata_cache: dict[str, dict[str, Any]] = {}
        self._metadata_inflight: set[str] = set()
        self._metadata_failures: dict[str, float] = {}
        self._request_sema = asyncio.Semaphore(4)
        self._metadata_sema = asyncio.Semaphore(2)
        self._libraries: dict[str, dict[str, Any]] = {}
        self._playlist_server: dict[str, Any] | None = None
        self._playlists: dict[str, dict[str, Any]] = {}
        self._leases: dict[str, Lease] = {}
        self._snapshot_names: dict[str, str] = {}
        self._playlist_listeners: list[callable] = []
        self._image_proxy_url = _image_proxy_url

    async def async_start(self) -> None:
        """Start the bridge."""
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

        self._playlist_task = asyncio.create_task(self._playlist_loop())
        _LOGGER.debug("mu bridge started with topic_base=%s", self.topic_base)

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
                return
            renderer_id = self._resolve_renderer(renderer)
            if renderer_id is None:
                return
            playlist_id = await self._resolve_playlist_id(playlist)
            if playlist_id is None:
                return
            body = {
                "playlistServerId": self._playlist_server["nodeId"],
                "playlistId": playlist_id,
                "mode": mode,
                "resolve": resolve,
            }
            await self.async_load_playlist(renderer_id, playlist_id, mode, resolve)

        async def _clear_queue(call):
            renderer = call.data.get("renderer")
            if not renderer:
                return
            renderer_id = self._resolve_renderer(renderer)
            if renderer_id is None:
                return
            await self._send_renderer_command(renderer_id, "queue.clear", {})

        async def _shuffle_queue(call):
            renderer = call.data.get("renderer")
            if not renderer:
                return
            renderer_id = self._resolve_renderer(renderer)
            if renderer_id is None:
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

    async def _playlist_loop(self) -> None:
        while True:
            try:
                await asyncio.sleep(self.playlist_refresh)
                if self._playlist_server is not None:
                    await self._refresh_playlists()
            except asyncio.CancelledError:
                return
            except Exception:
                continue

    async def _on_reply(self, msg) -> None:
        try:
            payload = json.loads(msg.payload)
        except Exception:
            return
        cmd_id = payload.get("id")
        if not cmd_id:
            return
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
        elif kind == "playlist":
            self._playlist_server = payload
            await self._publish_playlist_server_availability("online")
            await self._refresh_playlists()

    async def _on_state(self, msg) -> None:
        try:
            payload = json.loads(msg.payload)
        except Exception:
            return
        node_id = self._node_id_from_topic(msg.topic)
        if node_id is None:
            return
        if node_id not in self._renderers:
            return
        _LOGGER.debug("state %s status=%s", node_id, (payload.get("playback") or {}).get("status"))
        self._renderers[node_id]["state"] = payload
        await self._maybe_resolve_metadata(node_id, payload)
        self._notify_renderer_state_listeners(node_id, payload)
        await self._publish_renderer_state(node_id, payload)

    def register_renderer_listener(self, callback) -> None:
        self._renderer_listeners.append(callback)
        for node_id in list(self._renderers.keys()):
            self._notify_renderer_listener(callback, node_id)

    def register_renderer_state_listener(self, callback) -> None:
        self._renderer_state_listeners.append(callback)

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

    async def _maybe_resolve_metadata(self, node_id: str, payload: dict[str, Any]) -> None:
        current = payload.get("current") or {}
        item_id = current.get("itemId")
        if not item_id or not str(item_id).startswith("lib:"):
            return
        metadata = current.get("metadata") or {}
        if metadata.get("title") and (
            metadata.get("artist")
            or metadata.get("album")
            or metadata.get("artworkUrl")
        ):
            return
        cached = self._metadata_cache.get(item_id)
        if cached:
            current["metadata"] = cached
            return
        if item_id in self._metadata_inflight:
            return
        self._metadata_inflight.add(item_id)
        self.hass.async_create_task(self._resolve_metadata(node_id, item_id))

    async def _resolve_metadata(self, node_id: str, item_id: str) -> None:
        try:
            metadata = await self._fetch_metadata(item_id)
            if not metadata:
                return
            state = self._renderers.get(node_id, {}).get("state")
            if not state:
                return
            current = state.get("current") or {}
            current["metadata"] = metadata
            state["current"] = current
            self._renderers[node_id]["state"] = state
            self._notify_renderer_state_listeners(node_id, state)
        finally:
            self._metadata_inflight.discard(item_id)

    async def _fetch_metadata(self, item_id: str) -> dict[str, Any]:
        cached = self._metadata_cache.get(item_id)
        if cached:
            return self._normalize_metadata(cached)
        last_fail = self._metadata_failures.get(item_id)
        if last_fail and time.time()-last_fail < 60:
            return {}
        library_id, raw_id = self._split_lib_ref(item_id)
        if not library_id or not raw_id:
            return {}
        body = {"itemId": raw_id, "metadataOnly": True}
        async with self._metadata_sema:
            reply = await self._request(
                library_id,
                "library.resolve",
                body,
                need_lease=False,
                timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
            )
        if reply is None or reply.get("type") != "ack":
            _LOGGER.debug("resolve metadata failed %s reply=%s", item_id, reply)
            self._metadata_failures[item_id] = time.time()
            return {}
        metadata = (reply.get("body") or {}).get("metadata") or {}
        if metadata:
            metadata = self._normalize_metadata(metadata)
            self._metadata_cache[item_id] = metadata
        return metadata

    async def _fetch_metadata_batch(
        self, item_ids: list[str]
    ) -> dict[str, dict[str, Any]]:
        results: dict[str, dict[str, Any]] = {}
        if not item_ids:
            return results
        groups: dict[str, list[str]] = {}
        ref_map: dict[str, dict[str, str]] = {}
        for ref in item_ids:
            cached = self._metadata_cache.get(ref)
            if cached:
                results[ref] = self._normalize_metadata(cached)
                continue
            last_fail = self._metadata_failures.get(ref)
            if last_fail and time.time()-last_fail < 60:
                continue
            library_id, raw_id = self._split_lib_ref(ref)
            if not library_id or not raw_id:
                continue
            groups.setdefault(library_id, []).append(raw_id)
            ref_map.setdefault(library_id, {})[raw_id] = ref

        for library_id, raw_ids in groups.items():
            async with self._metadata_sema:
                reply = await self._request(
                    library_id,
                    "library.resolveBatch",
                    {"itemIds": raw_ids, "metadataOnly": True},
                    need_lease=False,
                    timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
                )
            if reply is None or reply.get("type") != "ack":
                err = (reply or {}).get("err") or {}
                message = str(err.get("message") or "")
                if "unsupported command" in message.lower():
                    for raw_id in raw_ids:
                        ref = ref_map[library_id].get(raw_id)
                        if not ref:
                            continue
                        meta = await self._fetch_metadata(ref)
                        if meta:
                            results[ref] = meta
                    continue
                for raw_id in raw_ids:
                    ref = ref_map[library_id].get(raw_id)
                    if ref:
                        self._metadata_failures[ref] = time.time()
                continue
            items = (reply.get("body") or {}).get("items") or []
            for item in items:
                raw_id = item.get("itemId")
                metadata = item.get("metadata") or {}
                if not raw_id or not metadata:
                    continue
                ref = ref_map[library_id].get(raw_id)
                if not ref:
                    continue
                metadata = self._normalize_metadata(metadata)
                self._metadata_cache[ref] = metadata
                results[ref] = metadata
        return results

    async def async_fetch_metadata_fresh(
        self, item_ids: list[str]
    ) -> dict[str, dict[str, Any]]:
        """Fetch metadata for items, bypassing cache (for queue browser)."""
        results: dict[str, dict[str, Any]] = {}
        if not item_ids:
            return results
        groups: dict[str, list[str]] = {}
        ref_map: dict[str, dict[str, str]] = {}
        for ref in item_ids:
            library_id, raw_id = self._split_lib_ref(ref)
            if not library_id or not raw_id:
                continue
            groups.setdefault(library_id, []).append(raw_id)
            ref_map.setdefault(library_id, {})[raw_id] = ref

        for library_id, raw_ids in groups.items():
            async with self._metadata_sema:
                reply = await self._request(
                    library_id,
                    "library.resolveBatch",
                    {"itemIds": raw_ids, "metadataOnly": True},
                    need_lease=False,
                    timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
                )
            if reply is None or reply.get("type") != "ack":
                # Fallback to individual requests
                for raw_id in raw_ids:
                    ref = ref_map[library_id].get(raw_id)
                    if not ref:
                        continue
                    body = {"itemId": raw_id, "metadataOnly": True}
                    reply2 = await self._request(
                        library_id,
                        "library.resolve",
                        body,
                        need_lease=False,
                        timeout_seconds=max(10, REPLY_TIMEOUT_SECONDS),
                    )
                    if reply2 and reply2.get("type") == "ack":
                        metadata = (reply2.get("body") or {}).get("metadata") or {}
                        if metadata:
                            results[ref] = self._normalize_metadata(metadata)
                continue
            items = (reply.get("body") or {}).get("items") or []
            for item in items:
                raw_id = item.get("itemId")
                metadata = item.get("metadata") or {}
                if not raw_id or not metadata:
                    continue
                ref = ref_map[library_id].get(raw_id)
                if not ref:
                    continue
                results[ref] = self._normalize_metadata(metadata)
        return results

    def _normalize_metadata(self, metadata: dict[str, Any]) -> dict[str, Any]:
        """Normalize metadata, applying base URL rewriting to artwork.

        Note: This only applies base URL rewriting, not proxying.
        Callers should use rewrite_artwork_url() to apply proxying as needed.
        """
        if not metadata:
            return metadata
        art = metadata.get("artworkUrl")
        if art:
            metadata = dict(metadata)
            # Only apply base URL rewriting, not proxying
            # Callers will apply proxying based on context (internal vs browser)
            metadata["artworkUrl"] = self._rewrite_artwork_base(art)
        return metadata

    async def _resolve_sources_batch(
        self, library_id: str, item_ids: list[str]
    ) -> dict[str, list[dict[str, Any]]]:
        results: dict[str, list[dict[str, Any]]] = {}
        if not item_ids:
            return results
        reply = await self._request(
            library_id,
            "library.resolveBatch",
            {"itemIds": item_ids},
            need_lease=False,
            timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
        )
        if reply is None or reply.get("type") != "ack":
            err = (reply or {}).get("err") or {}
            message = str(err.get("message") or "")
            if "unsupported command" in message.lower():
                for item_id in item_ids:
                    reply = await self._request(
                        library_id,
                        "library.resolve",
                        {"itemId": item_id},
                        need_lease=False,
                        timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
                    )
                    if reply is None or reply.get("type") != "ack":
                        continue
                    sources = (reply.get("body") or {}).get("sources") or []
                    if sources:
                        results[item_id] = sources
            return results
        items = (reply.get("body") or {}).get("items") or []
        for item in items:
            item_id = item.get("itemId")
            sources = item.get("sources") or []
            if item_id and sources:
                results[item_id] = sources
        if results:
            return results
        for item_id in item_ids:
            reply = await self._request(
                library_id,
                "library.resolve",
                {"itemId": item_id},
                need_lease=False,
                timeout_seconds=max(20, REPLY_TIMEOUT_SECONDS),
            )
            if reply is None or reply.get("type") != "ack":
                continue
            sources = (reply.get("body") or {}).get("sources") or []
            if sources:
                results[item_id] = sources
        return results

    async def _publish_renderer_state(self, node_id: str, state: dict[str, Any]) -> None:
        topics = self._renderer_topics.get(node_id)
        if not topics:
            return
        playback = state.get("playback") or {}
        current = state.get("current") or {}
        metadata = current.get("metadata") or {}

        status = playback.get("status") or "idle"
        if status == "stopped":
            status = "idle"

        title = metadata.get("title")
        artist = metadata.get("artist")
        album = metadata.get("album")
        artwork = self.rewrite_artwork_url(metadata.get("artworkUrl"))

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

    async def _publish_playlist_server_availability(self, status: str) -> None:
        if self._playlist_server is None:
            return
        topic = self._playlist_availability_topic(self._playlist_server["nodeId"])
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
        media_id = data.get("media_content_id")
        if not media_id:
            return
        entries = await self._resolve_media_entries(media_id)
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
        """Fetch the current queue entries for a renderer with metadata."""
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
                {"from": from_index, "count": page_size, "resolve": "metadata"},
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

    async def async_play_media(self, node_id: str, media_id: str) -> None:
        if str(media_id).startswith("playlist:"):
            playlist_id = str(media_id)[len("playlist:") :]
            if "?page=" in playlist_id:
                playlist_id = playlist_id.split("?page=", 1)[0]
            playlist_id = playlist_id.strip()
            if playlist_id:
                await self.async_load_playlist(node_id, playlist_id)
            return
        if str(media_id).startswith("snapshot:"):
            snapshot_id = str(media_id)[len("snapshot:") :]
            if "?page=" in snapshot_id:
                snapshot_id = snapshot_id.split("?page=", 1)[0]
            snapshot_id = snapshot_id.strip()
            if snapshot_id:
                await self.async_load_snapshot(node_id, snapshot_id)
            return
        if str(media_id).startswith("library:"):
            await self.async_play_library_container(node_id, str(media_id))
            return
        entries = await self._resolve_media_entries(media_id)
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
        if self._playlist_server is None:
            return
        body = {
            "playlistServerId": self._playlist_server["nodeId"],
            "snapshotId": snapshot_id,
            "mode": "replace",
            "resolve": "auto",
        }
        reply = await self._request_with_lease(
            renderer_id, "queue.loadSnapshot", body, timeout_seconds=RENDERER_LOAD_TIMEOUT_SECONDS
        )
        if reply is None or reply.get("type") != "ack":
            return
        await self._send_renderer_command(renderer_id, "playback.play", {"index": 0})

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
        length = queue.get("length") or 0
        items: list[str] = []
        from_index = 0
        page_size = 100
        while from_index < length:
            reply = await self._request(
                renderer_id,
                "queue.get",
                {"from": from_index, "count": page_size},
                need_lease=False,
            )
            if reply is None or reply.get("type") != "ack":
                break
            body = reply.get("body") or {}
            for entry in body.get("entries") or []:
                item_id = entry.get("itemId")
                if item_id:
                    items.append(item_id)
            from_index += page_size
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
                "items": items,
            },
        )
        return bool(reply and reply.get("type") == "ack")

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

    async def async_fetch_metadata(self, item_id: str) -> dict[str, Any]:
        return await self._fetch_metadata(item_id)

    async def async_fetch_metadata_batch(
        self, item_ids: list[str]
    ) -> dict[str, dict[str, Any]]:
        return await self._fetch_metadata_batch(item_ids)

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
        refs: list[str] = []
        for entry in entries:
            ref_id = ((entry.get("ref") or {}).get("id") or "").strip()
            if ref_id:
                refs.append(ref_id)
        await self._queue_set_and_fill_from_refs(renderer_id, refs)

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
        first_entries = await self._entries_for_library_items(library_id, item_ids[:1])
        if not first_entries:
            return
        reply = await self._request_with_lease(
            node_id, "queue.set", {"startIndex": 0, "entries": first_entries}
        )
        if reply is None or reply.get("type") != "ack":
            return
        await self._send_renderer_command(node_id, "playback.play", {"index": 0})
        if len(item_ids) > 1:
            self.hass.async_create_task(
                self._queue_add_library_items(node_id, library_id, item_ids[1:])
            )

    async def _queue_add_library_items(
        self, node_id: str, library_id: str, item_ids: list[str]
    ) -> None:
        chunk_size = 50
        for start in range(0, len(item_ids), chunk_size):
            chunk = item_ids[start : start + chunk_size]
            entries = await self._entries_for_library_items(library_id, chunk)
            if not entries:
                continue
            reply = await self._request_with_lease(
                node_id, "queue.add", {"position": "end", "entries": entries}
            )
            if reply is None or reply.get("type") != "ack":
                _LOGGER.warning("queue.add failed for %s", node_id)
                return

    async def _queue_set_and_fill_from_refs(self, node_id: str, refs: list[str]) -> None:
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

    async def _queue_add_refs(self, node_id: str, refs: list[str]) -> None:
        chunk_size = 50
        for start in range(0, len(refs), chunk_size):
            chunk = refs[start : start + chunk_size]
            entries = await self._entries_for_refs(chunk)
            if not entries:
                continue
            reply = await self._request_with_lease(
                node_id, "queue.add", {"position": "end", "entries": entries}
            )
            if reply is None or reply.get("type") != "ack":
                _LOGGER.warning("queue.add failed for %s", node_id)
                return

    async def _entries_for_library_items(
        self, library_id: str, item_ids: list[str]
    ) -> list[dict[str, Any]]:
        if not item_ids:
            return []
        sources_map = await self._resolve_sources_batch(library_id, item_ids)
        entries: list[dict[str, Any]] = []
        for item_id in item_ids:
            sources = sources_map.get(item_id)
            if not sources:
                continue
            ref_id = f"lib:{library_id}:{item_id}"
            entries.append({"ref": {"id": ref_id}, "resolved": sources[0]})
        return entries

    async def _entries_for_refs(self, refs: list[str]) -> list[dict[str, Any]]:
        if not refs:
            return []
        order: list[tuple[str, str, str]] = []
        grouped: dict[str, list[str]] = {}
        for ref in refs:
            selector, item_id = self._split_lib_ref(ref)
            if selector is None:
                continue
            library_id = self._resolve_library(selector)
            if library_id is None:
                continue
            order.append((ref, library_id, item_id))
            grouped.setdefault(library_id, []).append(item_id)
        sources_by_library: dict[str, dict[str, list[dict[str, Any]]]] = {}
        for library_id, item_ids in grouped.items():
            sources_by_library[library_id] = await self._resolve_sources_batch(
                library_id, item_ids
            )
        entries: list[dict[str, Any]] = []
        for ref, library_id, item_id in order:
            sources = sources_by_library.get(library_id, {}).get(item_id)
            if not sources:
                continue
            entries.append({"ref": {"id": ref}, "resolved": sources[0]})
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


    async def _resolve_media_entries(self, media_id: str) -> list[dict[str, Any]]:
        media_id = str(media_id).strip()
        if media_id.startswith("http://") or media_id.startswith("https://"):
            return [{"resolved": {"url": media_id, "byteRange": False}}]
        if media_id.startswith("lib:"):
            selector, item_id = self._split_lib_ref(media_id)
            if selector is None:
                return []
            library_id = self._resolve_library(selector)
            if library_id is None:
                return []
            reply = await self._request(
                library_id, "library.resolve", {"itemId": item_id}
            )
            if reply is None or reply.get("type") != "ack":
                return []
            body = reply.get("body") or {}
            sources = body.get("sources") or []
            entries = []
            for source in sources:
                entries.append({"ref": {"id": media_id}, "resolved": source})
            return entries
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
        reply = await self._request(
            self._playlist_server["nodeId"],
            "playlist.list",
            {"owner": self.identity},
        )
        if reply is None or reply.get("type") != "ack":
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
            return await self._acquire_lease(node_id)
        if lease.expires_at-now >= LEASE_RENEW_THRESHOLD_SECONDS:
            return lease
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
            _LOGGER.warning("lease renew failed for %s reply=%s", node_id, reply)
            return lease
        if reply.get("type") == "error":
            code = ((reply.get("err") or {}).get("code") or "").upper()
            if code == "LEASE_MISMATCH":
                _LOGGER.info("lease mismatch for %s, reacquiring", node_id)
                return await self._acquire_lease(node_id)
            _LOGGER.warning("lease renew failed for %s reply=%s", node_id, reply)
            return lease
        if reply.get("type") != "ack":
            _LOGGER.warning("lease renew failed for %s reply=%s", node_id, reply)
            return lease
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
        _LOGGER.debug("publish cmd topic=%s payload=%s", topic, payload)
        await self._publish(topic, payload, retain=False)

    async def _publish(self, topic: str, payload: Any, retain: bool) -> None:
        if isinstance(payload, (dict, list)):
            payload = json.dumps(payload)
        await mqtt.async_publish(self.hass, topic, payload, qos=1, retain=retain)

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

    def _split_lib_ref(self, ref: str) -> tuple[str | None, str | None]:
        """Split a library reference into (library_node_id, item_id).
        
        Format: lib:mu:library:{provider}:{namespace}:{resource}:{item_id}
        Example: lib:mu:library:upnp:mud@office:default:uuid::base64
        Returns: (mu:library:upnp:mud@office:default, uuid::base64)
        """
        if not ref.startswith("lib:"):
            return None, None
        ref = ref[len("lib:") :]
        # Library node IDs have the pattern: mu:library:{provider}:{namespace}:{resource}
        # So we need to find the 5th colon to separate library_id from item_id
        parts = ref.split(":", 5)  # Split into at most 6 parts
        if len(parts) < 6:
            return None, None
        # Rejoin first 5 parts as library_id, 6th part is item_id
        library_id = ":".join(parts[:5])
        item_id = parts[5]
        if not library_id or not item_id:
            return None, None
        return library_id, item_id


