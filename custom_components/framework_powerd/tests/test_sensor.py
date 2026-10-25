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
        assert sensor.native_value == "1.6.0"

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

    def test_native_value_maps_legacy_values_to_balanced(self, mock_coordinator):
        """Older daemons reported the tunable gate; the APU profile is balanced."""
        from custom_components.framework_powerd.sensor import PowerModeSensor

        mock_coordinator.data["mode"] = "performance"
        sensor = PowerModeSensor(mock_coordinator)
        assert sensor.native_value == "balanced"

        mock_coordinator.data["mode"] = "powersave"
        assert sensor.native_value == "balanced"

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
        from custom_components.framework_powerd.sensor import (
            RAMUsedSensor,
            RAMUsageSensor,
        )

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
        assert (
            RAMUsedSensor(mock_coordinator)._attr_native_unit_of_measurement
            == UnitOfInformation.GIBIBYTES
        )
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
        expected = round(mock_coordinator.data["gpu"]["gtt_total_bytes"] / (1024**3), 2)
        assert sensor.native_value == expected

    def test_gtt_free(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUTTGFreeSensor

        sensor = GPUTTGFreeSensor(mock_coordinator)
        expected = round(mock_coordinator.data["gpu"]["gtt_free_bytes"] / (1024**3), 2)
        assert sensor.native_value == expected

    def test_gtt_free_fallback(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import GPUTTGFreeSensor

        del mock_coordinator.data["gpu"]["gtt_free_bytes"]
        sensor = GPUTTGFreeSensor(mock_coordinator)
        gpu = mock_coordinator.data["gpu"]
        expected = round((gpu["gtt_total_bytes"] - gpu["gtt_used_bytes"]) / (1024**3), 2)
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


class TestNVMeSensors:
    """Test direct NVMe health sensors."""

    def test_health_sensor(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import NVMeHealthSensor

        sensor = NVMeHealthSensor(mock_coordinator, "/dev/nvme0")
        assert sensor.native_value == "ok"
        assert sensor._attr_unique_id == "nvme__dev_nvme0_health"
        assert sensor._device_data["model"] == "WD_BLACK SN850X 2000GB"

    def test_wear_and_spare(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import (
            NVMeAvailableSpareSensor,
            NVMePercentageUsedSensor,
        )

        assert (
            NVMePercentageUsedSensor(mock_coordinator, "/dev/nvme0").native_value == 7
        )
        assert (
            NVMeAvailableSpareSensor(mock_coordinator, "/dev/nvme0").native_value == 100
        )

    def test_errors_and_temperature(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import (
            NVMeCriticalWarningSensor,
            NVMeErrorLogSensor,
            NVMeMediaErrorsSensor,
            NVMeTemperatureSensor,
        )

        assert NVMeMediaErrorsSensor(mock_coordinator, "/dev/nvme0").native_value == 0
        assert NVMeErrorLogSensor(mock_coordinator, "/dev/nvme0").native_value == 0
        assert (
            NVMeCriticalWarningSensor(mock_coordinator, "/dev/nvme0").native_value == 0
        )
        assert NVMeTemperatureSensor(mock_coordinator, "/dev/nvme0").native_value == 49

    def test_data_written_and_uptime(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import (
            NVMeDataWrittenSensor,
            NVMePowerOnHoursSensor,
            NVMeUnsafeShutdownsSensor,
        )

        assert (
            NVMeDataWrittenSensor(mock_coordinator, "/dev/nvme0").native_value == 120.0
        )
        assert (
            NVMePowerOnHoursSensor(mock_coordinator, "/dev/nvme0").native_value == 500
        )
        assert (
            NVMeUnsafeShutdownsSensor(mock_coordinator, "/dev/nvme0").native_value == 4
        )

    def test_missing_device_returns_none(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import NVMeHealthSensor

        sensor = NVMeHealthSensor(mock_coordinator, "/dev/nvme9")
        assert sensor.native_value is None
        assert sensor._device_data == {}

    def test_all_sensor_classes_have_unique_keys(self):
        from custom_components.framework_powerd.sensor import NVME_SENSOR_CLASSES

        keys = [cls.sensor_key for cls in NVME_SENSOR_CLASSES]
        assert len(keys) == len(set(keys))
        assert "health" in keys
        assert "percentage_used" in keys


class TestTunableModeSensor:
    """Test the diagnostic tunable-mode sensor."""

    def test_native_value(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import TunableModeSensor

        assert TunableModeSensor(mock_coordinator).native_value == "powersave"

    def test_legacy_mode_fallback(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import TunableModeSensor

        del mock_coordinator.data["tunable_mode"]
        mock_coordinator.data["mode"] = "performance"
        assert TunableModeSensor(mock_coordinator).native_value == "performance"

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import TunableModeSensor

        assert TunableModeSensor(mock_coordinator)._attr_unique_id == "tunable_mode"


class TestDaemonHealthSensor:
    """Test the daemon health sensor."""

    def test_native_value(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import DaemonHealthSensor

        sensor = DaemonHealthSensor(mock_coordinator)
        assert sensor.native_value == "ok"
        attrs = sensor.extra_state_attributes
        assert attrs["turbostat"] == "ok"
        assert attrs["nvme"] == "ok"
        assert attrs["llm"] == "ok"
        assert attrs["metrics_state"]["enabled"] is True

    def test_llm_error_is_visible(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import DaemonHealthSensor

        mock_coordinator.data["health"]["status"] = "degraded"
        mock_coordinator.data["health"]["monitors"]["llm"] = "error"
        mock_coordinator.data["llm"]["last_error"] = "permission denied"

        sensor = DaemonHealthSensor(mock_coordinator)
        assert sensor.native_value == "degraded"
        attrs = sensor.extra_state_attributes
        assert attrs["llm"] == "error"
        assert attrs["llm_last_error"] == "permission denied"

    def test_missing_health(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import DaemonHealthSensor

        del mock_coordinator.data["health"]
        assert DaemonHealthSensor(mock_coordinator).native_value == "unknown"


class TestNVMeStatusSensor:
    """Test the aggregated NVMe status sensor."""

    def test_ok(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import NVMeStatusSensor

        sensor = NVMeStatusSensor(mock_coordinator)
        assert sensor.native_value == "ok"
        attrs = sensor.extra_state_attributes
        assert attrs["device_count"] == 1
        assert attrs["last_error"] is None

    def test_error(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import NVMeStatusSensor

        mock_coordinator.data["nvme"]["last_error"] = "permission denied"
        assert NVMeStatusSensor(mock_coordinator).native_value == "error"

    def test_no_devices(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import NVMeStatusSensor

        mock_coordinator.data["nvme"]["devices"] = []
        assert NVMeStatusSensor(mock_coordinator).native_value == "no_devices"

    def test_missing_block(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import NVMeStatusSensor

        del mock_coordinator.data["nvme"]
        assert NVMeStatusSensor(mock_coordinator).native_value == "disabled"


class TestLLMSensors:
    """Test LLM request accounting sensors."""

    def test_total_requests(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LLMRequestsTotalSensor

        sensor = LLMRequestsTotalSensor(mock_coordinator)
        assert sensor.native_value == 120
        assert sensor._attr_unique_id == "llm_requests_total"
        attrs = sensor.extra_state_attributes
        assert attrs["currency"] == "EUR"
        assert len(attrs["history"]) == 2

    def test_requests_today(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LLMRequestsTodaySensor

        sensor = LLMRequestsTodaySensor(mock_coordinator)
        assert sensor.native_value == 70
        assert sensor.extra_state_attributes["date"] == "2026-01-02"

    def test_requests_today_percent_removed(self):
        from custom_components.framework_powerd import sensor

        assert not hasattr(sensor, "LLMRequestsTodayPercentSensor")

    def test_percent_is_zero_without_requests(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LLMGroupPercentSensor

        mock_coordinator.data["llm"]["today"]["total"] = 0
        assert LLMGroupPercentSensor(mock_coordinator, "home").native_value == 0

    def test_energy_and_cost_are_today_values(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import (
            LLMCostTodaySensor,
            LLMEnergyTodaySensor,
        )

        assert LLMEnergyTodaySensor(mock_coordinator).native_value == 0.07

        cost = LLMCostTodaySensor(mock_coordinator)
        assert cost.native_value == 0.021
        assert cost.native_unit_of_measurement == "EUR"
        assert cost.extra_state_attributes["currency"] == "EUR"

    def test_group_requests(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LLMGroupRequestsSensor

        sensor = LLMGroupRequestsSensor(mock_coordinator, "home")
        assert sensor.native_value == 50
        assert sensor._attr_unique_id == "llm_group_home_requests"

    def test_group_percent(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LLMGroupPercentSensor

        sensor = LLMGroupPercentSensor(mock_coordinator, "home")
        assert sensor.native_value == 71.4
        assert sensor._attr_name == "LLM Home Requests Today Share"
        attrs = sensor.extra_state_attributes
        assert attrs["requests"] == 50
        assert attrs["total_requests"] == 70

    def test_ip_requests(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import LLMIPRequestsSensor

        sensor = LLMIPRequestsSensor(mock_coordinator, "192.168.1.50")
        assert sensor.native_value == 50
        assert sensor._attr_unique_id == "llm_ip_192_168_1_50"
        assert sensor.extra_state_attributes["percent"] == 71.4

    def test_missing_llm_block(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import (
            LLMGroupRequestsSensor,
            LLMRequestsTodaySensor,
            LLMRequestsTotalSensor,
        )

        del mock_coordinator.data["llm"]
        assert LLMRequestsTotalSensor(mock_coordinator).native_value == 0
        assert LLMRequestsTodaySensor(mock_coordinator).native_value == 0
        assert LLMGroupRequestsSensor(mock_coordinator, "home").native_value == 0


class TestLLMEntityFactory:
    """Test the persistent LLM entity factory (entities appear over time)."""

    def test_base_entities_added_exactly_once(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import build_llm_entities

        mock_coordinator.data["llm"]["by_group"] = {}
        mock_coordinator.data["llm"]["by_ip"] = {}

        registered = set()
        first = build_llm_entities(mock_coordinator, registered)
        assert sorted(type(e).__name__ for e in first) == [
            "LLMCostTodaySensor",
            "LLMEnergyTodaySensor",
            "LLMRequestsTodaySensor",
            "LLMRequestsTotalSensor",
        ]
        assert build_llm_entities(mock_coordinator, registered) == []

    def test_groups_and_ips_that_appear_later(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import build_llm_entities

        mock_coordinator.data["llm"]["by_group"] = {"home": 50}
        mock_coordinator.data["llm"]["by_ip"] = {"192.168.1.50": 50}
        registered = set()

        first = build_llm_entities(mock_coordinator, registered)
        assert sum(type(e).__name__ == "LLMGroupRequestsSensor" for e in first) == 1
        assert sum(type(e).__name__ == "LLMGroupPercentSensor" for e in first) == 1
        assert sum(type(e).__name__ == "LLMIPRequestsSensor" for e in first) == 1

        # A new group and a new client appear later.
        mock_coordinator.data["llm"]["by_group"]["vpn"] = 20
        mock_coordinator.data["llm"]["by_ip"]["10.8.0.42"] = 20

        second = build_llm_entities(mock_coordinator, registered)
        assert sorted(type(e).__name__ for e in second) == [
            "LLMGroupPercentSensor",
            "LLMGroupRequestsSensor",
            "LLMIPRequestsSensor",
        ]

        assert build_llm_entities(mock_coordinator, registered) == []

    def test_missing_llm_block(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import build_llm_entities

        del mock_coordinator.data["llm"]
        assert build_llm_entities(mock_coordinator, set()) == []

    def test_null_maps_are_tolerated(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import build_llm_entities

        mock_coordinator.data["llm"]["by_group"] = None
        mock_coordinator.data["llm"]["by_ip"] = None
        entities = build_llm_entities(mock_coordinator, set())
        assert entities, "the base sensors must still be created"

    def test_ip_entities_are_capped(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import (
            MAX_LLM_IP_SENSORS,
            build_llm_entities,
        )

        mock_coordinator.data["llm"]["by_group"] = {}
        mock_coordinator.data["llm"]["by_ip"] = {
            f"192.168.1.{i}": 1 for i in range(1, MAX_LLM_IP_SENSORS + 10)
        }

        registered = set()
        entities = build_llm_entities(mock_coordinator, registered)
        ip_entities = [e for e in entities if type(e).__name__ == "LLMIPRequestsSensor"]
        assert len(ip_entities) == MAX_LLM_IP_SENSORS

        extra = build_llm_entities(mock_coordinator, registered)
        assert not [e for e in extra if type(e).__name__ == "LLMIPRequestsSensor"]
