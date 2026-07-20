# Lumio OS — Threat Model

Scope: the Lumio OS server stack as described in BIG-PICTURE, from the
browser down to the host system APIs. Protocol details referenced here
are defined in [PROTOCOL.md](PROTOCOL.md); privilege enforcement in
[PRIVILEGE_MODEL.md](PRIVILEGE_MODEL.md); failure handling in
[ERROR_AND_RECOVERY.md](ERROR_AND_RECOVERY.md).

## Components and trust boundaries

```text
┌──────────────────────────────────────────────────────────┐
│ Browser (untrusted network side)                         │
└─────────────────────────┬────────────────────────────────┘
                B1 │ HTTPS (REST + WebSocket)
┌──────────────────▼───────────────────────────────────────┐
│ Web gateway — dedicated unprivileged user (lumio-gw)     │
└──────┬───────────────────────────────┬───────────────────┘
  B2   │ Unix socket                   │ Unix socket
       │ SO_PEERCRED                   │ SO_PEERCRED
┌──────▼──────────────┐      ┌─────────▼──────────────┐
│ sessiond (root)     │      │ Session agent          │
│ PAM, launches       │      │ Runs as real UID/GID   │
│ per-user agents     │      │ PTY, files, read APIs  │
└─────────────────────┘      └─────────┬──────────────┘
                             B3        │ Unix socket, SO_PEERCRED
                              ┌────────▼──────────────┐
                              │ Privileged broker     │
                              │ Root, typed API only  │
                              │ polkit + audit        │
                              └────────┬──────────────┘
                             B4        │
                 ┌───────────┬─────────┼─────────┬─────────┐
                 │ systemd   │ journal │ files   │ apt/PK  │ Netplan
                 │ D-Bus     │ sd-jrnl │ POSIX   │         │
                 └───────────┴─────────┴─────────┴─────────┘
```

1. B1 is the only boundary crossed by untrusted input.
2. The gateway holds no user privileges; compromising it must not
   yield root or another user's data.
3. B2 and B3 are local Unix sockets authenticated by peer credentials
   (`SO_PEERCRED`), never by message fields.
4. B4 is where typed actions become real system changes; everything
   reaching it has already been schema-validated and authorised.

## Assets

1. User credentials presented to PAM — never stored, never logged.
2. Session cookies and CSRF tokens.
3. Audit log integrity — the record of who did what.
4. Host availability — the product must not brick the server it runs
   on.
5. Confidentiality and integrity of host data, including PTY content.

## Threats

Each entry: boundary — attack — mitigation — owning component. The
numbering matches the Phase 7 security-test list in BIG-PICTURE where
applicable.

1. **Command injection** (B3/B4). Arguments reach a shell or are
   concatenated into command lines. Mitigation: no shell anywhere in
   the stack; the broker builds argument vectors, never strings, and
   validates every argument against its schema (unit names, paths,
   enums). Owner: broker and agent input validation.
2. **Path traversal** (B2/B4). A `files.*` path escapes the intended
   target via `..` or encoded variants. Mitigation: paths are
   canonicalised and rejected if not absolute after resolution; the
   canonical path is what gets opened. Owner: session agent (and
   broker for `files.writePrivileged`).
3. **Symlink races** (B4). Target path is swapped for a symlink
   between check and open. Mitigation: open with `O_NOFOLLOW` /
   `openat2(RESOLVE_NO_SYMLINKS)` on writes, `fstat` after open and
   compare device+inode with the expected file, write to a temporary
   file and atomically rename. Owner: agent and broker.
4. **Stale file writes** (B4). The file changed between read and
   write; the write clobbers someone else's edit. Mitigation: every
   read returns a `revision` hash; writes carry `expected.revision`
   and fail with `stale_revision` on mismatch
   ([PROTOCOL.md](PROTOCOL.md) idempotency). Owner: agent and broker.
5. **CSRF** (B1). A third-party site triggers a mutating request
   riding the session cookie. Mitigation: `SameSite=Strict` cookie
   plus the `X-Lumio-CSRF` double-submit header required on every
   non-GET call. Owner: gateway.
6. **Cross-site WebSocket** (B1). A third-party page opens the WS from
   another origin. Mitigation: `Origin` allowlist check at upgrade,
   CSRF header required at upgrade, and `SameSite=Strict` means the
   cookie is not sent cross-site. Owner: gateway.
7. **Session fixation** (B1). An attacker plants a known session id.
   Mitigation: session ids are generated server-side at login, never
   accepted from the client, rotated on login and on any privilege
   change; idle and absolute expiry apply. Owner: sessiond and
   gateway.
8. **Privilege confusion** (B2/B3). A caller claims an identity or
   authorisation it does not have (forged uid field, confused deputy).
   Mitigation: the broker derives the subject from `SO_PEERCRED` on
   the agent's socket, never from message content; every action is
   re-authorised through polkit with that UID as subject; the agent
   cannot self-authorize. Owner: broker.
9. **Malformed protocol messages** (B1/B2/B3). Oversized, truncated,
   or schema-violating frames. Mitigation: strict schema validation at
   every hop, unknown fields rejected, frame and body size caps
   ([PROTOCOL.md](PROTOCOL.md) §Transport), socket closed on repeated
   violation. Owner: gateway, agent, broker.
10. **Package-manager lock conflicts** (B4). Two operations contend
    for the apt/dpkg lock. Mitigation: a single package worker
    serialises operations; contention returns `busy` with
    `details.retryAfterMs` ([ERROR_AND_RECOVERY.md](ERROR_AND_RECOVERY.md)
    concurrency rules). Owner: broker's package worker.
11. **Disconnect during mutation** (B1). The client vanishes
    mid-action. Mitigation: mutations run to completion server-side;
    the result is retrievable by `requestId` for 24 h; idempotency
    keys make client retry safe. Owner: gateway and broker.
12. **Reboot during mutation** (B4). The host restarts mid-action.
    Mitigation: the audit-begin record is persisted before execution;
    on next boot the broker reconciles pending records against live
    state and marks the outcome. Owner: broker (audit + recovery).
13. **Concurrent tabs issuing conflicting actions** (B1). Two tabs
    race the same target. Mitigation: expected-state preconditions,
    per-target serialisation in the broker, second actor receives
    `conflict` or `busy`; true duplicates collapse via `requestId`.
    Owner: broker.

Additional baseline threats:

14. **Credential brute force** (B1). Mitigation: PAM enforces its own
    delays; the gateway adds per-account and per-source rate limiting
    on login. Owner: gateway and sessiond.
15. **XSS / asset injection** (B1). Mitigation: no server-supplied
    application JavaScript ever; strict Content Security Policy ships
    with Phase 7 packaging. Owner: gateway and frontend.
16. **Denial of service** (B1). Mitigation: per-session channel caps,
    frame size caps, ping/pong liveness, idle session expiry; the
    broker keeps a small, bounded job queue. Owner: gateway and
    broker.

## Non-goals

1. No generic root shell or arbitrary privileged command endpoint —
   the broker API is the typed set in
   [PRIVILEGE_MODEL.md](PRIVILEGE_MODEL.md), nothing else.
2. No server-supplied application JavaScript; the browser runs only
   its shipped bundle.
3. One server per session; no multi-host control plane, so no
   cross-host credential or code propagation.
4. Phase 2 binds to localhost only; the service is not internet-facing
   until the authentication phase is complete.
