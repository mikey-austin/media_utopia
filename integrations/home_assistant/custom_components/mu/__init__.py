"""Mud integration."""

from __future__ import annotations

from homeassistant.config_entries import ConfigEntry
from homeassistant.components import mqtt
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ConfigEntryNotReady

try:
    from homeassistant.components.mqtt.const import DATA_MQTT
except Exception:  # pragma: no cover - fallback for older HA
    DATA_MQTT = "mqtt"

from .bridge import MudBridge
from .const import DOMAIN

PLATFORMS: list[str] = ["media_player", "button", "select", "sensor", "text"]


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Mud from a config entry."""
    if not _mqtt_ready(hass):
        raise ConfigEntryNotReady(
            "MQTT integration is not configured. Add mqtt: to configuration.yaml."
        )
    bridge = MudBridge(hass, entry)
    await bridge.async_start()
    hass.data.setdefault(DOMAIN, {})[entry.entry_id] = {"bridge": bridge}
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload Mud config entry."""
    unload_ok = await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
    data = hass.data.get(DOMAIN, {}).pop(entry.entry_id, None)
    bridge: MudBridge | None = None
    if isinstance(data, dict):
        bridge = data.get("bridge")
    if bridge is not None:
        await bridge.async_stop()
    return unload_ok


def _mqtt_ready(hass: HomeAssistant) -> bool:
    is_configured = getattr(mqtt, "is_configured", None)
    if callable(is_configured):
        return bool(is_configured(hass))
    return DATA_MQTT in hass.data
