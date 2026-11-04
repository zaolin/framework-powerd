"""Tests for binary sensor entities."""
import pytest
from unittest.mock import MagicMock


class TestSmartdAlertBinarySensor:
    """Test SmartdAlertBinarySensor."""

    def test_is_on_when_alert_present(self, mock_coordinator):
        """Test is_on is True when alerts present."""
        from custom_components.framework_powerd.binary_sensor import SmartdAlertBinarySensor
        
        sensor = SmartdAlertBinarySensor(mock_coordinator)
        assert sensor.is_on is True

    def test_is_off_when_no_alerts(self, mock_coordinator):
        """Test is_on is False when no alerts."""
        from custom_components.framework_powerd.binary_sensor import SmartdAlertBinarySensor
        
        mock_coordinator.data["smartd"]["alerts"] = []
        sensor = SmartdAlertBinarySensor(mock_coordinator)
        assert sensor.is_on is False

    def test_is_off_when_no_smartd_data(self, mock_coordinator):
        """Test is_on is False when no smartd data."""
        from custom_components.framework_powerd.binary_sensor import SmartdAlertBinarySensor
        
        mock_coordinator.data["smartd"] = {}
        sensor = SmartdAlertBinarySensor(mock_coordinator)
        assert sensor.is_on is False

    def test_unique_id(self, mock_coordinator):
        """Test unique_id is set correctly."""
        from custom_components.framework_powerd.binary_sensor import SmartdAlertBinarySensor
        
        sensor = SmartdAlertBinarySensor(mock_coordinator)
        assert sensor._attr_unique_id == "smartd_alert"

    def test_name(self, mock_coordinator):
        """Test name is set correctly."""
        from custom_components.framework_powerd.binary_sensor import SmartdAlertBinarySensor
        
        sensor = SmartdAlertBinarySensor(mock_coordinator)
        assert sensor._attr_name == "Alert"


class TestGameRunningBinarySensor:
    """Test GameRunningBinarySensor uses the daemon's is_game_running field."""

    def test_uses_is_game_running_not_pid(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import GameRunningBinarySensor

        mock_coordinator.data["is_game_running"] = True
        mock_coordinator.data["game_pid"] = 0
        assert GameRunningBinarySensor(mock_coordinator).is_on is True

    def test_off_when_not_running(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import GameRunningBinarySensor

        mock_coordinator.data["is_game_running"] = False
        mock_coordinator.data["game_pid"] = 4242
        assert GameRunningBinarySensor(mock_coordinator).is_on is False


class TestStorageHealthBinarySensor:
    """Test the aggregated SMART/NVMe storage health indicator."""

    def test_on_with_smartd_critical_alert(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import StorageHealthBinarySensor

        sensor = StorageHealthBinarySensor(mock_coordinator)
        assert sensor.is_on is True
        assert sensor.extra_state_attributes["issues"][0]["source"] == "smartd"

    def test_off_when_everything_healthy(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import StorageHealthBinarySensor

        mock_coordinator.data["smartd"]["alerts"] = []
        sensor = StorageHealthBinarySensor(mock_coordinator)
        assert sensor.is_on is False

    def test_on_with_nvme_warning(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import StorageHealthBinarySensor

        mock_coordinator.data["smartd"]["alerts"] = []
        mock_coordinator.data["nvme"]["devices"][0]["health"] = "warning"
        mock_coordinator.data["nvme"]["devices"][0]["issues"] = ["percentage used 85%"]
        sensor = StorageHealthBinarySensor(mock_coordinator)
        assert sensor.is_on is True
        assert sensor.extra_state_attributes["issues"][0]["severity"] == "warning"

    def test_unique_id(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import StorageHealthBinarySensor

        assert StorageHealthBinarySensor(mock_coordinator)._attr_unique_id == "storage_health"


class TestNpuPresentBinarySensor:
    """Test the NPU presence binary sensor."""

    def test_present(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import (
            NpuPresentBinarySensor,
        )

        sensor = NpuPresentBinarySensor(mock_coordinator)
        assert sensor.is_on is True
        assert sensor.available is True

    def test_absent(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import (
            NpuPresentBinarySensor,
        )

        mock_coordinator.data["npu"]["present"] = False
        assert NpuPresentBinarySensor(mock_coordinator).is_on is False

    def test_no_npu_block(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import (
            NpuPresentBinarySensor,
        )

        del mock_coordinator.data["npu"]
        sensor = NpuPresentBinarySensor(mock_coordinator)
        assert sensor.available is False


class TestNpuPresentBinarySensorAvailability:
    """The presence sensor stays available so it can report the device leaving."""

    def test_available_even_when_absent(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import (
            NpuPresentBinarySensor,
        )

        mock_coordinator.data["npu"]["present"] = False
        sensor = NpuPresentBinarySensor(mock_coordinator)
        assert sensor.available is True
        assert sensor.is_on is False


class TestBinarySensorDeviceGrouping:
    """Binary sensors live on the sub-device that owns them."""

    def test_smartd_alert_device(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import SmartdAlertBinarySensor

        info = SmartdAlertBinarySensor(mock_coordinator).device_info
        assert info["name"] == "SMART"
        assert info["model"] == "SMART monitor"
        assert ("framework_powerd", "localhost:8080:smartd") in info["identifiers"]
        assert info["via_device"] == ("framework_powerd", "localhost:8080")

    def test_storage_health_device(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import StorageHealthBinarySensor

        sensor = StorageHealthBinarySensor(mock_coordinator)
        assert sensor._attr_name == "Health"
        assert sensor.device_info["name"] == "Storage"
        assert sensor.device_info["model"] == "NVMe monitor"
        assert ("framework_powerd", "localhost:8080:storage") in sensor.device_info["identifiers"]

    def test_npu_present_device(self, mock_coordinator):
        from custom_components.framework_powerd.binary_sensor import NpuPresentBinarySensor

        sensor = NpuPresentBinarySensor(mock_coordinator)
        assert sensor._attr_name == "Present"
        assert sensor.device_info["name"] == "NPU"
        assert sensor.device_info["model"] == "AMD XDNA NPU"
        assert ("framework_powerd", "localhost:8080:npu") in sensor.device_info["identifiers"]
