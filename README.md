# Lumio OS

A locally rendered, macOS-inspired web desktop for administering a real
headless Ubuntu server.

Lumio OS runs in your browser but manages the actual machine: real files,
real systemd services, real journal logs, real users and permissions. It
feels like an operating system — a menu bar, a dock, movable windows, a
command center — but it never pretends the browser is running GNOME or
macOS, and it never hides the server behind a duplicate configuration
database.

## What it is

- **A web desktop, not a dashboard.** After logging in you get a calm
  desktop with a menu bar, dock, windows, notifications and keyboard
  shortcuts. All shell interaction renders locally in the browser; moving
  a window or opening a menu never touches the network.
- **The live system is the source of truth.** Changes made over SSH or
  with other tools show up in the desktop immediately. There is no
  competing desired-state database.
- **Your normal Linux permissions.** You log in as a real Linux user and
  ordinary operations run as that user. Elevated actions go through a
  small, typed privileged broker (polkit + audit) — never a generic root
  shell.

### Core applications

| Application | What it controls |
|---|---|
| Home | Health, uptime, CPU, memory, storage, updates and alerts |
| Files | The real filesystem under your user's permissions |
| Terminal | A real PTY running as your Linux user |
| Services | systemd units, dependencies, start/stop/restart |
| Logs | journald with live filters and saved searches |
| Updates | Package refresh, upgrade plan, installation, reboot status |
| Storage | Disks, partitions, mounts, filesystems, SMART status |
| Network | Interfaces, addresses, DNS, routes, listeners, firewall |
| Containers | Docker/Podman containers, images, logs, Compose projects |
| Settings | Users, SSH keys, security, TLS, locale, time |

### Architecture at a glance

```text
Browser (React/TypeScript desktop, rendered locally)
        │  HTTPS — REST for requests, WebSocket for events
Web gateway (unprivileged)
        │  local Unix sockets
Per-user session agent (runs as your UID/GID)
        │  typed privileged requests
Privileged broker (root, tiny API surface, polkit + audit)
        │
systemd · journald · files · packages · network
```

The browser talks to an unprivileged gateway; a per-user agent performs
ordinary work as the authenticated user; a tiny root-owned broker handles
narrowly defined privileged actions (`services.restart`,
`packages.applyPlan`, …) with authorization, validation and audit. Risky
operations such as network or firewall changes are transactional with
automatic rollback.

### Supported systems

- **Primary:** Ubuntu 26.04 LTS (amd64 and arm64)
- **Compatibility:** Ubuntu 24.04 LTS
- One server per browser session; no multi-host control plane.

## How to use it

> **Status: Phase 1.** The desktop shell runs locally against mock data
> — no real server connection and no installable package yet. The
> numbered flow below describes the intended experience once the first
> release ships.

Run the Phase 1 preview:

```sh
npm install
npx playwright install chromium   # one-time, for tests
npm run dev                       # then open the printed localhost URL
```

Any non-empty username and password logs you into the mock desktop.
Useful shortcuts: `⌘/Ctrl+K` command center, `Alt+W` close window,
`Ctrl+Alt+←/→` cycle windows. `npm test` runs the Playwright smoke
tests; `npm run build` typechecks and builds.

The target experience for the first release:

1. **Install** one Ubuntu package on your server.
2. **Connect** securely over HTTPS and log in as a real Linux user.
3. **Land on the desktop** — check host health on Home, browse files,
   open a terminal, inspect and control services, stream logs.
4. **Elevate only when needed** — privileged actions ask for
   authorization and are recorded in an audit trail.
5. **Uninstall cleanly** at any time without damaging the server.

### Contributing / development

Development is specification-driven; the product, design and security
specifications are maintained alongside the code. See
[AGENTS.md](AGENTS.md) for the project mission and working rules.

This is an independent, clean-room implementation. Do not clone or
inspect third-party server-management projects for implementation
reference; build from the project's specifications and official Ubuntu,
systemd, D-Bus, polkit and web-standards documentation.

## License

Lumio OS is licensed under the GNU Affero General Public License,
version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE).

In SPDX terms, the project uses `AGPL-3.0-only` — **not**
`AGPL-3.0-or-later`. The "or any later version" text at the bottom of
the [LICENSE](LICENSE) file is part of the FSF's *How to Apply These
Terms* appendix (a sample notice) and does not, by itself, make this
project "or later". This README and the per-file SPDX headers are the
authoritative statement of the chosen license.
