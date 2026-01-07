"""Platform for sensor integration."""
from __future__ import annotations

from homeassistant.components.sensor import SensorEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN
from .coordinator import FrameworkPowerCoordinator

async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the sensor platform."""
    coordinator: FrameworkPowerCoordinator = hass.data[DOMAIN][entry.entry_id]
    
    entities = []
    # Add sensors based on API response structure
    # Assuming the API returns keys like "battery_level", "power_consumption", etc.
    # We will create a generic sensor for now that dumps all status or specific known fields if we knew them.
    # Based on previous code, it seemed to just dump everything. 
    # Let's check what the coordinator data looks like in a real scenario, 
    # but for now we'll implement a flexible sensor set.
    
    # We will instantiate sensors when data is available.
    if coordinator.data:
        for key in coordinator.data:
             entities.append(FrameworkPowerSensor(coordinator, key))

    async_add_entities(entities)


class FrameworkPowerSensor(CoordinatorEntity[FrameworkPowerCoordinator], SensorEntity):
    """Representation of a Framework Power Sensor."""

    def __init__(self, coordinator: FrameworkPowerCoordinator, key: str) -> None:
        """Initialize the sensor."""
        super().__init__(coordinator)
        self._key = key
        self._attr_has_entity_name = True
        self._attr_name = key.replace("_", " ").title()
        self._attr_unique_id = f"{coordinator.config_entry.entry_id}_{key}"

    @property
    def native_value(self) -> str | int | float | None:
        """Return the state of the sensor."""
        return self.coordinator.data.get(self._key)
