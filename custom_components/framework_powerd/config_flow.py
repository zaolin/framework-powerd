"""Config flow for Framework Power Daemon integration."""
import logging
import aiohttp
import voluptuous as vol

from homeassistant import config_entries, core, exceptions
from homeassistant.const import CONF_HOST, CONF_PORT, CONF_TOKEN
from .const import DOMAIN, DEFAULT_HOST, DEFAULT_PORT, CONF_CUSTOM_NAME

_LOGGER = logging.getLogger(__name__)

DATA_SCHEMA = vol.Schema({
    vol.Required(CONF_HOST, default=DEFAULT_HOST): str,
    vol.Required(CONF_PORT, default=DEFAULT_PORT): int,
    vol.Optional(CONF_TOKEN): str,
    vol.Optional(CONF_CUSTOM_NAME, default="Framework Power"): str,
})

async def validate_input(hass: core.HomeAssistant, data):
    """Validate the user input allows us to connect."""
    url = f"http://{data[CONF_HOST]}:{data[CONF_PORT]}/status"
    headers = {}
    if data.get(CONF_TOKEN):
        headers["Authorization"] = f"Bearer {data[CONF_TOKEN]}"

    async with aiohttp.ClientSession() as session:
        try:
            async with session.get(url, headers=headers, timeout=5) as response:
                if response.status == 401:
                    raise InvalidAuth
                if response.status != 200:
                    raise CannotConnect
        except aiohttp.ClientError:
            raise CannotConnect

    return {"title": "Framework Power"}

class ConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for Framework Power Daemon."""

    VERSION = 1

    async def async_step_user(self, user_input=None):
        """Handle the initial step."""
        errors = {}
        if user_input is not None:
            try:
                info = await validate_input(self.hass, user_input)
                # Use custom name from input or default from validate_input
                title = user_input.get(CONF_CUSTOM_NAME, info.get("title", "Framework Power"))
                return self.async_create_entry(title=title, data=user_input)
            except CannotConnect:
                errors["base"] = "cannot_connect"
            except InvalidAuth:
                errors["base"] = "invalid_auth"
            except Exception:  # pylint: disable=broad-except
                _LOGGER.exception("Unexpected exception")
                errors["base"] = "unknown"

        return self.async_show_form(
            step_id="user", data_schema=DATA_SCHEMA, errors=errors
        )

    @staticmethod
    @core.callback
    def async_get_options_flow(config_entry):
        """Get the options flow for this handler."""
        return OptionsFlowHandler(config_entry)

class OptionsFlowHandler(config_entries.OptionsFlow):
    """Handle options."""

    def __init__(self, config_entry):
        """Initialize options flow."""
        self.config_entry = config_entry

    async def async_step_init(self, user_input=None):
        """Manage the options."""
        if user_input is not None:
             return self.async_create_entry(title="", data=user_input)

        return self.async_show_form(
            step_id="init",
            data_schema=vol.Schema({
                vol.Optional(
                    CONF_CUSTOM_NAME,
                    default=self.config_entry.options.get(CONF_CUSTOM_NAME, "Framework Power"),
                ): str,
            }),
        )

class CannotConnect(exceptions.HomeAssistantError):
    """Error to indicate we cannot connect."""

class InvalidAuth(exceptions.HomeAssistantError):
    """Error to indicate there is invalid auth."""
