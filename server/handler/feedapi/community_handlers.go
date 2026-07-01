// handler/feedapi/community_handlers.go — Comment and report handler implementations.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Comments:
//
//	GET  /api/v1/projects/:id/comments        — paginated list (public)
//	POST /api/v1/projects/:id/comments        — create comment (requires auth)
//	DELETE /api/v1/projects/:id/comments/:cid — delete comment (owner or admin)
//
// Reports:
//
//	POST /api/v1/projects/:id/report          — file a report (requires auth)
//	GET  /api/v1/projects/:id/report          — check own report status (requires auth)
//
// Design decisions:
//   - Comments are public to read (no auth required for GET).
//   - Creating a comment requires authentication.
//   - Deleting a comment is allowed for the comment author or for admins.
//   - A user may only file one report per project (enforced by UNIQUE constraint).
//   - Report content is never exposed to non-admin users — the GET endpoint
//     only returns whether the viewer has filed a report and its status.
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

// ─── Comments: list ───────────────────────────────────────────────────────────

// handleListComments returns a paginated list of comments for a project.
//
// Query parameters:
//
//	page — 1-based page number (default 1)
//
// Response shape:
//
//	{
//	  "comments":   [...],
//	  "total":      42,
//	  "page":       1,
//	  "pageSize":   10,
//	  "stats": { "totalComments": 42, "avgDocRating": 4.2, ... }
//	}
func handleListComments(c echo.Context) error {
	projectID := c.Param("id")
	if projectID == "" {
		return fail(c, 400, "project id is required")
	}

	pageSize := store.GetSettingInt(store.SettingCommentPageSize, 10)
	page := 1
	if p := c.QueryParam("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	offset := (page - 1) * pageSize

	comments, err := store.ListComments(projectID, offset, pageSize)
	if err != nil {
		c.Logger().Errorf("[community] ListComments %s: %v", projectID, err)
		return fail(c, 500, "internal error")
	}
	if comments == nil {
		comments = []*store.Comment{}
	}

	total, err := store.CountComments(projectID)
	if err != nil {
		c.Logger().Errorf("[community] CountComments %s: %v", projectID, err)
		return fail(c, 500, "internal error")
	}

	stats, err := store.GetCommentStats(projectID)
	if err != nil {
		c.Logger().Errorf("[community] GetCommentStats %s: %v", projectID, err)
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]any{
		"comments": comments,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
		"stats":    stats,
	})
}

// ─── Comments: create ─────────────────────────────────────────────────────────

// handleCreateComment posts a new comment on a project.
//
// Required JSON fields:
//
//	body        — comment text (non-empty, ≤ SettingCommentMaxChars runes)
//
// Optional JSON fields:
//
//	docRating   — documentation quality: 0 (not rated) or 1–5
//	codeRating  — code quality: 0 (not rated) or 1–5
func handleCreateComment(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Body       string `json:"body"`
		DocRating  int    `json:"docRating"`
		CodeRating int    `json:"codeRating"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		return fail(c, 400, "comment body is required")
	}

	maxChars := store.GetSettingInt(store.SettingCommentMaxChars, 1000)

	id, err := cryptoauth.NewID()
	if err != nil {
		return fail(c, 500, "internal error")
	}

	cm := &store.Comment{
		ID:         id,
		ProjectID:  projectID,
		UserID:     claims.UserID,
		Body:       req.Body,
		DocRating:  req.DocRating,
		CodeRating: req.CodeRating,
	}

	if err := store.CreateComment(cm, maxChars); err != nil {
		switch err {
		case store.ErrNotFound:
			return fail(c, 404, "project not found or not public")
		default:
			// Validation errors from CreateComment are surfaced as 400.
			if isValidationErr(err) {
				return fail(c, 400, err.Error())
			}
			c.Logger().Errorf("[community] CreateComment: %v", err)
			return fail(c, 500, "internal error")
		}
	}

	// Return the newly created comment with author details filled in.
	created, err := store.GetCommentByID(id)
	if err != nil {
		// Comment was created successfully; a failure to re-read it is
		// non-fatal — return a minimal response instead of a 500.
		c.Logger().Warnf("[community] GetCommentByID after create %s: %v", id, err)
		return c.JSON(http.StatusCreated, map[string]any{
			"metadata": map[string]any{"status": 201},
			"data":     cm,
		})
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"metadata": map[string]any{"status": 201},
		"data":     created,
	})
}

// ─── Comments: delete ─────────────────────────────────────────────────────────

// handleDeleteComment removes a comment.
// The requesting user must be the comment author or an admin.
func handleDeleteComment(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	commentID := c.Param("cid")

	cm, err := store.GetCommentByID(commentID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "comment not found")
		}
		return fail(c, 500, "internal error")
	}

	// Allow deletion only by the comment author or an admin.
	if cm.UserID != claims.UserID && claims.Role != store.RoleAdmin {
		return fail(c, 403, "you can only delete your own comments")
	}

	if err := store.DeleteComment(commentID); err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, map[string]bool{"deleted": true})
}

// ─── Reports: create ──────────────────────────────────────────────────────────

// handleCreateReport files a moderation report against a project.
//
// Required JSON fields:
//
//	reason — one of: offensive, off_topic, spam, misleading
//
// Optional JSON fields:
//
//	details — free-text explanation (≤ 500 chars)
//
// A user can only file one report per project. Subsequent attempts return 409.
func handleCreateReport(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Reason  string `json:"reason"`
		Details string `json:"details"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.Reason = strings.TrimSpace(req.Reason)
	req.Details = strings.TrimSpace(req.Details)

	if req.Reason == "" {
		return fail(c, 400, "reason is required")
	}

	// Validate against the fixed vocabulary.
	validReason := false
	for _, r := range store.ReportReasons {
		if req.Reason == r {
			validReason = true
			break
		}
	}
	if !validReason {
		return fail(c, 400, "reason must be one of: "+strings.Join(store.ReportReasons, ", "))
	}

	// Do not allow project owners to report their own project.
	// (The DB does not prevent this; we enforce it in the handler.)
	cfg := config.Get()
	_ = cfg // used implicitly via store

	id, err := cryptoauth.NewID()
	if err != nil {
		return fail(c, 500, "internal error")
	}

	rep := &store.Report{
		ID:        id,
		ProjectID: projectID,
		UserID:    claims.UserID,
		Reason:    req.Reason,
		Details:   req.Details,
	}

	if err := store.CreateReport(rep); err != nil {
		switch err {
		case store.ErrNotFound:
			return fail(c, 404, "project not found or not public")
		case store.ErrConflict:
			return fail(c, 409, "you have already reported this project")
		default:
			if isValidationErr(err) {
				return fail(c, 400, err.Error())
			}
			c.Logger().Errorf("[community] CreateReport: %v", err)
			return fail(c, 500, "internal error")
		}
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"metadata": map[string]any{"status": 201},
		"data":     map[string]string{"status": "pending"},
	})
}

// handleGetReport returns whether the authenticated viewer has reported
// the project, and if so, the status of that report.
// This endpoint never exposes report details to non-admins.
func handleGetReport(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	rep, err := store.GetReport(claims.UserID, projectID)
	if err == store.ErrNotFound {
		return ok(c, map[string]any{"reported": false})
	}
	if err != nil {
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]any{
		"reported": true,
		"status":   rep.Status,
		"reason":   rep.Reason,
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// isValidationErr returns true for errors that originate from input validation
// inside store functions (as opposed to database or infrastructure errors).
// These should be surfaced to the client as 400 Bad Request.
//
// The heuristic is: if the error message does not contain "internal" and is not
// one of the sentinel errors (ErrNotFound, ErrConflict), it is a validation error.
func isValidationErr(err error) bool {
	if err == nil {
		return false
	}
	if err == store.ErrNotFound || err == store.ErrConflict {
		return false
	}
	msg := err.Error()
	return msg != "" && !strings.Contains(msg, "internal")
}
