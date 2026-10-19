"""Select platform for Framework Power Daemon."""
from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .entity import FrameworkEntity

TUNABLE_MODES = ["performance", "powersave"]


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the select platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([FrameworkPowerTunableModeSelect(coordinator)])


class FrameworkPowerTunableModeSelect(FrameworkEntity, SelectEntity):
    """Select the daemon's performance/powersave tunables."""

    _attr_name = "Power Tunable Mode"
    _attr_unique_id = "tunable_mode_select"
    _attr_options = TUNABLE_MODES
    _attr_icon = "mdi:tune"

    @property
    def current_option(self):
        data = self.coordinator.data or {}
        mode = data.get("tunable_mode")
        if mode not in self._attr_options:
            legacy = data.get("mode")
            mode = legacy if legacy in self._attr_options else None
        return mode

    async def async_select_option(self, option: str) -> None:
        await self.coordinator.async_set_mode(option)
