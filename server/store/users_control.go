// server/store/users_control.go — Store functions used by the control panel.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Kept separate from users.go (portal-facing functions) so the control panel
// functions are easy to audit independently.
package store

import "time"

// UserSummary is the view of a user returned by the control panel.
// Omits sensitive fields like password_hash.
type UserSummary struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Verified  bool      `json:"verified"`
	CreatedAt time.Time `json:"created_at"`
}

// UsersPage is the paginated result of ListUsers.
type UsersPage struct {
	Users      []UserSummary `json:"users"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	PageSize   int           `json:"pageSize"`
	TotalPages int           `json:"totalPages"`
}

// ListUsers returns a page of users filtered by an optional search query.
// Search matches against username and email (case-insensitive substring).
// Page is 1-indexed. PageSize must be > 0.
//
// created_at is stored as TEXT (RFC3339) in SQLite — scanned into string
// then parsed, matching the pattern used throughout the rest of the store.
func ListUsers(page, pageSize int, search string) (*UsersPage, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	// Build optional WHERE clause for search.
	whereSQL := ""
	countArgs := []any{}
	if search != "" {
		whereSQL = "WHERE username LIKE ? OR email LIKE ?"
		like := "%" + search + "%"
		countArgs = append(countArgs, like, like)
	}

	// COUNT for total pages calculation.
	var total int
	if err := DB.QueryRow(
		"SELECT COUNT(*) FROM users "+whereSQL,
		countArgs...,
	).Scan(&total); err != nil {
		return nil, err
	}

	// Fetch the requested page.
	pageArgs := append(countArgs, pageSize, offset)
	rows, err := DB.Query(`
		SELECT id, username, email, role, verified, created_at
		FROM   users
		`+whereSQL+`
		ORDER  BY created_at DESC
		LIMIT  ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserSummary
	for rows.Next() {
		var u UserSummary
		var verified int
		var createdAt string
		if err := rows.Scan(
			&u.ID, &u.Username, &u.Email,
			&u.Role, &verified, &createdAt,
		); err != nil {
			return nil, err
		}
		u.Verified = verified == 1
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := total / pageSize
	if total%pageSize != 0 {
		totalPages++
	}
	if totalPages < 1 {
		totalPages = 1
	}

	return &UsersPage{
		Users:      users,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// SetUserRole updates a single user's role.
// Returns ErrNotFound if the user does not exist.
func SetUserRole(userID, role string) error {
	res, err := DB.Exec(
		`UPDATE users SET role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		role, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
