// server/handler/projectapi/code_files_integration_test.go —
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// Integration tests for the handlers that persist codegen output:
// save, get, list, rename of code SNAPSHOTS.
//
// Why this file is codegen-relevant:
//
//	A project's source is a snapshot — a SET of files saved atomically
//	as one version (see store/project_code_files.go for the model).
//	The IDE saves through handleSaveCodeVersion, reads back via
//	handleGetCodeFile on project open, and mutates the set through the
//	rename/upload/delete endpoints, all of which are "a save with a
//	computed set". This file pins that contract end-to-end against a
//	real DB and a real disk mirror.
//
// What's covered here that the unit tests can't reach:
//
//   - Save → Get round trip (Go, single file): DB row set, disk
//     mirror, GET response shape.
//   - Multi-file C round trip: the slice-6 flagship — header + two
//     .c (one nested), order preserved, nested mirror path on disk.
//   - Verbatim contract: trailing newline survives byte-exact.
//   - Version monotonicity over snapshots.
//   - Rename as a new snapshot (DB is truth; mirror follows).
//   - Ownership isolation (foreign token → 404).
//
// Português: A fonte de um projeto é um snapshot — conjunto salvo
// atômico como uma versão. Este arquivo fixa o contrato ponta-a-ponta
// contra DB e disco reais; o carro-chefe é o round-trip C multiarquivo.
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

// fileBody builds the save request body in the snapshot shape.
func fileBody(entries ...store.CodeFileEntry) map[string]any {
	files := make([]map[string]any, len(entries))
	for i, e := range entries {
		files[i] = map[string]any{"path": e.Path, "content": e.Content}
	}
	return map[string]any{"files": files}
}

// ─── Save → Get round trip (Go, single file) ──────────────────────────────────

func TestHandleSaveCodeVersion_persistsAndIsReadBack(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-save-cycle"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Cycle Project")

	const source1 = "package main\n\nfunc main() {}"
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		token, fileBody(store.CodeFileEntry{Path: "blink.go", Content: source1}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("save: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var saved struct {
		ID      string                `json:"id"`
		Version int                   `json:"version"`
		Files   []store.CodeFileEntry `json:"files"`
	}
	decodeOKData(t, rec, &saved)
	if saved.Version != 1 {
		t.Errorf("saved.version: got %d, want 1", saved.Version)
	}
	if len(saved.Files) != 1 || saved.Files[0].Path != "blink.go" {
		t.Errorf("saved.files: got %+v, want the one blink.go entry", saved.Files)
	}
	if saved.ID == "" {
		t.Errorf("saved.id is empty")
	}

	// ── Verify the mirror landed on disk ────────────────────────────────────
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

	// ── GET the snapshot via the handler ────────────────────────────────────
	req = authedJSONRequest(t, http.MethodGet,
		"/api/v1/projects/"+projectID+"/files/code", token, nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var fetched struct {
		Files    []store.CodeFileEntry `json:"files"`
		Version  int                   `json:"version"`
		Versions []struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"versions"`
	}
	decodeOKData(t, rec, &fetched)
	if len(fetched.Files) != 1 || fetched.Files[0].Content != source1 {
		t.Errorf("fetched.files drift: got %+v", fetched.Files)
	}
	if fetched.Version != 1 {
		t.Errorf("fetched.version: got %d, want 1", fetched.Version)
	}
	if len(fetched.Versions) != 1 {
		t.Errorf("fetched.versions length: got %d, want 1", len(fetched.Versions))
	}
}

// ─── Multi-file C round trip — the slice-6 flagship ───────────────────────────
//
// A C project saves a real specialist layout: public header, core unit,
// nested util unit. The contract under test: order preserved (tab order
// IS snapshot order), every entry byte-exact on read-back, and the disk
// mirror reproduces the NESTED relative path.

func TestHandleSaveCodeVersion_multiFileCRoundTrip(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-multifile-c"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirectLang(t, userID, "Probe Device", "c")

	snapshot := []store.CodeFileEntry{
		{Path: "api.h", Content: "typedef struct probe { int fd; } probe_t;\n"},
		{Path: "core.c", Content: "#include \"api.h\"\nint probe_read(probe_t *p) { return util_clamp(p->fd); }\n"},
		{Path: "util/helpers.c", Content: "int g_probe_bias = 0;\nint util_clamp(int v) { return v + g_probe_bias; }\n"},
	}
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		token, fileBody(snapshot...))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Read back: same set, same ORDER, same bytes.
	latest, err := store.GetLatestProjectCodeVersion(projectID)
	if err != nil {
		t.Fatalf("GetLatestProjectCodeVersion: %v", err)
	}
	if len(latest.Files) != len(snapshot) {
		t.Fatalf("snapshot size: got %d, want %d", len(latest.Files), len(snapshot))
	}
	for i := range snapshot {
		if latest.Files[i].Path != snapshot[i].Path {
			t.Errorf("order drift at %d: got %q, want %q",
				i, latest.Files[i].Path, snapshot[i].Path)
		}
		if latest.Files[i].Content != snapshot[i].Content {
			t.Errorf("content drift at %q", snapshot[i].Path)
		}
	}

	// Disk mirror reproduces the nested relative path.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	got, err := os.ReadFile(filepath.Join(codeDir, "util", "helpers.c"))
	if err != nil {
		t.Fatalf("nested mirror path missing: %v", err)
	}
	if string(got) != snapshot[2].Content {
		t.Errorf("nested mirror drift: got %q", string(got))
	}
}

// ─── Trailing newline survives the round trip ─────────────────────────────────
//
// gofmt-formatted Go source always ends with '\n'. The handler must not
// mutate contents — only PATHS are trimmed — otherwise every save strips
// one byte and the next open shows an unprovoked diff.

func TestHandleSaveCodeVersion_preservesTrailingNewline(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-trailing-nl"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Trailing Newline Project")

	const source = "package main\n\nfunc main() {}\n" // gofmt-style trailing \n

	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		token, fileBody(store.CodeFileEntry{Path: "main.go", Content: source}))
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
		t.Errorf("disk content drift (handler is mutating content):\n got: %q\nwant: %q",
			string(got), source)
	}

	// DB row set: content must match exactly too.
	latest, err := store.GetLatestProjectCodeVersion(projectID)
	if err != nil {
		t.Fatalf("GetLatestProjectCodeVersion: %v", err)
	}
	if len(latest.Files) != 1 || latest.Files[0].Content != source {
		t.Errorf("DB snapshot drift: got %+v", latest.Files)
	}
}

// ─── Version monotonicity ─────────────────────────────────────────────────────
//
// Two sequential saves produce snapshots 1 and 2. The disk mirror is
// overwritten — the code/ directory is canonical-latest; the snapshot
// history lives in the DB.

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
			token, fileBody(store.CodeFileEntry{Path: "main.go", Content: source}))
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

	// The DB reflects two snapshot rows, each with its own file set.
	versions, err := store.ListProjectCodeVersions(projectID)
	if err != nil {
		t.Fatalf("ListProjectCodeVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("DB version count: got %d, want 2", len(versions))
	}
	if len(versions[0].Files) != 1 || versions[0].Files[0].Content != "package main // v2" {
		t.Errorf("latest snapshot drift: got %+v", versions[0].Files)
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
//
// A rename is "a save with one path changed": it produces a NEW snapshot
// (the DB is the source of truth) and the disk mirror follows. The old
// pre-write-a-loose-disk-file semantics died with the disk-as-truth era.

func TestHandleRenameCodeFile_createsRenamedSnapshot(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-rename"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Rename Project")

	const source = "package main\n"
	req := authedJSONRequest(t, http.MethodPost,
		"/api/v1/projects/"+projectID+"/files/code/versions",
		token, fileBody(store.CodeFileEntry{Path: "old.go", Content: source}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed save: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Rename via the handler.
	req = authedJSONRequest(t, http.MethodPut,
		"/api/v1/projects/"+projectID+"/files/code/rename",
		token, map[string]any{"newName": "new.go"})
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("rename: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Version int                   `json:"version"`
		Files   []store.CodeFileEntry `json:"files"`
	}
	decodeOKData(t, rec, &resp)
	if resp.Version != 2 {
		t.Errorf("rename must create a NEW snapshot: version got %d, want 2", resp.Version)
	}
	if len(resp.Files) != 1 || resp.Files[0].Path != "new.go" {
		t.Errorf("response.files: got %+v, want the renamed entry", resp.Files)
	}
	if resp.Files[0].Content != source {
		t.Errorf("rename must not touch content: got %q", resp.Files[0].Content)
	}

	// On disk: old gone, new present with same content.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	if _, err := os.Stat(filepath.Join(codeDir, "old.go")); !os.IsNotExist(err) {
		t.Errorf("old.go should be gone from the mirror; stat err = %v", err)
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
		fileBody(store.CodeFileEntry{Path: "main.go", Content: "package main\n"}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("intruder save: got %d, want %d (handler returns 404 to avoid leaking existence). body: %s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
