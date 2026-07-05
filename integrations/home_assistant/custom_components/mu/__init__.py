"""Media Utopia integration for Home Assistant.

Provides media player control, playlist management, and library browsing
via MQTT communication with Media Utopia renderers and services.
"""

from __future__ import annotations

import hashlib
import logging
import os
from pathlib import Path

_LOGGER = logging.getLogger(__name__)

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
    _LOGGER.debug("Setting up Media Utopia integration")
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
        _LOGGER.debug("Custom panel registered")

    bridge = MudBridge(hass, entry)
    try:
        await bridge.async_start()
    except ConfigEntryNotReady:
        raise
    except Exception as err:
        # MQTT subscribe failures (broker down / mqtt entry retrying) must
        # retry setup rather than landing in permanent SETUP_ERROR.
        await bridge.async_stop()
        raise ConfigEntryNotReady(f"MU bridge failed to start: {err}") from err
    domain_data[entry.entry_id] = {"bridge": bridge}
    _LOGGER.debug("Bridge started for entry %s", entry.entry_id)
    entry.async_on_unload(entry.add_update_listener(_async_update_options))
    try:
        await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    except Exception:
        domain_data.pop(entry.entry_id, None)
        await bridge.async_stop()
        raise
    return True


async def _async_update_options(hass: HomeAssistant, entry: ConfigEntry) -> None:
    """Handle options update."""
    _LOGGER.info("Options updated, reloading integration")
    await hass.config_entries.async_reload(entry.entry_id)


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
    # Compute content hash for cache busting
    panel_js = WWW_DIR / "mu-panel.js"
    js_hash = await hass.async_add_executor_job(
        lambda: hashlib.md5(panel_js.read_bytes()).hexdigest()[:8]
    )
    frontend.async_register_built_in_panel(
        hass,
        component_name="custom",
        sidebar_title="MU",
        sidebar_icon=PANEL_ICON,
        frontend_url_path="mu",
        config={
            "_panel_custom": {
                "name": "mu-panel",
                "module_url": f"/mu-panel-static/mu-panel.js?v={js_hash}",
            }
        },
        require_admin=False,
    )


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload Mud config entry."""
    unload_ok = await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
    domain_data = hass.data.get(DOMAIN, {})
    data = domain_data.pop(entry.entry_id, None)
    bridge: MudBridge | None = None
    if isinstance(data, dict):
        bridge = data.get("bridge")
    if bridge is not None:
        try:
            await bridge.async_stop()
        except Exception:
            _LOGGER.warning("Error stopping MU bridge", exc_info=True)
    # Remove the sidebar panel when the last entry goes away so removing
    # the integration doesn't leave a dead "MU" sidebar item.
    if not hass.config_entries.async_loaded_entries(DOMAIN) and domain_data.get(
        "panel_registered"
    ):
        try:
            frontend.async_remove_panel(hass, "mu")
        except Exception:
            _LOGGER.debug("Panel removal failed", exc_info=True)
        domain_data["panel_registered"] = False
    return unload_ok


def _mqtt_ready(hass: HomeAssistant) -> bool:
    is_configured = getattr(mqtt, "is_configured", None)
    if callable(is_configured):
        return bool(is_configured(hass))
    return DATA_MQTT in hass.data

