"""Binary Sensor platform for Framework Power Daemon."""
from homeassistant.components.binary_sensor import (
    BinarySensorDeviceClass,
    BinarySensorEntity,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .entity import FrameworkEntity

async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the binary sensor platform."""
    coordinator = hass.data[DOMAIN][entry.entry_id]

    entities = [
        IdleBinarySensor(coordinator),
        RemotePlayBinarySensor(coordinator),
        GameRunningBinarySensor(coordinator),
        GamePausedBinarySensor(coordinator),
        SmartdAlertBinarySensor(coordinator),
    ]

    async_add_entities(entities)


class IdleBinarySensor(FrameworkEntity, BinarySensorEntity):
    _attr_name = "System Idle"
    _attr_unique_id = "is_idle"
    _attr_device_class = BinarySensorDeviceClass.RUNNING

    @property
    def is_on(self):
        return self.coordinator.data.get("is_idle")

class RemotePlayBinarySensor(FrameworkEntity, BinarySensorEntity):
    _attr_name = "Remote Play Active"
    _attr_unique_id = "is_remote_play"
    _attr_device_class = BinarySensorDeviceClass.CONNECTIVITY

    @property
    def is_on(self):
        return self.coordinator.data.get("is_remote_play")

class GameRunningBinarySensor(FrameworkEntity, BinarySensorEntity):
    _attr_name = "Game Running"
    _attr_unique_id = "is_game_running"
    
    @property
    def is_on(self):
        return self.coordinator.data.get("is_game_running", False)

class GamePausedBinarySensor(FrameworkEntity, BinarySensorEntity):
    _attr_name = "Game Paused"
    _attr_unique_id = "is_game_paused"

    @property
    def is_on(self):
        return self.coordinator.data.get("is_game_paused")

class SmartdAlertBinarySensor(FrameworkEntity, BinarySensorEntity):
    _attr_name = "SMART Alert"
    _attr_unique_id = "smartd_alert"
    _attr_device_class = BinarySensorDeviceClass.PROBLEM

    @property
    def is_on(self):
        alerts = self.coordinator.data.get("smartd", {}).get("alerts", [])
        return len(alerts) > 0
