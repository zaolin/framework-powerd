"""Sensor platform for Framework Power Daemon."""
from homeassistant.components.sensor import (
    SensorDeviceClass,
    SensorEntity,
    SensorStateClass,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import (
    UnitOfInformation,
    UnitOfPower,
    UnitOfEnergy,
    UnitOfTime,
)
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity import EntityCategory
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .entity import FrameworkEntity


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
        # Power
        EstimatedTotalPowerSensor(coordinator),
        # Energy Sensors (kWh)
        EnergySensor(coordinator, "energy_24h_kwh", "Energy (24h)", "24h"),
        EnergySensor(coordinator, "energy_7d_kwh", "Energy (7 Days)", "7d"),
        LifetimeEnergySensor(coordinator),
    ]

    def _memory_entities(coord):
        return [
            RAMUsedSensor(coord),
            RAMFreeSensor(coord),
            RAMAvailableSensor(coord),
            RAMUsageSensor(coord),
        ]

    def _gpu_entities(coord):
        return [
            GPUTemperatureSensor(coord),
            GPUPowerSensor(coord),
            GPUVRAMUsedSensor(coord),
            GPUVRAMTotalSensor(coord),
            GPUVRAMFreeSensor(coord),
            GPUVRAMUsageSensor(coord),
            GPUTTGUsedSensor(coord),
            GPUTTGTotalSensor(coord),
            GPUTTGFreeSensor(coord),
            GPUTTGUsageSensor(coord),
            GPUCPUUsageSensor(coord),
        ]

    # Add SMART sensors
    entities.append(SmartdAlertsSensor(coordinator))

    # Add per-device SMART sensors dynamically based on active alerts
    smartd_data = coordinator.data.get("smartd", {})
    for alert in smartd_data.get("alerts", []):
        device = alert.get("device")
        if device:
            entities.append(SmartdDeviceSensor(coordinator, device))

    # Add memory sensors (dynamic /status.memory)
    memory_data = coordinator.data.get("memory", {})
    if memory_data:
        entities.extend(_memory_entities(coordinator))

    # Add GPU sensors
    gpu_data = coordinator.data.get("gpu", {})
    if gpu_data:
        entities.extend(_gpu_entities(coordinator))

    # Add system info sensors (always present — static data from /status)
    sysinfo_data = coordinator.data.get("system_info", {})
    if sysinfo_data:
        entities.extend([
            KernelVersionSensor(coordinator),
            OSSensor(coordinator),
            CPUModelSensor(coordinator),
            TotalRAMSensor(coordinator),
            DaemonVersionSensor(coordinator),
        ])

    async_add_entities(entities)

    # A11: dynamic factories for blocks that may not exist at setup time
    # (for example after upgrading the daemon without reloading the integration).
    def _memory_sensors_factory(coord):
        if (coord.data or {}).get("memory"):
            return _memory_entities(coord)
        return []

    def _gpu_sensors_factory(coord):
        if (coord.data or {}).get("gpu"):
            return _gpu_entities(coord)
        return []

    if not memory_data:
        coordinator.register_entity_factory(_memory_sensors_factory)
    if not gpu_data:
        coordinator.register_entity_factory(_gpu_sensors_factory)

    # A11: register a dynamic factory so per-device SMART alert entities appear
    # when they first show up in coordinator data, even if they were absent at
    # setup time.
    _registered_smartd_devices: set[str] = set(
        alert.get("device") for alert in smartd_data.get("alerts", []) if alert.get("device")
    )

    def _smartd_devices_factory(coord):
        data = coord.data or {}
        alerts = data.get("smartd", {}).get("alerts", [])
        new = []
        for alert in alerts:
            dev = alert.get("device")
            if dev and dev not in _registered_smartd_devices:
                _registered_smartd_devices.add(dev)
                new.append(SmartdDeviceSensor(coord, dev))
        return new

    coordinator.register_entity_factory(_smartd_devices_factory)
    coordinator.attach_dynamic_registrar(async_add_entities)


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


class EnergySensor(FrameworkEntity, SensorEntity):
    _attr_device_class = SensorDeviceClass.ENERGY
    _attr_native_unit_of_measurement = UnitOfEnergy.KILO_WATT_HOUR
    _attr_state_class = SensorStateClass.TOTAL
    _attr_suggested_display_precision = 3

    def __init__(self, coordinator, key, name, suffix):
        super().__init__(coordinator)
        self._key = key
        self._attr_name = name
        self._attr_unique_id = f"energy_{suffix}"

    @property
    def native_value(self):
        val = self.coordinator.data.get("power", {}).get(self._key, 0)
        return round(val, 3)


class LifetimeEnergySensor(FrameworkEntity, SensorEntity):
    """Total energy consumed since the metrics state was first persisted."""
    _attr_name = "Energy (Total)"
    _attr_unique_id = "energy_total"
    _attr_device_class = SensorDeviceClass.ENERGY
    _attr_native_unit_of_measurement = UnitOfEnergy.KILO_WATT_HOUR
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_suggested_display_precision = 3
    _attr_icon = "mdi:sigma"

    @property
    def native_value(self):
        val = self.coordinator.data.get("power", {}).get("lifetime_energy_kwh", 0)
        return round(val, 3)


class EstimatedTotalPowerSensor(FrameworkEntity, SensorEntity):
    """Estimated total system power: PkgWatt + peripherals + VRM correction."""
    _attr_name = "Estimated Total Power"
    _attr_unique_id = "power_estimated_total"
    _attr_device_class = SensorDeviceClass.POWER
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:flash"

    @property
    def native_value(self):
        power_data = self.coordinator.data.get("power", {}).get("current", {})
        val = power_data.get("estimated_total_watts", 0)
        return round(val, 2) if val else 0


class PeripheralPowerSensor(FrameworkEntity, SensorEntity):
    """Estimated peripheral power: USB + NVMe + fan + WiFi + Ethernet."""
    _attr_name = "Peripheral Power"
    _attr_unique_id = "power_peripheral"
    _attr_device_class = SensorDeviceClass.POWER
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:power-plug"

    @property
    def native_value(self):
        power_data = self.coordinator.data.get("power", {}).get("current", {})
        val = power_data.get("peripheral_watts", 0)
        return round(val, 2) if val else 0


class RAMUsedSensor(FrameworkEntity, SensorEntity):
    """RAM in use (MemTotal - MemAvailable)."""
    _attr_name = "RAM Used"
    _attr_unique_id = "ram_used"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        val = self.coordinator.data.get("memory", {}).get("used_bytes", 0)
        return round(val / (1024**3), 2)


class RAMFreeSensor(FrameworkEntity, SensorEntity):
    """Literal MemFree from /proc/meminfo."""
    _attr_name = "RAM Free"
    _attr_unique_id = "ram_free"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        val = self.coordinator.data.get("memory", {}).get("free_bytes", 0)
        return round(val / (1024**3), 2)


class RAMAvailableSensor(FrameworkEntity, SensorEntity):
    """MemAvailable: the kernel's estimate of memory available for new work."""
    _attr_name = "RAM Available"
    _attr_unique_id = "ram_available"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        val = self.coordinator.data.get("memory", {}).get("available_bytes", 0)
        return round(val / (1024**3), 2)


class RAMUsageSensor(FrameworkEntity, SensorEntity):
    """RAM usage percentage."""
    _attr_name = "RAM Usage"
    _attr_unique_id = "ram_usage"
    _attr_native_unit_of_measurement = "%"
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 1
    _attr_icon = "mdi:chart-donut"

    @property
    def native_value(self):
        return round(self.coordinator.data.get("memory", {}).get("used_percent", 0), 1)


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
    _attr_suggested_display_precision = 1
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
    _attr_suggested_display_precision = 2
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
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_suggested_display_precision = 2
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
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        vram_bytes = gpu_data.get("vram_total_bytes", 0)
        return round(vram_bytes / (1024**3), 2)


class GPUVRAMFreeSensor(FrameworkEntity, SensorEntity):
    """GPU VRAM free sensor."""
    _attr_name = "GPU VRAM Free"
    _attr_unique_id = "gpu_vram_free"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        free = gpu_data.get("vram_free_bytes")
        if free is None:
            free = max(
                gpu_data.get("vram_total_bytes", 0) - gpu_data.get("vram_used_bytes", 0),
                0,
            )
        return round(free / (1024**3), 2)


class GPUVRAMUsageSensor(FrameworkEntity, SensorEntity):
    """GPU VRAM usage percentage."""
    _attr_name = "GPU VRAM Usage"
    _attr_unique_id = "gpu_vram_usage"
    _attr_native_unit_of_measurement = "%"
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 1
    _attr_icon = "mdi:chart-donut"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        percent = gpu_data.get("vram_used_percent")
        if percent is None:
            total = gpu_data.get("vram_total_bytes", 0)
            used = gpu_data.get("vram_used_bytes", 0)
            percent = (used / total * 100) if total else 0
        return round(percent, 1)


class GPUTTGUsedSensor(FrameworkEntity, SensorEntity):
    """GPU GTT used sensor."""
    _attr_name = "GPU GTT Used"
    _attr_unique_id = "gpu_gtt_used"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.MEBIBYTES
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        gtt_bytes = gpu_data.get("gtt_used_bytes", 0)
        return round(gtt_bytes / (1024**2), 2)


class GPUTTGTotalSensor(FrameworkEntity, SensorEntity):
    """GPU GTT total sensor."""
    _attr_name = "GPU GTT Total"
    _attr_unique_id = "gpu_gtt_total"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.MEBIBYTES
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        gtt_bytes = gpu_data.get("gtt_total_bytes", 0)
        return round(gtt_bytes / (1024**2), 2)


class GPUTTGFreeSensor(FrameworkEntity, SensorEntity):
    """GPU GTT free sensor."""
    _attr_name = "GPU GTT Free"
    _attr_unique_id = "gpu_gtt_free"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.MEBIBYTES
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        free = gpu_data.get("gtt_free_bytes")
        if free is None:
            free = max(
                gpu_data.get("gtt_total_bytes", 0) - gpu_data.get("gtt_used_bytes", 0),
                0,
            )
        return round(free / (1024**2), 2)


class GPUTTGUsageSensor(FrameworkEntity, SensorEntity):
    """GPU GTT usage percentage."""
    _attr_name = "GPU GTT Usage"
    _attr_unique_id = "gpu_gtt_usage"
    _attr_native_unit_of_measurement = "%"
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 1
    _attr_icon = "mdi:chart-donut"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        percent = gpu_data.get("gtt_used_percent")
        if percent is None:
            total = gpu_data.get("gtt_total_bytes", 0)
            used = gpu_data.get("gtt_used_bytes", 0)
            percent = (used / total * 100) if total else 0
        return round(percent, 1)


class GPUCPUUsageSensor(FrameworkEntity, SensorEntity):
    """System CPU usage sensor."""
    _attr_name = "CPU Usage"
    _attr_unique_id = "cpu_usage"
    _attr_native_unit_of_measurement = "%"
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 1
    _attr_icon = "mdi:cpu-64-bit"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        return round(gpu_data.get("cpu_usage_percent", 0), 1)


# System Info Sensors

class KernelVersionSensor(FrameworkEntity, SensorEntity):
    """Kernel version sensor."""
    _attr_name = "Kernel Version"
    _attr_unique_id = "kernel_version"
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_icon = "mdi:linux"

    @property
    def native_value(self):
        return self.coordinator.data.get("system_info", {}).get("kernel_version", "Unknown")


class OSSensor(FrameworkEntity, SensorEntity):
    """Operating system sensor — shows 'Name Version' (e.g. 'Gentoo 2.18')."""
    _attr_name = "Operating System"
    _attr_unique_id = "operating_system"
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_icon = "mdi:desktop-classic"

    @property
    def native_value(self):
        sysinfo = self.coordinator.data.get("system_info", {})
        name = sysinfo.get("os_name", "")
        version = sysinfo.get("os_version", "")
        if name and version:
            return f"{name} {version}"
        return name or "Unknown"


class CPUModelSensor(FrameworkEntity, SensorEntity):
    """CPU model name sensor."""
    _attr_name = "CPU Model"
    _attr_unique_id = "cpu_model"
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_icon = "mdi:cpu-64-bit"

    @property
    def native_value(self):
        return self.coordinator.data.get("system_info", {}).get("cpu_model", "Unknown")

    @property
    def extra_state_attributes(self):
        sysinfo = self.coordinator.data.get("system_info", {})
        return {"cpu_count": sysinfo.get("cpu_count", 0)}


class TotalRAMSensor(FrameworkEntity, SensorEntity):
    """Total system RAM sensor."""
    _attr_name = "Total RAM"
    _attr_unique_id = "total_ram"
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        ram_bytes = self.coordinator.data.get("system_info", {}).get("total_ram_bytes", 0)
        return round(ram_bytes / (1024**3), 2) if ram_bytes else 0


class DaemonVersionSensor(FrameworkEntity, SensorEntity):
    """Daemon version sensor."""
    _attr_name = "Daemon Version"
    _attr_unique_id = "daemon_version"
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_icon = "mdi:power-plug"

    @property
    def native_value(self):
        return self.coordinator.data.get("system_info", {}).get("daemon_version", "Unknown")

