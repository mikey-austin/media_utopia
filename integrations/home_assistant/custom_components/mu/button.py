"""Button entities for Media Utopia."""

from __future__ import annotations

from homeassistant.components.button import ButtonEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .select import PlaylistSelectionManager


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up button entities."""
    data = hass.data[DOMAIN][entry.entry_id]
    bridge = data["bridge"]

    selection_manager = data.get("playlist_selection_manager")
    if selection_manager is None:
        selection_manager = PlaylistSelectionManager(bridge)
        data["playlist_selection_manager"] = selection_manager

    manager = ButtonManager(bridge, selection_manager, async_add_entities)
    await manager.async_start()
    data["button_manager"] = manager


class ButtonManager:
    """Create buttons for renderer leases and playlist loading."""

    def __init__(self, bridge, selection_manager: PlaylistSelectionManager, async_add_entities) -> None:
        self._bridge = bridge
        self._selection_manager = selection_manager
        self._async_add_entities = async_add_entities
        self._renderer_entities: dict[str, list[ButtonEntity]] = {}
        self._playlist_entities: dict[str, ButtonEntity] = {}

    async def async_start(self) -> None:
        self._bridge.register_renderer_listener(self._on_renderer)
        self._bridge.register_playlist_listener(self._on_playlist)
        for node_id, _ in self._bridge.list_renderers():
            self._on_renderer(node_id)
        for playlist_id, _ in self._bridge.list_playlists():
            self._on_playlist(playlist_id)

    @callback
    def _on_renderer(self, node_id: str) -> None:
        if node_id in self._renderer_entities:
            return
        entities: list[ButtonEntity] = [
            LeaseAcquireButton(self._bridge, node_id),
            LeaseRenewButton(self._bridge, node_id),
            LeaseReleaseButton(self._bridge, node_id),
        ]
        self._renderer_entities[node_id] = entities
        self._async_add_entities(entities)

    @callback
    def _on_playlist(self, playlist_id: str) -> None:
        if playlist_id in self._playlist_entities:
            return
        self._selection_manager.ensure_selection(playlist_id)
        entity = PlaylistLoadButton(self._bridge, self._selection_manager, playlist_id)
        self._playlist_entities[playlist_id] = entity
        self._async_add_entities([entity])


class LeaseButton(ButtonEntity):
    """Base class for renderer lease buttons."""

    _attr_should_poll = False

    def __init__(self, bridge, node_id: str, suffix: str, label: str) -> None:
        self._bridge = bridge
        self._node_id = node_id
        self._suffix = suffix
        self._label = label

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_lease_{self._suffix}_{safe}"

    @property
    def name(self) -> str | None:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        name = renderer.get("name", self._node_id)
        return f"{self._label} {name}"

    @property
    def device_info(self):
        renderer = self._bridge.get_renderer(self._node_id) or {}
        return {
            "identifiers": {("mu", self._node_id)},
            "name": renderer.get("name", self._node_id),
            "manufacturer": "Mu",
        }


class LeaseAcquireButton(LeaseButton):
    def __init__(self, bridge, node_id: str) -> None:
        super().__init__(bridge, node_id, "acquire", "Acquire Lease")

    async def async_press(self) -> None:
        await self._bridge.async_acquire_lease(self._node_id)


class LeaseRenewButton(LeaseButton):
    def __init__(self, bridge, node_id: str) -> None:
        super().__init__(bridge, node_id, "renew", "Renew Lease")

    async def async_press(self) -> None:
        await self._bridge.async_renew_lease(self._node_id)


class LeaseReleaseButton(LeaseButton):
    def __init__(self, bridge, node_id: str) -> None:
        super().__init__(bridge, node_id, "release", "Release Lease")

    async def async_press(self) -> None:
        await self._bridge.async_release_lease(self._node_id)


class PlaylistLoadButton(ButtonEntity):
    """Button to load a playlist into a selected renderer."""

    _attr_should_poll = False

    def __init__(self, bridge, selection_manager: PlaylistSelectionManager, playlist_id: str) -> None:
        self._bridge = bridge
        self._selection_manager = selection_manager
        self._playlist_id = playlist_id

    @property
    def unique_id(self) -> str:
        safe = self._playlist_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_playlist_load_{safe}"

    @property
    def name(self) -> str | None:
        playlist = self._bridge.get_playlist(self._playlist_id) or {}
        pl_name = playlist.get("name", self._playlist_id)
        return f"Load {pl_name}"

    @property
    def extra_state_attributes(self):
        return {"playlist_id": self._playlist_id}

    async def async_press(self) -> None:
        renderer_id = self._selection_manager.selected_renderer_id(self._playlist_id)
        if renderer_id is None:
            return
        await self._bridge.async_load_playlist(renderer_id, self._playlist_id)
