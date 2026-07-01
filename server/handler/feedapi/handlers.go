// handler/feedapi/handlers.go — Feed and marketplace handler implementations.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// All feed endpoints are public (no auth required) with one exception:
// rating and follow operations require a Bearer token.
//
// Feed tabs:
//
//	GET /api/v1/feed?tab=recent&cursor=...&category=...&lang=...&q=...
//	GET /api/v1/feed?tab=popular&page=N&...
//	GET /api/v1/feed?tab=discover&page=N&...
//	GET /api/v1/feed?tab=following&cursor=... (requires auth)
//
// Rating:
//
//	PUT    /api/v1/projects/:id/rating  { rating: 1-5 }
//	DELETE /api/v1/projects/:id/rating
//	GET    /api/v1/projects/:id/rating  (returns own rating, requires auth)
//
// Follow (also exposed from profileapi but mirrored here for feed cards):
//
//	POST   /api/v1/users/:username/follow
//	DELETE /api/v1/users/:username/follow
//	GET    /api/v1/users/:username/follow  (is-following check)
package feedapi

import (
	"net/http"
	"strconv"
	"strings"

	cryptoauth "server/auth"
	"server/config"
	"server/handler/spaauth"
	"server/store"

	"github.com/labstack/echo/v4"
)

// ─── Feed ─────────────────────────────────────────────────────────────────────

// handleGetFeed dispatches to the correct feed query based on the `tab` query
// parameter. Defaults to "discover" when tab is missing or unrecognised.
//
// Common query parameters (all optional):
//
//	tab      — "recent" | "popular" | "discover" | "following"
//	category — project_categories.id
//	sub      — project_subcategories.id
//	lang     — programming_languages.id
//	q        — free-text search (title, description, keywords)
//	cursor   — opaque pagination cursor (updated_at for recent/following)
//	page     — page number for popular/discover (1-based, default 1)
func handleGetFeed(c echo.Context) error {
	tab := strings.ToLower(c.QueryParam("tab"))
	if tab == "" {
		tab = "discover"
	}

	// Resolve the viewer ID from the optional Bearer token.
	// Anonymous requests are valid — own_rating and is_following will be zero.
	viewerID := ""
	if hdr := c.Request().Header.Get("Authorization"); strings.HasPrefix(hdr, "Bearer ") {
		token := strings.TrimPrefix(hdr, "Bearer ")
		cfg := config.Get()
		if claims, err := cryptoauth.ParseJWT(token, cfg.JWTSecret); err == nil {
			viewerID = claims.UserID
		}
	}

	// Following tab requires authentication.
	if tab == "following" && viewerID == "" {
		return fail(c, 401, "authentication required for the following feed")
	}

	pageSize := store.GetSettingInt(store.SettingFeedPageSize, 24)

	f := store.FeedFilter{
		CategoryID:    c.QueryParam("category"),
		SubcategoryID: c.QueryParam("sub"),
		LangID:        c.QueryParam("lang"),
		Search:        strings.TrimSpace(c.QueryParam("q")),
		ViewerUserID:  viewerID,
	}

	cursor := c.QueryParam("cursor")

	page := 1
	if p := c.QueryParam("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	offset := (page - 1) * pageSize

	var cards []*store.FeedCard
	var err error

	switch tab {
	case "recent":
		cards, err = store.ListFeedRecent(f, cursor, pageSize)
	case "popular":
		cards, err = store.ListFeedPopular(f, offset, pageSize)
	case "discover":
		cards, err = store.ListFeedDiscover(f, offset, pageSize)
	case "following":
		cards, err = store.ListFeedFollowing(viewerID, cursor, pageSize)
	default:
		cards, err = store.ListFeedDiscover(f, offset, pageSize)
	}

	if err != nil {
		c.Logger().Errorf("[feedapi] ListFeed tab=%s: %v", tab, err)
		return fail(c, 500, "internal error")
	}
	if cards == nil {
		cards = []*store.FeedCard{}
	}

	// Build the next-page cursor from the last card.
	nextCursor := ""
	if len(cards) == pageSize {
		last := cards[len(cards)-1]
		switch tab {
		case "recent":
			nextCursor = last.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
		case "following":
			nextCursor = last.EventAt.UTC().Format("2006-01-02T15:04:05Z")
		}
	}

	return ok(c, map[string]any{
		"tab":        tab,
		"cards":      cards,
		"nextCursor": nextCursor,
		"page":       page,
		"pageSize":   pageSize,
	})
}

// ─── Rating ───────────────────────────────────────────────────────────────────

// handleUpsertRating sets or updates the authenticated user's rating for a project.
//
// JSON body: { "rating": 1–5 }
func handleUpsertRating(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Rating int `json:"rating"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	if err := store.UpsertRating(claims.UserID, projectID, req.Rating); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found or not public")
		}
		return fail(c, 400, err.Error())
	}

	avg, count, err := store.GetProjectRatingStats(projectID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]any{
		"rating":      req.Rating,
		"avgRating":   avg,
		"ratingCount": count,
	})
}

// handleDeleteRating removes the authenticated user's rating for a project.
func handleDeleteRating(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	if err := store.DeleteRating(claims.UserID, projectID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "rating not found")
		}
		return fail(c, 500, "internal error")
	}

	avg, count, err := store.GetProjectRatingStats(projectID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]any{
		"rating":      0,
		"avgRating":   avg,
		"ratingCount": count,
	})
}

// handleGetOwnRating returns the authenticated user's current rating for a project.
func handleGetOwnRating(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	rating, err := store.GetUserRating(claims.UserID, projectID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	avg, count, err := store.GetProjectRatingStats(projectID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]any{
		"rating":      rating,
		"avgRating":   avg,
		"ratingCount": count,
	})
}

// ─── Follow (from feed card context) ─────────────────────────────────────────

// handleFollowByUsername follows a user identified by their username.
func handleFollowByUsername(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	username := strings.TrimSpace(c.Param("username"))

	target, err := store.GetUserByUsername(username)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}

	if err := store.FollowUser(claims.UserID, target.ID); err != nil {
		if err == store.ErrConflict {
			return ok(c, map[string]bool{"following": true}) // idempotent
		}
		return fail(c, 400, err.Error())
	}
	return ok(c, map[string]bool{"following": true})
}

// handleUnfollowByUsername unfollows a user identified by their username.
func handleUnfollowByUsername(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	username := strings.TrimSpace(c.Param("username"))

	target, err := store.GetUserByUsername(username)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}

	if err := store.UnfollowUser(claims.UserID, target.ID); err != nil {
		if err == store.ErrNotFound {
			return ok(c, map[string]bool{"following": false}) // idempotent
		}
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]bool{"following": false})
}

// handleIsFollowing returns whether the authenticated user follows a given username.
func handleIsFollowing(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	username := strings.TrimSpace(c.Param("username"))

	target, err := store.GetUserByUsername(username)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}

	following, err := store.IsFollowing(claims.UserID, target.ID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]bool{"following": following})
}

// handleGetFollowing returns the list of users that a given username follows.
func handleGetFollowing(c echo.Context) error {
	username := strings.TrimSpace(c.Param("username"))

	target, err := store.GetUserByUsername(username)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}

	following, err := store.ListFollowing(target.ID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if following == nil {
		following = []*store.FollowUser_{}
	}
	return ok(c, following)
}

// handleGetFollowers returns the list of users that follow a given username.
func handleGetFollowers(c echo.Context) error {
	username := strings.TrimSpace(c.Param("username"))

	target, err := store.GetUserByUsername(username)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "user not found")
		}
		return fail(c, 500, "internal error")
	}

	followers, err := store.ListFollowers(target.ID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if followers == nil {
		followers = []*store.FollowUser_{}
	}
	return ok(c, followers)
}

// ─── Response helpers ─────────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": 200},
		"data":     data,
	})
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
