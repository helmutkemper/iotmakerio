// server/store/wizard_drafts.go — Persistence for the wizard tab's drafts.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Why this file exists
// ====================
//
// The wizard tab gives users a long-running editing experience: they
// open a project, type Go in Monaco, click cards to open modals, save
// help files, stage images, and eventually publish to GitHub. Doing
// all of that without persistence would punish the user — close the
// tab and lose hours of work. CLAUDE_WIZARD_DESIGN.md §7 mandates a
// drafts table, keyed by (project_id, user_id), with a 30-day idle
// retention.
//
// One draft per (project, user)
// =============================
//
// A user has at most one wizard draft per project. Opening the wizard
// always loads (or creates) the same row; saving always replaces it.
// The UNIQUE(project_id, user_id) index enforces this at the database
// level — application code never has to reconcile duplicates.
//
// HMAC removal note (slice 3 follow-up)
// =====================================
//
// An earlier iteration of this file shipped a `Compute/Verify
// WizardHMAC` pair plus a per-server secret stored in
// `project_settings.wizard_hmac_secret`. The idea was a "sanity
// check" between the BlackBoxDef the server emitted and what the
// client echoed back on save. That check was unreliable
// (JSON.parse → JSON.stringify in the browser is not byte-stable)
// and misframed: the real integrity concern is at publish time,
// when the BlackBoxDef JSON gets pushed to a public GitHub release
// and any other user can re-import it. That is a slice 8 problem,
// and slice 8 will pick its own signing scheme.
//
// The dead helpers were removed. The `parsed_hmac` column survives
// in the schema (NOT NULL DEFAULT ”) so we don't have to migrate
// existing rows; UpsertWizardDraft writes the empty string and
// GetWizardDraft reads it but ignores the value. If slice 8 wants
// to repurpose the column, it can; if it picks a different shape,
// the column is harmless dead weight.
//
// Cleanup
// =======
//
// Drafts are deleted after 30 days without an UpdatedAt bump. The
// Asynq scheduled task in `server/tasks/wizard_cleanup.go` calls
// CleanupOldWizardDrafts daily. Tests can pass a custom maxAge.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	cryptoauth "server/auth"
)

// =============================================================================
//  Types
// =============================================================================

// WizardDraft is one row from the wizard_drafts table.
//
// All JSON fields are stored as raw text — never decoded inside the
// store layer. The handler layer is responsible for marshal/unmarshal
// because it owns the BlackBoxDef and []string types and we don't
// want a circular import (codegen/blackbox imports nothing from store).
type WizardDraft struct {
	// ID is a UUID generated on first insert. Stable across updates;
	// the (project_id, user_id) tuple is the natural key for lookups,
	// but the ID is useful for log lines and admin tooling.
	ID string

	// ProjectID and UserID together form the unique key. Both are
	// required; an empty value is rejected at insert time.
	ProjectID string
	UserID    string

	// Source is the full Go file as it stands. Always present.
	Source string

	// ParsedJSON is the BlackBoxDef as JSON, exactly as the server
	// emitted it on the last successful parse/rewrite. Never modified
	// by the store — pass-through bytes.
	ParsedJSON string

	// ParsedHMAC is the hex-encoded HMAC-SHA256 of ParsedJSON, computed
	// with the server-only secret. Verified on POST /draft.
	ParsedHMAC string

	// CompletionJSON is the JSON-encoded []string of incomplete paths,
	// recomputed by the server from ParsedJSON on every save.
	CompletionJSON string

	// ImagesJSON, ImagesBytes — staged-image bookkeeping. Slices 7+
	// will populate these; slice 3 stores empty list and zero.
	ImagesJSON  string
	ImagesBytes int64

	// HelpsJSON — staged help markdown bookkeeping. Slice 7+.
	HelpsJSON string

	// CreatedAt and UpdatedAt are Unix-seconds. Updated by Upsert.
	CreatedAt int64
	UpdatedAt int64
}

// =============================================================================
//  Migration
// =============================================================================

// wizardDraftsMigrationStmts returns the CREATE TABLE / CREATE INDEX
// statements for this file's table. Called from store.migrate (in db.go)
// alongside the menu / commerce / parser-limits migrators. Keeping the
// SQL with the CRUD code makes schema changes a single-file edit.
func wizardDraftsMigrationStmts() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS wizard_drafts (
			id              TEXT PRIMARY KEY,
			project_id      TEXT NOT NULL,
			user_id         TEXT NOT NULL,
			source          TEXT NOT NULL,
			parsed_json     TEXT NOT NULL,
			parsed_hmac     TEXT NOT NULL,
			completion_json TEXT NOT NULL,
			images_json     TEXT NOT NULL DEFAULT '[]',
			images_bytes    INTEGER NOT NULL DEFAULT 0,
			helps_json      TEXT NOT NULL DEFAULT '[]',
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			UNIQUE(project_id, user_id)
		);`,
		`CREATE INDEX IF NOT EXISTS wizard_drafts_user_idx
			ON wizard_drafts(user_id);`,
		`CREATE INDEX IF NOT EXISTS wizard_drafts_updated_idx
			ON wizard_drafts(updated_at);`,
	}
}

// =============================================================================
//  CRUD
// =============================================================================

// ErrWizardDraftNotFound is returned by GetWizardDraft when no draft
// exists for the (userID, projectID) pair. Distinct from sql.ErrNoRows
// so the handler layer can match it without importing database/sql.
var ErrWizardDraftNotFound = errors.New("wizard draft not found")

// GetWizardDraft returns the draft for the given user and project, or
// ErrWizardDraftNotFound when none exists. Reading a draft does NOT
// bump UpdatedAt — only Upsert does.
func GetWizardDraft(userID, projectID string) (*WizardDraft, error) {
	if userID == "" || projectID == "" {
		return nil, fmt.Errorf("userID and projectID are required")
	}
	row := DB.QueryRow(`
		SELECT id, project_id, user_id, source, parsed_json, parsed_hmac,
		       completion_json, images_json, images_bytes, helps_json,
		       created_at, updated_at
		  FROM wizard_drafts
		 WHERE user_id = ? AND project_id = ?`,
		userID, projectID,
	)
	var d WizardDraft
	err := row.Scan(
		&d.ID, &d.ProjectID, &d.UserID, &d.Source, &d.ParsedJSON, &d.ParsedHMAC,
		&d.CompletionJSON, &d.ImagesJSON, &d.ImagesBytes, &d.HelpsJSON,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWizardDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wizard draft: %w", err)
	}
	return &d, nil
}

// UpsertWizardDraft inserts or updates the draft identified by the
// (UserID, ProjectID) pair. ID is generated on insert and preserved
// on update; the caller does NOT need to populate it.
//
// CreatedAt is set on insert and preserved on update; UpdatedAt is
// always bumped to time.Now().Unix() on every call.
//
// SQLite's `INSERT ... ON CONFLICT(...) DO UPDATE SET ...` is the
// portable way to do an upsert against the unique (project_id,
// user_id) index. The conflict clause names the unique columns
// explicitly so a future schema change to the index name doesn't
// silently break this query.
func UpsertWizardDraft(d *WizardDraft) error {
	if d == nil {
		return fmt.Errorf("nil draft")
	}
	if d.UserID == "" || d.ProjectID == "" {
		return fmt.Errorf("userID and projectID are required")
	}
	if d.ID == "" {
		// MustNewID generates a UUID via crypto/rand. Other store callers
		// (blackbox.go, commerce.go, …) usually expect the handler to
		// pre-generate the ID; here we generate it inside the store
		// because the handler has no use for the value before insert
		// and forcing it to import cryptoauth just for that would be
		// noise.
		d.ID = cryptoauth.MustNewID()
	}
	now := time.Now().Unix()
	d.UpdatedAt = now

	// Default the JSON columns to "[]" when the caller leaves them
	// empty, matching the column DEFAULTs in the schema. Without this
	// an empty Go string would violate the NOT NULL contract on insert.
	if d.ImagesJSON == "" {
		d.ImagesJSON = "[]"
	}
	if d.HelpsJSON == "" {
		d.HelpsJSON = "[]"
	}

	_, err := DB.Exec(`
		INSERT INTO wizard_drafts (
			id, project_id, user_id, source, parsed_json, parsed_hmac,
			completion_json, images_json, images_bytes, helps_json,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, user_id) DO UPDATE SET
			source          = excluded.source,
			parsed_json     = excluded.parsed_json,
			parsed_hmac     = excluded.parsed_hmac,
			completion_json = excluded.completion_json,
			-- images_json/helps_json/images_bytes are managed by their own
			-- endpoints; preserving them on a draft save keeps them from
			-- being clobbered by a parse/rewrite round-trip that doesn't
			-- carry image state.
			updated_at      = excluded.updated_at`,
		d.ID, d.ProjectID, d.UserID, d.Source, d.ParsedJSON, d.ParsedHMAC,
		d.CompletionJSON, d.ImagesJSON, d.ImagesBytes, d.HelpsJSON,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert wizard draft: %w", err)
	}
	if d.CreatedAt == 0 {
		// Either this was a fresh insert (where created_at == updated_at)
		// or an upsert that did not touch created_at. Re-read so the
		// returned struct reflects what's actually persisted.
		row := DB.QueryRow(
			`SELECT created_at FROM wizard_drafts WHERE user_id = ? AND project_id = ?`,
			d.UserID, d.ProjectID,
		)
		_ = row.Scan(&d.CreatedAt)
	}
	return nil
}

// DeleteWizardDraft removes the draft for the given (user, project).
// Returns nil whether or not a row existed — the operation is
// idempotent so the wizard's "Cancel" button can be wired without
// special-casing the no-draft case.
func DeleteWizardDraft(userID, projectID string) error {
	if userID == "" || projectID == "" {
		return fmt.Errorf("userID and projectID are required")
	}
	_, err := DB.Exec(
		`DELETE FROM wizard_drafts WHERE user_id = ? AND project_id = ?`,
		userID, projectID,
	)
	if err != nil {
		return fmt.Errorf("delete wizard draft: %w", err)
	}
	return nil
}

// CleanupOldWizardDrafts deletes drafts whose updated_at is older than
// maxAge from now. Returns the number of rows deleted, which the
// Asynq task logs at INFO so admins can spot anomalies (e.g. a sudden
// 10x increase suggesting a worker outage backlog).
//
// maxAge is a Duration so tests can pass nanoseconds and force every
// row to expire. Production callers pass 30 * 24 * time.Hour.
func CleanupOldWizardDrafts(maxAge time.Duration) (int64, error) {
	if maxAge <= 0 {
		return 0, fmt.Errorf("maxAge must be positive")
	}
	cutoff := time.Now().Add(-maxAge).Unix()
	res, err := DB.Exec(
		`DELETE FROM wizard_drafts WHERE updated_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup wizard drafts: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		// SQLite always supports RowsAffected, so this branch is
		// effectively unreachable in our deployment. Reported as an
		// error anyway because hiding it would mask a real driver
		// problem if we ever migrate to one that doesn't.
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
