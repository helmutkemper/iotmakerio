// store/follows.go — User follow/unfollow CRUD.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// The user_follows table records (follower_id, following_id) pairs.
// The primary key is the composite (follower_id, following_id) so there
// are no duplicate follows, and queries by either side are indexed.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ─── Follow / Unfollow ────────────────────────────────────────────────────────

// FollowUser records that followerID wants to follow followingID.
// Returns ErrConflict if the follow already exists.
// Returns ErrNotFound if followingID is not a valid user.
// A user cannot follow themselves (returns a validation error).
func FollowUser(followerID, followingID string) error {
	if followerID == followingID {
		return errors.New("a user cannot follow themselves")
	}

	// Verify the target user exists.
	var count int
	if err := DB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE id = ?`, followingID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO user_follows (follower_id, following_id, created_at)
		VALUES (?, ?, ?)`,
		followerID, followingID, now,
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// UnfollowUser removes the follow relationship.
// Returns ErrNotFound if the follow does not exist.
func UnfollowUser(followerID, followingID string) error {
	res, err := DB.Exec(
		`DELETE FROM user_follows WHERE follower_id = ? AND following_id = ?`,
		followerID, followingID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// IsFollowing returns true if followerID follows followingID.
func IsFollowing(followerID, followingID string) (bool, error) {
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM user_follows WHERE follower_id = ? AND following_id = ?`,
		followerID, followingID,
	).Scan(&count)
	return count > 0, err
}

// FollowCounts returns the number of users that userID follows (following)
// and the number of users that follow userID (followers).
func FollowCounts(userID string) (following, followers int, err error) {
	err = DB.QueryRow(
		`SELECT COUNT(*) FROM user_follows WHERE follower_id = ?`, userID,
	).Scan(&following)
	if err != nil {
		return
	}
	err = DB.QueryRow(
		`SELECT COUNT(*) FROM user_follows WHERE following_id = ?`, userID,
	).Scan(&followers)
	return
}

// ListFollowing returns public profile summaries of all users that followerID
// follows, ordered by follow date DESC (most recently followed first).
func ListFollowing(followerID string) ([]*FollowUser_, error) {
	rows, err := DB.Query(`
		SELECT
			u.id, u.username,
			COALESCE(up.display_name, ''),
			COALESCE(up.avatar_url, ''),
			uf.created_at
		FROM user_follows uf
		JOIN users u ON u.id = uf.following_id
		LEFT JOIN user_profiles up ON up.user_id = u.id
		WHERE uf.follower_id = ?
		ORDER BY uf.created_at DESC`, followerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFollowUsers(rows)
}

// ListFollowers returns public profile summaries of all users that follow
// followingID, ordered by follow date DESC.
func ListFollowers(followingID string) ([]*FollowUser_, error) {
	rows, err := DB.Query(`
		SELECT
			u.id, u.username,
			COALESCE(up.display_name, ''),
			COALESCE(up.avatar_url, ''),
			uf.created_at
		FROM user_follows uf
		JOIN users u ON u.id = uf.follower_id
		LEFT JOIN user_profiles up ON up.user_id = u.id
		WHERE uf.following_id = ?
		ORDER BY uf.created_at DESC`, followingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFollowUsers(rows)
}

// ─── DTO ─────────────────────────────────────────────────────────────────────

// FollowUser_ is the lightweight user summary shown in following/followers lists.
// Named with a trailing underscore to avoid collision with the FollowUser function.
type FollowUser_ struct {
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	AvatarURL   string    `json:"avatarUrl"`
	FollowedAt  time.Time `json:"followedAt"`
}

func scanFollowUsers(rows *sql.Rows) ([]*FollowUser_, error) {
	var users []*FollowUser_
	for rows.Next() {
		var u FollowUser_
		var followedAt string
		if err := rows.Scan(
			&u.UserID, &u.Username, &u.DisplayName, &u.AvatarURL, &followedAt,
		); err != nil {
			return nil, err
		}
		u.FollowedAt, _ = time.Parse(time.RFC3339, followedAt)
		users = append(users, &u)
	}
	return users, rows.Err()
}
