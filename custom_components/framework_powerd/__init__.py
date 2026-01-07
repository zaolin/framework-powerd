"""The Framework Power Daemon integration."""
from __future__ import annotations

import os

from homeassistant.config_entries import ConfigEntry
from homeassistant.components.http import StaticPathConfig
from homeassistant.core import HomeAssistant

from .const import (
    DOMAIN,
    CONF_HOST,
    CONF_PORT,
    CONF_TOKEN,
    PLATFORMS,
    LOGGER
)
from .coordinator import FrameworkPowerCoordinator

async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Framework Power Daemon from a config entry."""
    hass.data.setdefault(DOMAIN, {})

    host = entry.data[CONF_HOST]
    port = entry.data[CONF_PORT]
    token = entry.data.get(CONF_TOKEN)

    api_url = f"http://{host}:{port}"

    coordinator = FrameworkPowerCoordinator(hass, api_url, token)
    await coordinator.async_config_entry_first_refresh()

    hass.data[DOMAIN][entry.entry_id] = coordinator

    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)

    entry.async_on_unload(entry.add_update_listener(update_listener))
    
    # Register custom card and logo
    # We use os.path.dirname to be relative to this file
    component_dir = os.path.dirname(__file__)
    card_path = os.path.join(component_dir, "framework-power-card.js")
    logo_path = os.path.join(component_dir, "logo.png")
    
    # Debug logging to verify paths
    LOGGER.debug(f"Registering static paths: card={card_path}, logo={logo_path}")

    # Register paths regardless of existence check to ensure HA attempts to serve them
    # and to surface permission errors if any.
    await hass.http.async_register_static_paths([
        StaticPathConfig("/framework_powerd/card.js", card_path, False),
        StaticPathConfig("/framework_powerd/logo.png", logo_path, False)
    ])

    # Auto-register Lovelace resource
    try:
        from homeassistant.components.lovelace import resources
        resource_collection = resources.ResourceStorageCollection(hass, hass.config)
        await resource_collection.async_load()
        
        resource_url = "/framework_powerd/card.js"
        # Check if resource already exists
        if not any(item["url"] == resource_url for item in resource_collection.async_items()):
            LOGGER.info("Auto-registering Framework Power Card resource")
            await resource_collection.async_create_item({
                "res_type": "module",
                "url": resource_url,
            })
    except Exception as e:
        LOGGER.warning(f"Failed to auto-register Lovelace resource: {e}")

    return True

async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a config entry."""
    if unload_ok := await hass.config_entries.async_unload_platforms(entry, PLATFORMS):
        hass.data[DOMAIN].pop(entry.entry_id)

    return unload_ok

async def update_listener(hass: HomeAssistant, entry: ConfigEntry) -> None:
    """Update listener."""
    await hass.config_entries.async_reload(entry.entry_id)
