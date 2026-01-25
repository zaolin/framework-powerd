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
from homeassistant.core import HomeAssistant, callback
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

    # Add Ollama per-group sensors if available
    ollama_data = coordinator.data.get("ollama", {})
    by_group = ollama_data.get("by_group", {})
    currency = ollama_data.get("currency", "EUR")

    for group_name in by_group.keys():
        entities.extend([
            OllamaGroupRequestsSensor(coordinator, group_name),
            OllamaGroupEnergySensor(coordinator, group_name),
            OllamaGroupCostSensor(coordinator, group_name, currency),
        ])

    # Also add ungrouped if present
    if ollama_data.get("ungrouped", {}).get("count", 0) > 0:
        entities.extend([
            OllamaGroupRequestsSensor(coordinator, "ungrouped"),
            OllamaGroupEnergySensor(coordinator, "ungrouped"),
            OllamaGroupCostSensor(coordinator, "ungrouped", currency),
        ])

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
        val = self.coordinator.data.get("power", {}).get(self._key, 0)
        return round(val, 3)


# Ollama Per-Group Sensors

class OllamaGroupRequestsSensor(FrameworkEntity, SensorEntity):
    """Ollama requests per group."""
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_icon = "mdi:message-processing"

    def __init__(self, coordinator, group_name):
        super().__init__(coordinator)
        self._group = group_name
        self._attr_name = f"Ollama {group_name.title()} Requests"
        self._attr_unique_id = f"ollama_{group_name}_requests"

    @property
    def native_value(self):
        ollama = self.coordinator.data.get("ollama", {})
        if self._group == "ungrouped":
            return ollama.get("ungrouped", {}).get("count", 0)
        return ollama.get("by_group", {}).get(self._group, {}).get("count", 0)


class OllamaGroupEnergySensor(FrameworkEntity, SensorEntity):
    """Ollama energy per group."""
    _attr_device_class = SensorDeviceClass.ENERGY
    _attr_native_unit_of_measurement = UnitOfEnergy.KILO_WATT_HOUR
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_icon = "mdi:lightning-bolt"

    def __init__(self, coordinator, group_name):
        super().__init__(coordinator)
        self._group = group_name
        self._attr_name = f"Ollama {group_name.title()} Energy"
        self._attr_unique_id = f"ollama_{group_name}_energy"

    @property
    def native_value(self):
        ollama = self.coordinator.data.get("ollama", {})
        if self._group == "ungrouped":
            val = ollama.get("ungrouped", {}).get("total_energy_kwh", 0)
        else:
            val = ollama.get("by_group", {}).get(self._group, {}).get("total_energy_kwh", 0)
        return round(val, 6)


class OllamaGroupCostSensor(FrameworkEntity, SensorEntity):
    """Ollama cost per group."""
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_icon = "mdi:currency-eur"

    def __init__(self, coordinator, group_name, currency):
        super().__init__(coordinator)
        self._group = group_name
        self._currency = currency
        self._attr_name = f"Ollama {group_name.title()} Cost"
        self._attr_unique_id = f"ollama_{group_name}_cost"
        self._attr_native_unit_of_measurement = currency

    @property
    def native_value(self):
        ollama = self.coordinator.data.get("ollama", {})
        if self._group == "ungrouped":
            val = ollama.get("ungrouped", {}).get("total_cost", 0)
        else:
            val = ollama.get("by_group", {}).get(self._group, {}).get("total_cost", 0)
        return round(val, 4)

