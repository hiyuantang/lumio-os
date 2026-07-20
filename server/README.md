# SPDX-License-Identifier: AGPL-3.0-only
# lumiod — Lumio OS Phase 5 server stack

`lumiod` is a single Go binary implementing the Lumio OS server stack from
[../docs/PROTOCOL.md](../docs/PROTOCOL.md). It uses the documented
multi-process architecture via subcommands:

```
lumiod gateway    HTTP/WS + auth + CSRF, unprivileged user (lumio-gw)
lumiod sessiond   root: PAM login, session store, spawns per-user agents
lumiod agent      the logged-in user: metrics, services, journal, files, PTY
lumiod broker     root: typed privileged actions (polkit + audit)
```

Processes talk over Unix sockets under `/run/lumio` (see
[../docs/PRIVILEGE_MODEL.md](../docs/PRIVILEGE_MODEL.md) §As built).
There is no legacy unauthenticated single-process subcommand. Internal
packages follow the same boundaries (`httpapi`, `wsapi`, `system`,
`services`, `journal`, `files`, `updates`, `privfiles`).

## Dependencies

- `github.com/gorilla/websocket` — WebSocket transport.
- `github.com/godbus/dbus/v5` — systemd and polkit D-Bus client.
- `github.com/creack/pty` — PTY allocation for the terminal channel.
- `modernc.org/sqlite` — pure-Go SQLite for the broker audit log.
- `github.com/msteinert/pam/v2` — PAM authentication, **only** under the
  `pam` build tag (cgo, Linux). Without the tag the build is cgo-free
  and PAM calls answer `unavailable`; see `-insecure-dev-auth` below.

## Run on macOS (development)

Build the binary:

```sh
cd server
go build -o lumiod ./cmd/lumiod
```

Full multi-process dev stack with dev auth (login as your macOS user
with any password; loud startup warning; **never** for production and
never settable via config file or environment in packaged units):

```sh
./lumiod sessiond -run-dir /tmp/lumio -insecure-dev-auth "$USER" &
./lumiod broker -run-dir /tmp/lumio -db /tmp/lumio/audit.db &
./lumiod gateway -run-dir /tmp/lumio -addr 127.0.0.1:8080
```

systemd/journal/polkit do not exist on macOS: services, journal and
broker actions answer `unavailable`; files, metrics, identity, auth
and PTYs work.

## Run on Ubuntu (production shape)

```sh
lumiod broker &     # root
lumiod sessiond &   # root
lumiod gateway &    # user lumio-gw, -addr 127.0.0.1:8080
```

PAM service file `/etc/pam.d/lumiod` (see `docker/pam.d-lumiod`),
polkit action file `/usr/share/polkit-1/actions/os.lumio.policy`
(see `docker/os.lumio.policy`). Reach the gateway through an SSH tunnel;
there is no TLS until Phase 7.

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

Builds `docker/Dockerfile.ubuntu24` (Ubuntu 24.04, systemd as PID 1), starts
it privileged with cgroup v2 mounted, then runs 66 REST, WebSocket, broker,
audit and repair assertions. The Phase 5 exit gate starts with a failed HTTP
service, finds its error through Services and Logs, validates and writes its
protected configuration, restarts it, confirms the endpoint, and checks the
audit and rollback records. The script removes the container and image on
exit.

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

### Privileged mutations

Service actions, saved package plans and protected `/etc` writes pass through
the peer-credentialed broker, polkit and audit log. A capability whose backing
service is absent answers `503 unavailable` in the standard error envelope
rather than 404, per PROTOCOL.md's capability table.

### Container binding

`docker/lumiod-gateway.service` binds 0.0.0.0:8080 only so the Docker port
forward used by the integration test can reach the gateway. The default
remains `127.0.0.1:8080` everywhere else. The container runs the full
Phase 5 process set (gateway as `lumio-gw`, sessiond and broker as root)
with a test user `alice` and a testbed-only polkit rules file
(`docker/os.lumio-testbed.rules`) that makes authorization deterministic:
`alice` may manage services, `ssh.service` requires `auth_admin` (the
reauth path), and `nginx.service` is denied outright (the audit-denial
path). A production host must not ship that rules file.

### Phase 4 design decisions

- **polkit subject.** polkit has no `unix-user` CheckAuthorization
  subject kind, so the broker subjects the agent's `unix-process` (peer
  pid + `start-time` from `/proc/<pid>/stat`); inside rules this
  resolves to the requesting uid exactly like a uid subject.
- **Reauth freshness lives in sessiond.** The broker validates the
  session token's uid against the peer credential and checks the
  5-minute window with sessiond; the agent cannot self-authorize.
- **polkit details.** The unit name is passed as a CheckAuthorization
  detail key so `.rules` can match per-unit without multiplying action
  ids (the testbed rules use this).
- **Audit.** SQLite via `modernc.org/sqlite` (pure Go) at
  `/var/lib/lumio/audit.db`, WAL mode. Kinds: `begin`/`end` pairs, and
  `deny` for polkit denials and unmet reauth demands. Precondition
  conflicts write no rows (documented pipeline order). Idempotent
  replays are served from the audit table itself, so dedup survives
  broker restarts.
- **WS through the gateway.** The gateway hijacks the upgraded
  connection and splices bytes to the agent's socket, so the Phase 2/3
  WS implementation in `wsapi` runs unchanged inside the agent.

### Metrics notes

### Terminal sessions

- `terminal.open` returns an opaque `session` token in the `subscribed`
  frame's `data` (additive protocol extension, documented in
  docs/PROTOCOL.md).
- Scrollback is capped at 64 KiB per session and replayed on reattach.
- On socket drop the PTY survives for 120 s; an explicit `unsubscribe`
  kills it immediately.
- The shell is `$SHELL` (fallback `/bin/sh`) with a clean environment
  (`TERM=xterm-256color`, `COLORTERM=truecolor`, minimal PATH/HOME/USER).
- Test-client note: input written while the shell's line discipline is
  still initializing can be eaten by the tty; `cmd/wscheck` waits 500 ms
  after `subscribed` before typing. Real clients (xterm.js) are driven by
  human keystrokes and never hit this.

### files.write / files.delete

- Atomic save: temp file in the same directory, fsync, existing mode and
  ownership preserved, rename, directory fsync. New files are `0644`.
- `expectedRevision` compares against the current `sha256:` revision;
  mismatch → `stale_revision` with `details.expectedRevision` /
  `actualRevision`. A precondition on a missing file → `not_found`.
- Decoded content cap is 8 MiB; the transport's 1 MiB REST body limit is
  overridden to 12 MiB for this route only (documented in PROTOCOL.md).
- Writes and deletes on the same resolved path are serialized by a
  per-path lock.
- `files.delete` moves to `~/.local/share/Trash/{files,info}` per the
  freedesktop trash spec (same-filesystem renames only; `EXDEV` and an
  unwritable trash → `validation_failed`). No permanent delete in Phase 3.

### Idempotency

`requestId` dedup is an in-memory store (24 h TTL, swept on insert).
It does not survive restarts — acceptable for Phase 3; Phase 4's audit
log will persist it.

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
cmd/lumiod/      subcommand dispatch: gateway | sessiond | agent | broker | (default: single-process)
cmd/wscheck/     WS assertion client used by the integration test
internal/config/ flag parsing (single-process mode)
internal/auth/   PAM behind the `pam` build tag + nopam fallback
internal/ipc/    unix-socket HTTP helpers, SO_PEERCRED, /proc start times
internal/gateway/ cookies, CSRF, login rate limiting, REST proxy, WS splice
internal/sessiond/ PAM login, session store/expiry, agent spawn + reap
internal/broker/ action validation, polkit authorizer, audit, systemd exec
internal/httpapi agent REST routing, envelopes, error-code mapping
internal/wsapi/  WS hub: hello/ping/pong, multiplexed numbered channels
internal/system/ identity (incl. user), overview, metrics sampler
internal/services systemd D-Bus client + change watcher (signals + 10 s poll)
internal/journal/ journal.Backend interface + journalctl CLI implementation
internal/files/  path cleaning, list/read, atomic write, freedesktop trash
internal/terminal/ PTY session manager (tokens, scrollback, 120 s grace)
internal/static/ embedded dist (webdist tag) + SPA fallback + -web dir
```
