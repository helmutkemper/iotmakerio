// store/db_parser_limits.go — Migration and seed for parser complexity limits.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Two persistence mechanisms work together:
//
//  1. project_settings rows (keys parser_max_*) — the global defaults that
//     apply to every parse call. Seeded on startup with safe values. Editable
//     by an admin via the settings API or directly in the database.
//
//  2. user_parser_limits table — per-user overrides. When a row exists for a
//     given (user_id, limit_key) pair, that value is used instead of the
//     global default. This lets an admin grant a trusted specialist a higher
//     limit (e.g. 64 methods) without raising the global ceiling for everyone.
//
// Both tables are created with IF NOT EXISTS so the migration is idempotent
// and safe to run on every server startup.
package store

// parserLimitMigrationStmts returns the DDL statements for the parser limits
// feature. Called from store.Open() as part of the sequential migration chain.
func parserLimitMigrationStmts() []string {
	return []string{
		// user_parser_limits: per-user overrides for any parser_max_* key.
		//
		// Columns:
		//   user_id   — FK to users.id (no FK constraint: SQLite FK enforcement
		//               is off by default and we prefer soft references here).
		//   limit_key — one of the SettingParser* constants (e.g. "parser_max_methods").
		//   value     — integer value as TEXT (matches project_settings.value type).
		//   note      — optional admin annotation (why this user has a higher limit).
		//   updated_at — ISO-8601 timestamp of last change.
		//
		// The (user_id, limit_key) pair is the primary key, which enforces
		// uniqueness and provides O(1) lookup for the hot-path resolver.
		`CREATE TABLE IF NOT EXISTS user_parser_limits (
			user_id    TEXT NOT NULL,
			limit_key  TEXT NOT NULL,
			value      TEXT NOT NULL,
			note       TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, limit_key)
		)`,

		// Index for listing all limits of a given user (used by the admin panel).
		`CREATE INDEX IF NOT EXISTS idx_upl_user
			ON user_parser_limits (user_id)`,
	}
}

// parserLimitSettingSeeds returns the global default rows for project_settings.
// Each seed uses INSERT OR IGNORE so existing admin edits are preserved across
// server restarts.
func parserLimitSettingSeeds() []struct{ key, value, description string } {
	return []struct{ key, value, description string }{
		{
			SettingParserMaxMethods,
			"32",
			"Maximum number of exported non-Init methods per device struct. " +
				"Exceeding this limit is a hard parse error — the component is rejected. " +
				"Increase only for specialist users via user_parser_limits. " +
				"Valid range: 1–256. Default: 32.",
		},
		{
			SettingParserMaxInputs,
			"16",
			"Maximum number of input ports (parameters) per method. " +
				"Excess inputs are silently truncated and a soft warning is emitted. " +
				"Valid range: 1–64. Default: 16.",
		},
		{
			SettingParserMaxOutputs,
			"16",
			"Maximum number of output ports (return values) per method. " +
				"Excess outputs are silently truncated and a soft warning is emitted. " +
				"Valid range: 1–64. Default: 16.",
		},
		{
			SettingParserMaxProps,
			"32",
			"Maximum number of prop-tagged struct fields per device. " +
				"Excess props are silently truncated and a soft warning is emitted. " +
				"Valid range: 1–128. Default: 32.",
		},
	}
}
