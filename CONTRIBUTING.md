# Contributing to IoTMaker

Thanks for your interest in improving IoTMaker!

## Before you start

IoTMaker is dual-licensed (AGPL + commercial) — see [`LICENSING.md`](./LICENSING.md).
So the project stays sustainable and both licenses remain possible, **every
contribution requires agreeing to the Contributor License Agreement**
([`CLA.md`](./CLA.md)).

- The easiest way is a **CLA bot** (e.g. [cla-assistant](https://github.com/cla-assistant/cla-assistant)):
  it prompts you to accept on your first pull request.
- Until that is set up, add a `Signed-off-by:` line to your commits and state in
  the pull request that you accept the CLA.

Contributions without an accepted CLA cannot be merged. This is not about
distrust — it is what lets a solo-maintained project be offered commercially and
fund its own continued development.

## Workflow

1. **Discuss non-trivial changes first** by opening an issue. IoTMaker follows an
   architecture-before-code rule: multi-layer changes need design agreement
   before implementation.
2. Fork, branch, and make **focused** changes — touch only what the change needs.
3. Keep the existing project conventions (see `CLAUDE.md` / `INVARIANTS.md` if
   present in the repo).
4. Open a pull request describing **what** changed and **why**.

## New source files

Every new source file starts with its **path comment** (existing convention),
followed by the SPDX license header:

**Go**
```go
// path/to/file.go
// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Helmut Kemper
```

**C99**
```c
/* path/to/file.c */
/* SPDX-License-Identifier: AGPL-3.0-only */
/* Copyright (C) 2026 Helmut Kemper */
```

The SPDX header is metadata and stays in English; your usual bilingual
(English + pt-BR) documentation comments follow as normal.

Files **emitted by the generator** must instead carry the generated-code header
from [`GENERATED-CODE-EXCEPTION.md`](./GENERATED-CODE-EXCEPTION.md) — **not** the
AGPL header — because that output is not covered by the AGPL.
