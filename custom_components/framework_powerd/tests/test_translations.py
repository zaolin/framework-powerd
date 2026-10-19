"""Validate the translation catalogs."""
import json
import os


def _load(relative):
    path = os.path.join(os.path.dirname(__file__), "..", relative)
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def test_strings_and_english_translation_match():
    strings = _load("strings.json")
    translation = _load("translations/en.json")
    assert strings == translation


def test_config_flow_translations_present():
    data = _load("translations/en.json")
    step = data["config"]["step"]["user"]
    for key in ("name", "host", "port", "token", "notify_service"):
        assert key in step["data"]
    assert "cannot_connect" in data["config"]["error"]
