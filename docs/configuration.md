# Configuration

The daemon reads a JSON configuration file. All settings are optional; missing
keys keep their defaults.

## File discovery

1. `--config=PATH` / `-c PATH` — explicit path. The daemon exits if it does not exist or is invalid.
2. `/etc/framework-powerd/config.json`
3. `~/.config/framework-powerd/config.json`
4. No file found — built-in defaults are used.

## Legacy keys

The `ollama` and `pricing` sections from 1.2 and earlier are ignored. Ollama
monitoring was removed in 1.3; the JSON decoder tolerates unknown keys, so the
daemon still starts, but those sections have no effect. Remove them to keep the
file accurate.

## CLI precedence

Only flags that were explicitly provided override the file. A flag whose value
happens to equal the built-in default still counts as explicit.

| Flag | Overrides |
|---|---|
| `--address` | `server.address` |
| `--port` | `server.port` |
| `--idle-timeout` | `server.idle_timeout` |
| `--jwt-secret` / `JWT_SECRET` | `server.jwt_secret` |
| `--debug` | verbose logging (no config equivalent) |
| `--disable-auth` | disables auth entirely (insecure) |

`framework-powerd --version` prints the build version.

## Reference

### `server`

| Key | Type | Default | Description |
|---|---|---|---|
| `address` | string | `localhost` | Bind address. Use `0.0.0.0` for LAN access. |
| `port` | int | `8080` | HTTP port, 1–65535. |
| `jwt_secret` | string | `""` | HMAC secret for JWT auth. Empty requires `--disable-auth`. |
| `idle_timeout` | duration | `5m` | Input inactivity before idle tunables are applied. |

### `smartd`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Watch smartd journal messages. |
| `service_unit` | string | `smartd.service` | Journald unit to follow. |
| `notify_service` | string | `""` | Optional HA notify service used by the integration for new warnings. |
| `alert_retention` | duration | `168h` | How long a sticky alert stays visible after it was last seen. |
| `state_file` | string | `/var/lib/framework-powerd/alerts.json` | Persisted alert state. |

Alerts are de-duplicated per device and category, keep `first_seen`,
`last_seen` and `count`, and survive daemon restarts. A critical alert is not
silently dropped after 30 seconds anymore.

### `nvme`

Direct NVMe SMART polling, independent of smartd journal messages.

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Poll NVMe health directly. |
| `poll_interval` | duration | `15m` | Poll interval. |
| `warn_percentage_used` | int | `80` | Wear warning threshold in percent. |
| `crit_percentage_used` | int | `90` | Wear critical threshold in percent. |
| `warn_available_spare` | int | `10` | Spare-capacity warning threshold in percent. |
| `warn_temperature_c` | int | `70` | Temperature warning threshold. |
| `crit_temperature_c` | int | `80` | Temperature critical threshold. |
| `warn_err_log_entries` | int | `1` | Error-log warning threshold. |
| `crit_err_log_entries` | int | `10` | Error-log critical threshold. |
| `max_media_errors` | int | `0` | Allowed media/data-integrity errors before critical. |

The poller uses `nvme smart-log --output-format=json` first and falls back to
`smartctl --json=o -a`. If neither tool is installed, polling is disabled and a
warning is logged. Health is exposed at `/status.nvme` and `/nvme/stats`.

### `gpu`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable AMD GPU sysfs monitoring. |
| `poll_interval` | duration | `5s` | Poll interval for GPU/CPU metrics. |

### `metrics`

| Key | Type | Default | Description |
|---|---|---|---|
| `persist` | bool | `true` | Persist the 24h/7d ring and lifetime counter. |
| `state_file` | string | `/var/lib/framework-powerd/metrics.json` | Atomic JSON state path. Required when `persist` is true. |
| `save_interval` | duration | `5m` | Periodic flush interval. |

The systemd unit creates `/var/lib/framework-powerd` via
`StateDirectory=framework-powerd`. When running the binary manually as a
non-root user, either point `state_file` to a writable path or set
`persist: false`.

### `peripherals`

Optional overrides for the peripheral power estimator. Zero values mean
"auto-detect / use the built-in default".

| Key | Type | Description |
|---|---|---|
| `vrm_loss_percent` | float | VRM loss correction in percent. |
| `fan_idle_rpm` | int | Idle fan RPM used to estimate fan power. |
| `nvme_idle_watts` | float | Per-NVMe idle power. |
| `nvme_active_watts` | float | Per-NVMe active power. |
| `wifi_idle_watts` | float | Wi-Fi idle power. |
| `wifi_active_watts` | float | Wi-Fi active power. |

## Validation

- `server.port` must be 1–65535.
- `server.idle_timeout` and `metrics.save_interval` must be valid Go durations greater than zero.
- `metrics.state_file` must not be empty when `metrics.persist` is true.

Invalid values abort startup with a clear error instead of silently falling
back.

## Example

```json
{
  "server": {
    "address": "localhost",
    "port": 8080,
    "jwt_secret": "",
    "idle_timeout": "5m"
  },
  "smartd": {
    "enabled": true,
    "service_unit": "smartd.service",
    "notify_service": "notify.mobile_app_phone",
    "alert_retention": "168h",
    "state_file": "/var/lib/framework-powerd/alerts.json"
  },
  "nvme": {
    "enabled": true,
    "poll_interval": "15m",
    "warn_percentage_used": 80,
    "crit_percentage_used": 90,
    "warn_available_spare": 10,
    "warn_temperature_c": 70,
    "crit_temperature_c": 80,
    "warn_err_log_entries": 1,
    "crit_err_log_entries": 10,
    "max_media_errors": 0
  },
  "gpu": {
    "enabled": true,
    "poll_interval": "5s"
  },
  "metrics": {
    "persist": true,
    "state_file": "/var/lib/framework-powerd/metrics.json",
    "save_interval": "5m"
  },
  "peripherals": {
    "vrm_loss_percent": 7,
    "fan_idle_rpm": 2000
  }
}
```

A ready-to-copy file lives at [`configs/config.example.json`](../configs/config.example.json).

## Credentials

Prefer `server.jwt_secret` or the `JWT_SECRET` environment variable over a
command-line secret: command-line arguments are visible in the process list.
`make install` creates `/etc/framework-powerd/env` (mode `600`) with a random
`JWT_SECRET`; the shipped unit loads it via `EnvironmentFile`.
See [Security](security.md).
