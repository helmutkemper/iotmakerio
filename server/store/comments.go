// store/comments.go — Project comment CRUD.
//
// Comments are append-only: there is no Update endpoint.
// A user may post multiple comments on the same project over time.
// Deletion is allowed for the comment author (handler checks ownership)
// and for admins (handler checks role).
//
// The optional sub-ratings (doc_rating, code_rating) let reviewers
// separately score documentation quality and code quality, independent
// of the overall 1-5 star rating stored in project_ratings.
// A value of 0 means "not rated" — the DB CHECK constraint allows 0–5.
//
// Pagination uses OFFSET/LIMIT with newest-first ordering. Cursor-based
// pagination is not used here because the comment list is short and
// ordered by creation time (stable), so OFFSET drift is not a concern.
package store

import (
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"
)

// ─── Create ───────────────────────────────────────────────────────────────────

// CreateComment inserts a new comment row.
//
// Validation performed by this function (not duplicated in the handler):
//   - body must be non-empty after trimming
//   - body must not exceed maxChars runes
//   - doc_rating and code_rating must be 0 (not provided) or 1–5
//
// Returns ErrNotFound if the project does not exist or is not public.
func CreateComment(c *Comment, maxChars int) error {
	if utf8.RuneCountInString(c.Body) == 0 {
		return errors.New("comment body is required")
	}
	if utf8.RuneCountInString(c.Body) > maxChars {
		return errors.New("comment body exceeds maximum length")
	}
	if c.DocRating < 0 || c.DocRating > 5 {
		return errors.New("doc_rating must be between 0 and 5")
	}
	if c.CodeRating < 0 || c.CodeRating > 5 {
		return errors.New("code_rating must be between 0 and 5")
	}

	// Verify the project exists and is public before allowing a comment.
	var count int
	if err := DB.QueryRow(
		`SELECT COUNT(*) FROM projects WHERE id = ? AND visibility = 'public'`,
		c.ProjectID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}

	c.CreatedAt = time.Now().UTC()
	_, err := DB.Exec(`
		INSERT INTO project_comments
			(id, project_id, user_id, body, doc_rating, code_rating, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ProjectID, c.UserID, c.Body,
		c.DocRating, c.CodeRating,
		c.CreatedAt.Format(time.RFC3339),
	)
	return err
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// ListComments returns a page of comments for a project, newest first.
// The author's username, display name, and avatar URL are joined in.
// offset=0 fetches the first page; use offset += limit for subsequent pages.
func ListComments(projectID string, offset, limit int) ([]*Comment, error) {
	rows, err := DB.Query(`
		SELECT
			c.id, c.project_id, c.user_id, c.body,
			c.doc_rating, c.code_rating, c.created_at,
			u.username,
			COALESCE(up.display_name, ''),
			COALESCE(up.avatar_url, '')
		FROM project_comments c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN user_profiles up ON up.user_id = c.user_id
		WHERE c.project_id = ?
		ORDER BY c.created_at DESC
		LIMIT ? OFFSET ?`,
		projectID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []*Comment
	for rows.Next() {
		var cm Comment
		var createdAt string
		if err := rows.Scan(
			&cm.ID, &cm.ProjectID, &cm.UserID, &cm.Body,
			&cm.DocRating, &cm.CodeRating, &createdAt,
			&cm.AuthorUsername, &cm.AuthorDisplayName, &cm.AuthorAvatarURL,
		); err != nil {
			return nil, err
		}
		cm.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		comments = append(comments, &cm)
	}
	return comments, rows.Err()
}

// CountComments returns the total number of comments for a project.
// Used by the pagination response so the client knows the total page count.
func CountComments(projectID string) (int, error) {
	var n int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM project_comments WHERE project_id = ?`,
		projectID,
	).Scan(&n)
	return n, err
}

// GetCommentByID returns the comment with the given ID.
// Returns ErrNotFound if it does not exist.
func GetCommentByID(id string) (*Comment, error) {
	var cm Comment
	var createdAt string
	err := DB.QueryRow(`
		SELECT
			c.id, c.project_id, c.user_id, c.body,
			c.doc_rating, c.code_rating, c.created_at,
			u.username,
			COALESCE(up.display_name, ''),
			COALESCE(up.avatar_url, '')
		FROM project_comments c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN user_profiles up ON up.user_id = c.user_id
		WHERE c.id = ?`, id,
	).Scan(
		&cm.ID, &cm.ProjectID, &cm.UserID, &cm.Body,
		&cm.DocRating, &cm.CodeRating, &createdAt,
		&cm.AuthorUsername, &cm.AuthorDisplayName, &cm.AuthorAvatarURL,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	cm.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &cm, nil
}

// ─── Delete ───────────────────────────────────────────────────────────────────

// DeleteComment removes a comment.
// The caller is responsible for checking that the requesting user either
// owns the comment (userID == comment.UserID) or has admin role.
// Returns ErrNotFound if the comment does not exist.
func DeleteComment(commentID string) error {
	res, err := DB.Exec(
		`DELETE FROM project_comments WHERE id = ?`, commentID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Aggregate stats ──────────────────────────────────────────────────────────

// CommentStats holds aggregate quality scores for a project's comments.
// Shown on the feed card and project page alongside the overall star rating.
type CommentStats struct {
	// TotalComments is the number of comments posted.
	TotalComments int `json:"totalComments"`

	// AvgDocRating is the average documentation sub-rating across all
	// comments that provided one (DocRating > 0). 0.0 when none exist.
	AvgDocRating float64 `json:"avgDocRating"`

	// DocRatingCount is the number of comments that included a doc_rating.
	DocRatingCount int `json:"docRatingCount"`

	// AvgCodeRating is the average code quality sub-rating across all
	// comments that provided one (CodeRating > 0). 0.0 when none exist.
	AvgCodeRating float64 `json:"avgCodeRating"`

	// CodeRatingCount is the number of comments that included a code_rating.
	CodeRatingCount int `json:"codeRatingCount"`
}

// GetCommentStats returns aggregate quality scores for a project.
func GetCommentStats(projectID string) (*CommentStats, error) {
	var s CommentStats
	err := DB.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(AVG(CASE WHEN doc_rating  > 0 THEN CAST(doc_rating  AS REAL) END), 0.0),
			COALESCE(SUM(CASE WHEN doc_rating  > 0 THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(CASE WHEN code_rating > 0 THEN CAST(code_rating AS REAL) END), 0.0),
			COALESCE(SUM(CASE WHEN code_rating > 0 THEN 1 ELSE 0 END), 0)
		FROM project_comments
		WHERE project_id = ?`, projectID,
	).Scan(
		&s.TotalComments,
		&s.AvgDocRating, &s.DocRatingCount,
		&s.AvgCodeRating, &s.CodeRatingCount,
	)
	return &s, err
}
