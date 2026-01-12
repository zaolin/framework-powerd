# Framework Power Daemon

A Go daemon to automatically manage power profiles on the **Framework Desktop**, specifically tailored for hybrid setups acting as both a **Steam Gaming Console** and an **AI Server**.

> [!IMPORTANT]
> This daemon is designed for [**Framework Desktop**](https://frame.work/desktop) usage and has been explicitly tested on [**CachyOS**](https://cachyos.org). It ensures the system runs at peak performance when gaming (Active or Remote Play) and drastically reduces power consumption during idle AI server operation (24/7).
>
> **Tip for AI/Ollama Users**: When running local LLMs, using `ollama-vulkan` has been observed to save **~20W/h** compared to `ollama-rocm` on this hardware.

## Features

- **Automated Power Management**: Switches to "Performance" during active usage (Gaming/Input) and "Powersave" during inactivity.
- **Idle Detection**: Monitors raw input activity (default 5-minute timeout). Support for **Joystick Deadzones** and **Event Deduplication** to prevent drift.
- **Game Pausing**: Recursively pauses the entire process tree of a Steam game (including Proton/Wine wrappers) when idle. **Syncs state** on startup to prevent conflicts.
- **Steam Remote Play Detection**: Detects virtual input devices from Remote Play (specifically `js` interfaces) to keep the system active.
- **REST API**: Allows manual mode overriding and idle resetting.
- **JWT Authentication**: Secure API access with JSON Web Tokens.
- **Systemd Integration**: Runs effectively as a background service.

## Architecture & Flow

The daemon operates by listening to multiple sources of input: Kernel Udev events (HDMI), Input Devices (Activity), Steam Processes, and Virtual Devices (Remote Play).

    subgraph Sources [Input Sources]
        Input[Raw Input Monitor]
        Steam[Steam Process Check]
        Remote[Virtual Joystick Check]
        Turbo[Turbostat Power Monitor]
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
    API -->|Override/Activity| Main

    Main -->|Set Profile| PCTL
    Main -->|SIGSTOP/SIGCONT| Pauser
    Input -.->|fsnotify| Hotplug[Device Hotplug]
    Hotplug --> Input
```

## Power Monitoring

The daemon uses `turbostat` to provide accurate, real-time power consumption metrics.

*   **Source**: `turbostat` (running via `sudo`, reading MSRs).
*   **Metrics**:
    *   **Package**: Total package power.
    *   **Core**: CPU Core power.
    *   **RAM**: Memory power.
*   **Total Energy**: Calculated as the **SUM** of `PkgWatt` + `CorWatt` + `RAMWatt` to account for all reported sensors as requested.
*   **History**: Tracks 24-hour and 7-day rolling energy consumption in **kWh**.

## Power State Flow

The following flowchart illustrates how the daemon determines which power profile to apply. **Active Gaming** (either Local Input or Remote Play + Running Game) takes priority.

```mermaid
flowchart TD
    Start([State Change Event]) --> CheckActive{Is Game Running?}
    
    CheckActive -- Yes --> CheckRemote{Is Remote Play?}
    CheckRemote -- Yes --> ForcePerf[Force Performance (Ignore Idle)]
    CheckRemote -- No --> CheckInput{Input Detected?}
    
    CheckActive -- No --> CheckInput
    
    CheckInput -- Yes --> ForcePerf
    CheckInput -- No --> CheckIdle{Is System Idle?}
    
    CheckIdle -- Yes --> Suspend[Pause Game Tree & Set Power Saver]
    CheckIdle -- No --> Resume[Resume Game Tree & Set Performance]
    
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
        *   **Local Input**: Moving mouse/keyboard/controller -> **Performance**.
        *   **Remote Play**: IF Remote Play is active **AND** a Game is running -> **Performance** (Ignores Idle).
    *   **Priority 2: Idle (No Active Usage)**.
        *   **Action**: Force **Power Saver**. If a game is running, **Recursively Pause It** (targeting `wineserver` or game process, avoiding Steam `reaper`).
3.  **Action**:
    *   Manages Power Profiles and Process States (Running/Stopped).

## Prerequisites

The daemon relies on the following tools:

- `powerprofilesctl`: For changing system power profiles.
- `turbostat`: For accurate power monitoring (usually part of `linux-tools` or `linux-cpupower`).
- `powertop` (Optional): For auto-tuning power parameters.
- `scxctl` (Optional): For sched-ext scheduler management.

## Installation

### Build from Source

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

## Usage

### CLI Flags

You can customize the daemon's behavior with flags:

```bash
# Debug mode (verbose logs)
framework-powerd serve --debug

# Custom Idle Timeout (default 5m)
framework-powerd serve --idle-timeout 30s
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

**Sensors Provided**:
*   **Power Mode**: `performance` / `powersave`
*   **System Idle**: `True` / `False`
*   **Game Status**: PID and Running/Paused state
*   **Power Usage**: Package, Core, RAM (Watts)
*   **Energy Consumption**: Last 24h & 7 Days (kWh, Sum of Pkg+Cor+RAM)
*   **Uptime**: System uptime (duration)
*   **Polling Interval**: Configurable number entity (seconds)

### API Control

Trigger modes manually using the REST API (default port 8080).

If JWT authentication is enabled, you must export your token first:
```bash
export TOKEN="your_jwt_token_here"
```

- **Set Performance**:
  ```bash
  curl -H "Authorization: Bearer $TOKEN" -X POST -d '{"mode":"performance"}' http://localhost:8080/mode
  ```

- **Set Powersave**:
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
  # Output: {"mode":"powersave","is_idle":true,"seconds_until_idle":0,"is_game_paused":true,...}
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

You can configure the listening address and port using CLI flags with the `serve` command.

- `--address`: The IP address to listen on (default: `localhost`). Use `0.0.0.0` to listen on all interfaces.
- `--port`: The port to listen on (default: `8080`).

Example:
```bash
/usr/local/bin/framework-powerd serve --address=0.0.0.0 --port=9090
```

> **Note**: If you change the port or address, remember to update your API calls (e.g., `curl`) accordingly.


