"""Number platform for Framework Power Daemon."""
from datetime import timedelta
import logging

from homeassistant.components.number import (
    NumberEntity,
    NumberMode,
    RestoreNumber,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import UnitOfTime
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)

async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the number platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([FrameworkPowerPollingInterval(coordinator)])


class FrameworkEntity(CoordinatorEntity):
    """Base entity."""

    def __init__(self, coordinator):
        super().__init__(coordinator)
        self._attr_has_entity_name = True
    
    @property
    def device_info(self):
        return {
            "identifiers": {(DOMAIN, f"{self.coordinator.host}:{self.coordinator.port}")},
            "name": "Framework Power Daemon",
            "manufacturer": "Framework",
            "model": "Power Daemon",
        }

class FrameworkPowerPollingInterval(FrameworkEntity, RestoreNumber):
    """Number entity to configure the polling interval."""
    
    _attr_name = "Polling Interval"
    _attr_unique_id = "polling_interval"
    _attr_native_min_value = 1
    _attr_native_max_value = 600
    _attr_native_step = 1
    _attr_native_unit_of_measurement = UnitOfTime.SECONDS
    _attr_mode = NumberMode.BOX
    _attr_icon = "mdi:timer-cog"

    def __init__(self, coordinator):
        """Initialize the entity."""
        super().__init__(coordinator)
        self._attr_native_value = coordinator.update_interval.total_seconds()

    async def async_added_to_hass(self) -> None:
        """Handle entity which will be added."""
        await super().async_added_to_hass()
        last_state = await self.async_get_last_number_data()
        if last_state and last_state.native_value is not None:
            new_interval = last_state.native_value
            self._attr_native_value = new_interval
            self.coordinator.update_interval = timedelta(seconds=new_interval)
            _LOGGER.debug(f"Restored polling interval: {new_interval}s")

    async def async_set_native_value(self, value: float) -> None:
        """Update the current value."""
        self._attr_native_value = value
        self.coordinator.update_interval = timedelta(seconds=value)
        self.async_write_ha_state()
        _LOGGER.info(f"Set polling interval to: {value}s")
