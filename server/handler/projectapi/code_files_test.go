// server/handler/projectapi/code_files_test.go — Validation tests for
// the code-file endpoints that gate what gets stored as a project's
// source code.
//
// Why this file is codegen-relevant:
//
//	The codegen pipeline (server/codegen) emits Go source. The project
//	stores that output via handleSaveCodeVersion and lets the user
//	rename it via handleRenameCodeFile. Both enforce the contract that
//	a project's "code" file ends in .go and contains no path-injection
//	characters. These tests pin those guards.
//
// Scope:
//
//	Pure validation paths only — cases where the handler returns 400
//	BEFORE reaching store.GetProjectByIDAndUser. The success path,
//	409 conflict on duplicate version, 404 on missing project, and the
//	disk-write side-effects all need a real DB and tmp filesystem;
//	those belong in an integration pass.
//
//	handleUploadCodeFile is intentionally NOT covered here: it calls
//	store.GetProjectByIDAndUser BEFORE inspecting the uploaded file's
//	extension, so its .go-extension validation is unreachable without
//	DB seeding. Move that test to the integration pass when you add it.
package projectapi

import (
	"net/http"
	"strings"
	"testing"
)

// ─── handleSaveCodeVersion — validation paths ─────────────────────────────────

func TestHandleSaveCodeVersion_validationFailsBeforeDB(t *testing.T) {
	cases := []struct {
		name          string
		body          map[string]any
		wantErrSubstr string
	}{
		{
			name:          "missing source",
			body:          map[string]any{"filename": "main.go"},
			wantErrSubstr: "source is required",
		},
		{
			name:          "blank source (whitespace only)",
			body:          map[string]any{"source": "   \n\t  ", "filename": "main.go"},
			wantErrSubstr: "source is required",
		},
		{
			name: "filename without .go extension",
			body: map[string]any{
				"source":   "package main",
				"filename": "main.py",
			},
			wantErrSubstr: ".go extension",
		},
		{
			name: "filename with forward slash",
			body: map[string]any{
				"source":   "package main",
				"filename": "sub/main.go",
			},
			wantErrSubstr: "invalid filename",
		},
		{
			name: "filename with backslash",
			body: map[string]any{
				"source":   "package main",
				"filename": `sub\main.go`,
			},
			wantErrSubstr: "invalid filename",
		},
		{
			name: "filename with asterisk",
			body: map[string]any{
				"source":   "package main",
				"filename": "ma*in.go",
			},
			wantErrSubstr: "invalid filename",
		},
		{
			name: "filename with pipe",
			body: map[string]any{
				"source":   "package main",
				"filename": "ma|in.go",
			},
			wantErrSubstr: "invalid filename",
		},
		{
			name: "filename with double quote",
			body: map[string]any{
				"source":   "package main",
				"filename": `ma"in.go`,
			},
			wantErrSubstr: "invalid filename",
		},
		{
			name: "filename with angle brackets",
			body: map[string]any{
				"source":   "package main",
				"filename": "ma<in>.go",
			},
			wantErrSubstr: "invalid filename",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newJSONContext(t, http.MethodPost,
				"/api/v1/projects/some-id/files/code/versions", tc.body)
			c.SetParamNames("id")
			c.SetParamValues("some-id")

			if err := handleSaveCodeVersion(c); err != nil {
				t.Fatalf("handler returned error (expected JSON 400 instead): %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want %d. body: %s",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			gotMsg := strings.ToLower(decodeFailBody(t, rec))
			if !strings.Contains(gotMsg, strings.ToLower(tc.wantErrSubstr)) {
				t.Errorf("error message missing %q; got %q",
					tc.wantErrSubstr, gotMsg)
			}
		})
	}
}

// TestHandleSaveCodeVersion_malformedJSON pins the "invalid request body"
// path with a malformed JSON payload.
func TestHandleSaveCodeVersion_malformedJSON(t *testing.T) {
	c, rec := newRawJSONContext(t, http.MethodPost,
		"/api/v1/projects/some-id/files/code/versions", `{"source":`)
	c.SetParamNames("id")
	c.SetParamValues("some-id")

	if err := handleSaveCodeVersion(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if msg := strings.ToLower(decodeFailBody(t, rec)); !strings.Contains(msg, "invalid request body") {
		t.Errorf("error message missing 'invalid request body'; got %q", msg)
	}
}

// ─── handleRenameCodeFile — validation paths ──────────────────────────────────

func TestHandleRenameCodeFile_validationFailsBeforeDB(t *testing.T) {
	cases := []struct {
		name          string
		body          map[string]any
		wantErrSubstr string
	}{
		{
			name:          "missing newName",
			body:          map[string]any{},
			wantErrSubstr: "newname is required",
		},
		{
			name:          "blank newName (whitespace only)",
			body:          map[string]any{"newName": "   "},
			wantErrSubstr: "newname is required",
		},
		{
			name:          "newName without .go extension",
			body:          map[string]any{"newName": "renamed.txt"},
			wantErrSubstr: ".go extension",
		},
		{
			name:          "newName with forward slash",
			body:          map[string]any{"newName": "sub/renamed.go"},
			wantErrSubstr: "invalid file name",
		},
		{
			name:          "newName with backslash",
			body:          map[string]any{"newName": `sub\renamed.go`},
			wantErrSubstr: "invalid file name",
		},
		{
			name:          "newName with asterisk",
			body:          map[string]any{"newName": "ren*amed.go"},
			wantErrSubstr: "invalid file name",
		},
		{
			name:          "newName with pipe",
			body:          map[string]any{"newName": "ren|amed.go"},
			wantErrSubstr: "invalid file name",
		},
		{
			name:          "newName with double quote",
			body:          map[string]any{"newName": `ren"amed.go`},
			wantErrSubstr: "invalid file name",
		},
		{
			name:          "newName with angle brackets",
			body:          map[string]any{"newName": "ren<amed>.go"},
			wantErrSubstr: "invalid file name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newJSONContext(t, http.MethodPut,
				"/api/v1/projects/some-id/files/code/rename", tc.body)
			c.SetParamNames("id")
			c.SetParamValues("some-id")

			if err := handleRenameCodeFile(c); err != nil {
				t.Fatalf("handler returned error (expected JSON 400 instead): %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want %d. body: %s",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			gotMsg := strings.ToLower(decodeFailBody(t, rec))
			if !strings.Contains(gotMsg, strings.ToLower(tc.wantErrSubstr)) {
				t.Errorf("error message missing %q; got %q",
					tc.wantErrSubstr, gotMsg)
			}
		})
	}
}

// TestHandleRenameCodeFile_malformedJSON pins the "invalid request body"
// path with a malformed JSON payload.
func TestHandleRenameCodeFile_malformedJSON(t *testing.T) {
	c, rec := newRawJSONContext(t, http.MethodPut,
		"/api/v1/projects/some-id/files/code/rename", `{"newName":`)
	c.SetParamNames("id")
	c.SetParamValues("some-id")

	if err := handleRenameCodeFile(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if msg := strings.ToLower(decodeFailBody(t, rec)); !strings.Contains(msg, "invalid request body") {
		t.Errorf("error message missing 'invalid request body'; got %q", msg)
	}
}
