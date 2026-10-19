# Architecture

`framework-powerd` is a single Go binary with a small Home Assistant companion
integration. The daemon owns the power state machine; the integration is a
read-mostly API client.

## Components

| Package | Responsibility |
|---|---|
| `internal/monitor` | Raw input watching, joystick deadzones, hotplug via fsnotify, idle timer. |
| `internal/detector` | Steam game detection and Steam Remote Play virtual devices. |
| `internal/power` | Power profile pinning, tunables, process pauser, turbostat, energy aggregation, metrics persistence. |
| `internal/gpu` | AMD GPU sysfs metrics (VRAM/GTT/temperature/power) and CPU usage. |
| `internal/smartd` | smartd journald alerts. |
| `internal/peripherals` | USB/NVMe/fan/Wi-Fi/Ethernet power estimation. |
| `internal/api` | HTTP control plane, JWT, rate limiting, `/healthz`. |
| `internal/config` | JSON configuration and validation. |
| `internal/persist` | Atomic JSON file writes. |
| `internal/sysinfo` | Static system info and dynamic `/proc/meminfo` readings. |
| `cmd/framework-powerd` | CLI, startup order, signal handling, systemd notify. |

## Runtime flow

```mermaid
graph TD
    subgraph Sources [Sources]
        Input[Raw input + hotplug]
        Steam[Steam process check]
        Remote[Remote Play virtual devices]
        Turbo[turbostat]
        GPU[GPU sysfs]
        SMART[smartd journal]
        API[HTTP API]
    end

    Main{Reconciler}

    Input --> Main
    Steam --> Main
    Remote --> Main
    API --> Main
    Turbo -->|power + energy| Main
    GPU -->|VRAM/GTT/CPU| Status[/status and /healthz/]
    SMART -->|alerts| Status
    Main -->|powerprofilesctl| PPD[power-profiles-daemon]
    Main -->|ASPM/powertop/USB/audio| Sysfs[/sys]
    Main -->|SIGSTOP/SIGCONT| Pauser[Game process tree]
    Main -->|snapshot| Persist[(metrics.json)]
```

The `Reconciler` is `updatePowerState` in `cmd/framework-powerd/main.go`. All
callbacks take a mutex, so idle, remote-play, Steam and API events cannot
interleave mode changes or pause/resume operations.

## State machine

Priority:

1. Remote Play **and** a game running → never idle, apply active tunables.
2. Idle → apply idle tunables, pause the game process tree.
3. Active with game or Remote Play → performance tunables, resume the game.
4. Active desktop → default active tunables, resume the game.

The APU profile trio (`powerprofilesctl` balanced, EPP `balance_performance`,
governor `powersave`) is pinned and re-asserted; it does not flip with idle.
Only the secondary tunables (PCIe ASPM, powertop, USB/audio latency) change.

## Monitor lifecycle

- **Idle monitor** watches `/dev/input/event*` and `js*`, plus fsnotify hotplug.
  It fires edge callbacks only when the idle flag changes.
- **Steam detector** polls every 5 seconds. On startup it detects an existing
  game so a daemon restart does not leave a paused game behind.
- **turbostat supervisor** restarts the process with exponential backoff
  (1 s → 30 s). A missing binary disables CPU power monitoring permanently. A
  restart resets the energy clock so the outage is not charged to an hour.
- **GPU monitor** polls sysfs every `gpu.poll_interval` (default 5 s) and derives
  VRAM/GTT free bytes and usage percentages.
- **SMART monitor** follows `smartd.service` in the journal and expires alerts
  after `smartd.alert_retention`.

## Energy aggregation

Raw turbostat samples are integrated over the real elapsed time (capped at
60 seconds per sample). The monitor keeps a 168-slot ring (7 days) plus a
monotonic lifetime counter.

The ring, index, last-hour timestamp and lifetime counter are written to
`metrics.json`:

- on every hour boundary,
- every `metrics.save_interval` (default 5 minutes),
- on graceful shutdown.

Writes are atomic: temp file → `fsync` → `rename` → directory `fsync`. On load
the ring is rotated forward by the whole hours the daemon was down, and missed
slots are zeroed. State older than seven days is discarded.

## Startup and shutdown

Startup order:

1. Load and validate config, apply explicit CLI overrides.
2. Pin the balanced profile and validate external tools.
3. Start idle, Remote Play, Steam and power monitors.
4. Restore persisted metrics, then start turbostat and the persistence loop.
5. Start optional SMART/GPU monitors.
6. Bind the HTTP listener, send systemd `READY=1`, start watchdog pings.

Shutdown order:

1. Receive `SIGTERM`/`SIGINT`, send systemd `STOPPING=1`.
2. Stop the power monitor and flush the final metrics snapshot.
3. Close the HTTP server with a 5-second grace period.
4. Resume any paused game process tree before exiting.

## Security boundary

The API is a control plane: it can switch power tunables and pause/resume
processes. Authentication is fail-closed unless `--disable-auth` is passed. The
systemd unit binds to localhost by default and runs with a hardened profile; see
[Security](security.md).
