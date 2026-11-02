"""Select platform for Framework Power Daemon."""
from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN, FAN_MODES, NPU_POWER_MODES
from .entity import FrameworkEntity

TUNABLE_MODES = ["performance", "powersave"]


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the select platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        [
            FrameworkPowerTunableModeSelect(coordinator),
            FrameworkPowerNpuPowerModeSelect(coordinator),
            FrameworkPowerFanModeSelect(coordinator),
        ]
    )


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


class FrameworkPowerNpuPowerModeSelect(FrameworkEntity, SelectEntity):
    """Select the AMD XDNA NPU power mode."""

    _attr_name = "NPU Power Mode"
    _attr_unique_id = "npu_power_mode_select"
    _attr_options = NPU_POWER_MODES
    _attr_icon = "mdi:expansion-card"

    @property
    def _npu(self):
        return (self.coordinator.data or {}).get("npu") or {}

    @property
    def available(self):
        return bool(self._npu.get("present"))

    @property
    def current_option(self):
        mode = self._npu.get("power_mode")
        return mode if mode in self._attr_options else None

    async def async_select_option(self, option: str) -> None:
        await self.coordinator.async_set_npu_power_mode(option)


class FrameworkPowerFanModeSelect(FrameworkEntity, SelectEntity):
    """Select between the EC fan curve and the daemon curve."""

    _attr_name = "Fan Mode"
    _attr_unique_id = "fan_mode_select"
    _attr_options = FAN_MODES
    _attr_icon = "mdi:fan"

    @property
    def _fan(self):
        return (self.coordinator.data or {}).get("fan") or {}

    @property
    def available(self):
        return (
            self.coordinator.last_update_success
            and bool(self._fan.get("enabled"))
            and bool(self._fan.get("supported", True))
        )

    @property
    def current_option(self):
        mode = self._fan.get("mode")
        return mode if mode in self._attr_options else None

    async def async_select_option(self, option: str) -> None:
        await self.coordinator.async_set_fan_mode(option)
