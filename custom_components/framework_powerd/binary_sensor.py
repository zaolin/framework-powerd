"""Binary Sensor platform for Framework Power Daemon."""
from homeassistant.components.binary_sensor import (
    BinarySensorDeviceClass,
    BinarySensorEntity,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN

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
    ]

    async_add_entities(entities)


class FrameworkEntity(CoordinatorEntity):
    """Base entity."""

    def __init__(self, coordinator):
        super().__init__(coordinator)
        self._attr_has_entity_name = True
    
    @property
    def device_info(self):
        return {
            "identifiers": {(DOMAIN, f"{self.coordinator.host}:{self.coordinator.port}")},
            "name": "Framework Power Daemon",
            "manufacturer": "Framework",
            "model": "Power Daemon",
        }

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
        return self.coordinator.data.get("game_pid", 0) > 0

class GamePausedBinarySensor(FrameworkEntity, BinarySensorEntity):
    _attr_name = "Game Paused"
    _attr_unique_id = "is_game_paused"

    @property
    def is_on(self):
        return self.coordinator.data.get("is_game_paused")
