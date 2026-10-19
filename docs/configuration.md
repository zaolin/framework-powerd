# Configuration

The daemon reads a JSON configuration file. All settings are optional; missing
keys keep their defaults.

## File discovery

1. `--config=PATH` / `-c PATH` — explicit path. The daemon exits if it does not exist or is invalid.
2. `/etc/framework-powerd/config.json`
3. `~/.config/framework-powerd/config.json`
4. No file found — built-in defaults are used.

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
| `notify_service` | string | `""` | Optional HA notify service metadata. |
| `alert_retention` | duration | `30s` | How long alerts stay visible. |

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
    "idle_timeout": "5m"
  },
  "smartd": {
    "enabled": true,
    "service_unit": "smartd.service",
    "notify_service": "notify.mobile_app_phone",
    "alert_retention": "30s"
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
