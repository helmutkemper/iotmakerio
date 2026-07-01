// server/store/db_api_keys_table.go — DDL for the api_keys table.
//
// API keys provide device-scoped authentication for webhook endpoints.
// Each key is tied to exactly one (user, project, device) triple,
// ensuring that a compromised key only grants access to a single device.
//
// The raw key is never stored — only its SHA-256 hash. Keys have no
// mandatory expiration because IoT hardware deployed in the field
// cannot easily rotate credentials.
//
// See api_keys.go for the CRUD operations and validation logic.
package store

// apiKeyMigrationStmts returns CREATE TABLE and CREATE INDEX statements
// for the api_keys table. Called by migrate() in db.go.
func apiKeyMigrationStmts() []string {
	return []string{
		// ── API Keys ────────────────────────────────────────────────────────
		// One row per project-scoped credential. The key_hash column stores
		// the SHA-256 hex digest of the raw key (never the raw key itself).
		// device_id is kept for legacy/informational purposes but is NOT
		// used for authentication — keys are validated by project_id only.
		//
		// expires_at is nullable: NULL means the key never expires.
		// revoked_at is nullable: NULL means the key is active.
		`CREATE TABLE IF NOT EXISTS api_keys (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id TEXT NOT NULL,
			device_id  TEXT NOT NULL DEFAULT '',
			key_hash   TEXT NOT NULL UNIQUE,
			label      TEXT NOT NULL DEFAULT '',
			expires_at TEXT,
			revoked_at TEXT,
			created_at TEXT NOT NULL
		);`,

		// Fast lookup by hash during webhook validation (the hot path).
		`CREATE INDEX IF NOT EXISTS idx_apikeys_hash
			ON api_keys(key_hash);`,

		// List all keys for a project (management UI).
		`CREATE INDEX IF NOT EXISTS idx_apikeys_user_project
			ON api_keys(user_id, project_id, created_at DESC);`,
	}
}
