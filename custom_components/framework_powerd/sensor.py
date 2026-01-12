"""Sensor platform for Framework Power Daemon."""
from homeassistant.components.sensor import (
    SensorDeviceClass,
    SensorEntity,
    SensorStateClass,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import (
    UnitOfPower,
    UnitOfEnergy,
    UnitOfTime,
)
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

    entities = [
        PowerModeSensor(coordinator),
        GamePIDSensor(coordinator),
        UptimeSensor(coordinator),
        # Power Sensors
        PowerSensor(coordinator, "pkg_watt", "Package Power", "pkg"),
        PowerSensor(coordinator, "cor_watt", "Core Power", "cor"),
        PowerSensor(coordinator, "ram_watt", "RAM Power", "ram"),
        # Energy Sensors (kWh)
        EnergySensor(coordinator, "energy_24h_kwh", "Energy (24h)", "24h"),
        EnergySensor(coordinator, "energy_7d_kwh", "Energy (7 Days)", "7d"),
    ]

    async_add_entities(entities)


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

class PowerModeSensor(FrameworkEntity, SensorEntity):
    _attr_name = "Power Mode"
    _attr_unique_id = "power_mode"
    _attr_icon = "mdi:speedometer"

    @property
    def native_value(self):
        return self.coordinator.data.get("mode")

class GamePIDSensor(FrameworkEntity, SensorEntity):
    _attr_name = "Game PID"
    _attr_unique_id = "game_pid"
    _attr_icon = "mdi:identifier"

    @property
    def native_value(self):
        return self.coordinator.data.get("game_pid")

class UptimeSensor(FrameworkEntity, SensorEntity):
    _attr_name = "Uptime"
    _attr_unique_id = "uptime"
    _attr_device_class = SensorDeviceClass.DURATION
    _attr_native_unit_of_measurement = UnitOfTime.SECONDS
    _attr_icon = "mdi:clock-outline"

    @property
    def native_value(self):
        # API returns float64 seconds
        return self.coordinator.data.get("uptime_seconds")

class PowerSensor(FrameworkEntity, SensorEntity):
    _attr_device_class = SensorDeviceClass.POWER
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_state_class = SensorStateClass.MEASUREMENT

    def __init__(self, coordinator, key, name, suffix):
        super().__init__(coordinator)
        self._key = key
        self._attr_name = name
        self._attr_unique_id = f"power_{suffix}"

    @property
    def native_value(self):
        power_data = self.coordinator.data.get("power", {}).get("current", {})
        return power_data.get(self._key)

class EnergySensor(FrameworkEntity, SensorEntity):
    _attr_device_class = SensorDeviceClass.ENERGY
    _attr_native_unit_of_measurement = UnitOfEnergy.KILO_WATT_HOUR
    _attr_state_class = SensorStateClass.TOTAL

    def __init__(self, coordinator, key, name, suffix):
        super().__init__(coordinator)
        self._key = key
        self._attr_name = name
        self._attr_unique_id = f"energy_{suffix}"

    @property
    def native_value(self):
        # API returns kWh directly now
        val = self.coordinator.data.get("power", {}).get(self._key, 0)
        return round(val, 3)
