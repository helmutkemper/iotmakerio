// store/otp.go — One-time password persistence for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// OTP codes are stored in the database rather than in-memory so they survive
// server restarts and work correctly in multi-process deployments.
//
// Each code is tied to a (user_id, purpose) pair and expires after 15 minutes.
// Consuming a code (ConsumeOTP) marks it used in a single atomic update so
// race conditions between concurrent requests are impossible.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// OTPLifetime is how long a code is valid after creation.
const OTPLifetime = 15 * time.Minute

// ─── Create ──────────────────────────────────────────────────────────────────

// CreateOTP persists a new one-time code.
// Any existing codes for the same (user_id, purpose) are invalidated first
// so the user always has exactly one active code per purpose.
func CreateOTP(o *OTPCode) error {
	now := time.Now().UTC()
	o.ExpiresAt = now.Add(OTPLifetime)

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Invalidate older codes for the same purpose (mark them used).
	if _, err = tx.Exec(`
		UPDATE otp_codes SET used = 1
		WHERE user_id = ? AND purpose = ? AND used = 0`,
		o.UserID, o.Purpose,
	); err != nil {
		return err
	}

	// Insert the new code.
	if _, err = tx.Exec(`
		INSERT INTO otp_codes (id, user_id, code, purpose, expires_at, used)
		VALUES (?, ?, ?, ?, ?, 0)`,
		o.ID, o.UserID, o.Code, o.Purpose,
		o.ExpiresAt.Format(time.RFC3339),
	); err != nil {
		return err
	}

	return tx.Commit()
}

// ─── Consume ──────────────────────────────────────────────────────────────────

// ConsumeOTP validates a code and marks it as used in a single atomic operation.
//
// Returns ErrNotFound when:
//   - No active code exists for the (userID, purpose) pair.
//   - The code has expired.
//   - The code was already used.
//   - The supplied code does not match the stored code.
func ConsumeOTP(userID, code, purpose string) error {
	res, err := DB.Exec(`
		UPDATE otp_codes
		SET    used = 1
		WHERE  user_id    = ?
		  AND  purpose    = ?
		  AND  code       = ?
		  AND  used       = 0
		  AND  expires_at > datetime('now')`,
		userID, purpose, code,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound // code invalid, expired, or already used
	}
	return nil
}

// ─── Lookup ───────────────────────────────────────────────────────────────────

// GetActiveOTP returns the latest active OTP for the given user and purpose.
// Useful only for debugging / admin tooling; not needed by normal auth flows.
func GetActiveOTP(userID, purpose string) (*OTPCode, error) {
	var o OTPCode
	var expiresAt string
	err := DB.QueryRow(`
		SELECT id, user_id, code, purpose, expires_at
		FROM   otp_codes
		WHERE  user_id    = ?
		  AND  purpose    = ?
		  AND  used       = 0
		  AND  expires_at > datetime('now')
		ORDER  BY expires_at DESC
		LIMIT  1`,
		userID, purpose,
	).Scan(&o.ID, &o.UserID, &o.Code, &o.Purpose, &expiresAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	return &o, nil
}

// ─── Maintenance ─────────────────────────────────────────────────────────────

// PruneExpiredOTPs deletes codes that are past their expiry time.
func PruneExpiredOTPs() error {
	_, err := DB.Exec(`DELETE FROM otp_codes WHERE expires_at <= datetime('now')`)
	return err
}
