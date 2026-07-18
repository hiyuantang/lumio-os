# Lumio OS — Agent Guide

## License (IMPORTANT — read before editing)

Lumio OS is licensed under the **GNU Affero General Public License v3 only**
(`AGPL-3.0-only`), NOT `AGPL-3.0-or-later`.

- The [LICENSE](LICENSE) file is the FSF's standard AGPL-3.0 text. The
  "or any later version" paragraph near the end is part of the FSF's
  *How to Apply These Terms* appendix — it is a **sample notice**, not a
  binding choice. Do not treat the project as "or later".
- The authoritative choice is declared by:
  1. The `## License` section in [README.md](README.md)
  2. `"license": "AGPL-3.0-only"` in [package.json](package.json)
  3. Per-file SPDX headers (see below)
- SPDX distinguishes `AGPL-3.0-only` from `AGPL-3.0-or-later`; the project
  uses the former.

### Rules

- **Never** modify the [LICENSE](LICENSE) file. It must stay byte-for-byte
  identical to the FSF's AGPL-3.0 text.
- **Never** add GPL/LGPL/MIT/Apache or any other license to the repository
  without explicit user approval.
- **Never** add a "or any later version" clause or `AGPL-3.0-or-later`
  anywhere. The choice is v3-only.
- Any new `package.json` (or equivalent manifest) **must** set
  `"license": "AGPL-3.0-only"`.

### Per-file SPDX headers

New source files **should** carry the SPDX header on the first line:

```ts
// SPDX-License-Identifier: AGPL-3.0-only
```

```py
# SPDX-License-Identifier: AGPL-3.0-only
```

When creating a new file, match the comment style of nearby files and
include the header. Do not remove existing SPDX headers when editing.

### Future: web interface

When a web UI exists, add an About → Legal page covering:

- **Source Code** — link to the repository
- **License** — AGPL-3.0-only, with a link to [LICENSE](LICENSE)
- **No Warranty** — AGPL-3.0 §15 / §16 disclaimer

This page is also the natural place to host the source-code offer
required by AGPL-3.0 §13 for modified versions used over a network.

## Coding conventions

- Do not add license headers other than the SPDX line above.
- Do not add inline comments unless explicitly requested.
- Match the style of neighboring files; do not introduce new frameworks
  without checking the project first.

## Project mission

Build an independent, web-native desktop for administering headless Ubuntu
servers. The browser renders the desktop, applications, windows, menus and
animations locally. The server exposes typed system capabilities and streams
state changes.

The product must feel calm and desktop-native, inspired by macOS interaction
principles, but must use original branding, visual assets, typography,
components, layouts and implementation.

## Clean-room rules

1. This is an independent implementation.
2. Do not clone, open, search, fetch or inspect third-party public
   repositories for implementation reference.
3. Do not reproduce source code, tests, CSS, assets, DOM structure,
   comments, strings, internal identifiers or undocumented protocols from
   other projects.
4. Do not translate another project's code into another language.
5. Do not use generated code derived from another project's source.
6. Official documentation (Ubuntu, systemd, D-Bus, polkit, POSIX, web
   standards) may be used as technical references.
7. Implement from the product specifications in this repository.
8. Record the reference specification and original design rationale in each
   pull request.
9. Stop and report any accidental exposure to another project's source.

## System principles

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
