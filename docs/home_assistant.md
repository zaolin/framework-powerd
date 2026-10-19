# Home Assistant integration

The integration polls the daemon's `/status` endpoint every 10 seconds. It is
installed through HACS as a custom integration.

## Setup

1. HACS → Custom repositories → add `https://github.com/zaolin/framework-powerd`
   as category **Integration**.
2. Install **Framework Power Daemon** and restart Home Assistant.
3. Settings → Devices & Services → Add Integration → **Framework Power Daemon**.
4. Enter host, port, JWT token and an optional notify service.

The token is the output of:

```bash
framework-powerd token --jwt-secret="mysecret"
```

## Entities

### Controls and state

| Entity | Type | Source |
|---|---|---|
| Power Mode | sensor | `mode` (always `balanced`) |
| System Idle | binary sensor | `is_idle` |
| Remote Play Active | binary sensor | `is_remote_play` |
| Game Running | binary sensor | `is_game_running` |
| Game Paused | binary sensor | `is_game_paused` |
| Game PID | sensor | `game_pid` |
| Uptime | sensor | `uptime_seconds` |
| Polling Interval | number (config) | local coordinator interval |

### Power and energy

| Entity | Unit | Source |
|---|---|---|
| Estimated Total Power | W | `power.current.estimated_total_watts` |
| Peripheral Power | W | `power.current.peripheral_watts` |
| Energy (24h) | kWh | `power.energy_24h_kwh` |
| Energy (7 Days) | kWh | `power.energy_7d_kwh` |
| Energy (Total) | kWh | `power.lifetime_energy_kwh` |

The 24h, 7d and lifetime values are persisted by the daemon and survive daemon
restarts. The lifetime sensor is a monotonic `TOTAL_INCREASING` counter.

### RAM

| Entity | Unit | Source |
|---|---|---|
| Total RAM | GiB | `system_info.total_ram_bytes` (diagnostic) |
| RAM Used | GiB | `memory.used_bytes` |
| RAM Free | GiB | `memory.free_bytes` (literal `MemFree`) |
| RAM Available | GiB | `memory.available_bytes` |
| RAM Usage | % | `memory.used_percent` |

`RAM Used` is `total - available`, which is the value that reflects real memory
pressure. `RAM Free` is the kernel's literal free counter and is usually much
smaller because page cache counts as used.

### GPU

Only present when `gpu.enabled` is true.

| Entity | Unit | Source |
|---|---|---|
| GPU Temperature | °C | `gpu.temperature_celsius` |
| GPU Power | W | `gpu.power_watts` |
| GPU VRAM Used | GiB | `gpu.vram_used_bytes` |
| GPU VRAM Total | GiB | `gpu.vram_total_bytes` |
| GPU VRAM Free | GiB | `gpu.vram_free_bytes` |
| GPU VRAM Usage | % | `gpu.vram_used_percent` |
| GPU GTT Used | GiB | `gpu.gtt_used_bytes` |
| GPU GTT Total | GiB | `gpu.gtt_total_bytes` |
| GPU GTT Free | GiB | `gpu.gtt_free_bytes` |
| GPU GTT Usage | % | `gpu.gtt_used_percent` |
| CPU Usage | % | `gpu.cpu_usage_percent` |

VRAM, GTT and RAM are all rendered in GiB. If the daemon is too old to send
`*_free_bytes` or `*_used_percent`, the integration derives them from used/total.

### SMART

Only present when `smartd.enabled` is true.

| Entity | Type | Source |
|---|---|---|
| SMART Alert | binary sensor | any active alert |
| SMART Alerts | sensor | number of alerts |
| SMART `<device>` | sensor | per-device alert count; created dynamically |

SMART alerts are sticky and persisted by the daemon. Each alert has a severity,
first/last seen timestamp and repeat count in the `alerts` attribute.

### NVMe

Only present when `nvme.enabled` is true and devices are detected.

| Entity | Unit | Source |
|---|---|---|
| NVMe `<device>` Health | text (`ok`/`warning`/`critical`) | `health` |
| NVMe `<device>` Percentage Used | % | `percentage_used` |
| NVMe `<device>` Available Spare | % | `available_spare` |
| NVMe `<device>` Media Errors | count | `media_errors` |
| NVMe `<device>` Error Log Entries | count | `error_log_entries` |
| NVMe `<device>` Critical Warning | bitmask | `critical_warning` |
| NVMe `<device>` Temperature | °C | `temperature_celsius` |
| NVMe `<device>` Data Written | TB (decimal) | `data_written_bytes` |
| NVMe `<device>` Power-On Hours | h | `power_on_hours` |
| NVMe `<device>` Unsafe Shutdowns | count | `unsafe_shutdowns` |

### Storage health

| Entity | Type | Meaning |
|---|---|---|
| Storage Health | binary sensor (`problem`) | on when any SMART alert or NVMe device is warning/critical |

The `issues` attribute lists the source, device, severity and message.

## Notifications

When a new SMART/NVMe warning or critical condition appears, the integration:

1. Creates a Home Assistant persistent notification.
2. Calls the configured `notify_service` if one is set.

The integration field takes precedence; if it is empty, the daemon's
`smartd.notify_service` value from `/smartd/stats` is used as a fallback.

Notifications are deduplicated per alert key and severity with a six-hour
cooldown, so a persistent warning does not spam the phone every poll cycle.
The key includes the severity, so escalation from warning to critical notifies
again.

Leave `notify_service` empty if you prefer to trigger your own automation from
the Storage Health binary sensor.

### System information

Kernel Version, Operating System, CPU Model, Total RAM and Daemon Version are
marked as **diagnostic** entities, so they stay out of the normal dashboard by
default. The device page also shows the daemon's `sw_version`.

## Dynamic entities

Per-device SMART sensors appear when the first alert for that device is
received, even if the device was not present at setup time. New RAM/GPU/NVMe
sensors are created on the first update that contains the corresponding block.

## Diagnostics

The integration supports **Download diagnostics** from the device page. The
download includes the coordinator state, update interval and last exception.
The JWT token and any `jwt_secret`/`password`/`authorization` fields are
redacted automatically.

## Dashboard example

```yaml
type: entities
title: Framework Power
entities:
  - sensor.estimated_total_power
  - sensor.energy_24h
  - sensor.energy_7_days
  - sensor.energy_total
  - sensor.ram_used
  - sensor.ram_free
  - sensor.ram_usage
  - sensor.gpu_vram_used
  - sensor.gpu_vram_free
  - sensor.gpu_vram_usage
  - sensor.gpu_gtt_used
  - sensor.gpu_gtt_free
  - sensor.gpu_gtt_usage
  - sensor.cpu_usage
  - binary_sensor.storage_health
  - sensor.nvme_dev_nvme0_health
  - sensor.nvme_dev_nvme0_percentage_used
  - sensor.nvme_dev_nvme0_available_spare
  - sensor.nvme_dev_nvme0_temperature
  - sensor.nvme_dev_nvme0_data_written
```

Entity IDs depend on your naming; check Developer Tools → States after setup.

## Troubleshooting

- `cannot_connect`: check host/port, that the daemon listens on an address
  reachable from HA, and that the token matches `server.jwt_secret`.
- Entities become unavailable: the coordinator reports a failed `/status` call.
  Check the daemon logs and `/healthz` on the daemon host.
- RAM/GPU sensors missing: update the daemon first. The integration creates them
  when the fields appear in `/status`.
- GTT totals are large: GTT is shared system memory, not dedicated VRAM.

See [Troubleshooting](troubleshooting.md) for daemon-side issues.
