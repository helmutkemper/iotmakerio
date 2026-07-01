// server/store/project_help_files.go — CRUD for the project_help_files
// table.
//
// All functions here are pure data access; path validation, MIME mapping,
// quota enforcement, and authorisation live in the handler layer
// (server/handler/projectapi/help_files.go). Keeping the store layer
// lean means tests can exercise the database without spinning up an HTTP
// server, and a future admin tool can reuse the same functions.
//
// See docs/tasks/HELP_FILES_FEATURE.md for the design rationale.
package store

import (
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// ─── Public errors ────────────────────────────────────────────────────────────

// ErrNoHelpFile is returned by GetHelpFile when the requested file does not
// exist for the given project. Callers compare against this sentinel rather
// than checking the underlying sql.ErrNoRows so the storage layer can change
// driver without breaking callers.
var ErrNoHelpFile = errors.New("help file not found")

// ErrHelpPathConflict is returned by RenameHelpFile when the destination
// path already exists. A rename should never silently overwrite — the
// handler catches this and returns 409 Conflict.
var ErrHelpPathConflict = errors.New("destination path already exists")

// ─── Public types ─────────────────────────────────────────────────────────────

// HelpFile is the full row including content. Returned by GetHelpFile and
// used internally by RenameHelpFile to copy bytes into the new row.
//
// JSON tags are explicit (camelCase) because the API contract — what the
// frontend file-manager module sees — must be camelCase, but the struct
// fields are exported (PascalCase) for the rest of the Go package. Without
// these tags, encoding/json would emit "Path", "MimeType", etc., and the
// frontend would silently see undefined values.
type HelpFile struct {
	ProjectID string    `json:"projectId"`
	Path      string    `json:"path"`
	MimeType  string    `json:"mimeType"`
	Content   []byte    `json:"content,omitempty"`
	SizeBytes int64     `json:"sizeBytes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// HelpFileMeta is the lightweight row used for directory listings — the
// content blob is omitted so a project with many large files can be
// listed without loading every blob into memory. The wizard's file
// manager calls ListHelpFiles to render the file tree.
//
// Same camelCase JSON-tag rationale as HelpFile above.
type HelpFileMeta struct {
	Path      string    `json:"path"`
	MimeType  string    `json:"mimeType"`
	SizeBytes int64     `json:"sizeBytes"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetHelpFile returns the full row including the content blob.
// Returns ErrNoHelpFile if no row matches (project_id, path).
func GetHelpFile(projectID, path string) (*HelpFile, error) {
	var (
		hf        HelpFile
		updatedAt string
	)
	err := DB.QueryRow(`
		SELECT project_id, path, mime_type, content, size_bytes, updated_at
		FROM project_help_files
		WHERE project_id = ? AND path = ?`,
		projectID, path,
	).Scan(
		&hf.ProjectID, &hf.Path, &hf.MimeType,
		&hf.Content, &hf.SizeBytes, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoHelpFile
	}
	if err != nil {
		return nil, err
	}
	hf.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &hf, nil
}

// ListHelpFiles returns every help-file metadata row for the given project,
// ordered by path. Content blobs are omitted to keep responses small —
// callers fetch individual files via GetHelpFile when needed.
//
// The path order matches the wizard's display order: lexicographic on the
// full path string, which puts root files before "examples/" entries
// because "/" sorts after letters in ASCII. (`readme.en.md` <
// `examples/foo.png` because lowercase 'r' < lowercase 'e'? actually
// reverse — but ASCII: 'e' (0x65) < 'r' (0x72), so examples/* sorts
// first. Either order is fine for the UI; alphabetical is the contract.)
func ListHelpFiles(projectID string) ([]HelpFileMeta, error) {
	rows, err := DB.Query(`
		SELECT path, mime_type, size_bytes, updated_at
		FROM project_help_files
		WHERE project_id = ?
		ORDER BY path ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Pre-allocate a non-nil slice so json.Marshal emits "[]" rather
	// than "null" when the project has no files. The wizard's list
	// rendering code expects a JSON array.
	out := make([]HelpFileMeta, 0)
	for rows.Next() {
		var (
			m         HelpFileMeta
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

// SumProjectBytes returns the total size of every help file belonging to
// the given project. Used by the per-project quota check before any PUT
// or rename. Returns 0 (not an error) when the project has no files yet.
func SumProjectBytes(projectID string) (int64, error) {
	var sum sql.NullInt64
	err := DB.QueryRow(`
		SELECT SUM(size_bytes)
		FROM project_help_files
		WHERE project_id = ?`,
		projectID,
	).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if !sum.Valid {
		return 0, nil
	}
	return sum.Int64, nil
}

// SumUserBytes returns the total size of every help file across every
// project the given user owns. Used by the per-user quota check.
//
// The join uses an inner join on projects.user_id, so deleted projects
// (which would have already cascaded their help_files rows away on
// delete) contribute nothing. Soft-deleted projects, if introduced
// later, would need an extra WHERE filter here.
func SumUserBytes(userID string) (int64, error) {
	var sum sql.NullInt64
	err := DB.QueryRow(`
		SELECT SUM(hf.size_bytes)
		FROM project_help_files hf
		INNER JOIN projects p ON p.id = hf.project_id
		WHERE p.user_id = ?`,
		userID,
	).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if !sum.Valid {
		return 0, nil
	}
	return sum.Int64, nil
}

// ─── Write ────────────────────────────────────────────────────────────────────

// SaveHelpFile upserts a help-file row. The mime type is recomputed by
// the handler from the path's extension, so callers always pass the
// authoritative value here — clients cannot influence it.
//
// updated_at is written as RFC3339 in UTC. The same format is used by
// GetHelpFile / ListHelpFiles when parsing back, so a round-trip
// preserves the timestamp exactly.
func SaveHelpFile(projectID, path, mimeType string, content []byte) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO project_help_files
			(project_id, path, mime_type, content, size_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, path) DO UPDATE SET
			mime_type  = excluded.mime_type,
			content    = excluded.content,
			size_bytes = excluded.size_bytes,
			updated_at = excluded.updated_at`,
		projectID, path, mimeType, content, len(content), now,
	)
	return err
}

// DeleteHelpFile removes a single row. Returns whether a row was actually
// deleted; the handler uses this to give the client an honest answer
// rather than 404-ing on idempotent re-deletes (per design doc, deletes
// are idempotent).
func DeleteHelpFile(projectID, path string) (bool, error) {
	res, err := DB.Exec(`
		DELETE FROM project_help_files
		WHERE project_id = ? AND path = ?`,
		projectID, path,
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

// RenameHelpFile moves a file from oldPath to newPath within the same
// project. Returns ErrNoHelpFile if oldPath does not exist and
// ErrHelpPathConflict if newPath already exists.
//
// Implemented as a transaction with explicit existence checks so the
// rename is atomic — if the destination becomes occupied between our
// check and our INSERT, the INSERT fails on the PK uniqueness and the
// transaction rolls back, leaving the source file intact.
//
// A rename does not change the byte total for the project, so no quota
// check is required here. The mime type is recomputed by the handler
// from the new extension before calling.
func RenameHelpFile(projectID, oldPath, newPath, newMimeType string) error {
	if oldPath == newPath {
		// No-op: client asked us to rename to the same name.
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	// Verify source exists. Without this we would silently no-op the
	// rename when the source is missing, which would mask client bugs.
	var dummy int
	err = tx.QueryRow(`
		SELECT 1 FROM project_help_files
		WHERE project_id = ? AND path = ?`,
		projectID, oldPath,
	).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoHelpFile
	}
	if err != nil {
		return err
	}

	// Reject if destination is already taken. Catching this up front
	// gives a friendly error code; relying on the PK uniqueness alone
	// would surface a generic "constraint failed" to the handler.
	err = tx.QueryRow(`
		SELECT 1 FROM project_help_files
		WHERE project_id = ? AND path = ?`,
		projectID, newPath,
	).Scan(&dummy)
	if err == nil {
		return ErrHelpPathConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Update path and mime in place. Updating in place (rather than
	// INSERT+DELETE) preserves any auxiliary metadata that may be
	// added to the schema later (created_at, owner_id, etc.) without
	// touching this code.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
		UPDATE project_help_files
		SET path = ?, mime_type = ?, updated_at = ?
		WHERE project_id = ? AND path = ?`,
		newPath, newMimeType, now, projectID, oldPath,
	); err != nil {
		return err
	}

	return tx.Commit()
}

// ─── Insert with shift ────────────────────────────────────────────────────────

// InsertHelpFileWithShift inserts a markdown help file at the requested
// position within the (basename, language) family, shifting any
// occupants at that position and beyond up by one.
//
// File-naming convention (set by the IDS spec; reproduced here so the
// store can compute the destination path without reaching back into
// the handler):
//
//	position 0   →  <basename>.<lang>.md      (unnumbered, "primary")
//	position N≥1 →  <basename>.<N>.<lang>.md
//
// If the requested position is empty, the new file is created there
// directly. If it is occupied — and N other slots ≥ position are
// also occupied — the existing files are renamed in cascade from
// the highest position down to the requested one, freeing the slot
// for the new content.
//
// Cascade direction matters: renaming bottom-up (e.g.
// position 0 → 1 first when 1 already exists) would collide with
// the existing file at 1. Renaming top-down (e.g. 2 → 3, then 1 → 2,
// then 0 → 1) avoids any intermediate collision.
//
// Atomicity: every rename plus the final insert happens inside a
// single sql.Tx. A failure mid-cascade rolls everything back, so
// the caller never sees a half-shifted state. The transaction also
// makes the operation safe against concurrent inserts to the same
// (basename, language) family — a second writer either sees the
// final state or the original, never something in between.
//
// What this function does NOT do:
//   - Path validation (e.g. allowed characters in basename). The
//     handler enforces this; the store trusts its caller.
//   - Per-project / per-user quota check. Handler responsibility.
//   - Mime-type computation. Always "text/markdown" here because the
//     shift logic only applies to .md files; images don't have an
//     ordering convention.
//
// basename, language, and content are required. position must be
// >= 0 and <= currentCount (the count of existing files in the
// family). A position > currentCount would leave a gap — not
// supported by the IDS spec, and the handler should reject it
// before calling here.
//
// Returns ErrHelpPathConflict if, despite all the precautions, the
// final insert collides with a row that the cascade did not displace
// (this can happen if the (basename, language) family contains a
// file outside the expected naming convention, e.g. a manually
// created "Init.weird.en.md"). The transaction is rolled back; the
// caller should surface a 409 Conflict to the user.
func InsertHelpFileWithShift(
	projectID, basename, language string,
	position int,
	content []byte,
) error {
	if position < 0 {
		return errors.New("position must be >= 0")
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	// Discover existing positions in the family. We look for any path
	// that matches the naming convention: <basename>.<lang>.md (= 0)
	// and <basename>.<N>.<lang>.md (= N).
	//
	// We use a tx-scoped query so the snapshot is consistent with the
	// updates that follow.
	occupied := map[int]string{} // position → current path

	rows, err := tx.Query(`
		SELECT path FROM project_help_files
		WHERE project_id = ?`,
		projectID,
	)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		if pos, ok := classifyHelpPath(p, basename, language); ok {
			occupied[pos] = p
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	// Cascade renames: every occupant whose position is >= our target
	// gets shifted up by one. We do this top-down so each rename
	// frees its destination first.
	//
	// The set of positions to move is collected first, then sorted
	// descending. Doing it in a single pass would require recomputing
	// the destination on every iteration; pre-sorting is cheaper and
	// reads more clearly.
	var toShift []int
	for pos := range occupied {
		if pos >= position {
			toShift = append(toShift, pos)
		}
	}
	// Manual descending sort — keeps the package free of an extra
	// import, and the slice is small (rarely > 10 entries).
	for i := 0; i < len(toShift); i++ {
		for j := i + 1; j < len(toShift); j++ {
			if toShift[j] > toShift[i] {
				toShift[i], toShift[j] = toShift[j], toShift[i]
			}
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, src := range toShift {
		dst := src + 1
		oldPath := occupied[src]
		newPath := numberedHelpPath(basename, dst, language)
		if _, err := tx.Exec(`
			UPDATE project_help_files
			SET path = ?, updated_at = ?
			WHERE project_id = ? AND path = ?`,
			newPath, now, projectID, oldPath,
		); err != nil {
			return err
		}
	}

	// Insert the new file at the requested position. After the
	// cascade above the slot is guaranteed empty for any path that
	// followed the naming convention; if a foreign path occupies it
	// (manually created file), the INSERT fails on the PK and we
	// surface a friendly conflict error.
	insertPath := numberedHelpPath(basename, position, language)
	res, err := tx.Exec(`
		INSERT INTO project_help_files
			(project_id, path, mime_type, content, size_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		projectID, insertPath, "text/markdown",
		content, len(content), now,
	)
	if err != nil {
		// SQLite returns "constraint failed" for PK collisions. We
		// don't try to detect the driver-specific error code (would
		// couple this layer to SQLite); instead, the caller treats
		// any insert failure post-cascade as a path conflict. The
		// transaction has already been rolled back via the defer.
		return ErrHelpPathConflict
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrHelpPathConflict
	}

	return tx.Commit()
}

// classifyHelpPath checks whether `path` belongs to the
// (basename, language) help-file family and returns its position.
//
// Returns:
//   - (0, true) for "<basename>.<lang>.md"
//   - (N, true) for "<basename>.<N>.<lang>.md" with N >= 1
//   - (0, false) for anything else
//
// The function is a small parser — not a regex — to keep allocation
// at zero and the logic auditable. It is called once per existing
// project file during InsertHelpFileWithShift.
func classifyHelpPath(path, basename, language string) (int, bool) {
	suffix := "." + language + ".md"
	primary := basename + suffix
	if path == primary {
		return 0, true
	}
	// Numbered form: <basename>.<N>.<lang>.md
	if len(path) <= len(basename)+1+len(suffix) {
		return 0, false
	}
	if path[:len(basename)+1] != basename+"." {
		return 0, false
	}
	if path[len(path)-len(suffix):] != suffix {
		return 0, false
	}
	mid := path[len(basename)+1 : len(path)-len(suffix)]
	// The mid segment must be a positive integer with no leading
	// zeros (apart from "0" itself, which is not allowed in numbered
	// form per the convention — that's the primary unnumbered path).
	if len(mid) == 0 {
		return 0, false
	}
	if mid == "0" || (mid[0] == '0' && len(mid) > 1) {
		return 0, false
	}
	n := 0
	for _, c := range mid {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0, false
		}
	}
	return n, true
}

// numberedHelpPath builds the canonical path for a given position.
// position 0 returns the unnumbered primary path; N >= 1 returns the
// numbered form. Mirrors classifyHelpPath in reverse.
func numberedHelpPath(basename string, position int, language string) string {
	if position == 0 {
		return basename + "." + language + ".md"
	}
	return basename + "." + strconv.Itoa(position) + "." + language + ".md"
}
