// store/reports.go — Project report CRUD.
//
// A report is a moderation signal filed by a community member against a project.
// One user can file at most one report per project (UNIQUE constraint).
//
// The report workflow:
//  1. User fills in reason + optional details → POST /projects/:id/report
//  2. Report lands in status = "pending".
//  3. A future admin panel lets moderators review reports and update status
//     to "reviewed" or "dismissed".
//
// The store layer never exposes pending reports to non-admins.
// Counting reports for a project is intentionally not a public endpoint —
// the count is only visible to admins to avoid gaming.
package store

import (
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"
)

// ─── Create ───────────────────────────────────────────────────────────────────

// CreateReport inserts a new report row.
//
// Validation:
//   - reason must be one of the ReportReasons vocabulary (enforced by handler)
//   - details must not exceed 500 runes (enforced here for defence in depth)
//
// Returns ErrConflict if the user has already reported this project.
// Returns ErrNotFound if the project does not exist or is not public.
func CreateReport(r *Report) error {
	const maxDetails = 500
	if utf8.RuneCountInString(r.Details) > maxDetails {
		return errors.New("report details exceed 500 characters")
	}

	// Verify the project exists and is public.
	var count int
	if err := DB.QueryRow(
		`SELECT COUNT(*) FROM projects WHERE id = ? AND visibility = 'public'`,
		r.ProjectID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}

	now := time.Now().UTC()
	r.CreatedAt = now
	r.UpdatedAt = now
	r.Status = ReportStatusPending

	_, err := DB.Exec(`
		INSERT INTO project_reports
			(id, project_id, user_id, reason, details, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ProjectID, r.UserID, r.Reason, r.Details, r.Status,
		r.CreatedAt.Format(time.RFC3339),
		r.UpdatedAt.Format(time.RFC3339),
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

// HasReported returns true if userID has already filed a report against projectID.
// Used by the handler to show "you already reported this" state on the client.
func HasReported(userID, projectID string) (bool, error) {
	var n int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM project_reports WHERE user_id = ? AND project_id = ?`,
		userID, projectID,
	).Scan(&n)
	return n > 0, err
}

// GetReport returns the report filed by userID against projectID.
// Returns ErrNotFound if no such report exists.
func GetReport(userID, projectID string) (*Report, error) {
	var rep Report
	var createdAt, updatedAt string
	err := DB.QueryRow(`
		SELECT id, project_id, user_id, reason, details, status, created_at, updated_at
		FROM project_reports
		WHERE user_id = ? AND project_id = ?`,
		userID, projectID,
	).Scan(
		&rep.ID, &rep.ProjectID, &rep.UserID,
		&rep.Reason, &rep.Details, &rep.Status,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rep.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	rep.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &rep, nil
}

// ListPendingReports returns all reports with status = "pending", ordered by
// creation time ASC (oldest first, so moderators clear the backlog in order).
// Admin-only endpoint — the handler must verify the requesting user's role.
func ListPendingReports(limit, offset int) ([]*Report, error) {
	rows, err := DB.Query(`
		SELECT id, project_id, user_id, reason, details, status, created_at, updated_at
		FROM project_reports
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReports(rows)
}

// ─── Update (admin moderation) ────────────────────────────────────────────────

// UpdateReportStatus sets the moderation status of a report.
// newStatus must be one of: "reviewed", "dismissed".
// Admin-only — the handler must verify the requesting user's role.
func UpdateReportStatus(reportID, newStatus string) error {
	if newStatus != ReportStatusReviewed && newStatus != ReportStatusDismissed {
		return errors.New("status must be 'reviewed' or 'dismissed'")
	}
	res, err := DB.Exec(`
		UPDATE project_reports
		SET status = ?, updated_at = datetime('now')
		WHERE id = ?`,
		newStatus, reportID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func scanReports(rows *sql.Rows) ([]*Report, error) {
	var reports []*Report
	for rows.Next() {
		var rep Report
		var createdAt, updatedAt string
		if err := rows.Scan(
			&rep.ID, &rep.ProjectID, &rep.UserID,
			&rep.Reason, &rep.Details, &rep.Status,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		rep.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		rep.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		reports = append(reports, &rep)
	}
	return reports, rows.Err()
}
