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
