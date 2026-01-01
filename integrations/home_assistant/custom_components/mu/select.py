"""Select entities for Media Utopia playlists."""

from __future__ import annotations

from typing import Any

from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up playlist select entities."""
    data = hass.data[DOMAIN][entry.entry_id]
    bridge = data["bridge"]

    manager = data.get("playlist_selection_manager")
    if manager is None:
        manager = PlaylistSelectionManager(bridge)
        data["playlist_selection_manager"] = manager

    select_manager = PlaylistSelectManager(bridge, manager, async_add_entities)
    await select_manager.async_start()
    data["playlist_select_manager"] = select_manager


class PlaylistSelectionManager:
    """Track playlist -> renderer selections and renderer options."""

    def __init__(self, bridge) -> None:
        self._bridge = bridge
        self._selections: dict[str, str] = {}
        self._options: list[str] = []
        self._name_to_id: dict[str, str] = {}
        self._listeners: list[callable] = []
        self.refresh_renderers()
        self._bridge.register_renderer_listener(self._on_renderer)

    def register_listener(self, callback) -> None:
        self._listeners.append(callback)
        callback()

    def _notify(self) -> None:
        for callback in self._listeners:
            try:
                callback()
            except Exception:
                continue

    def _on_renderer(self, _node_id: str) -> None:
        self.refresh_renderers()

    def refresh_renderers(self) -> None:
        renderers = self._bridge.list_renderers()
        counts: dict[str, int] = {}
        for _, name in renderers:
            counts[name] = counts.get(name, 0) + 1

        options: list[str] = []
        name_to_id: dict[str, str] = {}
        for node_id, name in renderers:
            display = name
            if counts.get(name, 0) > 1:
                display = f"{name} ({node_id.split(':')[-1]})"
            options.append(display)
            name_to_id[display] = node_id

        self._options = options
        self._name_to_id = name_to_id
        self._notify()

    def options(self) -> list[str]:
        return list(self._options)

    def ensure_selection(self, playlist_id: str) -> None:
        if playlist_id in self._selections:
            return
        if self._options:
            self._selections[playlist_id] = self._options[0]

    def set_selection(self, playlist_id: str, option: str) -> None:
        self._selections[playlist_id] = option

    def selection(self, playlist_id: str) -> str | None:
        return self._selections.get(playlist_id)

    def selected_renderer_id(self, playlist_id: str) -> str | None:
        option = self._selections.get(playlist_id)
        if option is None:
            return None
        return self._name_to_id.get(option)


class PlaylistSelectManager:
    """Create select entities for playlists."""

    def __init__(
        self, bridge, selection_manager: PlaylistSelectionManager, async_add_entities
    ) -> None:
        self._bridge = bridge
        self._selection_manager = selection_manager
        self._async_add_entities = async_add_entities
        self._entities: dict[str, PlaylistRendererSelect] = {}

    async def async_start(self) -> None:
        self._selection_manager.register_listener(self._on_options_change)
        self._bridge.register_playlist_listener(self._on_playlist)
        for playlist_id, _ in self._bridge.list_playlists():
            self._on_playlist(playlist_id)

    @callback
    def _on_options_change(self) -> None:
        for entity in self._entities.values():
            entity.async_write_ha_state()

    @callback
    def _on_playlist(self, playlist_id: str) -> None:
        if playlist_id in self._entities:
            return
        self._selection_manager.ensure_selection(playlist_id)
        entity = PlaylistRendererSelect(self._bridge, self._selection_manager, playlist_id)
        self._entities[playlist_id] = entity
        self._async_add_entities([entity])


class PlaylistRendererSelect(SelectEntity):
    """Select entity to choose renderer for a playlist."""

    _attr_should_poll = False

    def __init__(self, bridge, selection_manager: PlaylistSelectionManager, playlist_id: str) -> None:
        self._bridge = bridge
        self._selection_manager = selection_manager
        self._playlist_id = playlist_id

    @property
    def unique_id(self) -> str:
        safe = self._playlist_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_playlist_renderer_{safe}"

    @property
    def name(self) -> str | None:
        playlist = self._bridge.get_playlist(self._playlist_id) or {}
        pl_name = playlist.get("name", self._playlist_id)
        return f"Renderer for {pl_name}"

    @property
    def options(self) -> list[str]:
        return self._selection_manager.options()

    @property
    def current_option(self) -> str | None:
        return self._selection_manager.selection(self._playlist_id)

    async def async_select_option(self, option: str) -> None:
        self._selection_manager.set_selection(self._playlist_id, option)
        self.async_write_ha_state()

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        return {
            "playlist_id": self._playlist_id,
        }
