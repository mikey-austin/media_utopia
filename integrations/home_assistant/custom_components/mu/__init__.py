"""Mud integration."""

from __future__ import annotations

import os
from pathlib import Path

from homeassistant.config_entries import ConfigEntry
from homeassistant.components import mqtt, frontend
from homeassistant.components.http import StaticPathConfig
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ConfigEntryNotReady

try:
    from homeassistant.components.mqtt.const import DATA_MQTT
except Exception:  # pragma: no cover - fallback for older HA
    DATA_MQTT = "mqtt"

from .bridge import MudBridge
from .const import DOMAIN
from .views import ArtworkProxyView
from .websocket_api import async_register_websocket_api

PLATFORMS: list[str] = ["media_player", "button", "select", "sensor", "text"]

# Path to the www directory containing panel assets
WWW_DIR = Path(__file__).parent / "www"
PANEL_URL = "/mu-panel"
PANEL_ICON = "mdi:music-box-multiple"


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Mud from a config entry."""
    if not _mqtt_ready(hass):
        raise ConfigEntryNotReady(
            "MQTT integration is not configured. Add mqtt: to configuration.yaml."
        )
    domain_data = hass.data.setdefault(DOMAIN, {})
    
    # Register HTTP views
    if not domain_data.get("artwork_proxy_registered"):
        hass.http.register_view(ArtworkProxyView(hass))
        domain_data["artwork_proxy_registered"] = True
    
    # Register WebSocket API
    if not domain_data.get("websocket_api_registered"):
        async_register_websocket_api(hass)
        domain_data["websocket_api_registered"] = True
    
    # Register custom panel (serve static files and add sidebar entry)
    if not domain_data.get("panel_registered"):
        await _register_panel(hass)
        domain_data["panel_registered"] = True
    
    bridge = MudBridge(hass, entry)
    await bridge.async_start()
    domain_data[entry.entry_id] = {"bridge": bridge}
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    return True


async def _register_panel(hass: HomeAssistant) -> None:
    """Register the MU custom panel."""
    # Register static path for panel assets
    await hass.http.async_register_static_paths([
        StaticPathConfig(
            url_path="/mu-panel-static",
            path=str(WWW_DIR),
            cache_headers=False,
        )
    ])
    
    # Register the custom panel in sidebar using direct import
    frontend.async_register_built_in_panel(
        hass,
        component_name="custom",
        sidebar_title="MU",
        sidebar_icon=PANEL_ICON,
        frontend_url_path="mu",
        config={
            "_panel_custom": {
                "name": "mu-panel",
                "module_url": "/mu-panel-static/mu-panel.js",
            }
        },
        require_admin=False,
    )


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

