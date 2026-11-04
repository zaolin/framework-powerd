"""Shared entity base for the Framework Power Daemon integration."""

from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN

MAIN_DEVICE_NAME = "Framework Power Daemon"


class FrameworkEntity(CoordinatorEntity):
    """Base entity shared by all platforms.

    Entities with a non-empty device_key are attached to a sub-device nested
    under the main daemon device. That keeps the GPU, NPU, fan, LLM and NVMe
    entities out of one long list on the daemon's device page.
    """

    device_key = ""
    device_name = MAIN_DEVICE_NAME
    device_model = "Power Daemon"

    def __init__(self, coordinator):
        super().__init__(coordinator)
        self._attr_has_entity_name = True

    @property
    def _parent_identifier(self):
        return (DOMAIN, f"{self.coordinator.host}:{self.coordinator.port}")

    @property
    def _device_identifier(self):
        if not self.device_key:
            return self._parent_identifier
        return (
            DOMAIN,
            f"{self.coordinator.host}:{self.coordinator.port}:{self.device_key}",
        )

    @property
    def device_info(self):
        data = self.coordinator.data or {}
        system_info = data.get("system_info") or {}
        version = system_info.get("daemon_version")

        info = {
            "identifiers": {self._device_identifier},
            "name": self.device_name,
            "manufacturer": "Framework",
            "model": self.device_model,
        }
        if self.device_key:
            info["via_device"] = self._parent_identifier
        if version and version != "Unknown":
            info["sw_version"] = version
        return info
