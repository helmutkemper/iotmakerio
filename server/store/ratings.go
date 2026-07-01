// store/ratings.go — Project rating CRUD.
//
// Each user can rate each project once with a score of 1–5.
// Updating the rating replaces the previous value (UPSERT).
// Deleting sets the rating back to 0 (i.e. removes the row).
//
// The aggregated avg_rating and rating_count are computed at query time
// in feed.go — they are never stored as columns to avoid stale data.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ─── Upsert ───────────────────────────────────────────────────────────────────

// UpsertRating inserts or replaces the user's rating for a project.
// Returns ErrNotFound if the project does not exist.
// Returns a validation error if rating is not in [1, 5].
func UpsertRating(userID, projectID string, rating int) error {
	if rating < 1 || rating > 5 {
		return errors.New("rating must be between 1 and 5")
	}

	// Verify the project exists and is public before allowing a rating.
	var count int
	if err := DB.QueryRow(
		`SELECT COUNT(*) FROM projects WHERE id = ? AND visibility = 'public'`, projectID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO project_ratings (user_id, project_id, rating, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, project_id) DO UPDATE SET
			rating     = excluded.rating,
			updated_at = excluded.updated_at`,
		userID, projectID, rating, now, now,
	)
	return err
}

// ─── Delete ───────────────────────────────────────────────────────────────────

// DeleteRating removes the user's rating for a project.
// Returns ErrNotFound if the user has not rated this project.
func DeleteRating(userID, projectID string) error {
	res, err := DB.Exec(
		`DELETE FROM project_ratings WHERE user_id = ? AND project_id = ?`,
		userID, projectID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetUserRating returns the authenticated user's rating for a project.
// Returns 0 and no error when the user has not rated the project.
func GetUserRating(userID, projectID string) (int, error) {
	var rating int
	err := DB.QueryRow(
		`SELECT rating FROM project_ratings WHERE user_id = ? AND project_id = ?`,
		userID, projectID,
	).Scan(&rating)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rating, err
}

// GetProjectRatingStats returns the aggregate rating statistics for a project.
// Both values are zero when the project has no ratings.
func GetProjectRatingStats(projectID string) (avgRating float64, count int, err error) {
	err = DB.QueryRow(`
		SELECT COALESCE(AVG(CAST(rating AS REAL)), 0.0), COALESCE(COUNT(*), 0)
		FROM project_ratings
		WHERE project_id = ?`, projectID,
	).Scan(&avgRating, &count)
	return
}
