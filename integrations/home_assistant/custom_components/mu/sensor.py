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

    zone_manager = ZoneSensorManager(bridge, async_add_entities)
    await zone_manager.async_start()
    data["zone_sensor_manager"] = zone_manager


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
            QueueLengthSensor(self._bridge, node_id),
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

    def _queue(self) -> dict[str, Any]:
        state = self._bridge.get_renderer_state(self._node_id)
        return state.get("queue") or {}


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
        if not owner:
            return None
        if len(owner) > 250:
            return owner[:247] + "..."
        return owner

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        owner = self._session().get("owner")
        if not owner:
            return {}
        return {"full_owner": owner}


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
        if not session_id:
            return None
        # Show abbreviated version (last 8 chars), full ID in attributes
        if len(session_id) > 12:
            return "..." + session_id[-8:]
        return session_id

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        session_id = self._session().get("id")
        if not session_id:
            return {}
        return {"full_session_id": session_id}


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


class QueueLengthSensor(LeaseSensorBase):
    """Sensor for the current renderer queue length."""

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_renderer_queue_length_{safe}"

    @property
    def name(self) -> str | None:
        renderer = self._bridge.get_renderer(self._node_id) or {}
        base = renderer.get("name", self._node_id)
        return f"{base} Queue Length"

    @property
    def native_value(self) -> int | None:
        length = self._queue().get("length")
        if isinstance(length, int):
            return length
        return None


class ZoneSensorManager:
    """Manage sensors for zone controller nodes."""

    def __init__(self, bridge, async_add_entities: AddEntitiesCallback) -> None:
        self._bridge = bridge
        self._async_add_entities = async_add_entities
        self._sensors: dict[str, list[ZoneControllerSensorBase]] = {}

    async def async_start(self) -> None:
        self._bridge.register_zone_controller_listener(self._on_controller)
        self._bridge.register_zone_state_listener(self._on_zone_state)

        for node_id in self._bridge.list_zone_controllers():
            self._on_controller(node_id)

    @callback
    def _on_controller(self, node_id: str) -> None:
        if node_id in self._sensors:
            for sensor in self._sensors[node_id]:
                sensor.async_write_ha_state()
            return

        sensors: list[ZoneControllerSensorBase] = [
            TotalZonesSensor(self._bridge, node_id),
            ActiveZonesSensor(self._bridge, node_id),
            TotalSourcesSensor(self._bridge, node_id),
            SourceIDsSensor(self._bridge, node_id),
        ]
        self._sensors[node_id] = sensors
        self._async_add_entities(sensors)

    @callback
    def _on_zone_state(self, zone_id: str, _state: dict[str, Any]) -> None:
        zone = self._bridge.get_zone(zone_id)
        if not zone:
            return
        controller_id = zone.get("controllerId")
        if not controller_id or controller_id not in self._sensors:
            return
        for sensor in self._sensors[controller_id]:
            sensor.async_write_ha_state()


class ZoneControllerSensorBase(SensorEntity):
    """Base class for zone controller sensors."""

    _attr_should_poll = False
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_has_entity_name = True

    def __init__(self, bridge, node_id: str) -> None:
        self._bridge = bridge
        self._node_id = node_id

    @property
    def device_info(self):
        controller = self._bridge.get_zone_controller(self._node_id) or {}
        return {
            "identifiers": {("mu", self._node_id)},
            "name": controller.get("name", self._node_id),
            "manufacturer": "Snapcast",
            "model": "Zone Controller",
        }

    @property
    def available(self) -> bool:
        return self._bridge.get_zone_controller(self._node_id) is not None


class TotalZonesSensor(ZoneControllerSensorBase):
    """Sensor for total number of zones."""

    _attr_name = "Total Zones"
    _attr_unique_id_suffix = "total_zones"

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_zone_controller_{safe}_{self._attr_unique_id_suffix}"

    @property
    def native_value(self) -> int:
        controller = self._bridge.get_zone_controller(self._node_id) or {}
        zones = controller.get("zones") or []
        return len(zones)


class ActiveZonesSensor(ZoneControllerSensorBase):
    """Sensor for number of active (playing/unmuted) zones."""

    _attr_name = "Active Zones"
    _attr_unique_id_suffix = "active_zones"

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_zone_controller_{safe}_{self._attr_unique_id_suffix}"

    @property
    def native_value(self) -> int:
        controller = self._bridge.get_zone_controller(self._node_id) or {}
        zone_ids = controller.get("zones") or []
        count = 0
        for zid in zone_ids:
            zone = self._bridge.get_zone(zid)
            if not zone:
                continue
            state = zone.get("state") or {}
            if state.get("connected", True) and not state.get("mute", False):
                count += 1
        return count


class TotalSourcesSensor(ZoneControllerSensorBase):
    """Sensor for total number of sources."""

    _attr_name = "Total Sources"
    _attr_unique_id_suffix = "total_sources"

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_zone_controller_{safe}_{self._attr_unique_id_suffix}"

    @property
    def native_value(self) -> int:
        controller = self._bridge.get_zone_controller(self._node_id) or {}
        sources = controller.get("sources") or []
        return len(sources)


class SourceIDsSensor(ZoneControllerSensorBase):
    """Sensor displaying available source IDs for renderer configuration."""

    _attr_name = "Source IDs"
    _attr_unique_id_suffix = "source_ids"
    _attr_icon = "mdi:format-list-bulleted"

    @property
    def unique_id(self) -> str:
        safe = self._node_id.replace(":", "_").replace("@", "_").replace("/", "_")
        return f"mu_zone_controller_{safe}_{self._attr_unique_id_suffix}"

    @property
    def native_value(self) -> int:
        """Return count of sources (full list in attributes)."""
        controller = self._bridge.get_zone_controller(self._node_id) or {}
        sources = controller.get("sources") or []
        return len(sources)

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        """Return source details as attributes for easy reference."""
        controller = self._bridge.get_zone_controller(self._node_id) or {}
        sources = controller.get("sources") or []
        if not sources:
            return {}
        
        # Create a mapping of source IDs to names
        source_map = {}
        for source in sources:
            source_id = source.get("id")
            source_name = source.get("name", source_id)
            if source_id:
                source_map[source_id] = source_name
        
        return {
            "sources": source_map,
            "count": len(sources),
        }
