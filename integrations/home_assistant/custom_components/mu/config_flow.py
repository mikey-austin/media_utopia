"""Config flow for the Mud integration."""

import voluptuous as vol

from homeassistant import config_entries

from .const import (
    CONF_DISCOVERY_PREFIX,
    CONF_ENTITY_PREFIX,
    CONF_IDENTITY,
    CONF_PLAYLIST_REFRESH,
    CONF_TOPIC_BASE,
    DEFAULT_DISCOVERY_PREFIX,
    DEFAULT_ENTITY_PREFIX,
    DEFAULT_IDENTITY,
    DEFAULT_PLAYLIST_REFRESH,
    DEFAULT_TOPIC_BASE,
    DOMAIN,
)


class MudConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for Media Utopia."""

    VERSION = 1

    async def async_step_user(self, user_input=None):
        """Handle the initial step."""
        if user_input is not None:
            return self.async_create_entry(title="Media Utopia", data=user_input)

        schema = vol.Schema(
            {
                vol.Optional(CONF_TOPIC_BASE, default=DEFAULT_TOPIC_BASE): str,
                vol.Optional(CONF_DISCOVERY_PREFIX, default=DEFAULT_DISCOVERY_PREFIX): str,
                vol.Optional(CONF_ENTITY_PREFIX, default=DEFAULT_ENTITY_PREFIX): str,
                vol.Optional(CONF_IDENTITY, default=DEFAULT_IDENTITY): str,
                vol.Optional(
                    CONF_PLAYLIST_REFRESH, default=DEFAULT_PLAYLIST_REFRESH
                ): vol.Coerce(int),
            }
        )

        return self.async_show_form(step_id="user", data_schema=schema)
