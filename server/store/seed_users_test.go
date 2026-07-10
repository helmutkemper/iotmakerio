// server/store/seed_users_test.go — Seeds 50 random users for frontend testing.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// This is intentionally NOT a unit test — it is a DEV TOOL that writes to
// the real SQLite file so you can point the server at it and test the
// control panel UI with realistic data.
//
// It is OPT-IN: without IOTM_SEED_USERS=1 it self-skips. The original
// guard was opt-OUT (-short), which meant a plain `go test ./store/`
// — the natural validation command — silently seeded 48 fake users into
// the developer's live database (2026-07-08, caught in the field). A
// tool that mutates the live DB must require an explicit yes, not the
// memory of a flag.
//
// Português: FERRAMENTA de dev, não teste — grava usuários falsos no
// banco real para testar a UI do /control. OPT-IN via IOTM_SEED_USERS=1;
// sem a variável, pula. O guard antigo era opt-OUT (-short) e um
// `go test ./store/` comum semeava o banco vivo em silêncio.
//
// Usage:
//
//	cd server
//	IOTM_SEED_USERS=1 go test ./store/ -run TestSeedRandomUsers -v -count=1
//
// The database is written to ./data/iotmaker.db (same path the server uses).
// Run the server normally after seeding; the 50 users will appear in /control.
//
// Roles are distributed across the three role types to give the UI something
// interesting to display:
//
//	~70% user
//	~20% official_specialist
//	~10% admin  (besides the default seed admin)
//
// All generated users share the password "test1234" so you can log in as any
// of them from the portal if needed.
package store

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v6"

	cryptoauth "server/auth"
)

// passwordHash is pre-computed once for "test1234" to avoid 50 bcrypt rounds.
var testPasswordHash string

func init() {
	var err error
	testPasswordHash, err = cryptoauth.HashPassword("test1234")
	if err != nil {
		panic("seed_users_test: could not hash password: " + err.Error())
	}
}

func TestSeedRandomUsers(t *testing.T) {
	// Opt-IN gate — see the file header for why opt-out was not enough.
	if os.Getenv("IOTM_SEED_USERS") != "1" {
		t.Skip("dev tool, not a test — set IOTM_SEED_USERS=1 to seed the live DB")
	}
	if testing.Short() {
		t.Skip("skipping seed test in -short mode")
	}

	// Open the real development database, not a temp one.
	// The test file lives in server/store/, so ../data/iotmaker.db is the
	// standard development database path.
	_, filename, _, _ := runtime.Caller(0)
	dbPath := filepath.Join(filepath.Dir(filename), "..", "data", "iotmaker.db")
	t.Logf("opening database: %s", dbPath)

	if err := Open(dbPath); err != nil {
		t.Fatalf("Open(%q): %v", dbPath, err)
	}

	gofakeit.Seed(0) // deterministic run — same users every time

	roles := weightedRoles()
	created := 0
	skipped := 0

	for i := 0; i < 50; i++ {
		id := cryptoauth.MustNewID()
		username := uniqueUsername(i)
		email := fmt.Sprintf("%s@%s", gofakeit.Username(), gofakeit.DomainName())

		u := &User{
			ID:              id,
			Username:        username,
			Email:           email,
			PasswordHash:    testPasswordHash,
			Role:            roles[i],
			Verified:        true,
			PreferredLocale: randomLocale(),
		}

		// A seeding tool that randomly drops rows is a bad tool: macOS +
		// modernc occasionally throws SQLITE_IOERR_DELETE_NOENT (5898) on
		// the rollback-journal delete under rapid sequential writes — a
		// transient race, gone on retry. Three attempts with a breath
		// between them; a persistent error still surfaces.
		//
		// Português: macOS + modernc às vezes solta IOERR_DELETE_NOENT
		// (5898) na deleção do journal sob escrita rápida — transiente,
		// some no retry. Três tentativas; erro persistente ainda aparece.
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = CreateUser(u); err == nil || err == ErrConflict {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if err != nil {
			if err == ErrConflict {
				// Username or email already exists — skip silently.
				skipped++
				continue
			}
			t.Errorf("CreateUser(%q): %v", username, err)
			continue
		}
		created++
		t.Logf("[%02d] %-30s %-40s %s", i+1, username, email, u.Role)
	}

	t.Logf("done: %d created, %d skipped (conflict)", created, skipped)
}

// weightedRoles returns a 50-element slice with a realistic role distribution:
// 35 user, 10 official_specialist, 5 admin.
func weightedRoles() []string {
	roles := make([]string, 0, 50)
	for i := 0; i < 35; i++ {
		roles = append(roles, RoleUser)
	}
	for i := 0; i < 10; i++ {
		roles = append(roles, RoleOfficialSpecialist)
	}
	for i := 0; i < 5; i++ {
		roles = append(roles, RoleAdmin)
	}
	// Shuffle so roles are not grouped.
	rand.Shuffle(len(roles), func(i, j int) { roles[i], roles[j] = roles[j], roles[i] })
	return roles
}

// uniqueUsername generates a username that is unlikely to collide with existing
// ones by appending the index as a suffix.
func uniqueUsername(i int) string {
	name := gofakeit.Username()
	return fmt.Sprintf("%s_%02d", name, i)
}

// randomLocale returns one of the supported portal locales.
func randomLocale() string {
	locales := []string{"pt-BR", "en-US", "es-ES"}
	return locales[rand.Intn(len(locales))]
}
