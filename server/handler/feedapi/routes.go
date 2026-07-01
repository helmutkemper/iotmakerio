// handler/feedapi/routes.go — Feed, community, and follow API routes.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Routes:
//
//	GET    /api/v1/feed                               — main feed (all tabs, auth optional)
//
//	GET    /api/v1/projects/:id/rating                — own rating (requires auth)
//	PUT    /api/v1/projects/:id/rating                — upsert rating (requires auth)
//	DELETE /api/v1/projects/:id/rating                — delete rating (requires auth)
//
//	GET    /api/v1/projects/:id/comments              — list comments (public)
//	POST   /api/v1/projects/:id/comments              — create comment (requires auth)
//	DELETE /api/v1/projects/:id/comments/:cid         — delete comment (owner or admin)
//
//	POST   /api/v1/projects/:id/report                — file a report (requires auth)
//	GET    /api/v1/projects/:id/report                — check own report status (requires auth)
//
//	GET    /api/v1/users/:username/follow             — is-following check (requires auth)
//	POST   /api/v1/users/:username/follow             — follow user (requires auth)
//	DELETE /api/v1/users/:username/follow             — unfollow user (requires auth)
//	GET    /api/v1/users/:username/following          — list who username follows (public)
//	GET    /api/v1/users/:username/followers          — list username's followers (public)
//
// Route ordering notes:
//   - /users/:username/following and /followers must be registered before
//     the auth group to avoid Echo matching "following" as a :username value.
//   - /projects/:id/comments is registered before /projects/:id/rating so
//     static paths win over any future parameterised siblings.
package feedapi

import (
	"server/handler/spaauth"

	"github.com/labstack/echo/v4"
)

// Register mounts all feed, community, and follow routes on the /api/v1 group.
func Register(g *echo.Group) {
	// ── Feed (public — auth optional for own_rating and is_following) ─────────
	g.GET("/feed", handleGetFeed)

	// ── Rating (requires auth) ────────────────────────────────────────────────
	rating := g.Group("/projects/:id/rating", spaauth.RequireBearerToken())
	rating.GET("", handleGetOwnRating)
	rating.PUT("", handleUpsertRating)
	rating.DELETE("", handleDeleteRating)

	// ── Comments ──────────────────────────────────────────────────────────────
	// GET is public; POST and DELETE require a Bearer token.
	// The DELETE route has a second parameter :cid (comment ID) so Echo can
	// distinguish it from the collection endpoint.
	g.GET("/projects/:id/comments", handleListComments)
	commentsAuth := g.Group("/projects/:id/comments", spaauth.RequireBearerToken())
	commentsAuth.POST("", handleCreateComment)
	commentsAuth.DELETE("/:cid", handleDeleteComment)

	// ── Reports (requires auth) ───────────────────────────────────────────────
	report := g.Group("/projects/:id/report", spaauth.RequireBearerToken())
	report.POST("", handleCreateReport)
	report.GET("", handleGetReport)

	// ── Follow — public list endpoints (no auth) ──────────────────────────────
	// These must be registered before the auth group to ensure Echo matches
	// the static segments (/following, /followers) before the parameterised routes.
	g.GET("/users/:username/following", handleGetFollowing)
	g.GET("/users/:username/followers", handleGetFollowers)

	// ── Follow — mutation endpoints (requires auth) ───────────────────────────
	follow := g.Group("/users/:username/follow", spaauth.RequireBearerToken())
	follow.GET("", handleIsFollowing)
	follow.POST("", handleFollowByUsername)
	follow.DELETE("", handleUnfollowByUsername)
}
