// store/db_community_tables.go — Migration for community interaction tables.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Tables added here:
//
//	project_comments — user reviews/comments on projects, with optional
//	                   sub-ratings for documentation and code quality.
//	project_reports  — user reports filed against projects for moderation.
//
// Both files follow the same idempotent pattern as db_feed_tables.go:
//
//	CREATE TABLE IF NOT EXISTS + INSERT OR IGNORE for seeds.
//	The server calls communityMigrationStmts() from migrate() in db.go.
//
// Why separate file?
//
//	Keeping each feature's migration in its own file makes code review
//	easier and reduces the risk of merge conflicts on db.go as the project
//	grows. All migration files are called in sequence from db.go.
package store

// communityMigrationStmts returns the DDL for the community interaction tables.
// Called by migrate() in db.go during startup.
func communityMigrationStmts() []string {
	return []string{

		// ── Project Comments ─────────────────────────────────────────────────
		// One row per user comment on a project. Comments are append-only —
		// no UPDATE endpoint exists. A user can delete their own comment;
		// admins can delete any comment.
		//
		// doc_rating and code_rating are optional sub-ratings (0 = not provided,
		// 1–5 = quality score). They are stored as INTEGER so the DB CHECK
		// constraint handles validation without application-layer gymnastics.
		//
		// A single user can post multiple comments on the same project —
		// there is intentionally no UNIQUE(user_id, project_id) constraint here
		// because additional observations over time are valuable. Rate-limiting
		// should be applied at the handler level if needed.
		`CREATE TABLE IF NOT EXISTS project_comments (
			id          TEXT PRIMARY KEY,
			project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			body        TEXT NOT NULL DEFAULT '',
			doc_rating  INTEGER NOT NULL DEFAULT 0
			             CHECK(doc_rating  BETWEEN 0 AND 5),
			code_rating INTEGER NOT NULL DEFAULT 0
			             CHECK(code_rating BETWEEN 0 AND 5),
			created_at  TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_comments_project
		    ON project_comments(project_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_comments_user
		    ON project_comments(user_id, created_at DESC);`,

		// ── Project Reports ──────────────────────────────────────────────────
		// One report per (user, project) pair — UNIQUE enforces this.
		// Attempting a second report on the same project returns UNIQUE
		// constraint error, which the store maps to ErrConflict.
		//
		// reason: one of the fixed vocabulary strings defined in models.go
		//         (offensive, off_topic, spam, misleading). Validated by the
		//         handler before INSERT; the DB has no CHECK so the vocabulary
		//         can evolve without a migration.
		//
		// details: optional free-text context for moderators (max 500 chars,
		//          enforced by the handler).
		//
		// status: pending → reviewed | dismissed. Managed by future admin UI.
		`CREATE TABLE IF NOT EXISTS project_reports (
			id         TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			reason     TEXT NOT NULL,
			details    TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(user_id, project_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_reports_project
		    ON project_reports(project_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_reports_status
		    ON project_reports(status, created_at DESC);`,
	}
}

// communitySettingSeeds returns the project_settings rows for community features.
func communitySettingSeeds() []struct{ key, value, description string } {
	return []struct{ key, value, description string }{
		{
			SettingCommentMaxChars,
			"1000",
			"Maximum character length for a project comment body. " +
				"Content exceeding this limit is rejected with a 400 error. " +
				"Valid range: 100–5000.",
		},
		{
			SettingCommentPageSize,
			"10",
			"Number of comments returned per page in GET /projects/:id/comments. " +
				"Valid range: 5–50.",
		},
	}
}
