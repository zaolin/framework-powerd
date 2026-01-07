"""The Framework Power Daemon integration."""
import logging
import asyncio
import async_timeout
import aiohttp
import os
from datetime import timedelta
from homeassistant.components.http import StaticPathConfig

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.const import CONF_HOST, CONF_PORT, CONF_TOKEN
from homeassistant.helpers.update_coordinator import (
    DataUpdateCoordinator,
    UpdateFailed,
)

from .const import DOMAIN, DEFAULT_SCAN_INTERVAL

_LOGGER = logging.getLogger(__name__)

PLATFORMS = ["sensor", "select"]

async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Framework Power Daemon from a config entry."""
    hass.data.setdefault(DOMAIN, {})

    host = entry.data[CONF_HOST]
    port = entry.data[CONF_PORT]
    token = entry.data.get(CONF_TOKEN)

    api_url = f"http://{host}:{port}"

    coordinator = FrameworkPowerCoordinator(hass, api_url, token)
    await coordinator.async_config_entry_first_refresh()

    hass.data[DOMAIN][entry.entry_id] = coordinator

    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)

    entry.async_on_unload(entry.add_update_listener(update_listener))
    
    # Register custom card and logo
    # We use os.path.dirname to be relative to this file
    component_dir = os.path.dirname(__file__)
    card_path = os.path.join(component_dir, "framework-power-card.js")
    logo_path = os.path.join(component_dir, "logo.png")
    
    # Debug logging to verify paths
    _LOGGER.debug(f"Registering static paths: card={card_path}, logo={logo_path}")

    # Register paths regardless of existence check to ensure HA attempts to serve them
    # and to surface permission errors if any.
    await hass.http.async_register_static_paths([
        StaticPathConfig("/framework_powerd/card.js", card_path, False),
        StaticPathConfig("/framework_powerd/logo.png", logo_path, False)
    ])

    return True

async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a config entry."""
    if unload_ok := await hass.config_entries.async_unload_platforms(entry, PLATFORMS):
        hass.data[DOMAIN].pop(entry.entry_id)

    return unload_ok

async def update_listener(hass: HomeAssistant, entry: ConfigEntry) -> None:
    """Update listener."""
    await hass.config_entries.async_reload(entry.entry_id)

class FrameworkPowerCoordinator(DataUpdateCoordinator):
    """Class to manage fetching data from the API."""

    def __init__(self, hass, api_url, token):
        """Initialize."""
        self.api_url = api_url
        self.token = token
        self.headers = {}
        if token:
            self.headers["Authorization"] = f"Bearer {token}"

        super().__init__(
            hass,
            _LOGGER,
            name=DOMAIN,
            update_interval=timedelta(seconds=DEFAULT_SCAN_INTERVAL),
        )

    async def _async_update_data(self):
        """Fetch data from API endpoint."""
        try:
            async with async_timeout.timeout(10):
                async with aiohttp.ClientSession() as session:
                    async with session.get(
                        f"{self.api_url}/status", headers=self.headers
                    ) as response:
                        if response.status == 401:
                             raise UpdateFailed("Authentication failed")
                        response.raise_for_status()
                        return await response.json()
        except Exception as err:
            raise UpdateFailed(f"Error communicating with API: {err}")

    async def set_mode(self, mode):
        """Set the power mode."""
        try:
            async with async_timeout.timeout(10):
                async with aiohttp.ClientSession() as session:
                    async with session.post(
                        f"{self.api_url}/mode",
                        json={"mode": mode},
                        headers=self.headers,
                    ) as response:
                        response.raise_for_status()
                        await self.async_request_refresh()
        except Exception as err:
            _LOGGER.error("Failed to set mode: %s", err)
