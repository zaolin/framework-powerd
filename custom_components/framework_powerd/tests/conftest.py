"""Pytest configuration for Framework Power Daemon tests."""
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import pytest
from unittest.mock import MagicMock


@pytest.fixture
def mock_coordinator():
    """Return a MagicMock coordinator pre-loaded with representative data.

    The shape mirrors the /status JSON the Go daemon emits,
    so sensor/binary_sensor entity tests can read .data without a live HA setup.
    """
    coord = MagicMock()
    coord.host = "localhost"
    coord.port = 8080
    coord.notify_service = "notify.mobile_phone"
    coord.base_url = "http://localhost:8080"
    coord.headers = {}
    coord.data = {
        "mode": "balanced",
        "is_idle": False,
        "seconds_until_idle": 300,
        "is_game_running": False,
        "is_game_paused": False,
        "is_remote_play": False,
        "game_pid": 0,
        "uptime_seconds": 1234.0,
        "power": {
            "current": {
                "pkg_watt": 12.5,
                "cor_watt": 3.2,
                "ram_watt": 1.1,
                "estimated_total_watts": 18.2,
                "peripheral_watts": 3.5,
            },
            "energy_24h_kwh": 0.5,
            "energy_7d_kwh": 3.5,
            "lifetime_energy_kwh": 42.125,
        },
        "network_devices": [],
        "memory": {
            "total_bytes": 48838284 * 1024,
            "free_bytes": 8 * 1024**3,
            "available_bytes": 20 * 1024**3,
            "used_bytes": 48838284 * 1024 - 20 * 1024**3,
            "used_percent": (48838284 * 1024 - 20 * 1024**3) / (48838284 * 1024) * 100,
        },
        "smartd": {
            "alerts": [
                {
                    "id": "/dev/sda|selftest",
                    "device": "/dev/sda",
                    "fail_type": "selftest",
                    "severity": "critical",
                    "message": "FAILED SMART self-test",
                    "raw": "Device: /dev/sda, FAILED SMART self-test",
                    "first_seen": "2026-01-01T00:00:00Z",
                    "last_seen": "2026-01-01T00:00:00Z",
                    "count": 1,
                    "timestamp": "2026-01-01T00:00:00Z",
                },
            ],
            "last_check": "2026-01-01T00:00:00Z",
            "notify_service": "notify.mobile_phone",
        },
        "nvme": {
            "enabled": True,
            "last_update": "2026-01-01T00:00:00Z",
            "devices": [
                {
                    "device": "/dev/nvme0",
                    "model": "WD_BLACK SN850X 2000GB",
                    "serial": "23202F801634",
                    "firmware": "620361WD",
                    "source": "nvme",
                    "critical_warning": 0,
                    "percentage_used": 7,
                    "available_spare": 100,
                    "available_spare_threshold": 10,
                    "media_errors": 0,
                    "error_log_entries": 0,
                    "temperature_celsius": 49,
                    "data_written_bytes": 120 * 1000**4,
                    "power_on_hours": 500,
                    "unsafe_shutdowns": 4,
                    "health": "ok",
                    "issues": [],
                    "last_update": "2026-01-01T00:00:00Z",
                }
            ],
        },
        "system_info": {
            "kernel_version": "6.12.1-cachyos-x86_64",
            "os_name": "CachyOS",
            "os_version": "1.0",
            "cpu_model": "AMD Ryzen AI 7 350 w/ Radeon 860M",
            "cpu_count": 16,
            "total_ram_bytes": 48838284 * 1024,
            "daemon_version": "1.4.0",
        },
        "gpu": {
            "vram_used_bytes": 2984828928,
            "vram_total_bytes": 17179869184,
            "vram_free_bytes": 17179869184 - 2984828928,
            "vram_used_percent": 2984828928 / 17179869184 * 100,
            "gtt_used_bytes": 1143558144,
            "gtt_total_bytes": 25001775104,
            "gtt_free_bytes": 25001775104 - 1143558144,
            "gtt_used_percent": 1143558144 / 25001775104 * 100,
            "cpu_usage_percent": 0.6,
            "temperature_celsius": 49,
            "power_watts": 9.0,
        },
    }
    return coord


@pytest.fixture
def mock_entry():
    """Return a MagicMock config entry mirroring what config_flow creates.

    Used by TestConfigFlow.test_notify_service_saved_in_entry to assert the
    notify_service field is persisted in the entry data.
    """
    entry = MagicMock()
    entry.data = {
        "name": "Framework Power",
        "host": "localhost",
        "port": 8080,
        "token": None,
        "notify_service": "notify.mobile_phone",
    }
    return entry
