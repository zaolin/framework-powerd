"""Coordinator for Framework Power Daemon."""
from __future__ import annotations

from datetime import timedelta
import logging

import aiohttp
import async_timeout

from homeassistant.core import HomeAssistant
from homeassistant.helpers.update_coordinator import (
    DataUpdateCoordinator,
    UpdateFailed,
)

from .const import DOMAIN, LOGGER, DEFAULT_SCAN_INTERVAL

class FrameworkPowerCoordinator(DataUpdateCoordinator):
    """Class to manage fetching data from the API."""

    def __init__(self, hass: HomeAssistant, api_url: str, token: str | None) -> None:
        """Initialize."""
        self.api_url = api_url
        self.token = token
        self.headers = {}
        if token:
            self.headers["Authorization"] = f"Bearer {token}"

        super().__init__(
            hass,
            LOGGER,
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

    async def set_mode(self, mode: str) -> None:
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
            LOGGER.error("Failed to set mode: %s", err)
            raise
