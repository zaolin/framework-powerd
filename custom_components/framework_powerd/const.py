"""Constants for the Framework Power Daemon integration."""

DOMAIN = "framework_powerd"

CONF_HOST = "host"
CONF_PORT = "port"
CONF_TOKEN = "token"
CONF_NAME = "name"
CONF_NOTIFY_SERVICE = "notify_service"

DEFAULT_NAME = "Framework Power"
DEFAULT_HOST = "localhost"
DEFAULT_PORT = 8080

# Power modes exposed by the AMD XDNA driver, in driver enum order.
NPU_POWER_MODES = ["default", "low", "medium", "high", "turbo"]

# Fan control modes: the EC curve or the daemon curve.
FAN_MODES = ["auto", "curve"]
