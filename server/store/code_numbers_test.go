// server/store/code_numbers_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package store

// CONTRACT TESTS for CodeNumberAllocator (see code_numbers.go for the five
// clauses). These assert the CONTRACT, not the engine: when the database
// migrates, re-point the setup at the new implementation and every test in
// this file must still pass unchanged — that is how "engine-agnostic" is
// enforced rather than merely claimed.
//
// NOTE: like every test in the store package, these compile only where the
// SQLite driver's dependencies are available (the project's Docker flow —
// `make docker-up-full`); the offline codegen recipe cannot build them.
//
// Português: Testes de CONTRATO do alocador (cinco cláusulas em
// code_numbers.go). Testam o contrato, não o motor: na migração de banco,
// re-aponte o setup para a implementação nova e tudo aqui deve passar sem
// mudanças — é assim que "agnóstico" é imposto, não só declarado. Como todo
// teste do store, compila só no fluxo Docker.

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"
)

// setupCodeNumbersTest opens an in-memory database, points the package-level
// DB at it, creates the minimal owner tables the backfill reads, and runs
// MigrateCodeNumbers. Reassigning DB is safe because tests run sequentially
// within the package.
func setupCodeNumbersTest(t *testing.T) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	DB = db

	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			created_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS blackboxes (
			id         TEXT PRIMARY KEY,
			created_at TEXT
		)`,
	} {
		if _, err := DB.Exec(s); err != nil {
			t.Fatalf("create schema: %v\n%s", err, s)
		}
	}
	if err := MigrateCodeNumbers(); err != nil {
		t.Fatalf("MigrateCodeNumbers: %v", err)
	}
}

// TestCodeNumbers_StrictlyIncreasing_SharedCounter asserts clauses 1 and 5
// together: numbers are positive and strictly increasing IN ALLOCATION ORDER
// regardless of kind — wizard projects and marketplace black-boxes draw from
// the one counter, because both feed the same def identity space.
func TestCodeNumbers_StrictlyIncreasing_SharedCounter(t *testing.T) {
	setupCodeNumbersTest(t)

	ids := []struct{ id, kind string }{
		{"aaaa", CodeKindProject},
		{"bbbb", CodeKindBlackBox},
		{"cccc", CodeKindProject},
	}
	var prev int64
	for _, c := range ids {
		n, err := AllocateCodeNumber(c.id, c.kind)
		if err != nil {
			t.Fatalf("allocate %s: %v", c.id, err)
		}
		if n <= 0 {
			t.Fatalf("allocate %s: n = %d, want positive", c.id, n)
		}
		if n <= prev {
			t.Fatalf("allocate %s: n = %d, want > previous %d (strictly increasing, one counter)", c.id, n, prev)
		}
		prev = n
	}
}

// TestCodeNumbers_IdempotentPerID asserts clause 3: re-allocating for an id
// that already holds a number returns that number and does not advance the
// sequence — the property that makes creation hooks safe under retries.
func TestCodeNumbers_IdempotentPerID(t *testing.T) {
	setupCodeNumbersTest(t)

	first, err := AllocateCodeNumber("aaaa", CodeKindBlackBox)
	if err != nil {
		t.Fatalf("first allocate: %v", err)
	}
	again, err := AllocateCodeNumber("aaaa", CodeKindBlackBox)
	if err != nil {
		t.Fatalf("re-allocate: %v", err)
	}
	if again != first {
		t.Fatalf("re-allocate: got %d, want the original %d (idempotent per id)", again, first)
	}
	next, err := AllocateCodeNumber("bbbb", CodeKindBlackBox)
	if err != nil {
		t.Fatalf("next allocate: %v", err)
	}
	if next != first+1 {
		t.Fatalf("sequence advanced by the no-op re-allocation: next = %d, want %d", next, first+1)
	}
}

// TestCodeNumbers_NeverReusedAfterDelete asserts clause 2, the one that
// protects exported code: even when a registry row is deleted outright, its
// number is never handed out again. (In the current engine that is
// AUTOINCREMENT's job; in a future engine it must be someone's job — this
// test is the enforcement.)
func TestCodeNumbers_NeverReusedAfterDelete(t *testing.T) {
	setupCodeNumbersTest(t)

	if _, err := AllocateCodeNumber("aaaa", CodeKindProject); err != nil {
		t.Fatalf("allocate aaaa: %v", err)
	}
	b, err := AllocateCodeNumber("bbbb", CodeKindProject)
	if err != nil {
		t.Fatalf("allocate bbbb: %v", err)
	}
	if _, err := DB.Exec(`DELETE FROM code_numbers WHERE full_id = 'bbbb'`); err != nil {
		t.Fatalf("delete bbbb: %v", err)
	}
	c, err := AllocateCodeNumber("cccc", CodeKindProject)
	if err != nil {
		t.Fatalf("allocate cccc: %v", err)
	}
	if c <= b {
		t.Fatalf("number reused after delete: got %d, want > %d", c, b)
	}
}

// TestCodeNumbers_Lookup pins CodeNumberFor's two answers: (n, true) for a
// registered id, (0, false) — not an error — for an unknown one, which is
// what lets the loader degrade to the full-id fallback quietly.
func TestCodeNumbers_Lookup(t *testing.T) {
	setupCodeNumbersTest(t)

	want, err := AllocateCodeNumber("aaaa", CodeKindProject)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	n, ok, err := CodeNumberFor("aaaa")
	if err != nil || !ok || n != want {
		t.Fatalf("lookup present: got (%d, %v, %v), want (%d, true, nil)", n, ok, err, want)
	}
	n, ok, err = CodeNumberFor("missing")
	if err != nil || ok || n != 0 {
		t.Fatalf("lookup absent: got (%d, %v, %v), want (0, false, nil)", n, ok, err)
	}
	if got := codeNumberString("aaaa"); got != formatInt64(want) {
		t.Fatalf("codeNumberString present: got %q, want %q", got, formatInt64(want))
	}
	if got := codeNumberString("missing"); got != "" {
		t.Fatalf("codeNumberString absent: got %q, want \"\"", got)
	}
}

// TestCodeNumbers_ConcurrentAllocation asserts clause 4 the only honest way:
// many goroutines allocating distinct ids must produce all-distinct numbers,
// and re-allocating a contended id from several goroutines must converge on
// one number.
func TestCodeNumbers_ConcurrentAllocation(t *testing.T) {
	setupCodeNumbersTest(t)

	const workers = 16
	var wg sync.WaitGroup
	results := make([]int64, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = AllocateCodeNumber(fmt.Sprintf("id-%02d", i), CodeKindBlackBox)
		}(i)
	}
	wg.Wait()
	seen := make(map[int64]int)
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if prev, dup := seen[results[i]]; dup {
			t.Fatalf("number %d allocated to both id-%02d and id-%02d", results[i], prev, i)
		}
		seen[results[i]] = i
	}

	// Contended single id: every goroutine must see the same number.
	contended := make([]int64, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			contended[i], _ = AllocateCodeNumber("contended", CodeKindBlackBox)
		}(i)
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if contended[i] != contended[0] {
			t.Fatalf("contended id diverged: %d vs %d", contended[i], contended[0])
		}
	}
}

// TestMigrateCodeNumbers_Backfill pins the migration's promise: pre-existing
// projects and black-boxes are numbered by CREATION order (interleaved
// across the two tables), the oldest first — as if the registry had always
// existed — and re-running the migration is a no-op.
func TestMigrateCodeNumbers_Backfill(t *testing.T) {
	setupCodeNumbersTest(t)

	for _, q := range []string{
		`INSERT INTO projects   (id, user_id, created_at) VALUES ('proj-new', 'u1', '2026-03-01T00:00:00Z')`,
		`INSERT INTO projects   (id, user_id, created_at) VALUES ('proj-old', 'u1', '2026-01-01T00:00:00Z')`,
		`INSERT INTO blackboxes (id, created_at)           VALUES ('bb-mid',        '2026-02-01T00:00:00Z')`,
	} {
		if _, err := DB.Exec(q); err != nil {
			t.Fatalf("seed: %v: %s", err, q)
		}
	}
	if err := MigrateCodeNumbers(); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	want := map[string]int64{"proj-old": 1, "bb-mid": 2, "proj-new": 3}
	for id, n := range want {
		got, ok, err := CodeNumberFor(id)
		if err != nil || !ok {
			t.Fatalf("lookup %s: (%v, %v)", id, ok, err)
		}
		if got != n {
			t.Fatalf("%s: got %d, want %d (creation order across both tables)", id, got, n)
		}
	}

	// Idempotence: a second run changes nothing.
	if err := MigrateCodeNumbers(); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	for id, n := range want {
		got, _, _ := CodeNumberFor(id)
		if got != n {
			t.Fatalf("re-run changed %s: got %d, want %d", id, got, n)
		}
	}
}
