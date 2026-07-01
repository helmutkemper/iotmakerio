# Parser Complexity Limits

## Overview

The IoTMaker portal parses Go source code submitted by specialists using
`go/ast`. Without limits, a malicious or careless specialist could submit a
struct with thousands of methods, ports, or prop-tagged fields, causing:

- **Memory exhaustion** in the worker and server processes.
- **IDE menu overflow** — the hex menu layout engine allocates one entry per
	method; thousands of entries make the menu unusable.
- **Codegen bloat** — the code generator emits one call site per port; thousands
	of ports produce megabytes of generated source code.

The limit system caps complexity at four structural points:

| Limit | What it caps | Violation behaviour |
|---|---|---|
| `parser_max_methods` | Exported non-Init methods per device struct | **Hard error** — component rejected |
| `parser_max_inputs` | Input ports (parameters) per method | **Soft** — excess truncated, warning emitted |
| `parser_max_outputs` | Output ports (return values) per method | **Soft** — excess truncated, warning emitted |
| `parser_max_props` | `prop:`-tagged struct fields per device | **Soft** — excess truncated, warning emitted |

Hard errors reject the component entirely. Soft limits allow the component to
be saved and used, but the specialist sees a warning in the Templates or
Projects page.

---

## Architecture

### No circular dependencies

```
codegen/blackbox   ← defines ParserLimits, DefaultParserLimits()
       ↑
     store         ← defines GetParserLimits(userID) → bbparser.ParserLimits
       ↑
   handler/*       ← calls store.GetParserLimits(claims.UserID)
   cmd/worker      ← calls store.GetParserLimits(p.UserID)
```

`codegen/blackbox` has no database dependency. `store` imports it and returns
a populated `ParserLimits` from the database. Handlers and the worker import
`store` and pass limits down to `bbparser.Parse()`.

### Three-layer resolution

Every parse call resolves limits in priority order:

```
1. Per-user override   user_parser_limits WHERE user_id = ? AND limit_key = ?
        ↓ (not found)
2. Global setting      project_settings   WHERE key = ?
        ↓ (not found or <= 0)
3. Hard fallback       compile-time constant in codegen/blackbox/limits.go
```

The hard fallback guarantees the parser always has a valid positive-integer
limit, even if the database is unavailable or a key was accidentally deleted.
Its values match the seed values so behaviour is identical before and after the
first server start.

---

## Database tables

### `project_settings` rows (global defaults)

Seeded automatically on every server start with `INSERT OR IGNORE`:

| Key | Default | Description |
|---|---|---|
| `parser_max_methods` | `32` | Maximum exported methods per device. Hard error if exceeded. |
| `parser_max_inputs` | `16` | Maximum input ports per method. Excess truncated. |
| `parser_max_outputs` | `16` | Maximum output ports per method. Excess truncated. |
| `parser_max_props` | `32` | Maximum prop-tagged fields per device. Excess truncated. |

To change a global default at runtime:

```sql
UPDATE project_settings
SET value = '64', updated_at = datetime('now')
WHERE key = 'parser_max_methods';
```

### `user_parser_limits` table (per-user overrides)

```sql
CREATE TABLE user_parser_limits (
    user_id    TEXT NOT NULL,
    limit_key  TEXT NOT NULL,
    value      TEXT NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, limit_key)
);
```

`note` is a free-text field for admin annotations:

```
"Trusted specialist — ships I2C driver library with 48 methods"
```

---

## Go API

### Reading limits (call before every parse)

```go
// Server handlers — authenticated user
limits := store.GetParserLimits(claims.UserID)
def, err := bbparser.Parse(src, limits)

// Background worker — user ID from task payload
limits := store.GetParserLimits(p.UserID)
def, err := bbparser.Parse(src, limits)

// No user context (public listing, codegen pipeline, tests)
limits := bbparser.DefaultParserLimits()
def, err := bbparser.Parse(src, limits)
```

### Writing per-user overrides (admin only)

```go
// Grant a higher method limit to a trusted specialist
err := store.SetUserParserLimit(userID, store.SettingParserMaxMethods, 64,
    "trusted specialist — ships I2C driver library")

// Remove the override (user reverts to global default)
err := store.DeleteUserParserLimit(userID, store.SettingParserMaxMethods)

// List all overrides for a user
limits, err := store.ListUserParserLimits(userID)
for _, l := range limits {
    fmt.Printf("%s = %s  (%s)\n", l.LimitKey, l.Value, l.Note)
}
```

### Available setting key constants

```go
store.SettingParserMaxMethods  // "parser_max_methods"
store.SettingParserMaxInputs   // "parser_max_inputs"
store.SettingParserMaxOutputs  // "parser_max_outputs"
store.SettingParserMaxProps    // "parser_max_props"
```

---

## Behaviour at each call site

| Call site | User ID source | Rationale |
|---|---|---|
| `handler/bblegacy` `handleParse` | `claims.UserID` (JWT) | Interactive parse — the specialist editing their own code benefits from their own limit |
| `handler/bblegacy` `handleSave` (worker) | `p.UserID` set from `claims.UserID` | Task payload carries the owner ID to the worker |
| `handler/blackboxapi` `handleList` | `""` (no user) | Public listing — no authenticated user; global limits applied |
| `templatepack.ParseZip` | `p.UploaderUserID` (task payload) | The specialist who uploaded the ZIP gets their own limit |
| `store.LoadBlackBoxDefsForScene` | `DefaultParserLimits()` | Re-parsing stored code that already passed upload-time checks; compile-time defaults prevent breakage if global limits are tightened later |

---

## Soft vs hard errors

**Hard error** — `parser_max_methods`:

```
MyDevice has more than 32 exported methods (found at least "ReadSensor");
reduce the number of exported methods or request a higher limit from an admin
```

The component is **rejected**. The specialist must split the device or request
a higher per-user limit.

**Soft error** — ports or props truncated:

```
MyDevice.Run: input port count truncated to 16 (limit)
MyDevice: setting count truncated to 32 (limit); excess fields ignored
```

The component is **accepted** and usable. Only the first N ports/props are
visible in the IDE. The specialist sees the warning in the Templates page.

---

## Adding a new limit

1. Add a constant in `store/models.go`:
	 ```go
	 SettingParserMaxManualPages = "parser_max_manual_pages"
	 ```

2. Add a field to `codegen/blackbox/limits.go`:
	 ```go
	 type ParserLimits struct {
			 // ...existing fields...
			 MaxManualPages int
	 }
	 ```

3. Update `DefaultParserLimits()` and add a `compiledDefault*` constant.

4. Add a seed in `store/db_parser_limits.go`:
	 ```go
	 {SettingParserMaxManualPages, "10", "Maximum manual page blocks per device."},
	 ```

5. Update `GetParserLimits()` in `store/parser_limits.go` to resolve the
	 new field.

6. Enforce the limit in the parser.

---

## Admin panel (planned)

A future admin UI panel will expose:

- A table of all `project_settings` rows with inline edit.
- A per-user override editor: select user → set limit key → set value → add note.
- A read-only audit log of changes (who changed what, when).

Until the panel is built, changes are made directly via SQL or via the
`store.SetUserParserLimit` function called from a one-off CLI tool or migration
script.
