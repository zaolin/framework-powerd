# AMD XDNA NPU Monitoring

The daemon can report telemetry from the AMD XDNA NPU (Ryzen AI, Strix Point and
Strix Halo) that is driven by the in-kernel `amdxdna` accelerator driver.

## Where the numbers come from

The driver exposes only static identity over sysfs. Power, utilization, clocks
and contexts are read through the DRM accelerator ioctl
`DRM_IOCTL_AMDXDNA_GET_INFO` on `/dev/accel/accelN`:

| Query | Data |
|---|---|
| `QUERY_SENSORS` | NPU power in mW, plus one utilization percentage per AIE column |
| `QUERY_CLOCK_METADATA` | MP-NPU clock and H clock in MHz |
| `QUERY_RESOURCE_INFO` | maximum clock, TOPs and task limits, current TOPs and tasks |
| `QUERY_HW_CONTEXTS` | hardware contexts with owning PID and submission counters |
| `QUERY_FIRMWARE_VERSION` | firmware major.minor.patch.build |
| `GET_POWER_MODE` | configured power mode |

The static identity comes from sysfs: `/sys/class/accel/accel*/device/`
provides `vbnv`, `fw_version`, `device_type`, `revision` and `power_state`.

No external tool is required. `xrt-smi` can report similar data, but the daemon
talks to the kernel directly.

## Setup

```json
"npu": {
  "enabled": true,
  "poll_interval": "15s",
  "device": ""
}
```

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable NPU monitoring. |
| `poll_interval` | duration | `15s` | How often to query the device, minimum 1s. |
| `device` | string | auto | Accel device node, for example `/dev/accel/accel0`. Empty means auto-detect. |

Auto-detection scans `/sys/class/accel` for a device driven by `amdxdna` (PCI
vendor `0x1022`, device `0x17f0`).

Every query resumes the NPU for the duration of the ioctl, so keep the interval
modest. Fifteen seconds is a good default; five seconds is fine while you are
watching a workload.

The NPU is optional. If no device is found the daemon still starts and reports
the monitor as `absent`.

### Power mode control

`POST /npu/power-mode` changes the NPU power mode:

```bash
curl -X POST http://localhost:8080/npu/power-mode \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"mode":"high"}'
```

Accepted modes are `default`, `low`, `medium`, `high` and `turbo`. The kernel
restricts this ioctl to root, so the daemon must run as root (the shipped unit
does). Without privileges the endpoint returns a 400 with an explanatory
message.

## API

`GET /npu/stats` returns the latest snapshot:

```json
{
  "present": true,
  "device": "/dev/accel/accel0",
  "pci_address": "0000:c2:00.1",
  "vbnv": "RyzenAI-npu6",
  "device_type": "kmq",
  "firmware_version": "1.0.0.63",
  "revision": "0x20",
  "power_state": "D3hot",
  "power_mode": "default",
  "power_watts": 0.002,
  "column_utilization": [0, 0, 0, 0, 0, 0, 0, 0],
  "column_utilization_avg": 0,
  "column_utilization_max": 0,
  "mp_npu_clock_mhz": 76,
  "h_clock_mhz": 120,
  "tops_current": 3,
  "tops_max": 58,
  "tasks_current": 0,
  "tasks_max": 16,
  "last_update": "2026-11-01T20:23:37Z"
}
```

The same block is part of `GET /status`. `/healthz` reports `monitors.npu` as
`disabled`, `absent`, `waiting`, `ok` or `error`.

When contexts exist they are included:

```json
"contexts": [
  {
    "context_id": 1,
    "start_column": 0,
    "num_columns": 4,
    "pid": 4242,
    "command_submissions": 10,
    "command_completions": 10,
    "migrations": 0,
    "preemptions": 0,
    "errors": 0
  }
]
```

## Home Assistant entities

The telemetry sensors are only created when an NPU is present. The presence
binary sensor and the power-mode select are always registered so they can show
the device arriving or leaving.

| Entity | Unit | Source |
|---|---|---|
| NPU Present | binary sensor | `npu.present` |
| NPU Power | W | `npu.power_watts` |
| NPU Column Utilization | % | `npu.column_utilization_avg` |
| NPU MP-NPU Clock | MHz | `npu.mp_npu_clock_mhz` |
| NPU H Clock | MHz | `npu.h_clock_mhz` |
| NPU TOPs | count | `npu.tops_current` |
| NPU Active Contexts | count | number of `npu.contexts` |
| NPU Firmware | text | `npu.firmware_version` |
| NPU Power State | text | `npu.power_state` |
| NPU Power Mode | select | `npu.power_mode` |

The column sensor carries the per-column values and the maximum as attributes.
The H clock sensor carries the maximum clock as `max_mhz`. The context sensor
carries the full context table, including the owning PID.

`NPU Present` and `NPU Power Mode` are always created so they can report the
device appearing later. The binary sensor stays available and reports off while
no NPU is detected; the select is unavailable until a device appears. The other
NPU sensors are only created when a device is present.

## What the numbers mean

- `power_watts` is the NPU's own draw from the platform telemetry, not the
  system total. An idle, suspended NPU reports a value close to zero.
- Column utilization is per AIE column, reported for up to eight columns.
  The sensor state is the mean across the reported columns.
- Clocks drop to the lowest DPM level when the NPU is idle, so low MHz values
  are normal outside of a workload.
- `power_state` is `D3hot` while suspended and `D0` while active.
- TOPs and task counters come from the driver's resource accounting.

There is no generic "NPU load" percentage in sysfs. The per-column utilization
is the closest supported signal.

## Troubleshooting

- **`monitors.npu` is `absent`**: no `amdxdna` device was found. Check
  `lspci -nn | grep 17f0` and `ls /sys/class/accel`.
- **`monitors.npu` is `error`**: the device node could not be opened or the
  query failed. Check `ls -l /dev/accel/`. The daemon runs as root, so
  permissions should not be the cause.
- **Power and utilization stay at zero**: nothing is running on the NPU. Large
  language models served through ROCm use the iGPU, not the NPU.
- **Setting the power mode returns 400**: the daemon is not running as root, or
  the kernel does not support the ioctl on this device.
