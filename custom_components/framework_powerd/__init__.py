"""The Framework Power Daemon integration."""
import asyncio
import logging
from datetime import timedelta

import aiohttp

from homeassistant.config_entries import ConfigEntry
from homeassistant.const import CONF_HOST, CONF_PORT, CONF_TOKEN, Platform
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.update_coordinator import (
    CoordinatorEntity,
    DataUpdateCoordinator,
    UpdateFailed,
)

from .const import DOMAIN, CONF_NOTIFY_SERVICE

_LOGGER = logging.getLogger(__name__)

PLATFORMS: list[Platform] = [Platform.SENSOR, Platform.BINARY_SENSOR, Platform.NUMBER]

async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Framework Power Daemon from a config entry."""

    host = entry.data[CONF_HOST]
    port = entry.data[CONF_PORT]
    token = entry.data.get(CONF_TOKEN)
    notify_service = entry.data.get(CONF_NOTIFY_SERVICE)

    coordinator = FrameworkPowerCoordinator(hass, host, port, token, notify_service)

    await coordinator.async_config_entry_first_refresh()

    hass.data.setdefault(DOMAIN, {})
    hass.data[DOMAIN][entry.entry_id] = coordinator

    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)

    return True

async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a config entry."""
    if unload_ok := await hass.config_entries.async_unload_platforms(entry, PLATFORMS):
        coordinator = hass.data[DOMAIN].get(entry.entry_id)
        if coordinator is not None:
            coordinator.shutdown_dynamic_registrar()
        hass.data[DOMAIN].pop(entry.entry_id)

    return unload_ok


class FrameworkPowerCoordinator(DataUpdateCoordinator):
    """Class to manage fetching data from the API."""

    def __init__(self, hass, host, port, token, notify_service=None):
        """Initialize."""
        self.host = host
        self.port = port
        self.token = token
        self.notify_service = notify_service
        self.base_url = f"http://{host}:{port}"
        self.headers = {}
        if token:
            self.headers["Authorization"] = f"Bearer {token}"

        # A11: dynamic entity registration. Platforms register factories here;
        # each factory returns a list of entities to add on the update where it
        # first becomes applicable, or an empty list to keep waiting. Once a
        # factory yields entities it is removed so it isn't called again.
        self._entity_factories: list[tuple[callable, bool]] = []
        self._dynamic_unsub = None

        super().__init__(
            hass,
            _LOGGER,
            name=DOMAIN,
            update_interval=timedelta(seconds=10),
        )

    def register_entity_factory(self, factory: callable) -> None:
        """Register a dynamic entity factory.

        factory is called with (self) after each successful update and must
        return a list of entity instances to add, or an empty list to keep
        waiting. Once it returns a non-empty list it is removed.
        """
        self._entity_factories.append([factory, False])

    def attach_dynamic_registrar(self, add_entities_cb: callable) -> None:
        """Attach the update listener that drains pending factories.

        add_entities_cb is the async_add_entities callback from the sensor
        platform's async_setup_entry. It is called with the entities produced
        by any factory that becomes applicable on an update.
        """
        if self._dynamic_unsub is not None:
            return  # already attached

        @callback
        def _on_update():
            if not self.last_update_success or self.data is None:
                return
            pending: list = []
            for entry in self._entity_factories:
                factory, done = entry
                if done:
                    continue
                try:
                    entities = factory(self) or []
                except Exception:  # pragma: no cover - defensive
                    _LOGGER.exception("dynamic entity factory raised; skipping")
                    entities = []
                if entities:
                    entry[1] = True
                    pending.extend(entities)
            if pending:
                add_entities_cb(pending)

        self._dynamic_unsub = self.async_add_listener(_on_update)

    def shutdown_dynamic_registrar(self) -> None:
        """Detach the update listener (called on unload)."""
        if self._dynamic_unsub is not None:
            self._dynamic_unsub()
            self._dynamic_unsub = None

    async def _async_update_data(self):
        """Fetch data from API endpoints."""
        try:
            async with asyncio.timeout(10):
                async with aiohttp.ClientSession() as session:
                    # Fetch main status (may already include ollama data
                    # if the daemon has Ollama monitoring enabled).
                    async with session.get(
                        f"{self.base_url}/status", headers=self.headers
                    ) as response:
                        if response.status != 200:
                            raise UpdateFailed(f"Error fetching status: {response.status}")
                        data = await response.json()

                    # Fix 2: Only fetch /ollama/stats separately if /status
                    # didn't already include ollama data.
                    if "ollama" not in data:
                        try:
                            async with session.get(
                                f"{self.base_url}/ollama/stats", headers=self.headers
                            ) as response:
                                if response.status == 200:
                                    data["ollama"] = await response.json()
                                else:
                                    _LOGGER.debug(
                                        "Ollama stats endpoint returned %d "
                                        "(Ollama monitoring may not be enabled in the daemon config)",
                                        response.status,
                                    )
                        except Exception as err:
                            _LOGGER.debug("Ollama stats fetch failed: %s", err)

                    # Fix 3: Warn once if Ollama data is still missing after
                    # both the /status and /ollama/stats attempts.
                    if "ollama" not in data:
                        if not getattr(self, "_ollama_missing_warned", False):
                            _LOGGER.warning(
                                "Ollama data not available. Enable Ollama monitoring "
                                "in the daemon config (ollama.enabled: true) or ensure "
                                "Ollama is running on localhost:11434."
                            )
                            self._ollama_missing_warned = True
                    else:
                        self._ollama_missing_warned = False

                    # Fix 4: Direct Ollama API fallback. If the daemon doesn't
                    # provide Ollama data (monitoring disabled), but Ollama is
                    # running locally, query /api/ps and /api/version directly
                    # so the HA integration can still show running models,
                    # VRAM usage, and version.
                    if "ollama" not in data:
                        ollama_data = await self._fetch_ollama_direct(session)
                        if ollama_data is not None:
                            data["ollama"] = ollama_data
                            _LOGGER.info(
                                "Ollama data fetched directly from Ollama API "
                                "(daemon monitoring not enabled)"
                            )

                    return data
        except Exception as err:
            raise UpdateFailed(f"Error communicating with API: {err}")

    async def _fetch_ollama_direct(self, session: aiohttp.ClientSession) -> dict | None:
        """Query Ollama's API directly as a fallback when the daemon doesn't
        provide Ollama stats. Returns a dict with models, loaded_vram_bytes,
        and ollama_version, or None if Ollama is unreachable.
        """
        ollama_url = "http://localhost:11434"
        result: dict = {"models": [], "loaded_vram_bytes": 0, "ollama_version": ""}

        try:
            # /api/ps — running models
            async with session.get(
                f"{ollama_url}/api/ps", timeout=aiohttp.ClientTimeout(total=3)
            ) as resp:
                if resp.status == 200:
                    ps_data = await resp.json()
                    models = ps_data.get("models", [])
                    result["models"] = models
                    total_vram = sum(m.get("size_vram", 0) for m in models)
                    result["loaded_vram_bytes"] = total_vram
                else:
                    return None
        except Exception:
            return None

        try:
            # /api/version — Ollama version
            async with session.get(
                f"{ollama_url}/api/version", timeout=aiohttp.ClientTimeout(total=3)
            ) as resp:
                if resp.status == 200:
                    ver_data = await resp.json()
                    result["ollama_version"] = ver_data.get("version", "")
        except Exception:
            pass  # version is optional

        # Only return if we got at least the models endpoint
        if result["models"] or result["ollama_version"]:
            return result
        return None

