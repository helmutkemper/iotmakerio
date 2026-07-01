// server/handler/projectapi/code_files_integration_test.go —
// Integration tests for the handlers that persist codegen output:
// save, get, list, rename of code versions.
//
// Why this file is codegen-relevant:
//
//	The codegen pipeline emits Go source. The IDE saves that source
//	through handleSaveCodeVersion, reads it back via handleGetCodeFile
//	on project open, and lets the user rename the file via
//	handleRenameCodeFile. Together these handlers are the persistence
//	contract that the codegen output flows through. This file pins
//	the contract end-to-end against a real DB and a real disk.
//
// What's covered here that the unit tests can't reach:
//
//   - Save → Get round trip: a successful POST creates a DB row,
//     writes the .go file under {basePath}/code/, and a subsequent
//     GET returns the same source plus version metadata.
//
//   - Default filename: when the request omits "filename", the
//     handler stores it as "main.go" (the unit test could only see
//     this default applied if it could pass through the DB call,
//     which requires a real DB).
//
//   - Version monotonic increment: two sequential saves produce
//     versions 1 and 2 in that order. The DB UNIQUE(project_id,
//     version) constraint must not race the GetNextCodeVersionNumber
//     helper.
//
//   - Rename: a saved code file is moved to a new name on disk and
//     reported back to the client.
package projectapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"server/config"
	"server/store"
)

// ─── Save → Get round trip ────────────────────────────────────────────────────

func TestHandleSaveCodeVersion_persistsAndIsReadBack(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-save-cycle"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Cycle Project")

	// ── Save a first version ────────────────────────────────────────────────
	// Source is intentionally written WITHOUT a trailing newline because
	// handleSaveCodeVersion calls strings.TrimSpace on req.Source before
	// persisting (handlers.go ~L547). Sending a trim-clean string keeps
	// this test focused on the round-trip rather than the trim semantics.
	// If you want to pin the trim behaviour itself, do it in a dedicated
	// test rather than here.
	const source1 = "package main\n\nfunc main() {}"
	saveBody := map[string]any{
		"source":   source1,
		"filename": "blink.go",
	}
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		token, saveBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("save: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var saved struct {
		ID       string `json:"id"`
		Version  int    `json:"version"`
		Filename string `json:"filename"`
	}
	decodeOKData(t, rec, &saved)
	if saved.Version != 1 {
		t.Errorf("saved.version: got %d, want 1", saved.Version)
	}
	if saved.Filename != "blink.go" {
		t.Errorf("saved.filename: got %q, want %q", saved.Filename, "blink.go")
	}
	if saved.ID == "" {
		t.Errorf("saved.id is empty")
	}

	// ── Verify the file landed on disk ──────────────────────────────────────
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	got, err := os.ReadFile(filepath.Join(codeDir, "blink.go"))
	if err != nil {
		t.Fatalf("read back from disk: %v", err)
	}
	if string(got) != source1 {
		t.Errorf("disk content drift:\n got: %q\nwant: %q", string(got), source1)
	}

	// ── GET the code file via the handler ───────────────────────────────────
	req = authedJSONRequest(t, http.MethodGet,
		"/api/v1/projects/"+projectID+"/files/code", token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var fetched struct {
		Source   string `json:"source"`
		Version  int    `json:"version"`
		Filename string `json:"filename"`
		Versions []struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"versions"`
	}
	decodeOKData(t, rec, &fetched)
	if fetched.Source != source1 {
		t.Errorf("fetched.source drift:\n got: %q\nwant: %q",
			fetched.Source, source1)
	}
	if fetched.Version != 1 {
		t.Errorf("fetched.version: got %d, want 1", fetched.Version)
	}
	if fetched.Filename != "blink.go" {
		t.Errorf("fetched.filename: got %q, want %q", fetched.Filename, "blink.go")
	}
	if len(fetched.Versions) != 1 {
		t.Errorf("fetched.versions length: got %d, want 1", len(fetched.Versions))
	}
}

// ─── Trailing newline survives the round trip ─────────────────────────────────
//
// gofmt-formatted Go source always ends with '\n'. The handler must
// not mutate req.Source — otherwise every save strips one byte and
// the next open shows an unprovoked diff against the user's local
// copy. This test pins the verbatim contract: what comes in goes
// out exactly, including leading/trailing whitespace as part of the
// payload (whitespace-only is still 400 — see the unit tests).

func TestHandleSaveCodeVersion_preservesTrailingNewline(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-trailing-nl"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Trailing Newline Project")

	const source = "package main\n\nfunc main() {}\n" // gofmt-style trailing \n

	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		token, map[string]any{"source": source, "filename": "main.go"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Disk: bytes must match exactly.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	got, err := os.ReadFile(filepath.Join(codeDir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if string(got) != source {
		t.Errorf("disk content drift (handler is mutating source):\n got: %q\nwant: %q",
			string(got), source)
	}

	// DB version row: source field must match exactly too.
	latest, err := store.GetLatestProjectCodeVersion(projectID)
	if err != nil {
		t.Fatalf("GetLatestProjectCodeVersion: %v", err)
	}
	if latest.Source != source {
		t.Errorf("DB row source drift:\n got: %q\nwant: %q",
			latest.Source, source)
	}
}

// ─── Default filename ─────────────────────────────────────────────────────────
//
// When the client omits "filename", the handler must default to
// "main.go". This is the path the codegen UI exercises on the very
// first save of a new project — the IDE doesn't always know what
// to call the file yet.

func TestHandleSaveCodeVersion_defaultsFilenameToMainGo(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-default-filename"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Default Filename Project")

	saveBody := map[string]any{
		"source": "package main\n",
		// filename omitted on purpose
	}
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		token, saveBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("save: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var saved struct {
		Filename string `json:"filename"`
	}
	decodeOKData(t, rec, &saved)
	if saved.Filename != "main.go" {
		t.Errorf("saved.filename: got %q, want %q (the documented default)",
			saved.Filename, "main.go")
	}

	// Confirm the file actually has that name on disk too.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	if _, err := os.Stat(filepath.Join(codeDir, "main.go")); err != nil {
		t.Errorf("main.go missing on disk: %v", err)
	}
}

// ─── Version monotonicity ─────────────────────────────────────────────────────
//
// Two sequential saves produce versions 1 and 2. The disk file is
// overwritten — the project's code/ directory is canonical-latest,
// and the version history lives in the DB.

func TestHandleSaveCodeVersion_versionsMonotonicallyIncrement(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-versions"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Versions Project")

	saveAndGetVersion := func(source string) int {
		t.Helper()
		req := authedJSONRequest(t, http.MethodPost,
			"/api/v1/projects/"+projectID+"/files/code/versions",
			token, map[string]any{"source": source, "filename": "main.go"})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("save: got %d, body: %s", rec.Code, rec.Body.String())
		}
		var v struct {
			Version int `json:"version"`
		}
		decodeOKData(t, rec, &v)
		return v.Version
	}

	if v := saveAndGetVersion("package main // v1"); v != 1 {
		t.Errorf("first save version: got %d, want 1", v)
	}
	if v := saveAndGetVersion("package main // v2"); v != 2 {
		t.Errorf("second save version: got %d, want 2", v)
	}

	// And the DB reflects two version rows.
	versions, err := store.ListProjectCodeVersions(projectID)
	if err != nil {
		t.Fatalf("ListProjectCodeVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("DB version count: got %d, want 2", len(versions))
	}

	// Disk still has only the latest.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	got, err := os.ReadFile(filepath.Join(codeDir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if string(got) != "package main // v2" {
		t.Errorf("disk content drift after second save: got %q", string(got))
	}
}

// ─── Rename ───────────────────────────────────────────────────────────────────

func TestHandleRenameCodeFile_movesFileOnDisk(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-rename"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Rename Project")

	// Pre-write a code file on disk so rename has something to move.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	const source = "package main\n"
	oldPath := filepath.Join(codeDir, "old.go")
	if err := os.WriteFile(oldPath, []byte(source), 0644); err != nil {
		t.Fatalf("write old.go: %v", err)
	}

	// Rename via the handler.
	req := authedJSONRequest(t, http.MethodPut,
		"/api/v1/projects/"+projectID+"/files/code/rename",
		token, map[string]any{"newName": "new.go"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("rename: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Name string `json:"name"`
	}
	decodeOKData(t, rec, &resp)
	if resp.Name != "new.go" {
		t.Errorf("response.name: got %q, want %q", resp.Name, "new.go")
	}

	// On disk: old gone, new present with same content.
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old.go should be gone; stat err = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(codeDir, "new.go"))
	if err != nil {
		t.Fatalf("read new.go: %v", err)
	}
	if string(got) != source {
		t.Errorf("new.go content drift: got %q, want %q", string(got), source)
	}
}

// ─── Ownership: foreign user cannot save into someone else's project ─────────
//
// projectapi/routes.go documents user_id as the scoping key. A token
// belonging to user B must NOT be able to write to user A's project.
// The DB ownership check (store.GetProjectByIDAndUser) is what
// enforces this — the test pins it.

func TestHandleSaveCodeVersion_rejectsForeignUser(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const owner = "u-owner"
	const intruder = "u-intruder"
	tokenIntruder := newTestUserToken(t, intruder)

	projectID := seedProjectDirect(t, owner, "Owner's Project")

	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		tokenIntruder,
		map[string]any{"source": "package main\n", "filename": "main.go"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("intruder save: got %d, want %d (handler returns 404 to avoid leaking existence). body: %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
