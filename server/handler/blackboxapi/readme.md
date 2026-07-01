# server/handler/blackboxapi — IDE-facing black-box listing endpoint

## What this package does

Exposes a single HTTP endpoint consumed exclusively by the WASM IDE:

```
GET /api/v1/blackbox
```

Returns the **latest version of each saved component** as a JSON array, in the
format the WASM IDE expects — lean DTOs with port metadata, property labels, and
manual page content. Heavy fields (`StructCode`, `MethodsCode`, `Imports`) are
intentionally omitted.

---

## Files

| File         | Responsibility                                                                                                            |
|--------------|---------------------------------------------------------------------------------------------------------------------------|
| `routes.go`  | `Register(v1 *echo.Group)` — mounts `GET /api/v1/blackbox`                                                                |
| `handler.go` | `handleList()`, all DTO types (`clientBlackBoxDef`, …), conversion helpers (`toClientDef`, `toClientFuncDef`)             |
| `version.go` | Semantic version parsing (`parseVersion`), comparison (`semverGreater`), and deduplication (`deduplicateLatestByVersion`) |

---

## Version deduplication — why two layers?

A black-box component may be saved many times. Each save creates a new row in
the `blackboxes` table with a new `updated_at` timestamp and potentially a new
`version` string (e.g. `1.0.0` → `1.0.1` → `1.0.12`). The WASM Hardware menu
must show **exactly one entry per component**, always the most current one.

### Layer 1 — SQL (`store.ListLatestBlackBoxes`)

Uses the standard "max per group" SQL pattern:

```sql
SELECT b1.*
FROM   blackboxes b1
INNER JOIN (
    SELECT struct_name, MAX(updated_at) AS max_updated
    FROM   blackboxes
    GROUP  BY struct_name
) b2 ON b1.struct_name = b2.struct_name
     AND b1.updated_at  = b2.max_updated
```

This returns one row per `struct_name` with the highest `updated_at`. Works with
every SQL database the project may migrate to — no SQLite-specific syntax.

### Layer 2 — Go (`deduplicateLatestByVersion` in `version.go`)

Handles two edge cases the SQL layer cannot:

1. **Clock collision**: two rows share the same `struct_name` AND `updated_at`
	 (two saves within the same second). SQL returns both; Go keeps the higher semver.

2. **Re-saved old version**: a specialist saves `1.0.12` on Monday, then accidentally
	 re-saves `1.0.0` on Tuesday. SQL picks `1.0.0` (higher `updated_at`); Go corrects
	 it back to `1.0.12` (higher semver).

### Version format

Valid versions match `^[0-9]+\.[0-9]+\.[0-9]+$` (major.minor.patch, integers only).
Invalid strings are treated as `0.0.0` — any valid version beats them.

---

## Why re-parse instead of caching

Each component's source code is already stored in the database. Re-parsing on
request is a pure AST walk with no I/O, taking under 1 ms per component. The
alternative — storing the codegen-format JSON in a second database column —
would create a risk of the two formats diverging if the parser changes.
Re-parsing is simpler and always correct.

Components that fail re-parsing are **skipped with a log warning** (soft failure).
One broken component never blocks the rest from loading.

---

## Response format

```json
[
  {
    "name": "APDS9960",
    "structIcon": "greater-than-equal",
    "structLabel": "APDS9960",
    "doc": "APDS9960 is a colour/proximity sensor connected via I2C.",
    "init": {
      "doc": "Init configures the sensor.",
      "icon": "gear",
      "label": "Init",
      "menuCol": -1,
      "menuRow": -1,
      "menuPosSet": true,
      "inputs":  [{"name":"i2c","goType":"*machine.I2C"}],
      "outputs": [{"name":"err","goType":"error","isError":true}]
    },
    "methods": [
      {
        "name": "Run",
        "icon": "greater-than-equal",
        "label": "Read RGBC",
        "executionOrder": 10,
        "menuCol": 1,
        "menuRow": -1,
        "menuPosSet": true,
        "outputs": [...]
      }
    ]
  }
]
```

Fields intentionally omitted: `StructCode`, `MethodsCode`, `Imports`.

The `menuCol`, `menuRow`, `menuPosSet` fields are new — populated from the
`menu:col,row.` IDS directive. The WASM `rulesMainMenu.ApplyRadialLayout`
function reads them to place method entries in the Hardware hex menu.

---

## Adding a new field to the response

1. Add the field to the server-side `BlackBoxDef` or `FuncDef` in
	 `server/codegen/blackbox/types.go`.
2. Extract it in `server/codegen/blackbox/parser.go`.
3. Add the DTO field to the appropriate `client*` struct in `handler.go`.
4. Propagate it in `toClientDef()` or `toClientFuncDef()`.
5. Mirror the field in `blackbox/clientTypes.go` (WASM client types).
6. Update `server/blackbox/readme.md` (IDS standard documentation).
7. Update this file.

---

## Registration

In `server/cmd/server/main.go`:

```go
v1 := e.Group("/api/v1")
blackboxapi.Register(v1)   // mounts GET /api/v1/blackbox
```

## Access control

The endpoint is currently public (no auth middleware). All latest-version
components are visible to any IDE client. Add `requireBearerToken()` middleware
in `routes.go` if per-user isolation is needed in the future.

---

## Wizard endpoints (Slices 0–1 of the device wizard)

Three endpoints support the assisted-creation flow on the Projects page in
the SPA portal. They live in `wizard.go` and share the same handler
struct as the rest of the package.

```
POST /api/v1/blackbox/wizard/parse      (auth)
POST /api/v1/blackbox/wizard/analyze    (auth)
POST /api/v1/blackbox/wizard/rewrite    (auth)
```

The `parse` and `analyze` endpoints **replace** two routes that used to
live in `server/handler/_bblegacy/`. The leading underscore makes that
whole directory invisible to `go build`, so the routes had been
silently missing from the live binary for a while — every Parse /
Live-analysis click in the Projects page returned 404. The SPA had no
UI for that failure, so it appeared as a stuck spinner.

The `rewrite` endpoint is new. It is the engine of the wizard tab
(slices 3+): each modal save sends a list of typed edits, the engine
applies them via AST mutation + gofmt-clean byte splices, and returns
the rewritten source. See
`server/codegen/blackbox/readme.md → "Rewrite engine"` for the full
contract; this file documents only the HTTP surface.

### Why all wizard endpoints live on the same handler tree

The WASM IDE's component bank uses `GET /api/v1/blackbox` (in
`handler.go`). The Projects page wizard uses
`POST /api/v1/blackbox/wizard/{parse,analyze,rewrite}`. All three
touch the same parser package (`server/codegen/blackbox`), so keeping
them in the same handler package avoids duplicating its dependencies
(parser-limit resolution, claims extraction, envelope helpers).

### Request and response shapes

`/parse` and `/analyze` accept the same JSON body. The field name is
`code` (not `source`) for backward-compat with the Projects page JS,
which already sends that name and would otherwise need a coordinated
release.

```json
POST /api/v1/blackbox/wizard/parse
{ "code": "<full Go source>" }
```

`/rewrite` adds an `edits` array on the request. Each edit is a
`{op, path, args}` triple — see `codegen/blackbox/types.go →
WizardEdit` and the design doc for the full grammar.

```json
POST /api/v1/blackbox/wizard/rewrite
{
  "code":  "<full Go source>",
  "edits": [
    { "op": "setStructDirectives", "path": "struct.Sensor",
      "args": { "label": "Color Sensor", "icon": "eye" } },
    { "op": "setFieldProp", "path": "struct.Sensor.field.Gain",
      "args": { "label": "ADC Gain", "default": "1",
                "format": "options",
                "formatArgs": { "values": ["0","1","2","3"] } } }
  ]
}
```

All three endpoints respond with the canonical two-key envelope used
by the SPA:

```json
{
  "metadata": { "status": 200 },
  "data":     { ... }
}
```

For `/parse`, `data` is `{ parsed, incomplete }`:

- `parsed` is the live `BlackBoxDef` JSON produced by
  `codegen/blackbox.Parse` — the **canonical** shape (`name`, `props`,
  `structIcon`, `structLabel`, `methods[].inputs`, `methods[].outputs`).
  The legacy parser used field names like `structName`, `settings`, and
  `parseWarnings`; those are gone everywhere now.
- `incomplete` is a sorted JSON array of dotted paths — the wizard's
  single source of truth for ⚠ rendering. See
  `server/codegen/blackbox/readme.md → Completion engine` for the
  rules. Always an array, never null.

For `/analyze`, `data` is `{ diagnostics, durationMs, hasErrors }` —
the same shape `server/blackbox.Analyze` returns and the same shape
Monaco's `setModelMarkers` expects (positions are 1-based).

For `/rewrite`, `data` is `{ code, parsed, incomplete, applied }` —
the rewritten gofmt-clean source, plus a freshly computed parsed
BlackBoxDef and incomplete set so the wizard tab does not need to
follow up with a /parse call. `applied` is the count of edits the
engine accepted; today it is always equal to `len(request.edits)`.

### Errors

Hard parse errors (syntactically invalid Go) return HTTP 400 from
`/parse` and HTTP 422 from `/rewrite` (semantic problem, not a server
fault). Empty bodies always return 400.

`/parse` does not surface soft parser warnings (missing `connection:`
tags, prop/port truncation, manual-page malformation). The same
information is exposed more cleanly via the `incomplete` set in the
response — surfacing it twice would double-warn the user. The wizard
renders ⚠ from the set; soft warnings are silently dropped.

`/analyze` never errors on bad code — surfacing those problems as
diagnostics is its whole job; an empty body returns an empty
diagnostics array rather than an error so the SPA's debounced
on-keystroke calls don't spam the console mid-edit.

`/rewrite` returns 422 with the engine error message in
`metadata.error` for: malformed paths, unknown ops, missing required
args, bad source, and post-format failures. On any error the engine
guarantees the source is **not partially mutated** — the original
source is what comes back as the side effect of the failure.

### Future slices

## Wizard draft endpoints (Slice 3)

Three endpoints round-trip the wizard's per-user, per-project draft
state. The draft is keyed by the authenticated user and the path
`:projectId`; users can never read or write each other's drafts.

```
GET    /api/v1/blackbox/wizard/draft/:projectId    (auth)
POST   /api/v1/blackbox/wizard/draft/:projectId    (auth)
DELETE /api/v1/blackbox/wizard/draft/:projectId    (auth)
```

### GET /wizard/draft/:projectId

Loads the draft. Returns `404 no draft for this project` when the
user has not yet saved anything for this project — a normal first-open
condition that the wizard tab handles by initialising from the
current Editor source.

Response data shape:
```
{ code, parsed, parsedHmac, incomplete[], images[], helps[], updatedAt }
```

`parsed` is opaque BlackBoxDef JSON, identical bytes to what the
server emitted on the last successful /parse or /rewrite. Treat it as
a black box on the client (literally — it's a black-box def). The
`parsedHmac` lets the client echo it back on save without having to
re-parse on the server.

`images` and `helps` are placeholders for slice 7+ — slice 3 always
emits `[]`.

### POST /wizard/draft/:projectId

Upserts the draft. Body:
```
{ code, parsed, parsedHmac }
```

The handler:
1. Verifies `parsedHmac` against a fresh recomputation under the
   server's wizard secret. Rejects with 422 on mismatch.
2. Unmarshals `parsed` into a BlackBoxDef and recomputes
   `incomplete` server-side — defence in depth, so the persisted
   completion set always reflects the actual parsed bytes.
3. Inserts or updates the row. `created_at` is preserved across
   updates; `updated_at` is bumped on every call.

Field name note: the body uses `code`, not `source`. The design doc
§8 calls this field `source`; the codebase has used `code` since
slice 0 (`/parse` and `/rewrite` both use `code`). Renaming would
force a coordinated SPA + server release for no functional benefit.
The store column is `source` regardless — the JSON name is the only
divergence.

### DELETE /wizard/draft/:projectId

Drops the draft. Idempotent — deleting a non-existent draft is a
no-op and still returns 200. Used by the wizard's "Cancel" button.

### HMAC machinery

The hex-encoded HMAC-SHA256 lives in `parsedHmac` on every /parse,
/rewrite, GET /draft, and POST /draft body. The server-only secret
is stored in `project_settings.wizard_hmac_secret`; it is generated
on first use and persisted across restarts. See
`server/store/wizard_drafts.go` for the threat model — short version,
this is a sanity check, not a security barrier, and rotating the
row invalidates every in-flight draft.

---

## Future slices

Slice 6 adds `GET /wizard/icons`. Slice 7 adds
`POST /wizard/draft/:projectId/help` and the image variants.
Slice 8 adds `POST /wizard/publish/:projectId` (GitHub-App push
that reuses the existing submit pipeline). All go in `wizard.go`
next to the six handlers we have today.
