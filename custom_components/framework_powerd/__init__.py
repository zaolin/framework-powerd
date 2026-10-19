"""The Framework Power Daemon integration."""
import asyncio
import logging
from datetime import datetime, timedelta, timezone

import aiohttp

from homeassistant.components import persistent_notification
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

        # Alert notifications: deduplicate per alert key and keep a cooldown
        # so a persistent warning does not spam the phone every poll cycle.
        self._notified_alerts: dict[str, datetime] = {}
        self._notify_cooldown = timedelta(hours=6)

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
        """Fetch data from the daemon."""
        try:
            async with asyncio.timeout(10):
                async with aiohttp.ClientSession() as session:
                    async with session.get(
                        f"{self.base_url}/status", headers=self.headers
                    ) as response:
                        if response.status != 200:
                            raise UpdateFailed(f"Error fetching status: {response.status}")
                        data = await response.json()
            try:
                await self._process_alerts(data)
            except Exception:  # pragma: no cover - defensive
                _LOGGER.exception("Alert notification failed; continuing")
            return data
        except Exception as err:
            raise UpdateFailed(f"Error communicating with API: {err}")

    @staticmethod
    def _collect_alerts(data: dict) -> list[tuple[str, str, str, str]]:
        """Extract actionable alerts from the daemon payload.

        Returns (dedup_key, severity, title, message) tuples. Severity is part
        of the key so escalation from warning to critical notifies again.
        """
        alerts: list[tuple[str, str, str, str]] = []

        smartd = data.get("smartd") or {}
        for alert in smartd.get("alerts") or []:
            severity = alert.get("severity") or "warning"
            if severity not in ("warning", "critical"):
                continue
            key = alert.get("id") or f"{alert.get('device')}|{alert.get('fail_type')}"
            device = alert.get("device", "disk")
            message = alert.get("message", "SMART alert")
            alerts.append(
                (f"smartd:{key}:{severity}", severity, f"SMART alert on {device}", message)
            )

        nvme = data.get("nvme") or {}
        for device in nvme.get("devices") or []:
            severity = device.get("health")
            if severity not in ("warning", "critical"):
                continue
            dev = device.get("device", "NVMe")
            issues = "; ".join(device.get("issues") or []) or severity
            model = device.get("model") or "NVMe"
            alerts.append(
                (
                    f"nvme:{dev}:{severity}",
                    severity,
                    f"NVMe health {severity} on {dev}",
                    f"{model} ({dev}): {issues}",
                )
            )

        return alerts

    @staticmethod
    def _parse_notify_service(value: str) -> tuple[str, str]:
        """Return (domain, service) for a configured notify service."""
        value = (value or "").strip()
        if "." in value:
            domain, service = value.split(".", 1)
            return domain or "notify", service
        return "notify", value

    @staticmethod
    def _notification_id(key: str) -> str:
        safe = "".join(ch if ch.isalnum() else "_" for ch in key)
        return f"{DOMAIN}_{safe}"[:120]

    async def _process_alerts(self, data: dict) -> None:
        """Create persistent notifications and optionally call notify.*.

        Alerts are deduplicated per key and severity, with a cooldown. The
        daemon already keeps alerts sticky, so this only fires when a new
        condition (or an escalation) appears.
        """
        now = datetime.now(timezone.utc)
        for key, severity, title, message in self._collect_alerts(data):
            last = self._notified_alerts.get(key)
            if last is not None and now - last < self._notify_cooldown:
                continue
            self._notified_alerts[key] = now

            prefix = "CRITICAL: " if severity == "critical" else "Warning: "
            persistent_notification.async_create(
                self.hass,
                message,
                title=prefix + title,
                notification_id=self._notification_id(key),
            )

            if not self.notify_service:
                notify_service = (data.get("smartd") or {}).get("notify_service")
            else:
                notify_service = self.notify_service
            if not notify_service:
                continue
            domain, service = self._parse_notify_service(notify_service)
            try:
                await self.hass.services.async_call(
                    domain,
                    service,
                    {"title": prefix + title, "message": message},
                    blocking=False,
                )
            except Exception as err:  # pragma: no cover - defensive
                _LOGGER.warning("Could not call %s.%s: %s", domain, service, err)
