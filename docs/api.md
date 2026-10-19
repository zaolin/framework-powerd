# REST API

Default base URL: `http://localhost:8080`.

Authentication uses a JWT Bearer token when `server.jwt_secret` is set:

```bash
export TOKEN=$(framework-powerd token --jwt-secret="mysecret")
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/status
```

`/healthz` is intentionally unauthenticated. All other endpoints require a
valid token. Non-Bearer schemes are rejected.

## Rate limiting

`POST /mode` and `POST /activity` allow **1 request/second with a burst of 5**
per client IP. Exceeding it returns `429 Too Many Requests`.

All JSON responses set `Cache-Control: no-store`.

## `GET /healthz`

Liveness/readiness probe. No authentication. Safe for systemd, uptime checks
and monitoring.

```bash
curl http://localhost:8080/healthz
```

```json
{
  "status": "ok",
  "version": "1.3.0",
  "uptime_seconds": 12345.6,
  "is_idle": false,
  "power_sample_age_seconds": 1.2,
  "memory_used_percent": 57.1,
  "monitors": {
    "turbostat": "ok",
    "smartd": true,
    "gpu": true
  },
  "metrics_state": {
    "enabled": true,
    "state_file": "/var/lib/framework-powerd/metrics.json",
    "last_saved_at": "2026-10-19T14:30:00Z"
  }
}
```

- `status` is `ok` or `degraded`.
- `monitors.turbostat` is `ok`, `stale` (no sample for more than 15 s) or
  `waiting` (no sample yet).
- `status` becomes `degraded` when turbostat is stale, still waiting after the
  30-second startup grace period, or the last metrics write failed.
- `last_error` is omitted while persistence is healthy.

## `GET /status`

Full daemon state. Requires auth.

```json
{
  "mode": "balanced",
  "is_idle": false,
  "seconds_until_idle": 241.5,
  "is_game_running": true,
  "is_game_paused": false,
  "is_remote_play": false,
  "game_pid": 4242,
  "uptime": "3h12m4s",
  "uptime_seconds": 11524,
  "power": {
    "current": {
      "pkg_watt": 18.4,
      "cor_watt": 3.1,
      "gfx_watt": 0,
      "ram_watt": 1.2,
      "estimated_total_watts": 27.5,
      "peripheral_watts": 6.7
    },
    "energy_24h_kwh": 0.412,
    "energy_7d_kwh": 2.871,
    "lifetime_energy_kwh": 42.125
  },
  "memory": {
    "total_bytes": 50010562560,
    "free_bytes": 8589934592,
    "available_bytes": 21474836480,
    "used_bytes": 28535726080,
    "used_percent": 57.06
  },
  "network_devices": [
    {"interface": "wlan0", "power_control": "on"}
  ],
  "gpu": {
    "vram_used_bytes": 2984828928,
    "vram_total_bytes": 17179869184,
    "vram_free_bytes": 14195040256,
    "vram_used_percent": 17.37,
    "gtt_used_bytes": 1143558144,
    "gtt_total_bytes": 25001775104,
    "gtt_free_bytes": 23858216960,
    "gtt_used_percent": 4.57,
    "cpu_usage_percent": 0.6,
    "temperature_celsius": 49,
    "power_watts": 9.0,
    "last_update": "2026-10-19T14:30:00Z"
  },
  "system_info": {
    "kernel_version": "6.12.1-cachyos-x86_64",
    "os_name": "CachyOS",
    "os_version": "1.0",
    "cpu_model": "AMD Ryzen AI 7 350 w/ Radeon 860M",
    "cpu_count": 16,
    "total_ram_bytes": 50010562560,
    "daemon_version": "1.3.0"
  }
}
```

Notes:

- `mode` is always `balanced`: the APU profile trio is pinned. The internal
  performance/powersave gate only selects the secondary tunables.
- `smartd` is added when SMART monitoring is enabled. `gpu` is added when GPU
  monitoring is enabled.
- `power.current.estimated_total_watts` includes peripheral estimates and VRM
  correction. `cor_watt` is a subset of `pkg_watt` and is not added again.
- `memory.used_bytes = total - available`; `free_bytes` is literal `MemFree`.
- `gtt_*` values are bytes, like the VRAM values. Home Assistant renders GTT in
  MiB and VRAM/RAM in GiB.

## `POST /mode`

Applies the active or idle secondary tunables. Requires auth.

```bash
curl -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"performance"}' \
  http://localhost:8080/mode
```

Allowed modes: `performance`, `powersave`. Response:

```json
{"message": "Mode set successfully", "mode": "balanced"}
```

`500` is returned if `powerprofilesctl` fails; the mode is not committed in
that case.

## `POST /activity`

Resets the idle timer without changing the mode. Requires auth.

```bash
curl -H "Authorization: Bearer $TOKEN" -X POST http://localhost:8080/activity
```

```json
{"message": "Activity triggered, idle timer reset"}
```

## `GET /smartd/stats`

Returns SMART alerts when `smartd.enabled` is true; otherwise `404`.
Requires auth.

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/smartd/stats
```

```json
{
  "alerts": [
    {"device": "/dev/sda", "fail_type": "SelfTest", "message": "FAILED SMART self-test"}
  ],
  "last_check": "2026-10-19T14:30:00Z",
  "notify_service": "notify.mobile_phone"
}
```

## Errors

| Status | Meaning |
|---|---|
| `400` | Invalid JSON or unsupported mode. |
| `401` | Missing/invalid token, wrong scheme, or `authorized=false`. |
| `404` | Unknown path or optional monitor disabled. |
| `405` | Wrong HTTP method. |
| `429` | Rate limit exceeded. |
| `500` | Power profile command failed. Details are logged server-side only. |
