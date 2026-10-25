# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
semantic versioning.

## [1.6.0] - 2026-10-21

### Added

- LLM request accounting in `internal/llm`: a Caddy JSON access log is tailed
  and requests are attributed to client IPs and IP groups.
- `llm` and `pricing` configuration sections with validation, including IP
  groups, excluded IPs, counted paths, history length and persistence.
- `GET /llm/stats` and the `llm` block in `GET /status`.
- `/healthz` reports `monitors.llm` as `ok`, `waiting`, `error` or `disabled`.
- Home Assistant entities for LLM requests (total, today), per-group counts and
  shares, per-IP counts, today's energy and cost, plus the daily history as an
  attribute.
- Caddy snippet `configs/caddy/llm` that proxies `llama-server` and writes a
  rotating JSON access log with credentials stripped. The public listen port
  and the loopback upstream port are separate and can be overridden with
  `LLM_PORT`, `LLM_UPSTREAM` and `LLM_LOG`.
- `docs/llm.md` describing the setup, configuration and troubleshooting.

### Fixed

- A missing access log is no longer reported as an error. `/healthz` now shows
  `monitors.llm` as `waiting` until the proxy creates the file, and a previously
  recorded read error is cleared once reading succeeds.
- Restarting the daemon no longer re-reads the whole access log: the log
  position is persisted with the counters, and day buckets use the log
  timestamp instead of the processing time.
- The accounting snapshot is a deep copy, so the HTTP handlers and the
  persistence loop can no longer race the polling goroutine.
- `history_days` keeps exactly that many days including today.
- Home Assistant energy and cost use the daily buckets, the cost sensor reports
  the configured currency, and group and client-IP entities are added as
  clients appear rather than only at setup.
- Per-IP counters now follow the retention window instead of growing forever,
  and the integration caps the number of per-IP sensors.
- Configuration validation rejects a negative `history_days`, empty
  `counted_paths`, a negative energy price, a currency that is not a
  three-letter ISO 4217 code, and
  group names that only differ in case.

## [1.5.0] - 2026-10-19

### Added

- `tunable_mode` in `/status` and a diagnostic HA Tunable Mode sensor.
- `health` block in `/status` mirroring `/healthz`.
- HA Daemon Health and NVMe Status diagnostic sensors.
- NVMe poll error/device-count details in the Storage Health attributes.
- HA select entity for performance/powersave and a reset-idle button.
- HA services `framework_powerd.set_mode`, `framework_powerd.reset_idle` and
  `framework_powerd.refresh`.

### Changed

- `/status.mode` now reports the pinned APU profile (`balanced`); the internal
  performance/powersave gate moved to `tunable_mode`.
- Home Assistant Power Mode maps legacy `performance`/`powersave` values to
  `balanced`.

### Fixed

- Corrected the README and architecture Mermaid diagrams; they now render and
  reflect the NVMe, health and Home Assistant paths.
- Corrected the idle-logic documentation and added a ticker/state-machine
  diagram.
- Converted the icon/logo assets to real PNG files and added HA 2026.3+
  `brand/` images: transparent light/dark icon at 256/512 and logo at
  512/1024.

## [1.4.0] - 2026-10-19

### Added

- Sticky, de-duplicated SMART alerts with severity, first/last seen and repeat
  count, persisted to `/var/lib/framework-powerd/alerts.json`.
- Broader smartd parsing: health, self-test, attributes, critical warning,
  media errors, available spare, percentage used, error-log growth, pending/
  offline sectors, reallocated sectors, ATA errors and temperature warnings.
- Journal reader retry with backoff.
- Direct NVMe health polling via `nvme smart-log` with `smartctl` fallback.
- NVMe wear, spare, media-error, error-log and temperature thresholds.
- `GET /nvme/stats`, `/status.nvme`, and `monitors.nvme` in `/healthz`.
- Home Assistant NVMe sensors and an aggregated Storage Health binary sensor.
- Persistent notifications and optional `notify_service` calls for new
  warning/critical storage alerts, with dedup and a six-hour cooldown.

### Changed

- `smartd.alert_retention` now defaults to `168h` and means "stay visible after
  last seen" instead of a 30-second TTL.
- SMART alerts are keyed per device and category, so different failures on the
  same disk no longer overwrite each other.
- Home Assistant GTT sensors now render in GiB instead of MiB, matching RAM and
  VRAM.
- Home Assistant integration version bumped to 1.4.0.

## [1.3.0] - 2026-10-19

### Added

- Persistent 24h/7d energy aggregates and a monotonic lifetime counter, written
  atomically to `/var/lib/framework-powerd/metrics.json`.
- `GET /healthz` with daemon version, uptime, idle state, turbostat sample age,
  memory usage, optional monitor state and persistence status.
- Dynamic `/proc/meminfo` readings in `GET /status` (`memory` block).
- Derived GPU fields: VRAM/GTT free bytes and usage percentages.
- Home Assistant sensors for RAM used/free/available/usage, GPU VRAM free/usage,
  GPU GTT total/free/usage and `Energy (Total)`.
- Home Assistant diagnostics with token redaction.
- English config-flow translations (`strings.json`, `translations/en.json`).
- Shared Home Assistant entity base with `sw_version` from the daemon.
- CI workflows for Go, Python, HACS and hassfest validation.
- Makefile targets for binary, config, JWT secret, systemd unit, enable,
  disable, uninstall and token generation.
- Documentation for configuration, API, Home Assistant entities, architecture,
  troubleshooting, security and migration.

### Changed

- `server.idle_timeout` from the config file is now honored; previously the CLI
  default always won.
- CLI overrides use explicit flag detection, so a flag equal to its old default
  still overrides the file.
- Power-state reconciliation is serialized across idle, remote-play, Steam and
  API callbacks.
- turbostat is restarted with exponential backoff after unexpected exits.
- Energy is integrated over real elapsed time instead of a fixed one-second
  assumption.
- The systemd unit uses `Type=notify`, a 30-second watchdog, `RestartSec`, a
  start limit, `StateDirectory` and additional hardening.
- Build version is injected via `-ldflags`; `--version` is available.
- Rate limiting keys on client IP instead of TCP source port and prunes idle
  buckets.
- JSON API responses set `Cache-Control: no-store`.
- Game Running binary sensor uses the daemon's `is_game_running` field.
- System information entities are marked diagnostic.

### Removed

- Ollama usage and model monitoring, including `/ollama/stats`, the `ollama`
  block in `/status`, `ollama.*` config, `pricing.*` config and all Ollama Home
  Assistant sensors.
- `System VRAM Used` Home Assistant sensor (redundant without Ollama VRAM).
- Unused generic `PowerSensor` class.

### Fixed

- Paused game processes are resumed when the daemon shuts down.
- Startup no longer ignores the configured idle timeout.
- A turbostat outage is no longer charged to the energy aggregates.
- Rate-limit buckets no longer grow per TCP connection.
- Energy accumulation no longer assumes every sample is exactly one second.

### Breaking changes

- `/ollama/stats` is gone.
- The `ollama` block is gone from `/status`.
- Removed Home Assistant entities stay unavailable until deleted manually.
- `ollama.*` and `pricing.*` config keys are ignored.
