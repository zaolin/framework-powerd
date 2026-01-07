"""Platform for sensor integration."""
from homeassistant.components.sensor import SensorEntity
from homeassistant.components.binary_sensor import BinarySensorEntity, BinarySensorDeviceClass
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
    """Set up the sensor platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([
        FrameworkPowerModeSensor(coordinator),
        FrameworkHDMIConnectedSensor(coordinator)
    ])

class FrameworkPowerModeSensor(CoordinatorEntity, SensorEntity):
    """Representation of the Power Mode Sensor."""

    def __init__(self, coordinator):
        """Initialize the sensor."""
        super().__init__(coordinator)
        self._attr_name = "Framework Power Mode"
        self._attr_unique_id = f"{DOMAIN}_mode"

    @property
    def native_value(self):
        """Return the state of the sensor."""
        return self.coordinator.data.get("mode")

class FrameworkHDMIConnectedSensor(CoordinatorEntity, BinarySensorEntity):
    """Representation of the HDMI Connected Sensor."""

    def __init__(self, coordinator):
        """Initialize the sensor."""
        super().__init__(coordinator)
        self._attr_name = "Framework HDMI"
        self._attr_unique_id = f"{DOMAIN}_hdmi"
        self._attr_device_class = BinarySensorDeviceClass.CONNECTIVITY

    @property
    def is_on(self):
        """Return true if the binary sensor is on."""
        return self.coordinator.data.get("hdmi_connected")
