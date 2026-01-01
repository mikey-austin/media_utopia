"""Text entities for Media Utopia snapshots."""

from __future__ import annotations

from homeassistant.components.text import TextEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up snapshot name text entities."""
    data = hass.data[DOMAIN][entry.entry_id]
    bridge = data["bridge"]

    manager = SnapshotNameManager(bridge, async_add_entities)
    await manager.async_start()
    data["snapshot_name_manager"] = manager


class SnapshotNameManager:
    """Manage snapshot name inputs per renderer."""

    def __init__(self, bridge, async_add_entities: AddEntitiesCallback) -> None:
        self._bridge = bridge
        self._async_add_entities = async_add_entities
        self._entities: dict[str, SnapshotNameEntity] = {}

    async def async_start(self) -> None:
        self._bridge.register_renderer_listener(self._on_renderer)
        for node_id, _ in self._bridge.list_renderers():
            self._on_renderer(node_id)

    @callback
    def _on_renderer(self, node_id: str) -> None:
        if node_id in self._entities:
            return
        entity = SnapshotNameEntity(self._bridge, node_id)
        self._entities[node_id] = entity
        self._async_add_entities([entity])

    def get_name(self, node_id: str) -> str:
        entity = self._entities.get(node_id)
        if entity is None:
            return ""
        return entity.native_value or ""


class SnapshotNameEntity(TextEntity):
    """Text input for snapshot name."""

    _attr_should_poll = False
    _attr_native_min = 0
    _attr_native_max = 128

    def __init__(self, bridge, node_id: str) -> None:
        self._bridge = bridge
        self._node_id = node_id
        self._value = bridge.get_snapshot_name(node_id)

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_snapshot_name_{safe}"

    @property
    def name(self) -> str | None:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        base = renderer.get("name", self._node_id)
        return f"{base} Snapshot Name"

    @property
    def native_value(self) -> str | None:
        return self._value

    async def async_set_value(self, value: str) -> None:
        self._value = value
        self._bridge.set_snapshot_name(self._node_id, value)
        self.async_write_ha_state()

    @property
    def device_info(self):
        renderer = self._bridge.get_renderer(self._node_id) or {}
        return {
            "identifiers": {("mu", self._node_id)},
            "name": renderer.get("name", self._node_id),
            "manufacturer": "Mu",
        }
