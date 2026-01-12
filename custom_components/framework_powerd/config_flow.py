"""Config flow for Framework Power Daemon integration."""
import logging
import aiohttp
import voluptuous as vol

from homeassistant import config_entries
from homeassistant.const import CONF_HOST, CONF_PORT, CONF_TOKEN, CONF_NAME
import homeassistant.helpers.config_validation as cv

from .const import DOMAIN, DEFAULT_HOST, DEFAULT_PORT, DEFAULT_NAME

_LOGGER = logging.getLogger(__name__)

STATUS_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_NAME, default=DEFAULT_NAME): str,
        vol.Required(CONF_HOST, default=DEFAULT_HOST): str,
        vol.Required(CONF_PORT, default=DEFAULT_PORT): int,
        vol.Optional(CONF_TOKEN): str,
    }
)

class FrameworkPowerConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for Framework Power Daemon."""

    VERSION = 1

    async def async_step_user(self, user_input=None):
        """Handle the initial step."""
        errors = {}

        if user_input is not None:
            # Validate connection
            valid = await self._test_connection(
                user_input[CONF_HOST],
                user_input[CONF_PORT],
                user_input.get(CONF_TOKEN),
            )

            if valid:
                return self.async_create_entry(
                    title=user_input[CONF_NAME],
                    data=user_input,
                )
            else:
                errors["base"] = "cannot_connect"

        return self.async_show_form(
            step_id="user", data_schema=STATUS_SCHEMA, errors=errors
        )

    async def _test_connection(self, host, port, token):
        """Test if we can connect to the daemon."""
        url = f"http://{host}:{port}/status"
        headers = {}
        if token:
            headers["Authorization"] = f"Bearer {token}"

        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(url, headers=headers, timeout=5) as response:
                    return response.status == 200
        except Exception:
            return False
