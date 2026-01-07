"""Constants for Framework Power Daemon."""
from logging import Logger, getLogger

DOMAIN = "framework_powerd"
LOGGER: Logger = getLogger(__package__)

NAME = "Framework Power Daemon"
VERSION = "1.0.0"

CONF_HOST = "host"
CONF_PORT = "port"
CONF_TOKEN = "token"
CONF_CUSTOM_NAME = "custom_name"

DEFAULT_HOST = "localhost"
DEFAULT_PORT = 7890
DEFAULT_SCAN_INTERVAL = 30
DEFAULT_NAME = "Framework Power"

PLATFORMS = ["sensor", "select"]
