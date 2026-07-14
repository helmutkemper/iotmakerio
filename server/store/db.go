// server/store/db.go — SQLite database lifecycle for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Responsibilities:
//   - Open the database with sane WAL pragmas for concurrent reads.
//   - Run all schema migrations idempotently on startup.
//   - Export the *sql.DB handle used by every other store file.
//
// Migration strategy:
//   - New tables:   CREATE TABLE IF NOT EXISTS  — always safe to re-run.
//   - Seeds:        INSERT OR IGNORE            — always safe to re-run.
//   - No ALTER TABLE — the database is always created from scratch.
//     Delete the database file to reset.
//
// Table overview:
//
//	users                     — registered accounts
//	user_profiles             — public-facing profile data (display name, bio, links, avatar)
//	invite_codes              — single-use registration invite codes
//	otp_codes                 — one-time verification / 2FA / password-reset codes
//	i18n_bundles              — one row per supported locale
//	i18n_messages             — individual translated strings (locale, key) → value
//	blackboxes                — parsed IoT component definitions (legacy)
//	programming_languages     — languages available for project creation
//	project_ui_languages      — user interface languages for project documentation
//	projects                  — user projects grouping code, images and docs
//	project_code_versions     — snapshot headers of the editor saves (content: project_code_files)
//	project_settings          — server-side configurable limits and feature flags
//	project_categories        — top-level component categories
//	project_subcategories     — subcategories scoped to a parent category
//	template_packages         — specialist-uploaded project templates with configurable devices
package store

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

// DB is the shared database handle.
// It is set by Open() and used by all store functions.
var DB *sql.DB

// Open initialises the SQLite database at the given path and runs migrations.
// It must be called exactly once from main() before any store function is used.
func Open(path string) error {
	var err error
	DB, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}

	// SQLite performs best with a single writer; allow many concurrent readers.
	DB.SetMaxOpenConns(1)

	if err = applyPragmas(); err != nil {
		return err
	}
	if err = migrate(); err != nil {
		return err
	}

	log.Printf("[store] database ready at %s", path)
	return nil
}

// applyPragmas sets pragmas that must be enabled once per connection.
//
// journal_mode=DELETE is used instead of WAL intentionally.
// WAL relies on a memory-mapped shared-memory file (-shm) for the WAL index.
// On ARM64 Linux inside Docker containers (overlay2 / tmpfs storage drivers)
// the mmap of the WAL index can fail silently, causing a SIGBUS when SQLite
// tries to write to the mapped address via _walIndexAppend → Xmemset.
// DELETE mode uses standard file locking which works correctly on all
// platforms and storage drivers without shared memory.
//
// Performance trade-off: DELETE mode serialises readers and writers, but
// because we already set MaxOpenConns(1) the database is single-connection
// anyway — WAL's concurrent-reader benefit is not available to us.
func applyPragmas() error {
	pragmas := []string{
		`PRAGMA journal_mode = DELETE;`,
		`PRAGMA foreign_keys = ON;`,
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA synchronous = NORMAL;`, // safe with DELETE mode; faster than FULL
	}
	for _, p := range pragmas {
		if _, err := DB.Exec(p); err != nil {
			return err
		}
	}
	return nil
}

// migrate creates all required tables and seeds initial data.
// Every operation is idempotent — safe to run on every startup.
// Tables are created in dependency order: referenced tables first.
//
// There is no ALTER TABLE migration phase. The database is always created
// from scratch with the complete schema. Delete the database file to reset.
func migrate() error {

	// ── Phase 1: CREATE TABLE IF NOT EXISTS ──────────────────────────────────
	createStmts := []string{

		// ── Users ────────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS users (
			id               TEXT PRIMARY KEY,
			username         TEXT NOT NULL UNIQUE COLLATE NOCASE,
			email            TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash    TEXT NOT NULL,
			role             TEXT NOT NULL DEFAULT 'user',
			verified         INTEGER NOT NULL DEFAULT 0,
			preferred_locale TEXT NOT NULL DEFAULT 'en-US',
			country_code     TEXT NOT NULL DEFAULT '',
			menu_profile_id  TEXT,
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_users_email    ON users(email COLLATE NOCASE);`,
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username COLLATE NOCASE);`,

		// ── User Profiles ─────────────────────────────────────────────────────
		// Stores public-facing profile data separately from authentication data.
		// The row is created during registration and always present for verified users.
		// display_name is the public name shown in the feed; it differs from username
		// (the login handle) and can contain spaces and unicode characters.
		`CREATE TABLE IF NOT EXISTS user_profiles (
			user_id         TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			display_name    TEXT NOT NULL DEFAULT '',
			bio             TEXT NOT NULL DEFAULT '',
			avatar_url      TEXT NOT NULL DEFAULT '',
			github_url      TEXT NOT NULL DEFAULT '',
			website_url     TEXT NOT NULL DEFAULT '',
			-- github_username is the verified GitHub login obtained via OAuth.
			-- Set by store.SetGithubUsername after a successful OAuth flow.
			-- Empty string means the user has not connected their GitHub account.
			github_username TEXT NOT NULL DEFAULT '',
			updated_at      TEXT NOT NULL
		);`,

		// ── Invite Codes ──────────────────────────────────────────────────────
		// Single-use registration codes. Any verified user can generate them.
		// The invite system can be toggled on/off via the 'invite_required' setting.
		// used_by has ON DELETE SET NULL so that deleting a user account does not
		// erase the audit trail of which invites were redeemed.
		`CREATE TABLE IF NOT EXISTS invite_codes (
			id          TEXT PRIMARY KEY,
			code        TEXT NOT NULL UNIQUE,
			created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			used_by     TEXT REFERENCES users(id) ON DELETE SET NULL,
			used_at     TEXT,
			expires_at  TEXT NOT NULL,
			created_at  TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_invite_code       ON invite_codes(code);`,
		`CREATE INDEX IF NOT EXISTS idx_invite_created_by ON invite_codes(created_by, created_at DESC);`,

		// ── OTP Codes ────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS otp_codes (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			code       TEXT NOT NULL,
			purpose    TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			used       INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_otp_user_purpose ON otp_codes(user_id, purpose);`,

		// ── i18n Bundles ─────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS i18n_bundles (
			locale      TEXT PRIMARY KEY,
			bundle_id   TEXT NOT NULL,
			display     TEXT NOT NULL DEFAULT '',
			updated_at  TEXT NOT NULL
		);`,

		// ── i18n Messages ────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS i18n_messages (
			locale      TEXT NOT NULL REFERENCES i18n_bundles(locale) ON DELETE CASCADE,
			message_id  TEXT NOT NULL,
			other       TEXT NOT NULL DEFAULT '',
			one         TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (locale, message_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_i18n_locale ON i18n_messages(locale);`,

		// ── BlackBoxes ────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS blackboxes (
			id                 TEXT PRIMARY KEY,
			user_id            TEXT REFERENCES users(id) ON DELETE SET NULL,
			struct_name        TEXT NOT NULL DEFAULT '',
			display_name       TEXT NOT NULL DEFAULT '',
			display_name_human TEXT NOT NULL DEFAULT '',
			category           TEXT NOT NULL DEFAULT '',
			category_id        TEXT,
			subcategory_id     TEXT,
			-- Source language token (programming_languages.id: "golang", "c", …).
			-- Set by the import worker; Go-only today. Drives the IDE menu's
			-- per-language device filtering via the list endpoint.
			programming_language_id TEXT,
			author             TEXT NOT NULL DEFAULT '',
			version            INTEGER NOT NULL DEFAULT 1,
			package_doc        TEXT NOT NULL DEFAULT '',
			source_code        TEXT NOT NULL DEFAULT '',
			parsed_json        TEXT NOT NULL DEFAULT '{}',
			methods_json       TEXT NOT NULL DEFAULT '[]',
			settings_json      TEXT NOT NULL DEFAULT '[]',
			parse_errors       TEXT NOT NULL DEFAULT '[]',
			github_url         TEXT NOT NULL DEFAULT '',
			github_owner       TEXT NOT NULL DEFAULT '',
			github_repo        TEXT NOT NULL DEFAULT '',
			github_tag         TEXT NOT NULL DEFAULT '',
			tags               TEXT NOT NULL DEFAULT '',
			blocked            INTEGER NOT NULL DEFAULT 0,
			status             TEXT NOT NULL DEFAULT 'ready',
			visibility         TEXT NOT NULL DEFAULT 'private',
			publish_to_feed    INTEGER NOT NULL DEFAULT 0,
			publish_to_search  INTEGER NOT NULL DEFAULT 0,
			ready_to_use       INTEGER NOT NULL DEFAULT 0,
			created_at         TEXT NOT NULL,
			updated_at         TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_blackboxes_user    ON blackboxes(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_blackboxes_updated ON blackboxes(updated_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_blackboxes_version ON blackboxes(struct_name, version DESC);`,

		// ── Programming Languages ─────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS programming_languages (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL UNIQUE,
			display    TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,

		// ── Project UI Languages ──────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS project_ui_languages (
			id         TEXT PRIMARY KEY,
			code       TEXT NOT NULL UNIQUE,
			display    TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,

		// ── Project Settings ─────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS project_settings (
			key         TEXT PRIMARY KEY,
			value       TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			updated_at  TEXT NOT NULL
		);`,

		// ── Project Categories ────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS project_categories (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			icon_fa    TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,

		// ── Project Subcategories ─────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS project_subcategories (
			id          TEXT PRIMARY KEY,
			category_id TEXT NOT NULL REFERENCES project_categories(id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			sort_order  INTEGER NOT NULL DEFAULT 0,
			icon_fa     TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL,
			UNIQUE(category_id, name)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_psc_category ON project_subcategories(category_id, sort_order);`,

		// ── Projects ─────────────────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS projects (
			id                       TEXT PRIMARY KEY,
			user_id                  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name                     TEXT NOT NULL,
			type                     TEXT NOT NULL DEFAULT 'custom_device',
			-- visibility is always 'private'. Projects in this table are
			-- the user's own working drafts (wizard-authored devices,
			-- iterative Monaco sessions); they are never shared. Devices
			-- intended for sharing live in the blackboxes table and
			-- are imported from GitHub releases via the worker. The
			-- column is kept (instead of being dropped) so existing
			-- code paths that read p.Visibility continue to work, and
			-- so a future change of policy doesn't need a column add.
			-- The CHECK constraint enforces the rule at the DB level --
			-- no rogue UPDATE can flip it to 'public'.
			visibility               TEXT NOT NULL DEFAULT 'private'
			                         CHECK(visibility = 'private'),
			programming_language_id  TEXT NOT NULL REFERENCES programming_languages(id),
			ui_language_id           TEXT NOT NULL REFERENCES project_ui_languages(id),
			card_title               TEXT NOT NULL DEFAULT '',
			card_image               TEXT NOT NULL DEFAULT '',
			card_description         TEXT NOT NULL DEFAULT '',
			card_keywords            TEXT NOT NULL DEFAULT '',
			category_id              TEXT,
			subcategory_id           TEXT,
			publish_to_feed          INTEGER NOT NULL DEFAULT 0,
			publish_to_search        INTEGER NOT NULL DEFAULT 0,
			ready_to_use             INTEGER NOT NULL DEFAULT 0,
			created_at               TEXT NOT NULL,
			updated_at               TEXT NOT NULL,
			UNIQUE(user_id, name)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_projects_user       ON projects(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_projects_visibility ON projects(visibility);`,
		`CREATE INDEX IF NOT EXISTS idx_projects_lang       ON projects(programming_language_id);`,
		`CREATE INDEX IF NOT EXISTS idx_projects_category   ON projects(category_id);`,
		`CREATE INDEX IF NOT EXISTS idx_projects_card       ON projects(visibility, card_title) WHERE card_title != '';`,

		// ── Project Backups ───────────────────────────────────────────────────
		//
		// One-row-per-project transient backup of the working source code.
		// Distinct from project_code_versions (which is the user-facing
		// "Save" history, incremental and immutable). The backup is a
		// single slot that overwrites itself on every auto-save trigger:
		// tab switch, wizard edit, debounced Monaco edit.
		//
		// Lifecycle:
		//   - Created on first non-empty save (the project itself starts
		//     with no backup row at all).
		//   - Updated in place — no version history kept.
		//   - Deleted automatically when the source becomes empty (the
		//     user shouldn't see the backup persist across reopens if
		//     they cleared their work).
		//   - Deleted via ON DELETE CASCADE when the project is deleted.
		//
		// Recovery semantics:
		//   - When a project is reopened, the client compares
		//     backup.updated_at against the latest project_code_versions
		//     row. If the backup is newer, the source comes from the
		//     backup AND the Save button starts in the "pending" state
		//     (red) so the user sees they have unsaved work.
		`CREATE TABLE IF NOT EXISTS project_backups (
			project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			source     TEXT NOT NULL,
			filename   TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);`,

		// ── Project Code Versions ─────────────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS project_code_versions (
			id         TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			version    INTEGER NOT NULL DEFAULT 1,
			-- last_parse_ok records whether the wizard's /parse endpoint
			-- returned a successful BlackBoxDef for this exact snapshot at
			-- save time. The IDE uses this on project open to decide
			-- whether to silently re-parse and populate the Preview tab
			-- without user intervention. When false, the Preview tab
			-- stays at its placeholder until the user clicks Parse.
			-- We do not persist the parsed JSON itself: the parser is
			-- deterministic and re-running it on a known-good snapshot is
			-- cheaper than maintaining a column whose schema would have
			-- to evolve in lockstep with BlackBoxDef.
			--
			-- The snapshot's CONTENT lives in project_code_files (one row
			-- per file; see store/project_code_files.go) — this row is the
			-- snapshot HEADER only. Dev databases created before the
			-- multi-file model may carry orphan filename/source columns;
			-- they are harmless and unused. Delete ./data to reset
			-- (pre-release, no legacy data exists by decision — 2026-07).
			last_parse_ok INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			UNIQUE(project_id, version)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_pcv_project_version ON project_code_versions(project_id, version DESC);`,

		// ── Template Packages ─────────────────────────────────────────────────
		//
		// A template package is a ZIP uploaded by a specialist containing:
		//   - devices/ — IDS-compliant Go structs (parsed into visual IDE blocks)
		//   - output/  — static assets with {{.VarName}} directives for injection
		//   - template.json — manifest declaring var → device.field mappings
		//
		// Upload lifecycle:
		//   status lifecycle (server-managed):
		//   status=no_version — template created, no ZIP uploaded yet
		//   status=pending    — ZIP version saved to disk, worker parse queued
		//   status=ready      — latest version parsed OK, def_json promoted from version row
		//   status=error      — latest version parse failed, parse_errors promoted from version row
		//
		// Visibility lifecycle (owner-controlled):
		//   visibility=private — only the specialist can use this template
		//   visibility=public  — any authenticated maker can use this template
		//
		// def_json and parse_errors are promoted from the latest
		// template_package_versions row by the worker after each parse.
		// zip_path on the parent is kept for the latest version for convenience
		// (generate handler reads it); the canonical source is the version row.
		`CREATE TABLE IF NOT EXISTS template_packages (
			id                 TEXT PRIMARY KEY,
			user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name               TEXT NOT NULL DEFAULT '',
			description        TEXT NOT NULL DEFAULT '',
			visibility         TEXT NOT NULL DEFAULT 'private'
			                   CHECK(visibility IN ('private','public')),
			status             TEXT NOT NULL DEFAULT 'no_version'
			                   CHECK(status IN ('no_version','pending','ready','error')),
			latest_version     INTEGER NOT NULL DEFAULT 0,
			zip_path           TEXT NOT NULL DEFAULT '',
			def_json           TEXT NOT NULL DEFAULT '{}',
			parse_errors       TEXT NOT NULL DEFAULT '[]',
			github_url         TEXT NOT NULL DEFAULT '',
			github_owner       TEXT NOT NULL DEFAULT '',
			github_repo        TEXT NOT NULL DEFAULT '',
			github_tag         TEXT NOT NULL DEFAULT '',
			tags               TEXT NOT NULL DEFAULT '',
			blocked            INTEGER NOT NULL DEFAULT 0,
			display_name_human TEXT NOT NULL DEFAULT '',
			category_id        TEXT,
			subcategory_id     TEXT,
			publish_to_feed    INTEGER NOT NULL DEFAULT 0,
			publish_to_search  INTEGER NOT NULL DEFAULT 0,
			ready_to_use       INTEGER NOT NULL DEFAULT 0,
			created_at         TEXT NOT NULL,
			updated_at         TEXT NOT NULL
		);`,
		// idx_tpkg_user: the specialist lists their own templates, newest first.
		`CREATE INDEX IF NOT EXISTS idx_tpkg_user ON template_packages(user_id, updated_at DESC);`,
		// idx_tpkg_public: the IDE picker lists all ready public templates.
		`CREATE INDEX IF NOT EXISTS idx_tpkg_public ON template_packages(visibility, status, updated_at DESC);`,

		// template_package_versions mirrors project_code_versions.
		// Each row is one ZIP upload by the specialist. The version number is
		// auto-incremented by the server (MAX(version)+1 per pkg_id).
		// The parent template_packages row is kept in sync by the worker after
		// each successful parse (status, def_json, parse_errors are promoted up).
		// Only the latest version is active; older rows are kept for audit only.
		`CREATE TABLE IF NOT EXISTS template_package_versions (
			id           TEXT PRIMARY KEY,
			pkg_id       TEXT NOT NULL REFERENCES template_packages(id) ON DELETE CASCADE,
			user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			version      INTEGER NOT NULL DEFAULT 1,
			zip_path     TEXT NOT NULL DEFAULT '',
			github_url   TEXT NOT NULL DEFAULT '',
			github_tag   TEXT NOT NULL DEFAULT '',
			def_json     TEXT NOT NULL DEFAULT '{}',
			parse_errors TEXT NOT NULL DEFAULT '[]',
			created_at   TEXT NOT NULL,
			UNIQUE(pkg_id, version)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_tpkgv_pkg_version ON template_package_versions(pkg_id, version DESC);`,
	}

	// Append menu sections and commerce tables (defined in db_menu_commerce_tables.go).
	createStmts = append(createStmts, menuCommerceMigrationStmts()...)

	// Append menu tree tables — database-driven menu system with profiles,
	// per-audience layouts, locale labels, and help markdown.
	// Defined in db_menu_tree_tables.go.
	createStmts = append(createStmts, menuTreeMigrationStmts()...)

	// Append wizard drafts table — per-user, per-project draft state for
	// the device wizard tab. One row per (project_id, user_id), with HMAC
	// machinery and a 30-day cleanup. Defined in wizard_drafts.go.
	createStmts = append(createStmts, wizardDraftsMigrationStmts()...)

	for _, stmt := range createStmts {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}

	// ── Seeds ────────────────────────────────────────────────────────────────
	if err := seedLocales(); err != nil {
		return err
	}
	if err := SeedTranslations(); err != nil {
		return err
	}
	if err := seedProgrammingLanguages(); err != nil {
		return err
	}
	if err := seedProjectUILanguages(); err != nil {
		return err
	}
	if err := seedProjectSettings(); err != nil {
		return err
	}
	if err := seedCategories(); err != nil {
		return err
	}
	if err := SeedMenuTree(); err != nil {
		return err
	}
	// New system menu items added after a database was first seeded must be
	// inserted by an explicit migration — SeedMenuTree skips everything once
	// the catalog is populated. Idempotent (INSERT OR IGNORE throughout).
	if err := MigrateMenuTreeConstArrays(); err != nil {
		return err
	}
	if err := MigrateMenuTreeCase(); err != nil {
		return err
	}
	if err := MigrateMenuTreeVariables(); err != nil {
		return err
	}
	if err := MigrateMenuTreeData(); err != nil {
		return err
	}
	if err := MigrateI18nBackfillKeys(); err != nil {
		return err
	}
	if err := MigrateMenuTreeIndex(); err != nil {
		return err
	}
	if err := MigrateMenuTreePrint(); err != nil {
		return err
	}
	if err := MigrateMenuTreeDebugPosition(); err != nil {
		return err
	}
	// Sequential code numbers for generated-code names (iotm_47_…): creates
	// the registry and backfills every pre-existing project and black-box by
	// creation order. Idempotent. See store/code_numbers.go for the
	// engine-independent contract.
	//
	// Português: Números de código sequenciais dos nomes gerados: cria o
	// registro e numera as linhas pré-existentes por ordem de criação.
	// Idempotente. Contrato agnóstico em store/code_numbers.go.
	if err := MigrateCodeNumbers(); err != nil {
		return err
	}
	// Multi-file device sources: the snapshot-content table (one row per
	// file per version). See store/project_code_files.go for the model.
	//
	// Português: Fontes multiarquivo: a tabela de conteúdo dos snapshots
	// (uma linha por arquivo por versão). Modelo em project_code_files.go.
	if err := MigrateProjectCodeFiles(); err != nil {
		return err
	}

	// ── Feed feature: ratings, follows, feed_events ───────────────────────────
	for _, stmt := range feedMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}
	for _, s := range feedSettingSeeds() {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_settings (key, value, description, updated_at)
			VALUES (?, ?, ?, datetime('now'))`,
			s.key, s.value, s.description,
		); err != nil {
			return err
		}
	}

	// ── Community feature: comments and reports ───────────────────────────────
	for _, stmt := range communityMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}
	for _, s := range communitySettingSeeds() {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_settings (key, value, description, updated_at)
			VALUES (?, ?, ?, datetime('now'))`,
			s.key, s.value, s.description,
		); err != nil {
			return err
		}
	}

	// ── Stage files feature: saved IDE scenes with virtual folders ─────────
	for _, stmt := range stageFileMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}
	for _, s := range stageFileSettingSeeds() {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_settings (key, value, description, updated_at)
			VALUES (?, ?, ?, datetime('now'))`,
			s.key, s.value, s.description,
		); err != nil {
			return err
		}
	}

	// ── Live communication: API keys for device-scoped webhook auth ────────
	for _, stmt := range apiKeyMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}

	// ── Live communication: projects (unique ID per project) ───────────────
	for _, stmt := range liveProjectMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}

	// ── Project help files: SQLite-blob storage for wizard-authored
	//     readme / per-method / examples assets that travel with a
	//     published device. See db_help_files.go for the full rationale.
	for _, stmt := range helpFilesMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}
	for _, s := range helpFilesSettingSeeds() {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_settings (key, value, description, updated_at)
			VALUES (?, ?, ?, datetime('now'))`,
			s.key, s.value, s.description,
		); err != nil {
			return err
		}
	}

	// ── Project variables: user-declared GetVar/SetVar named values ────────
	//     Project-scoped (FK to projects, cascade delete). The name is the
	//     codegen identifier/register; its validity is enforced in the store
	//     layer (project_variables.go). See db_project_variables.go for the
	//     schema rationale.
	for _, stmt := range projectVariablesMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}

	// ── Stage prefs: per-user zoom/pan/cursor knobs for the IDE stage ──────
	//     Sparse rows; absence of a row (or NULL fields) falls back to the
	//     compile-time defaults in store.DefaultStagePrefs. See
	//     db_stage_prefs.go for the column rationale.
	for _, stmt := range stagePrefsMigrationStmts() {
		if _, err := DB.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}

// seedLocales inserts the initial supported locales.
// The display column is used in the registration form language selector.
// INSERT OR IGNORE — preserves any admin edits at runtime.
func seedLocales() error {
	seeds := []struct{ locale, bundleID, display string }{
		{"en-US", "en-US-seed", "English (US)"},
		{"pt-BR", "pt-BR-seed", "Português (BR)"},
	}
	for _, s := range seeds {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, display, updated_at)
			VALUES (?, ?, ?, datetime('now'))`,
			s.locale, s.bundleID, s.display,
		); err != nil {
			return err
		}
		// Update display name for existing rows that pre-date this column.
		if _, err := DB.Exec(`
			UPDATE i18n_bundles SET display = ? WHERE locale = ? AND display = ''`,
			s.display, s.locale,
		); err != nil {
			return err
		}
	}
	return nil
}

// seedProgrammingLanguages inserts the initial programming languages.
//
// The id is the canonical token used throughout the codebase to address
// the language. The display string is what shows in the UI dropdowns.
// New entries are appended to this slice and picked up on the next
// server start (INSERT OR IGNORE preserves any existing rows).
//
// Display-name changes need an explicit UPDATE step (see below) — the
// INSERT OR IGNORE preserves the row but does NOT refresh its columns.
// We migrate display names below the insert loop so renames land on
// existing databases too.
//
// Português: Insere as linguagens iniciais. Novas entradas vão sendo
// adicionadas; INSERT OR IGNORE não afeta linhas já existentes.
// Mudanças no display precisam de UPDATE explícito (abaixo do loop).
func seedProgrammingLanguages() error {
	langs := []struct {
		id, name, display string
		sortOrder         int
	}{
		{"golang", "golang", "Go", 1},
		// C99 is the second target language for the device wizard.
		// The parser (codegen/blackbox/parser_c.go) accepts code
		// following the IDS convention adapted to C99: comment-based
		// directives on the struct, `// prop:"..."` lines above
		// fields, and `<Struct>_<Method>(struct <Struct>* s, ...)`
		// receiver-style functions for methods (functions land in
		// later slices). See docs/CLAUDE_C99_DEVICE_SUPPORT.md for
		// the full convention.
		//
		// The display "C99" is intentional — it pins the language
		// version so the specialist knows which grammar the parser
		// targets. Arduino (a C++ dialect) will land as a separate
		// language row later; pinning the version now avoids
		// confusing the two.
		{"c", "c", "C99", 2},
	}
	for _, l := range langs {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO programming_languages (id, name, display, sort_order, created_at)
			VALUES (?, ?, ?, ?, datetime('now'))`,
			l.id, l.name, l.display, l.sortOrder,
		); err != nil {
			return err
		}
		// Display rename migration — keep the row's display column in
		// sync with the seed slice on every boot. Idempotent: when
		// they already match the UPDATE is a no-op. This is how we
		// roll out display renames (e.g. "C" → "C99") without
		// needing a dedicated migration system. The id is stable; the
		// display is the only column we treat as authored-here.
		if _, err := DB.Exec(`
			UPDATE programming_languages
			SET display = ?, sort_order = ?
			WHERE id = ?`,
			l.display, l.sortOrder, l.id,
		); err != nil {
			return err
		}
	}
	return nil
}

// seedProjectUILanguages inserts the initial project documentation languages.
func seedProjectUILanguages() error {
	uiLangs := []struct {
		id, code, display string
		sortOrder         int
	}{
		{"en", "en", "English", 1},
		{"pt-BR", "pt-BR", "Português (BR)", 2},
	}
	for _, l := range uiLangs {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_ui_languages (id, code, display, sort_order, created_at)
			VALUES (?, ?, ?, ?, datetime('now'))`,
			l.id, l.code, l.display, l.sortOrder,
		); err != nil {
			return err
		}
	}
	return nil
}

// seedProjectSettings inserts initial server-side configuration values.
// INSERT OR IGNORE ensures admin changes at runtime are never overwritten.
func seedProjectSettings() error {
	seeds := []struct {
		key, value, description string
	}{
		{
			SettingInviteRequired,
			"1",
			"When set to '1', new accounts require a valid invite code during " +
				"registration. Set to '0' to allow open registration. " +
				"Takes effect immediately without a server restart.",
		},
		{
			SettingInviteCodeExpiresDays,
			"7",
			"Number of days an unused invite code remains valid. " +
				"After expiry the code is rejected even if it was never used. " +
				"Valid range: 1–365.",
		},
		{
			SettingProfileBioMaxChars,
			"280",
			"Maximum character count for the user profile bio field. " +
				"Valid range: 50–2000.",
		},
		{
			SettingAvatarMaxBytes,
			"2097152",
			"Maximum avatar image file size in bytes (default: 2 097 152 = 2 MB). " +
				"Uploads larger than this limit are rejected with a 413 error.",
		},
		{
			SettingCardDescriptionMaxChars,
			"500",
			"Maximum character length for the card description field parsed from " +
				"readme.md frontmatter. Content exceeding this limit is silently " +
				"truncated on save. Valid range: 100–2000.",
		},
	}
	for _, s := range seeds {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_settings (key, value, description, updated_at)
			VALUES (?, ?, ?, datetime('now'))`,
			s.key, s.value, s.description,
		); err != nil {
			return err
		}
	}
	return nil
}

// seedCategories inserts the initial component taxonomy.
func seedCategories() error {
	categories := []struct {
		id, name  string
		sortOrder int
	}{
		{"sensors", "Sensors", 1},
		{"actuators", "Actuators", 2},
		{"communication", "Communication", 3},
		{"power", "Power Management", 4},
		{"display", "Display & Output", 5},
		{"storage", "Storage", 6},
		{"processing", "Processing & Logic", 7},
		{"networking", "Networking", 8},
	}
	for _, c := range categories {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_categories (id, name, sort_order, created_at)
			VALUES (?, ?, ?, datetime('now'))`,
			c.id, c.name, c.sortOrder,
		); err != nil {
			return err
		}
	}

	subcategories := []struct {
		id, categoryID, name string
		sortOrder            int
	}{
		{"sensors-optical", "sensors", "Optical", 1},
		{"sensors-temperature", "sensors", "Temperature", 2},
		{"sensors-motion", "sensors", "Motion", 3},
		{"sensors-pressure", "sensors", "Pressure", 4},
		{"sensors-chemical", "sensors", "Chemical", 5},
		{"sensors-distance", "sensors", "Distance", 6},
		{"sensors-magnetic", "sensors", "Magnetic", 7},
		{"sensors-sound", "sensors", "Sound", 8},
		{"actuators-motors", "actuators", "Motors", 1},
		{"actuators-servos", "actuators", "Servos", 2},
		{"actuators-relays", "actuators", "Relays", 3},
		{"actuators-leds", "actuators", "LEDs", 4},
		{"actuators-solenoids", "actuators", "Solenoids", 5},
		{"comm-i2c", "communication", "I2C", 1},
		{"comm-spi", "communication", "SPI", 2},
		{"comm-uart", "communication", "UART", 3},
		{"comm-canbus", "communication", "CAN Bus", 4},
		{"comm-1wire", "communication", "1-Wire", 5},
		{"power-battery", "power", "Battery", 1},
		{"power-regulators", "power", "Regulators", 2},
		{"power-chargers", "power", "Chargers", 3},
		{"display-oled", "display", "OLED", 1},
		{"display-lcd", "display", "LCD", 2},
		{"display-epaper", "display", "E-Paper", 3},
		{"display-rgbmatrix", "display", "RGB Matrix", 4},
		{"storage-flash", "storage", "Flash", 1},
		{"storage-eeprom", "storage", "EEPROM", 2},
		{"storage-sdcard", "storage", "SD Card", 3},
		{"proc-filters", "processing", "Filters / DSP", 1},
		{"proc-pid", "processing", "PID Controllers", 2},
		{"net-wifi", "networking", "Wi-Fi", 1},
		{"net-ble", "networking", "Bluetooth LE", 2},
		{"net-lora", "networking", "LoRa", 3},
		{"net-zigbee", "networking", "Zigbee", 4},
		{"net-ethernet", "networking", "Ethernet", 5},
	}
	for _, s := range subcategories {
		if _, err := DB.Exec(`
			INSERT OR IGNORE INTO project_subcategories (id, category_id, name, sort_order, created_at)
			VALUES (?, ?, ?, ?, datetime('now'))`,
			s.id, s.categoryID, s.name, s.sortOrder,
		); err != nil {
			return err
		}
	}
	return nil
}
