"""Platform for select integration."""
from homeassistant.components.select import SelectEntity
from homeassistant.helpers.update_coordinator import CoordinatorEntity
from .const import DOMAIN, CONF_CUSTOM_NAME

async def async_setup_entry(hass, entry, async_add_entities):
    """Set up the select platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([
        FrameworkPowerSelect(coordinator, entry.entry_id)
    ])

class FrameworkPowerSelect(CoordinatorEntity, SelectEntity):
    """Select entity for Power Mode."""

    def __init__(self, coordinator, entry_id):
        """Initialize the select entity."""
        super().__init__(coordinator)
        self._attr_unique_id = f"{entry_id}_power_select"
        self._attr_name = "Framework Power Control"
        self._attr_icon = "mdi:speedometer"
        self._attr_options = ["performance", "powersave", "auto"]

    @property
    def current_option(self):
        """Return the selected entity option to represent the entity state."""
        return self.coordinator.data.get("mode")

    @property
    def extra_state_attributes(self):
        """Return the state attributes."""
        # Fallback to data if options not set
        custom_name = self.coordinator.config_entry.options.get(
            CONF_CUSTOM_NAME, 
            self.coordinator.config_entry.data.get(CONF_CUSTOM_NAME, "Framework Power")
        )
        return {
            "branding_name": custom_name
        }

    async def async_select_option(self, option: str) -> None:
        """Change the selected option."""
        await self.coordinator.set_mode(option)
