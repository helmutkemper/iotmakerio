// server/handler/projectapi/upload_code_integration_test.go —
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// Integration tests for handleUploadCodeFile.
//
// Why this file is codegen-relevant:
//
//	The IDE has two paths for installing source into a project: the
//	JSON Save flow (handleSaveCodeVersion) and the multipart upload
//	flow (handleUploadCodeFile). Codegen output usually reaches a
//	project through the Save flow, but the upload flow is the
//	"import existing .go file" case — for example, when a user has
//	hand-written code outside the IDE that they want to bring in.
//	Both paths share the same snapshot contract (validateCodeFileSet,
//	extension per language) and both mirror to the code/ directory.
//
// What's covered here that the unit tests can't reach:
//
//   - The extension reject path: the language-aware check needs the
//     project row, so it runs after GetProjectByIDAndUser — only
//     reachable with a DB.
//   - Happy path: a new SNAPSHOT is written (response {version,
//     files}) and the mirror lands on disk with the right name.
//   - Go replace semantics: Go is single-file by declared contract,
//     so a second upload REPLACES the set — the single-slot intent
//     of the pre-multi-file era, preserved where it is still true.
//     (C projects add/replace BY PATH; pinned by the multi-file C
//     round trip in code_files_integration_test.go.)
package projectapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"server/config"
	"server/store"
)

// ─── Happy path ───────────────────────────────────────────────────────────────

func TestHandleUploadCodeFile_writesToDisk(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-upload-happy"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Upload Happy Project")

	const source = "package main\n\nfunc Hello() string { return \"hi\" }\n"
	req := authedMultipartCodeUploadRequest(t,
		"/api/v1/projects/"+projectID+"/files/code",
		token, "imported.go", []byte(source))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload: got %d, want %d. body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	// Upload is "a save with a computed set": the response is the new
	// SNAPSHOT ({version, files}), not the {name, url} of the
	// disk-slot era.
	var resp struct {
		Version int                   `json:"version"`
		Files   []store.CodeFileEntry `json:"files"`
	}
	decodeOKData(t, rec, &resp)
	if resp.Version != 1 {
		t.Errorf("response.version: got %d, want 1", resp.Version)
	}
	if len(resp.Files) != 1 || resp.Files[0].Path != "imported.go" {
		t.Errorf("response.files: got %+v, want the one imported.go entry", resp.Files)
	}

	// The file actually lands on disk with the same bytes.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	got, err := os.ReadFile(filepath.Join(codeDir, "imported.go"))
	if err != nil {
		t.Fatalf("read imported.go: %v", err)
	}
	if string(got) != source {
		t.Errorf("disk content drift:\n got: %q\nwant: %q", string(got), source)
	}
}

// ─── Reject non-.go file ──────────────────────────────────────────────────────
//
// This case is the integration counterpart of the validation tests:
// it pins the contract that a project's code/ slot is a Go file, no
// matter which intake handler you went through.

func TestHandleUploadCodeFile_rejectsNonGoExtension(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-upload-bad-ext"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Bad Ext Project")

	req := authedMultipartCodeUploadRequest(t,
		"/api/v1/projects/"+projectID+"/files/code",
		token, "imported.py", []byte("print('hello')\n"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d. body: %s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	msg := strings.ToLower(decodeFailBody(t, rec))
	if !strings.Contains(msg, ".go") {
		t.Errorf("error message missing '.go' guidance; got %q", msg)
	}

	// Belt-and-braces: nothing was written to disk.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	entries, err := os.ReadDir(codeDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("rejected upload still wrote files: %v", names)
	}
}

// ─── Second upload replaces the first ─────────────────────────────────────────
//
// Since GoMF a Go project is a Go PACKAGE: a second upload with a NEW
// name ADDS the file beside the first (same per-path semantics as C),
// and re-uploading an EXISTING name replaces that entry only. The old
// whole-set replacement died with the single-file era — uploading
// helpers.go must not silently delete device.go.

func TestHandleUploadCodeFile_goPackageGrowsByPath(t *testing.T) {
	setupTestDB(t)
	e := newProjectsEcho()

	const userID = "u-upload-replace"
	token := newTestUserToken(t, userID)
	projectID := seedProjectDirect(t, userID, "Replace Project")

	upload := func(filename, body string) {
		t.Helper()
		req := authedMultipartCodeUploadRequest(t,
			"/api/v1/projects/"+projectID+"/files/code",
			token, filename, []byte(body))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("upload %q: got %d. body: %s",
				filename, rec.Code, rec.Body.String())
		}
	}

	upload("first.go", "package main // first")
	upload("second.go", "package main // second")

	// GoMF: both files coexist in the snapshot's disk mirror — the
	// package grew, nothing was silently deleted.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	entries, err := os.ReadDir(codeDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(entries) != 2 {
		t.Fatalf("expected first.go AND second.go in code/; got %d: %v", len(entries), names)
	}

	// Re-uploading an EXISTING name replaces that entry only — still two
	// files, first.go carrying the new bytes.
	upload("first.go", "package main // first, revised")
	got, rErr := os.ReadFile(filepath.Join(codeDir, "first.go"))
	if rErr != nil {
		t.Fatalf("read first.go: %v", rErr)
	}
	if string(got) != "package main // first, revised" {
		t.Errorf("re-upload must replace by path: got %q", string(got))
	}
	if entries2, _ := os.ReadDir(codeDir); len(entries2) != 2 {
		t.Errorf("re-upload must not grow the set: got %d files", len(entries2))
	}
}
