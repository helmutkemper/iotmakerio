// server/store/db_stage_files.go — Migration for stage file storage tables.
//
// Stage files are the user's saved IDE scenes — JSON snapshots of the canvas
// (devices, wires, containment, camera state). They are completely independent
// from the portal's "projects" feature; a maker can save and load scenes
// without ever creating a project.
//
// Virtual folders give the user a familiar directory structure for organisation.
// Folders can be nested to arbitrary depth via the self-referencing parent_id.
//
// Limit resolution (highest priority first):
//
//  1. Per-user override   — stage_file_user_limits.max_files
//  2. Per-group override  — stage_file_group_limits.max_files (user's group)
//  3. Global setting      — project_settings key "stage_file_max_per_user"
//  4. Hard fallback       — DefaultStageFileMaxPerUser (compile-time constant)
//
// All statements are idempotent (CREATE TABLE IF NOT EXISTS / INSERT OR IGNORE).
package store

// stageFileMigrationStmts returns all CREATE TABLE and CREATE INDEX statements
// for the stage file feature. Called by migrate() in db.go.
func stageFileMigrationStmts() []string {
	return []string{

		// ── Virtual folders ─────────────────────────────────────────────────
		// parent_id NULL = root folder. Self-referencing FK allows nested folders.
		// UNIQUE(user_id, parent_id, name) prevents duplicate folder names at
		// the same level for the same user. SQLite treats each NULL as distinct
		// in UNIQUE constraints, so two root-level folders with the same name
		// are correctly rejected thanks to the COALESCE trick in the unique index.
		`CREATE TABLE IF NOT EXISTS stage_folders (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			parent_id  TEXT REFERENCES stage_folders(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		// Unique index using COALESCE so that NULL parent_id values are treated
		// as equal (SQLite UNIQUE considers NULLs distinct by default).
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sf_folder_unique
			ON stage_folders(user_id, COALESCE(parent_id, ''), name);`,
		`CREATE INDEX IF NOT EXISTS idx_sf_folder_user
			ON stage_folders(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sf_folder_parent
			ON stage_folders(parent_id);`,

		// ── Stage files ─────────────────────────────────────────────────────
		// folder_id NULL = file lives at the root level.
		// scene_json stores the full SceneJSON produced by scene.Serializer.Export().
		// device_count is denormalised for display purposes — updated on every save.
		//
		// The `kind` column discriminates between regular stage files ('stage',
		// the default) and tutorial files ('tutorial'). Tutorial files carry a
		// tutorial object inside scene_json alongside the normal scene section;
		// see /ide/docs/DELIVERY_C_TUTORIAL_DESIGN.md §3 for the file format.
		// The default value 'stage' preserves backwards compatibility — any file
		// written before this column existed (or by a client that omits the field)
		// reads back as a regular stage file.
		//
		// The `language` column captures the target source language of the
		// project — 'c' for C99 (default — the choice when the user closes
		// the welcome modal without picking) or 'go' for Go. The language is
		// fixed at project creation and irreversible: a project is 100% Go
		// or 100% C99 from the moment it is created. The IDE filters its
		// device catalogue and code-export options by this value, so a C99
		// project never sees Go-only devices in the menu and never receives
		// "Export Go Code" as an action.
		//
		// Português: Coluna `kind` distingue arquivos normais de stage ('stage',
		// padrão) de arquivos de tutorial ('tutorial'). Arquivos de tutorial
		// trazem um objeto tutorial dentro de scene_json junto com a seção scene.
		// Coluna `language` captura a linguagem-alvo do projeto: 'c' para C99
		// (padrão — escolha quando o usuário fecha o modal sem escolher) ou
		// 'go' para Go. A linguagem é fixada na criação do projeto e é
		// irreversível: 100% Go ou 100% C99 desde o início. A IDE filtra o
		// catálogo de devices e os items de export por esse valor.
		`CREATE TABLE IF NOT EXISTS stage_files (
			id           TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			folder_id    TEXT REFERENCES stage_folders(id) ON DELETE SET NULL,
			name         TEXT NOT NULL,
			kind         TEXT NOT NULL DEFAULT 'stage',
			language     TEXT NOT NULL DEFAULT 'c',
			-- icon_id is a FontAwesome Free icon name (e.g. "cpu", "thermometer").
			-- NULL or empty means "no icon chosen" and the UI renders a default
			-- (today: "cube") rather than tofu. The server validates only the
			-- format ([a-z0-9-]+); whether the name actually resolves to a free
			-- icon is the client's responsibility — the client filters its
			-- picker against window.FA_FREE_STYLES, so the only way to arrive
			-- at an invalid name is a direct API call, in which case the worst
			-- outcome is a tofu glyph the maker can fix by editing the file.
			icon_id      TEXT,
			scene_json   TEXT NOT NULL DEFAULT '{}',
			device_count INTEGER NOT NULL DEFAULT 0,
			is_backup    INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sf_file_unique
			ON stage_files(user_id, COALESCE(folder_id, ''), name);`,
		`CREATE INDEX IF NOT EXISTS idx_sf_file_user
			ON stage_files(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sf_file_folder
			ON stage_files(folder_id);`,
		// Supports the user-facing Tutorials tab: lists every file where
		// kind = 'tutorial'. Cheap index because the cardinality on kind
		// is small (two values today) and tutorials are a small fraction
		// of total rows.
		`CREATE INDEX IF NOT EXISTS idx_sf_file_kind
			ON stage_files(kind);`,

		// ── Per-user file limit override ─────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS stage_file_user_limits (
			user_id   TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			max_files INTEGER NOT NULL
		);`,

		// ── Per-group file limit override ────────────────────────────────────
		`CREATE TABLE IF NOT EXISTS stage_file_group_limits (
			group_id  TEXT PRIMARY KEY REFERENCES user_groups(id) ON DELETE CASCADE,
			max_files INTEGER NOT NULL
		);`,
	}
}

// stageFileSettingSeeds returns INSERT OR IGNORE seeds for the global limit.
func stageFileSettingSeeds() []struct{ key, value, description string } {
	return []struct{ key, value, description string }{
		{
			SettingStageFileMaxPerUser,
			"50",
			"Maximum number of stage files (saved scenes) per user. " +
				"Overridden by stage_file_group_limits or stage_file_user_limits when set.",
		},
	}
}
