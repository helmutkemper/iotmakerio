// server/store/api_keys.go — API key management for IoTMaker live device communication.
//
// API keys authenticate external hardware and scripts that send data to the
// IoTMaker server via webhooks. Each key is scoped to a single device within
// a project, providing granular security isolation: compromising one device's
// key does not affect other devices or the user's portal account.
//
// Security model:
//   - The raw key (64-char hex, 256 bits of entropy) is shown to the user
//     exactly once at creation time. It is NEVER stored in the database.
//   - Only the SHA-256 hash of the key is persisted. Validation hashes the
//     incoming key and compares against the stored hash.
//   - Keys have no mandatory expiration (field is nullable) because hardware
//     deployed in the field cannot be easily updated with new credentials.
//   - Keys can be soft-revoked by setting revoked_at, which is checked on
//     every validation call.
//
// Português:
//
//	Chaves de API autenticam hardware e scripts externos. Cada chave é
//	vinculada a um único device dentro de um projeto. A chave bruta é
//	mostrada uma única vez; apenas o hash SHA-256 é armazenado. Sem
//	validade obrigatória — equipamento em campo não pode parar.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ─── Errors ───────────────────────────────────────────────────────────────────

var (
	// ErrAPIKeyNotFound is returned when no matching active key exists.
	ErrAPIKeyNotFound = errors.New("api key not found")

	// ErrAPIKeyRevoked is returned when the key exists but has been revoked.
	ErrAPIKeyRevoked = errors.New("api key has been revoked")

	// ErrAPIKeyExpired is returned when the key has passed its optional expiry.
	ErrAPIKeyExpired = errors.New("api key has expired")
)

// ─── Model ────────────────────────────────────────────────────────────────────

// APIKey represents a device-scoped authentication credential for webhooks.
//
// The raw key is never stored. Only key_hash (SHA-256 hex) is persisted.
// Validation: hash the incoming key and compare with key_hash.
type APIKey struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	ProjectID string  `json:"project_id"`
	DeviceID  string  `json:"device_id"`
	KeyHash   string  `json:"-"` // SHA-256 hex — never exposed via JSON
	Label     string  `json:"label"`
	ExpiresAt *string `json:"expires_at,omitempty"` // RFC3339 or nil (no expiry)
	RevokedAt *string `json:"revoked_at,omitempty"` // RFC3339 or nil (active)
	CreatedAt string  `json:"created_at"`
}

// ─── Key generation ───────────────────────────────────────────────────────────

// GenerateAPIKey creates a cryptographically random 256-bit key and returns
// both the raw hex string (to show the user once) and its SHA-256 hash
// (to store in the database).
//
// The raw key is 64 hex characters (32 bytes of entropy).
// The hash is 64 hex characters (SHA-256 digest).
func GenerateAPIKey() (rawKey, keyHash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("store: generate api key: %w", err)
	}
	rawKey = hex.EncodeToString(b)
	keyHash = HashAPIKey(rawKey)
	return rawKey, keyHash, nil
}

// HashAPIKey returns the SHA-256 hex digest of a raw API key string.
// Used both at creation (to store) and at validation (to compare).
func HashAPIKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

// CreateAPIKey inserts a new API key record into the database.
// The caller must generate the ID (via cryptoauth.MustNewID()) and the
// key hash (via GenerateAPIKey()) before calling this function.
//
// Returns the created APIKey struct (without the raw key — that is only
// available at generation time).
func CreateAPIKey(key *APIKey) error {
	_, err := DB.Exec(`
		INSERT INTO api_keys (id, user_id, project_id, device_id, key_hash, label, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.UserID, key.ProjectID, key.DeviceID,
		key.KeyHash, key.Label, key.ExpiresAt, key.CreatedAt,
	)
	return err
}

// ValidateAPIKey checks if a raw API key is valid for the given project.
//
// Validation steps:
//  1. Hash the raw key and find a matching row in the database.
//  2. Check that the key is not revoked.
//  3. Check that the key is not expired (if an expiry was set).
//  4. Verify that project_id matches.
//
// Returns the matching APIKey on success, or an appropriate error.
func ValidateAPIKey(rawKey, projectID string) (*APIKey, error) {
	keyHash := HashAPIKey(rawKey)

	var k APIKey
	var expiresAt, revokedAt sql.NullString

	err := DB.QueryRow(`
		SELECT id, user_id, project_id, device_id, key_hash, label,
		       expires_at, revoked_at, created_at
		FROM api_keys
		WHERE key_hash = ? AND project_id = ?`,
		keyHash, projectID,
	).Scan(
		&k.ID, &k.UserID, &k.ProjectID, &k.DeviceID,
		&k.KeyHash, &k.Label, &expiresAt, &revokedAt, &k.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: validate api key: %w", err)
	}

	// Check revocation.
	if revokedAt.Valid {
		k.RevokedAt = &revokedAt.String
		return nil, ErrAPIKeyRevoked
	}

	// Check expiration (only if an expiry was set).
	if expiresAt.Valid {
		k.ExpiresAt = &expiresAt.String
		exp, parseErr := time.Parse(time.RFC3339, expiresAt.String)
		if parseErr == nil && time.Now().After(exp) {
			return nil, ErrAPIKeyExpired
		}
	}

	return &k, nil
}

// RevokeAPIKey soft-revokes a key by setting revoked_at to now.
// The key row is never deleted — it remains for audit purposes.
// Only the key owner (user_id) can revoke their own keys.
func RevokeAPIKey(keyID, userID string) error {
	result, err := DB.Exec(`
		UPDATE api_keys SET revoked_at = ?
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), keyID, userID,
	)
	if err != nil {
		return fmt.Errorf("store: revoke api key: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

// ListAPIKeysByProject returns all API keys for a project, ordered by creation date.
// Includes revoked keys (the caller can filter by RevokedAt != nil).
// The key_hash field is intentionally NOT exposed — only metadata is returned.
func ListAPIKeysByProject(userID, projectID string) ([]APIKey, error) {
	rows, err := DB.Query(`
		SELECT id, user_id, project_id, device_id, label,
		       expires_at, revoked_at, created_at
		FROM api_keys
		WHERE user_id = ? AND project_id = ?
		ORDER BY created_at DESC`,
		userID, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var expiresAt, revokedAt sql.NullString
		if err := rows.Scan(
			&k.ID, &k.UserID, &k.ProjectID, &k.DeviceID, &k.Label,
			&expiresAt, &revokedAt, &k.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan api key: %w", err)
		}
		if expiresAt.Valid {
			k.ExpiresAt = &expiresAt.String
		}
		if revokedAt.Valid {
			k.RevokedAt = &revokedAt.String
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// DeleteAPIKey permanently removes a key. Use RevokeAPIKey for soft-revoke.
// Only provided for admin/cleanup scenarios — normal flow uses revocation.
func DeleteAPIKey(keyID, userID string) error {
	result, err := DB.Exec(
		`DELETE FROM api_keys WHERE id = ? AND user_id = ?`,
		keyID, userID,
	)
	if err != nil {
		return fmt.Errorf("store: delete api key: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}
