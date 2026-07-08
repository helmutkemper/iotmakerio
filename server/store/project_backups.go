// store/project_backups.go — single-slot backup of a project's working source.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Backups are distinct from project_code_versions (the user-facing Save
// history, incremental and immutable). A backup is a single row per
// project that the client overwrites on every auto-save trigger:
//
//   - tab switch (Editor / Wizard / Preview / Debug)
//   - wizard modal save
//   - debounced Monaco keystroke (~2s)
//
// The backup's purpose is recovery: if the user closes the browser
// without clicking Save, the next open recovers their working state
// from the backup. The Save button shows a red "pending" tint until
// the backup is promoted to a real version via the regular Save flow.
//
// Empty source is treated as "no backup": when the user clears the
// editor, the row is deleted transparently so reopens don't restore
// emptiness. See SaveProjectBackup.
//
// FORMAT (multi-file, Slice 6c-3): the Source column holds the WHOLE
// working copy as a JSON blob — [{"path","content"},…] in tab order —
// and Filename holds the ACTIVE tab's path. A JSON blob in a text
// column would be wrong for a model of record (that's why versions got
// a child table); backup is SCRATCH — one transient row, overwritten
// constantly, never queried by content — and the blob is the honest
// cheap shape for scratch. Encoding/decoding lives at the HTTP
// boundary (handler/projectapi); this layer stays "a blob and a
// label".
//
// Português: FORMATO (multiarquivo, 6c-3): Source guarda a cópia de
// trabalho INTEIRA como blob JSON na ordem das abas; Filename guarda a
// aba ATIVA. Blob em coluna de texto seria errado para modelo de
// registro (por isso versões ganharam tabela filha); backup é RASCUNHO
// — uma linha transitória, sobrescrita o tempo todo, nunca consultada
// pelo conteúdo — e o blob é a forma honesta e barata para rascunho.
// Codificação mora na borda HTTP; esta camada segue "um blob e um
// rótulo".
package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ProjectBackup is the transient working-copy snapshot for one project.
type ProjectBackup struct {
	ProjectID string
	Source    string // JSON blob of the file set (see FORMAT above)
	Filename  string // active tab's path
	UpdatedAt string // ISO 8601 / RFC3339
}

// ErrNoBackup is returned by GetProjectBackup when no row exists for the
// given project. Callers treat this as "use the latest saved version".
var ErrNoBackup = errors.New("no backup for project")

// GetProjectBackup returns the backup row for projectID, or ErrNoBackup if
// the row doesn't exist. A missing row is the normal "no unsaved work"
// state — not an error condition.
func GetProjectBackup(projectID string) (*ProjectBackup, error) {
	var b ProjectBackup
	err := DB.QueryRow(`
		SELECT project_id, source, filename, updated_at
		FROM   project_backups
		WHERE  project_id = ?`, projectID,
	).Scan(&b.ProjectID, &b.Source, &b.Filename, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoBackup
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// SaveProjectBackup writes (UPSERT) the working-source snapshot for
// projectID. The single row is overwritten on every call — there is
// no version history.
//
// If source is empty (after the caller's own trim), the existing row
// is deleted instead. This keeps the "empty backup" rule simple: an
// empty editor never persists across reopens. The caller should pass
// the source EXACTLY as the user typed it; trimming for the
// emptiness check happens here so the policy lives in one place.
func SaveProjectBackup(projectID, source, filename string) error {
	// Empty backup → delete. Trim only for the emptiness test so
	// "    \n  " counts as empty; the user clearly didn't intend
	// to persist whitespace-only content.
	if strings.TrimSpace(source) == "" {
		_, err := DB.Exec(`DELETE FROM project_backups WHERE project_id = ?`, projectID)
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO project_backups (project_id, source, filename, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
		    source     = excluded.source,
		    filename   = excluded.filename,
		    updated_at = excluded.updated_at
	`, projectID, source, filename, now)
	return err
}

// DeleteProjectBackup removes the backup row for projectID, regardless
// of its content. Called when the user successfully Saves — promoting
// the backup to a real version makes the backup redundant. Idempotent:
// no error if the row doesn't exist.
func DeleteProjectBackup(projectID string) error {
	_, err := DB.Exec(`DELETE FROM project_backups WHERE project_id = ?`, projectID)
	return err
}
