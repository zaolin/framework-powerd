![Framework Power Daemon Logo](docs/logo.png)

A Go daemon to automatically manage power profiles on the **Framework Desktop**, specifically tailored for hybrid setups acting as both a **Steam Gaming Console** and an **AI Server**.

> [!IMPORTANT]
> This daemon is designed for [**Framework Desktop**](https://frame.work/desktop) usage and has been explicitly tested on [**CachyOS**](https://cachyos.org). It keeps the APU in a **balanced** profile with low wake-up latency for the 24/7 AI server, applies performance tunables when gaming (Active or Remote Play), and drastically reduces power consumption during idle operation.

## Features

- **Automated Power Management**: Switches to "Performance" during active usage (Gaming/Input) and "Powersave" during inactivity.
- **Idle Detection**: Monitors raw input activity (default 5-minute timeout). Support for **Joystick Deadzones** and **Event Deduplication** to prevent drift.
- **Game Pausing**: Recursively pauses the entire process tree of a Steam game (including Proton/Wine wrappers) when idle. **Syncs state** on startup to prevent conflicts.
- **Steam Remote Play Detection**: Detects virtual input devices from Remote Play (specifically `js` interfaces) to keep the system active.
- **GPU Monitoring**: Tracks AMD GPU metrics via sysfs (VRAM, GTT, temperature, power) and system CPU usage via `/proc/stat`.
- **SMART Health Monitoring**: Monitors SMART health alerts via smartd journald logs for hard drive failure detection.
- **Persistent Energy Metrics**: 24h, 7d and lifetime energy aggregates survive daemon restarts (atomic JSON state file).
- **Health Endpoint**: Unauthenticated `/healthz` with monitor and persistence status for systemd/monitoring probes.
- **REST API**: Allows manual mode overriding, idle resetting, and usage statistics.
- **JWT Authentication**: Secure API access with JSON Web Tokens.
- **Configuration File**: JSON-based daemon configuration.
- **Systemd Integration**: Runs effectively as a background service.

## Documentation

- [Configuration](docs/configuration.md) — every key, defaults and precedence
- [REST API](docs/api.md) — endpoints, response schemas and examples
- [Home Assistant entities](docs/home_assistant.md) — setup, entity table, dashboard example
- [Architecture](docs/architecture.md) — components, state machine, persistence
- [Troubleshooting](docs/troubleshooting.md) — tools, permissions, watchdog, HA
- [Security](docs/security.md) — JWT, network exposure, systemd hardening
- [Migration to 1.3](docs/migration-1.3.md) — Ollama removal and entity changes
- [Idle detection](docs/idle_logic.md) — detailed idle-state logic
- [Changelog](CHANGELOG.md)

## Architecture & Flow

The daemon operates by listening to multiple sources of input: Kernel Udev events (HDMI), Input Devices (Activity), Steam Processes, and Virtual Devices (Remote Play).

```mermaid
graph TD
    subgraph Sources [Input Sources]
        Input[Raw Input Monitor]
        Steam[Steam Process Check]
        Remote[Virtual Joystick Check]
        Turbo[Turbostat Power Monitor]
        GPU[GPU Sysfs Monitor]
        SMART[SMARTd Journal Monitor]
        API[User API]
    end

    subgraph Logic [State Machine]
        Main{Power Daemon}
    end

    subgraph Actions [Power Management]
        PCTL[powerprofilesctl]
        Pauser[Recursive Process Pauser]
    end
    
    Input -->|Activity| Main
    Steam -->|Game PID| Main
    Remote -->|Virtual Device| Main
    Turbo -->|Power/Energy Stats| Main
    GPU -->|GPU Metrics| Main
    SMART -->|Health Alerts| Main
    API -->|Override/Activity| Main

    Main -->|Set Profile| PCTL
    Main -->|SIGSTOP/SIGCONT| Pauser
    Input -.->|fsnotify| Hotplug[Device Hotplug]
    Hotplug --> Input
    Turbo -.->|persist| State[Metrics State File]
    GPU -.->|sysfs| Sysfs[/sys/class/drm]
    SMART -.->|journald| Journal[systemd Journal]
```

## Power Monitoring

The daemon uses `turbostat` to provide accurate, real-time power consumption metrics.

*   **Source**: `turbostat` (running via `sudo`, reading MSRs).
*   **Metrics**:
    *   **Package**: Total package power.
    *   **Core**: CPU Core power.
    *   **RAM**: Memory power.
*   **Total Energy**: Calculated as `PkgWatt` + `RAMWatt`. `CorWatt` is a subset of `PkgWatt` (cores only) and is deliberately not added, to avoid double-counting.
*   **History**: Tracks 24-hour and 7-day rolling energy consumption in **kWh**, plus a monotonic **lifetime** counter. The aggregates are persisted to `/var/lib/framework-powerd/metrics.json` (atomic writes) and survive daemon restarts; a gap during downtime is not attributed to any hour.
*   **Integration**: Energy is integrated over the real elapsed time between turbostat samples, capped at 60 seconds so a suspend/resume gap cannot inject an implausible jump. If turbostat exits, it is restarted with exponential backoff.

## Metrics Persistence

The 24h/7d ring buffer and the lifetime energy counter are written atomically to
`/var/lib/framework-powerd/metrics.json` every `metrics.save_interval`, on every
hour boundary, and on graceful shutdown. On startup the daemon rotates the ring
forward by the hours it was down and zeroes the missed slots, so downtime is
never charged to a request. A state file older than seven days is discarded.

Writes use a temp file, `fsync`, `rename` and a directory `fsync`; a crash or
power loss cannot leave a truncated JSON file behind. Corrupt or incompatible
state files are logged and ignored, and the daemon starts with a fresh buffer.
The systemd unit sets `StateDirectory=framework-powerd` to create the directory.

## Network Device Stability

Network devices (Wi-Fi and Ethernet) are explicitly excluded from Runtime Power Management (forced to `on`) to prevent connection drops and instability. This is particularly important for chipsets like the **MediaTek MT7925 (Wi-Fi 7)** which are known to be unstable with `powertop --auto-tune` or aggressive ASPM. The status of these devices can be verified via the `/status` API endpoint.

## Power State Flow

The daemon pins the APU profile trio — `powerprofilesctl` **balanced**, CPU EPP **balance_performance**, scaling governor **powersave** — once at startup and re-asserts it on every transition. It never flips these per mode: the balanced profile keeps inference wake-up latency low while the 24/7 llama server is running, and the hardware scales dynamically. The scx scheduler (`scx_lavd`) is likewise pinned to **gaming** mode.

What *does* switch with idle detection is the secondary tuning layer:

| Tunable | Active Usage | Idle |
|---|---|---|
| PCIe ASPM | `performance` | `powersupersave` |
| powertop auto-tune | off | on |
| USB autosuspend / audio power-save | disabled | may re-enable* |
| Game processes | running | recursively paused |

\* USB/audio latency is disabled (reverted) on active transitions; during idle, `powertop --auto-tune` may re-enable autosuspend/power-save. Network devices are always excluded for stability.

**Active Gaming** (either Local Input or Remote Play + Running Game) takes priority.

```mermaid
flowchart TD
    Start([State Change Event]) --> CheckActive{Is Game Running?}
    
    CheckActive -- Yes --> CheckRemote{Is Remote Play?}
    CheckRemote -- Yes --> ForcePerf["Active Tunables (Ignore Idle)"]
    CheckRemote -- No --> CheckInput{Input Detected?}
    
    CheckActive -- No --> CheckInput
    
    CheckInput -- Yes --> ForcePerf
    CheckInput -- No --> CheckIdle{Is System Idle?}
    
    CheckIdle -- Yes --> Suspend["Pause Game Tree & Apply Idle Tunables"]
    CheckIdle -- No --> Resume["Resume Game Tree & Apply Active Tunables"]
    
    Resume --> ForcePerf
```

## How it works

1.  **Monitoring**:
    *   **Remote Play**: Polls `/proc/bus/input/devices` to detect virtual joysticks.
    *   **Input**: Monitors `/dev/input/js*` (with deadzone) and valid `/dev/input/event*` devices. Ignores noise and init bursts.
    *   **Steam**: Periodically checks for running Steam games and identifies the "Reaper" root process.

> [!NOTE]
> For a detailed explanation of the idle detection mechanism, including diagrams, see [Idle Detection Logic](docs/idle_logic.md).


2.  **Decision Making**:
    *   **Priority 1: Active Usage**.
        *   **Local Input**: Moving mouse/keyboard/controller -> **Active Tunables** (ASPM performance, USB/audio latency off).
        *   **Remote Play**: IF Remote Play is active **AND** a Game is running -> **Active Tunables** (Ignores Idle).
    *   **Priority 2: Idle (No Active Usage)**.
        *   **Action**: Apply **Idle Tunables** (ASPM powersupersave, powertop auto-tune). If a game is running, **Recursively Pause It** (targeting `wineserver` or game process, avoiding Steam `reaper`).
3.  **Action**:
    *   Pins the APU profile trio to balanced, applies per-state tunables, and manages Process States (Running/Stopped).

## Prerequisites

The daemon relies on the following tools:

- `powerprofilesctl`: For pinning the balanced system power profile.
- `turbostat`: For accurate power monitoring (usually part of `linux-tools` or `linux-cpupower`).
- `libsystemd-dev`: Required for building (SMART journald integration uses cgo).
- `powertop` (Optional): For auto-tuning power parameters.
- `scxctl` (Optional): For sched-ext scheduler management (pinned to gaming mode).

### GPU Monitoring

For AMD GPU metrics, ensure your GPU is accessible via sysfs:
- `/sys/class/drm/card*/device/` must contain `mem_info_vram_*`, `mem_info_gtt_*`
- For temperature/power: `hwmon` interface must be present

### SMART Health Monitoring

Configure smartd to log to journald. Add to `/etc/smartd.conf`:
```
/dev/sda -a -H -l error -l selftest -m root
```
Or for NVMe:
```
/dev/nvme0n1 -a -W 4,45,50 -m root
```

## Installation

### Makefile (recommended)

```bash
git clone https://github.com/zaolin/framework-powerd.git
cd framework-powerd
make install    # build, binary, config, JWT secret and systemd unit
make enable     # systemctl enable --now framework-powerd
make token      # print a JWT token for Home Assistant
```

`make install`:

- builds with the version injected from `git describe`,
- installs the binary to `/usr/local/bin/framework-powerd`,
- installs `configs/config.example.json` to `/etc/framework-powerd/config.json` **only if no config exists**,
- creates `/etc/framework-powerd/env` with a random `JWT_SECRET` (mode `600`) when neither that file nor `server.jwt_secret` exists,
- installs the unit to `/etc/systemd/system/framework-powerd.service` and runs `systemctl daemon-reload`.

Additional targets:

| Target | Action |
|---|---|
| `make enable` | Enable and start the service, then show its status. |
| `make disable` | Stop and disable the service. |
| `make uninstall` | Remove unit and binary; keep config and JWT secret. |
| `make token` | Print a token signed with the installed JWT secret. |

The targets use `sudo` internally. Run `make install SUDO=` as root, or override
paths with `PREFIX`, `BINDIR`, `CONFIG_DIR` and `SYSTEMD_DIR`. If you change
`CONFIG_DIR`, update `EnvironmentFile` in the unit accordingly.

### Manual installation

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/zaolin/framework-powerd.git
    cd framework-powerd
    ```

2.  **Build**:
    ```bash
    go build ./cmd/framework-powerd
    ```

3.  **Install Binary**:
    ```bash
    sudo cp framework-powerd /usr/local/bin/
    ```

4.  **Install Service**:
    ```bash
    sudo cp configs/systemd/framework-powerd.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now framework-powerd
    ```

The unit uses `Type=notify` with a 30-second watchdog: the daemon sends `READY=1`
after binding the API and pings the watchdog while running. `StateDirectory`
creates `/var/lib/framework-powerd` for the persisted metrics. The optional
`EnvironmentFile=-/etc/framework-powerd/env` supplies `JWT_SECRET` on systems
installed via `make install`.

## Usage

### CLI Flags

You can customize the daemon's behavior with flags:

```bash
# Debug mode (verbose logs)
framework-powerd serve --debug

# Custom Idle Timeout (overrides server.idle_timeout; default 5m)
framework-powerd serve --idle-timeout 30s

# Print the build version
framework-powerd --version
```

### Home Assistant Integration

This project is compatible with [HACS](https://hacs.xyz/) (Home Assistant Community Store).

**Installation via HACS**:
1.  Open **HACS** in Home Assistant.
2.  Click the menu (three dots) in the top right corner and select **Custom repositories**.
3.  Add the repository URL: `https://github.com/zaolin/framework-powerd`.
4.  Select **Integration** as the Category and click **Add**.
5.  Find **Framework Power Daemon** in the list and click **Download**.
6.  Restart Home Assistant.
7.  Go to **Settings > Devices & Services > Add Integration**.
8.  Search for **Framework Power Daemon** and configure it.
    *   **Host**: IP address of the daemon (e.g. `192.168.1.x` or `localhost` if on same machine).
    *   **Port**: `8080` (default).
    *   **JWT Secret**: If you started the daemon with a secret.

**Sensors Provided** (full table in [Home Assistant entities](docs/home_assistant.md)):
*   **Power Mode**: `balanced` (the APU profile trio is pinned; the mode endpoint still accepts `performance`/`powersave` to switch the secondary tunables)
*   **System Idle**: `True` / `False`
*   **Game Status**: PID and Running/Paused state
*   **Power Usage**: Estimated total and peripheral power (Watts)
*   **Energy Consumption**: Last 24h, 7 Days and lifetime total (kWh; persisted across daemon restarts)
*   **RAM**: Used, Free, Available (GiB) and Usage (%)
*   **Uptime**: System uptime (duration)
*   **Polling Interval**: Configurable number entity (seconds)
*   **GPU Metrics** (if enabled):
    *   GPU Temperature (°C)
    *   GPU Power (Watts)
    *   GPU VRAM Used/Total/Free (GiB) and Usage (%)
    *   GPU GTT Used/Total/Free (MiB) and Usage (%)
    *   CPU Usage (%)
*   **SMART Health** (if enabled):
    *   SMART Alert (Binary Sensor)
    *   SMART Alerts Count
*   **System Information** (diagnostic): Kernel, OS, CPU model, total RAM, daemon version

### API Control

Trigger modes manually using the REST API (default port 8080). Both `performance` and `powersave` re-assert the balanced APU profile; they switch only the secondary tunables (ASPM, powertop, USB/audio latency). The full reference is in [REST API](docs/api.md).

If JWT authentication is enabled, you must export your token first:
```bash
export TOKEN="your_jwt_token_here"
```

- **Apply Active Tunables** (performance):
  ```bash
  curl -H "Authorization: Bearer $TOKEN" -X POST -d '{"mode":"performance"}' http://localhost:8080/mode
  ```

- **Apply Idle Tunables** (powersave):
  ```bash
  curl -H "Authorization: Bearer $TOKEN" -X POST -d '{"mode":"powersave"}' http://localhost:8080/mode
  ```

- **Trigger Activity (Reset Idle Timer)**:
  ```bash
  curl -H "Authorization: Bearer $TOKEN" -X POST http://localhost:8080/activity
  ```

- **Get Status**:
  ```bash
  curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/status
  ```

- **Get SMART Stats** (if enabled):
  ```bash
  curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/smartd/stats
  # Output: {"alerts":[{"device":"/dev/sda","fail_type":"SelfTest",...}]}
  ```

- **Health Check** (no authentication required):
  ```bash
  curl http://localhost:8080/healthz
  # Output includes status, version, uptime, power sample age, monitor
  # booleans and the metrics persistence state.
  ```

### Authentication

To enable JWT authentication, start the daemon with the `--jwt-secret` flag:

```bash
/usr/local/bin/framework-powerd serve --jwt-secret="mysecret"
```

To generate a token:

```bash
/usr/local/bin/framework-powerd token --secret="mysecret"
```

Use the token in your requests:

```bash
export TOKEN=$(/usr/local/bin/framework-powerd token --secret="mysecret")
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/status
```

## Configuration

The daemon supports a JSON configuration file. The full reference is in [Configuration](docs/configuration.md). Create `/etc/framework-powerd/config.json`:

```json
{
  "server": {
    "address": "0.0.0.0",
    "port": 8080,
    "idle_timeout": "5m"
  },
  "smartd": {
    "enabled": true,
    "service_unit": "smartd.service",
    "notify_service": "notify.mobile_phone",
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
  }
}
```

### Configuration Options

| Section | Option | Type | Default | Description |
|---------|--------|------|---------|-------------|
| `server.address` | Address | string | `localhost` | IP to bind to |
| `server.port` | Port | int | `8080` | HTTP port |
| `server.idle_timeout` | Idle Timeout | duration | `5m` | Time before idle |
| `smartd.enabled` | Enabled | bool | `false` | Enable SMART monitoring |
| `smartd.notify_service` | Notify Service | string | `""` | HA notify service |
| `smartd.alert_retention` | Alert Retention | duration | `30s` | How long to keep alerts |
| `gpu.enabled` | Enabled | bool | `false` | Enable GPU monitoring |
| `gpu.poll_interval` | Poll Interval | duration | `5s` | GPU polling frequency |
| `metrics.persist` | Persist | bool | `true` | Persist energy aggregates across restarts |
| `metrics.state_file` | State File | string | `/var/lib/framework-powerd/metrics.json` | Atomic JSON state path |
| `metrics.save_interval` | Save Interval | duration | `5m` | Periodic flush interval |

Run with config:
```bash
/usr/local/bin/framework-powerd serve --config=/etc/framework-powerd/config.json
```

CLI flags override config file values:
- `--address`: The IP address to listen on (default: `localhost`). Use `0.0.0.0` to listen on all interfaces.
- `--port`: The port to listen on (default: `8080`).
- `--idle-timeout`: Overrides `server.idle_timeout`.

> **Note**: If you change the port or address, remember to update your API calls accordingly.

