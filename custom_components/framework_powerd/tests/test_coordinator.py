"""Tests for coordinator."""
import pytest
from unittest.mock import MagicMock, AsyncMock, patch, mock_open
import json


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
        from unittest.mock import patch

        from custom_components.framework_powerd import FrameworkPowerCoordinator

        # DataUpdateCoordinator.__init__ without an explicit config_entry
        # reports usage via homeassistant.helpers.frame, which requires a
        # live hass. Unit tests don't set up the frame helper, so stub it.
        with patch("homeassistant.helpers.frame.report_usage"):
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

    def test_data_includes_lifetime_energy(self, mock_coordinator):
        """Test that coordinator data includes the persisted lifetime counter."""
        assert mock_coordinator.data["power"]["lifetime_energy_kwh"] == 42.125

    def test_data_includes_system_info(self, mock_coordinator):
        """Test that coordinator mock data includes system_info."""
        assert "system_info" in mock_coordinator.data
        assert "kernel_version" in mock_coordinator.data["system_info"]
        assert "cpu_model" in mock_coordinator.data["system_info"]


class TestAlertNotifications:
    """Test alert extraction and notification helpers."""

    def test_collect_alerts_smartd_and_nvme(self, mock_coordinator):
        from custom_components.framework_powerd import FrameworkPowerCoordinator

        data = mock_coordinator.data
        data["nvme"]["devices"][0]["health"] = "warning"
        data["nvme"]["devices"][0]["issues"] = ["percentage used 85%"]

        alerts = FrameworkPowerCoordinator._collect_alerts(data)
        keys = [a[0] for a in alerts]
        assert any(key.startswith("smartd:") and key.endswith(":critical") for key in keys)
        assert any(key.startswith("nvme:/dev/nvme0:warning") for key in keys)

    def test_collect_alerts_ignores_info_and_healthy(self, mock_coordinator):
        from custom_components.framework_powerd import FrameworkPowerCoordinator

        data = mock_coordinator.data
        data["smartd"]["alerts"][0]["severity"] = "info"
        data["nvme"]["devices"][0]["health"] = "ok"

        assert FrameworkPowerCoordinator._collect_alerts(data) == []

    def test_parse_notify_service(self):
        from custom_components.framework_powerd import FrameworkPowerCoordinator

        assert FrameworkPowerCoordinator._parse_notify_service("notify.mobile_app_x") == (
            "notify",
            "mobile_app_x",
        )
        assert FrameworkPowerCoordinator._parse_notify_service("mobile_app_x") == (
            "notify",
            "mobile_app_x",
        )
        assert FrameworkPowerCoordinator._parse_notify_service("") == ("notify", "")

    def test_notification_id_is_safe(self):
        from custom_components.framework_powerd import FrameworkPowerCoordinator

        notification_id = FrameworkPowerCoordinator._notification_id(
            "nvme:/dev/nvme0:critical"
        )
        assert notification_id.startswith("framework_powerd_")
        assert "/" not in notification_id
        assert ":" not in notification_id


class TestNotificationDelivery:
    """Test that notifications are delivered once within the cooldown."""

    def test_process_alerts_dedupes_and_calls_notify(self, mock_coordinator):
        import asyncio
        from datetime import timedelta
        from unittest.mock import AsyncMock, MagicMock, patch

        from custom_components.framework_powerd import FrameworkPowerCoordinator

        coordinator = object.__new__(FrameworkPowerCoordinator)
        coordinator.hass = MagicMock()
        coordinator.hass.services.async_call = AsyncMock()
        coordinator.notify_service = "notify.mobile_app_x"
        coordinator._notified_alerts = {}
        coordinator._notify_cooldown = timedelta(hours=6)

        data = mock_coordinator.data

        with patch(
            "custom_components.framework_powerd.persistent_notification.async_create"
        ) as create:
            asyncio.run(coordinator._process_alerts(data))
            asyncio.run(coordinator._process_alerts(data))

        assert create.call_count == 1
        assert coordinator.hass.services.async_call.await_count == 1


class TestDynamicEntityFactories:
    """Test the dynamic entity registrar used for late-appearing entities."""

    @staticmethod
    def _coordinator(callbacks):
        from custom_components.framework_powerd import FrameworkPowerCoordinator

        coord = object.__new__(FrameworkPowerCoordinator)
        coord._entity_factories = []
        coord._dynamic_unsub = None
        coord.last_update_success = True
        coord.data = {}
        coord.async_add_listener = lambda cb: (callbacks.append(cb), (lambda: None))[1]
        return coord

    def test_one_shot_factory_is_dropped_after_first_result(self):
        callbacks = []
        coord = self._coordinator(callbacks)
        calls = []

        def factory(_coord):
            calls.append(1)
            return ["entity"]

        coord.register_entity_factory(factory)

        added = []
        coord.attach_dynamic_registrar(added.extend)
        coord.data = {"llm": {}}
        callbacks[0]()
        callbacks[0]()

        assert added == ["entity"]
        assert len(calls) == 1, "a one-shot factory must not run again"

    def test_persistent_factory_stays_registered_and_can_grow(self):
        callbacks = []
        coord = self._coordinator(callbacks)
        seen = []
        calls = []

        def factory(c):
            calls.append(1)
            new = []
            for key in c.data.get("keys", []):
                if key not in seen:
                    seen.append(key)
                    new.append(key)
            return new

        coord.register_entity_factory(factory, persistent=True)

        added = []
        coord.attach_dynamic_registrar(added.extend)
        coord.data = {"keys": ["a"]}
        callbacks[0]()
        coord.data = {"keys": ["a", "b"]}
        callbacks[0]()
        callbacks[0]()

        assert added == ["a", "b"]
        assert len(calls) == 3, "a persistent factory must keep running"

    def test_factories_are_skipped_on_failed_update(self):
        callbacks = []
        coord = self._coordinator(callbacks)
        calls = []

        def factory(_coord):
            calls.append(1)
            return ["entity"]

        coord.register_entity_factory(factory)
        added = []
        coord.attach_dynamic_registrar(added.extend)

        coord.last_update_success = False
        callbacks[0]()

        assert added == []
        assert calls == []
