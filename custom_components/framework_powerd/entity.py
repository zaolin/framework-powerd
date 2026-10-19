"""Shared entity base for the Framework Power Daemon integration."""
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN


class FrameworkEntity(CoordinatorEntity):
    """Base entity shared by all platforms."""

    def __init__(self, coordinator):
        super().__init__(coordinator)
        self._attr_has_entity_name = True

    @property
    def device_info(self):
        data = self.coordinator.data or {}
        system_info = data.get("system_info") or {}
        version = system_info.get("daemon_version")

        info = {
            "identifiers": {(DOMAIN, f"{self.coordinator.host}:{self.coordinator.port}")},
            "name": "Framework Power Daemon",
            "manufacturer": "Framework",
            "model": "Power Daemon",
        }
        if version and version != "Unknown":
            info["sw_version"] = version
        return info
