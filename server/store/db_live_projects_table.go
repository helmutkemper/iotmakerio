// server/store/db_live_projects_table.go — DDL for the live_projects table.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// A live project groups all live communication resources (API keys, WebSocket
// connections) under a unique server-generated ID. The project name is a
// human-friendly label chosen by the user; the project ID is used in webhook
// URLs and WebSocket channels.
//
// Storing projects server-side means the configuration follows the user
// across machines — no localStorage dependency for critical data.
//
// Português:
//
//	Um live project agrupa todos os recursos de comunicação live sob um ID
//	único gerado pelo servidor. O nome é amigável; o ID é usado em URLs.
//	Armazenamento server-side permite acessar de qualquer máquina.
package store

// liveProjectMigrationStmts returns CREATE TABLE and CREATE INDEX statements
// for the live_projects table. Called by migrate() in db.go.
func liveProjectMigrationStmts() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS live_projects (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,

		// One user lists their projects.
		`CREATE INDEX IF NOT EXISTS idx_liveproj_user
			ON live_projects(user_id, created_at DESC);`,

		// Unique name per user — prevents confusion.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_liveproj_user_name
			ON live_projects(user_id, name);`,
	}
}
