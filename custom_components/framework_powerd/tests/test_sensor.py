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


class TestOllamaModelsSensor:
    """Test OllamaModelsSensor — shows model count + structured per-model details."""

    def test_native_value_returns_count(self, mock_coordinator):
        """native_value should return the number of loaded models."""
        from custom_components.framework_powerd.sensor import OllamaModelsSensor

        sensor = OllamaModelsSensor(mock_coordinator)
        assert sensor.native_value == 1  # conftest has 1 model

    def test_native_value_no_models(self, mock_coordinator):
        """native_value should return 0 when no models loaded."""
        from custom_components.framework_powerd.sensor import OllamaModelsSensor

        mock_coordinator.data["ollama"]["models"] = []
        sensor = OllamaModelsSensor(mock_coordinator)
        assert sensor.native_value == 0

    def test_extra_state_attributes_contains_model_details(self, mock_coordinator):
        """extra_state_attributes should contain per-model detail dicts."""
        from custom_components.framework_powerd.sensor import OllamaModelsSensor

        sensor = OllamaModelsSensor(mock_coordinator)
        attrs = sensor.extra_state_attributes

        assert "models" in attrs
        assert len(attrs["models"]) == 1
        model = attrs["models"][0]
        assert model["name"] == "llama3:8b"
        assert model["family"] == "llama"
        assert model["parameter_size"] == "8.0B"
        assert model["quantization_level"] == "Q4_K_M"
        assert model["vram_gib"] == round(4000000000 / (1024**3), 2)
        assert model["context_length"] == 8192

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import OllamaModelsSensor

        sensor = OllamaModelsSensor(mock_coordinator)
        assert sensor._attr_unique_id == "ollama_models"


class TestOllamaTotalRequestsSensor:
    """Test OllamaTotalRequestsSensor — sums all group + ungrouped requests."""

    def test_native_value_sums_groups_and_ungrouped(self, mock_coordinator):
        """Should sum by_group counts + ungrouped count."""
        from custom_components.framework_powerd.sensor import OllamaTotalRequestsSensor

        mock_coordinator.data["ollama"]["by_group"] = {
            "lan": {"count": 5},
            "tailscale": {"count": 3},
        }
        mock_coordinator.data["ollama"]["ungrouped"] = {"count": 2}

        sensor = OllamaTotalRequestsSensor(mock_coordinator)
        assert sensor.native_value == 10  # 5 + 3 + 2

    def test_native_value_no_data(self, mock_coordinator):
        """Should return 0 when no requests recorded."""
        from custom_components.framework_powerd.sensor import OllamaTotalRequestsSensor

        sensor = OllamaTotalRequestsSensor(mock_coordinator)
        assert sensor.native_value == 0

    def test_native_value_only_ungrouped(self, mock_coordinator):
        """Should handle only ungrouped (no by_group)."""
        from custom_components.framework_powerd.sensor import OllamaTotalRequestsSensor

        mock_coordinator.data["ollama"]["by_group"] = {}
        mock_coordinator.data["ollama"]["ungrouped"] = {"count": 7}

        sensor = OllamaTotalRequestsSensor(mock_coordinator)
        assert sensor.native_value == 7

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import OllamaTotalRequestsSensor

        sensor = OllamaTotalRequestsSensor(mock_coordinator)
        assert sensor._attr_unique_id == "ollama_total_requests"


class TestOllamaVersionSensor:
    """Test OllamaVersionSensor."""

    def test_native_value(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import OllamaVersionSensor

        sensor = OllamaVersionSensor(mock_coordinator)
        assert sensor.native_value == "0.23.0"

    def test_native_value_unknown(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import OllamaVersionSensor

        mock_coordinator.data["ollama"]["ollama_version"] = "Unknown"
        sensor = OllamaVersionSensor(mock_coordinator)
        assert sensor.native_value == "Unknown"

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import OllamaVersionSensor

        sensor = OllamaVersionSensor(mock_coordinator)
        assert sensor._attr_unique_id == "ollama_version"


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
        assert sensor.native_value == "1.2.0"

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


class TestSystemVRAMUsedSensor:
    """Test SystemVRAMUsedSensor — GPU VRAM minus Ollama VRAM."""

    def test_native_value(self, mock_coordinator):
        """System VRAM = GPU VRAM used - Ollama loaded VRAM."""
        from custom_components.framework_powerd.sensor import SystemVRAMUsedSensor

        # conftest: gpu vram_used_bytes = 2.78 GiB, ollama loaded_vram_bytes = 0
        # system_vram = 2.78 - 0 = 2.78 GiB
        sensor = SystemVRAMUsedSensor(mock_coordinator)
        gpu_vram = mock_coordinator.data.get("gpu", {}).get("vram_used_bytes", 0)
        ollama_vram = mock_coordinator.data.get("ollama", {}).get("loaded_vram_bytes", 0)
        expected = round(max(gpu_vram - ollama_vram, 0) / (1024**3), 2)
        assert sensor.native_value == expected

    def test_native_value_with_ollama_loaded(self, mock_coordinator):
        """When Ollama has models loaded, system VRAM should be the remainder."""
        from custom_components.framework_powerd.sensor import SystemVRAMUsedSensor

        # Set GPU VRAM to 37.57 GiB, Ollama to 31.91 GiB
        mock_coordinator.data["gpu"]["vram_used_bytes"] = int(37.57 * (1024**3))
        mock_coordinator.data["ollama"]["loaded_vram_bytes"] = int(31.91 * (1024**3))
        sensor = SystemVRAMUsedSensor(mock_coordinator)
        # system_vram = 37.57 - 31.91 = 5.66 GiB (approximately)
        assert 5.5 < sensor.native_value < 5.8

    def test_native_value_no_ollama(self, mock_coordinator):
        """When no Ollama data, system VRAM should equal GPU VRAM."""
        from custom_components.framework_powerd.sensor import SystemVRAMUsedSensor

        mock_coordinator.data["ollama"] = {}
        sensor = SystemVRAMUsedSensor(mock_coordinator)
        gpu_vram = mock_coordinator.data.get("gpu", {}).get("vram_used_bytes", 0)
        expected = round(gpu_vram / (1024**3), 2)
        assert sensor.native_value == expected

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.sensor import SystemVRAMUsedSensor

        sensor = SystemVRAMUsedSensor(mock_coordinator)
        assert sensor._attr_unique_id == "system_vram_used"
