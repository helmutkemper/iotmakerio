// server/handler/projectapi/backup_integration_test.go —
// Integration tests for the working-source backup endpoints.
//
// Why this file is codegen-relevant:
//
//	The backup endpoints (handleGetCodeBackup, handleSaveCodeBackup,
//	handleDeleteCodeBackup) own the single-slot snapshot of a
//	project's editor content between Save clicks. The IDE posts here
//	on tab switches, wizard edits, and debounced Monaco edits — so
//	any source produced by the codegen pipeline AND not yet
//	committed as a versioned save lives in this row. If the backup
//	contract drifts (saves leak away, empty backups linger, the
//	post-version cleanup fails), the user opens the IDE next time
//	and either loses unsaved generated code or sees a stale
//	"pending" indicator.
//
// What's covered here:
//
//   - Save → Get round trip with content and filename.
//   - Empty source on save deletes the existing row (documented
//     contract: "an empty editor never persists across reopens").
//   - Explicit DELETE clears an existing row and is idempotent.
//   - GET returns 404 when no row exists — the "no unsaved work"
//     signal that the frontend uses to decide whether to show the
//     red Save indicator.
//   - After a successful POST /code/versions (a Save), the backup
//     row is dropped automatically — the "promotion" step in
//     handleSaveCodeVersion. This is the concrete observation that
//     the backup-cleared-on-save behaviour the comments describe is
//     actually wired up.
package projectapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"server/store"
)

// ─── Save → Get round trip ────────────────────────────────────────────────────

func TestHandleSaveCodeBackup_persistsAndIsReadBack(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-backup-cycle"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Backup Cycle Project")

	// Save a backup.
	const source = "package main // mid-edit, not yet saved"
	saveBody := map[string]any{
		"source":   source,
		"filename": "main.go",
	}
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/backup",
		token, saveBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Get it back through the handler.
	req = authedJSONRequest(t, http.MethodGet,
		"/api/v1/projects/"+projectID+"/files/code/backup",
		token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get: got %d, body: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Source   string `json:"source"`
		Filename string `json:"filename"`
	}
	decodeOKData(t, rec, &got)
	if got.Source != source {
		t.Errorf("source drift:\n got: %q\nwant: %q", got.Source, source)
	}
	if got.Filename != "main.go" {
		t.Errorf("filename: got %q, want %q", got.Filename, "main.go")
	}
}

// ─── Empty source on save deletes the row ─────────────────────────────────────
//
// The store doc on SaveProjectBackup pins this behaviour: "If source
// is empty (after trim), the existing row is deleted instead. This
// keeps the 'empty backup' rule simple: an empty editor never
// persists across reopens." The handler returns 200 either way so
// the client never has to distinguish save-vs-clear.

func TestHandleSaveCodeBackup_emptySourceDeletesExistingRow(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-backup-empty"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Backup Empty Project")

	// Seed a non-empty backup first.
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/backup", token,
		map[string]any{"source": "package main", "filename": "main.go"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed save: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Now save with whitespace-only source — should delete the row.
	req = authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/backup", token,
		map[string]any{"source": "   \n\t\n  ", "filename": "main.go"})
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty save: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// GET must now return 404 — the "no unsaved work" signal.
	req = authedJSONRequest(t, http.MethodGet,
		"/api/v1/projects/"+projectID+"/files/code/backup", token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("get after empty save: got %d, want %d (row should be gone). body: %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// ─── Explicit DELETE ──────────────────────────────────────────────────────────

func TestHandleDeleteCodeBackup_clearsExistingRow(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-backup-delete"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Backup Delete Project")

	// Seed a backup.
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/backup", token,
		map[string]any{"source": "package main", "filename": "main.go"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Delete it.
	req = authedJSONRequest(t, http.MethodDelete,
		"/api/v1/projects/"+projectID+"/files/code/backup", token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// GET now 404.
	req = authedJSONRequest(t, http.MethodGet,
		"/api/v1/projects/"+projectID+"/files/code/backup", token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404. body: %s",
			rec.Code, rec.Body.String())
	}
}

// TestHandleDeleteCodeBackup_idempotent verifies that DELETE is
// idempotent — a second delete (or a delete with no row to clear)
// returns 200 too. The handler's docstring says "Idempotent: 200
// even if no row existed". This pins that contract.
func TestHandleDeleteCodeBackup_idempotent(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-backup-idempotent"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Backup Idempotent Project")

	// No backup exists yet — DELETE must still succeed.
	req := authedJSONRequest(t, http.MethodDelete,
		"/api/v1/projects/"+projectID+"/files/code/backup", token, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first delete (no row): got %d, want 200. body: %s",
			rec.Code, rec.Body.String())
	}

	// Second delete also succeeds.
	req = authedJSONRequest(t, http.MethodDelete,
		"/api/v1/projects/"+projectID+"/files/code/backup", token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("second delete: got %d, want 200. body: %s",
			rec.Code, rec.Body.String())
	}
}

// ─── 404 on missing backup ────────────────────────────────────────────────────

func TestHandleGetCodeBackup_notFoundWhenNoRow(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-no-backup"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "No Backup Project")

	req := authedJSONRequest(t, http.MethodGet,
		"/api/v1/projects/"+projectID+"/files/code/backup", token, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// ─── Backup is auto-cleared after a Save ──────────────────────────────────────
//
// handleSaveCodeVersion deletes the backup row at the end of a
// successful save (the "promotion" step). The frontend relies on
// this so the red "pending" indicator clears the next time the user
// opens the project. If this regresses, the user sees pending state
// for code they already saved.

func TestHandleSaveCodeVersion_clearsBackupAfterSave(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-backup-promo"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Backup Promotion Project")

	// Seed a backup.
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/backup", token,
		map[string]any{"source": "package main // mid-edit", "filename": "main.go"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed backup: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Confirm the row exists at the store level.
	if _, err := store.GetProjectBackup(projectID); err != nil {
		t.Fatalf("expected backup before save, got err: %v", err)
	}

	// Save a real version — handler must drop the backup row as a
	// side effect.
	req = authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions", token,
		map[string]any{"source": "package main // saved", "filename": "main.go"})
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save version: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Backup must now be gone.
	if _, err := store.GetProjectBackup(projectID); err != store.ErrNoBackup {
		t.Errorf("expected ErrNoBackup after save, got %v (backup was not promoted/cleared)", err)
	}

	// And via the handler too — should 404.
	req = authedJSONRequest(t, http.MethodGet,
		"/api/v1/projects/"+projectID+"/files/code/backup", token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("get after save: got %d, want 404. body: %s",
			rec.Code, rec.Body.String())
	}
}
