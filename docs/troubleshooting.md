# Troubleshooting

Start with:

```bash
curl http://localhost:8080/healthz
journalctl -u framework-powerd -f
```

`/healthz` reports `status`, the last power sample age, optional monitor state
and the metrics persistence status.

## Startup fails with "Dependencies missing"

The daemon requires `powerprofilesctl`:

```bash
# Debian/Ubuntu
sudo apt install power-profiles-daemon
# Arch/CachyOS
sudo pacman -S power-profiles-daemon
```

`turbostat`, `powertop`, `scxctl` and `iw` are optional.

## CPU power and energy stay at zero

- Install `turbostat` (`linux-tools` / `linux-cpupower`).
- The daemon must run as root or have MSR access; the shipped unit uses root.
- Check `/healthz`: `monitors.turbostat` should be `ok` after the first sample.
  `waiting` means no sample yet; `stale` means the last sample is older than
  15 seconds.
- Look for `turbostat stopped ...` / `restarting in ...` in the logs. The
  supervisor retries with backoff.

## Idle detection never triggers

- The daemon needs read access to `/dev/input/event*` and `/dev/input/js*`.
  Running as root (as in the unit) is the simplest fix.
- If running as a normal user, add the user to the `input` group and re-login.
- Moving a mouse during startup is ignored for the initial scan? No — activity
  is timestamped from daemon start. Wait for `server.idle_timeout`.
- Check `seconds_until_idle` in `/status`. A stuck value close to the timeout
  means input events are still arriving (for example a drifting joystick).
  The built-in deadzone filters small axis noise.

## A game stays paused after stopping the daemon

The daemon resumes the paused process tree on graceful shutdown. If the process
was killed with `SIGKILL` or the machine lost power, the game may remain stopped.
Resume it manually with:

```bash
kill -CONT <pid>
```

## Home Assistant cannot connect

- Verify the daemon is reachable from the HA host:
  `curl http://<daemon-ip>:8080/healthz`.
- If the daemon binds to `localhost`, HA on another machine cannot reach it.
  Set `server.address` to `0.0.0.0` (and firewall the port) or run HA on the same
  host.
- A `401` means the JWT is missing, expired or signed with a different secret.
  Generate a fresh token with `framework-powerd token --jwt-secret=...`.
- `--disable-auth` is only safe when the API is bound to localhost.

## RAM/VRAM/GTT sensors are missing

- Update the daemon first; the new fields (`memory`, derived GPU values) must be
  present in `/status`.
- GPU sensors require `gpu.enabled: true` and a readable AMD sysfs path. Check
  `/sys/class/drm/card*/device/mem_info_vram_used`.
- The integration creates the sensors on the first update that contains the
  corresponding block. Reload the integration if needed.

## Metrics persistence errors

`/healthz` shows `metrics_state.last_error` and `last_saved_at`.

- Default path: `/var/lib/framework-powerd/metrics.json`.
- The systemd unit creates the directory via `StateDirectory=framework-powerd`.
- When running manually as a non-root user, point `metrics.state_file` to a
  writable path or set `"persist": false`.
- A corrupt state file is logged and ignored; the daemon starts with a fresh
  buffer. The next save overwrites it.

## systemd restarts the service repeatedly

- Check `journalctl -u framework-powerd`.
- The unit uses `Type=notify` and `WatchdogSec=30s`. If startup takes longer than
  30 seconds, systemd kills it before `READY=1`. Investigate slow
  `powerprofilesctl` or `scxctl` calls.
- `RestartSec=5s` and `StartLimitBurst=5` prevent tight restart loops; after the
  limit, systemd stops trying. Fix the underlying error and run
  `systemctl reset-failed framework-powerd`.
- If you use a custom unit, make sure `ProtectKernelTunables` stays **off**; the
  daemon writes `/sys` tunables.

## SMART alerts do not appear

- `smartd.enabled` must be true.
- smartd must log to journald and use the configured `service_unit` (default
  `smartd.service`).
- Check the journal: `journalctl -u smartd.service`.
- Alerts are sticky and persisted to `smartd.state_file`; check
  `/smartd/stats` and `/healthz`.
- If smartd logged a failure before the daemon started, the direct NVMe poll
  still catches current health, but journal-only events from the past are not
  replayed.

## NVMe health is unavailable or zero

- Install `nvme-cli` (preferred) or `smartmontools`:
  ```bash
  sudo apt install nvme-cli smartmontools
  # or: sudo pacman -S nvme-cli smartmontools
  ```
- The daemon must run as root to read NVMe SMART data. The shipped unit does.
- Verify manually:
  ```bash
  sudo nvme smart-log --output-format=json /dev/nvme0
  sudo smartctl --json=o -a /dev/nvme0
  ```
- Check `/healthz` → `monitors.nvme` and `/status.nvme`; `last_error` explains
  a failed poll.
- If neither tool is installed, NVMe polling is disabled and smartd journal
  detection remains active.

## NVMe warnings or notifications are too noisy / too quiet

Tune the `nvme` section:

- `warn_percentage_used` / `crit_percentage_used` — wear thresholds.
- `warn_available_spare` — spare capacity warning.
- `warn_temperature_c` / `crit_temperature_c` — temperature.
- `warn_err_log_entries` / `crit_err_log_entries` — error-log growth.
- `max_media_errors` — allowed media errors before critical.

Notifications are deduplicated per alert key and severity with a six-hour
cooldown. A persistent warning re-notifies after the cooldown; an escalation
from warning to critical notifies immediately.

## Storage notifications do not arrive

- The integration creates a persistent notification and calls `notify_service`
  when set.
- In Home Assistant, verify the service exists under Developer Tools → Actions
  and use the full name, e.g. `notify.mobile_app_pixel_11_pro`.
- The Storage Health binary sensor is on when any SMART alert or NVMe device is
  warning/critical. You can build your own automation from it.
- Check Home Assistant logs for `Could not call notify.*`.

## GTT sensors show MiB instead of GiB

Check the entity registry override:

**Settings → Devices & Services → Entities → search “GPU GTT Used” → gear →
Unit of measurement** — clear the old `MiB` override or select `GiB`. Repeat for
`GPU GTT Total` and `GPU GTT Free`. The integration code already reports GiB.

## Daemon Health is degraded / NVMe Status is not ok

- `Daemon Health` mirrors `/healthz`. Its attributes show turbostat, NVMe and
  metrics persistence state.
- `NVMe Status` is `error` when the last poll failed; check `last_error`.
  Common causes: `nvme`/`smartctl` missing, no access to `/dev/nvme*`, or the
  device is not visible inside a container.
- `NVMe Status` is `no_devices` when polling works but no controllers were
  discovered.
- `NVMe Status` is `disabled` when `nvme.enabled` is false or no tool exists.
- The per-device NVMe sensors are only created when at least one device is
  discovered.

## Caddy will not start and every proxied site is down

Caddy loads all sites from a single configuration, so one bad site block aborts
the whole load. Every site on the host goes down, not just the broken one. The
unit exits with status 1, and `Restart=on-abnormal` does not restart it, so it
stays down until it is started again.

Find the cause:

```bash
journalctl -u caddy --no-pager -n 40
```

A common error is an address collision:

```
loading new config: http app module: start: listening on :18080:
listen tcp :18080: bind: address already in use
```

Check who currently owns the port, then move one of the two:

```bash
ss -ltnp | grep 18080
```

For a reverse-proxy site the listen port and the upstream port must be
different. A site that proxies to its own listen port is both a loop and a
collision. See [LLM Request Accounting](llm.md#setup) for the port layout used
by the LLM snippet.

After fixing the config, validate and restart:

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl restart caddy
```

`caddy validate` can print `HTTP request failed ... validation complete` for a
`reverse_proxy` health check even when the configuration is valid. That message
is produced in validation mode and is not the reason a start fails.

## API returns 429

`/mode` and `/activity` are limited to 1 request/second with a burst of 5 per
client IP. Wait and retry. This is intentional protection for the control plane.

## Debug logging

```bash
framework-powerd serve --debug
```

Or add `--debug` to `ExecStart` in a drop-in unit. Debug mode logs device
hotplug, input activity and failed optional sysfs writes.
