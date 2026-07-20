# Lumio OS — Clean-Room Policy

## Policy

1. Lumio OS is an **independent, clean-room, behavioral
   reimplementation** of a Linux server-management desktop.
2. Cockpit is a **benchmark and research reference only**. It is never
   an implementation source.
3. Never access, clone, fetch, search or inspect the Cockpit repository
   or any `cockpit-project/*` URL from the implementation workspace.
4. "Never copy and paste" is not enough: an agent that reads Cockpit
   source and then implements in the same context can unintentionally
   reproduce distinctive structures, names, tests, protocol details or
   UI expression. The two stages below exist to prevent that.
5. Stop and report any accidental exposure to Cockpit source.

## The two stages

1. **Reference analyst** — studies Cockpit in a separate, read-only
   workspace and produces written, code-free behavioral documents.
2. **Implementation agent** — builds the product from specifications
   and official documentation, and **never sees the Cockpit
   repository**.

The flow is: Cockpit repository → reference analyst → behavioral
documents only → independent specification repository → implementation
agent → the Lumio OS repository.

**Current status (decision recorded 2026-07-18):** the analyst stage is
**deferred**. Implementation proceeds from the product specifications in
this repository and official documentation only — Ubuntu, systemd,
D-Bus, polkit, POSIX and browser documentation. The rules below remain
binding and take effect the moment the analyst stage is activated.

## Analyst output rules

The analyst **may** produce:

- user-visible behavior descriptions;
- architecture diagrams;
- state machines;
- lists of Linux APIs involved;
- security observations;
- error and recovery cases;
- black-box test scenarios;
- feature comparison tables.

The analyst **must not** produce:

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
- instructions such as "implement it exactly as file X does."

## Implementation environment rules

The implementation environment must deny access to:

```text
github.com/cockpit-project/*
raw.githubusercontent.com/cockpit-project/*
local paths containing the Cockpit clone
```

1. Never mount a Cockpit checkout into the implementation workspace.
2. Keep the reference and implementation repositories under different
   users or isolated containers.
3. The builder receives only: product requirements, original UI designs,
   behavioral specifications, official Ubuntu/systemd/D-Bus/polkit/
   browser documentation, and acceptance tests written in product
   language.

## Provenance controls

Every feature pull request must include this block:

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

As the project matures, add these CI checks:

- dependency license inventory;
- software bill of materials (SBOM) generation;
- secret scanning;
- duplicated-code scanning;
- token or fingerprint similarity checks against the reference
  repository;
- verification that no Cockpit assets or source files exist in this
  repository;
- source-header and third-party attribution validation.

This is not a guarantee against every intellectual-property issue, but
it produces a far stronger engineering provenance record than "the model
promised not to copy."

## Licensing note

Cockpit's repository contains components under several licenses,
including LGPL-2.1+, GPL-3.0+, BSD-3-Clause, CC-BY-SA and MIT; the main
project is generally described as LGPL-2.1+.

Methods and systems may inspire an independent implementation; source
code and other original expression may not be copied. This section is an
engineering provenance record, not legal advice.
