"""Tests for coordinator."""
import pytest
from unittest.mock import MagicMock


class TestFrameworkPowerCoordinator:
    """Test FrameworkPowerCoordinator."""

    def test_notify_service_stored(self, mock_coordinator):
        """Test notify_service is stored in coordinator."""
        assert mock_coordinator.notify_service == "notify.mobile_phone"

    def test_host_stored(self, mock_coordinator):
        """Test host is stored in coordinator."""
        assert mock_coordinator.host == "localhost"

    def test_port_stored(self, mock_coordinator):
        """Test port is stored in coordinator."""
        assert mock_coordinator.port == 8080

    def test_base_url_constructed(self, mock_coordinator):
        """Test base_url is constructed correctly."""
        assert mock_coordinator.base_url == "http://localhost:8080"

    def test_headers_empty_without_token(self, mock_coordinator):
        """Test headers are empty when no token."""
        assert mock_coordinator.headers == {}

    def test_headers_with_token(self, mock_coordinator):
        """Test headers include Authorization when token set."""
        from custom_components.framework_powerd import FrameworkPowerCoordinator
        
        coordinator = FrameworkPowerCoordinator(
            MagicMock(), "localhost", 8080, "test-token", "notify.mobile_phone"
        )
        
        assert "Authorization" in coordinator.headers
        assert coordinator.headers["Authorization"] == "Bearer test-token"

    def test_data_includes_smartd(self, mock_coordinator):
        """Test that coordinator data includes smartd."""
        assert "smartd" in mock_coordinator.data
        assert "alerts" in mock_coordinator.data["smartd"]
        assert "notify_service" in mock_coordinator.data["smartd"]

    def test_data_includes_power(self, mock_coordinator):
        """Test that coordinator data includes power info."""
        assert "power" in mock_coordinator.data
        assert "current" in mock_coordinator.data["power"]

    def test_update_interval_default(self, mock_coordinator):
        """Test update_interval is set."""
        # Coordinator has update_interval from DataUpdateCoordinator
        # We can't directly test timedelta without async, but verify structure
        assert hasattr(mock_coordinator, 'update_interval')
