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
        TunableModeSensor(coordinator),
        DaemonHealthSensor(coordinator),
        NVMeStatusSensor(coordinator),
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

    def _nvme_entities(coord):
        entities = []
        nvme = (coord.data or {}).get("nvme", {})
        for device in nvme.get("devices", []):
            name = device.get("device")
            if not name:
                continue
            entities.extend(cls(coord, name) for cls in NVME_SENSOR_CLASSES)
        return entities

    # LLM request accounting sensors. Group and client-IP entities appear as
    # sources show up, so the factory keeps running and emits only new ones.
    _llm_registered: set[tuple[str, str]] = set()

    def _llm_entities(coord):
        return build_llm_entities(coord, _llm_registered)

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

    # Add NVMe health sensors (one set per device)
    nvme_data = coordinator.data.get("nvme", {})
    if nvme_data.get("devices"):
        entities.extend(_nvme_entities(coordinator))

    # Add system info sensors (always present — static data from /status)
    sysinfo_data = coordinator.data.get("system_info", {})
    if sysinfo_data:
        entities.extend(
            [
                KernelVersionSensor(coordinator),
                OSSensor(coordinator),
                CPUModelSensor(coordinator),
                TotalRAMSensor(coordinator),
                DaemonVersionSensor(coordinator),
            ]
        )

    # Add LLM request accounting sensors
    llm_data = coordinator.data.get("llm", {})
    if llm_data:
        entities.extend(_llm_entities(coordinator))

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

    def _nvme_sensors_factory(coord):
        if (coord.data or {}).get("nvme", {}).get("devices"):
            return _nvme_entities(coord)
        return []

    def _llm_sensors_factory(coord):
        return _llm_entities(coord)

    if not memory_data:
        coordinator.register_entity_factory(_memory_sensors_factory)
    if not gpu_data:
        coordinator.register_entity_factory(_gpu_sensors_factory)
    if not nvme_data.get("devices"):
        coordinator.register_entity_factory(_nvme_sensors_factory)
    coordinator.register_entity_factory(_llm_sensors_factory, persistent=True)

    # A11: register a dynamic factory so per-device SMART alert entities appear
    # when they first show up in coordinator data, even if they were absent at
    # setup time.
    _registered_smartd_devices: set[str] = set(
        alert.get("device")
        for alert in smartd_data.get("alerts", [])
        if alert.get("device")
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
        mode = self.coordinator.data.get("mode")
        # Older daemons reported the internal tunable gate here. The pinned
        # APU profile is always "balanced".
        if mode in ("performance", "powersave"):
            return "balanced"
        return mode


class TunableModeSensor(FrameworkEntity, SensorEntity):
    """Internal performance/powersave tunable gate (diagnostic)."""

    _attr_name = "Tunable Mode"
    _attr_unique_id = "tunable_mode"
    _attr_icon = "mdi:tune"
    _attr_entity_category = EntityCategory.DIAGNOSTIC

    @property
    def native_value(self):
        data = self.coordinator.data
        mode = data.get("tunable_mode")
        if not mode:
            legacy = data.get("mode")
            mode = legacy if legacy in ("performance", "powersave") else None
        return mode


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
                gpu_data.get("vram_total_bytes", 0)
                - gpu_data.get("vram_used_bytes", 0),
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
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        gtt_bytes = gpu_data.get("gtt_used_bytes", 0)
        return round(gtt_bytes / (1024**3), 2)


class GPUTTGTotalSensor(FrameworkEntity, SensorEntity):
    """GPU GTT total sensor."""

    _attr_name = "GPU GTT Total"
    _attr_unique_id = "gpu_gtt_total"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
    _attr_suggested_display_precision = 2
    _attr_icon = "mdi:memory"

    @property
    def native_value(self):
        gpu_data = self.coordinator.data.get("gpu", {})
        gtt_bytes = gpu_data.get("gtt_total_bytes", 0)
        return round(gtt_bytes / (1024**3), 2)


class GPUTTGFreeSensor(FrameworkEntity, SensorEntity):
    """GPU GTT free sensor."""

    _attr_name = "GPU GTT Free"
    _attr_unique_id = "gpu_gtt_free"
    _attr_device_class = SensorDeviceClass.DATA_SIZE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfInformation.GIBIBYTES
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
        return round(free / (1024**3), 2)


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


# NVMe Sensors


class NVMeDeviceSensor(FrameworkEntity, SensorEntity):
    """Base class for per-NVMe-device sensors."""

    sensor_key = ""
    sensor_name = ""
    value_key = ""
    native_unit = None
    device_class = None
    state_class = SensorStateClass.MEASUREMENT
    scale = 1
    precision = None
    icon_name = "mdi:harddisk"

    def __init__(self, coordinator, device):
        super().__init__(coordinator)
        self._device = device
        self._attr_name = f"NVMe {device} {self.sensor_name}"
        self._attr_unique_id = f"nvme_{device.replace('/', '_')}_{self.sensor_key}"
        self._attr_native_unit_of_measurement = self.native_unit
        self._attr_device_class = self.device_class
        self._attr_state_class = self.state_class
        self._attr_icon = self.icon_name
        if self.precision is not None:
            self._attr_suggested_display_precision = self.precision

    @property
    def _device_data(self):
        nvme = self.coordinator.data.get("nvme", {})
        for device in nvme.get("devices", []):
            if device.get("device") == self._device:
                return device
        return {}

    @property
    def native_value(self):
        value = self._device_data.get(self.value_key)
        if value is None:
            return None
        if isinstance(value, (int, float)) and self.scale != 1:
            value = value / self.scale
            return round(value, self.precision or 2)
        return value


class NVMeHealthSensor(NVMeDeviceSensor):
    sensor_key = "health"
    sensor_name = "Health"
    value_key = "health"
    state_class = None

    @property
    def icon(self):
        return {
            "ok": "mdi:check-circle",
            "warning": "mdi:alert",
            "critical": "mdi:alert-octagon",
        }.get(self.native_value, "mdi:harddisk")


class NVMePercentageUsedSensor(NVMeDeviceSensor):
    sensor_key = "percentage_used"
    sensor_name = "Percentage Used"
    value_key = "percentage_used"
    native_unit = "%"
    precision = 1
    icon_name = "mdi:chart-donut"


class NVMeAvailableSpareSensor(NVMeDeviceSensor):
    sensor_key = "available_spare"
    sensor_name = "Available Spare"
    value_key = "available_spare"
    native_unit = "%"
    precision = 1
    icon_name = "mdi:shield-check"


class NVMeMediaErrorsSensor(NVMeDeviceSensor):
    sensor_key = "media_errors"
    sensor_name = "Media Errors"
    value_key = "media_errors"
    icon_name = "mdi:alert-circle"


class NVMeErrorLogSensor(NVMeDeviceSensor):
    sensor_key = "error_log_entries"
    sensor_name = "Error Log Entries"
    value_key = "error_log_entries"
    icon_name = "mdi:script-text-alert"


class NVMeCriticalWarningSensor(NVMeDeviceSensor):
    sensor_key = "critical_warning"
    sensor_name = "Critical Warning"
    value_key = "critical_warning"
    icon_name = "mdi:alert-octagon"


class NVMeTemperatureSensor(NVMeDeviceSensor):
    sensor_key = "temperature"
    sensor_name = "Temperature"
    value_key = "temperature_celsius"
    native_unit = "°C"
    device_class = SensorDeviceClass.TEMPERATURE
    precision = 0
    icon_name = "mdi:thermometer"


class NVMeDataWrittenSensor(NVMeDeviceSensor):
    sensor_key = "data_written"
    sensor_name = "Data Written"
    value_key = "data_written_bytes"
    native_unit = UnitOfInformation.TERABYTES
    device_class = SensorDeviceClass.DATA_SIZE
    scale = 1000**4
    precision = 2
    icon_name = "mdi:database"


class NVMePowerOnHoursSensor(NVMeDeviceSensor):
    sensor_key = "power_on_hours"
    sensor_name = "Power-On Hours"
    value_key = "power_on_hours"
    native_unit = UnitOfTime.HOURS
    device_class = SensorDeviceClass.DURATION
    state_class = SensorStateClass.TOTAL_INCREASING
    icon_name = "mdi:clock-outline"


class NVMeUnsafeShutdownsSensor(NVMeDeviceSensor):
    sensor_key = "unsafe_shutdowns"
    sensor_name = "Unsafe Shutdowns"
    value_key = "unsafe_shutdowns"
    state_class = SensorStateClass.TOTAL_INCREASING
    icon_name = "mdi:power-plug-off"


NVME_SENSOR_CLASSES = [
    NVMeHealthSensor,
    NVMePercentageUsedSensor,
    NVMeAvailableSpareSensor,
    NVMeMediaErrorsSensor,
    NVMeErrorLogSensor,
    NVMeCriticalWarningSensor,
    NVMeTemperatureSensor,
    NVMeDataWrittenSensor,
    NVMePowerOnHoursSensor,
    NVMeUnsafeShutdownsSensor,
]


class NVMeStatusSensor(FrameworkEntity, SensorEntity):
    """Aggregated NVMe polling status and diagnostics."""

    _attr_name = "NVMe Status"
    _attr_unique_id = "nvme_status"
    _attr_icon = "mdi:harddisk"
    _attr_entity_category = EntityCategory.DIAGNOSTIC

    @property
    def _nvme(self):
        return self.coordinator.data.get("nvme") or {}

    @property
    def native_value(self):
        nvme = self._nvme
        if not nvme or not nvme.get("enabled", True):
            return "disabled"
        if nvme.get("last_error"):
            return "error"
        devices = nvme.get("devices") or []
        if not devices:
            return "no_devices"
        health = [device.get("health", "ok") for device in devices]
        if "critical" in health:
            return "critical"
        if "warning" in health:
            return "warning"
        return "ok"

    @property
    def extra_state_attributes(self):
        nvme = self._nvme
        devices = nvme.get("devices") or []
        return {
            "enabled": nvme.get("enabled"),
            "last_error": nvme.get("last_error"),
            "last_update": nvme.get("last_update"),
            "device_count": len(devices),
            "devices": [
                {
                    "device": device.get("device"),
                    "health": device.get("health"),
                    "issues": device.get("issues") or [],
                }
                for device in devices
            ],
        }


class DaemonHealthSensor(FrameworkEntity, SensorEntity):
    """Overall daemon health from /status.health."""

    _attr_name = "Daemon Health"
    _attr_unique_id = "daemon_health"
    _attr_icon = "mdi:heart-pulse"
    _attr_entity_category = EntityCategory.DIAGNOSTIC

    @property
    def _health(self):
        return self.coordinator.data.get("health") or {}

    @property
    def native_value(self):
        return self._health.get("status", "unknown")

    @property
    def extra_state_attributes(self):
        health = self._health
        monitors = health.get("monitors") or {}
        return {
            "version": health.get("version"),
            "turbostat": monitors.get("turbostat"),
            "smartd": monitors.get("smartd"),
            "nvme": monitors.get("nvme"),
            "llm": monitors.get("llm"),
            "gpu": monitors.get("gpu"),
            "llm_last_error": (self.coordinator.data.get("llm") or {}).get(
                "last_error"
            ),
            "power_sample_age_seconds": health.get("power_sample_age_seconds"),
            "memory_used_percent": health.get("memory_used_percent"),
            "metrics_state": health.get("metrics_state"),
        }


# LLM Request Accounting Sensors


class LLMSensorBase(FrameworkEntity, SensorEntity):
    """Base class for LLM request accounting sensors."""

    @property
    def _llm(self):
        return self.coordinator.data.get("llm") or {}

    @property
    def _today(self):
        return self._llm.get("today") or {}

    @property
    def _history(self):
        return self._llm.get("days") or []


class LLMRequestsTotalSensor(LLMSensorBase):
    """Total number of counted LLM requests since accounting state was created."""

    _attr_name = "LLM Requests (Total)"
    _attr_unique_id = "llm_requests_total"
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_icon = "mdi:message-processing"

    @property
    def native_value(self):
        return self._llm.get("total", {}).get("count", 0)

    @property
    def extra_state_attributes(self):
        total = self._llm.get("total", {})
        return {
            "total_duration_secs": total.get("total_duration_secs", 0),
            "total_energy_kwh": total.get("total_energy_kwh", 0),
            "total_cost": total.get("total_cost", 0),
            "currency": self._llm.get("currency", ""),
            "last_request": self._llm.get("last_request"),
            "history": self._history,
        }


class LLMRequestsTodaySensor(LLMSensorBase):
    """Number of counted LLM requests today (UTC)."""

    _attr_name = "LLM Requests Today"
    _attr_unique_id = "llm_requests_today"
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_icon = "mdi:calendar-today"

    @property
    def native_value(self):
        return self._today.get("total", 0)

    @property
    def extra_state_attributes(self):
        return {
            "date": self._today.get("date"),
            "by_ip": self._today.get("by_ip", {}),
            "by_group": self._today.get("by_group", {}),
            "history": self._history,
        }


class LLMEnergyTodaySensor(LLMSensorBase):
    """Energy attributed to today's LLM requests."""

    _attr_name = "LLM Energy Today"
    _attr_unique_id = "llm_energy_today"
    _attr_device_class = SensorDeviceClass.ENERGY
    _attr_native_unit_of_measurement = UnitOfEnergy.KILO_WATT_HOUR
    _attr_state_class = SensorStateClass.TOTAL
    _attr_suggested_display_precision = 4
    _attr_icon = "mdi:lightning-bolt"

    @property
    def native_value(self):
        return round(self._today.get("total_energy_kwh", 0), 4)


class LLMCostTodaySensor(LLMSensorBase):
    """Cost attributed to today's LLM requests."""

    _attr_name = "LLM Cost Today"
    _attr_unique_id = "llm_cost_today"
    _attr_device_class = SensorDeviceClass.MONETARY
    _attr_state_class = SensorStateClass.TOTAL
    _attr_suggested_display_precision = 4
    _attr_icon = "mdi:cash"

    @property
    def native_value(self):
        return round(self._today.get("total_cost", 0), 4)

    @property
    def native_unit_of_measurement(self):
        return self._llm.get("currency") or None

    @property
    def extra_state_attributes(self):
        return {
            "currency": self._llm.get("currency", ""),
            "price_per_kwh": self._llm.get("price_per_kwh", 0),
            "total_cost": self._llm.get("total", {}).get("total_cost", 0),
        }


class LLMGroupRequestsSensor(LLMSensorBase):
    """Requests today for one IP group."""

    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_icon = "mdi:account-group"

    def __init__(self, coordinator, group):
        super().__init__(coordinator)
        self._group = group
        self._attr_name = f"LLM {group.title()} Requests Today"
        self._attr_unique_id = f"llm_group_{group}_requests"

    @property
    def native_value(self):
        return self._today.get("by_group", {}).get(self._group, 0)

    @property
    def extra_state_attributes(self):
        return {"group": self._group, "date": self._today.get("date")}


class LLMGroupPercentSensor(LLMSensorBase):
    """Share of today's requests attributed to one IP group."""

    _attr_native_unit_of_measurement = "%"
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_suggested_display_precision = 1
    _attr_icon = "mdi:chart-donut"

    def __init__(self, coordinator, group):
        super().__init__(coordinator)
        self._group = group
        self._attr_name = f"LLM {group.title()} Requests Today Share"
        self._attr_unique_id = f"llm_group_{group}_percent"

    @property
    def native_value(self):
        total = self._today.get("total", 0)
        if not total:
            return 0
        count = self._today.get("by_group", {}).get(self._group, 0)
        return round(count / total * 100, 1)

    @property
    def extra_state_attributes(self):
        return {
            "group": self._group,
            "date": self._today.get("date"),
            "requests": self._today.get("by_group", {}).get(self._group, 0),
            "total_requests": self._today.get("total", 0),
        }


class LLMIPRequestsSensor(LLMSensorBase):
    """Requests today from one client IP."""

    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_icon = "mdi:ip-network"

    def __init__(self, coordinator, ip):
        super().__init__(coordinator)
        self._ip = ip
        self._attr_name = f"LLM {ip} Requests Today"
        self._attr_unique_id = f"llm_ip_{ip.replace('.', '_').replace(':', '_')}"

    @property
    def native_value(self):
        return self._today.get("by_ip", {}).get(self._ip, 0)

    @property
    def extra_state_attributes(self):
        total = self._today.get("total", 0)
        count = self._today.get("by_ip", {}).get(self._ip, 0)
        percent = round(count / total * 100, 1) if total else 0
        return {
            "ip": self._ip,
            "date": self._today.get("date"),
            "requests": count,
            "percent": percent,
        }


# Upper bound on per-IP sensors. Client addresses are unbounded in principle
# (DHCP churn, IPv6 privacy addresses), so the integration caps how many
# entity rows it creates. The full per-IP data stays in the attributes.
MAX_LLM_IP_SENSORS = 25


def build_llm_entities(coord, registered):
    """Return the LLM sensors that have not been created yet.

    registered holds the (kind, name) pairs already added and is updated in
    place, so this can be used as a persistent entity factory: a group or a
    client that shows up later gets its entities without a reload.
    """
    llm = (coord.data or {}).get("llm")
    if not llm:
        return []

    new = []
    if ("base", "") not in registered:
        registered.add(("base", ""))
        new.extend(
            [
                LLMRequestsTotalSensor(coord),
                LLMRequestsTodaySensor(coord),
                LLMEnergyTodaySensor(coord),
                LLMCostTodaySensor(coord),
            ]
        )

    for group in llm.get("by_group") or {}:
        key = ("group", group)
        if key in registered:
            continue
        registered.add(key)
        new.append(LLMGroupRequestsSensor(coord, group))
        new.append(LLMGroupPercentSensor(coord, group))

    ip_count = sum(1 for kind, _ in registered if kind == "ip")
    for ip in llm.get("by_ip") or {}:
        if ip_count >= MAX_LLM_IP_SENSORS:
            break
        key = ("ip", ip)
        if key in registered:
            continue
        registered.add(key)
        ip_count += 1
        new.append(LLMIPRequestsSensor(coord, ip))

    return new


# System Info Sensors


class KernelVersionSensor(FrameworkEntity, SensorEntity):
    """Kernel version sensor."""

    _attr_name = "Kernel Version"
    _attr_unique_id = "kernel_version"
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_icon = "mdi:linux"

    @property
    def native_value(self):
        return self.coordinator.data.get("system_info", {}).get(
            "kernel_version", "Unknown"
        )


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
        ram_bytes = self.coordinator.data.get("system_info", {}).get(
            "total_ram_bytes", 0
        )
        return round(ram_bytes / (1024**3), 2) if ram_bytes else 0


class DaemonVersionSensor(FrameworkEntity, SensorEntity):
    """Daemon version sensor."""

    _attr_name = "Daemon Version"
    _attr_unique_id = "daemon_version"
    _attr_entity_category = EntityCategory.DIAGNOSTIC
    _attr_icon = "mdi:power-plug"

    @property
    def native_value(self):
        return self.coordinator.data.get("system_info", {}).get(
            "daemon_version", "Unknown"
        )
