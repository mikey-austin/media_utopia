"""Zone media_player entities for Snapcast multi-room audio."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.components.media_player import (
    MediaPlayerEntity,
    MediaPlayerEntityFeature,
    MediaPlayerState,
    MediaPlayerDeviceClass,
)

from .const import DOMAIN


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up Media Utopia zone entities."""
    data = hass.data[DOMAIN][entry.entry_id]
    bridge = data["bridge"]

    manager = ZoneEntityManager(bridge, async_add_entities)
    await manager.async_start()
    data["zone_manager"] = manager


class ZoneEntityManager:
    """Manage zone entities for zone controller nodes."""

    def __init__(self, bridge, async_add_entities: AddEntitiesCallback) -> None:
        self._bridge = bridge
        self._async_add_entities = async_add_entities
        self._entities: dict[str, MuZoneEntity] = {}

    async def async_start(self) -> None:
        self._bridge.register_zone_listener(self._on_zone)
        self._bridge.register_zone_state_listener(self._on_zone_state)

        # Add existing zones
        for node_id in self._bridge.list_zones():
            self._on_zone(node_id)

    @callback
    def _on_zone(self, node_id: str) -> None:
        if node_id in self._entities:
            return
        entity = MuZoneEntity(self._bridge, node_id)
        self._entities[node_id] = entity
        self._async_add_entities([entity])

    @callback
    def _on_zone_state(self, node_id: str, _state: dict[str, Any]) -> None:
        entity = self._entities.get(node_id)
        if entity:
            entity.async_write_ha_state()


class MuZoneEntity(MediaPlayerEntity):
    """A zone speaker entity for Snapcast."""

    _attr_should_poll = False
    _attr_device_class = MediaPlayerDeviceClass.SPEAKER
    _attr_supported_features = (
        MediaPlayerEntityFeature.VOLUME_SET
        | MediaPlayerEntityFeature.VOLUME_MUTE
        | MediaPlayerEntityFeature.SELECT_SOURCE
    )

    def __init__(self, bridge, node_id: str) -> None:
        self._bridge = bridge
        self._node_id = node_id

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_zone_{safe}"

    @property
    def name(self) -> str | None:
        zone = self._bridge.get_zone(self._node_id) or {}
        return zone.get("name", self._node_id)

    @property
    def available(self) -> bool:
        zone = self._bridge.get_zone(self._node_id) or {}
        state = zone.get("state") or {}
        return state.get("connected", True)

    @property
    def device_info(self):
        zone = self._bridge.get_zone(self._node_id) or {}
        controller_id = zone.get("controllerId", "")
        controller = self._bridge.get_zone_controller(controller_id) or {}
        return {
            "identifiers": {("mu", self._node_id)},
            "name": zone.get("name", self._node_id),
            "manufacturer": "Snapcast",
            "model": "Zone Speaker",
            "via_device": ("mu", controller_id) if controller_id else None,
        }

    @property
    def state(self) -> MediaPlayerState:
        zone = self._bridge.get_zone(self._node_id) or {}
        state = zone.get("state") or {}
        if not state.get("connected", True):
            return MediaPlayerState.OFF
        return MediaPlayerState.IDLE

    @property
    def volume_level(self) -> float | None:
        zone = self._bridge.get_zone(self._node_id) or {}
        state = zone.get("state") or {}
        return state.get("volume")

    @property
    def is_volume_muted(self) -> bool | None:
        zone = self._bridge.get_zone(self._node_id) or {}
        state = zone.get("state") or {}
        return state.get("mute")

    @property
    def source(self) -> str | None:
        zone = self._bridge.get_zone(self._node_id) or {}
        state = zone.get("state") or {}
        source_id = state.get("sourceId")
        if not source_id:
            return None
        # Get name from controller's sources
        controller_id = zone.get("controllerId", "")
        controller = self._bridge.get_zone_controller(controller_id) or {}
        sources = controller.get("sources") or []
        for s in sources:
            if s.get("id") == source_id:
                return s.get("name", source_id)
        return source_id

    @property
    def source_list(self) -> list[str] | None:
        zone = self._bridge.get_zone(self._node_id) or {}
        controller_id = zone.get("controllerId", "")
        controller = self._bridge.get_zone_controller(controller_id) or {}
        sources = controller.get("sources") or []
        return [s.get("name", s.get("id", "")) for s in sources]

    async def async_set_volume_level(self, volume: float) -> None:
        await self._bridge.async_zone_set_volume(self._node_id, volume)

    async def async_mute_volume(self, mute: bool) -> None:
        await self._bridge.async_zone_set_mute(self._node_id, mute)

    async def async_select_source(self, source: str) -> None:
        # Convert source name to source ID
        zone = self._bridge.get_zone(self._node_id) or {}
        controller_id = zone.get("controllerId", "")
        controller = self._bridge.get_zone_controller(controller_id) or {}
        sources = controller.get("sources") or []
        source_id = None
        for s in sources:
            if s.get("name") == source or s.get("id") == source:
                source_id = s.get("id")
                break
        if source_id:
            await self._bridge.async_zone_select_source(self._node_id, source_id)

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        zone = self._bridge.get_zone(self._node_id) or {}
        state = zone.get("state") or {}
        return {
            "zone_id": self._node_id,
            "controller_id": zone.get("controllerId"),
            "source_id": state.get("sourceId"),
            "connected": state.get("connected", True),
        }

    def _get_matching_renderer(self) -> str | None:
        """Get renderer node_id that matches the zone's current source."""
        zone = self._bridge.get_zone(self._node_id) or {}
        state = zone.get("state") or {}
        zone_source_id = state.get("sourceId")
        if not zone_source_id:
            return None
        
        # Iterate through all renderers to find one with matching source
        for renderer_id, renderer_name in self._bridge.list_renderers():
            renderer = self._bridge.get_renderer(renderer_id)
            if renderer and renderer.get("source") == zone_source_id:
                return renderer_id
        return None

    @property
    def media_title(self) -> str | None:
        """Return the title of current playing media from matched renderer."""
        renderer_id = self._get_matching_renderer()
        if not renderer_id:
            return None
        state = self._bridge.get_renderer_state(renderer_id)
        current = state.get("current") or {}
        metadata = current.get("metadata") or {}
        return metadata.get("title")

    @property
    def media_artist(self) -> str | None:
        """Return the artist of current playing media from matched renderer."""
        renderer_id = self._get_matching_renderer()
        if not renderer_id:
            return None
        state = self._bridge.get_renderer_state(renderer_id)
        current = state.get("current") or {}
        metadata = current.get("metadata") or {}
        artist = metadata.get("artist")
        if artist:
            return artist
        artists = metadata.get("artists")
        if isinstance(artists, list) and artists:
            return ", ".join(str(a) for a in artists if a)
        return None

    @property
    def media_album_name(self) -> str | None:
        """Return the album name of current playing media from matched renderer."""
        renderer_id = self._get_matching_renderer()
        if not renderer_id:
            return None
        state = self._bridge.get_renderer_state(renderer_id)
        current = state.get("current") or {}
        metadata = current.get("metadata") or {}
        return metadata.get("album")

    @property
    def media_image_url(self) -> str | None:
        """Return the image URL of current playing media from matched renderer."""
        renderer_id = self._get_matching_renderer()
        if not renderer_id:
            return None
        state = self._bridge.get_renderer_state(renderer_id)
        current = state.get("current") or {}
        metadata = current.get("metadata") or {}
        # Use for_internal=True so HA fetches directly from upstream
        return self._bridge.rewrite_artwork_url(metadata.get("artworkUrl"), for_internal=True)

    @property
    def media_duration(self) -> int | None:
        """Return the duration of current playing media from matched renderer."""
        renderer_id = self._get_matching_renderer()
        if not renderer_id:
            return None
        state = self._bridge.get_renderer_state(renderer_id)
        playback = state.get("playback") or {}
        duration = playback.get("durationMs")
        if duration is None:
            return None
        return int(duration / 1000)

    @property
    def media_position(self) -> int | None:
        """Return the position of current playing media from matched renderer."""
        renderer_id = self._get_matching_renderer()
        if not renderer_id:
            return None
        state = self._bridge.get_renderer_state(renderer_id)
        playback = state.get("playback") or {}
        position = playback.get("positionMs")
        if position is None:
            return None
        return int(position / 1000)

    @property
    def media_position_updated_at(self):
        """Return when the media position was last updated from matched renderer."""
        renderer_id = self._get_matching_renderer()
        if not renderer_id:
            return None
        from datetime import datetime
        from homeassistant.util import dt as dt_util
        state = self._bridge.get_renderer_state(renderer_id)
        playback = state.get("playback") or {}
        status = (playback.get("status") or "").lower()
        if status == "playing":
            return dt_util.utcnow()
        ts = state.get("ts")
        if ts is None:
            return None
        return dt_util.as_utc(datetime.utcfromtimestamp(float(ts)))
