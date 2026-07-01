# store

This package owns **all database access** for the IoTMaker portal.
Nothing outside this package is allowed to run raw SQL queries.

## Database engine

[modernc/sqlite](https://gitlab.com/cznic/sqlite) — a pure-Go SQLite driver
(no CGO required). Chosen for zero-dependency deploys and easy CI.

The connection is opened once in `db.go` and the handle is exported as `store.DB`.
All store functions use this shared handle. Because SQLite allows only one writer
at a time, `DB.SetMaxOpenConns(1)` is set to avoid write contention.

## File overview

| File | Responsibility |
|------|---------------|
| `db.go` | Open the database, run migrations, seed lookup tables |
| `models.go` | All Go structs that map to database rows |
| `users.go` | User CRUD — create, read, update password, verify email |
| `otp.go` | One-time code create/consume/prune |
| `blackbox.go` | BlackBox component CRUD |
| `projects.go` | Project CRUD + lookup tables (programming languages, UI languages) |
| `i18n.go` | i18n bundles and messages CRUD |
| `seed.go` | SeedAdmin (first-run admin account) + SeedIDETranslations |

## Schema overview

```
users
  └── otp_codes           (FK → users.id CASCADE DELETE)

i18n_bundles
  └── i18n_messages       (FK → i18n_bundles.locale CASCADE DELETE)

blackboxes                (FK → users.id SET NULL on delete)

programming_languages     (lookup — seeded, admin-managed)
project_ui_languages      (lookup — seeded, admin-managed)

projects
  ├── FK → users.id                    CASCADE DELETE
  ├── FK → programming_languages.id    (protected)
  └── FK → project_ui_languages.id     (protected)
```

## Adding a new programming language

Edit `seedProgrammingLanguages()` in `db.go`:

```go
{"rust", "rust", "Rust", 2},
```

Uses `INSERT OR IGNORE` — safe to deploy without a migration.

## Adding a new UI language

Edit `seedProjectUILanguages()` in `db.go`:

```go
{"es", "es", "Español", 3},
```

## Migration strategy

All migrations are additive `CREATE TABLE IF NOT EXISTS` statements inside
`migrate()` in `db.go`. There are no destructive migrations and no migration
versioning library. This keeps the codebase lean for the current scale.

When a breaking change is needed in the future, add a new `ALTER TABLE … ADD COLUMN`
statement at the end of the `stmts` slice — SQLite supports adding nullable columns
without a table rebuild.

## Error conventions

| Error | Meaning |
|-------|---------|
| `store.ErrNotFound` | `sql.ErrNoRows` — query matched nothing |
| `store.ErrConflict` | UNIQUE or PRIMARY KEY constraint violated |

Handlers should map these to HTTP 404 and 409 respectively.

## Wizard drafts

`wizard_drafts.go` holds the per-user, per-project draft state for the
device wizard tab — see `docs/CLAUDE_WIZARD_DESIGN.md` §7. One row per
`(project_id, user_id)` enforced by a UNIQUE index. CRUD is
`GetWizardDraft`, `UpsertWizardDraft`, `DeleteWizardDraft`. A
30-day-idle cleanup is run by the Asynq task in
`server/tasks/wizard_cleanup.go` (registered in `cmd/worker/main.go`).

The integrity story: the server **recomputes** `incomplete[]` from
the posted `parsed` on every save, persists that recomputed list,
and ignores anything the client claims about completion state.
Even if a malicious client tampers with `parsed`, the persisted
view reflects what the server actually saw — that is the real
barrier here, not any client-echoed signature. The earlier slice 3
HMAC implementation has been removed; the dormant `parsed_hmac`
column survives in the schema (NOT NULL DEFAULT '') so existing
rows do not need migration, and slice 8 (publish to GitHub) can
repurpose it for a publish-time signature if useful.

A specific error is returned when the draft does not exist:

| Error | Meaning |
|-------|---------|
| `store.ErrWizardDraftNotFound` | No draft for the (user, project) pair — first-time-open is normal, not a server error |
