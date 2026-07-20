# Lumio OS — Product

## Definition

*A locally rendered, macOS-inspired web desktop for administering a real headless Ubuntu server.*

It feels like an operating system, but it never pretends that the browser
is running GNOME or macOS.

## Target user

A developer or small-team operator running one or a few headless Ubuntu
servers (VPS or homelab) who wants GUI clarity without giving up SSH or
normal Linux permissions.

## Operating principles

1. The real server is the source of truth.
2. Changes made through SSH must immediately appear in the GUI.
3. Users retain their normal Linux permissions.
4. Privilege elevation uses existing Linux mechanisms.
5. The application has little or no idle server footprint.
6. The UI uses real system APIs rather than maintaining a competing
   configuration database.

## Core applications

| Application | Scope | Status |
|---|---|---|
| Home | Health, uptime, CPU, memory, storage, updates and alerts | Phase 2 complete |
| Files | Real filesystem plus protected configuration repair | Phase 5 complete |
| Terminal | A real PTY running as the logged-in Linux user | Phase 3 complete |
| Services | systemd units, dependencies, startup and restart operations | Phase 5 complete |
| Logs | journald with live filters and saved searches | Phase 5 complete |
| Updates | Package refresh, upgrade plan, installation and reboot status | Phase 5 complete |
| Storage | Disks, partitions, mounts, filesystems and SMART status | planned |
| Network | Interfaces, addresses, DNS, routes, listeners and firewall | planned |
| Containers | Docker or Podman containers, images, logs and Compose projects | planned |
| Settings | Users, SSH keys, security, TLS, locale, time and product settings | planned |

## First useful release

The first meaningful release lets a user:

1. Install one Ubuntu package.
2. Connect securely.
3. Log in as a real Linux user.
4. See a macOS-inspired desktop.
5. Inspect host health.
6. Browse permitted files.
7. Use a real terminal.
8. Inspect and control systemd services.
9. Search and stream journal logs.
10. Plan and apply updates.
11. Elevate only for defined actions.
12. Recover after reconnecting.
13. See an audit trail.
14. Observe changes made through SSH.
15. Uninstall the product without damaging the server.

It explicitly does not include:

- Multiple hosts in one browser session.
- A plugin marketplace.
- Every Cockpit feature.
- Arbitrary graphical desktop applications.
- Kubernetes.
- Complete storage provisioning.
- An autonomous AI administrator.
- An exact macOS visual replica.

## Naming and branding

- The product name is **Lumio OS**.
- Original logo and iconography are pending; all branding, visual assets,
  typography, icons and window controls are original work.
- Never use Apple or Cockpit assets.
