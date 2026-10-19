"""Button platform for Framework Power Daemon."""
from homeassistant.components.button import ButtonEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .entity import FrameworkEntity


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the button platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([FrameworkPowerResetIdleButton(coordinator)])


class FrameworkPowerResetIdleButton(FrameworkEntity, ButtonEntity):
    """Reset the daemon idle timer."""

    _attr_name = "Reset Idle Timer"
    _attr_unique_id = "reset_idle"
    _attr_icon = "mdi:gesture-tap"

    async def async_press(self) -> None:
        await self.coordinator.async_reset_idle()
