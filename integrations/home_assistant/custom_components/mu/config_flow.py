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

_REQUIRED_STR = vol.All(str, vol.Strip, vol.Length(min=1))
_REFRESH_SECONDS = vol.All(vol.Coerce(int), vol.Range(min=5, max=3600))


def _schema(current: dict) -> vol.Schema:
    return vol.Schema(
        {
            vol.Optional(
                CONF_TOPIC_BASE,
                default=current.get(CONF_TOPIC_BASE, DEFAULT_TOPIC_BASE),
            ): _REQUIRED_STR,
            vol.Optional(
                CONF_DISCOVERY_PREFIX,
                default=current.get(CONF_DISCOVERY_PREFIX, DEFAULT_DISCOVERY_PREFIX),
            ): _REQUIRED_STR,
            vol.Optional(
                CONF_ENTITY_PREFIX,
                default=current.get(CONF_ENTITY_PREFIX, DEFAULT_ENTITY_PREFIX),
            ): _REQUIRED_STR,
            vol.Optional(
                CONF_IDENTITY,
                default=current.get(CONF_IDENTITY, DEFAULT_IDENTITY),
            ): _REQUIRED_STR,
            vol.Optional(
                CONF_PLAYLIST_REFRESH,
                default=current.get(CONF_PLAYLIST_REFRESH, DEFAULT_PLAYLIST_REFRESH),
            ): _REFRESH_SECONDS,
            vol.Optional(
                CONF_ARTWORK_BASE_URL,
                default=current.get(CONF_ARTWORK_BASE_URL, DEFAULT_ARTWORK_BASE_URL),
            ): str,
        }
    )


class MudOptionsFlow(config_entries.OptionsFlow):
    """Handle options for Media Utopia."""

    async def async_step_init(self, user_input=None):
        """Handle options."""
        if user_input is not None:
            return self.async_create_entry(title="", data=user_input)

        # Show effective settings: options (if previously saved) over data.
        current = {**self.config_entry.data, **self.config_entry.options}
        return self.async_show_form(step_id="init", data_schema=_schema(current))


class MudConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for Media Utopia."""

    VERSION = 1

    @staticmethod
    def async_get_options_flow(
        config_entry: config_entries.ConfigEntry,
    ) -> MudOptionsFlow:
        return MudOptionsFlow()

    async def async_step_user(self, user_input=None):
        """Handle the initial step."""
        # A second entry would fight the first over shared MQTT discovery
        # topics and domain services; allow only one.
        await self.async_set_unique_id(DOMAIN)
        self._abort_if_unique_id_configured()
        if user_input is not None:
            return self.async_create_entry(title="Media Utopia", data=user_input)

        return self.async_show_form(step_id="user", data_schema=_schema({}))
