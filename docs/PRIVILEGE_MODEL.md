# Lumio OS — Privilege Model

Who may do what, and how elevation works. Wire shapes are defined in
[PROTOCOL.md](PROTOCOL.md); the threats this model defends against are
in [THREAT_MODEL.md](THREAT_MODEL.md); failure handling in
[ERROR_AND_RECOVERY.md](ERROR_AND_RECOVERY.md).

## Principles

1. Ordinary operations run as the authenticated Linux user — nothing
   else.
2. Elevation happens through a small, typed, root-owned broker using
   polkit for authorisation decisions.
3. There is no generic root shell and no arbitrary privileged command
   API.
4. Every privileged mutation is authenticated, authorised, validated,
   idempotent where possible, and audited.

## Process and identity map

| Process | Identity | Role |
|---|---|---|
| Web gateway | Dedicated unprivileged user (`lumio-gw`) | TLS, static assets, cookies, CSRF, WS routing |
| sessiond | Root, minimal | PAM authentication; launches per-user agents |
| Session agent | The real UID/GID of the logged-in user | PTY, files, journal, metrics, D-Bus reads |
| Privileged broker | Root, tiny API surface | Typed privileged actions, polkit checks, audit |

The agent runs inside the user's PAM session with the user's
supplementary groups and limits. It can do exactly what the same user
could do over SSH — no more.

## Operation routing

| Operation | Executed by | Why |
|---|---|---|
| `system.identity`, `system.overview`, `system.metrics` | Agent | World-readable host data (`/proc`, D-Bus) |
| `services.list`, `services.subscribe` | Agent | systemd D-Bus read access |
| `journal.query`, `journal.stream` | Agent | User's journal view; group membership governs system journal |
| `files.list`, `files.read`, `files.write` | Agent | Real UID/GID permissions are the enforcement |
| `terminal.*` (PTY) | Agent | The shell must be the user, not root |
| `services.start` / stop / restart / reload / enable / disable | Broker | Requires privilege; typed and auditable |
| `files.writePrivileged` | Broker | Root-owned configuration files |
| `packages.applyPlan` | Broker | Package management requires privilege and a lock |
| `network.applyWithRollback`, `firewall.applyWithRollback` | Broker | Can cut off access; needs the dead-man switch |
| `users.addSshKey` | Broker | Writes another user's authorisation material |
| `system.reboot`, `system.poweroff` | Broker | Highest-risk power actions |
| `updates.refresh`, `updates.plan` | Agent → broker's package worker | Read-only against the package cache, but serialised |

Reads never go through the broker. The broker exists only for
mutations that need privilege.

## polkit

### Action ids

All Lumio OS actions live under `os.lumio.*`:

| Action id | Covers | Authorisation |
|---|---|---|
| `os.lumio.services.manage` | restart, start, stop, reload, enable, disable | Active session |
| `os.lumio.packages.apply` | apply a previously computed plan | Active session |
| `os.lumio.files.write-privileged` | write root-owned files | Active session |
| `os.lumio.network.apply` | Netplan apply with rollback | Reauthentication |
| `os.lumio.firewall.apply` | firewall apply with rollback | Reauthentication |
| `os.lumio.users.manage` | add/remove SSH keys, user changes | Reauthentication |
| `os.lumio.system.power` | reboot, poweroff | Reauthentication |

1. "Active session" maps to polkit `allow_active`: an authenticated,
   active Lumio OS session is sufficient.
2. "Reauthentication" maps to polkit `auth_admin`: the user proves
   their password again for that action group. The session agent
   registers a polkit authentication agent for the user's session, so
   the prompt surfaces in the browser as an authorisation sheet.
3. A reauthentication is cached for 5 minutes within the same action
   group, then expires.
4. Action ids are per-group, never per-unit or per-path; granularity
   inside a group is enforced by argument validation and preconditions,
   not by multiplying polkit actions.

### Request flow

```text
Browser ──REST──▶ Gateway ──▶ Agent ──Unix socket──▶ Broker
                                                  │
                                                  ├─ peer creds (SO_PEERCRED) → real UID
                                                  ├─ schema-validate action + arguments
                                                  ├─ polkit CheckAuthorization(actionId, subject=UID)
                                                  ├─ on ALLOW: audit-begin → execute → verify → audit-end
                                                  └─ on DENY: 403 forbidden, audit record of the denial
```

1. The broker trusts nothing from the message about identity; the
   subject is the peer credential of the agent connection.
2. On polkit denial the broker returns `forbidden` with
   `details.actionId` and records the denial in the audit log.
3. If polkit itself is unavailable, the broker returns `unavailable`
   and executes nothing.

## Broker API

The typed action set for Phases 4–6, from BIG-PICTURE. Every action
accepts `requestId` and an optional `expected` precondition object;
every action is validated, idempotent, and audited.

### `services.start` / `services.stop` / `services.restart` / `services.reload` / `services.enable` / `services.disable`

```json
{
  "action": "services.restart",
  "arguments": { "unit": "nginx.service" },
  "expected": { "activeState": "active" }
}
```

`unit` must match the systemd unit-name grammar. Preconditions compare
live unit state via D-Bus immediately before the call.

### `packages.applyPlan` (Phase 5)

```json
{
  "action": "packages.applyPlan",
  "arguments": { "planId": "pln_01J…" },
  "expected": { "planId": "pln_01J…" }
}
```

The plan must exist, be unexpired, and have been computed by
`updates.plan` on this host. Applying a stale plan fails with
`conflict`.

### `files.writePrivileged` (Phase 5)

```json
{
  "action": "files.writePrivileged",
  "arguments": {
    "path": "/etc/nginx/nginx.conf",
    "contentBase64": "dXNlciB3d3ctZGF0YTsK…",
    "mode": "0644"
  },
  "expected": { "revision": "sha256:9f2c…" }
}
```

Path canonicalised and symlink-checked; revision precondition
mandatory; the write follows the temp-file/fsync/rename sequence and
keeps a rollback copy ([ERROR_AND_RECOVERY.md](ERROR_AND_RECOVERY.md)).

### `network.applyWithRollback` (Phase 6)

```json
{
  "action": "network.applyWithRollback",
  "arguments": {
    "config": { "version": 2, "ethernets": { } },
    "confirmTimeoutSec": 90
  }
}
```

Applies a Netplan configuration under the dead-man switch; a matching
`network.confirm` with the returned token commits, expiry reverts. The
config is a typed Netplan subset, not free-form YAML.

### `firewall.applyWithRollback` (Phase 6)

Same shape as `network.applyWithRollback`, with `arguments.rules`
describing the typed rule set. Same confirm-or-revert flow.

### `users.addSshKey` (Phase 6)

```json
{
  "action": "users.addSshKey",
  "arguments": {
    "user": "alice",
    "publicKey": "ssh-ed25519 AAAA…"
  }
}
```

Key format validated against sshd's accepted types; the target user's
`authorized_keys` is written atomically with correct ownership.

### `system.reboot` / `system.poweroff` (Phase 6)

```json
{
  "action": "system.reboot",
  "arguments": {},
  "expected": {}
}
```

Always requires reauthentication (`os.lumio.system.power`).

### Forbidden list

The broker must never grow any of:

```text
runRootCommand(string)
executeShell(string)
sudoAnything(args)
```

A request for an unknown action is a `validation_failed` error, not a
fallback.

## Audit

1. Privileged actions write to an append-only audit log (SQLite, per
   BIG-PICTURE). Records are never modified or deleted by the product;
   rotation and export are operator decisions, with a minimum of
   90 days of history kept by default.
2. Record shape: `timestamp`, `requestId`, `user` (uid and name),
   `action`, `argumentsHash` (SHA-256 of the canonical arguments —
   secrets are hashed, never stored), `polkitResult`, `outcome`,
   `error`, `durationMs`.
3. Two rows per action: an audit-begin row written before execution
   and an audit-end row written after completion. A begin row with no
   end row after a crash is the recovery evidence used in
   [ERROR_AND_RECOVERY.md](ERROR_AND_RECOVERY.md).
4. Polkit denials are recorded too — the audit log covers attempts,
   not only successes.
5. The authenticated user may read their own records through a
   read-only capability; reading all records is a root operation on
   the host, not a product API.

## Idempotency and reauthentication rules

1. `requestId` deduplication applies to broker actions exactly as in
   [PROTOCOL.md](PROTOCOL.md): same id within 24 h returns the stored
   outcome without re-executing.
2. An action that is already in the desired end state succeeds
   idempotently (enabling an enabled unit is a no-op success, not an
   error).
3. `expected` preconditions are evaluated after polkit authorisation
   and immediately before execution, inside the broker's per-target
   serialisation, so the check and the mutation cannot race.
4. Reauthentication (the 5-minute cache) never overrides idempotency:
   a replayed `requestId` returns the original result and does not
   prompt again.

## As built in Phase 4

Process map (one binary, four subcommands):

```text
lumiod gateway   (user lumio-gw)  HTTP/WS, cookies, CSRF, proxies to agents
lumiod sessiond  (root)           PAM auth, session store, spawns agents
lumiod agent     (real UID/GID)   Phase 2/3 capabilities per user
lumiod broker    (root)           typed actions, polkit, audit
```

Sockets:

| Path | Owner | Mode | Purpose |
|---|---|---|---|
| `/run/lumio/sessiond.sock` | root:lumio-gw | 0660 | login, logout, validate, reauth, session check |
| `/run/lumio/users/<uid>.sock` | uid:lumio-gw | 0660 | per-user agent API (created by sessiond, fd-inherited by the agent) |
| `/run/lumio/broker.sock` | root:root | 0666 | broker actions; authorisation via SO_PEERCRED + polkit, not file permissions |

`/run/lumio` itself is 0755 so agents can reach the broker; the
`users/` subdirectory stays 0750 root:lumio-gw so only the gateway (and
each agent's owner via the sessiond-created socket) can reach agents.

Notes on the as-built mapping to this document:

1. **polkit subject kind.** polkit (≥ 123) has no `unix-user` subject
   kind for `CheckAuthorization`; the broker uses `unix-process` with
   the agent's peer pid plus `start-time` from `/proc/<pid>/stat`,
   which resolves to the requesting uid inside polkit rules exactly as
   a uid subject would (`subject.user`).
2. **Reauthentication freshness.** The 5-minute reauthentication cache
   lives in sessiond. On `is_challenge` the broker calls sessiond's
   `/session/check` with the session token and proceeds only when the
   token's uid matches the peer credential and the window is fresh;
   the audit record stores `challenge+reauth` in that case.
3. **Audit record kinds.** `begin` and `end` rows per §Audit, plus a
   single `deny` row for polkit denials and failed reauthentication
   demands (nothing executes in either case; precondition conflicts
   produce no rows, matching the documented pipeline order).
4. **polkit details.** The broker passes the unit name as a
   `CheckAuthorization` detail key (`unit`), so site policy (`.rules`)
   may match per-unit behaviour via `action.lookup("unit")` without
   multiplying action ids — the Phase 4 testbed image uses this for
   its deterministic grants.
