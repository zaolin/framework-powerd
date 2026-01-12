"""The Framework Power Daemon integration."""
import asyncio
import logging
from datetime import timedelta

import aiohttp
import async_timeout

from homeassistant.config_entries import ConfigEntry
from homeassistant.const import CONF_HOST, CONF_PORT, CONF_TOKEN, Platform
from homeassistant.core import HomeAssistant
from homeassistant.helpers.update_coordinator import (
    CoordinatorEntity,
    DataUpdateCoordinator,
    UpdateFailed,
)

from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)

PLATFORMS: list[Platform] = [Platform.SENSOR, Platform.BINARY_SENSOR]

async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Framework Power Daemon from a config entry."""

    host = entry.data[CONF_HOST]
    port = entry.data[CONF_PORT]
    token = entry.data.get(CONF_TOKEN)

    coordinator = FrameworkPowerCoordinator(hass, host, port, token)

    await coordinator.async_config_entry_first_refresh()

    hass.data.setdefault(DOMAIN, {})
    hass.data[DOMAIN][entry.entry_id] = coordinator

    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)

    return True

async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a config entry."""
    if unload_ok := await hass.config_entries.async_unload_platforms(entry, PLATFORMS):
        hass.data[DOMAIN].pop(entry.entry_id)

    return unload_ok


class FrameworkPowerCoordinator(DataUpdateCoordinator):
    """Class to manage fetching data from the API."""

    def __init__(self, hass, host, port, token):
        """Initialize."""
        self.host = host
        self.port = port
        self.token = token
        self.url = f"http://{host}:{port}/status"
        self.headers = {}
        if token:
            self.headers["Authorization"] = f"Bearer {token}"

        super().__init__(
            hass,
            _LOGGER,
            name=DOMAIN,
            update_interval=timedelta(seconds=10),
        )

    async def _async_update_data(self):
        """Fetch data from API endpoint."""
        try:
            async with async_timeout.timeout(5):
                async with aiohttp.ClientSession() as session:
                    async with session.get(self.url, headers=self.headers) as response:
                        if response.status != 200:
                            raise UpdateFailed(f"Error fetching data: {response.status}")
                        return await response.json()
        except Exception as err:
            raise UpdateFailed(f"Error communicating with API: {err}")
