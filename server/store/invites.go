// store/invites.go — Invite code CRUD for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Invite codes control access to registration when invite_required=1.
// Any verified user can create an invite. Each code is single-use and
// time-limited to SettingInviteCodeExpiresDays days.
//
// The atomic flow (create user + redeem invite + create profile) lives in
// registration.go so that all three operations share a single transaction.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ─── Sentinel errors ──────────────────────────────────────────────────────────

// ErrInvalidInvite is returned when a code does not exist, has already been
// used, or has expired. A single sentinel is used intentionally — the caller
// should not be able to distinguish between "wrong code" and "used code"
// to prevent enumeration attacks.
var ErrInvalidInvite = errors.New("invite code is invalid, already used, or expired")

// ErrInviteRequired is returned by RegisterUser when invite_required=1 and
// no invite code was supplied in the registration request.
var ErrInviteRequired = errors.New("an invite code is required to register")

// ─── Create ───────────────────────────────────────────────────────────────────

// CreateInvite inserts a new invite code created by a verified user.
// The caller is responsible for generating the code via auth.NewInviteCode()
// and computing ExpiresAt from SettingInviteCodeExpiresDays.
func CreateInvite(inv *InviteCode) error {
	now := time.Now().UTC()
	inv.CreatedAt = now

	_, err := DB.Exec(`
		INSERT INTO invite_codes (id, code, created_by, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		inv.ID, inv.Code, inv.CreatedBy,
		inv.ExpiresAt.UTC().Format(time.RFC3339),
		inv.CreatedAt.Format(time.RFC3339),
	)
	return err
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetInviteByCode returns the invite code record for the given code string.
// Returns ErrNotFound if no such code exists (does not check validity —
// use ValidateInviteCode for that).
func GetInviteByCode(code string) (*InviteCode, error) {
	return scanInvite(DB.QueryRow(`
		SELECT id, code, created_by,
		       COALESCE(used_by, ''), COALESCE(used_at, ''),
		       expires_at, created_at
		FROM invite_codes
		WHERE code = ?`, code))
}

// ValidateInviteCode checks that the code exists, has not been used, and has
// not expired. It does NOT consume the code — that is done inside the
// RegisterUser transaction in registration.go.
//
// Returns ErrInvalidInvite for any failure case (single sentinel — see above).
func ValidateInviteCode(code string) (*InviteCode, error) {
	inv, err := GetInviteByCode(code)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, err
	}
	if inv.UsedBy != "" {
		return nil, ErrInvalidInvite
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return nil, ErrInvalidInvite
	}
	return inv, nil
}

// ListInvitesByUser returns all invite codes created by the given user,
// ordered newest first. Used by the profile page to show invite history.
func ListInvitesByUser(userID string) ([]*InviteCode, error) {
	rows, err := DB.Query(`
		SELECT id, code, created_by,
		       COALESCE(used_by, ''), COALESCE(used_at, ''),
		       expires_at, created_at
		FROM invite_codes
		WHERE created_by = ?
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []*InviteCode
	for rows.Next() {
		inv, err := scanInviteRow(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

// ─── Helpers (used inside registration.go transaction) ───────────────────────

// validateInviteInTx checks the invite code is valid inside an active transaction.
// This is the transactional version of ValidateInviteCode — called exclusively
// from registration.go so the validation and the update are atomic.
func validateInviteInTx(tx *sql.Tx, code string) (*InviteCode, error) {
	inv, err := scanInvite(tx.QueryRow(`
		SELECT id, code, created_by,
		       COALESCE(used_by, ''), COALESCE(used_at, ''),
		       expires_at, created_at
		FROM invite_codes
		WHERE code = ?`, code))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidInvite
	}
	if err != nil {
		return nil, err
	}
	if inv.UsedBy != "" {
		return nil, ErrInvalidInvite
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		return nil, ErrInvalidInvite
	}
	return inv, nil
}

// redeemInviteInTx marks the invite as used inside an active transaction.
// Must only be called after the user row has been created (FK constraint).
func redeemInviteInTx(tx *sql.Tx, code, usedByID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(`
		UPDATE invite_codes
		SET used_by = ?, used_at = ?
		WHERE code = ? AND used_by IS NULL`,
		usedByID, now, code,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

// scanInvite scans a *sql.Row into an InviteCode and returns a pointer.
func scanInvite(row *sql.Row) (*InviteCode, error) {
	var inv InviteCode
	var usedBy, usedAt, expiresAt, createdAt string
	err := row.Scan(
		&inv.ID, &inv.Code, &inv.CreatedBy,
		&usedBy, &usedAt,
		&expiresAt, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	inv.UsedBy = usedBy
	if usedAt != "" {
		inv.UsedAt, _ = time.Parse(time.RFC3339, usedAt)
	}
	inv.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	inv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &inv, nil // ← pointer to the local value
}

// scanInviteRow scans a *sql.Rows into an InviteCode and returns a pointer.
func scanInviteRow(rows *sql.Rows) (*InviteCode, error) {
	var inv InviteCode
	var usedBy, usedAt, expiresAt, createdAt string
	err := rows.Scan(
		&inv.ID, &inv.Code, &inv.CreatedBy,
		&usedBy, &usedAt,
		&expiresAt, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	inv.UsedBy = usedBy
	if usedAt != "" {
		inv.UsedAt, _ = time.Parse(time.RFC3339, usedAt)
	}
	inv.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	inv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &inv, nil // ← pointer to the local value
}
