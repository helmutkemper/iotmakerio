// server/store/seed_users_test.go — Seeds 50 random users for frontend testing.
//
// This test is intentionally NOT a unit test — it writes to a real SQLite file
// so you can point the server at it and test the control panel UI with realistic
// data. It is skipped in CI by the -short flag.
//
// Usage:
//
//	cd server
//	go test ./store/ -run TestSeedRandomUsers -v -count=1
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
	"path/filepath"
	"runtime"
	"testing"

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

		if err := CreateUser(u); err != nil {
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
