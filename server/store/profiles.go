// store/profiles.go — UserProfile CRUD and locale listing for the registration form.
//
// User profiles hold public-facing data (display name, bio, avatar, links)
// that is separate from the authentication record in users. The profile row
// is created atomically during registration (see registration.go).
//
// Existing users who pre-date the profile feature will not have a profile row.
// EnsureProfile() creates an empty profile on demand; call it in any handler
// that reads a profile to avoid nil-check boilerplate.
//
// Note on ListLocales: store/i18n.go already exports ListLocales() which
// returns []string (locale codes only). ListUILocales() is the registration-form
// variant that returns []*Locale (code + human-readable display name).
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ─── Create ───────────────────────────────────────────────────────────────────

// CreateProfile inserts a new profile row.
// Called externally only in tests. During registration, use createProfileInTx.
func CreateProfile(p *UserProfile) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO user_profiles (user_id, display_name, bio, avatar_url, github_url, website_url, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.DisplayName, p.Bio,
		p.AvatarURL, p.GithubURL, p.WebsiteURL, now,
	)
	return err
}

// createProfileInTx inserts a new profile row inside an active transaction.
// Called exclusively by RegisterUser in registration.go.
func createProfileInTx(tx *sql.Tx, p *UserProfile) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.Exec(`
		INSERT INTO user_profiles (user_id, display_name, bio, avatar_url, github_url, website_url, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.DisplayName, p.Bio,
		p.AvatarURL, p.GithubURL, p.WebsiteURL, now,
	)
	return err
}

// EnsureProfile creates an empty profile row for userID if one does not yet
// exist. Idempotent — safe to call on every request that reads a profile.
// This handles users who registered before the profile feature was added.
func EnsureProfile(userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT OR IGNORE INTO user_profiles
			(user_id, display_name, bio, avatar_url, github_url, website_url, updated_at)
		VALUES (?, '', '', '', '', '', ?)`,
		userID, now,
	)
	return err
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetProfileByUserID returns the profile for the given user ID.
// Returns ErrNotFound if no profile row exists (call EnsureProfile first
// to guarantee a row is present).
func GetProfileByUserID(userID string) (*UserProfile, error) {
	var p UserProfile
	var updatedAt string
	err := DB.QueryRow(`
		SELECT user_id, display_name, bio, avatar_url, github_url,
		       COALESCE(github_username, ''), website_url, updated_at
		FROM user_profiles
		WHERE user_id = ?`, userID,
	).Scan(
		&p.UserID, &p.DisplayName, &p.Bio,
		&p.AvatarURL, &p.GithubURL, &p.GithubUsername, &p.WebsiteURL, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &p, nil
}

// GetPublicProfileByUsername returns the public profile for the given username.
// It LEFT JOIN-queries users and user_profiles so that users without a profile
// row still get an empty profile response.
// Returns ErrNotFound if the username does not exist.
func GetPublicProfileByUsername(username string) (*PublicProfile, error) {
	var p PublicProfile
	var displayName, bio, avatarURL, githubURL, websiteURL, createdAt string

	err := DB.QueryRow(`
		SELECT
			u.username,
			u.created_at,
			COALESCE(pr.display_name, ''),
			COALESCE(pr.bio, ''),
			COALESCE(pr.avatar_url, ''),
			COALESCE(pr.github_url, ''),
			COALESCE(pr.website_url, '')
		FROM users u
		LEFT JOIN user_profiles pr ON pr.user_id = u.id
		WHERE u.username = ? COLLATE NOCASE`, username,
	).Scan(
		&p.Username, &createdAt,
		&displayName, &bio, &avatarURL, &githubURL, &websiteURL,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.DisplayName = displayName
	p.Bio = bio
	p.AvatarURL = avatarURL
	p.GithubURL = githubURL
	p.WebsiteURL = websiteURL
	p.MemberSince, _ = time.Parse(time.RFC3339, createdAt)
	return &p, nil
}

// ─── Update ───────────────────────────────────────────────────────────────────

// UpdateProfile replaces the editable profile fields for userID.
// EnsureProfile is called first so that the UPDATE always affects a row
// (avoids a silent no-op for users who pre-date the profile feature).
func UpdateProfile(userID string, u *ProfileUpdate) error {
	if err := EnsureProfile(userID); err != nil {
		return err
	}
	_, err := DB.Exec(`
		UPDATE user_profiles
		SET display_name = ?,
		    bio          = ?,
		    github_url   = ?,
		    website_url  = ?,
		    updated_at   = datetime('now')
		WHERE user_id = ?`,
		u.DisplayName, u.Bio, u.GithubURL, u.WebsiteURL, userID,
	)
	return err
}

// UpdateAvatarURL replaces the avatar_url for userID.
// Called after the avatar file has been written to disk successfully.
func UpdateAvatarURL(userID, avatarURL string) error {
	if err := EnsureProfile(userID); err != nil {
		return err
	}
	_, err := DB.Exec(`
		UPDATE user_profiles
		SET avatar_url = ?, updated_at = datetime('now')
		WHERE user_id = ?`,
		avatarURL, userID,
	)
	return err
}

// SetGithubUsername saves the verified GitHub login for a user.
// Called by the GitHub OAuth callback handler after successfully fetching
// the user's GitHub login. The login is the username returned by the
// GitHub API — it is the canonical identifier, not the display name.
// This is the only function that writes github_username; it is never
// set through the regular profile update flow.
func SetGithubUsername(userID, githubUsername string) error {
	if err := EnsureProfile(userID); err != nil {
		return err
	}
	_, err := DB.Exec(`
		UPDATE user_profiles
		SET    github_username = ?,
		       updated_at      = datetime('now')
		WHERE  user_id = ?`,
		githubUsername, userID,
	)
	return err
}

// GetGithubUsername returns the verified GitHub login for userID.
// Returns an empty string if the user has not connected their GitHub account.
func GetGithubUsername(userID string) (string, error) {
	var username string
	err := DB.QueryRow(`
		SELECT COALESCE(github_username, '')
		FROM   user_profiles
		WHERE  user_id = ?`, userID,
	).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return username, err
}

// ─── Locale listing ───────────────────────────────────────────────────────────

// ListUILocales returns all supported UI locales for the registration form
// language selector. Unlike store/i18n.go's ListLocales() which returns
// []string (codes only), this function returns []*Locale (code + display name)
// so the form can render human-readable options (e.g. "Português (BR)").
//
// The display names are stored in i18n_bundles.display and seeded during
// migration (see db.go → seedLocales).
func ListUILocales() ([]*Locale, error) {
	rows, err := DB.Query(`
		SELECT locale, display
		FROM i18n_bundles
		ORDER BY locale ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locales []*Locale
	for rows.Next() {
		var l Locale
		if err := rows.Scan(&l.Code, &l.Display); err != nil {
			return nil, err
		}
		locales = append(locales, &l)
	}
	return locales, rows.Err()
}
