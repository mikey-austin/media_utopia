"""Sensor entities for Media Utopia renderer leases."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.components.sensor import SensorEntity
from homeassistant.helpers.entity import EntityCategory

try:
    from homeassistant.components.sensor import SensorDeviceClass
except Exception:  # pragma: no cover - older HA compatibility
    SensorDeviceClass = None

from .const import DOMAIN


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up Media Utopia sensors."""
    data = hass.data[DOMAIN][entry.entry_id]
    bridge = data["bridge"]

    manager = LeaseSensorManager(bridge, async_add_entities)
    await manager.async_start()
    data["lease_sensor_manager"] = manager


class LeaseSensorManager:
    """Manage lease sensors for renderer nodes."""

    def __init__(self, bridge, async_add_entities: AddEntitiesCallback) -> None:
        self._bridge = bridge
        self._async_add_entities = async_add_entities
        self._sensors: dict[str, list[LeaseSensorBase]] = {}

    async def async_start(self) -> None:
        self._bridge.register_renderer_listener(self._on_renderer)
        self._bridge.register_renderer_state_listener(self._on_state)

    @callback
    def _on_renderer(self, node_id: str) -> None:
        if node_id in self._sensors:
            return
        sensors: list[LeaseSensorBase] = [
            LeaseOwnerSensor(self._bridge, node_id),
            LeaseIDSensor(self._bridge, node_id),
            LeaseTTLSecondsSensor(self._bridge, node_id),
        ]
        self._sensors[node_id] = sensors
        self._async_add_entities(sensors)

    @callback
    def _on_state(self, node_id: str, _state: dict[str, Any]) -> None:
        sensors = self._sensors.get(node_id)
        if not sensors:
            return
        for sensor in sensors:
            sensor.async_write_ha_state()


class LeaseSensorBase(SensorEntity):
    """Base class for lease sensors."""

    _attr_should_poll = False
    _attr_entity_category = EntityCategory.DIAGNOSTIC

    def __init__(self, bridge, node_id: str) -> None:
        self._bridge = bridge
        self._node_id = node_id

    @property
    def available(self) -> bool:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        return bool(renderer.get("online", True))

    @property
    def device_info(self):
        renderer = self._bridge.get_renderer(self._node_id) or {}
        return {
            "identifiers": {("mu", self._node_id)},
            "name": renderer.get("name", self._node_id),
            "manufacturer": "Mu",
        }

    def _session(self) -> dict[str, Any]:
        state = self._bridge.get_renderer_state(self._node_id)
        return state.get("session") or {}


class LeaseOwnerSensor(LeaseSensorBase):
    """Sensor for the current lease owner."""

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_renderer_lease_owner_{safe}"

    @property
    def name(self) -> str | None:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        base = renderer.get("name", self._node_id)
        return f"{base} Lease Owner"

    @property
    def native_value(self) -> str | None:
        owner = self._session().get("owner")
        return owner or None


class LeaseIDSensor(LeaseSensorBase):
    """Sensor for the current lease session id."""

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_renderer_lease_id_{safe}"

    @property
    def name(self) -> str | None:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        base = renderer.get("name", self._node_id)
        return f"{base} Lease ID"

    @property
    def native_value(self) -> str | None:
        session_id = self._session().get("id")
        return session_id or None


class LeaseTTLSecondsSensor(LeaseSensorBase):
    """Sensor for the remaining lease TTL in seconds."""

    if SensorDeviceClass:
        _attr_device_class = SensorDeviceClass.DURATION
    _attr_native_unit_of_measurement = "s"

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_renderer_lease_ttl_{safe}"

    @property
    def name(self) -> str | None:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        base = renderer.get("name", self._node_id)
        return f"{base} Lease TTL"

    @property
    def native_value(self) -> int | None:
        expires_at = self._session().get("leaseExpiresAt")
        if not isinstance(expires_at, (int, float)):
            return None
        remaining = int(expires_at - datetime.now(tz=timezone.utc).timestamp())
        return max(0, remaining)

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        expires_at = self._session().get("leaseExpiresAt")
        if not isinstance(expires_at, (int, float)):
            return {}
        return {"lease_expires_at": datetime.fromtimestamp(float(expires_at), tz=timezone.utc).isoformat()}
