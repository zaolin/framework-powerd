# EC Fan Control

The daemon can drive the Framework Desktop's embedded-controller fan instead of
letting the EC stop and ramp it on its own. The goal is a steady, quiet baseline
with a temperature-driven ramp, rather than the EC's on/off behaviour.

## Hardware interface

The `cros_ec` hwmon device exposes `fan1_input` and `fan1_target` read-only, so
the fan cannot be controlled through hwmon. Writes go through `framework_tool`,
which talks to the EC directly:

| Command | Effect |
|---|---|
| `framework_tool --fansetduty <0-100>` | set the fan duty in percent |
| `framework_tool --fansetrpm <rpm>` | closed-loop RPM target, clamped by the EC fan table |
| `framework_tool --autofanctrl` | return control to the EC curve |
| `framework_tool --thermal` | read the EC sensors and fan speeds |

The daemon uses **duty**, not RPM. An aftermarket fan's tach scaling can differ
from the stock fan's pole count, so a closed RPM loop may target a wrong value.
Duty maps directly to the fan regardless.

On the test hardware, duty maps roughly linearly to speed: 20 % = 371 RPM,
40 % = 896 RPM, 50 % = 1148 RPM, 100 % = 2340 RPM.

`framework_tool` needs root, and `/dev/cros_ec` is root-only. The shipped unit
already runs as root.

## Configuration

```json
"fan": {
  "enabled": true,
  "mode": "curve",
  "min_duty_percent": 50,
  "start_temp_c": 75,
  "full_temp_c": 90,
  "critical_temp_c": 90,
  "board_critical_temp_c": 85,
  "hysteresis_c": 3,
  "fall_step_percent": 3,
  "poll_interval": "5s"
}
```

`poll_interval` defaults to `5s` with a minimum of 1 s. Every key, default and
validation rule is in [Configuration](configuration.md#fan).

## Behaviour

- Below `start_temp_c` the fan runs at `min_duty_percent`, constantly. It never
  stops.
- Between `start_temp_c` and `full_temp_c` the duty rises linearly.
- Rises are immediate. Falls wait at least 90 seconds after the last increase,
  require the temperature to drop a full hysteresis band below the current
  duty, and move by at most `fall_step_percent` per update. That avoids audible
  hunting.
- A sensor at or above `critical_temp_c`, or a mainboard sensor at or above
  `board_critical_temp_c`, forces 100 % regardless of the curve.
- The duty is re-asserted every 60 seconds even when unchanged, so an EC reset
  cannot leave a stale value behind.

Sensors used: `k10temp` (CPU), `amdgpu` (GPU), the `cros_ec` sensors split into
CPU and mainboard by label, and the NVMe `hwmon` node. The curve follows the
hottest of them.

## Safety

A manual duty survives a dead daemon, so the fan is returned to the EC on every
exit path:

- the daemon calls `--autofanctrl` on SIGTERM/SIGINT
- the systemd unit runs it as `ExecStopPost`, which also fires on a crash, a
  watchdog kill and a start-limit failure
- if the sensors cannot be read twice in a row, the daemon calls
  `--autofanctrl` and stays in `auto` until it recovers

The hooks run on every unit stop, including when `fan.enabled` is false. If you
use framework-powerd only for power monitoring and drive the fan some other way,
remove the two `ExecStopPost` lines from the unit.

If the process is killed with `SIGKILL` outside systemd, run
`framework_tool --autofanctrl` manually.

The EC does **not** override a manual duty, even above its own `fan_max`
thresholds. That was verified on the test hardware: at 20 % duty the hottest
sensor reached 100 °C and the fan stayed at 371 RPM. The software ramp and the
systemd hook are therefore the real protection.

## API

- `GET /fan/stats` returns the current snapshot
- `POST /fan/mode` with `{"mode":"auto"}` or `{"mode":"curve"}`
- the `fan` block is part of `GET /status`
- `/healthz` reports `monitors.fan` as `disabled`, `unsupported`, `waiting`,
  `auto`, `curve` or `error`

## Home Assistant entities

The entities live on the **Fan** sub-device under the daemon device. Entity
names on that device:

| Entity | Unit | Source |
|---|---|---|
| Speed | RPM (diagnostic) | `fan.sensors.fan_rpm` |
| Duty | % (diagnostic) | `fan.duty_percent` |
| Mainboard Temperature | °C | `fan.sensors.board_celsius` |
| CPU Board Temperature | °C | `fan.sensors.cpu_ec_celsius` |
| Mode | select | `fan.mode` (`auto`, `curve`) |

The `fan` block also carries the CPU, GPU and NVMe temperatures. The CPU die
temperature has its own **CPU Temperature** sensor on the daemon device; see
[Home Assistant entities](home_assistant.md).

## Calibration notes

Measured on a Framework Desktop with llama.cpp running four parallel requests:

| Duty | RPM | equilibrium |
|---|---|---|
| 20 % | 371 | ~100 °C |
| 40 % | 896 | 82 °C |
| 50 % | 1148 | ~78 °C (interpolated) |

20 % is fine at idle and far too little under load, which is why the floor is
50 %: it keeps the mainboard and the drive in a comfortable range while staying
audible only up close.

## Troubleshooting

- **`monitors.fan` is `unsupported`**: `framework_tool` is not installed or not
  in the unit's `PATH`.
- **The fan does not change**: the EC rejected the write, or another process is
  holding manual control. Check `framework_tool --thermal` and run
  `framework_tool --autofanctrl` to reset.
- **The fan stays high after stopping the service**: an `ExecStopPost` should
  have returned it to auto; run `framework_tool --autofanctrl` manually.
- **Temperatures are missing**: the daemon found no hwmon source it could read
  and stayed in `auto`.
