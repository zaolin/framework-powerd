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
        assert sensor._attr_name == "SMART Alert"
