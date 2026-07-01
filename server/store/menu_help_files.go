// server/store/menu_help_files.go — CRUD for the menu_help_files table.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Mirrors server/store/project_help_files.go in shape, but scoped per
// menu slot rather than per project. The two share the same conceptual
// purpose: store binary assets (PNG, JPG, SVG, GIF, WebP) that markdown
// help references with relative paths like `![alt](./diagram.png)`,
// and let the serve-time path rewriter (RewriteImagePaths in
// server/codegen/blackbox/devicehelp.go) swap those references for
// inline `data:` URLs the browser can render without an authenticated
// fetch.
//
// Differences from project_help_files:
//
//   - Owner: menu_items.slot_id (admin-managed) instead of projects.id
//     (user-owned). The admin alone writes here; any authenticated user
//     reads (indirectly, by receiving the rendered markdown).
//
//   - No per-slot or per-user byte quota in the v1 of this slice.
//     Menu help is admin-only and the size envelope is trusted. If
//     abuse appears later, add SumMenuHelpBytes() and a setting key in
//     project_settings; the table schema needs no change.
//
// All functions here are pure data access; path validation, MIME
// mapping, and OTP authorisation live in the handler layer
// (server/handler/adminapi/menu_tree_handlers.go). Keeping the store
// layer lean means tests exercise the database without spinning up an
// HTTP server, and a future admin tool can reuse the same functions.
package store

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

// ─── Public errors ────────────────────────────────────────────────────────────

// ErrNoMenuHelpFile is returned by GetMenuHelpFile when the requested
// file does not exist for the given slot. Callers compare against this
// sentinel rather than checking the underlying sql.ErrNoRows so the
// storage layer can change driver without breaking callers.
//
// Distinct from ErrNoHelpFile (project files) to keep the call-site
// error handling unambiguous — a function that touches both pools
// should not accidentally catch the wrong sentinel.
var ErrNoMenuHelpFile = errors.New("menu help file not found")

// ErrMenuHelpPathConflict is returned by RenameMenuHelpFile when the
// destination path is already occupied by another row. The handler
// turns this into a 409 Conflict response. A rename should never
// silently overwrite.
//
// Mirrors ErrHelpPathConflict in project_help_files.go.
var ErrMenuHelpPathConflict = errors.New("menu help destination path already exists")

// ─── Public types ─────────────────────────────────────────────────────────────

// MenuHelpFile is the full row including content. Returned by
// GetMenuHelpFile and used wherever the file body is actually needed
// (e.g. inline image rewriting at serve time).
//
// JSON tags are explicit (camelCase) because the API contract — what
// the /control file-manager module sees — must be camelCase, but the
// struct fields are exported (PascalCase) for the rest of the Go
// package. Without these tags, encoding/json would emit "Path",
// "MimeType", etc., and the frontend would silently see undefined
// values.
type MenuHelpFile struct {
	SlotID    string    `json:"slotId"`
	Path      string    `json:"path"`
	MimeType  string    `json:"mimeType"`
	Content   []byte    `json:"content,omitempty"`
	SizeBytes int64     `json:"sizeBytes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MenuHelpFileMeta is the lightweight row used for listings — the
// content blob is omitted so a slot with many large images can be
// listed without loading every blob into memory. The serve-time image
// rewriter calls ListMenuHelpFiles to know which paths exist before
// fetching the blobs only for the ones that the markdown actually
// references.
//
// Same camelCase JSON-tag rationale as MenuHelpFile above.
type MenuHelpFileMeta struct {
	Path      string    `json:"path"`
	MimeType  string    `json:"mimeType"`
	SizeBytes int64     `json:"sizeBytes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetMenuHelpFile returns the full row including the content blob.
// Returns ErrNoMenuHelpFile if no row matches (slot_id, path).
//
// The two-argument lookup matches the table's composite primary key,
// which means SQLite uses the PK index directly with no scan.
func GetMenuHelpFile(slotID, path string) (*MenuHelpFile, error) {
	var (
		hf        MenuHelpFile
		updatedAt string
	)
	err := DB.QueryRow(`
		SELECT slot_id, path, mime_type, content, size_bytes, updated_at
		FROM menu_help_files
		WHERE slot_id = ? AND path = ?`,
		slotID, path,
	).Scan(
		&hf.SlotID, &hf.Path, &hf.MimeType,
		&hf.Content, &hf.SizeBytes, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoMenuHelpFile
	}
	if err != nil {
		return nil, err
	}
	hf.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &hf, nil
}

// ListMenuHelpFiles returns every help-file metadata row for the given
// slot, ordered by path. Content blobs are omitted to keep responses
// small — callers fetch individual files via GetMenuHelpFile when
// needed.
//
// The path order is lexicographic, the same convention used by
// project_help_files.ListHelpFiles. The serve-time rewriter does not
// rely on order; the order matters only for UI rendering of the file
// list, which prefers a stable alphabetical sort.
//
// Returns a non-nil empty slice when the slot has no files, so callers
// can json.Marshal the result without producing "null".
func ListMenuHelpFiles(slotID string) ([]MenuHelpFileMeta, error) {
	rows, err := DB.Query(`
		SELECT path, mime_type, size_bytes, updated_at
		FROM menu_help_files
		WHERE slot_id = ?
		ORDER BY path ASC`,
		slotID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MenuHelpFileMeta, 0)
	for rows.Next() {
		var (
			m         MenuHelpFileMeta
			updatedAt string
		)
		if err := rows.Scan(&m.Path, &m.MimeType, &m.SizeBytes, &updatedAt); err != nil {
			return nil, err
		}
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

// LoadAllMenuHelpImageURLs builds a (slot_id → filename → data: URL)
// nested map for every image stored in menu_help_files. Used by
// loadResolvedHelp to call RewriteImagePaths once per slot with the
// right per-slot pool of available images.
//
// Why a `data:` URL and not an authenticated endpoint URL — the
// rationale documented in attach_help_files.go applies verbatim here:
// the WASM renderer produces `<img src=...>` tags from the markdown,
// and the browser fetches those `src`s with only the cookie session,
// not the JavaScript-side Bearer header. An authenticated URL would
// 401 and the user would see a broken image. Inlining as base64
// sidesteps the auth problem entirely because the bytes travel inside
// the same authenticated JSON response that delivers the menu tree.
//
// Cost: base64 inflates each image by ~33%, and every menu render
// sends all images of all slots. With no quota in the v1 schema this
// is in theory unbounded; in practice menu images are diagrams /
// icons (tens of KB) and the catalogue has fewer than 100 slots.
// Acceptable for now. If the JSON ever grows uncomfortable, the next
// evolution is per-slot lazy loading driven by a markdown pre-scan
// for `![…](…)` references; this function's surface stays unchanged.
//
// Returns an empty (non-nil) outer map when there are no images at
// all, so callers can range over it safely.
func LoadAllMenuHelpImageURLs() (map[string]map[string]string, error) {
	rows, err := DB.Query(`
		SELECT slot_id, path, mime_type, content
		FROM menu_help_files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]map[string]string)
	for rows.Next() {
		var (
			slotID, path, mime string
			content            []byte
		)
		if err := rows.Scan(&slotID, &path, &mime, &content); err != nil {
			return nil, err
		}
		if out[slotID] == nil {
			out[slotID] = make(map[string]string)
		}
		// Encode as data: URL. Mirrors the attach_help_files.go
		// recipe; SVGs go through base64 too (not utf8-escaped data
		// URLs) because base64 is content-safe for any byte sequence
		// without XML/percent-encoding gymnastics.
		out[slotID][path] = "data:" + mime + ";base64," +
			base64.StdEncoding.EncodeToString(content)
	}
	return out, rows.Err()
}

// ─── Write ────────────────────────────────────────────────────────────────────

// SaveMenuHelpFile upserts a help-file row. The mime type is recomputed
// by the handler from the path's extension, so callers always pass the
// authoritative value here — clients cannot influence it (a PNG body
// submitted with path "evil.md" gets MIME stamped as markdown by the
// handler before reaching here, which is fine: garbage in, garbage out,
// with no security implication).
//
// updated_at is written as RFC3339 in UTC. The same format is used by
// GetMenuHelpFile / ListMenuHelpFiles when parsing back, so a
// round-trip preserves the timestamp exactly.
func SaveMenuHelpFile(slotID, path, mimeType string, content []byte) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO menu_help_files
			(slot_id, path, mime_type, content, size_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(slot_id, path) DO UPDATE SET
			mime_type  = excluded.mime_type,
			content    = excluded.content,
			size_bytes = excluded.size_bytes,
			updated_at = excluded.updated_at`,
		slotID, path, mimeType, content, len(content), now,
	)
	return err
}

// DeleteMenuHelpFile removes a single row. Returns whether a row was
// actually deleted; the handler uses this to give the client an honest
// answer rather than 404-ing on idempotent re-deletes.
//
// Mirrors project_help_files.DeleteHelpFile in shape so the handler
// boilerplate is symmetric.
func DeleteMenuHelpFile(slotID, path string) (bool, error) {
	res, err := DB.Exec(`
		DELETE FROM menu_help_files
		WHERE slot_id = ? AND path = ?`,
		slotID, path,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteAllMenuHelpFilesForSlot wipes every file row for the given
// slot. Called when an admin deletes a catalog entry from the Control
// Panel — the ON DELETE CASCADE on the FK already handles this
// automatically when menu_items rows are removed, but this helper is
// exposed for explicit pre-deletion cleanup (e.g. when the catalog
// row is renamed in place rather than deleted, or when an admin wants
// to clear an item's image pool without removing the item itself).
//
// Returns the number of rows deleted. Always returns a count, never
// an error sentinel for "no rows": deleting zero rows is a valid
// outcome (the pool was already empty).
func DeleteAllMenuHelpFilesForSlot(slotID string) (int64, error) {
	res, err := DB.Exec(`
		DELETE FROM menu_help_files
		WHERE slot_id = ?`,
		slotID,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RenameMenuHelpFile moves a file from oldPath to newPath within the
// same slot. Returns ErrNoMenuHelpFile if oldPath does not exist and
// ErrMenuHelpPathConflict if newPath already exists.
//
// Implemented as a transaction with explicit existence checks so the
// rename is atomic. The mime type is recomputed by the handler from
// the new extension before calling — passing it as an argument keeps
// the storage layer simple (one source of truth: the handler).
//
// Mirrors project_help_files.RenameHelpFile.
func RenameMenuHelpFile(slotID, oldPath, newPath, newMimeType string) error {
	if oldPath == newPath {
		// No-op: client asked to rename to the same name.
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Verify source exists. Without this we would silently no-op the
	// rename when the source is missing, masking client bugs.
	var dummy int
	err = tx.QueryRow(`
		SELECT 1 FROM menu_help_files
		WHERE slot_id = ? AND path = ?`,
		slotID, oldPath,
	).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoMenuHelpFile
	}
	if err != nil {
		return err
	}

	// Reject if destination is already taken. Catching this up front
	// gives a friendly error code; relying on PK uniqueness alone
	// would surface a generic "constraint failed" to the handler.
	err = tx.QueryRow(`
		SELECT 1 FROM menu_help_files
		WHERE slot_id = ? AND path = ?`,
		slotID, newPath,
	).Scan(&dummy)
	if err == nil {
		return ErrMenuHelpPathConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Update path and mime in place. Updating in place (rather than
	// INSERT+DELETE) preserves any auxiliary metadata that may be
	// added to the schema later without touching this code.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
		UPDATE menu_help_files
		SET path = ?, mime_type = ?, updated_at = ?
		WHERE slot_id = ? AND path = ?`,
		newPath, newMimeType, now, slotID, oldPath,
	); err != nil {
		return err
	}

	return tx.Commit()
}
