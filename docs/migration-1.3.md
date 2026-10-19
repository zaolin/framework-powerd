# Migrating to 1.3

Version 1.3 removes the Ollama integration and adds persistent energy metrics,
new Home Assistant memory/GPU sensors and operational hardening.

## 1. Update the daemon

```bash
git pull
make build
sudo cp framework-powerd /usr/local/bin/
sudo systemctl restart framework-powerd
```

Verify:

```bash
framework-powerd --version
curl http://localhost:8080/healthz
```

## 2. Clean the configuration

Remove the old sections:

```json
"ollama": { ... },
"pricing": { ... }
```

Unknown keys are ignored by the JSON decoder, so the daemon still starts, but
remove them to keep the file accurate. Add the new section if you want to
customize persistence:

```json
"metrics": {
  "persist": true,
  "state_file": "/var/lib/framework-powerd/metrics.json",
  "save_interval": "5m"
}
```

The defaults already enable persistence at that path.

## 3. Update the systemd unit

The shipped unit changed:

- `Type=notify` and `WatchdogSec=30s`
- `RestartSec=5s` and a start limit
- `StateDirectory=framework-powerd`
- `EnvironmentFile=-/etc/framework-powerd/env` (created by `make install`)
- additional hardening (`NoNewPrivileges`, `PrivateTmp`, `ProtectHome`,
  `ProtectSystem=full`, `ProtectKernelModules`, `ProtectKernelLogs`, etc.)

If you have a custom unit, merge these settings. Keep
`ProtectKernelTunables=no`; the daemon writes `/sys` power tunables.

```bash
sudo cp configs/systemd/framework-powerd.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl restart framework-powerd
```

## 4. Update Home Assistant

Update through HACS and restart Home Assistant.

Removed entities:

- all `Ollama ...` sensors
- `System VRAM Used`

`System VRAM Used` was only meaningful when Ollama VRAM was subtracted. With
Ollama removed it duplicated `GPU VRAM Used`.

Added entities:

- `Energy (Total)` — persisted lifetime counter
- `RAM Used`, `RAM Free`, `RAM Available`, `RAM Usage`
- `GPU VRAM Free`, `GPU VRAM Usage`
- `GPU GTT Total`, `GPU GTT Free`, `GPU GTT Usage`

If removed entities remain in the registry as unavailable, delete them manually
in Settings → Devices & Services → Entities.

The integration device now shows the daemon version as `sw_version`, and
diagnostics downloads redact the JWT token.

## 5. API changes

- `GET /ollama/stats` was removed.
- `GET /status` no longer contains an `ollama` block.
- `GET /status` now contains `memory` and derived GPU free/percentage fields.
- `GET /healthz` is new and does not require authentication.

Clients that parse `/status` should tolerate additive fields; removing the
`ollama` field is a breaking change for any client that required it.

## 6. Persistence notes

The first start after upgrading creates `/var/lib/framework-powerd/metrics.json`
on the first flush. The 24h/7d history starts empty; the lifetime counter starts
at zero. From then on it survives restarts. A state file older than seven days
is discarded.

If the daemon runs as a non-root user without a writable `/var/lib`, set
`metrics.state_file` to a writable path or disable persistence:

```json
"metrics": {"persist": false}
```
