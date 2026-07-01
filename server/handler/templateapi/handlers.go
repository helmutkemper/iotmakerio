// /ide/server/handler/templateapi/handlers.go — Template Package API handler implementations.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// See routes.go for the full route list and the lifecycle description.
package templateapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	cryptoauth "server/auth"
	"server/config"
	"server/handler/spaauth"
	"server/store"
	"server/tasks"
)

// ── Constants ──────────────────────────────────────────────────────────────────

const ()

// safeFilenameRe matches characters NOT safe for a Content-Disposition filename.
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._\-]`)

// sanitizeFilename returns a safe ASCII string for Content-Disposition headers.
func sanitizeFilename(name string) string {
	safe := safeFilenameRe.ReplaceAllString(name, "_")
	safe = strings.Trim(safe, "_")
	if safe == "" {
		return "template"
	}
	return safe
}

// ── Create package (no ZIP) ────────────────────────────────────────────────────

// handleCreate creates a new template package and immediately enqueues a
// template:github worker task to download and parse the release ZIP.
//
// This unified flow mirrors POST /api/v1/blackbox/submit for devices:
// one request, one job_id to poll, no two-step creation.
//
// Request:
//
//	{
//	  "github_url":    "https://github.com/owner/repo/releases/tag/v1.0",
//	  "tags":          "webserver,echo",
//	  "visibility":    "private" | "public",
//	  "category_id":   "...",       // optional
//	  "subcategory_id":"...",       // optional
//	}
//
// Response: 202 Accepted  { "job_id": "...", "status": "pending" }
//
// Poll GET /api/v1/templates/jobs/:jobId until status != "pending".
func (h *handler) handleCreate(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var body struct {
		// Name is the human-readable display name chosen by the specialist in
		// the "New Project" modal. When present it is used as the initial
		// pkg.Name placeholder instead of the bare repo slug.
		Name          string `json:"name"`
		GithubURL     string `json:"github_url"`
		Tags          string `json:"tags"`
		Visibility    string `json:"visibility"`
		CategoryID    string `json:"category_id"`
		SubcategoryID string `json:"subcategory_id"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	body.GithubURL = strings.TrimSpace(body.GithubURL)
	if body.GithubURL == "" {
		return fail(c, http.StatusBadRequest, "github_url is required")
	}
	if body.Visibility != store.TemplatePkgVisibilityPublic {
		body.Visibility = store.TemplatePkgVisibilityPrivate
	}

	// Validate the GitHub release URL and verify ownership.
	owner, repo, tag, parseErr := parseGithubReleaseURL(body.GithubURL)
	if parseErr != nil {
		return fail(c, http.StatusBadRequest, parseErr.Error())
	}
	githubUsername, dbErr := store.GetGithubUsername(claims.UserID)
	if dbErr != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if githubUsername == "" {
		return fail(c, http.StatusForbidden,
			"GitHub account not connected. Before submitting a template, go to your Profile page and click 'Connect GitHub' to link your account.")
	}
	if !strings.EqualFold(owner, githubUsername) {
		return fail(c, http.StatusForbidden,
			"The repository owner does not match your connected GitHub account. IoTMaker only allows publishing repositories that belong to your own account. Fork the repository to your account first, then submit the forked URL.")
	}

	// pkgName starts as the repository slug and is replaced by the user-
	// supplied name when the specialist fills it in the modal.
	// The worker may still overwrite it with the first # heading from readme.md.
	pkgName := repo
	if n := strings.TrimSpace(body.Name); n != "" {
		pkgName = n
	}

	// Create the parent template_packages record.
	// The display name will be updated by the worker from readme.md.
	pkg := &store.TemplatePackage{
		UserID:        claims.UserID,
		Name:          pkgName,
		Visibility:    body.Visibility,
		GithubURL:     body.GithubURL,
		GithubOwner:   owner,
		GithubRepo:    repo,
		GithubTag:     tag,
		Tags:          body.Tags,
		CategoryID:    body.CategoryID,
		SubcategoryID: body.SubcategoryID,
	}
	if err := store.CreateTemplatePkg(pkg); err != nil {
		log.Printf("[templateapi/create] db create userID=%s: %v", claims.UserID, err)
		return fail(c, http.StatusInternalServerError, "could not create template record")
	}

	// Create the version row.
	v := &store.TemplatePackageVersion{
		PkgID:     pkg.ID,
		UserID:    claims.UserID,
		GithubURL: body.GithubURL,
		GithubTag: tag,
	}
	if err := store.CreateTemplatePkgVersionGitHub(v); err != nil {
		log.Printf("[templateapi/create] version create pkgID=%s: %v", pkg.ID, err)
		return fail(c, http.StatusInternalServerError, "could not create version record")
	}

	// Generate a job ID for polling (same pattern as device submit).
	jobID, err := cryptoauth.NewID()
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Enqueue parse task.
	t, taskErr := tasks.NewTemplateGitHubTask(tasks.TemplateGitHubPayload{
		JobID:          jobID,
		VersionID:      v.ID,
		PkgID:          pkg.ID,
		Name:           pkgName,
		GithubURL:      body.GithubURL,
		Owner:          owner,
		Repo:           repo,
		Tag:            tag,
		Visibility:     body.Visibility,
		CategoryID:     body.CategoryID,
		SubcategoryID:  body.SubcategoryID,
		Tags:           body.Tags,
		UploaderUserID: claims.UserID,
	})
	if taskErr != nil {
		log.Printf("[templateapi/create] build task pkgID=%s: %v", pkg.ID, taskErr)
		return fail(c, http.StatusInternalServerError, "could not create task")
	}
	if _, err := h.asynq.Enqueue(t); err != nil {
		log.Printf("[templateapi/create] enqueue pkgID=%s: %v", pkg.ID, err)
		return fail(c, http.StatusInternalServerError, "could not enqueue task")
	}

	return c.JSON(http.StatusAccepted, map[string]any{
		"job_id": jobID,
		"status": "pending",
		"pkg_id": pkg.ID,
	})
}

// handleJobStatus polls the Redis key "template:job:{jobId}" for the worker
// result. Returns 202 while pending, 200 when done or error.
// This mirrors GET /api/v1/blackbox/jobs/:jobId for devices.
func (h *handler) handleJobStatus(c echo.Context) error {
	jobID := c.Param("jobId")
	if jobID == "" {
		return fail(c, http.StatusBadRequest, "jobId is required")
	}
	key := "template:job:" + jobID
	val, err := h.redis.Get(context.Background(), key).Result()
	if err == redis.Nil {
		return c.JSON(http.StatusAccepted, map[string]string{"status": "pending"})
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	var result json.RawMessage
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return fail(c, http.StatusInternalServerError, "malformed job result")
	}
	return c.JSON(http.StatusOK, result)
}

// ── Upload version (ZIP) ───────────────────────────────────────────────────────

// handleUploadVersion previously accepted a ZIP file upload.
// Template versions are now submitted via GitHub release URL — see the
// implementation plan for the new POST /:id/github endpoint.
// Returns 410 Gone so the SPA shows a clear message instead of a silent error.
func (h *handler) handleUploadVersion(c echo.Context) error {
	return fail(c, http.StatusGone,
		"ZIP uploads are no longer supported. Submit a GitHub release URL instead.")
}

// ── Submit GitHub release ─────────────────────────────────────────────────────

// handleSubmitGithub receives a GitHub release URL for an existing template
// package, creates a version row, and enqueues a template:github worker task.
//
// Request:  { "github_url": "https://github.com/owner/repo/releases/tag/v1.2",
//
//	"tags": "ecommerce,postgresql" }
//
// Response: 202 Accepted  { TemplatePackageVersion }
//
// Poll GET /api/v1/templates/:id until status != "pending".
func (h *handler) handleSubmitGithub(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	pkgID := c.Param("id")

	var body struct {
		GithubURL string `json:"github_url"`
		Tags      string `json:"tags"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	body.GithubURL = strings.TrimSpace(body.GithubURL)
	if body.GithubURL == "" {
		return fail(c, http.StatusBadRequest, "github_url is required")
	}

	// Verify ownership.
	pkg, err := store.GetTemplatePkg(pkgID)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, "template not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if pkg.UserID != claims.UserID {
		return fail(c, http.StatusForbidden, "not your template")
	}

	// Parse and validate the GitHub release URL.
	owner, repo, tag, parseErr := parseGithubReleaseURL(body.GithubURL)
	if parseErr != nil {
		return fail(c, http.StatusBadRequest, parseErr.Error())
	}

	// Verify the URL owner matches the user's verified GitHub username.
	githubUsername, dbErr := store.GetGithubUsername(claims.UserID)
	if dbErr != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if githubUsername == "" {
		return fail(c, http.StatusForbidden,
			"GitHub account not connected. Before submitting a template, go to your Profile page and click 'Connect GitHub' to link your account.")
	}
	if !strings.EqualFold(owner, githubUsername) {
		return fail(c, http.StatusForbidden,
			"The repository owner does not match your connected GitHub account. IoTMaker only allows publishing repositories that belong to your own account. Fork the repository to your account first, then submit the forked URL.")
	}

	// Create the version row.
	v := &store.TemplatePackageVersion{
		PkgID:     pkgID,
		UserID:    claims.UserID,
		GithubURL: body.GithubURL,
		GithubTag: tag,
	}
	if err := store.CreateTemplatePkgVersionGitHub(v); err != nil {
		log.Printf("[templateapi/submit-github] db create version pkgID=%s: %v", pkgID, err)
		return fail(c, http.StatusInternalServerError, "could not create version record")
	}

	// Update tags on the parent package if provided.
	if body.Tags != "" {
		_ = store.UpdateTemplatePkgTags(pkgID, claims.UserID, body.Tags)
	}

	// Enqueue parse task.
	t, taskErr := tasks.NewTemplateGitHubTask(tasks.TemplateGitHubPayload{
		VersionID:      v.ID,
		PkgID:          pkgID,
		GithubURL:      body.GithubURL,
		Owner:          owner,
		Repo:           repo,
		Tag:            tag,
		UploaderUserID: claims.UserID,
	})
	if taskErr != nil {
		log.Printf("[templateapi/submit-github] build task versionID=%s: %v", v.ID, taskErr)
	} else if _, err := h.asynq.Enqueue(t); err != nil {
		log.Printf("[templateapi/submit-github] enqueue task versionID=%s: %v", v.ID, err)
	}

	return c.JSON(http.StatusAccepted, ok200(v))
}

// parseGithubReleaseURL validates and extracts owner, repo, tag from a GitHub
// release URL. Accepted format:
//
//	https://github.com/{owner}/{repo}/releases/tag/{tag}
var githubReleaseRe = regexp.MustCompile(
	`^(?:https?://)?github\.com/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)/releases/tag/([^/\s]+)$`,
)

func parseGithubReleaseURL(rawURL string) (owner, repo, tag string, err error) {
	m := githubReleaseRe.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", "", fmt.Errorf(
			"invalid GitHub release URL — expected: https://github.com/{owner}/{repo}/releases/tag/{version}")
	}
	return m[1], m[2], m[3], nil
}

// ── List ───────────────────────────────────────────────────────────────────────

// handleList returns templates visible to the authenticated caller.
//
//	?mine=true  — caller's own templates only (all statuses)
//	default     — public+ready templates merged with caller's own
//
// ─── Ownership origin constants ──────────────────────────────────────────────
//
// These strings travel in the JSON response as the "origin" field and are
// mirrored on the WASM side in /ide/templateclient/clientTypes.go. A typo on
// either side produces a silent "My Items" filtering bug — keep the two lists
// exactly identical. The third value used by the WASM, "curated", does not
// appear in template responses because templates are never embedded in admin-
// curated menu sections (only devices are).
const (
	// originOwn marks a template authored by the authenticated caller.
	originOwn = "own"
	// originPublic marks a template authored by another specialist and shared
	// publicly. The caller may place it on the canvas but it must NEVER appear
	// under "My Items" in the IDE menu.
	originPublic = "public"
)

// templateListItem is the DTO returned by handleList.
// It extends TemplatePackage with resolved category/subcategory names so the
// WASM client can build the IDE menu hierarchy without extra round-trips, and
// with ownership markers so the client does not have to decode the JWT or
// perform any other caller-identity reasoning to answer "is this mine?".
type templateListItem struct {
	store.TemplatePackage
	MenuCategory    string `json:"menuCategory,omitempty"`
	MenuSubcategory string `json:"menuSubcategory,omitempty"`

	// Origin is one of originOwn or originPublic (see constants above).
	// Omitempty is intentional: unset means "unknown provenance", which is
	// the safe default for any future caller that forgets to populate it.
	Origin string `json:"origin,omitempty"`

	// IsOwn is the boolean shortcut for Origin == originOwn. Having both a
	// discriminator (Origin) and a flag (IsOwn) costs two bytes per item and
	// removes an entire class of "did you remember to compare against the
	// right string constant?" bugs on the WASM client.
	IsOwn bool `json:"isOwn,omitempty"`
}

// stampTemplateOwnership returns the appropriate (origin, isOwn) pair for a
// template package given the authenticated caller's id.
//
// Extracted as a pure function so it can be unit-tested without standing up
// a DB or an Echo context. See handlers_test.go.
//
// The empty-callerID case is handled conservatively: no caller means nothing
// is "own". That matches the intuition "I am not logged in, nothing here
// belongs to me" and prevents a race where a misconfigured middleware could
// accidentally stamp templates as owned by the nil user.
func stampTemplateOwnership(pkgUserID, callerID string) (origin string, isOwn bool) {
	if callerID != "" && pkgUserID == callerID {
		return originOwn, true
	}
	return originPublic, false
}

// resolveCategoryNames looks up the human-readable names for a template's
// CategoryID and SubcategoryID. Missing IDs are silently ignored — the
// caller treats an empty name as "Other".
func resolveCategoryNames(pkg store.TemplatePackage) (catName, subName string) {
	if pkg.CategoryID != "" {
		cat, err := store.GetCategoryByID(pkg.CategoryID)
		if err == nil {
			catName = cat.Name
		}
	}
	if pkg.SubcategoryID != "" {
		sub, err := store.GetSubcategoryByID(pkg.SubcategoryID)
		if err == nil {
			subName = sub.Name
		}
	}
	return
}

func (h *handler) handleList(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	if c.QueryParam("mine") == "true" {
		pkgs, err := store.ListTemplatePkgsByUser(claims.UserID)
		if err != nil {
			return fail(c, http.StatusInternalServerError, "internal error")
		}
		return ok(c, pkgs)
	}

	public, err := store.ListPublicTemplatePkgs()
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	own, err := store.ListTemplatePkgsByUser(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// Own templates first, then public — deduplicated by ID.
	seen := make(map[string]bool, len(public)+len(own))
	merged := make([]store.TemplatePackage, 0, len(public)+len(own))
	for _, p := range own {
		if !seen[p.ID] {
			seen[p.ID] = true
			merged = append(merged, p)
		}
	}
	for _, p := range public {
		if !seen[p.ID] {
			seen[p.ID] = true
			merged = append(merged, p)
		}
	}

	// Enrich each item with resolved category/subcategory names and with
	// ownership markers so the WASM client does not need to compare
	// UserID against a JWT-decoded uid. Category lookups are cheap
	// (small static table).
	items := make([]templateListItem, 0, len(merged))
	for _, pkg := range merged {
		catName, subName := resolveCategoryNames(pkg)
		origin, isOwn := stampTemplateOwnership(pkg.UserID, claims.UserID)
		items = append(items, templateListItem{
			TemplatePackage: pkg,
			MenuCategory:    catName,
			MenuSubcategory: subName,
			Origin:          origin,
			IsOwn:           isOwn,
		})
	}

	return ok(c, items)
}

// ── Get ────────────────────────────────────────────────────────────────────────

// handleGet returns the full template record and its parsed definition.
// Access: owner always; others only see public+ready.
func (h *handler) handleGet(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")

	pkg, err := store.GetTemplatePkg(id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, "template not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if pkg.UserID != claims.UserID {
		if pkg.Visibility != store.TemplatePkgVisibilityPublic ||
			pkg.Status != store.TemplatePkgStatusReady {
			return fail(c, http.StatusNotFound, "template not found")
		}
	}

	response := map[string]any{"template": pkg}
	if pkg.Status == store.TemplatePkgStatusReady {
		def, err := store.GetTemplatePkgDef(id)
		if err != nil {
			log.Printf("[templateapi/get] load def pkgID=%s: %v", id, err)
		} else {
			// def is json.RawMessage — passed through as-is to the client.
			response["def"] = def
		}
	}

	return ok(c, response)
}

// ── Visibility ─────────────────────────────────────────────────────────────────

// handleVisibility changes the visibility of a template package.
// Only the owner can change it. Publishing to public requires status=ready.
//
// Request: { "visibility": "public" | "private" }
func (h *handler) handleVisibility(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")

	var body struct {
		Visibility string `json:"visibility"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	if body.Visibility != store.TemplatePkgVisibilityPublic &&
		body.Visibility != store.TemplatePkgVisibilityPrivate {
		return fail(c, http.StatusBadRequest, "visibility must be 'public' or 'private'")
	}

	pkg, err := store.GetTemplatePkg(id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, "template not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if pkg.UserID != claims.UserID {
		return fail(c, http.StatusForbidden, "not your template")
	}
	if body.Visibility == store.TemplatePkgVisibilityPublic &&
		pkg.Status != store.TemplatePkgStatusReady {
		return fail(c, http.StatusConflict,
			"template must be ready before it can be made public (status: "+pkg.Status+")",
		)
	}

	if err := store.UpdateTemplatePkgVisibility(id, claims.UserID, body.Visibility); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "template not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	// ── Menu sync: visibility changed → update the menu tree ─────────────
	// Only admin and official_specialist templates are visible to everyone.
	// Regular user templates stay in "My Items" only.
	if pkg.Visibility != body.Visibility &&
		(claims.Role == store.RoleAdmin || claims.Role == store.RoleOfficialSpecialist) {
		if body.Visibility == store.TemplatePkgVisibilityPublic {
			label := pkg.DisplayNameHuman
			if label == "" {
				label = pkg.Name
			}
			if err := store.AutoInsertTemplateToMenu(
				id, label, pkg.CategoryID, pkg.SubcategoryID,
			); err != nil {
				log.Printf("[templateapi] auto-insert template %s to menu: %v", id, err)
			}
		} else {
			if err := store.RemoveTemplateFromMenu(id); err != nil {
				log.Printf("[templateapi] remove template %s from menu: %v", id, err)
			}
		}
	}

	return ok(c, map[string]string{"visibility": body.Visibility})
}

// ── Tags + category ────────────────────────────────────────────────────────────

// handleTags updates the tags, categoryId, and subcategoryId for a template.
// Request: { "tags": "...", "categoryId": "...", "subcategoryId": "..." }
func (h *handler) handleTags(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")

	var body struct {
		Tags          string `json:"tags"`
		CategoryID    string `json:"categoryId"`
		SubcategoryID string `json:"subcategoryId"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}

	// Fetch current display name — UpdateTemplatePkgMeta requires it.
	pkg, err := store.GetTemplatePkg(id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, "template not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if pkg.UserID != claims.UserID {
		return fail(c, http.StatusForbidden, "not your template")
	}

	// Preserve the display name, update only tags and category.
	displayName := pkg.DisplayNameHuman
	if displayName == "" {
		displayName = pkg.GithubOwner + "/" + pkg.GithubRepo
	}
	if err := store.UpdateTemplatePkgMeta(id, displayName, body.Tags,
		body.CategoryID, body.SubcategoryID); err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	return ok(c, map[string]bool{"updated": true})
}

// ── Publishing flags ───────────────────────────────────────────────────────────

// handlePublishing sets the three community publishing flags for a template.
//
// Request body:
//
//	{ "publishToFeed": bool, "publishToSearch": bool, "readyToUse": bool }
func (h *handler) handlePublishing(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")

	var body struct {
		PublishToFeed   bool `json:"publishToFeed"`
		PublishToSearch bool `json:"publishToSearch"`
		ReadyToUse      bool `json:"readyToUse"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}

	pkg, err := store.GetTemplatePkg(id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, "template not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if pkg.UserID != claims.UserID {
		return fail(c, http.StatusForbidden, "not your template")
	}

	anyFlagEnabled := body.PublishToFeed || body.PublishToSearch || body.ReadyToUse
	if anyFlagEnabled {
		if pkg.Visibility != store.TemplatePkgVisibilityPublic {
			return fail(c, http.StatusConflict,
				"publishing flags can only be enabled for public templates — "+
					"change visibility to 'public' first",
			)
		}
		if pkg.Status != store.TemplatePkgStatusReady {
			return fail(c, http.StatusConflict,
				"publishing flags can only be enabled for ready templates (status: "+pkg.Status+")",
			)
		}
	}

	upd := &store.TemplatePublishingUpdate{
		PublishToFeed:   body.PublishToFeed,
		PublishToSearch: body.PublishToSearch,
		ReadyToUse:      body.ReadyToUse,
	}
	if err := store.UpdateTemplatePkgPublishing(id, claims.UserID, upd); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "template not found")
		}
		return fail(c, http.StatusInternalServerError, "internal error")
	}

	return ok(c, map[string]any{
		"publishToFeed":   body.PublishToFeed,
		"publishToSearch": body.PublishToSearch,
		"readyToUse":      body.ReadyToUse,
	})
}

// ── Delete ─────────────────────────────────────────────────────────────────────

// handleDelete removes the DB rows (parent + all versions via CASCADE) and the
// entire templates/{pkgID} directory from disk.
func (h *handler) handleDelete(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")
	cfg := config.Get()

	pkg, err := store.GetTemplatePkg(id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, "template not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	if pkg.UserID != claims.UserID {
		return fail(c, http.StatusForbidden, "not your template")
	}

	if err := store.DeleteTemplatePkg(id, claims.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail(c, http.StatusNotFound, "template not found")
		}
		return fail(c, http.StatusInternalServerError, "could not delete template record")
	}

	// Remove the entire templates/{pkgID}/ directory (all version ZIPs).
	pkgDir := filepath.Join(cfg.UserFilesDir, "templates", id)
	if err := os.RemoveAll(pkgDir); err != nil {
		log.Printf("[templateapi/delete] orphaned dir id=%s path=%s: %v", id, pkgDir, err)
	}

	// Remove from menu tree only if the owner is admin/official_specialist.
	if claims.Role == store.RoleAdmin || claims.Role == store.RoleOfficialSpecialist {
		if err := store.RemoveTemplateFromMenu(id); err != nil {
			log.Printf("[templateapi/delete] remove template %s from menu: %v", id, err)
		}
	}

	return ok(c, map[string]bool{"deleted": true})
}

// ── Generate ───────────────────────────────────────────────────────────────────

// handleGenerate produces a configured project ZIP for the maker to download.
//
// The def_json stored by the worker contains the full file tree from the
// GitHub release ZIP. Generation pipeline:
//
//  1. Load the template package and verify the caller has access.
//  2. Unmarshal def_json → file list (path, content, isBinary).
//  3. Find template.json in the file list → read Manifest.Vars.
//  4. Read the maker's config from the POST body; fill missing vars with "".
//  5. Walk every file under output/:
//     text file  → apply Go text/template substitution with config as data
//     binary     → copy verbatim
//  6. Return the assembled ZIP as application/zip.
//
// Request:  POST /api/v1/templates/:id/generate
//
//	{ "config": { "Port": "8081", "Message": "Hello!", "ModuleName": "..." } }
//
// Response: 200 OK  application/zip  Content-Disposition: attachment; filename="<n>.zip"
func (h *handler) handleGenerate(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	id := c.Param("id")

	// ── 1. Load and authorise ─────────────────────────────────────────────────
	pkg, err := store.GetTemplatePkg(id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, "template not found")
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, "internal error")
	}
	// Non-owner access: template must be public and ready.
	if pkg.UserID != claims.UserID {
		if pkg.Visibility != store.TemplatePkgVisibilityPublic ||
			pkg.Status != store.TemplatePkgStatusReady {
			return fail(c, http.StatusNotFound, "template not found")
		}
	}
	if pkg.Status != store.TemplatePkgStatusReady {
		return fail(c, http.StatusConflict,
			"template is not ready yet (status: "+pkg.Status+")")
	}

	// ── 2. Load def_json ──────────────────────────────────────────────────────
	raw, err := store.GetTemplatePkgDef(id)
	if err != nil {
		log.Printf("[templateapi/generate] load def pkgID=%s: %v", id, err)
		return fail(c, http.StatusInternalServerError, "could not load template definition")
	}

	// def_json shape written by parseTemplateFromZip in the worker:
	//   { "devices": [...], "files": [{"path":"...","content":"...","isBinary":false}, ...] }
	var def struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			IsBin   bool   `json:"isBinary"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		log.Printf("[templateapi/generate] unmarshal def pkgID=%s: %v", id, err)
		return fail(c, http.StatusInternalServerError, "malformed template definition")
	}

	// ── 3. Read template.json from the stored file list ───────────────────────
	// template.json is stored at path "template.json" (after stripGitHubRootDir).
	manifestVars := make(map[string]string) // varName → "StructName.fieldName"
	for _, f := range def.Files {
		if strings.EqualFold(f.Path, "template.json") {
			var manifest struct {
				Vars map[string]string `json:"vars"`
			}
			if err := json.Unmarshal([]byte(f.Content), &manifest); err != nil {
				log.Printf("[templateapi/generate] parse template.json pkgID=%s: %v", id, err)
			} else if manifest.Vars != nil {
				manifestVars = manifest.Vars
			}
			break
		}
	}

	// ── 4. Read config from request body ──────────────────────────────────────
	var body struct {
		Config map[string]string `json:"config"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	if body.Config == nil {
		body.Config = make(map[string]string)
	}

	// Build the data map for text/template substitution.
	// Every var declared in Manifest.Vars is pre-populated; missing keys get "".
	data := make(map[string]any, len(manifestVars))
	for varName := range manifestVars {
		if val, ok := body.Config[varName]; ok && val != "" {
			data[varName] = val
		} else {
			data[varName] = ""
		}
	}
	// Also accept extra keys the client sent (future vars, forward compat).
	for k, v := range body.Config {
		if _, declared := data[k]; !declared {
			data[k] = v
		}
	}

	// ── 5. Assemble the output ZIP ────────────────────────────────────────────
	const outputPrefix = "output/"
	var outBuf bytes.Buffer
	zw := zip.NewWriter(&outBuf)

	for _, f := range def.Files {
		if !strings.HasPrefix(f.Path, outputPrefix) {
			continue // skip devices/, template.json, etc.
		}
		relPath := strings.TrimPrefix(f.Path, outputPrefix)
		if relPath == "" {
			continue
		}

		var content []byte
		if f.IsBin || !genIsTextContent(f.Content) {
			content = []byte(f.Content)
		} else {
			content = genApplyTemplate(relPath, f.Content, data)
		}

		w, werr := zw.Create(relPath)
		if werr != nil {
			log.Printf("[templateapi/generate] zip create %q pkgID=%s: %v", relPath, id, werr)
			continue
		}
		if _, werr = w.Write(content); werr != nil {
			log.Printf("[templateapi/generate] zip write %q pkgID=%s: %v", relPath, id, werr)
		}
	}

	if err := zw.Close(); err != nil {
		return fail(c, http.StatusInternalServerError, "could not assemble output ZIP")
	}

	// ── 6. Send ZIP as download ───────────────────────────────────────────────
	safeName := sanitizeFilename(pkg.Name)
	if safeName == "" {
		safeName = "template"
	}
	c.Response().Header().Set("Content-Disposition",
		`attachment; filename="`+safeName+`.zip"`)
	return c.Blob(http.StatusOK, "application/zip", outBuf.Bytes())
}

// genIsTextContent returns true when s is valid UTF-8 with no null bytes.
// Used by handleGenerate to decide between template substitution and verbatim copy.
func genIsTextContent(s string) bool {
	return utf8.ValidString(s) && !strings.Contains(s, "\x00")
}

// genApplyTemplate runs Go text/template substitution on src with data as
// the template context. On any parse or execute error the raw src is returned
// so the output ZIP always contains the file intact.
func genApplyTemplate(relPath, src string, data map[string]any) []byte {
	tmpl, err := template.New(relPath).
		Funcs(template.FuncMap{}).
		Option("missingkey=zero").
		Parse(src)
	if err != nil {
		log.Printf("[templateapi/generate] template parse %q: %v — copied verbatim", relPath, err)
		return []byte(src)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("[templateapi/generate] template execute %q: %v — copied verbatim", relPath, err)
		return []byte(src)
	}
	return buf.Bytes()
}

// ── Response helpers ───────────────────────────────────────────────────────────

func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{
		"metadata": map[string]any{"status": 200},
		"data":     data,
	})
}

// ok200 wraps data in the standard SPA envelope (used for 201 Created responses
// where the handler explicitly sets the status code via c.JSON).
func ok200(data any) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"status": 200},
		"data":     data,
	}
}

func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{
		"metadata": map[string]any{"status": status, "error": msg},
		"data":     nil,
	})
}
