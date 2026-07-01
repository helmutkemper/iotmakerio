// server/store/seed.go — Database seeding for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Seeding is split across files by domain:
//
//	seed.go        — admin user bootstrap (this file)
//	i18n.go        — translation bundles (SeedTranslations)
//	db.go          — locales, categories, UI languages, project settings…
//
// SeedAdmin creates the initial admin user if no admin account exists.
// It is called once from main() after the database migrations run.
//
// The seeded password is hardcoded for development convenience. Anyone with
// access to this source knows it — the first action in any non-dev deploy
// must be to log in and change it.
package store

import (
	"errors"
	"log"

	"server/auth"
)

// defaultAdminPassword is the fixed password used for the seeded admin account
// during development. It is intentionally hardcoded for convenience.
//
// ⚠ SECURITY WARNING: this password is published in the source tree. Change
// it immediately after the first login in any environment reachable from a
// network.
const defaultAdminPassword = "#Senha123"

// SeedAdmin creates the first admin account if the database has no admin user.
// Safe to call on every startup — it is a no-op when an admin already exists.
func SeedAdmin() error {
	// Check whether any admin user exists.
	exists, err := adminExists()
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	hash, err := auth.HashPassword(defaultAdminPassword)
	if err != nil {
		return err
	}

	id, err := auth.NewID()
	if err != nil {
		return err
	}

	u := &User{
		ID:              id,
		Username:        "admin",
		Email:           "admin@iotmaker.io",
		PasswordHash:    hash,
		Role:            RoleAdmin,
		Verified:        true,
		PreferredLocale: "en-US",
	}

	if err := CreateUser(u); err != nil {
		if errors.Is(err, ErrConflict) {
			// A race at startup; another process already seeded. Not an error.
			return nil
		}
		return err
	}

	log.Println("╔══════════════════════════════════════════════════════════╗")
	log.Println("║  [SEED] Admin account created                           ║")
	log.Printf("║         username : %-37s║\n", "admin")
	log.Printf("║         email    : %-37s║\n", "admin@iotmaker.io")
	log.Printf("║         password : %-37s║\n", defaultAdminPassword)
	log.Println("║                                                          ║")
	log.Println("║  ⚠  SECURITY WARNING: password is hardcoded in source.  ║")
	log.Println("║     Change it immediately after first login.             ║")
	log.Println("╚══════════════════════════════════════════════════════════╝")

	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// adminExists reports whether at least one user has the admin role.
func adminExists() (bool, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
