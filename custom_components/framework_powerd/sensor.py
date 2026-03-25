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

    # Add SMART sensors
    entities.append(SmartdAlertsSensor(coordinator))

    # Add per-device SMART sensors dynamically based on active alerts
    smartd_data = coordinator.data.get("smartd", {})
    for alert in smartd_data.get("alerts", []):
        device = alert.get("device")
        if device:
            entities.append(SmartdDeviceSensor(coordinator, device))

    # Add GPU sensors
    gpu_data = coordinator.data.get("gpu", {})
    if gpu_data:
        entities.extend([
            GPUTemperatureSensor(coordinator),
            GPUPowerSensor(coordinator),
            GPUVRAMUsedSensor(coordinator),
            GPUVRAMTotalSensor(coordinator),
            GPUTTGUsedSensor(coordinator),
            GPUCPUUsageSensor(coordinator),
        ])

    # Add Ollama model sensors
    ollama_data = coordinator.data.get("ollama", {})
    if ollama_data.get("models"):
        entities.append(OllamaModelsSensor(coordinator))
        entities.append(OllamaVRAMSensor(coordinator))

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


# SMART Sensors

class SmartdAlertsSensor(FrameworkEntity, SensorEntity):
    """SMART alerts count sensor."""
    _attr_name = "SMART Alerts"
    _attr_icon = "mdi:alert"
    _attr_unique_id = "smartd_alerts_count"

    @property
    def native_value(self):
        alerts = self.coordinator.data.get("smartd", {}).get("alerts", [])
        return len(alerts)

    @property
    def extra_state_attributes(self):
        return {"alerts": self.coordinator.data.get("smartd", {}).get("alerts", [])}


class SmartdDeviceSensor(FrameworkEntity, SensorEntity):
    """Per-device SMART alert sensor."""
    _attr_icon = "mdi:harddisk"

    def __init__(self, coordinator, device_name):
        super().__init__(coordinator)
        self._device = device_name
        self._attr_name = f"SMART {device_name}"
        self._attr_unique_id = f"smartd_{device_name.replace('/', '_')}"

    @property
    def native_value(self):
        alerts = self.coordinator.data.get("smartd", {}).get("alerts", [])
        if any(a.get("device") == self._device for a in alerts):
            return 1
        return 0

    @property
    def extra_state_attributes(self):
        alerts = self.coordinator.data.get("smartd", {}).get("alerts", [])
        device_alerts = [a for a in alerts if a.get("device") == self._device]
        return {"alerts": device_alerts}


# GPU Sensors

class GPUTemperatureSensor(FrameworkEntity, SensorEntity):
    """GPU temperature sensor."""
    _attr_name = "GPU Temperature"
    _attr_unique_id = "gpu_temperature"
    _attr_device_class = SensorDeviceClass.TEMPERATURE
    _attr_native_unit_of_measurement = "°C"
    _attr_icon = "mdi:thermometer"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        return gpu_data.get("temperature_celsius")


class GPUPowerSensor(FrameworkEntity, SensorEntity):
    """GPU power sensor."""
    _attr_name = "GPU Power"
    _attr_unique_id = "gpu_power"
    _attr_device_class = SensorDeviceClass.POWER
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_icon = "mdi:flash"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        return gpu_data.get("power_watts")


class GPUVRAMUsedSensor(FrameworkEntity, SensorEntity):
    """GPU VRAM used sensor."""
    _attr_name = "GPU VRAM Used"
    _attr_unique_id = "gpu_vram_used"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        vram_bytes = gpu_data.get("vram_used_bytes", 0)
        return round(vram_bytes / (1024**3), 2)


class GPUVRAMTotalSensor(FrameworkEntity, SensorEntity):
    """GPU VRAM total sensor."""
    _attr_name = "GPU VRAM Total"
    _attr_unique_id = "gpu_vram_total"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        vram_bytes = gpu_data.get("vram_total_bytes", 0)
        return round(vram_bytes / (1024**3), 2)


class GPUTTGUsedSensor(FrameworkEntity, SensorEntity):
    """GPU GTT used sensor."""
    _attr_name = "GPU GTT Used"
    _attr_unique_id = "gpu_gtt_used"
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        gtt_bytes = gpu_data.get("gtt_used_bytes", 0)
        return round(gtt_bytes / (1024**2), 2)


class GPUCPUUsageSensor(FrameworkEntity, SensorEntity):
    """System CPU usage sensor."""
    _attr_name = "CPU Usage"
    _attr_unique_id = "cpu_usage"
    _attr_icon = "mdi:cpu-64-bit"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        return round(gpu_data.get("cpu_usage_percent", 0), 1)


# Ollama Model Sensors

class OllamaModelsSensor(FrameworkEntity, SensorEntity):
    """Ollama loaded models sensor."""
    _attr_name = "Ollama Models"
    _attr_unique_id = "ollama_models"
    _attr_icon = "mdi:brain"

    @property
    def native_value(self):
        models = self.coordinator.data.get("ollama", {}).get("models", [])
        return ", ".join(models) if models else "None"

    @property
    def extra_state_attributes(self):
        models = self.coordinator.data.get("ollama", {}).get("models", [])
        return {"models": models}


class OllamaVRAMSensor(FrameworkEntity, SensorEntity):
    """Ollama VRAM usage sensor."""
    _attr_name = "Ollama VRAM"
    _attr_unique_id = "ollama_vram"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        ollama = self.coordinator.data.get("ollama", {})
        vram_bytes = ollama.get("loaded_vram_bytes", 0)
        return round(vram_bytes / (1024**3), 2)

