// server/handler/projectapi/upload_code_integration_test.go —
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
//	Both paths share the same .go-only contract, and both write to
//	the same code/ directory. These tests pin the upload-side guard.
//
// What's covered here that the unit tests can't reach:
//
//   - The .go-extension reject path: handleUploadCodeFile calls
//     store.GetProjectByIDAndUser BEFORE inspecting the uploaded
//     file's extension, so the .go check is unreachable in a no-DB
//     test. Now that we have a DB, this case becomes testable.
//   - Happy path: the file lands on disk under {basePath}/code/
//     with the right name.
//   - clearDirectory contract: a second upload replaces the first
//     (the code/ directory is single-slot at any given moment).
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

	// Response carries the saved name + a static-serving URL.
	var resp struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	decodeOKData(t, rec, &resp)
	if resp.Name != "imported.go" {
		t.Errorf("response.name: got %q, want %q", resp.Name, "imported.go")
	}
	if resp.URL == "" {
		t.Errorf("response.url is empty")
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
// The handler calls clearDirectory before saving, so the code/
// directory holds at most one .go file at a time. This pins that
// contract — important because the IDE assumes the canonical-latest
// file is whatever sits in code/.

func TestHandleUploadCodeFile_secondUploadReplacesFirst(t *testing.T) {
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

	// Only second.go should remain.
	cfg := config.Get()
	codeDir := filepath.Join(
		projectBasePath(cfg, userID, store.ProjectTypeCustomDevice, projectID),
		store.ProjectFileSectionCode,
	)
	entries, err := os.ReadDir(codeDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected 1 file in code/; got %d: %v", len(entries), names)
	}
	if entries[0].Name() != "second.go" {
		t.Errorf("expected second.go to be the only file; got %q", entries[0].Name())
	}
}
