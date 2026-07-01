// server/store/db_help_files.go — Migration and seeds for the help-files
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// feature.
//
// Project help files are markdown / image / SVG assets that travel with a
// published device. The wizard authors them inside an in-IDE file manager
// before the project is published to GitHub. Examples:
//
//	readme.en.md            — project overview, English
//	readme.pt-br.md         — same overview in pt-BR
//	Init.en.md              — per-method help for Init()
//	examples/howTo.png      — stego-encoded example scene
//
// Storage rationale (locked in the design session 2026-05-02):
//
//   - SQLite blobs only — no filesystem cache. Quota is small enough
//     (50 MB-class per user) that BLOB storage is comfortable, and serving
//     via authenticated endpoint sidesteps both path traversal and the
//     atomicity hazards of "database PLUS disk".
//   - Per-project and per-user byte quotas, both readable from the
//     existing project_settings table. See the SettingHelpFilesMaxBytes*
//     constants in models.go and helpFilesSettingSeeds() below for the
//     defaults.
//   - Path layout permits at most one level of subdirectory. "examples/"
//     is the convention for stego-encoded scene PNGs, mirroring the layout
//     of devices already in the wild. The path validator in the handler
//     (validateHelpFilePath) enforces this; no schema-level CHECK.
//
// Per-user quota override is NOT implemented in this slice. When needed,
// add a `user_help_files_limits` table mirroring user_parser_limits (see
// db_parser_limits.go for the canonical shape) and tweak the handler to
// honour `userOverride OR globalDefault`. The migration here is forward-
// compatible: nothing in the schema will need to change for that.
package store

// helpFilesMigrationStmts returns the DDL statements for the help-files
// feature. Called by migrate() in db.go as part of the sequential migration
// chain. All statements are idempotent (CREATE TABLE IF NOT EXISTS / CREATE
// INDEX IF NOT EXISTS).
func helpFilesMigrationStmts() []string {
	return []string{

		// ── project_help_files ────────────────────────────────────────────
		//
		// Columns:
		//   project_id — FK to projects.id with ON DELETE CASCADE so deleting
		//                a project sweeps all its help files automatically.
		//   path       — relative path within the project, e.g.
		//                "readme.en.md" or "examples/howTo.png". The
		//                composite PK (project_id, path) makes the path
		//                unique within a project and gives O(1) lookup.
		//   mime_type  — MIME type derived server-side from the extension.
		//                Stored explicitly (rather than recomputed on every
		//                read) so a future config change to the extension
		//                whitelist doesn't break already-stored rows.
		//   content    — raw file bytes. Markdown is stored as UTF-8.
		//                BLOBs work for both text and binary; SQLite handles
		//                them transparently.
		//   size_bytes — denormalised length(content). Repeated to keep the
		//                quota-sum query (SELECT SUM(size_bytes) WHERE
		//                project_id IN ...) cheap; computing LENGTH(blob)
		//                across every row is much slower for large datasets.
		//                Always written together with content in a single
		//                statement, so cannot drift.
		//   updated_at — RFC3339 timestamp of the last write. Used to
		//                generate the ETag header on GET responses.
		`CREATE TABLE IF NOT EXISTS project_help_files (
			project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			path        TEXT NOT NULL,
			mime_type   TEXT NOT NULL,
			content     BLOB NOT NULL,
			size_bytes  INTEGER NOT NULL,
			updated_at  TEXT NOT NULL,
			PRIMARY KEY (project_id, path)
		);`,

		// Index used by ListHelpFiles (display the file tree) and by
		// SumProjectBytes (per-project quota check). Both queries scope
		// by project_id, so this index covers them with a single scan.
		`CREATE INDEX IF NOT EXISTS idx_help_files_project
			ON project_help_files (project_id);`,
	}
}

// helpFilesSettingSeeds returns the global default rows for project_settings
// related to help-file quotas. Called by migrate() with INSERT OR IGNORE so
// existing admin edits are preserved across server restarts.
//
// Default values:
//
//   - per project:  5_000_000 bytes (5 MB)
//   - per user:    50_000_000 bytes (50 MB)
//
// Rationale: a typical device repo with one readme, four method markdown
// files, and a handful of small example PNGs comes in under 500 KB. 5 MB
// per project covers richly-illustrated specialist devices; 50 MB per user
// covers a generous catalogue (10x the per-project cap) without inviting
// abuse. Raise either by UPDATE-ing the project_settings row at runtime;
// no restart required because the handlers read the value on every PUT.
func helpFilesSettingSeeds() []struct{ key, value, description string } {
	return []struct{ key, value, description string }{
		{
			SettingHelpFilesMaxBytesPerProject,
			"5000000",
			"Maximum total size in bytes for help files (markdown, images, " +
				"SVGs) belonging to a single project. Enforced on every " +
				"PUT/rename. Default: 5 MB.",
		},
		{
			SettingHelpFilesMaxBytesPerUser,
			"50000000",
			"Maximum total size in bytes for help files across every project " +
				"a user owns. Enforced on every PUT/rename. Default: 50 MB.",
		},
	}
}
