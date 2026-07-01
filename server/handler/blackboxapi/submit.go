// server/handler/blackboxapi/submit.go — Device submit and job status endpoints.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// POST /api/v1/blackbox/submit
//
//	Validates a GitHub release URL, verifies the owner matches the user's
//	verified github_username, enqueues a device:github worker task, and
//	returns the job ID for polling.
//
//	Request:  { "github_url": "https://github.com/owner/repo/releases/tag/v1.2",
//	            "tags": "math,signal" }
//	Response: { "job_id": "...", "status": "pending" }
//
// GET /api/v1/blackbox/jobs/:jobId
//
//	Polls the Redis key "device:job:{jobId}" for the worker result.
//	Returns 202 while pending, 200 when done or error.
//
//	Response: { "status": "pending"|"done"|"error",
//	            "devices": [...],   // on done
//	            "error": "..." }    // on error
//
// GET /api/v1/blackbox/mine
//
//	Lists all devices owned by the authenticated user.
package blackboxapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	cryptoauth "server/auth"
	"server/handler/spaauth"
	"server/store"
	"server/tasks"
)

// githubReleaseRe matches: github.com/{owner}/{repo}/releases/tag/{tag}
// Both http:// and https:// prefixes are accepted; the scheme is optional.
var githubReleaseRe = regexp.MustCompile(
	`^(?:https?://)?github\.com/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)/releases/tag/([^/\s]+)$`,
)

// ─── Submit ───────────────────────────────────────────────────────────────────

func (h *handler) handleSubmit(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var body struct {
		// Name is the human-readable display name chosen by the specialist in
		// the "New Project" modal. When present it overrides the default
		// "owner/repo" placeholder. The worker may still overwrite this with
		// the first # heading found in readme.md inside the ZIP.
		Name          string `json:"name"`
		GithubURL     string `json:"github_url"`
		Tags          string `json:"tags"`
		Visibility    string `json:"visibility"`
		CategoryID    string `json:"categoryId"`
		SubcategoryID string `json:"subcategoryId"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	body.GithubURL = strings.TrimSpace(body.GithubURL)
	if body.GithubURL == "" {
		return fail(c, http.StatusBadRequest, "github_url is required")
	}

	// Parse and validate the GitHub release URL.
	owner, repo, tag, err := parseGithubReleaseURL(body.GithubURL)
	if err != nil {
		return fail(c, http.StatusBadRequest, err.Error())
	}

	// Verify that the URL owner matches the user's verified GitHub username.
	// This prevents submitting repositories that belong to someone else.
	githubUsername, dbErr := store.GetGithubUsername(claims.UserID)
	if dbErr != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if githubUsername == "" {
		return fail(c, http.StatusForbidden,
			"GitHub account not connected. Before submitting a device, go to your Profile page and click ‘Connect GitHub’ to link your account. This is required so IoTMaker can verify that you own the repository.")
	}
	if !strings.EqualFold(owner, githubUsername) {
		return fail(c, http.StatusForbidden,
			fmt.Sprintf("The repository owner \"%s\" does not match your connected GitHub account \"%s\". IoTMaker only allows publishing repositories that belong to your own account. To publish someone else's code, fork the repository to your account first, then submit the forked URL.",
				owner, githubUsername))
	}

	// Find existing devices from this repo by owner+repo (ignoring tag/version).
	// Passing existing IDs causes the worker to UPDATE those rows instead of
	// creating duplicates — one row per struct per repo, always the latest.
	existingIDs, lookupErr := store.GetDeviceIDsByGithubRepo(claims.UserID, owner, repo)
	if lookupErr != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Generate a job ID for polling.
	jobID, err := cryptoauth.NewID()
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Normalise visibility — only "public" is accepted; everything else is private.
	visibility := body.Visibility
	if visibility != "public" {
		visibility = "private"
	}

	// DisplayNameHuman defaults to "owner/repo".
	// When the specialist fills in the name field in the modal, that value is
	// used instead. The worker may still overwrite it with the first # heading
	// from readme.md inside the ZIP — the user-supplied name acts only as the
	// initial placeholder shown while the worker runs.
	displayNameHuman := owner + "/" + repo
	if n := strings.TrimSpace(body.Name); n != "" {
		displayNameHuman = n
	}

	t, err := tasks.NewDeviceGitHubTask(tasks.DeviceGitHubPayload{
		JobID:            jobID,
		UserID:           claims.UserID,
		ExistingIDs:      existingIDs,
		GithubURL:        body.GithubURL,
		Owner:            owner,
		Repo:             repo,
		Tag:              tag,
		Tags:             body.Tags,
		Visibility:       visibility,
		DisplayNameHuman: displayNameHuman,
		CategoryID:       body.CategoryID,
		SubcategoryID:    body.SubcategoryID,
	})
	if err != nil {
		return fail(c, http.StatusInternalServerError, "could not create task")
	}
	if _, err := h.asynq.Enqueue(t); err != nil {
		return fail(c, http.StatusInternalServerError, "could not enqueue task")
	}

	return c.JSON(http.StatusAccepted, map[string]any{
		"job_id": jobID,
		"status": "pending",
	})
}

// ─── Job status ───────────────────────────────────────────────────────────────

func (h *handler) handleJobStatus(c echo.Context) error {
	jobID := c.Param("jobId")
	if jobID == "" {
		return fail(c, http.StatusBadRequest, "jobId is required")
	}

	key := fmt.Sprintf("device:job:%s", jobID)
	val, err := h.redis.Get(context.Background(), key).Result()
	if err == redis.Nil {
		// Key not yet set — worker hasn't finished.
		return c.JSON(http.StatusAccepted, map[string]string{"status": "pending"})
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Pass the raw JSON from Redis straight to the client.
	// The worker writes { status, devices, errors } or { status, error }.
	var result json.RawMessage
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return fail(c, http.StatusInternalServerError, "malformed job result")
	}

	return c.JSON(http.StatusOK, result)
}

// ─── Get one (with parsedJson) ────────────────────────────────────────────────

// handleGetOne returns the full Device record — including parsed_json — for
// the authenticated owner. Called by the Projects page when the user expands
// a device row to render the visual block preview.
//
// GET /api/v1/blackbox/:id
//
// Response: the Device JSON (same shape as the blackboxes table row), with
// parsedJson included. Only the owner can fetch their own device this way.
func (h *handler) handleGetOne(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")
	if id == "" {
		return fail(c, http.StatusBadRequest, "device id is required")
	}

	d, err := store.GetDeviceForOwner(id, claims.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "device not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	return c.JSON(http.StatusOK, d)
}

// ─── List mine ────────────────────────────────────────────────────────────────

func (h *handler) handleListMine(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	devices, err := store.ListDevicesByUser(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	return c.JSON(http.StatusOK, devices)
}

// ─── Delete ──────────────────────────────────────────────────────────────────

// handleDelete permanently deletes a device owned by the authenticated user.
func (h *handler) handleDelete(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")
	if id == "" {
		return fail(c, http.StatusBadRequest, "device id is required")
	}

	// Fetch the device before deleting to get the struct name for menu cleanup.
	oldDev, _ := store.GetDeviceForOwner(id, claims.UserID)

	if err := store.DeleteDevice(id, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, http.StatusNotFound, "device not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Remove from menu tree only if the owner is admin/official_specialist.
	// Regular user devices are never in the global menu.
	if oldDev != nil && oldDev.DisplayName != "" &&
		(claims.Role == store.RoleAdmin || claims.Role == store.RoleOfficialSpecialist) {
		if err := store.RemoveDeviceFromMenu(oldDev.DisplayName); err != nil {
			log.Printf("[blackboxapi] remove deleted device %s from menu: %v", oldDev.DisplayName, err)
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── Update meta ──────────────────────────────────────────────────────────────

// handleUpdateMeta edits the tags and visibility of a device owned by the user.
//
//	PATCH /api/v1/blackbox/:id
//	Body: { "tags": "sensor,i2c", "visibility": "public" }
func (h *handler) handleUpdateMeta(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")
	if id == "" {
		return fail(c, http.StatusBadRequest, "device id is required")
	}
	var body struct {
		Tags            string `json:"tags"`
		Visibility      string `json:"visibility"`
		CategoryID      string `json:"categoryId"`
		SubcategoryID   string `json:"subcategoryId"`
		PublishToFeed   bool   `json:"publishToFeed"`
		PublishToSearch bool   `json:"publishToSearch"`
		ReadyToUse      bool   `json:"readyToUse"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	if body.Visibility != "public" && body.Visibility != "private" {
		return fail(c, http.StatusBadRequest, "visibility must be 'public' or 'private'")
	}

	// Fetch the device BEFORE the update to detect visibility changes.
	oldDev, err := store.GetDeviceForOwner(id, claims.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "device not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	if err := store.UpdateDeviceMeta(
		id, claims.UserID,
		body.Tags, body.Visibility,
		body.CategoryID, body.SubcategoryID,
		body.PublishToFeed, body.PublishToSearch, body.ReadyToUse,
	); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "device not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// ── Menu sync: visibility changed → update the menu tree ─────────────
	// Only admin and official_specialist devices are visible to everyone.
	// Regular user devices stay in "My Items" only — they appear in the
	// feed when public, but not in the IDE menu of other users.
	if oldDev.Visibility != body.Visibility &&
		(claims.Role == store.RoleAdmin || claims.Role == store.RoleOfficialSpecialist) {
		if body.Visibility == "public" && oldDev.Status == "ready" {
			label := oldDev.DisplayNameHuman
			if label == "" {
				label = oldDev.DisplayName
			}
			if err := store.AutoInsertDeviceToMenu(
				id, oldDev.DisplayName, label,
				body.CategoryID, body.SubcategoryID,
			); err != nil {
				log.Printf("[blackboxapi] auto-insert device %s to menu: %v", oldDev.DisplayName, err)
			}
		} else if body.Visibility == "private" {
			if err := store.RemoveDeviceFromMenu(oldDev.DisplayName); err != nil {
				log.Printf("[blackboxapi] remove device %s from menu: %v", oldDev.DisplayName, err)
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "updated"})
}

// ─── URL parser ───────────────────────────────────────────────────────────────

// parseGithubReleaseURL validates and extracts owner, repo, tag from a GitHub
// release URL. Accepted format:
//
//	https://github.com/{owner}/{repo}/releases/tag/{tag}
func parseGithubReleaseURL(rawURL string) (owner, repo, tag string, err error) {
	m := githubReleaseRe.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", "", fmt.Errorf(
			"invalid GitHub release URL — expected: https://github.com/{owner}/{repo}/releases/tag/{version}")
	}
	return m[1], m[2], m[3], nil
}

// ─── Response helpers ─────────────────────────────────────────────────────────

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"error": msg,
	})
}
