// Package auth
//
// Cryptographic primitives for the IoTMaker portal.
//
// This file is the single source of truth for all security-sensitive operations:
//   - Password hashing and verification (bcrypt)
//   - Session token generation (crypto/rand, URL-safe hex)
//   - OTP code generation (crypto/rand, 6-digit decimal)
//   - Invite code generation (crypto/rand, 32-char hex)
//   - Opaque ID generation for database primary keys
//
// Functions here have no side effects and no external dependencies beyond
// golang.org/x/crypto/bcrypt and the standard library.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor for bcrypt.
// Cost 12 ≈ 300 ms on modern hardware — expensive enough to slow brute-force
// attacks, cheap enough for normal login throughput.
const bcryptCost = 12

// ─── Password ────────────────────────────────────────────────────────────────

// HashPassword returns a bcrypt digest of the plaintext password.
func HashPassword(plaintext string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword reports whether plaintext matches the stored bcrypt hash.
// Returns true on success, false on mismatch or error.
func CheckPassword(hash, plaintext string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	return err == nil
}

// ─── Session token ───────────────────────────────────────────────────────────

// NewSessionToken generates a 32-byte cryptographically random token
// encoded as a 64-character lowercase hex string.
// Collisions are statistically impossible (2^256 possible values).
func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ─── OTP code ────────────────────────────────────────────────────────────────

// NewOTPCode generates a 6-digit numeric one-time password using crypto/rand.
// The result is always exactly 6 characters (zero-padded).
func NewOTPCode() (string, error) {
	// Draw a random integer in [0, 1_000_000)
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("auth: generate otp: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// ─── Invite code ─────────────────────────────────────────────────────────────

// NewInviteCode generates a 16-byte cryptographically random invite code
// encoded as a 32-character lowercase hex string.
//
// Properties:
//   - 2^128 possible values — brute-force is computationally infeasible even at
//     millions of requests per second over thousands of years.
//   - Single-use: codes are marked as consumed in the database on first redemption.
//   - Time-limited: codes expire after a configurable number of days
//     (see SettingInviteCodeExpiresDays in store/settings.go).
//
// Codes are shared via URL:
//
//	{origin}/app#register?invite={code}
func NewInviteCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate invite code: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ─── Opaque ID ────────────────────────────────────────────────────────────────

// NewID generates a 16-byte random identifier encoded as a 32-character
// lowercase hex string. Used as primary key for users, OTP records, and invites.
func NewID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// MustNewID is like NewID but panics on error.
// Use only in contexts where a failure is unrecoverable (e.g., startup seed).
func MustNewID() string {
	id, err := NewID()
	if err != nil {
		panic(err)
	}
	return id
}
