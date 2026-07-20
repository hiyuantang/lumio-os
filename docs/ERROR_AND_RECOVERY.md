# Lumio OS — Error and Recovery

Failure semantics for the wire protocol and the privileged stack.
Error codes and envelope shapes are defined in
[PROTOCOL.md](PROTOCOL.md); ownership of mitigations in
[THREAT_MODEL.md](THREAT_MODEL.md); the mutation lifecycle in
[PRIVILEGE_MODEL.md](PRIVILEGE_MODEL.md).

## Error taxonomy and retry policy

| Code | Class | Client behaviour |
|---|---|---|
| `unauthorized` | Fail-fast | Re-authenticate; never retry blindly |
| `forbidden` | Fail-fast | Surface the denial; retry only after the user changes something |
| `not_found` | Fail-fast | Refresh the listing; the object is gone |
| `validation_failed` | Fail-fast | Client bug or bad user input; fix and resubmit |
| `conflict` | Precondition | Refresh live state, show the user, let them re-issue |
| `stale_revision` | Precondition | Re-read the object, merge, retry with the new revision |
| `busy` | Retry | Wait `details.retryAfterMs`, then retry; cap at 5 attempts |
| `unavailable` | Retry | Back off (500 ms, doubling, max 8 s); safe because of idempotency keys |
| `internal` | Retry once | One retry with the same `requestId`, then fail loudly |

Rules:

1. Mutations are only ever retried with the original `requestId`;
   server-side deduplication makes this safe
   ([PROTOCOL.md](PROTOCOL.md) §Idempotency).
2. Queueing is reserved for the package worker: a second package
   operation while one runs is queued server-side, and a contended
   external lock surfaces as `busy`, not as a hung request.
3. Everything else fails fast rather than piling retries onto a
   broken precondition.

## Disconnect semantics per stream

| Stream | On socket drop |
|---|---|
| `system.metrics` | Samples are dropped, never buffered; the client resubscribes fresh |
| `services.subscribe` | Client resubscribes and receives a new snapshot |
| `journal.stream` | Client resubscribes with `after` = last cursor; at-least-once delivery, dedup by cursor |
| `terminal` (PTY) | Server keeps the PTY alive for 120 s; resubscribing `terminal.open` with the same session token reattaches and replays the scrollback tail; after 120 s the PTY is killed |
| `updates.progress` | Client rebinds to the channel by `requestId` |
| Mutations | Continue server-side to completion; the outcome is retrievable by `requestId` for 24 h |

A client that missed events detects gaps by per-channel `seq` numbers
and resubscribes rather than assuming continuity.

## Transactional mutation pattern

Every privileged mutation follows the same pipeline:

```text
validate ─▶ authorise (polkit) ─▶ check preconditions ─▶ audit-begin
      ─▶ execute ─▶ verify end state ─▶ audit-end
```

1. **Validate** — schema and argument checks; failure →
   `validation_failed`.
2. **Authorise** — polkit; failure → `forbidden`, still audited.
3. **Preconditions** — `expected` compared against live state under
   per-target serialisation; failure → `conflict` / `stale_revision`.
4. **Audit-begin** — persisted before any system call.
5. **Execute** — the real system call.
6. **Verify** — live state re-read and compared to the intended end
   state; mismatch marks the outcome `failed` even if the call
   returned success.
7. **Audit-end** — outcome, error, duration.

### Per-operation rollback notes

- **`files.writePrivileged`** — before writing, the current file is
  copied to a rollback location; the write itself is temp-file, fsync,
  atomic rename with preserved mode and ownership. The rollback copy
  is restorable through the same action with the old revision as
  content.
- **`packages.applyPlan`** — **not rollbackable.** apt/dpkg has no
  transactional undo; downgrades are their own risky operation. This
  limitation is shown in the UI before apply. The mitigation is the
  plan itself: the user reviews exact package and size changes first,
  and progress streams until completion or a concrete error.
- **`network.applyWithRollback` / `firewall.applyWithRollback`** —
  the dead-man switch from BIG-PICTURE, with concrete timers:

```text
Apply candidate configuration
        ↓
Begin rollback timer (90 s)
        ↓
Client reconnects successfully
        ↓
User confirms "Keep changes"   ──▶  Commit (prior config discarded)

No successful confirmation within 90 s
        ↓
Automatically restore prior configuration
```

  The timer runs in the broker, not the browser, so a dead client or
  a severed tunnel cannot prevent the revert. `confirmTimeoutSec` may
  be set between 30 and 300 s; the default is 90 s. Only one pending
  network or firewall change exists at a time; a second apply while
  one is pending returns `busy`.

## Crash recovery

1. **Agent crash** — systemd restarts the user's agent. In-flight
   requests fail with `unavailable`; the client retries with the same
   `requestId`. PTYs owned by the dead agent enter their 120-second
   grace via the broker, which holds them.
2. **Broker crash** — systemd restarts the broker. Because audit-begin
   is written before execution, a begin row with no end row marks an
   interrupted action. On startup the broker reconciles each pending
   row against live state and closes it with the observed outcome.
3. **Gateway crash** — systemd restarts the gateway. Sessions and
   channels are re-established by the client; streams resume per the
   table above.
4. **Host reboot during mutation** — the audit log is the recovery
   source of truth. On next boot the broker performs the same
   pending-row reconciliation, and clients verify expected state
   rather than assuming the mutation landed. The UI surfaces
   interrupted actions from the audit trail.
5. **Pending network dead-man switch across reboot** — a pending
   candidate configuration is not persisted as active; on boot the
   last committed configuration applies, which is the revert path.

## Concurrency rules

1. **Concurrent tabs** — all tabs share one session and one socket
   model; nothing prevents two tabs issuing actions, so the broker
   serialises per target (per unit, per file, per plan). The second
   action on the same target waits or receives `conflict` if its
   preconditions no longer hold.
2. **Package-manager lock contention** — one package worker owns the
   apt/dpkg lock. Contention, including against an operator running
   apt over SSH, returns `busy` with `details.retryAfterMs`
   (default hint 5000 ms); the client retries after the delay.
3. **Unit state changed mid-action** — if `systemctl` over SSH flips a
   unit between precondition check and execution, the serialised
   recheck fails with `conflict`; the client refreshes from a
   `services.subscribe` snapshot and shows the new state.
4. **Idempotency under races** — genuine duplicate submissions (same
   `requestId`) collapse to one execution regardless of timing;
   distinct `requestId`s with stale preconditions fail, they never
   silently merge.
