// store/registration.go — Atomic user registration transaction.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// This file owns the single entry point for creating a new user account.
// All three DB operations (create user, redeem invite, create profile) are
// wrapped in one sql.Tx so that a failure at any step leaves the database
// in its original state — no orphaned user records, no half-consumed invites.
//
// Why a dedicated file?
//   - CreateUser in users.go is reused by admin tools and tests that don't
//     need invite validation or profile creation.
//   - CreateProfile in profiles.go is reused by EnsureProfile.
//   - The transaction logic that glues them together belongs neither to users
//     nor to profiles — it belongs to the registration concept itself.
package store

import (
	"database/sql"
	"time"
)

// RegisterUserArgs holds all data needed to register a new user atomically.
type RegisterUserArgs struct {
	User       *User
	Profile    *UserProfile
	InviteCode string // empty when invite_required=0
}

// RegisterUser atomically validates the invite, creates the user, redeems the
// invite, and creates the profile — all inside a single sql.Tx.
//
// Sentinel errors:
//   - ErrInviteRequired — invite_required=1 but no code was supplied.
//   - ErrInvalidInvite  — code does not exist, is used, or is expired.
//   - ErrConflict       — username or email is already taken.
func RegisterUser(args RegisterUserArgs) error {
	inviteRequired := GetSettingInt(SettingInviteRequired, 1) == 1
	if inviteRequired && args.InviteCode == "" {
		return ErrInviteRequired
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if args.InviteCode != "" {
		if _, err := validateInviteInTx(tx, args.InviteCode); err != nil {
			return err
		}
	}

	if err := createUserInTx(tx, args.User); err != nil {
		return err
	}

	if args.InviteCode != "" {
		if err := redeemInviteInTx(tx, args.InviteCode, args.User.ID); err != nil {
			return err
		}
	}

	if err := createProfileInTx(tx, args.Profile); err != nil {
		return err
	}

	return tx.Commit()
}

// createUserInTx inserts a user row using the provided transaction.
// Returns ErrConflict on UNIQUE constraint violation.
func createUserInTx(tx *sql.Tx, u *User) error {
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now

	_, err := tx.Exec(`
		INSERT INTO users
			(id, username, email, password_hash, role, verified, preferred_locale, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.Email, u.PasswordHash,
		u.Role, boolToInt(u.Verified), u.PreferredLocale,
		u.CreatedAt.Format(time.RFC3339),
		u.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}
