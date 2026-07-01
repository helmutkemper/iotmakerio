// store/users.go — User CRUD for the IoTMaker portal.
//
// All functions accept a *sql.DB (or *sql.Tx) so callers can participate
// in transactions. The package-level DB variable is used by default.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound is returned when a query matches no rows.
var ErrNotFound = errors.New("record not found")

// ErrConflict is returned when a unique constraint is violated.
var ErrConflict = errors.New("record already exists")

// ─── Create ──────────────────────────────────────────────────────────────────

// CreateUser inserts a new user into the database.
// The caller is responsible for hashing the password before calling this function.
// Returns ErrConflict if the username or email is already taken.
func CreateUser(u *User) error {
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now

	_, err := DB.Exec(`
		INSERT INTO users
			(id, username, email, password_hash, role, verified, preferred_locale, country_code, created_at, updated_at)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.Email, u.PasswordHash,
		u.Role, boolToInt(u.Verified), u.PreferredLocale, u.CountryCode,
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

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetUserByID returns the user with the given ID.
// Returns ErrNotFound if no such user exists.
func GetUserByID(id string) (*User, error) {
	return scanUser(DB.QueryRow(`
		SELECT id, username, email, password_hash, role, verified, preferred_locale, country_code, created_at, updated_at
		FROM users WHERE id = ?`, id))
}

// GetUserByEmail returns the user with the given email (case-insensitive).
// Returns ErrNotFound if no such user exists.
func GetUserByEmail(email string) (*User, error) {
	return scanUser(DB.QueryRow(`
		SELECT id, username, email, password_hash, role, verified, preferred_locale, country_code, created_at, updated_at
		FROM users WHERE email = ? COLLATE NOCASE`, email))
}

// GetUserByUsername returns the user with the given username (case-insensitive).
// Returns ErrNotFound if no such user exists.
func GetUserByUsername(username string) (*User, error) {
	return scanUser(DB.QueryRow(`
		SELECT id, username, email, password_hash, role, verified, preferred_locale, country_code, created_at, updated_at
		FROM users WHERE username = ? COLLATE NOCASE`, username))
}

// GetUserByLogin resolves a login field that can be either a username or email.
// It tries email first, then username.
func GetUserByLogin(login string) (*User, error) {
	// Try email first
	u, err := GetUserByEmail(login)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Fall back to username
	return GetUserByUsername(login)
}

// ─── Update ───────────────────────────────────────────────────────────────────

// VerifyUser marks the user's email as verified.
func VerifyUser(userID string) error {
	res, err := DB.Exec(`
		UPDATE users SET verified = 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// UpdatePassword replaces the user's password hash.
func UpdatePassword(userID, newHash string) error {
	res, err := DB.Exec(`
		UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		newHash, time.Now().UTC().Format(time.RFC3339), userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// UpdatePreferredLocale updates the user's language preference.
func UpdatePreferredLocale(userID, locale string) error {
	res, err := DB.Exec(`
		UPDATE users SET preferred_locale = ?, updated_at = ? WHERE id = ?`,
		locale, time.Now().UTC().Format(time.RFC3339), userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// UpdateCountryCode updates the user's country code (ISO 3166-1 alpha-2).
// The user self-declares their country in the profile settings. This value
// is used by menu visibility rules to filter items by country.
func UpdateCountryCode(userID, countryCode string) error {
	res, err := DB.Exec(`
		UPDATE users SET country_code = ?, updated_at = ? WHERE id = ?`,
		countryCode, time.Now().UTC().Format(time.RFC3339), userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var verified int
	var createdAt, updatedAt string
	err := row.Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash,
		&u.Role, &verified, &u.PreferredLocale, &u.CountryCode,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Verified = verified == 1
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &u, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// isSQLiteConstraint returns true for UNIQUE or PRIMARY KEY constraint errors.
// modernc/sqlite returns an error whose message contains "UNIQUE constraint".
func isSQLiteConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "UNIQUE constraint") || contains(msg, "PRIMARY KEY constraint")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
