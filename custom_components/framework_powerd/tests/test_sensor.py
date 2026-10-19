"""Tests for sensor entities."""
import pytest
from unittest.mock import MagicMock


class TestSmartdAlertsSensor:
    """Test SmartdAlertsSensor."""

    def test_native_value_returns_count(self, mock_coordinator):
        """Test that native_value returns count of alerts."""
        from custom_components.framework_powerd.sensor import SmartdAlertsSensor
        
        sensor = SmartdAlertsSensor(mock_coordinator)
        assert sensor.native_value == 1

    def test_native_value_no_alerts(self, mock_coordinator):
        """Test native_value when no alerts."""
        from custom_components.framework_powerd.sensor import SmartdAlertsSensor
        
        mock_coordinator.data["smartd"]["alerts"] = []
        sensor = SmartdAlertsSensor(mock_coordinator)
        assert sensor.native_value == 0

    def test_extra_state_attributes(self, mock_coordinator):
        """Test extra_state_attributes contains alerts."""
        from custom_components.framework_powerd.sensor import SmartdAlertsSensor
        
        sensor = SmartdAlertsSensor(mock_coordinator)
        attrs = sensor.extra_state_attributes
        
        assert "alerts" in attrs
        assert len(attrs["alerts"]) == 1
        assert attrs["alerts"][0]["device"] == "/dev/sda"


class TestSmartdDeviceSensor:
    """Test SmartdDeviceSensor."""

    def test_native_value_on_when_alert(self, mock_coordinator):
        """Test native_value is 1 when alert exists for device."""
        from custom_components.framework_powerd.sensor import SmartdDeviceSensor
        
        sensor = SmartdDeviceSensor(mock_coordinator, "/dev/sda")
        assert sensor.native_value == 1

    def test_native_value_off_when_no_alert(self, mock_coordinator):
        """Test native_value is 0 when no alert for device."""
        from custom_components.framework_powerd.sensor import SmartdDeviceSensor
        
        sensor = SmartdDeviceSensor(mock_coordinator, "/dev/sdb")
        assert sensor.native_value == 0

    def test_dynamic_name(self, mock_coordinator):
        """Test name includes device name."""
        from custom_components.framework_powerd.sensor import SmartdDeviceSensor
        
        sensor = SmartdDeviceSensor(mock_coordinator, "/dev/sda")
        assert sensor.name == "SMART /dev/sda"

    def test_unique_id(self, mock_coordinator):
        """Test unique_id is generated correctly."""
        from custom_components.framework_powerd.sensor import SmartdDeviceSensor
        
        sensor = SmartdDeviceSensor(mock_coordinator, "/dev/sda")
        assert sensor._attr_unique_id == "smartd__dev_sda"

    def test_extra_state_attributes_device_alerts(self, mock_coordinator):
        """Test extra_state_attributes for specific device."""
        from custom_components.framework_powerd.sensor import SmartdDeviceSensor
        
        sensor = SmartdDeviceSensor(mock_coordinator, "/dev/sda")
        attrs = sensor.extra_state_attributes
        
        assert "alerts" in attrs
        assert len(attrs["alerts"]) == 1
        assert attrs["alerts"][0]["device"] == "/dev/sda"


class TestSystemInfoSensors:
    """Test system info sensors (kernel, OS, CPU, RAM, daemon version)."""

    def test_kernel_version(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import KernelVersionSensor

        sensor = KernelVersionSensor(mock_coordinator)
        assert sensor.native_value == "6.12.1-cachyos-x86_64"

    def test_os_sensor(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import OSSensor

        sensor = OSSensor(mock_coordinator)
        assert sensor.native_value == "CachyOS 1.0"

    def test_cpu_model(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import CPUModelSensor

        sensor = CPUModelSensor(mock_coordinator)
        assert sensor.native_value == "AMD Ryzen AI 7 350 w/ Radeon 860M"

    def test_cpu_model_attributes(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import CPUModelSensor

        sensor = CPUModelSensor(mock_coordinator)
        attrs = sensor.extra_state_attributes
        assert attrs["cpu_count"] == 16

    def test_total_ram(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import TotalRAMSensor

        sensor = TotalRAMSensor(mock_coordinator)
        # 48838284 kB = 48838284 * 1024 bytes = 50,010,558,464 bytes → ~46.6 GiB
        expected = round(48838284 * 1024 / (1024**3), 2)
        assert sensor.native_value == expected

    def test_daemon_version(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import DaemonVersionSensor

        sensor = DaemonVersionSensor(mock_coordinator)
        assert sensor.native_value == "1.3.0"

    def test_kernel_version_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import KernelVersionSensor

        sensor = KernelVersionSensor(mock_coordinator)
        assert sensor._attr_unique_id == "kernel_version"


class TestEstimatedTotalPowerSensor:
    """Test EstimatedTotalPowerSensor."""

    def test_native_value(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import EstimatedTotalPowerSensor

        sensor = EstimatedTotalPowerSensor(mock_coordinator)
        assert sensor.native_value == 18.2

    def test_native_value_missing(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import EstimatedTotalPowerSensor

        mock_coordinator.data["power"]["current"]["estimated_total_watts"] = 0
        sensor = EstimatedTotalPowerSensor(mock_coordinator)
        assert sensor.native_value == 0

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import EstimatedTotalPowerSensor

        sensor = EstimatedTotalPowerSensor(mock_coordinator)
        assert sensor._attr_unique_id == "power_estimated_total"


class TestPeripheralPowerSensor:
    """Test PeripheralPowerSensor."""

    def test_native_value(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import PeripheralPowerSensor

        sensor = PeripheralPowerSensor(mock_coordinator)
        assert sensor.native_value == 3.5

    def test_native_value_missing(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import PeripheralPowerSensor

        mock_coordinator.data["power"]["current"]["peripheral_watts"] = 0
        sensor = PeripheralPowerSensor(mock_coordinator)
        assert sensor.native_value == 0

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import PeripheralPowerSensor

        sensor = PeripheralPowerSensor(mock_coordinator)
        assert sensor._attr_unique_id == "power_peripheral"


class TestLifetimeEnergySensor:
    """Test LifetimeEnergySensor — persisted total energy counter."""

    def test_native_value(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LifetimeEnergySensor

        sensor = LifetimeEnergySensor(mock_coordinator)
        assert sensor.native_value == 42.125

    def test_native_value_missing(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LifetimeEnergySensor

        del mock_coordinator.data["power"]["lifetime_energy_kwh"]
        sensor = LifetimeEnergySensor(mock_coordinator)
        assert sensor.native_value == 0

    def test_unique_id_and_unit(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LifetimeEnergySensor
        from homeassistant.components.sensor import SensorDeviceClass, SensorStateClass
        from homeassistant.const import UnitOfEnergy

        sensor = LifetimeEnergySensor(mock_coordinator)
        assert sensor._attr_unique_id == "energy_total"
        assert sensor._attr_device_class == SensorDeviceClass.ENERGY
        assert sensor._attr_state_class == SensorStateClass.TOTAL_INCREASING
        assert sensor._attr_native_unit_of_measurement == UnitOfEnergy.KILO_WATT_HOUR


class TestPowerModeSensor:
    """Test PowerModeSensor — passthrough of the daemon's reported mode.

    With the balanced APU pinning the daemon always reports "balanced"
    regardless of the internal performance/powersave tunable gate, so the
    sensor must display that string verbatim without any value branching.
    """

    def test_native_value_balanced(self, mock_coordinator):
        """native_value should return the daemon-reported mode verbatim."""
        from custom_components.framework_powerd.sensor import PowerModeSensor

        sensor = PowerModeSensor(mock_coordinator)
        assert sensor.native_value == "balanced"

    def test_native_value_passthrough_legacy_values(self, mock_coordinator):
        """Older daemons may still report performance/powersave — display as-is."""
        from custom_components.framework_powerd.sensor import PowerModeSensor

        mock_coordinator.data["mode"] = "performance"
        sensor = PowerModeSensor(mock_coordinator)
        assert sensor.native_value == "performance"

        mock_coordinator.data["mode"] = "powersave"
        assert sensor.native_value == "powersave"

    def test_native_value_missing_mode(self, mock_coordinator):
        """Missing mode key should return None, not raise."""
        from custom_components.framework_powerd.sensor import PowerModeSensor

        del mock_coordinator.data["mode"]
        sensor = PowerModeSensor(mock_coordinator)
        assert sensor.native_value is None

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import PowerModeSensor

        sensor = PowerModeSensor(mock_coordinator)
        assert sensor._attr_unique_id == "power_mode"

    def test_name(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import PowerModeSensor

        sensor = PowerModeSensor(mock_coordinator)
        assert sensor._attr_name == "Power Mode"


class TestGamePIDSensor:
    """Test GamePIDSensor."""

    def test_native_value_with_pid(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GamePIDSensor

        mock_coordinator.data["game_pid"] = 4242
        sensor = GamePIDSensor(mock_coordinator)
        assert sensor.native_value == 4242

    def test_native_value_no_game(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GamePIDSensor

        sensor = GamePIDSensor(mock_coordinator)
        assert sensor.native_value == 0

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GamePIDSensor

        sensor = GamePIDSensor(mock_coordinator)
        assert sensor._attr_unique_id == "game_pid"


class TestUptimeSensor:
    """Test UptimeSensor."""

    def test_native_value(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import UptimeSensor

        sensor = UptimeSensor(mock_coordinator)
        assert sensor.native_value == 1234.0

    def test_native_value_missing(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import UptimeSensor

        mock_coordinator.data.pop("uptime_seconds")
        sensor = UptimeSensor(mock_coordinator)
        assert sensor.native_value is None

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import UptimeSensor

        sensor = UptimeSensor(mock_coordinator)
        assert sensor._attr_unique_id == "uptime"

    def test_device_class_and_unit(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import UptimeSensor
        from homeassistant.components.sensor import SensorDeviceClass
        from homeassistant.const import UnitOfTime

        sensor = UptimeSensor(mock_coordinator)
        assert sensor._attr_device_class == SensorDeviceClass.DURATION
        assert sensor._attr_native_unit_of_measurement == UnitOfTime.SECONDS


class TestRAMMemorySensors:
    """Test RAM used/free/available/usage sensors."""

    def test_ram_used(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import RAMUsedSensor

        sensor = RAMUsedSensor(mock_coordinator)
        expected = round(mock_coordinator.data["memory"]["used_bytes"] / (1024**3), 2)
        assert sensor.native_value == expected

    def test_ram_free(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import RAMFreeSensor

        sensor = RAMFreeSensor(mock_coordinator)
        assert sensor.native_value == 8.0

    def test_ram_available(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import RAMAvailableSensor

        sensor = RAMAvailableSensor(mock_coordinator)
        assert sensor.native_value == 20.0

    def test_ram_usage(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import RAMUsageSensor

        sensor = RAMUsageSensor(mock_coordinator)
        expected = round(
            mock_coordinator.data["memory"]["used_percent"], 1
        )
        assert sensor.native_value == expected

    def test_missing_memory_returns_zero(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import RAMUsedSensor, RAMUsageSensor

        del mock_coordinator.data["memory"]
        assert RAMUsedSensor(mock_coordinator).native_value == 0
        assert RAMUsageSensor(mock_coordinator).native_value == 0

    def test_unique_ids_and_units(self, mock_coordinator):
        from homeassistant.const import UnitOfInformation

        from custom_components.framework_powerd.sensor import (
            RAMAvailableSensor,
            RAMFreeSensor,
            RAMUsageSensor,
            RAMUsedSensor,
        )

        assert RAMUsedSensor(mock_coordinator)._attr_unique_id == "ram_used"
        assert RAMFreeSensor(mock_coordinator)._attr_unique_id == "ram_free"
        assert RAMAvailableSensor(mock_coordinator)._attr_unique_id == "ram_available"
        assert RAMUsageSensor(mock_coordinator)._attr_unique_id == "ram_usage"
        assert RAMUsedSensor(mock_coordinator)._attr_native_unit_of_measurement == UnitOfInformation.GIBIBYTES
        assert RAMUsageSensor(mock_coordinator)._attr_native_unit_of_measurement == "%"


class TestGPUVRAMDerivedSensors:
    """Test VRAM free and usage sensors."""

    def test_vram_free(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUVRAMFreeSensor

        sensor = GPUVRAMFreeSensor(mock_coordinator)
        expected = round(mock_coordinator.data["gpu"]["vram_free_bytes"] / (1024**3), 2)
        assert sensor.native_value == expected

    def test_vram_free_fallback(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUVRAMFreeSensor

        del mock_coordinator.data["gpu"]["vram_free_bytes"]
        sensor = GPUVRAMFreeSensor(mock_coordinator)
        expected = round(
            (
                mock_coordinator.data["gpu"]["vram_total_bytes"]
                - mock_coordinator.data["gpu"]["vram_used_bytes"]
            )
            / (1024**3),
            2,
        )
        assert sensor.native_value == expected

    def test_vram_usage(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUVRAMUsageSensor

        sensor = GPUVRAMUsageSensor(mock_coordinator)
        expected = round(mock_coordinator.data["gpu"]["vram_used_percent"], 1)
        assert sensor.native_value == expected

    def test_vram_usage_fallback(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUVRAMUsageSensor

        del mock_coordinator.data["gpu"]["vram_used_percent"]
        sensor = GPUVRAMUsageSensor(mock_coordinator)
        gpu = mock_coordinator.data["gpu"]
        expected = round(gpu["vram_used_bytes"] / gpu["vram_total_bytes"] * 100, 1)
        assert sensor.native_value == expected


class TestGPUTTGSensors:
    """Test GTT total/free/usage sensors."""

    def test_gtt_total(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUTTGTotalSensor

        sensor = GPUTTGTotalSensor(mock_coordinator)
        expected = round(mock_coordinator.data["gpu"]["gtt_total_bytes"] / (1024**2), 2)
        assert sensor.native_value == expected

    def test_gtt_free(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUTTGFreeSensor

        sensor = GPUTTGFreeSensor(mock_coordinator)
        expected = round(mock_coordinator.data["gpu"]["gtt_free_bytes"] / (1024**2), 2)
        assert sensor.native_value == expected

    def test_gtt_free_fallback(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUTTGFreeSensor

        del mock_coordinator.data["gpu"]["gtt_free_bytes"]
        sensor = GPUTTGFreeSensor(mock_coordinator)
        gpu = mock_coordinator.data["gpu"]
        expected = round((gpu["gtt_total_bytes"] - gpu["gtt_used_bytes"]) / (1024**2), 2)
        assert sensor.native_value == expected

    def test_gtt_usage(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUTTGUsageSensor

        sensor = GPUTTGUsageSensor(mock_coordinator)
        expected = round(mock_coordinator.data["gpu"]["gtt_used_percent"], 1)
        assert sensor.native_value == expected

    def test_gtt_usage_fallback(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUTTGUsageSensor

        del mock_coordinator.data["gpu"]["gtt_used_percent"]
        sensor = GPUTTGUsageSensor(mock_coordinator)
        gpu = mock_coordinator.data["gpu"]
        expected = round(gpu["gtt_used_bytes"] / gpu["gtt_total_bytes"] * 100, 1)
        assert sensor.native_value == expected
