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

from homeassistant.components import mqtt
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.storage import Store
import voluptuous as vol

from .const import (
    CONF_DISCOVERY_PREFIX,
    CONF_ENTITY_PREFIX,
    CONF_IDENTITY,
    CONF_PLAYLIST_REFRESH,
    CONF_TOPIC_BASE,
    DEFAULT_DISCOVERY_PREFIX,
    DEFAULT_ENTITY_PREFIX,
    DEFAULT_IDENTITY,
    DEFAULT_PLAYLIST_REFRESH,
    DEFAULT_TOPIC_BASE,
    DOMAIN,
    LEASE_RENEW_THRESHOLD_SECONDS,
    LEASE_TTL_MS,
    REPLY_TIMEOUT_SECONDS,
)

_LOGGER = logging.getLogger(__name__)


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
        self._libraries: dict[str, dict[str, Any]] = {}
        self._playlist_server: dict[str, Any] | None = None
        self._playlists: dict[str, dict[str, Any]] = {}
        self._leases: dict[str, Lease] = {}
        self._playlist_listeners: list[callable] = []

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
            await self._send_renderer_command(renderer_id, "queue.loadPlaylist", body)

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
        if metadata.get("title") or metadata.get("artworkUrl"):
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
            library_id, raw_id = self._split_lib_ref(item_id)
            if not library_id or not raw_id:
                return
            body = {"itemId": raw_id, "metadataOnly": True}
            reply = await self._request(library_id, "library.resolve", body, need_lease=False)
            if reply is None or reply.get("type") != "ack":
                _LOGGER.debug("resolve metadata failed %s reply=%s", item_id, reply)
                return
            metadata = (reply.get("body") or {}).get("metadata") or {}
            if not metadata:
                return
            self._metadata_cache[item_id] = metadata
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
        artwork = metadata.get("artworkUrl")

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
        repeat = mode in ["all", "one", "on", "true"]
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
        if not entries:
            return
        body = {"startIndex": 0, "entries": entries}
        await self._send_renderer_command(node_id, "queue.set", body)
        await self._send_renderer_command(node_id, "playback.play", {})

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
            await self._send_renderer_command(node_id, "playback.seek", {"positionMs": 0})
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

    async def async_shuffle(self, node_id: str, shuffle: bool) -> None:
        if not shuffle:
            return
        await self._send_renderer_command(node_id, "queue.shuffle", {"seed": int(time.time())})

    async def async_repeat(self, node_id: str, repeat: bool) -> None:
        await self._send_renderer_command(node_id, "queue.setRepeat", {"repeat": repeat})

    async def async_play_media(self, node_id: str, media_id: str) -> None:
        entries = await self._resolve_media_entries(media_id)
        if not entries:
            return
        body = {"startIndex": 0, "entries": entries}
        await self._send_renderer_command(node_id, "queue.set", body)
        await self._send_renderer_command(node_id, "playback.play", {})

    async def async_load_playlist(
        self, renderer_id: str, playlist_id: str, mode: str = "replace", resolve: str = "auto"
    ) -> None:
        if self._playlist_server is None:
            return
        body = {
            "playlistServerId": self._playlist_server["nodeId"],
            "playlistId": playlist_id,
            "mode": mode,
            "resolve": resolve,
        }
        await self._send_renderer_command(renderer_id, "queue.loadPlaylist", body)

    async def async_acquire_lease(self, node_id: str) -> None:
        await self._ensure_lease(node_id)

    async def async_renew_lease(self, node_id: str) -> None:
        lease = self._leases.get(node_id)
        if lease is None:
            await self._ensure_lease(node_id)
            return
        await self._request(
            node_id,
            "session.renew",
            {"ttlMs": LEASE_TTL_MS},
            need_lease=True,
            lease=lease,
        )

    async def async_release_lease(self, node_id: str) -> None:
        lease = self._leases.get(node_id)
        if lease is None:
            return
        await self._request(
            node_id,
            "session.release",
            {},
            need_lease=True,
            lease=lease,
        )
        self._leases.pop(node_id, None)

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
            "value_template": "{{ value_json.name }}",
            "json_attributes_topic": state_topic,
            "icon": "mdi:playlist-music",
        }
        await self._publish_discovery(discovery_topic, payload)
        state_payload = {
            "name": playlist.get("name"),
            "playlistId": playlist_id,
            "revision": playlist.get("revision"),
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

    async def _ensure_lease(self, node_id: str) -> Lease | None:
        lease = self._leases.get(node_id)
        now = int(time.time())
        if lease is None or lease.expires_at - now < LEASE_RENEW_THRESHOLD_SECONDS:
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
        reply = await self._request(
            node_id,
            "session.renew",
            {"ttlMs": LEASE_TTL_MS},
            need_lease=True,
            lease=lease,
        )
        if reply is None or reply.get("type") != "ack":
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
    ) -> dict[str, Any] | None:
        cmd_id = uuid.uuid4().hex
        future = self.hass.loop.create_future()
        self._pending[cmd_id] = future
        await self._publish_command(node_id, cmd_type, body, cmd_id, need_lease, lease)
        try:
            payload = await asyncio.wait_for(future, timeout=REPLY_TIMEOUT_SECONDS)
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
        if not ref.startswith("lib:"):
            return None, None
        ref = ref[len("lib:") :]
        idx = ref.rfind(":")
        if idx <= 0 or idx >= len(ref) - 1:
            return None, None
        return ref[:idx], ref[idx + 1 :]
