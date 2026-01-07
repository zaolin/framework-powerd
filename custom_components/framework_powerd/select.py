"""Platform for select integration."""
from __future__ import annotations

from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN, LOGGER
from .coordinator import FrameworkPowerCoordinator

async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the select platform."""
    coordinator: FrameworkPowerCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([FrameworkPowerModeSelect(coordinator)])


class FrameworkPowerModeSelect(CoordinatorEntity[FrameworkPowerCoordinator], SelectEntity):
    """Representation of a Framework Power Mode Selector."""

    _attr_has_entity_name = True
    _attr_name = "Power Mode"
    _attr_options = ["power-saver", "balanced", "performance"]

    def __init__(self, coordinator: FrameworkPowerCoordinator) -> None:
        """Initialize the selector."""
        super().__init__(coordinator)
        self._attr_unique_id = f"{coordinator.config_entry.entry_id}_power_mode"

    @property
    def current_option(self) -> str | None:
        """Return the selected entity option to represent the entity state."""
        # Assuming the API returns a 'mode' field
        return self.coordinator.data.get("mode")

    async def async_select_option(self, option: str) -> None:
        """Change the selected option."""
        await self.coordinator.set_mode(option)
