# Lumio OS — Supported Systems

## Primary target

- **Ubuntu 26.04 LTS** — systemd 259, Netplan 1.2, kernel 7.0.
- Architectures: **amd64** and **arm64**.

## Compatibility target

- **Ubuntu 24.04 LTS**, which remains under standard security maintenance
  through May 2029.
- Compatibility is enforced by automated VM testing, not by assumption.
- Where behavior differs between releases — for example systemd 255 vs 259,
  or the removal of cgroup v1 in 26.04 — the product must feature-detect,
  not version-sniff.

## Topology

1. One server per browser session.
2. No multi-host control plane.
3. Never load server-supplied application JavaScript from managed hosts.

## Explicitly out of scope

- Arbitrary native GUI streaming in the first release.
- A built-in root AI agent.
- Kubernetes.
- Other distributions may work but are unsupported.
