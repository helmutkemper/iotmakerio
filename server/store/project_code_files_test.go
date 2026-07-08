// server/store/project_code_files_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"database/sql"
	"testing"
)

// Store-level pins for the snapshot model (see project_code_files.go for
// the doctrine). The handler integration tests exercise the same paths
// through HTTP; these tests exist so a storage regression is reported at
// the layer that owns it — a broken ORDER BY should fail HERE, not as a
// mysterious tab-shuffle three layers up.
//
// Docker-only, like every store test: modernc sqlite does not compile in
// the offline sandbox. Run inside the container:
//
//	cd server && go test ./store/ -run CodeSnapshot -count=1
//
// Português: Fixações da camada de store para o modelo snapshot. Os testes
// de integração exercitam o mesmo via HTTP; estes existem para regressão de
// armazenamento falhar NA camada dona — ORDER BY quebrado falha AQUI, não
// como embaralhamento de abas três camadas acima. Docker-only.

// setupCodeSnapshotTest opens an in-memory database with the two snapshot
// tables plus the minimal parent rows the foreign keys demand.
func setupCodeSnapshotTest(t *testing.T) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	DB = db

	for _, stmt := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY)`,
		`CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL DEFAULT '',
			programming_language_id TEXT
		)`,
		`CREATE TABLE project_code_versions (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			version INTEGER NOT NULL DEFAULT 1,
			last_parse_ok INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			UNIQUE(project_id, version)
		)`,
		`INSERT INTO users (id) VALUES ('u1')`,
		`INSERT INTO projects (id, user_id, name, programming_language_id)
		 VALUES ('p1', 'u1', 'Probe', 'c')`,
	} {
		if _, err := DB.Exec(stmt); err != nil {
			t.Fatalf("setup stmt failed: %v\n%s", err, stmt)
		}
	}
	if err := MigrateProjectCodeFiles(); err != nil {
		t.Fatalf("MigrateProjectCodeFiles: %v", err)
	}
}

// TestCodeSnapshot_RoundTripPreservesOrder pins the core contract: a save
// is atomic over the set, and slice order (tab order) survives storage —
// including a path that would sort differently alphabetically.
func TestCodeSnapshot_RoundTripPreservesOrder(t *testing.T) {
	setupCodeSnapshotTest(t)

	files := []CodeFileEntry{
		{Path: "zeta.h", Content: "// deliberately alphabetical-last, tab-first"},
		{Path: "core.c", Content: "int a;"},
		{Path: "util/helpers.c", Content: "int b;"},
	}
	v := &ProjectCodeVersion{
		ID: "v1", ProjectID: "p1", UserID: "u1", Version: 1, Files: files,
	}
	if err := CreateProjectCodeVersion(v); err != nil {
		t.Fatalf("CreateProjectCodeVersion: %v", err)
	}

	got, err := GetLatestProjectCodeVersion("p1")
	if err != nil {
		t.Fatalf("GetLatestProjectCodeVersion: %v", err)
	}
	if len(got.Files) != 3 {
		t.Fatalf("files count: got %d, want 3", len(got.Files))
	}
	for i := range files {
		if got.Files[i].Path != files[i].Path || got.Files[i].Content != files[i].Content {
			t.Errorf("entry %d drift: got %+v, want %+v", i, got.Files[i], files[i])
		}
	}
}

// TestCodeSnapshot_VersionsAreImmutableSnapshots: saving version 2 must not
// disturb version 1's set — restore means "read the old rows", so the old
// rows must still be exactly what was saved.
func TestCodeSnapshot_VersionsAreImmutableSnapshots(t *testing.T) {
	setupCodeSnapshotTest(t)

	v1 := &ProjectCodeVersion{ID: "v1", ProjectID: "p1", UserID: "u1", Version: 1,
		Files: []CodeFileEntry{{Path: "core.c", Content: "int v = 1;"}}}
	if err := CreateProjectCodeVersion(v1); err != nil {
		t.Fatalf("v1: %v", err)
	}
	v2 := &ProjectCodeVersion{ID: "v2", ProjectID: "p1", UserID: "u1", Version: 2,
		Files: []CodeFileEntry{
			{Path: "core.c", Content: "int v = 2;"},
			{Path: "api.h", Content: "typedef int t;"},
		}}
	if err := CreateProjectCodeVersion(v2); err != nil {
		t.Fatalf("v2: %v", err)
	}

	old, err := GetProjectCodeVersionByID("v1")
	if err != nil {
		t.Fatalf("GetProjectCodeVersionByID(v1): %v", err)
	}
	if len(old.Files) != 1 || old.Files[0].Content != "int v = 1;" {
		t.Errorf("v1 snapshot mutated: %+v", old.Files)
	}

	all, err := ListProjectCodeVersions("p1")
	if err != nil {
		t.Fatalf("ListProjectCodeVersions: %v", err)
	}
	if len(all) != 2 || len(all[0].Files) != 2 || len(all[1].Files) != 1 {
		t.Errorf("history shape drift: got %d versions, latest %d files, oldest %d files",
			len(all), len(all[0].Files), len(all[1].Files))
	}
}

// TestCodeSnapshot_ListLatestFiltersEmptySnapshots: the catalog queries
// only surface projects whose latest snapshot has at least one non-empty
// file — the EXISTS-over-children translation of the old source != ”.
func TestCodeSnapshot_ListLatestFiltersEmptySnapshots(t *testing.T) {
	setupCodeSnapshotTest(t)

	// p1's latest is all-empty → filtered out.
	empty := &ProjectCodeVersion{ID: "v1", ProjectID: "p1", UserID: "u1", Version: 1,
		Files: []CodeFileEntry{{Path: "core.c", Content: ""}}}
	if err := CreateProjectCodeVersion(empty); err != nil {
		t.Fatalf("empty save: %v", err)
	}
	items, err := ListAllLatestProjectCodeVersions()
	if err != nil {
		t.Fatalf("ListAllLatest (empty case): %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty latest must be filtered: got %d items", len(items))
	}

	// A newer non-empty snapshot brings the project back.
	full := &ProjectCodeVersion{ID: "v2", ProjectID: "p1", UserID: "u1", Version: 2,
		Files: []CodeFileEntry{{Path: "core.c", Content: "int a;"}}}
	if err := CreateProjectCodeVersion(full); err != nil {
		t.Fatalf("full save: %v", err)
	}
	items, err = ListAllLatestProjectCodeVersions()
	if err != nil {
		t.Fatalf("ListAllLatest (full case): %v", err)
	}
	if len(items) != 1 || len(items[0].Files) != 1 || items[0].Language != "c" {
		t.Fatalf("latest listing drift: %+v", items)
	}
}
