// store/db_feed_tables.go — Migration additions for the feed feature.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// This file is separate from db.go to keep the migration list manageable
// as the project grows. The init() function appends new statements and seeds
// to the global migration runners at startup.
//
// To apply these tables to an existing database, just restart the server —
// all CREATE TABLE IF NOT EXISTS and INSERT OR IGNORE statements are idempotent.
package store

// feedMigrationStmts returns the DDL for the three feed tables.
// Called by migrate() in db.go via FeedMigrationHook.
func feedMigrationStmts() []string {
	return []string{

		// ── Project Ratings ───────────────────────────────────────────────────
		// One row per (user, project) pair. UNIQUE enforces the one-rating rule.
		// Aggregate stats (avg, count) are computed at query time in feed.go —
		// never stored as columns to avoid stale data.
		`CREATE TABLE IF NOT EXISTS project_ratings (
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			rating     INTEGER NOT NULL CHECK(rating BETWEEN 1 AND 5),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, project_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ratings_project ON project_ratings(project_id);`,

		// ── User Follows ──────────────────────────────────────────────────────
		// (follower_id, following_id) — who follows whom.
		// Both directions are indexed for cheap lookups in either direction.
		`CREATE TABLE IF NOT EXISTS user_follows (
			follower_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			following_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at   TEXT NOT NULL,
			PRIMARY KEY (follower_id, following_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_follows_following ON user_follows(following_id);`,

		// ── Feed Events ───────────────────────────────────────────────────────
		// Activity log for the "Following" feed tab. One row per noteworthy
		// event (project created, code version saved, readme updated).
		// The deduplication logic in LogFeedEvent prevents bursts of the same
		// event type within 1 hour from flooding the feed.
		`CREATE TABLE IF NOT EXISTS feed_events (
			id          TEXT PRIMARY KEY,
			project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			event_type  TEXT NOT NULL,
			created_at  TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_feed_events_project  ON feed_events(project_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_feed_events_user     ON feed_events(user_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_feed_events_created  ON feed_events(created_at DESC);`,
	}
}

// feedSettingSeeds returns the project_settings rows for the feed feature.
func feedSettingSeeds() []struct{ key, value, description string } {
	return []struct{ key, value, description string }{
		{
			SettingFeedPageSize,
			"24",
			"Number of cards returned per feed page. " +
				"Applies to all feed tabs (Recent, Popular, Discover, Following). " +
				"Valid range: 12–100.",
		},
	}
}
