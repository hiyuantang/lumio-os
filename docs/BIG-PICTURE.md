# My view

Build an **independent, clean-room, behavioral reimplementation of a Linux server-management desktop**, using Cockpit only as a product benchmark and research reference.

https://github.com/cockpit-project/cockpit

“Never copy and paste” is not enough. An AI agent that reads Cockpit’s source and then implements the replacement in the same context can unintentionally reproduce distinctive structures, names, tests, protocol details, or UI expression. A better process separates:

1. a **reference analyst** that studies Cockpit;
2. a written, code-free behavioral specification;
3. an **implementation agent** that never sees the Cockpit repository.

Cockpit’s repository contains components under several licenses, including LGPL-2.1+, GPL-3.0+, BSD-3-Clause, CC-BY-SA, and MIT. Its main project is generally described as LGPL-2.1+, but individual components differ.  The general distinction you are aiming for is reasonable: methods and systems can inspire an independent implementation, while source code and other original expression remain protected. That is a general copyright principle, not a legal determination for your particular product.

The best description of the product is:

> **A locally rendered, macOS-inspired web desktop for administering a real headless Ubuntu server.**

It should feel like an operating system, but it should not pretend that the browser is actually running GNOME or macOS.

---

# What the application would feel like

## The desktop

After login, the user sees a calm desktop rather than a dashboard:

```text
┌──────────────────────────────────────────────────────────────┐
│ server-name   Services   File   View        CPU  Net  🔔  👤 │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│       ┌──────────────── Services ───────────────────┐         │
│       │ Search services                             │         │
│       │ ● nginx.service              Running        │         │
│       │ ● postgresql.service         Running        │         │
│       │ ○ backup.service             Inactive       │         │
│       │                                             │         │
│       │ [Logs] [Configuration] [Restart]            │         │
│       └─────────────────────────────────────────────┘         │
│                                                              │
│      ┌──────────────────────────────────────────────────┐    │
│      │ Home Files Terminal Services Logs Updates ...   │    │
│      └──────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

The main shell would contain:

- a persistent top menu bar;
- a dock showing pinned and running applications;
- movable, resizable and minimizable windows;
- a command center similar in spirit to Spotlight;
- a notification center;
- keyboard shortcuts;
- saved window positions per user;
- light, dark and reduced-motion modes;
- a single-window mobile layout when the viewport is narrow.

The macOS feeling should come from **calm interaction, hierarchy, consistency and animation**, not from copying Apple assets pixel-for-pixel.

Use your own:

- product name and logo;
- wallpaper and color system;
- icon set;
- typography;
- window controls;
- sound and animation language;
- application names where appropriate.

For example, use an open font such as Inter or Geist rather than copying Apple’s typography, and create subtly different left-aligned window controls rather than reproducing macOS exactly.

## The core applications

| Application | What it feels like | What it controls |
|---|---|---|
| **Home** | A restrained system overview | Health, uptime, CPU, memory, storage, updates and alerts |
| **Files** | Finder-like list and column views | Real filesystem under the logged-in user’s permissions |
| **Terminal** | A native-feeling terminal with tabs and splits | A real PTY running as the logged-in Linux user |
| **Services** | A live application/process manager | systemd units, dependencies, startup and restart operations |
| **Logs** | Console-style searchable event stream | journald with live filters and saved searches |
| **Updates** | Software Update-style workflow | Package refresh, upgrade plan, installation and reboot status |
| **Storage** | Disk Utility-inspired view | Disks, partitions, mounts, filesystems and SMART status |
| **Network** | Visual network configuration | Interfaces, addresses, DNS, routes, listeners and firewall |
| **Containers** | Container workspace | Docker or Podman containers, images, logs and Compose projects |
| **Settings** | System Settings-inspired navigation | Users, SSH keys, security, TLS, locale, time and product settings |

## Cross-application behavior

This is where it can feel more like a real operating system than a normal server panel.

From a service window, the user could:

- open its logs in a Logs window;
- reveal its unit file in Files;
- open a terminal already scoped to relevant commands;
- see listening ports and dependent services;
- restart it with an authorization sheet;
- receive a notification when the operation finishes.

From Files, pressing Space could open a Quick Look-style preview. Editing a privileged configuration file could show:

1. the current file;
2. the proposed diff;
3. a syntax-validation result;
4. affected services;
5. the rollback copy;
6. an authorization confirmation.

The command center could understand:

```text
Open nginx logs
Restart PostgreSQL
Show files larger than 1 GB
Find failed services
Install pending security updates
Open /etc/ssh/sshd_config
```

Initially those should map to deterministic product actions, not to an unrestricted AI-generated root shell command.

---

# What to borrow from Cockpit

Cockpit’s most valuable ideas are not its visual components. They are its operating principles:

1. **The real server is the source of truth.**
2. **Changes made through SSH must immediately appear in the GUI.**
3. **Users retain their normal Linux permissions.**
4. **Privilege elevation uses existing Linux mechanisms.**
5. **The application should have little or no idle server footprint.**
6. **The UI should use real system APIs rather than maintaining a competing configuration database.**

Those principles are explicitly part of Cockpit’s project ideals. Cockpit says that it reacts to the actual system state, does not store its own opinion of server state, and uses existing privilege mechanisms such as polkit or sudo.

Cockpit’s documented architecture also gives you a useful pattern: a web service authenticates the user, starts a per-user bridge, and that bridge relays access to system APIs, D-Bus and processes.

Borrow the **pattern**, but design your own protocol and implementation.

Do not build your new product on undocumented Cockpit internals. Cockpit states that its JavaScript APIs are for Cockpit packages and that it does not expose a supported general-purpose external HTTP or REST API.

---

# Recommended architecture

```text
┌────────────────────────────────────────────────────────────┐
│ Browser                                                    │
│                                                            │
│ React/TypeScript web desktop                               │
│ Window manager • Dock • Menu bar • Apps • Notifications   │
└──────────────────────────┬─────────────────────────────────┘
                           │ HTTPS
                           │ REST for requests
                           │ WebSocket for events/streams
┌──────────────────────────▼─────────────────────────────────┐
│ Web gateway — unprivileged dedicated Linux user           │
│                                                            │
│ Static assets • Session cookies • CSRF • WebSocket routing │
└──────────────┬───────────────────────────────┬─────────────┘
               │ local Unix socket             │
┌──────────────▼─────────────┐      ┌──────────▼─────────────┐
│ Authentication/sessiond   │      │ Per-user session agent │
│ Small root-owned service  │      │ Runs as actual UID/GID │
│ PAM and worker launching  │      │ PTY, files, read APIs  │
└────────────────────────────┘      └──────────┬─────────────┘
                                               │
                                      typed privileged request
                                               │
                                    ┌──────────▼─────────────┐
                                    │ Privileged broker      │
                                    │ Root, tiny API surface │
                                    │ polkit + audit         │
                                    └──────────┬─────────────┘
                                               │
          ┌──────────────┬─────────────┬────────┼─────────────┐
          │              │             │        │             │
       systemd        journald       files    packages     network
       D-Bus          libsystemd     POSIX     apt/PK       Netplan
```

## Why this separation matters

The process accepting traffic from the internet should not also be an unrestricted root process.

The privileged broker should expose operations such as:

```text
services.restart
services.enable
packages.applyPlan
files.writePrivileged
network.applyWithRollback
firewall.applyWithRollback
users.addSshKey
system.reboot
```

It should **not** expose:

```text
runRootCommand(string)
executeShell(string)
sudoAnything(args)
```

Polkit is designed around this division: unprivileged subjects request defined actions from privileged mechanisms, which then perform authorization.

## Technology choices

I would use:

### Browser

- TypeScript;
- React with Vite rather than a server-rendered framework;
- a custom desktop/window-state layer;
- CSS variables for design tokens;
- xterm.js for the terminal;
- a lightweight chart library for live metrics;
- IndexedDB for cached UI preferences;
- a service worker for static asset caching;
- Playwright for browser testing.

The browser should render all shell interactions locally. Moving a window, opening a menu or switching applications should require no round trip to the VPS.

### Server

- Go for the gateway, session worker and privileged broker;
- Unix domain sockets for communication among local processes;
- peer credential validation on those sockets;
- JSON Schema-generated Go and TypeScript protocol types;
- systemd socket/service activation;
- SQLite only for audit records and user workspace preferences;
- no database copy of system services, users, mounts or network state.

Go is a pragmatic fit here: it is memory-safe, produces deployable binaries, handles streaming connections well and is comparatively easy for an AI coding agent to implement and audit.

### System integration

Prefer stable machine interfaces:

- systemd D-Bus for services;
- libsystemd journal APIs for logs;
- `/proc`, `/sys` and cgroup interfaces for metrics;
- PTYs for terminals;
- filesystem system calls for Files;
- PackageKit or a narrowly scoped apt helper for packages;
- Netplan for Ubuntu network configuration;
- UDisks2 and machine-readable `lsblk` data for storage;
- Docker and Podman APIs for containers.

Systemd explicitly treats its documented D-Bus interfaces as stable, while warning that human-oriented command output such as `systemctl status` is not generally a stable parsing interface.

For Ubuntu 26.04 LTS, the relevant base includes systemd 259 and Netplan 1.2. I would make Ubuntu 26.04 the primary target and retain automated compatibility testing for Ubuntu 24.04 LTS, which remains security-maintained through May 2029.

---

# The clean-room development process

## Do not use one agent for both stages

The safest setup is:

```text
Cockpit repository
       │
       ▼
Reference analyst
       │
       │ behavioral documents only
       ▼
Independent specification repository
       │
       ▼
Implementation agent
       │
       ▼
Your original repository
```

## Reference workspace

Cockpit may be cloned into a separate, read-only workspace accessible only to the reference analyst.

The analyst may produce:

- user-visible behavior descriptions;
- architecture diagrams;
- state machines;
- lists of Linux APIs involved;
- security observations;
- error and recovery cases;
- black-box test scenarios;
- feature comparison tables.

It must not produce:

- copied code;
- translated code;
- source excerpts;
- exact internal class or function structures;
- copied tests;
- copied comments or strings;
- copied CSS values;
- copied SVGs or assets;
- reconstructed DOM trees;
- undocumented protocol frames;
- instructions such as “implement it exactly as file X does.”

## Implementation workspace

The builder receives only:

- your product requirements;
- original UI designs;
- behavioral specifications;
- official Ubuntu, systemd, D-Bus, polkit and browser documentation;
- acceptance tests written in product language.

The implementation environment should deny access to:

```text
github.com/cockpit-project/*
raw.githubusercontent.com/cockpit-project/*
local paths containing the Cockpit clone
```

Keep the two repositories under different users or isolated containers. Do not mount the Cockpit directory into the implementation environment.

## Provenance controls

Create a `CLEAN_ROOM.md` containing the rules, and require every feature pull request to include:

```text
Reference inputs:
- SPEC-SERVICES-002
- systemd D-Bus documentation
- Product design mockup DS-014

Original implementation:
- No external source code copied
- No Cockpit source viewed by implementation agent
- New tests authored from acceptance criteria
```

Run these checks in CI:

- dependency license inventory;
- software bill of materials generation;
- secret scanning;
- duplicated-code scanning;
- token or fingerprint similarity checks against the reference repository;
- verification that no Cockpit assets or source files exist in the repository;
- source-header and third-party attribution validation.

This is not a guarantee against every intellectual-property issue, but it gives you a much stronger engineering provenance record than “the model promised not to copy.”

---

# Build plan

Use milestones with exit criteria rather than trying to build full Cockpit parity.

## Phase 0 — Product and security foundation

Create these documents before production code:

```text
PRODUCT.md
DESIGN_PRINCIPLES.md
CLEAN_ROOM.md
SUPPORTED_SYSTEMS.md
THREAT_MODEL.md
PROTOCOL.md
PRIVILEGE_MODEL.md
ERROR_AND_RECOVERY.md
```

Decide:

- Ubuntu 26.04 as primary;
- Ubuntu 24.04 as compatibility target;
- amd64 and arm64;
- one server per session;
- no arbitrary native GUI streaming in the first release;
- no built-in root AI agent;
- no multi-server control plane yet.

Cockpit’s current documentation warns that its older multi-host approach can load code from several hosts in the same browser context, and that feature is deprecated. Your product should never load server-supplied application JavaScript from remote managed hosts.

**Exit gate:** an approved threat model, protocol draft and clean-room policy.

---

## Phase 1 — Desktop shell with simulated data

Build the frontend against a mock server.

Deliver:

- login screen;
- top menu bar;
- dock;
- draggable and resizable windows;
- minimize, maximize and close;
- window layering and focus;
- keyboard navigation;
- command center;
- notification center;
- light and dark themes;
- reduced motion;
- persistent workspace layout;
- responsive single-window mode.

Build mock versions of Home, Services, Files, Terminal and Logs.

**Exit gate:** the product feels coherent without any real Ubuntu connection. All key operations are keyboard accessible.

---

## Phase 2 — Read-only Ubuntu agent

Build the unprivileged local agent first.

Deliver:

- host identity and OS information;
- CPU, memory, disk and network metrics;
- systemd service listing and status;
- journal queries and live journal subscription;
- filesystem listing under the agent’s user;
- health and update-availability indicators.

Initially bind only to localhost and access it through an SSH tunnel. That allows development without prematurely exposing an unfinished administrative service to the internet.

**Exit gate:** running `systemctl` or changing files through SSH causes the browser to update without refreshing the page.

---

## Phase 3 — Terminal and Files

Deliver a real per-user session:

- PTY terminal;
- multiple tabs;
- reconnect behavior;
- terminal resize;
- upload and download;
- Finder-like file browser;
- Quick Look;
- text editor;
- atomic file saves;
- file revision checking;
- symlink and path traversal protection;
- trash or recoverable deletion for eligible files.

File writes should work like this:

```text
1. Read file and revision/hash
2. User edits
3. Submit new content with expected revision
4. Server verifies the file has not changed
5. Write to a temporary file
6. fsync
7. Preserve mode and ownership
8. Atomically rename
9. Return the new revision
```

**Exit gate:** the terminal runs under the authenticated Linux UID, and Files cannot escape that user’s permissions.

---

## Phase 4 — Authentication and privilege broker

Deliver:

- PAM-based local-user authentication;
- secure HttpOnly session cookies;
- CSRF protection;
- session expiry;
- per-user worker processes;
- root-owned privileged broker;
- polkit actions;
- reauthentication for high-risk actions;
- append-only audit events;
- request IDs and idempotency keys.

The privileged API should use narrow action schemas.

Example:

```json
{
  "requestId": "6c4e...",
  "action": "services.restart",
  "arguments": {
    "unit": "nginx.service"
  },
  "expected": {
    "activeState": "active"
  }
}
```

**Exit gate:** there is no generic privileged command endpoint, and every privileged action has authorization, input validation, an audit record and a negative test.

---

## Phase 5 — First complete system applications

Build these in order:

### Services

- list and search units;
- state subscriptions;
- start, stop, restart and reload;
- enable and disable;
- view dependencies;
- open related logs;
- show unit files and overrides.

### Logs

- live journal stream;
- priority, unit, boot and time filters;
- structured field inspector;
- saved searches;
- export;
- jump from a log line to the responsible service.

### Updates

- refresh package metadata;
- calculate an upgrade plan;
- separate security updates;
- show package and size changes;
- apply the saved plan;
- stream progress;
- indicate reboot requirements.

Cockpit uses PackageKit’s D-Bus API for this area, which is a reasonable first reference for your abstraction even if you later implement an Ubuntu-specific apt worker.

### Privileged Files

- request elevation for a particular file;
- show the proposed diff;
- validate known configuration formats;
- keep a rollback copy;
- optionally restart an affected service.

**Exit gate:** a user can diagnose and repair a failed web service entirely through the new interface.

---

## Phase 6 — Risky operations with rollback

Add:

- Netplan network editing;
- DNS and route management;
- firewall rules;
- users and SSH keys;
- mount management;
- disk inspection;
- hostname, timezone and locale;
- reboot and shutdown.

Network changes need a dead-man switch:

```text
Apply candidate configuration
        ↓
Begin rollback timer
        ↓
Client reconnects successfully
        ↓
User confirms "Keep changes"
        ↓
Commit

No successful confirmation
        ↓
Automatically restore prior configuration
```

The same concept should apply to firewall changes.

**Exit gate:** intentionally breaking the network configuration results in automatic recovery without requiring console access.

---

## Phase 7 — Packaging and hardening

Deliver:

- `.deb` packages;
- signed package repository;
- systemd service and socket units;
- AppArmor profiles;
- Content Security Policy;
- configurable TLS certificates;
- safe reverse-proxy documentation;
- upgrade and uninstall paths;
- backup and restoration of product preferences;
- dependency SBOM;
- reproducible build process;
- automated Ubuntu 24.04 and 26.04 VM tests;
- amd64 and arm64 builds.

Test on a modest VPS profile rather than only on a development workstation.

Security testing must include:

- command injection;
- path traversal;
- symlink races;
- stale file writes;
- CSRF;
- cross-site WebSocket use;
- session fixation;
- privilege confusion;
- malformed protocol messages;
- package-manager lock conflicts;
- disconnect during mutation;
- reboot during mutation;
- concurrent tabs issuing conflicting actions.

**Exit gate:** installation on a clean Ubuntu Server machine works from package installation through first login, without manual source-code setup.

---

## Phase 8 — Containers and advanced workflows

After the core host-management product is trustworthy, add:

- Docker and Podman;
- Compose projects;
- image updates;
- container terminals;
- port and volume inspection;
- container resource limits;
- scheduled tasks;
- backup jobs;
- certificate management;
- application-specific plugins.

Keep plugins isolated. A plugin should declare the capabilities it needs and should not automatically inherit a root-capable browser channel.

---

## Phase 9 — Optional native application windows

Only after the web-native system is mature, add selective GUI streaming.

A user could launch something such as a graphical database tool in an isolated session:

```text
Browser desktop
   └── Native App window
          └── Isolated Xpra session
                 └── One Linux GUI application
```

This should be an escape hatch, not the foundation. The browser shell, Files, Terminal, Services, Logs and Settings should remain locally rendered.

---

# Definition of the first useful release

The first meaningful release should let a user:

1. install one Ubuntu package;
2. connect securely;
3. log in as a real Linux user;
4. see a macOS-inspired desktop;
5. inspect host health;
6. browse permitted files;
7. use a real terminal;
8. inspect and control systemd services;
9. search and stream journal logs;
10. plan and apply updates;
11. elevate only for defined actions;
12. recover after reconnecting;
13. see an audit trail;
14. observe changes made through SSH;
15. uninstall the product without damaging the server.

It does **not** need initial support for:

- multiple hosts in one browser session;
- a plugin marketplace;
- every Cockpit feature;
- arbitrary graphical desktop applications;
- Kubernetes;
- complete storage provisioning;
- an autonomous AI administrator;
- an exact macOS visual replica.

---

# Master directive for the implementation agent

You can place this at the root of the new repository as `AGENTS.md`:

```text
PROJECT MISSION

Build an independent, web-native desktop for administering headless Ubuntu
servers. The browser renders the desktop, applications, windows, menus and
animations locally. The server exposes typed system capabilities and streams
state changes.

The product must feel calm and desktop-native, inspired by macOS interaction
principles, but must use original branding, visual assets, typography,
components, layouts and implementation.

CLEAN-ROOM RULES

1. This is an independent implementation.
2. Do not clone, open, search, fetch or inspect the Cockpit source repository.
3. Do not reproduce Cockpit source code, tests, CSS, assets, DOM structure,
   comments, strings, internal identifiers or undocumented protocols.
4. Do not translate Cockpit code into another language.
5. Do not use generated code that was derived from Cockpit source.
6. Public documentation may be used to understand high-level behavior.
7. Official Ubuntu, systemd, D-Bus, polkit, POSIX and browser documentation
   may be used as technical references.
8. Implement from the product specifications and acceptance tests in this
   repository.
9. Record the reference specification and original design rationale in each
   pull request.
10. Stop and report any accidental exposure to Cockpit source.

SYSTEM PRINCIPLES

1. The live Ubuntu system is the source of truth.
2. Do not maintain a duplicate desired-state database for the host.
3. Reflect changes made through SSH or other tools.
4. Run ordinary operations as the authenticated Linux user.
5. Use a small, typed privileged broker for elevated actions.
6. Never expose a generic root shell or arbitrary privileged command API.
7. Prefer stable machine APIs such as D-Bus over parsing human-readable CLI
   output.
8. Make risky operations transactional and reversible.
9. Every privileged mutation must be authenticated, authorized, validated,
   idempotent where possible and audited.
10. Keep idle server resource usage low.

IMPLEMENTATION WORKFLOW

For every feature:

1. Write or update the behavioral specification.
2. Define observable acceptance criteria.
3. Add a brief threat analysis.
4. Define request, result and event schemas.
5. Write failing unit and integration tests.
6. Implement the smallest complete vertical slice.
7. Run formatting, static analysis, tests and security checks.
8. Test on an Ubuntu VM, not only through mocks.
9. Verify external system changes are reflected in the UI.
10. Update the architecture decision record and provenance declaration.

Do not declare a feature complete merely because its happy path works.
Test permission denial, stale state, disconnection, concurrency, malformed
input and rollback behavior.
```

For the separate reference analyst:

```text
You are a behavioral research analyst. You may inspect Cockpit in an isolated,
read-only environment.

Your output may contain:
- user-facing behavior;
- abstract component responsibilities;
- sequence and state diagrams;
- underlying public Linux APIs;
- security and failure observations;
- acceptance tests expressed in original language.

Your output must not contain:
- source code or pseudocode derived line-by-line from source;
- code excerpts;
- copied tests;
- copied CSS or assets;
- exact internal function, class or variable structures;
- copied comments or user-interface strings;
- undocumented wire messages;
- implementation instructions tied to source files.

Describe what the system accomplishes and the observable constraints. Do not
describe how to recreate Cockpit's particular code.
```

# Final recommendation

Build it from scratch **only because the differentiated product is the desktop experience and system workflow**, not because rewriting Cockpit is intrinsically valuable.

The right strategy is:

> **Cockpit as a benchmark and behavioral oracle; Ubuntu’s stable system interfaces as the actual foundation; your own macOS-inspired shell and interaction model as the product.**

Start with the desktop shell, Services, Logs, Terminal, Files and Updates. Delay storage mutation, networking, containers, multi-host management and graphical application streaming until the authentication, permission and rollback model has proven trustworthy.