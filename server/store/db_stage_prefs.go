// server/store/db_stage_prefs.go — Migration for stage preferences storage.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Stage preferences are per-user knobs that tune the IDE's stage (the
// visual workspace where devices are placed and connected). Examples:
// zoom sensitivity, pan behaviour, cursor hints. These are distinct
// from the editor's menu preferences (already in stage_user_overrides)
// and from the user's profile (identity).
//
// English:
//
//	A user is not required to have a row in this table. Absence means
//	"use defaults from rulesSprite/rules.go". When the user opens the
//	Stage tab in Editor Settings for the first time and changes any
//	value, a row is created. Reset wipes the row, reverting to defaults.
//
// Português:
//
//	Um usuário não é obrigado a ter uma linha nesta tabela. Ausência
//	significa "usa defaults de rulesSprite/rules.go". Quando o usuário
//	abre a aba Stage em Editor Settings pela primeira vez e muda algum
//	valor, uma linha é criada. Reset apaga a linha, volta aos defaults.
//
// All statements are idempotent (CREATE TABLE IF NOT EXISTS).
package store

// stagePrefsMigrationStmts returns CREATE TABLE statements for the
// stage preferences feature. Called by migrate() in db.go.
//
// Column design notes:
//
//   - Each knob gets a dedicated column (not a JSON blob). The
//     IoTMaker preference set is small and bounded; typed columns
//     give us cheap validation, indexing options if we ever need
//     them, and sane behaviour in SQL tooling.
//
//   - Every knob column is nullable. NULL means "unset, fall back to
//     the default". Storing NULL instead of an explicit default
//     lets us change defaults later without migrating data — every
//     user who never touched that knob immediately picks up the
//     new default.
//
//   - zoom_step is REAL (SQLite double). The UI constrains it to
//     0.01–0.15 but the DB accepts any positive float.
//
//   - Boolean knobs are INTEGER 0/1 per SQLite convention.
//
//   - updated_at is always stamped on write so an admin can see
//     activity and we can migrate schemas gracefully later.
func stagePrefsMigrationStmts() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS stage_prefs (
			user_id                TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			zoom_step              REAL,
			pan_empty_area         INTEGER,
			show_grab_cursor       INTEGER,
			updated_at             TEXT NOT NULL
		);`,
	}
}
