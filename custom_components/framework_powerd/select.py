"""Platform for select integration."""
from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN

async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the select platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([FrameworkPowerSelect(coordinator)])

class FrameworkPowerSelect(CoordinatorEntity, SelectEntity):
    """Representation of the Power Mode Select."""

    def __init__(self, coordinator):
        """Initialize the select."""
        super().__init__(coordinator)
        self._attr_name = "Framework Power Control"
        self._attr_unique_id = f"{DOMAIN}_control"
        self._attr_options = ["performance", "powersave", "auto"]

    @property
    def current_option(self):
        """Return the selected entity option to represent the entity state."""
        return self.coordinator.data.get("mode")

    async def async_select_option(self, option: str) -> None:
        """Change the selected option."""
        await self.coordinator.set_mode(option)
