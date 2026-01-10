"""Media player entities for Media Utopia."""

from __future__ import annotations

import asyncio
from datetime import datetime
from typing import Any
from urllib.parse import parse_qs

from homeassistant.components.media_player import MediaPlayerEntity
try:
    from homeassistant.components.media_player import BrowseMedia
except Exception:  # pragma: no cover
    from homeassistant.components.media_player.browse_media import BrowseMedia
from homeassistant.components.media_player.const import (
    MediaPlayerEntityFeature,
    MediaPlayerState,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.util import dt as dt_util

from .const import DOMAIN
from .zone import ZoneEntityManager

import logging

try:
    from homeassistant.components.media_player.const import MediaPlayerRepeatMode
except ImportError:  # pragma: no cover - older HA compatibility
    MediaPlayerRepeatMode = None

REPEAT_ALL = MediaPlayerRepeatMode.ALL if MediaPlayerRepeatMode else "all"
REPEAT_OFF = MediaPlayerRepeatMode.OFF if MediaPlayerRepeatMode else "off"
REPEAT_ONE = MediaPlayerRepeatMode.ONE if MediaPlayerRepeatMode else "one"

try:
    from homeassistant.components.media_player.const import (
        MEDIA_CLASS_DIRECTORY,
        MEDIA_CLASS_MUSIC,
        MEDIA_CLASS_PLAYLIST,
    )
except Exception:  # pragma: no cover
    MEDIA_CLASS_DIRECTORY = "directory"
    MEDIA_CLASS_MUSIC = "music"
    MEDIA_CLASS_PLAYLIST = "playlist"

try:
    from homeassistant.components.media_player.const import (
        MEDIA_TYPE_DIRECTORY,
        MEDIA_TYPE_MUSIC,
        MEDIA_TYPE_PLAYLIST,
    )
except Exception:  # pragma: no cover
    MEDIA_TYPE_DIRECTORY = "directory"
    MEDIA_TYPE_MUSIC = "music"
    MEDIA_TYPE_PLAYLIST = "playlist"

_LOGGER = logging.getLogger(__name__)

async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up Media Utopia media players."""
    data = hass.data[DOMAIN][entry.entry_id]
    bridge = data["bridge"]

    manager = RendererManager(bridge, async_add_entities)
    await manager.async_start()
    data["renderer_manager"] = manager

    zone_manager = ZoneEntityManager(bridge, async_add_entities)
    await zone_manager.async_start()
    data["zone_manager"] = zone_manager


class RendererManager:
    """Manage renderer entities from mu presence."""

    def __init__(self, bridge, async_add_entities: AddEntitiesCallback) -> None:
        self._bridge = bridge
        self._async_add_entities = async_add_entities
        self._entities: dict[str, MuRendererEntity] = {}

    async def async_start(self) -> None:
        self._bridge.register_renderer_listener(self._on_renderer)
        self._bridge.register_renderer_state_listener(self._on_state)

    @callback
    def _on_renderer(self, node_id: str) -> None:
        if node_id in self._entities:
            return
        entity = MuRendererEntity(self._bridge, node_id)
        self._entities[node_id] = entity
        self._async_add_entities([entity])

    @callback
    def _on_state(self, node_id: str, _state: dict[str, Any]) -> None:
        entity = self._entities.get(node_id)
        if entity is None:
            return
        entity.async_write_ha_state()


class MuRendererEntity(MediaPlayerEntity):
    """Media player entity backed by a mu renderer node."""

    _attr_should_poll = False

    def __init__(self, bridge, node_id: str) -> None:
        self._bridge = bridge
        self._node_id = node_id

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_renderer_{safe}"

    @property
    def name(self) -> str | None:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        return renderer.get("name", self._node_id)

    @property
    def available(self) -> bool:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        return bool(renderer.get("online", True))

    @property
    def state(self) -> MediaPlayerState | None:
        status = (self._playback().get("status") or "").lower()
        if status == "playing":
            return MediaPlayerState.PLAYING
        if status == "paused":
            return MediaPlayerState.PAUSED
        if status == "stopped":
            return MediaPlayerState.IDLE
        return MediaPlayerState.IDLE

    @property
    def volume_level(self) -> float | None:
        return self._playback().get("volume")

    @property
    def is_volume_muted(self) -> bool | None:
        return self._playback().get("mute")

    @property
    def media_title(self) -> str | None:
        return self._metadata().get("title")

    @property
    def media_artist(self) -> str | None:
        return self._artist()

    @property
    def media_album_name(self) -> str | None:
        return self._metadata().get("album")

    @property
    def media_duration(self) -> int | None:
        duration = self._playback().get("durationMs")
        if duration is None:
            return None
        return int(duration / 1000)

    @property
    def media_position(self) -> int | None:
        position = self._playback().get("positionMs")
        if position is None:
            return None
        return int(position / 1000)

    @property
    def media_position_updated_at(self) -> datetime | None:
        state = self._bridge.get_renderer_state(self._node_id)
        status = (state.get("playback") or {}).get("status", "").lower()
        if status == "playing":
            return dt_util.utcnow()
        ts = state.get("ts")
        if ts is None:
            return None
        return dt_util.as_utc(datetime.utcfromtimestamp(float(ts)))

    @property
    def media_image_url(self) -> str | None:
        # Use for_internal=True so HA fetches directly from upstream
        # (avoids SSL issues when fetching via external proxy URL)
        return self._bridge.rewrite_artwork_url(self._metadata().get("artworkUrl"), for_internal=True)

    @property
    def media_content_id(self) -> str | None:
        state = self._bridge.get_renderer_state(self._node_id)
        current = state.get("current") or {}
        return current.get("itemId")

    @property
    def media_content_type(self) -> str | None:
        meta = self._metadata()
        media_type = (meta.get("mediaType") or meta.get("type") or "").lower()
        if media_type == "audio":
            return MEDIA_TYPE_MUSIC
        if media_type == "video":
            return "video"
        return media_type or None

    @property
    def shuffle(self) -> bool | None:
        queue = self._queue()
        return queue.get("shuffle")

    @property
    def repeat(self) -> str | None:
        queue = self._queue()
        mode = (queue.get("repeatMode") or "").lower()
        if mode == "one":
            return REPEAT_ONE
        if mode == "all" or queue.get("repeat"):
            return REPEAT_ALL
        return REPEAT_OFF

    @property
    def supported_features(self) -> MediaPlayerEntityFeature:
        return (
            MediaPlayerEntityFeature.PLAY
            | MediaPlayerEntityFeature.PAUSE
            | MediaPlayerEntityFeature.STOP
            | MediaPlayerEntityFeature.NEXT_TRACK
            | MediaPlayerEntityFeature.PREVIOUS_TRACK
            | MediaPlayerEntityFeature.SEEK
            | MediaPlayerEntityFeature.VOLUME_SET
            | MediaPlayerEntityFeature.VOLUME_MUTE
            | MediaPlayerEntityFeature.SHUFFLE_SET
            | MediaPlayerEntityFeature.REPEAT_SET
            | MediaPlayerEntityFeature.PLAY_MEDIA
            | MediaPlayerEntityFeature.BROWSE_MEDIA
        )

    async def async_media_play(self) -> None:
        _LOGGER.debug("play %s", self._node_id)
        await self._bridge.async_play(self._node_id)

    async def async_media_pause(self) -> None:
        _LOGGER.debug("pause %s", self._node_id)
        await self._bridge.async_pause(self._node_id)

    async def async_media_stop(self) -> None:
        _LOGGER.debug("stop %s", self._node_id)
        await self._bridge.async_stop(self._node_id)

    async def async_media_next_track(self) -> None:
        _LOGGER.debug("next %s", self._node_id)
        await self._bridge.async_next(self._node_id)

    async def async_media_previous_track(self) -> None:
        _LOGGER.debug("prev %s", self._node_id)
        await self._bridge.async_previous(self._node_id)

    async def async_set_volume_level(self, volume: float) -> None:
        _LOGGER.debug("volume %s %.2f", self._node_id, volume)
        await self._bridge.async_set_volume(self._node_id, volume)

    async def async_mute_volume(self, mute: bool) -> None:
        _LOGGER.debug("mute %s %s", self._node_id, mute)
        await self._bridge.async_mute(self._node_id, mute)

    async def async_media_seek(self, position: float) -> None:
        _LOGGER.debug("seek %s %.2f", self._node_id, position)
        await self._bridge.async_seek(self._node_id, position)

    async def async_set_shuffle(self, shuffle: bool) -> None:
        _LOGGER.debug("shuffle %s %s", self._node_id, shuffle)
        await self._bridge.async_shuffle(self._node_id, shuffle)

    async def async_set_repeat(self, repeat: str) -> None:
        _LOGGER.debug("repeat %s %s", self._node_id, repeat)
        repeat_value = (repeat or "").lower()
        if repeat_value in {"one", "single"}:
            await self._bridge.async_repeat_mode(self._node_id, "one")
            return
        enabled = repeat_value in {"all", "on", "true"}
        await self._bridge.async_repeat(self._node_id, enabled)

    async def async_play_media(
        self, media_type: str, media_id: str, **kwargs: Any
    ) -> None:
        _ = media_type
        _LOGGER.debug("play_media %s %s", self._node_id, media_id)
        # Handle queue jump requests (from Current Queue browser)
        if str(media_id).startswith("queue_jump:"):
            try:
                index = int(str(media_id)[len("queue_jump:"):])
                await self._bridge.async_queue_jump(self._node_id, index)
                return
            except ValueError:
                pass
        await self._bridge.async_play_media(self._node_id, media_id)

    async def async_browse_media(self, media_content_type=None, media_content_id=None):
        root = BrowseMedia(
            media_class=MEDIA_CLASS_DIRECTORY,
            media_content_id="root",
            media_content_type=MEDIA_TYPE_DIRECTORY,
            title="Media Utopia",
            can_play=False,
            can_expand=True,
            children=[],
        )

        if not media_content_id or media_content_id == "root":
            root.children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_PLAYLIST,
                    media_content_id="queue",
                    media_content_type=MEDIA_TYPE_PLAYLIST,
                    title="Current Queue",
                    can_play=False,
                    can_expand=True,
                )
            )
            root.children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_DIRECTORY,
                    media_content_id="playlists",
                    media_content_type=MEDIA_TYPE_DIRECTORY,
                    title="Playlists",
                    can_play=False,
                    can_expand=True,
                )
            )
            root.children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_DIRECTORY,
                    media_content_id="snapshots",
                    media_content_type=MEDIA_TYPE_DIRECTORY,
                    title="Snapshots",
                    can_play=False,
                    can_expand=True,
                )
            )
            for library_id, name in self._bridge.list_libraries():
                root.children.append(
                    BrowseMedia(
                        media_class=MEDIA_CLASS_DIRECTORY,
                        media_content_id=f"library:{library_id}",
                        media_content_type=MEDIA_TYPE_DIRECTORY,
                        title=name,
                        can_play=True,
                        can_expand=True,
                    )
                )
            return root

        if str(media_content_id) == "playlists":
            playlists = BrowseMedia(
                media_class=MEDIA_CLASS_DIRECTORY,
                media_content_id="playlists",
                media_content_type=MEDIA_TYPE_DIRECTORY,
                title="Playlists",
                can_play=False,
                can_expand=True,
                children=[],
            )
            for playlist_id, name in self._bridge.list_playlists():
                playlists.children.append(
                    BrowseMedia(
                        media_class=MEDIA_CLASS_PLAYLIST,
                        media_content_id=f"playlist:{playlist_id}",
                        media_content_type=MEDIA_TYPE_PLAYLIST,
                        title=name,
                        can_play=True,
                        can_expand=True,
                    )
                )
            return playlists

        if str(media_content_id) == "snapshots":
            snapshots = BrowseMedia(
                media_class=MEDIA_CLASS_DIRECTORY,
                media_content_id="snapshots",
                media_content_type=MEDIA_TYPE_DIRECTORY,
                title="Snapshots",
                can_play=False,
                can_expand=True,
                children=[],
            )
            for snapshot_id, name in await self._bridge.async_list_snapshots():
                snapshots.children.append(
                    BrowseMedia(
                        media_class=MEDIA_CLASS_DIRECTORY,
                        media_content_id=f"snapshot:{snapshot_id}",
                        media_content_type=MEDIA_TYPE_DIRECTORY,
                        title=name,
                        can_play=True,
                        can_expand=True,
                    )
                )
            return snapshots

        if str(media_content_id).startswith("library:"):
            return await self._browse_library(media_content_id)

        if str(media_content_id).startswith("snapshot:"):
            return await self._browse_snapshot(media_content_id)

        if str(media_content_id) == "queue" or str(media_content_id).startswith("queue?"):
            return await self._browse_queue(media_content_id)

        rest = str(media_content_id)[len("playlist:") :]
        page = 1
        if "?page=" in rest:
            rest, page_str = rest.rsplit("?page=", 1)
            try:
                page = max(1, int(page_str))
            except ValueError:
                page = 1
        playlist_id = rest.strip()
        if not playlist_id:
            return root

        playlist = await self._bridge.async_fetch_playlist(playlist_id)
        if not playlist:
            return root

        entries = playlist.get("entries") or []
        page_size = 25
        total = len(entries)
        start = max(0, (page - 1) * page_size)
        end = min(total, start + page_size)
        slice_entries = entries[start:end]
        children = []

        item_ids = []
        for entry in slice_entries:
            ref = (entry or {}).get("ref") or {}
            resolved = (entry or {}).get("resolved") or {}
            item_id = ref.get("id") or resolved.get("url")
            if not item_id:
                continue
            item_ids.append(item_id)

        lib_ids = [item_id for item_id in item_ids if str(item_id).startswith("lib:")]
        meta_map = {}
        if lib_ids:
            meta_map = await self._bridge.async_fetch_metadata_batch(lib_ids)

        for item_id in item_ids:
            meta = meta_map.get(item_id, {})
            title = meta.get("title") or item_id
            artist = meta.get("artist")
            album = meta.get("album")
            artwork = self._bridge.rewrite_artwork_url(meta.get("artworkUrl"))
            if artist and album:
                display = f"{title} — {artist} ({album})"
            elif artist:
                display = f"{title} — {artist}"
            else:
                display = title
            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_MUSIC,
                    media_content_id=item_id,
                    media_content_type=MEDIA_TYPE_MUSIC,
                    title=display,
                    thumbnail=artwork,
                    can_play=True,
                    can_expand=False,
                )
            )

        if end < total:
            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_PLAYLIST,
                    media_content_id=f"playlist:{playlist_id}?page={page+1}",
                    media_content_type=MEDIA_TYPE_PLAYLIST,
                    title="Next page",
                    can_play=False,
                    can_expand=True,
                )
            )

        return BrowseMedia(
            media_class=MEDIA_CLASS_PLAYLIST,
            media_content_id=f"playlist:{playlist_id}?page={page}",
            media_content_type=MEDIA_TYPE_PLAYLIST,
            title=f"{playlist.get('name', playlist_id)} (page {page})",
            can_play=True,
            can_expand=True,
            children=children,
        )

    async def _browse_snapshot(self, media_content_id: str) -> BrowseMedia:
        rest = str(media_content_id)[len("snapshot:") :]
        page = 1
        if "?page=" in rest:
            rest, page_str = rest.rsplit("?page=", 1)
            try:
                page = max(1, int(page_str))
            except ValueError:
                page = 1
        snapshot_id = rest.strip()
        if not snapshot_id:
            return BrowseMedia(
                media_class=MEDIA_CLASS_DIRECTORY,
                media_content_id="snapshots",
                media_content_type=MEDIA_TYPE_DIRECTORY,
                title="Snapshots",
                can_play=False,
                can_expand=True,
                children=[],
            )

        snapshot = await self._bridge.async_get_snapshot(snapshot_id)
        if not snapshot:
            return BrowseMedia(
                media_class=MEDIA_CLASS_DIRECTORY,
                media_content_id=f"snapshot:{snapshot_id}",
                media_content_type=MEDIA_TYPE_DIRECTORY,
                title=snapshot_id,
                can_play=False,
                can_expand=True,
                children=[],
            )

        items = snapshot.get("items") or []
        page_size = 25
        total = len(items)
        start = max(0, (page - 1) * page_size)
        end = min(total, start + page_size)
        slice_items = items[start:end]

        lib_ids = [item for item in slice_items if str(item).startswith("lib:")]
        meta_map = {}
        if lib_ids:
            meta_map = await self._bridge.async_fetch_metadata_batch(lib_ids)

        children = []

        for item_id in slice_items:
            meta = meta_map.get(item_id, {})
            title = meta.get("title") or item_id
            artist = meta.get("artist")
            album = meta.get("album")
            artwork = self._bridge.rewrite_artwork_url(meta.get("artworkUrl"))
            if artist and album:
                display = f"{title} — {artist} ({album})"
            elif artist:
                display = f"{title} — {artist}"
            else:
                display = title
            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_MUSIC,
                    media_content_id=item_id,
                    media_content_type=MEDIA_TYPE_MUSIC,
                    title=display,
                    thumbnail=artwork,
                    can_play=True,
                    can_expand=False,
                )
            )

        if end < total:
            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_DIRECTORY,
                    media_content_id=f"snapshot:{snapshot_id}?page={page+1}",
                    media_content_type=MEDIA_TYPE_DIRECTORY,
                    title="Next page",
                    can_play=False,
                    can_expand=True,
                )
            )

        return BrowseMedia(
            media_class=MEDIA_CLASS_DIRECTORY,
            media_content_id=f"snapshot:{snapshot_id}?page={page}",
            media_content_type=MEDIA_TYPE_DIRECTORY,
            title=f"{snapshot.get('name', snapshot_id)} (page {page})",
            can_play=True,
            can_expand=True,
            children=children,
        )

    async def _browse_queue(self, media_content_id: str = "queue") -> BrowseMedia:
        """Browse the current queue and allow jumping to any track."""
        # Parse page from content_id
        page = 1
        if "?page=" in str(media_content_id):
            try:
                page = max(1, int(str(media_content_id).split("?page=")[1]))
            except ValueError:
                page = 1

        entries = await self._bridge.async_get_queue(self._node_id)

        # Empty queue case
        if not entries:
            return BrowseMedia(
                media_class=MEDIA_CLASS_PLAYLIST,
                media_content_id="queue",
                media_content_type=MEDIA_TYPE_PLAYLIST,
                title="Current Queue",
                can_play=False,
                can_expand=False,
                children=[],
            )

        # Pagination
        page_size = 25
        total = len(entries)
        start = max(0, (page - 1) * page_size)
        end = min(total, start + page_size)
        page_entries = entries[start:end]

        # Collect item IDs that need metadata lookup (those without embedded metadata)
        items_needing_meta = []
        for entry in page_entries:
            item_id = entry.get("itemId")
            embedded_meta = entry.get("metadata") or {}
            if item_id and not embedded_meta.get("title") and str(item_id).startswith("lib:"):
                items_needing_meta.append(item_id)

        # Fetch metadata in batch for items that need it
        meta_map = {}
        if items_needing_meta:
            meta_map = await self._bridge.async_fetch_metadata_fresh(items_needing_meta)

        children = []
        for idx, entry in enumerate(page_entries):
            global_idx = start + idx  # Actual queue index for jumping
            item_id = entry.get("itemId") or ""
            resolved = entry.get("resolved") or {}
            url = resolved.get("url") or ""

            # Try metadata sources in order: embedded, fetched, resolved
            embedded_meta = entry.get("metadata") or {}
            fetched_meta = meta_map.get(item_id, {})
            resolved_meta = resolved.get("metadata") or {}
            meta = {**resolved_meta, **fetched_meta, **embedded_meta}

            title = meta.get("title") or item_id or url or f"Track {global_idx + 1}"
            artist = meta.get("artist")
            album = meta.get("album")
            artwork = self._bridge.rewrite_artwork_url(meta.get("artworkUrl"))

            # Build display title
            if artist and album:
                display = f"{title} — {artist} ({album})"
            elif artist:
                display = f"{title} — {artist}"
            else:
                display = title

            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_MUSIC,
                    media_content_id=f"queue_jump:{global_idx}",
                    media_content_type=MEDIA_TYPE_MUSIC,
                    title=display,
                    thumbnail=artwork,
                    can_play=True,
                    can_expand=False,
                )
            )

        # Add "Next page" if there are more entries
        if end < total:
            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_DIRECTORY,
                    media_content_id=f"queue?page={page + 1}",
                    media_content_type=MEDIA_TYPE_DIRECTORY,
                    title="Next page",
                    can_play=False,
                    can_expand=True,
                )
            )

        title = "Current Queue" if page == 1 else f"Current Queue (page {page})"
        return BrowseMedia(
            media_class=MEDIA_CLASS_PLAYLIST,
            media_content_id=f"queue?page={page}",
            media_content_type=MEDIA_TYPE_PLAYLIST,
            title=title,
            can_play=False,
            can_expand=False,
            children=children,
        )

    async def _browse_library(self, media_content_id: str) -> BrowseMedia:
        rest = str(media_content_id)[len("library:") :]
        node_id = rest
        container_id = ""
        page = 1
        if "?" in rest:
            node_id, query = rest.split("?", 1)
            params = parse_qs(query)
            container_id = (params.get("container") or [""])[0]
            try:
                page = max(1, int((params.get("page") or ["1"])[0]))
            except ValueError:
                page = 1
        node_id = node_id.strip()
        if not node_id:
            return BrowseMedia(
                media_class=MEDIA_CLASS_DIRECTORY,
                media_content_id="root",
                media_content_type=MEDIA_TYPE_DIRECTORY,
                title="Media Utopia",
                can_play=False,
                can_expand=True,
                children=[],
            )

        page_size = 50
        start = (page - 1) * page_size
        payload = await self._bridge.async_browse_library(
            node_id, container_id, start, page_size
        )
        if not payload:
            return BrowseMedia(
                media_class=MEDIA_CLASS_DIRECTORY,
                media_content_id=f"library:{node_id}",
                media_content_type=MEDIA_TYPE_DIRECTORY,
                title=node_id,
                can_play=False,
                can_expand=True,
                children=[],
            )

        items = payload.get("items") or []
        total_raw = payload.get("total")
        try:
            total = int(total_raw) if total_raw is not None else 0
        except (TypeError, ValueError):
            total = 0
        info = self._bridge.get_library(node_id) or {}
        title = info.get("name") or node_id
        if container_id:
            title = f"{title} (page {page})"

        children = []
        for item in items:
            item_id = item.get("itemId")
            if not item_id:
                continue
            media_type = (item.get("mediaType") or "").lower()
            item_type = (item.get("type") or "").lower()
            title_text = item.get("name") or item_id
            artist = ""
            if item.get("artists"):
                artist = ", ".join(item.get("artists"))
            album = item.get("album")
            if artist and album:
                display = f"{title_text} — {artist} ({album})"
            elif artist:
                display = f"{title_text} — {artist}"
            else:
                display = title_text
            artwork = self._bridge.rewrite_artwork_url(item.get("imageUrl"))

            if item_type == "playlist":
                playable = False
            else:
                playable = media_type in {"audio", "video"} or item_type in {
                "audio",
                "video",
                "movie",
                "episode",
                "musicvideo",
                }
            if playable:
                children.append(
                    BrowseMedia(
                        media_class=MEDIA_CLASS_MUSIC,
                        media_content_id=f"lib:{node_id}:{item_id}",
                        media_content_type=MEDIA_TYPE_MUSIC,
                        title=display,
                        thumbnail=artwork,
                        can_play=True,
                        can_expand=False,
                    )
                )
                continue

            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_DIRECTORY,
                    media_content_id=f"library:{node_id}?container={item_id}",
                    media_content_type=MEDIA_TYPE_DIRECTORY,
                    title=display,
                    thumbnail=artwork,
                    can_play=True,
                    can_expand=True,
                )
            )

        if container_id and total > start + len(items):
            next_id = f"library:{node_id}?container={container_id}&page={page+1}"
            children.append(
                BrowseMedia(
                    media_class=MEDIA_CLASS_DIRECTORY,
                    media_content_id=next_id,
                    media_content_type=MEDIA_TYPE_DIRECTORY,
                    title="Next page",
                    can_play=False,
                    can_expand=True,
                )
            )

        current_id = f"library:{node_id}"
        if container_id or page > 1:
            current_id = f"library:{node_id}?container={container_id}&page={page}"

        return BrowseMedia(
            media_class=MEDIA_CLASS_DIRECTORY,
            media_content_id=current_id,
            media_content_type=MEDIA_TYPE_DIRECTORY,
            title=title,
            can_play=True,
            can_expand=True,
            children=children,
        )

    def _playback(self) -> dict[str, Any]:
        state = self._bridge.get_renderer_state(self._node_id)
        return state.get("playback") or {}

    def _metadata(self) -> dict[str, Any]:
        state = self._bridge.get_renderer_state(self._node_id)
        current = state.get("current") or {}
        return current.get("metadata") or {}

    def _queue(self) -> dict[str, Any]:
        state = self._bridge.get_renderer_state(self._node_id)
        return state.get("queue") or {}

    def _artist(self) -> str | None:
        meta = self._metadata()
        artist = meta.get("artist")
        if artist:
            return artist
        artists = meta.get("artists")
        if isinstance(artists, list) and artists:
            return ", ".join(str(a) for a in artists if a)
        return None

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        state = self._bridge.get_renderer_state(self._node_id)
        current = state.get("current") or {}
        metadata = current.get("metadata") or {}
        session = state.get("session") or {}
        queue = state.get("queue") or {}
        return {
            "artist": self._artist(),
            "album": metadata.get("album"),
            "media_type": metadata.get("mediaType"),
            "item_type": metadata.get("type"),
            "duration_ms": metadata.get("durationMs"),
            "position_ms": (state.get("playback") or {}).get("positionMs"),
            "item_id": current.get("itemId"),
            "lease_owner": session.get("owner"),
            "lease_id": session.get("id"),
            "lease_expires_at": session.get("leaseExpiresAt"),
            "queue_length": queue.get("length"),
            "queue_index": queue.get("index"),
        }

    @property
    def device_info(self):
        renderer = self._bridge.get_renderer(self._node_id) or {}
        return {
            "identifiers": {("mu", self._node_id)},
            "name": renderer.get("name", self._node_id),
            "manufacturer": "Mu",
        }
