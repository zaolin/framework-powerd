"""Tests for select and button controls."""
import asyncio
from unittest.mock import AsyncMock, MagicMock


class TestTunableModeSelect:
    """Test the tunable-mode select entity."""

    def test_current_option(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerTunableModeSelect,
        )

        entity = FrameworkPowerTunableModeSelect(mock_coordinator)
        assert entity.current_option == "powersave"
        assert entity.options == ["performance", "powersave"]
        assert entity._attr_unique_id == "tunable_mode_select"

    def test_legacy_mode_fallback(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerTunableModeSelect,
        )

        del mock_coordinator.data["tunable_mode"]
        mock_coordinator.data["mode"] = "performance"
        entity = FrameworkPowerTunableModeSelect(mock_coordinator)
        assert entity.current_option == "performance"

    def test_select_option_calls_coordinator(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerTunableModeSelect,
        )

        mock_coordinator.async_set_mode = AsyncMock()
        entity = FrameworkPowerTunableModeSelect(mock_coordinator)
        asyncio.run(entity.async_select_option("performance"))
        mock_coordinator.async_set_mode.assert_awaited_once_with("performance")


class TestResetIdleButton:
    """Test the reset-idle button."""

    def test_metadata(self, mock_coordinator):
        from custom_components.framework_powerd.button import (
            FrameworkPowerResetIdleButton,
        )

        entity = FrameworkPowerResetIdleButton(mock_coordinator)
        assert entity._attr_unique_id == "reset_idle"
        assert entity.name == "Reset Idle Timer"

    def test_press_calls_coordinator(self, mock_coordinator):
        from custom_components.framework_powerd.button import (
            FrameworkPowerResetIdleButton,
        )

        mock_coordinator.async_reset_idle = AsyncMock()
        entity = FrameworkPowerResetIdleButton(mock_coordinator)
        asyncio.run(entity.async_press())
        mock_coordinator.async_reset_idle.assert_awaited_once()


class TestServices:
    """Test service registration."""

    def test_registers_services_once(self):
        from custom_components.framework_powerd import (
            _async_register_services,
            SERVICE_REFRESH,
            SERVICE_RESET_IDLE,
            SERVICE_SET_MODE,
        )
        from custom_components.framework_powerd.const import DOMAIN

        hass = MagicMock()
        hass.services.has_service.return_value = False
        hass.data = {DOMAIN: {}}

        asyncio.run(_async_register_services(hass))

        registered = [call.args[1] for call in hass.services.async_register.call_args_list]
        assert registered == [SERVICE_SET_MODE, SERVICE_RESET_IDLE, SERVICE_REFRESH]

    def test_skips_when_already_registered(self):
        from custom_components.framework_powerd import _async_register_services

        hass = MagicMock()
        hass.services.has_service.return_value = True
        hass.data = {}

        asyncio.run(_async_register_services(hass))
        hass.services.async_register.assert_not_called()
