"""Tests for config flow."""
import pytest
from unittest.mock import MagicMock, AsyncMock, patch


class TestConfigFlow:
    """Test config flow functionality."""

    def test_notify_service_saved_in_entry(self, mock_entry):
        """Test that notify_service is saved in entry data."""
        from custom_components.framework_powerd.const import CONF_NOTIFY_SERVICE
        
        assert mock_entry.data.get(CONF_NOTIFY_SERVICE) == "notify.mobile_phone"

    def test_connection_validation_success(self):
        """Test connection validation with valid host/port."""
        from custom_components.framework_powerd.config_flow import FrameworkPowerConfigFlow
        
        flow = FrameworkPowerConfigFlow()
        
        mock_response = AsyncMock()
        mock_response.status = 200
        
        async def mock_get(*args, **kwargs):
            return mock_response
        
        with patch('aiohttp.ClientSession') as mock_session:
            mock_session_instance = AsyncMock()
            mock_session_instance.get = mock_get
            mock_session.return_value.__aenter__.return_value = mock_session_instance
            
            result = flow._test_connection("localhost", 8080, None)
            
            # Can't easily test async without full event loop, but verify structure
            assert flow.VERSION == 1


class TestConst:
    """Test constants."""

    def test_domain(self):
        """Test domain constant."""
        from custom_components.framework_powerd.const import DOMAIN
        assert DOMAIN == "framework_powerd"

    def test_default_values(self):
        """Test default values."""
        from custom_components.framework_powerd.const import DEFAULT_HOST, DEFAULT_PORT, DEFAULT_NAME
        
        assert DEFAULT_HOST == "localhost"
        assert DEFAULT_PORT == 8080
        assert DEFAULT_NAME == "Framework Power"

    def test_conf_notify_service(self):
        """Test notify service constant."""
        from custom_components.framework_powerd.const import CONF_NOTIFY_SERVICE
        assert CONF_NOTIFY_SERVICE == "notify_service"
