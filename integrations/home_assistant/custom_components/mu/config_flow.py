"""Config flow for the Mud integration."""

import voluptuous as vol

from homeassistant import config_entries

from .const import (
    CONF_ARTWORK_BASE_URL,
    CONF_DISCOVERY_PREFIX,
    CONF_ENTITY_PREFIX,
    CONF_IDENTITY,
    CONF_PLAYLIST_REFRESH,
    CONF_TOPIC_BASE,
    DEFAULT_ARTWORK_BASE_URL,
    DEFAULT_DISCOVERY_PREFIX,
    DEFAULT_ENTITY_PREFIX,
    DEFAULT_IDENTITY,
    DEFAULT_PLAYLIST_REFRESH,
    DEFAULT_TOPIC_BASE,
    DOMAIN,
)


class MudOptionsFlow(config_entries.OptionsFlow):
    """Handle options for Media Utopia."""

    def __init__(self, config_entry: config_entries.ConfigEntry) -> None:
        self.config_entry = config_entry

    async def async_step_init(self, user_input=None):
        """Handle options."""
        if user_input is not None:
            return self.async_create_entry(title="", data=user_input)

        current = self.config_entry.data
        schema = vol.Schema(
            {
                vol.Optional(
                    CONF_TOPIC_BASE,
                    default=current.get(CONF_TOPIC_BASE, DEFAULT_TOPIC_BASE),
                ): str,
                vol.Optional(
                    CONF_DISCOVERY_PREFIX,
                    default=current.get(CONF_DISCOVERY_PREFIX, DEFAULT_DISCOVERY_PREFIX),
                ): str,
                vol.Optional(
                    CONF_ENTITY_PREFIX,
                    default=current.get(CONF_ENTITY_PREFIX, DEFAULT_ENTITY_PREFIX),
                ): str,
                vol.Optional(
                    CONF_IDENTITY,
                    default=current.get(CONF_IDENTITY, DEFAULT_IDENTITY),
                ): str,
                vol.Optional(
                    CONF_PLAYLIST_REFRESH,
                    default=current.get(CONF_PLAYLIST_REFRESH, DEFAULT_PLAYLIST_REFRESH),
                ): vol.Coerce(int),
                vol.Optional(
                    CONF_ARTWORK_BASE_URL,
                    default=current.get(CONF_ARTWORK_BASE_URL, DEFAULT_ARTWORK_BASE_URL),
                ): str,
            }
        )
        return self.async_show_form(step_id="init", data_schema=schema)


class MudConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for Media Utopia."""

    VERSION = 1

    @staticmethod
    def async_get_options_flow(
        config_entry: config_entries.ConfigEntry,
    ) -> MudOptionsFlow:
        return MudOptionsFlow(config_entry)

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
                vol.Optional(CONF_ARTWORK_BASE_URL, default=DEFAULT_ARTWORK_BASE_URL): str,
            }
        )

        return self.async_show_form(step_id="user", data_schema=schema)
