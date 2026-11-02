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


class TestNpuPowerModeSelect:
    """Test the NPU power mode select entity."""

    def test_options_and_current(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerNpuPowerModeSelect,
        )

        entity = FrameworkPowerNpuPowerModeSelect(mock_coordinator)
        assert entity.options == ["default", "low", "medium", "high", "turbo"]
        assert entity.current_option == "default"
        assert entity._attr_unique_id == "npu_power_mode_select"
        assert entity.available is True

    def test_unknown_mode_is_ignored(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerNpuPowerModeSelect,
        )

        mock_coordinator.data["npu"]["power_mode"] = "warp"
        assert FrameworkPowerNpuPowerModeSelect(mock_coordinator).current_option is None

    def test_absent_npu_is_unavailable(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerNpuPowerModeSelect,
        )

        mock_coordinator.data["npu"]["present"] = False
        entity = FrameworkPowerNpuPowerModeSelect(mock_coordinator)
        assert entity.available is False

    def test_select_option_calls_coordinator(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerNpuPowerModeSelect,
        )

        mock_coordinator.async_set_npu_power_mode = AsyncMock()
        entity = FrameworkPowerNpuPowerModeSelect(mock_coordinator)
        asyncio.run(entity.async_select_option("high"))
        mock_coordinator.async_set_npu_power_mode.assert_awaited_once_with("high")


class TestFanModeSelect:
    """Test the fan mode select entity."""

    def test_options_and_current(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerFanModeSelect,
        )

        entity = FrameworkPowerFanModeSelect(mock_coordinator)
        assert entity.options == ["auto", "curve"]
        assert entity.current_option == "curve"
        assert entity._attr_unique_id == "fan_mode_select"
        assert entity.available is True

    def test_unavailable_when_disabled(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerFanModeSelect,
        )

        mock_coordinator.data["fan"]["enabled"] = False
        entity = FrameworkPowerFanModeSelect(mock_coordinator)
        assert entity.available is False

        mock_coordinator.data["fan"]["enabled"] = True
        mock_coordinator.data["fan"]["supported"] = False
        entity = FrameworkPowerFanModeSelect(mock_coordinator)
        assert entity.available is False

    def test_select_option_calls_coordinator(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerFanModeSelect,
        )

        mock_coordinator.async_set_fan_mode = AsyncMock()
        entity = FrameworkPowerFanModeSelect(mock_coordinator)
        asyncio.run(entity.async_select_option("auto"))
        mock_coordinator.async_set_fan_mode.assert_awaited_once_with("auto")

    def test_unavailable_when_coordinator_failed(self, mock_coordinator):
        from custom_components.framework_powerd.select import (
            FrameworkPowerFanModeSelect,
        )

        mock_coordinator.last_update_success = False
        assert FrameworkPowerFanModeSelect(mock_coordinator).available is False
