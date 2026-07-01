// store/feed.go — Feed and marketplace queries.
//
// The feed has four modes that all share the same rule:
//
//	Only projects with ready_to_use = 1 appear in any feed tab.
//	This flag is set by the project owner in the Properties modal and
//	represents a quality commitment ("documented and ready for others").
//
//	Discover  — weighted shuffle: 40% recency + 40% popularity + 20% random.
//	            No cursor — caller re-requests for a fresh shuffle.
//	            SQLite note: julianday() computes fractional days since the
//	            Julian epoch. PostgreSQL equivalent:
//	            EXTRACT(EPOCH FROM age())/86400.
//
//	Recent    — public projects with a filled card, newest updated_at first.
//	            Cursor-based pagination on updated_at for stable results.
//
//	Popular   — same set ordered by average rating DESC, then rating count DESC.
//	            OFFSET pagination (stable ranking, changes slowly).
//
//	Following — activity (feed_events) from users the viewer follows.
//	            Cursor-based on event created_at DESC.
//
// All modes share FeedFilter for optional narrowing (category, language, search).
// FeedCard is the denormalized DTO — one per project, no further requests needed.
//
// ── Scan column count ────────────────────────────────────────────────────────
//
// feedSelect        → 18 columns  → use runFeedQuery
// feedSelectWithScore → 19 columns (adds feed_score) → use runFeedQueryWithScore
//
// IMPORTANT: always match the SELECT builder to the correct run function.
// Mixing them causes "sql: expected N destination arguments in Scan, not M".
package store

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// ─── DTO ─────────────────────────────────────────────────────────────────────

// FeedCard is the read-only projection of a project returned by all feed
// endpoints. Built by JOIN queries — never persisted.
type FeedCard struct {
	ProjectID       string `json:"projectId"`
	Name            string `json:"name"`
	CardTitle       string `json:"cardTitle"`
	CardImage       string `json:"cardImage"`
	CardDescription string `json:"cardDescription"`
	CardKeywords    string `json:"cardKeywords"`
	CategoryName    string `json:"categoryName"`
	SubcategoryName string `json:"subcategoryName"`
	LangDisplay     string `json:"langDisplay"`
	LangID          string `json:"langId"`

	AuthorUsername    string `json:"authorUsername"`
	AuthorDisplayName string `json:"authorDisplayName"`
	AuthorAvatarURL   string `json:"authorAvatarUrl"`

	// Rating stats aggregated from project_ratings.
	AvgRating   float64 `json:"avgRating"`
	RatingCount int     `json:"ratingCount"`

	// OwnRating is the authenticated viewer's rating (0 = not rated).
	OwnRating int `json:"ownRating"`

	// IsFollowing is true when the viewer follows the project author.
	IsFollowing bool `json:"isFollowing"`

	UpdatedAt time.Time `json:"updatedAt"`

	// EventType and EventAt are populated only in the Following feed.
	EventType string    `json:"eventType,omitempty"`
	EventAt   time.Time `json:"eventAt,omitempty"`
}

// ─── Filter ───────────────────────────────────────────────────────────────────

// FeedFilter holds optional narrowing criteria shared by all feed modes.
// Empty string fields are ignored.
type FeedFilter struct {
	CategoryID    string
	SubcategoryID string
	LangID        string
	Search        string // searched in card_title, card_description, card_keywords
	ViewerUserID  string // populates OwnRating and IsFollowing
}

// ─── Discover (weighted shuffle) ─────────────────────────────────────────────

// ListFeedDiscover returns public, ready-to-use projects ordered by a weighted
// feed score:
//
//	score = 0.4 * recency + 0.4 * popularity + 0.2 * random
//
// recency    = 1 / (1 + days_old / 30)   — half-life of 30 days
// popularity = avg_rating / 5.0
// random     = ABS(RANDOM() % 10000) / 10000.0
//
// PostgreSQL migration note:
//
//	Replace julianday() with:
//	1.0 / (1.0 + EXTRACT(EPOCH FROM (NOW() - p.updated_at::timestamptz)) / 86400.0 / 30.0)
//
// No cursor — each call produces a different shuffle. OFFSET is used because
// the random order makes cursor-based pagination meaningless.
//
// Uses feedSelectWithScore (19 columns) → must call runFeedQueryWithScore.
func ListFeedDiscover(f FeedFilter, offset, limit int) ([]*FeedCard, error) {
	filterSQL, filterArgs := feedFilterClause(f)

	q := feedSelectWithScore(f.ViewerUserID) +
		"\nWHERE p.visibility = 'public' AND p.card_title != '' AND p.ready_to_use = 1" +
		filterSQL +
		"\nORDER BY feed_score DESC\nLIMIT ? OFFSET ?"

	args := []any{f.ViewerUserID, f.ViewerUserID}
	args = append(args, filterArgs...)
	args = append(args, limit, offset)

	// feedSelectWithScore adds feed_score as the 19th column.
	// runFeedQueryWithScore scans that extra column into a throwaway float64.
	return runFeedQueryWithScore(q, args)
}

// ─── Recent ───────────────────────────────────────────────────────────────────

// ListFeedRecent returns public, ready-to-use projects with a filled card
// ordered by updated_at DESC. Cursor is the RFC3339 updated_at of the last
// card from the previous page; empty string fetches the first page.
//
// Uses feedSelect (18 columns) → must call runFeedQuery.
func ListFeedRecent(f FeedFilter, cursor string, limit int) ([]*FeedCard, error) {
	filterSQL, filterArgs := feedFilterClause(f)

	cursorSQL := ""
	var cursorArgs []any
	if cursor != "" {
		cursorSQL = " AND p.updated_at < ?"
		cursorArgs = []any{cursor}
	}

	q := feedSelect(f.ViewerUserID) +
		"\nWHERE p.visibility = 'public' AND p.card_title != '' AND p.ready_to_use = 1" +
		filterSQL + cursorSQL +
		"\nORDER BY p.updated_at DESC\nLIMIT ?"

	args := []any{f.ViewerUserID, f.ViewerUserID}
	args = append(args, filterArgs...)
	args = append(args, cursorArgs...)
	args = append(args, limit)

	return runFeedQuery(q, args)
}

// ─── Popular ──────────────────────────────────────────────────────────────────

// ListFeedPopular returns public, ready-to-use projects ordered by avg_rating
// DESC, then rating_count DESC. Uses OFFSET pagination (ranking is stable).
//
// Uses feedSelect (18 columns) → must call runFeedQuery.
func ListFeedPopular(f FeedFilter, offset, limit int) ([]*FeedCard, error) {
	filterSQL, filterArgs := feedFilterClause(f)

	q := feedSelect(f.ViewerUserID) +
		"\nWHERE p.visibility = 'public' AND p.card_title != '' AND p.ready_to_use = 1" +
		filterSQL +
		"\nORDER BY avg_rating DESC, rating_count DESC, p.updated_at DESC\nLIMIT ? OFFSET ?"

	args := []any{f.ViewerUserID, f.ViewerUserID}
	args = append(args, filterArgs...)
	args = append(args, limit, offset)

	return runFeedQuery(q, args)
}

// ─── Following ────────────────────────────────────────────────────────────────

// ListFeedFollowing returns feed events from users that viewerUserID follows,
// but only for projects with ready_to_use = 1. One card per project (the most
// recent event wins via GROUP BY + MAX). Cursor-based on event created_at DESC.
func ListFeedFollowing(viewerUserID, cursor string, limit int) ([]*FeedCard, error) {
	cursorSQL := ""
	var cursorArgs []any
	if cursor != "" {
		cursorSQL = "AND fe.created_at < ? "
		cursorArgs = []any{cursor}
	}

	// SQLite GROUP BY note: when selecting non-aggregated columns alongside
	// MAX(fe.created_at), SQLite returns the value from the row that contains
	// the MAX. This is documented SQLite behaviour and used intentionally.
	q := `
SELECT
    p.id, p.name, p.card_title, p.card_image, p.card_description, p.card_keywords,
    COALESCE(pc.name, ''), COALESCE(ps.name, ''),
    pl.display, pl.id,
    u.username, COALESCE(up.display_name, ''), COALESCE(up.avatar_url, ''),
    COALESCE(r.avg_rating, 0.0), COALESCE(r.rating_count, 0),
    COALESCE(my_r.rating, 0),
    1 AS is_following,
    p.updated_at,
    MAX(fe.created_at) AS event_at,
    fe.event_type
FROM feed_events fe
JOIN user_follows uf    ON uf.following_id = fe.user_id AND uf.follower_id = ?
JOIN projects p         ON p.id = fe.project_id
JOIN users u            ON u.id = p.user_id
JOIN programming_languages pl ON pl.id = p.programming_language_id
LEFT JOIN user_profiles up  ON up.user_id = p.user_id
LEFT JOIN project_categories pc   ON pc.id = p.category_id
LEFT JOIN project_subcategories ps ON ps.id = p.subcategory_id
LEFT JOIN (
    SELECT project_id,
           AVG(CAST(rating AS REAL)) AS avg_rating,
           COUNT(*) AS rating_count
    FROM project_ratings GROUP BY project_id
) r ON r.project_id = p.id
LEFT JOIN project_ratings my_r ON my_r.project_id = p.id AND my_r.user_id = ?
WHERE p.visibility = 'public' AND p.card_title != '' AND p.ready_to_use = 1
` + cursorSQL + `
GROUP BY p.id
ORDER BY event_at DESC
LIMIT ?`

	args := []any{viewerUserID, viewerUserID}
	args = append(args, cursorArgs...)
	args = append(args, limit)

	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cards []*FeedCard
	for rows.Next() {
		var c FeedCard
		var updatedAt, eventAt string
		var isFollowingInt int
		if err := rows.Scan(
			&c.ProjectID, &c.Name,
			&c.CardTitle, &c.CardImage, &c.CardDescription, &c.CardKeywords,
			&c.CategoryName, &c.SubcategoryName,
			&c.LangDisplay, &c.LangID,
			&c.AuthorUsername, &c.AuthorDisplayName, &c.AuthorAvatarURL,
			&c.AvgRating, &c.RatingCount,
			&c.OwnRating, &isFollowingInt,
			&updatedAt, &eventAt, &c.EventType,
		); err != nil {
			return nil, err
		}
		c.IsFollowing = isFollowingInt == 1
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		c.EventAt, _ = time.Parse(time.RFC3339, eventAt)
		cards = append(cards, &c)
	}
	return cards, rows.Err()
}

// ─── Feed events ──────────────────────────────────────────────────────────────

// Feed event type constants. Used by LogFeedEvent and the Following feed.
const (
	FeedEventCreated       = "project_created"
	FeedEventCodeUpdated   = "code_updated"
	FeedEventReadmeUpdated = "readme_updated"
)

// LogFeedEvent records a feed activity event for a project.
// Called by project handlers after successful mutations. Best-effort: callers
// log errors and continue; the feed is never the bottleneck.
//
// Deduplication: events of the same type for the same project within 1 hour
// are silently dropped. This prevents spam when a user saves many code
// versions in quick succession.
func LogFeedEvent(projectID, userID, eventType string) error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	id := fmt.Sprintf("%x", b)

	_, err := DB.Exec(`
		INSERT INTO feed_events (id, project_id, user_id, event_type, created_at)
		SELECT ?, ?, ?, ?, datetime('now')
		WHERE NOT EXISTS (
			SELECT 1 FROM feed_events
			WHERE project_id = ?
			  AND event_type = ?
			  AND created_at > datetime('now', '-1 hour')
		)`,
		id, projectID, userID, eventType,
		projectID, eventType,
	)
	return err
}

// ─── SQL builders (private) ───────────────────────────────────────────────────

// feedSelect returns the full SELECT...FROM...JOIN block for the standard feed
// modes (Discover, Recent, Popular).
//
// Column count: 18 — use with runFeedQuery.
//
// The two ? placeholders are both viewerUserID (own-rating and follow).
func feedSelect(viewerUserID string) string {
	return `SELECT
    p.id, p.name, p.card_title, p.card_image, p.card_description, p.card_keywords,
    COALESCE(pc.name, ''), COALESCE(ps.name, ''),
    pl.display, pl.id,
    u.username, COALESCE(up.display_name, ''), COALESCE(up.avatar_url, ''),
    COALESCE(r.avg_rating, 0.0), COALESCE(r.rating_count, 0),
    COALESCE(my_r.rating, 0),
    COALESCE(fol.is_following, 0),
    p.updated_at
FROM projects p
JOIN users u ON u.id = p.user_id
JOIN programming_languages pl ON pl.id = p.programming_language_id
LEFT JOIN user_profiles up ON up.user_id = p.user_id
LEFT JOIN project_categories pc ON pc.id = p.category_id
LEFT JOIN project_subcategories ps ON ps.id = p.subcategory_id
LEFT JOIN (
    SELECT project_id,
           AVG(CAST(rating AS REAL)) AS avg_rating,
           COUNT(*) AS rating_count
    FROM project_ratings GROUP BY project_id
) r ON r.project_id = p.id
LEFT JOIN project_ratings my_r ON my_r.project_id = p.id AND my_r.user_id = ?
LEFT JOIN (
    SELECT following_id, 1 AS is_following
    FROM user_follows WHERE follower_id = ?
) fol ON fol.following_id = p.user_id`
}

// feedSelectWithScore appends the feed_score computed column used by Discover.
//
// Column count: 19 (feed_score is the 19th) — use with runFeedQueryWithScore.
//
// The two ? placeholders are both viewerUserID (own-rating and follow).
func feedSelectWithScore(viewerUserID string) string {
	return `SELECT
    p.id, p.name, p.card_title, p.card_image, p.card_description, p.card_keywords,
    COALESCE(pc.name, ''), COALESCE(ps.name, ''),
    pl.display, pl.id,
    u.username, COALESCE(up.display_name, ''), COALESCE(up.avatar_url, ''),
    COALESCE(r.avg_rating, 0.0), COALESCE(r.rating_count, 0),
    COALESCE(my_r.rating, 0),
    COALESCE(fol.is_following, 0),
    p.updated_at,
    (
        0.4 * (1.0 / (1.0 + (julianday('now') - julianday(p.updated_at)) / 30.0))
      + 0.4 * (COALESCE(r.avg_rating, 0.0) / 5.0)
      + 0.2 * (ABS(RANDOM() % 10000)) / 10000.0
    ) AS feed_score
FROM projects p
JOIN users u ON u.id = p.user_id
JOIN programming_languages pl ON pl.id = p.programming_language_id
LEFT JOIN user_profiles up ON up.user_id = p.user_id
LEFT JOIN project_categories pc ON pc.id = p.category_id
LEFT JOIN project_subcategories ps ON ps.id = p.subcategory_id
LEFT JOIN (
    SELECT project_id,
           AVG(CAST(rating AS REAL)) AS avg_rating,
           COUNT(*) AS rating_count
    FROM project_ratings GROUP BY project_id
) r ON r.project_id = p.id
LEFT JOIN project_ratings my_r ON my_r.project_id = p.id AND my_r.user_id = ?
LEFT JOIN (
    SELECT following_id, 1 AS is_following
    FROM user_follows WHERE follower_id = ?
) fol ON fol.following_id = p.user_id`
}

// feedFilterClause builds optional WHERE conditions from a FeedFilter.
// Returns a SQL fragment starting with " AND " and its bound args.
func feedFilterClause(f FeedFilter) (string, []any) {
	var parts []string
	var args []any

	if f.CategoryID != "" {
		parts = append(parts, "p.category_id = ?")
		args = append(args, f.CategoryID)
	}
	if f.SubcategoryID != "" {
		parts = append(parts, "p.subcategory_id = ?")
		args = append(args, f.SubcategoryID)
	}
	if f.LangID != "" {
		parts = append(parts, "p.programming_language_id = ?")
		args = append(args, f.LangID)
	}
	if f.Search != "" {
		// Escape % and _ so they are treated as literal characters.
		// PostgreSQL migration: replace with to_tsvector / plainto_tsquery.
		like := "%" + strings.ReplaceAll(
			strings.ReplaceAll(f.Search, "%", "\\%"),
			"_", "\\_",
		) + "%"
		parts = append(parts,
			"(p.card_title LIKE ? OR p.card_description LIKE ? OR p.card_keywords LIKE ?)")
		args = append(args, like, like, like)
	}

	if len(parts) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(parts, " AND "), args
}

// runFeedQuery executes a standard feed SELECT and scans into []*FeedCard.
//
// Expected column count: 18 (feedSelect output).
// Do NOT use this with feedSelectWithScore — it will cause a Scan error.
func runFeedQuery(q string, args []any) ([]*FeedCard, error) {
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cards []*FeedCard
	for rows.Next() {
		var c FeedCard
		var updatedAt string
		var isFollowingInt int
		if err := rows.Scan(
			&c.ProjectID, &c.Name,
			&c.CardTitle, &c.CardImage, &c.CardDescription, &c.CardKeywords,
			&c.CategoryName, &c.SubcategoryName,
			&c.LangDisplay, &c.LangID,
			&c.AuthorUsername, &c.AuthorDisplayName, &c.AuthorAvatarURL,
			&c.AvgRating, &c.RatingCount,
			&c.OwnRating, &isFollowingInt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		c.IsFollowing = isFollowingInt == 1
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		cards = append(cards, &c)
	}
	return cards, rows.Err()
}

// runFeedQueryWithScore is like runFeedQuery but scans the extra feed_score
// column appended by feedSelectWithScore (the value is discarded client-side).
//
// Expected column count: 19 (feedSelectWithScore output).
// Do NOT use this with feedSelect — it will fail with "not enough columns".
func runFeedQueryWithScore(q string, args []any) ([]*FeedCard, error) {
	rows, err := DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cards []*FeedCard
	for rows.Next() {
		var c FeedCard
		var updatedAt string
		var isFollowingInt int
		var score float64 // computed by SQLite, discarded here
		if err := rows.Scan(
			&c.ProjectID, &c.Name,
			&c.CardTitle, &c.CardImage, &c.CardDescription, &c.CardKeywords,
			&c.CategoryName, &c.SubcategoryName,
			&c.LangDisplay, &c.LangID,
			&c.AuthorUsername, &c.AuthorDisplayName, &c.AuthorAvatarURL,
			&c.AvgRating, &c.RatingCount,
			&c.OwnRating, &isFollowingInt,
			&updatedAt, &score,
		); err != nil {
			return nil, err
		}
		c.IsFollowing = isFollowingInt == 1
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		cards = append(cards, &c)
	}
	return cards, rows.Err()
}
