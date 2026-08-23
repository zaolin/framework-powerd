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

    The shape mirrors the /status + /ollama/stats JSON the Go daemon emits,
    so sensor/binary_sensor entity tests can read .data without a live HA setup.
    """
    coord = MagicMock()
    coord.host = "localhost"
    coord.port = 8080
    coord.notify_service = "notify.mobile_phone"
    coord.base_url = "http://localhost:8080"
    coord.headers = {}
    coord.data = {
        "mode": "performance",
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
        },
        "network_devices": [],
        "smartd": {
            "alerts": [
                {"device": "/dev/sda", "fail_type": "SelfTest", "message": "FAILED SMART self-test"},
            ],
            "last_check": "2026-01-01T00:00:00Z",
            "notify_service": "notify.mobile_phone",
        },
        "ollama": {
            "by_ip": {},
            "by_group": {},
            "ungrouped": {"count": 0},
            "currency": "EUR",
            "price_per_kwh": 0.30,
            "ollama_version": "0.23.0",
            "models": [
                {
                    "name": "llama3:8b",
                    "model": "llama3:8b",
                    "size": 4661219072,
                    "size_vram": 4000000000,
                    "digest": "a6990ed6b5e1f3d4",
                    "details": {
                        "parent_model": "",
                        "format": "gguf",
                        "family": "llama",
                        "families": ["llama"],
                        "parameter_size": "8.0B",
                        "quantization_level": "Q4_K_M",
                    },
                    "expires_at": "",
                    "context_length": 8192,
                },
            ],
            "loaded_vram_bytes": 4000000000,
        },
        "system_info": {
            "kernel_version": "6.12.1-cachyos-x86_64",
            "os_name": "CachyOS",
            "os_version": "1.0",
            "cpu_model": "AMD Ryzen AI 7 350 w/ Radeon 860M",
            "cpu_count": 16,
            "total_ram_bytes": 48838284 * 1024,
            "daemon_version": "1.2.0",
        },
        "gpu": {
            "vram_used_bytes": 2984828928,
            "vram_total_bytes": 17179869184,
            "gtt_used_bytes": 1143558144,
            "gtt_total_bytes": 25001775104,
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
