"""Diagnostics support for the Framework Power Daemon integration."""
from homeassistant.components.diagnostics import async_redact_data
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant

from .const import DOMAIN

# Keys that must never appear in a diagnostics download.
TO_REDACT = {"token", "jwt_secret", "password", "secret", "authorization"}


async def async_get_config_entry_diagnostics(
    hass: HomeAssistant, entry: ConfigEntry
) -> dict:
    """Return redacted diagnostics for a config entry."""
    coordinator = hass.data.get(DOMAIN, {}).get(entry.entry_id)

    if coordinator is None:
        return {
            "entry": async_redact_data(dict(entry.data), TO_REDACT),
            "coordinator": None,
        }

    update_interval = getattr(coordinator, "update_interval", None)
    return {
        "entry": async_redact_data(dict(entry.data), TO_REDACT),
        "coordinator": {
            "last_update_success": getattr(coordinator, "last_update_success", None),
            "last_exception": str(getattr(coordinator, "last_exception", "") or "") or None,
            "update_interval_seconds": update_interval.total_seconds()
            if update_interval
            else None,
            "data": async_redact_data(coordinator.data or {}, TO_REDACT),
        },
    }
