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

## Devices

Everything is nested under the main **Framework Power Daemon** device, with one
sub-device per component. That keeps the daemon's own page short and puts each
entity where it belongs.

| Device | Contents |
|---|---|
| Framework Power Daemon | power and energy, RAM, idle/game state, CPU temperature and usage, daemon health, system information, controls |
| GPU | temperature, power, VRAM and GTT usage |
| NPU | power, column utilization, clocks, TOPs, contexts, firmware, power mode |
| Fan | speed, duty, mainboard and CPU board temperatures, mode |
| LLM | request counters, energy and cost |
| NVMe nvme0 (one per drive) | health, temperature, wear, spare, errors, data written |
| Storage | aggregated status and the Storage Health binary sensor |
| SMART | alerts and per-device SMART markers |

Every temperature is a normal sensor. Capacities and internals (VRAM/GTT/RAM
total and free, uptime, clocks, NVMe wear counters, daemon health, system
information) are diagnostic, so they stay out of the main entity list. The
polling interval is a configuration entity. The tables below list the entity
name on its device; the full name is `<Device> <Entity>`.

## Entities

### Controls and state

| Entity | Type | Source |
|---|---|---|
| Power Mode | sensor | `mode` (always `balanced`) |
| Tunable Mode | sensor (diagnostic) | `tunable_mode` (`performance`/`powersave`) |
| Power Tunable Mode | select | sets `performance`/`powersave` via `/mode` |
| Reset Idle Timer | button | calls `/activity` |
| Daemon Health | sensor (diagnostic) | `health` block from `/status` |
| System Idle | binary sensor | `is_idle` |
| Remote Play Active | binary sensor | `is_remote_play` |
| Game Running | binary sensor | `is_game_running` |
| Game Paused | binary sensor | `is_game_paused` |
| Game PID | sensor (diagnostic) | `game_pid` |
| Uptime | sensor (diagnostic) | `uptime_seconds` |
| Polling Interval | number (config) | local coordinator interval |

### Power, energy and memory

| Entity | Unit | Source |
|---|---|---|
| Estimated Total Power | W | `power.current.estimated_total_watts` |
| Peripheral Power | W | `power.current.peripheral_watts` |
| Energy (24h) | kWh | `power.energy_24h_kwh` |
| Energy (7 Days) | kWh | `power.energy_7d_kwh` |
| Energy (Total) | kWh | `power.lifetime_energy_kwh` |
| RAM Used | GiB | `memory.used_bytes` |
| RAM Usage | % | `memory.used_percent` |
| RAM Free | GiB (diagnostic) | `memory.free_bytes` (literal `MemFree`) |
| RAM Available | GiB (diagnostic) | `memory.available_bytes` |

The energy values survive daemon restarts; the lifetime sensor is a monotonic
`TOTAL_INCREASING` counter. `RAM Used` is `total - available`, which reflects
real memory pressure; `RAM Free` is the kernel's literal free counter and is
usually much smaller because page cache counts as used.

### System information

All diagnostic, on the daemon device: **Kernel Version**, **Operating System**,
**CPU Model**, **Total RAM** and **Daemon Version**. The device page also shows
the daemon version as `sw_version`.

### GPU

Only present when `gpu.enabled` is true.

| Entity | Unit | Source |
|---|---|---|
| Temperature | °C | `gpu.temperature_celsius` |
| Power | W | `gpu.power_watts` |
| VRAM Used | GiB | `gpu.vram_used_bytes` |
| VRAM Total | GiB (diagnostic) | `gpu.vram_total_bytes` |
| VRAM Free | GiB (diagnostic) | `gpu.vram_free_bytes` |
| VRAM Usage | % | `gpu.vram_used_percent` |
| GTT Used | GiB | `gpu.gtt_used_bytes` |
| GTT Total | GiB (diagnostic) | `gpu.gtt_total_bytes` |
| GTT Free | GiB (diagnostic) | `gpu.gtt_free_bytes` |
| GTT Usage | % | `gpu.gtt_used_percent` |

Two more sensors live on the daemon device:

| Entity | Unit | Source |
|---|---|---|
| CPU Temperature | °C | `gpu.cpu_temperature_celsius` (k10temp) |
| CPU Usage | % | `gpu.cpu_usage_percent` |

VRAM, GTT and RAM are rendered in GiB. If the daemon is too old to send
`*_free_bytes` or `*_used_percent`, the integration derives them from
used/total. GTT is shared system memory, not dedicated VRAM. CPU Temperature is
unavailable when the kernel exposes no `k10temp` sensor.

### NPU

See [AMD XDNA NPU monitoring](npu.md).

| Entity | Unit | Source |
|---|---|---|
| Present | binary sensor | `npu.present` |
| Power | W | `npu.power_watts` |
| Column Utilization | % | `npu.column_utilization_avg` |
| MP-NPU Clock | MHz (diagnostic) | `npu.mp_npu_clock_mhz` |
| H Clock | MHz (diagnostic) | `npu.h_clock_mhz` |
| TOPs | count (diagnostic) | `npu.tops_current` |
| Active Contexts | count (diagnostic) | number of `npu.contexts` |
| Firmware | text (diagnostic) | `npu.firmware_version` |
| Power State | text (diagnostic) | `npu.power_state` |
| Power Mode | select | `npu.power_mode` |

`Present` and the Power Mode select are registered unconditionally. With NPU
monitoring enabled but no device, `Present` is available and reports off while
the Power Mode select is unavailable. Both are unavailable while NPU monitoring
is disabled, and the telemetry sensors are only created when an NPU is present.
The column sensor exposes per-column percentages and the maximum as attributes;
the context sensor exposes the full context table, including the owning PID.

### Fan

See [EC fan control](fan.md).

| Entity | Unit | Source |
|---|---|---|
| Speed | RPM (diagnostic) | `fan.sensors.fan_rpm` |
| Duty | % (diagnostic) | `fan.duty_percent` |
| Mainboard Temperature | °C | `fan.sensors.board_celsius` |
| CPU Board Temperature | °C | `fan.sensors.cpu_ec_celsius` |
| Mode | select | `fan.mode` (`auto`, `curve`) |

The fan entities are unavailable while the controller is unsupported, for
example when `framework_tool` is missing. Setting Mode to `auto` returns control
to the EC immediately.

### LLM requests

Only present when `llm.enabled` is true. See
[LLM request accounting](llm.md).

| Entity | Unit | Source |
|---|---|---|
| Requests (Total) | count | `llm.total.count` |
| Requests Today | count | `llm.today.total` |
| Energy Today | kWh | `llm.today.total_energy_kwh` |
| Cost Today | currency | `llm.today.total_cost` |
| `<Group>` Requests Today | count | `llm.today.by_group` |
| `<Group>` Requests Today Share | % | group share of today |
| `<IP>` Requests Today | count | `llm.today.by_ip` |

Group and client-IP entities are created as those clients first appear, without
reloading the integration. The share for a single IP is the `percent` attribute
of its sensor. At most 25 per-IP sensors are created; the full per-IP data stays
in the `by_ip` attribute of the today sensor. Daily history is the `history`
attribute of the total and today sensors, so it can be graphed without extra
entities.

### SMART

| Entity | Type | Source |
|---|---|---|
| Alert | binary sensor | any active alert |
| Alerts | sensor | number of alerts |
| `<device>` | sensor | per-device alert count; created dynamically |

The Alert and Alerts entities are always registered and report no alert while
`smartd.enabled` is false. Per-device sensors appear with the first alert for
that device. SMART alerts are sticky and persisted by the daemon. Each alert
has a severity, first/last seen timestamp and repeat count in the `alerts`
attribute.

### NVMe

Only present when `nvme.enabled` is true and devices are detected. Each drive
gets its own device named `NVMe nvme0`, `NVMe nvme1` and so on, with the drive
model as the device model and the firmware as the software version.

| Entity | Unit | Source |
|---|---|---|
| Health | text (`ok`/`warning`/`critical`) | `health` |
| Temperature | °C | `temperature_celsius` |
| Percentage Used | % (diagnostic) | `percentage_used` |
| Available Spare | % (diagnostic) | `available_spare` |
| Media Errors | count (diagnostic) | `media_errors` |
| Error Log Entries | count (diagnostic) | `error_log_entries` |
| Critical Warning | bitmask (diagnostic) | `critical_warning` |
| Data Written | TB, decimal (diagnostic) | `data_written_bytes` |
| Power-On Hours | h (diagnostic) | `power_on_hours` |
| Unsafe Shutdowns | count (diagnostic) | `unsafe_shutdowns` |

The Storage device holds two aggregate entities:

| Entity | Type | Meaning |
|---|---|---|
| Status | sensor (diagnostic) | `ok`, `warning`, `critical`, `error`, `no_devices` or `disabled` |
| Health | binary sensor (`problem`) | on when any SMART alert or NVMe device is warning/critical |

`Storage Status` attributes include `last_error`, `last_update`, `device_count`
and per-device issues, which is the place to look when per-device sensors are
missing (for example `nvme`/`smartctl` not installed or no access to
`/dev/nvme*`). The Storage Health `issues` attribute lists the source, device,
severity and message.

## Services

| Service | Fields | Action |
|---|---|---|
| `framework_powerd.set_mode` | `mode`: `performance`/`powersave` | Calls `/mode` |
| `framework_powerd.reset_idle` | — | Calls `/activity` |
| `framework_powerd.refresh` | — | Polls the daemon immediately |

These can be used in automations, scripts and dashboards. The select and button
entities call the same coordinator methods.

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

## Dynamic entities

Per-device SMART sensors appear when the first alert for that device is
received, even if the device was not present at setup time. GPU, NPU, fan, LLM
and per-drive NVMe entities are created on the first update that contains the
corresponding block, without reloading the integration.

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
  - sensor.framework_power_daemon_estimated_total_power
  - sensor.framework_power_daemon_energy_24h
  - sensor.framework_power_daemon_ram_used
  - sensor.framework_power_daemon_ram_usage
  - sensor.framework_power_daemon_cpu_temperature
  - sensor.framework_power_daemon_cpu_usage
  - sensor.gpu_temperature
  - sensor.gpu_power
  - sensor.gpu_vram_usage
  - sensor.storage_status
  - sensor.nvme_nvme0_health
  - sensor.nvme_nvme0_temperature
  - sensor.nvme_nvme0_percentage_used
  - sensor.llm_requests_today
  - sensor.npu_power
  - sensor.fan_speed
  - select.npu_power_mode
  - select.fan_mode
  - select.framework_power_daemon_power_tunable_mode
  - button.framework_power_daemon_reset_idle_timer
  - binary_sensor.storage_health
  - binary_sensor.npu_present
```

Entity IDs are generated from the device name and the entity name. Existing
installations keep the IDs they already have, so the example matches a fresh
install; check Developer Tools → States after setup. Renaming a device changes
the IDs of its entities unless you disable that behaviour during the rename.

## Troubleshooting

- `cannot_connect`: check host/port, that the daemon listens on an address
  reachable from HA, and that the token matches `server.jwt_secret`.
- Entities become unavailable: the coordinator reports a failed `/status` call.
  Check the daemon logs and `/healthz` on the daemon host.
- RAM/GPU sensors missing: update the daemon first. The integration creates them
  when the fields appear in `/status`.
- NPU telemetry entities missing: the daemon only creates them when
  `npu.enabled` is true and an `amdxdna` device was found. The presence sensor
  and power-mode select are registered unconditionally. Check `monitors.npu` in
  `/healthz`.
- Fan entities missing: they require `fan.enabled` and a working
  `framework_tool`. Check `monitors.fan` in `/healthz`.

See [Troubleshooting](troubleshooting.md) for daemon-side issues.
