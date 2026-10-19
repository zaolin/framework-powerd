"""Tests for diagnostics."""
import asyncio
from datetime import timedelta
from types import SimpleNamespace

from custom_components.framework_powerd.const import DOMAIN
from custom_components.framework_powerd.diagnostics import (
    async_get_config_entry_diagnostics,
)


def _run(entry, hass):
    return asyncio.run(async_get_config_entry_diagnostics(hass, entry))


def test_diagnostics_redacts_token_and_jwt_secret():
    coordinator = SimpleNamespace(
        data={
            "mode": "balanced",
            "jwt_secret": "top-secret",
            "nested": {"token": "also-secret", "value": 1},
        },
        last_update_success=True,
        last_exception=None,
        update_interval=timedelta(seconds=10),
    )
    entry = SimpleNamespace(
        entry_id="abc",
        data={"host": "localhost", "token": "entry-secret"},
    )
    hass = SimpleNamespace(data={DOMAIN: {"abc": coordinator}})

    result = _run(entry, hass)

    assert result["entry"]["host"] == "localhost"
    assert result["entry"]["token"] == "**REDACTED**"
    assert result["coordinator"]["data"]["jwt_secret"] == "**REDACTED**"
    assert result["coordinator"]["data"]["nested"]["token"] == "**REDACTED**"
    assert result["coordinator"]["data"]["nested"]["value"] == 1
    assert result["coordinator"]["update_interval_seconds"] == 10


def test_diagnostics_without_coordinator():
    entry = SimpleNamespace(entry_id="missing", data={"token": "secret"})
    hass = SimpleNamespace(data={DOMAIN: {}})

    result = _run(entry, hass)

    assert result["coordinator"] is None
    assert result["entry"]["token"] == "**REDACTED**"
