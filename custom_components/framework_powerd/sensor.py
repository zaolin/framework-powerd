"""Platform for sensor integration."""
from homeassistant.helpers.update_coordinator import CoordinatorEntity
from homeassistant.components.sensor import SensorEntity
from homeassistant.components.binary_sensor import BinarySensorEntity
from .const import DOMAIN, CONF_CUSTOM_NAME

class FrameworkPowerModeSensor(CoordinatorEntity, SensorEntity):
    """Representation of the Power Mode Sensor."""

    def __init__(self, coordinator, entry_id):
        """Initialize the sensor."""
        super().__init__(coordinator)
        self._attr_unique_id = f"{entry_id}_power_mode"
        self._attr_name = "Framework Power Mode"
        self._attr_icon = "mdi:flash"

    @property
    def native_value(self):
        """Return the state of the sensor."""
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

class FrameworkHDMIConnectedSensor(CoordinatorEntity, BinarySensorEntity):
    """Representation of the HDMI Connected Sensor."""

    def __init__(self, coordinator, entry_id):
        """Initialize the sensor."""
        super().__init__(coordinator)
        self._attr_unique_id = f"{entry_id}_hdmi_connected"
        self._attr_name = "Framework HDMI Connected"
        self._attr_icon = "mdi:video-input-hdmi"

    @property
    def is_on(self):
        """Return true if the binary sensor is on."""
        return self.coordinator.data.get("hdmi_connected", False)

async def async_setup_entry(hass, entry, async_add_entities):
    """Set up the sensor platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities([
        FrameworkPowerModeSensor(coordinator, entry.entry_id),
        FrameworkHDMIConnectedSensor(coordinator, entry.entry_id)
    ])
