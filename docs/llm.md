# LLM Request Accounting

The daemon can attribute LLM API requests to client IPs and IP groups. It
reads the access log of a reverse proxy that sits in front of
`llama-server`.

This replaces the Ollama usage monitoring that was removed in 1.3, but works
with `llama-server` instead of Ollama.

## Why a proxy is required

`llama-server` does not expose the client IP:

- The only place the address appears is the HTTP access logger in
  `tools/server/server-http.cpp`, and that logger is **disabled** in the
  upstream source (`// srv->set_logger(log_server_request);`).
- The `server_http_req` struct that the route handlers receive carries no
  remote address.
- `/slots` returns slot and token statistics, `/metrics` returns Prometheus
  counters — neither contains the client IP.

A reverse proxy sees the client address on every request and can log it. Caddy
writes a structured JSON access log that the daemon parses directly.

## Setup

### 1. Run llama-server on loopback only

Publish the container on loopback and move its host port away from the one
Caddy will publish, so the proxy is the only entry point. For example, in the
service definition of your compose file:

```yaml
services:
  llama-server:
    ports:
      - "127.0.0.1:18081:18080"   # host 18081 -> container 18080
```

Only the host side of the mapping changes. The container keeps listening on
18080 internally, so `LLAMA_ARG_PORT`, `--port` and the container healthcheck
stay as they are.

Apply it with `docker compose up -d`. The container is recreated, which reloads
the model preset.

> **The two ports must differ.** Caddy listens on the public port and proxies to
> the container's published port. If both are the same number, Caddy proxies to
> itself; if the listen port is already taken, Caddy aborts the whole config
> load and **every** site on that host goes down, not just the LLM one. Confirm
> the host side of the mapping with `docker ps`; it must read
> `127.0.0.1:18081->18080/tcp`.

### 2. Configure Caddy

Install the shipped snippet:

```bash
sudo install -m644 configs/caddy/llm /etc/caddy/conf.d/llm
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

The snippet listens on port 18080 and proxies to `127.0.0.1:18081`, then writes
the JSON access log to `/var/log/caddy/llm-access.log` with rotation. It deletes
the `Authorization` and `Cookie` request headers before logging, so bearer
tokens and session cookies stay out of the file. Other headers and the query
string are still logged, so do not pass credentials in a query parameter.

Those are example ports. The listen port, upstream and log path can be
overridden without editing the file, through the `LLM_PORT`, `LLM_UPSTREAM` and
`LLM_LOG` environment variables; pick values that match your container. Do not
reuse the daemon's own API port (default `8080`) as the Caddy listen port.

Two things the snippet cannot do for you:

- your `/etc/caddy/Caddyfile` must import the directory it is installed into,
  for example `import /etc/caddy/conf.d/*`
- `/var/log/caddy` must exist and be writable by the user Caddy runs as,
  otherwise Caddy fails to start when it opens the log file

`caddy validate` may print `HTTP request failed ... validation complete` for the
health check. That is validation-mode noise, not a configuration error.

Check that the log is being written:

```bash
sudo tail -f /var/log/caddy/llm-access.log
```

### 3. Enable accounting in the daemon

In `/etc/framework-powerd/config.json`:

```json
"llm": {
  "enabled": true,
  "access_log": "/var/log/caddy/llm-access.log",
  "ignore_local_ip": true,
  "exclude_ips": [],
  "groups": [
    {"name": "home", "cidrs": ["192.168.0.0/16"]},
    {"name": "vpn", "cidrs": ["10.8.0.0/24"]}
  ],
  "counted_paths": [
    "/v1/chat/completions",
    "/chat/completions",
    "/v1/completions",
    "/completions",
    "/v1/responses",
    "/v1/messages",
    "/v1/embeddings",
    "/embedding"
  ],
  "history_days": 30,
  "state_file": "/var/lib/framework-powerd/llm.json",
  "save_interval": "1m"
},
"pricing": {
  "energy_price_per_kwh": 0.30,
  "currency": "EUR"
}
```

Restart the daemon afterwards.

> **Note**: `/var/log/caddy` is only readable by root and the `caddy` user.
> The daemon must run as root (the shipped unit does) to read the access log.

## Configuration reference

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable LLM request accounting. |
| `access_log` | string | `/var/log/caddy/llm-access.log` | Path to the Caddy JSON access log. |
| `groups` | list | `[]` | Named IP groups (`name` + `cidrs`). |
| `exclude_ips` | list | `[]` | IPs that are never counted. |
| `counted_paths` | list | inference paths | Only these request paths are counted. |
| `history_days` | int | `30` | Days of history to keep, including today. |
| `state_file` | string | `/var/lib/framework-powerd/llm.json` | Persisted accounting state. |
| `save_interval` | duration | `1m` | Periodic flush interval. |
| `ignore_local_ip` | bool | `true` | Skip loopback traffic. |

A CIDR may also be a single IP address. The first matching group wins. Group
names are compared case-insensitively because they become Home Assistant entity
names. `counted_paths` must not be empty while accounting is enabled, and
`pricing.currency` must be a three-letter ISO 4217 code such as `EUR` or `USD`,
because Home Assistant uses it as the unit of the cost sensor.

## What is counted

Only requests whose path is listed in `counted_paths`. Health checks, model
listings and token counting endpoints are ignored, so the numbers reflect real
inference traffic.

Days are bucketed in UTC from the timestamp Caddy writes into each log line, not
from the time the daemon reads it. A request that finished just before midnight
stays in the previous day even if the daemon picks it up afterwards.

## Energy and cost attribution

Each request is attributed an energy estimate of

```
energy_kWh = measured_total_watts × duration_hours / 1000
```

For example, 100 W for 10 seconds is `100 × (10 / 3600) / 1000 ≈ 0.00028 kWh`.

using the daemon's live power measurement. The result is an estimate, not a
metered value: several concurrent requests share the same measured power draw,
so the attribution depends on the request duration rather than on the actual
per-request energy.

Cost is `energy_kWh × pricing.energy_price_per_kwh`.

## API

`GET /llm/stats` returns the accounting state:

```json
{
  "enabled": true,
  "since": "2026-10-21T00:00:00Z",
  "total": {"count": 120, "total_duration_secs": 3600.5, "total_energy_kwh": 0.12, "total_cost": 0.0372, "endpoints": {"/v1/chat/completions": 120}},
  "by_ip": {"192.168.1.50": {"count": 100}},
  "by_group": {"home": {"count": 100}},
  "ungrouped": {"count": 0},
  "days": [{"date": "2026-10-21", "total": 70, "total_energy_kwh": 0.07, "total_cost": 0.021, "by_ip": {}, "by_group": {}}],
  "today": {"date": "2026-10-21", "total": 70, "total_energy_kwh": 0.07, "total_cost": 0.021, "by_ip": {}, "by_group": {}},
  "currency": "EUR",
  "price_per_kwh": 0.30,
  "last_request": "2026-10-21T14:30:00Z"
}
```

The same block is part of `GET /status`. `/healthz` reports
`monitors.llm` as `ok`, `waiting`, `error` or `disabled`.

## Home Assistant entities

| Entity | Unit | Source |
|---|---|---|
| LLM Requests (Total) | count | `llm.total.count` |
| LLM Requests Today | count | `llm.today.total` |
| LLM Energy Today | kWh | `llm.today.total_energy_kwh` |
| LLM Cost Today | currency | `llm.today.total_cost` |
| LLM `<group>` Requests Today | count | `llm.today.by_group` |
| LLM `<group>` Requests Today Share | % | group share of today |
| LLM `<ip>` Requests Today | count | `llm.today.by_ip` |

Group and client-IP entities are added as those clients first appear, without
reloading the integration. The share of today's requests for a single IP is
exposed as the `percent` attribute of its sensor. Because client addresses are
unbounded in principle, the integration creates at most 25 per-IP sensors; the
complete per-IP data is always available in the `by_ip` attribute of the
`LLM Requests Today` sensor. The daily history is available as the `history`
attribute on the total and today sensors, so it can be graphed without extra
entities.

## Persistence

The accounting state is written atomically to `llm.json` every
`save_interval` and once more on shutdown. It holds the counters, the daily
buckets and the position in the access log. On startup the daemon restores them
and continues reading where it stopped, so an ordinary restart neither loses nor
re-counts requests. If the log was rotated while the daemon was down, the current
file is read from its beginning and lines written to the rotated file during
that gap are lost.

Retention applies unevenly by design. `total`, `by_group` and `ungrouped` are
lifetime counters. The daily history and `today` cover `history_days`, and an
address that has been idle longer than that window is dropped from `by_ip`, so
the per-client counters do not grow without bound.

## Troubleshooting

- **Caddy does not start and every site is unreachable**: the listen port
  collides with something else, usually the container's published port.
  `journalctl -u caddy` shows `bind: address already in use`, and
  `ss -ltnp | grep <port>` shows the current owner. Give Caddy its own port and
  move the container, as described in [Setup](#setup).
- **No requests are counted**: check that the access log exists and grows
  (`sudo tail /var/log/caddy/llm-access.log`), that `llm.enabled` is true, and
  that the request path is listed in `counted_paths`.
- **Health reports `waiting` forever**: the log file does not exist yet, so the
  proxy is not writing it. Check the Caddy site and the `output file` path.
- **Health reports `error`**: the daemon cannot read the log file. The file
  lives in `/var/log/caddy`, which is root-only — the daemon must run as root.
- **Everything lands in "ungrouped"**: the client IP does not match any CIDR.
  Check the groups and whether the proxy logs the real client address (it logs
  the immediate peer, so the proxy must be the first hop).
- **Counts are too high**: a client path that is not inference traffic is
  listed in `counted_paths`. If the numbers jumped after a restart, the state
  file is not being written or is not readable, so the log is read from the
  beginning again; check `llm.state_file` and the daemon log.
