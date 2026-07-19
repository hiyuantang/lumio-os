# SPDX-License-Identifier: AGPL-3.0-only
# lumiod — Lumio OS Phase 2 read-only Ubuntu agent

`lumiod` is a single, unprivileged Go binary that implements the Phase 2
subset of [../docs/PROTOCOL.md](../docs/PROTOCOL.md): REST endpoints for
system identity, overview, services, journal and files, plus the WebSocket
channel model (`system.metrics`, `services.subscribe`, `journal.stream`).

Phase 2 runs gateway and agent as one process bound to `127.0.0.1`; the
multi-process split (gateway / per-user agent / privileged broker) arrives
in Phase 4. Internal packages are already separated along those lines
(`httpapi`, `wsapi`, `system`, `services`, `journal`, `files`) so the split
is a wiring change, not a rewrite. Request handling is centralized in
`httpapi.Server.wrap` so Phase 4 session/CSRF middleware slots in cleanly.

## Dependencies

- `github.com/gorilla/websocket` — WebSocket transport.
- `github.com/godbus/dbus/v5` — systemd D-Bus client.

No other third-party dependencies; the module builds cgo-free.

## Run on macOS (development)

```sh
cd server
go build -o lumiod ./cmd/lumiod
./lumiod -addr 127.0.0.1:8080
```

systemd and journald do not exist on macOS: `/api/v1/services`,
`services.subscribe` and the journal capabilities answer with the
`unavailable` error shape while identity, metrics (zeroed `/proc` fields),
and files keep working.

Flags:

- `-addr` — listen address, default `127.0.0.1:8080`.
- `-web` — serve the frontend from a directory (e.g. `-web ../dist`).
  Without it the binary serves embedded assets when built with
  `-tags webdist`, otherwise `/` answers 404 with a hint.

## Run on Ubuntu

```sh
./lumiod -addr 127.0.0.1:8080
```

Reach it through an SSH tunnel (`ssh -L 8080:127.0.0.1:8080 host`) during
Phase 2; there is deliberately no TLS and no auth on this build.

## Build with the embedded frontend

```sh
scripts/build-with-web.sh   # npm build -> copy dist -> go build -tags webdist
```

produces `server/bin/lumiod` with `dist/` embedded via `go:embed`
(`internal/static`).

## Integration test (Docker, the real gate)

```sh
scripts/integration-test.sh
```

Builds `docker/Dockerfile.ubuntu24` (Ubuntu 24.04, systemd as PID 1, cron as
a demo unit), starts it privileged with cgroup v2 mounted, then asserts the
REST surface with curl and the WS surface with `cmd/wscheck`. The Phase 2
exit gate is a hard assertion: a subscribed `services.subscribe` channel
must observe a `changed` event when the script runs
`docker exec … systemctl stop cron`. The script removes the container and
image on exit.

## Design decisions and deviations

### Journal backend: `journalctl --output=json`, not libsystemd

BIG-PICTURE names libsystemd's journal API as the preferred interface. For
Phase 2 we deliberately shell out to `journalctl --output=json` (with `-u`,
`-p`, `--since`, `-n`, `--after-cursor`, `-f`) and parse the JSON lines:

- libsystemd's sd-journal API requires cgo and a Linux build host, which
  breaks the cgo-free, cross-compilable build and the macOS dev loop.
- `journalctl -o json` is the documented machine-readable output mode;
  only the human-readable formats are unstable.
- The dependency is hidden behind the `journal.Backend` interface
  (`internal/journal/journal.go`), so a libsystemd/sdjournal implementation
  can replace `journal.CLI` later without touching `httpapi`/`wsapi`.

Cursors are the journal's own `__CURSOR` values, passed through opaquely;
paging uses `--after-cursor`, and `journal.stream` resumes via `after`.
Entry messages are capped at 16 KiB and individual fields at 4 KiB so a
single event always fits the protocol's 64 KiB WebSocket frame limit.

### Binary files in `files.read`

PROTOCOL.md says binary files are "flagged in `details` rather than
decoded". The envelope's `details` only exists on errors, so this build
flags binary content inside the success payload instead:
`data.encoding` is `"binary"` and `data.content` is `null` (revision and
size are still returned). Encoding detection: NUL byte in the first 8000
bytes, or invalid UTF-8 after trimming a trailing partial rune.

### `services.action` and other future capabilities

Endpoints whose phase has not shipped (`services.action`, `files.write`,
`updates.*`) answer `503 unavailable` in the standard error envelope rather
than 404, per PROTOCOL.md's capability table.

### Container binding

`docker/lumiod.service` starts lumiod with `-addr 0.0.0.0:8080` only so the
Docker port forward used by the integration test can reach it. The default
remains `127.0.0.1:8080` everywhere else.

### Metrics notes

- CPU percent is computed from `/proc/stat` deltas between samples; the
  first sample after start reports 0.
- Disks are statfs'd on real mounts: `/proc/mounts` entries whose source
  starts with `/dev/` (loop devices excluded).
- Network rates are deltas of `/proc/net/dev` between samples; the first
  sample reports an empty rate list.
- `system.overview` measures CPU over a ~150 ms window and reads the
  updates indicator from `/usr/lib/update-notifier/apt-check` when present.

## Layout

```
cmd/lumiod/      main + flags
cmd/wscheck/     WS assertion client used by the integration test
internal/config/ flag parsing
internal/httpapi REST routing, envelopes, error-code mapping
internal/wsapi/  WS hub: hello/ping/pong, multiplexed numbered channels
internal/system/ identity, overview, metrics sampler (/proc, statfs)
internal/services systemd D-Bus client + change watcher (signals + 10 s poll)
internal/journal/ journal.Backend interface + journalctl CLI implementation
internal/files/  path cleaning, symlink resolution, list/read with revision
internal/static/ embedded dist (webdist tag) + SPA fallback + -web dir
```
