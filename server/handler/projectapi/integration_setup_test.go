// server/handler/projectapi/integration_setup_test.go — TestMain and
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// shared helpers for the integration-level tests in this package.
//
// Why this file exists:
//
//	The validation tests (create_project_test.go, code_files_test.go,
//	paths_test.go) exercise paths that 400 BEFORE any DB call. They do
//	not need this file. The integration tests, on the other hand, need
//	a real SQLite database and a real JWT secret to drive the full
//	handler chain (middleware + DB + filesystem). This file owns the
//	one-time setup so each integration test stays small.
//
// Architecture choices:
//
//   - The whole package boots once via TestMain. We set USER_FILES_DIR
//     and JWT_SECRET to test values BEFORE calling config.Load(),
//     because config uses sync.Once and the first Load() call wins.
//
//   - Each test that needs a DB calls setupTestDB(t), which opens a
//     fresh SQLite file under t.TempDir(). store.Open runs the full
//     migrate() which seeds programming_languages, project_ui_languages,
//     project_settings, and the category taxonomy — exactly what the
//     handlers expect to find at runtime.
//
//   - Auth runs through the real spaauth.RequireBearerToken middleware.
//     Tests issue a real JWT via cryptoauth.NewJWT and pass it in the
//     Authorization header. No hooks into the private context-key
//     constant; if the middleware contract drifts, the tests catch it.
//
//   - seedProjectDirect inserts a project row via store.CreateProject
//     and prepares the on-disk directory tree, intentionally bypassing
//     handleCreateProject. This decouples downstream tests (rename,
//     save, get) from the create handler's side effects (readme.md
//     authoring, feed events, etc.).
package projectapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	cryptoauth "server/auth"
	"server/config"
	"server/handler/spaauth"
	"server/store"

	"github.com/labstack/echo/v4"
)

// testJWTSecret is the secret used for all integration-test JWTs.
// It MUST be set in TestMain before config.Load() runs, because Load
// captures the env var via sync.Once and ignores later changes.
const testJWTSecret = "iotmaker-integration-test-secret-not-for-prod"

// testUserFilesDir is the process-wide root for project files written
// during integration tests. Each test scopes its own subtree below
// this root by using a unique user ID and project ID, so two tests
// never collide on a path.
var testUserFilesDir string

// TestMain prepares package-level state once and runs every test in
// this package under that state. Tests in this package come in two
// flavours: pure validation (no DB, no filesystem) and integration
// (DB + filesystem). The setup here is harmless for the validation
// tests and required for the integration ones, so we always run it.
func TestMain(m *testing.M) {
	// Create a single tmp root that survives the whole package run.
	// We can't use t.TempDir() here because there's no *testing.T;
	// os.MkdirTemp is the equivalent for TestMain.
	tmp, err := os.MkdirTemp("", "iotmaker-projectapi-test-*")
	if err != nil {
		panic("integration_setup_test: MkdirTemp: " + err.Error())
	}
	testUserFilesDir = tmp

	// Pin every env var that config.Load reads. We must do this BEFORE
	// any code in this process calls config.Load() or config.Get(),
	// because Load() uses sync.Once.
	_ = os.Setenv("JWT_SECRET", testJWTSecret)
	_ = os.Setenv("USER_FILES_DIR", testUserFilesDir)
	// DB_PATH is set to a value that won't be used — every integration
	// test opens its own DB at a unique path via setupTestDB.
	_ = os.Setenv("DB_PATH", filepath.Join(tmp, "unused.db"))

	config.Load()

	code := m.Run()

	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// setupTestDB opens a fresh SQLite database at t.TempDir()/test.db,
// runs all migrations and seeds, and registers cleanup that closes
// the connection and clears the global. Each test that needs DB
// access calls this; tests that don't, don't.
//
// SAFETY: store.DB is a package-level global. Go runs tests within a
// single package sequentially by default, so this cleanup-and-replace
// dance is safe as long as no test calls t.Parallel().
func setupTestDB(t *testing.T) {
	t.Helper()

	// Defensive: if a previous test forgot to clean up (or panicked
	// before its cleanup ran), drop the stale connection.
	if store.DB != nil {
		_ = store.DB.Close()
		store.DB = nil
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := store.Open(dbPath); err != nil {
		t.Fatalf("setupTestDB: store.Open(%q): %v", dbPath, err)
	}

	t.Cleanup(func() {
		if store.DB != nil {
			_ = store.DB.Close()
			store.DB = nil
		}
	})
}

// newTestUserToken issues a real JWT for a synthetic user. Tests use
// this to populate the Authorization: Bearer header so the request
// passes spaauth.RequireBearerToken.
//
// Side effect: the user row is also seeded into the DB (idempotently)
// so that any project created on behalf of this user satisfies the
// projects.user_id FOREIGN KEY constraint. setupTestDB(t) MUST have
// been called first.
func newTestUserToken(t *testing.T, userID string) string {
	t.Helper()
	seedUser(t, userID)
	tok, err := cryptoauth.NewJWT(userID, "user", config.Get().JWTSecret)
	if err != nil {
		t.Fatalf("newTestUserToken: NewJWT: %v", err)
	}
	return tok
}

// seedUser inserts a minimal user row with the given ID. INSERT OR
// IGNORE makes it idempotent — calling this multiple times for the
// same userID is safe (newTestUserToken and seedProjectDirect both
// call it for the user they touch).
//
// Why this exists:
//
//	The projects table has a NOT NULL FOREIGN KEY on user_id
//	REFERENCES users(id), and migrate() enables PRAGMA foreign_keys.
//	Any test that creates a project — whether via store.CreateProject
//	directly or via the handler — must have a matching users row or
//	hit "FOREIGN KEY constraint failed (787)" / a 500 from the
//	handler's generic "internal error" path.
//
//	The username, email, and password_hash are placeholders. Each
//	test gets its own SQLite file so the username/email UNIQUE
//	constraints are scoped per-test; "user-<userID>" is unique by
//	construction within a single DB.
func seedUser(t *testing.T, userID string) {
	t.Helper()
	if store.DB == nil {
		t.Fatal("seedUser: store.DB is nil — call setupTestDB(t) first")
	}
	_, err := store.DB.Exec(`
		INSERT OR IGNORE INTO users
			(id, username, email, password_hash, role, verified,
			 preferred_locale, country_code, created_at, updated_at)
		VALUES
			(?, ?, ?, 'test-no-login', 'user', 1,
			 'en-US', '', datetime('now'), datetime('now'))`,
		userID,
		"user-"+userID,       // unique per userID, scoped to this test's DB
		userID+"@test.local", // unique per userID, scoped to this test's DB
	)
	if err != nil {
		t.Fatalf("seedUser(%q): %v", userID, err)
	}
}

// newProjectsEcho builds a minimal Echo app that mounts the project
// routes the integration tests touch. Adding more routes here as new
// tests need them is fine; do NOT mount everything just-in-case.
//
// Note on auth: the /projects/meta/* lookups are intentionally
// public — they expose the static taxonomy needed to populate the
// "create project" form and are mounted WITHOUT
// spaauth.RequireBearerToken to mirror what projectapi.Register does
// in production.
func newProjectsEcho() *echo.Echo {
	e := echo.New()
	g := e.Group("/api/v1")

	// Public lookups — no auth.
	meta := g.Group("/projects/meta")
	meta.GET("/languages", handleListProgrammingLanguages)
	meta.GET("/ui-languages", handleListUILanguages)

	// Authenticated CRUD.
	auth := spaauth.RequireBearerToken()
	projects := g.Group("/projects", auth)
	projects.GET("", handleListProjects)
	projects.POST("", handleCreateProject)
	projects.PUT("/:id", handleUpdateProject)
	projects.DELETE("/:id", handleDeleteProject)

	// Authenticated file operations.
	files := g.Group("/projects/:id/files", auth)
	files.GET("", handleListProjectFiles)

	// Code file: the static-segment routes (/versions, /backup, /rename)
	// must be registered before the parameterised /code so Echo
	// resolves them first. See the comment in routes.go.
	files.GET("/code/versions", handleListCodeVersions)
	files.POST("/code/versions", handleSaveCodeVersion)
	files.GET("/code/backup", handleGetCodeBackup)
	files.POST("/code/backup", handleSaveCodeBackup)
	files.DELETE("/code/backup", handleDeleteCodeBackup)
	files.PUT("/code/rename", handleRenameCodeFile)
	files.GET("/code", handleGetCodeFile)
	files.POST("/code", handleUploadCodeFile)
	files.DELETE("/code", handleDeleteCodeFile)

	return e
}

// authedJSONRequest builds an HTTP request with a JSON body and a
// Bearer token. The body is marshalled from any value; pass nil when
// there is no body (e.g. GET / DELETE).
func authedJSONRequest(t *testing.T, method, path, token string, body any) *http.Request {
	t.Helper()

	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("authedJSONRequest: marshal body: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// decodeOKData unmarshals the "data" field of the standard ok() envelope
// into out. On any structural mismatch the test fails.
func decodeOKData(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decodeOKData: outer: %v. body: %s", err, rec.Body.String())
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		t.Fatalf("decodeOKData: data is null. body: %s", rec.Body.String())
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("decodeOKData: inner: %v. data: %s", err, string(env.Data))
	}
}

// authedMultipartCodeUploadRequest builds an HTTP POST request whose
// body is a multipart form with a single "file" field containing the
// given filename and source bytes. The Authorization header is set
// for the bearer token. This is the shape handleUploadCodeFile reads
// via c.FormFile("file").
func authedMultipartCodeUploadRequest(t *testing.T, path, token, filename string, source []byte) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(source); err != nil {
		t.Fatalf("write multipart body: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// seedProjectDirect inserts a project row via store.CreateProject and
// prepares the on-disk directory tree the same way handleCreateProject
// would, but without the side-effects (readme.md authoring, feed
// events). Returns the new project ID.
//
// Use this in tests that target a handler OTHER than create — it lets
// us isolate "given a project exists" from the create handler's
// behaviour.
//
// Side effect: seeds the user row first (idempotently) so the FK
// from projects.user_id is satisfied. This handles the case where a
// test names a project owner that never authenticates (e.g. the
// "foreign user" rejection test only issues a token for the intruder).
func seedProjectDirect(t *testing.T, userID, name string) string {
	t.Helper()
	return seedProjectDirectLang(t, userID, name, "golang")
}

// seedProjectDirectLang seeds a project with an explicit programming
// language — the multi-file save contract validates extensions PER
// LANGUAGE, so C-flow integration tests need a "c" project to exercise
// the .c/.h whitelist the same way the wizard does.
//
// Português: Semeia projeto com linguagem explícita — o contrato de save
// valida extensão POR LINGUAGEM; testes do fluxo C precisam de projeto "c".
func seedProjectDirectLang(t *testing.T, userID, name, languageID string) string {
	t.Helper()
	seedUser(t, userID)

	id, err := cryptoauth.NewID()
	if err != nil {
		t.Fatalf("seedProjectDirect: NewID: %v", err)
	}

	p := &store.Project{
		ID:                    id,
		UserID:                userID,
		Name:                  name,
		Type:                  store.ProjectTypeCustomDevice,
		Visibility:            store.ProjectVisibilityPrivate,
		ProgrammingLanguageID: languageID,
		UILanguageID:          "en",
	}
	if err := store.CreateProject(p); err != nil {
		t.Fatalf("seedProjectDirect: CreateProject: %v", err)
	}

	cfg := config.Get()
	base := projectBasePath(cfg, userID, p.Type, p.ID)
	for _, dir := range []string{
		filepath.Join(base, store.ProjectFileSectionCode),
		filepath.Join(base, store.ProjectFileSectionImg),
		filepath.Join(base, store.ProjectFileSectionDocs),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("seedProjectDirect: MkdirAll(%q): %v", dir, err)
		}
	}
	return id
}
