// handler/projectapi/handlers.go — Project and file management implementations.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Each handler:
//  1. Extracts and validates input.
//  2. Verifies ownership via GetProjectByIDAndUser before any mutation.
//  3. Coordinates DB operations (store) with filesystem operations (os).
//
// Filesystem layout:
//
//	{UserFilesDir}/{userID}/project/{typeSlug}/{projectID}/code/
//	{UserFilesDir}/{userID}/project/{typeSlug}/{projectID}/img/
//	{UserFilesDir}/{userID}/project/{typeSlug}/{projectID}/docs/
//
// Code version strategy:
//   - DB (project_code_versions): full history, used by diff tool
//   - Disk (code/): always the latest version; overwritten on every save
//
// Markdown docs strategy:
//   - POST /files/docs        → create a new .md file (fails if already exists).
//   - PUT  /files/docs/:name  → update an existing .md file (overwrites content).
//   - DELETE /files/docs/:name → delete a .md file (readme.md is protected).
//   - When the file being saved via PUT is readme.md, its YAML frontmatter is
//     parsed and the card fields are persisted to the projects table so the
//     marketplace feed can query them without reading disk files.
//
// Frontmatter format (readme.md):
//
//	---
//	title: My Sensor Board
//	image: /static/.../img/sensor.png
//	description: Short description shown in the feed card (max chars from settings).
//	keywords: i2c, sensor, proximity
//	category: Sensors
//	subcategory: Optical
//	---
package projectapi

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	cryptoauth "server/auth"
	"server/config"
	"server/handler/spaauth"
	"server/store"

	"github.com/labstack/echo/v4"
)

// ─── Constants ────────────────────────────────────────────────────────────────

// projectReadmeFilename is the name of the readme file that is automatically
// created in the docs/ section when a new project is created.
//
// This file can be edited through the Monaco editor (PUT /files/docs/:name)
// but it cannot be deleted (DELETE /files/docs/:name returns 403).
const projectReadmeFilename = "readme.md"

// ─── Lookup Tables ────────────────────────────────────────────────────────────

func handleListProgrammingLanguages(c echo.Context) error {
	langs, err := store.GetProgrammingLanguages()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, langs)
}

func handleListUILanguages(c echo.Context) error {
	langs, err := store.GetProjectUILanguages()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, langs)
}

// handleListReadmeConfig returns the full component taxonomy and the server-side
// limits needed by the readme.md Monaco editor in a single request.
//
// Response shape:
//
//	{
//	  "descriptionMaxChars": 500,
//	  "categories":    [{id, name, sortOrder}, ...],
//	  "subcategories": [{id, categoryId, name, sortOrder}, ...]
//	}
//
// The slash menu in the Monaco editor calls this endpoint when the user opens
// readme.md so that /category and /subcategory completions are available
// without additional round-trips.
func handleListReadmeConfig(c echo.Context) error {
	maxChars := store.GetSettingInt(store.SettingCardDescriptionMaxChars, 500)

	cats, err := store.ListCategories()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if cats == nil {
		cats = []*store.ProjectCategory{}
	}

	subs, err := store.ListAllSubcategories()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if subs == nil {
		subs = []*store.ProjectSubcategory{}
	}

	return ok(c, map[string]any{
		"descriptionMaxChars": maxChars,
		"categories":          cats,
		"subcategories":       subs,
	})
}

// handleListSubcategories returns subcategories for a given category ID.
// Query param: ?categoryId=xxx
// Returns all subcategories ordered by sort_order when categoryId is empty.
func handleListSubcategories(c echo.Context) error {
	categoryID := c.QueryParam("categoryId")
	var subs []*store.ProjectSubcategory
	var err error
	if categoryID == "" {
		subs, err = store.ListAllSubcategories()
	} else {
		subs, err = store.ListSubcategoriesByCategoryID(categoryID)
	}
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if subs == nil {
		subs = []*store.ProjectSubcategory{}
	}
	return ok(c, subs)
}

// ─── Project CRUD ─────────────────────────────────────────────────────────────

func handleListProjects(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projects, err := store.ListProjectsByUser(claims.UserID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if projects == nil {
		projects = []*store.Project{}
	}
	return ok(c, projects)
}

// handleCreateProject creates a project DB row, its directory structure, and
// an auto-generated readme.md in the docs/ directory.
//
// Required JSON fields:
//
//	name                   — project name (unique per user)
//	type                   — "custom_device" (only option for now)
//	visibility             — "public" or "private"
//	programmingLanguageId  — id from programming_languages table
//	uiLanguageId           — id from project_ui_languages table
func handleCreateProject(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var req struct {
		Name                  string `json:"name"`
		Type                  string `json:"type"`
		Visibility            string `json:"visibility"`
		ProgrammingLanguageID string `json:"programmingLanguageId"`
		UILanguageID          string `json:"uiLanguageId"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return fail(c, 400, "project name is required")
	}
	if len(req.Name) > 100 {
		return fail(c, 400, "project name must be 100 characters or fewer")
	}
	if strings.ContainsAny(req.Name, `/\:*?"<>|`) {
		return fail(c, 400, `project name must not contain: / \ : * ? " < > |`)
	}

	if req.Type == "" {
		req.Type = store.ProjectTypeCustomDevice
	}
	if req.Type != store.ProjectTypeCustomDevice {
		return fail(c, 400, fmt.Sprintf("unsupported project type: %s", req.Type))
	}

	// Visibility is always 'private' for projects, by design:
	//
	//   - Wizard is a tool for authoring the user's own device drafts.
	//   - Sharing devices happens via the `blackboxes` table, populated
	//     by the worker from GitHub releases — a separate ingestion
	//     path that lives outside the project lifecycle.
	//
	// We deliberately do NOT honour req.Visibility here: a client that
	// asks for 'public' is either out-of-date or attempting something
	// that the domain forbids. Silently coercing to 'private' keeps
	// older clients working without forcing them to change.
	//
	// The DB schema also has CHECK(visibility = 'private') as a
	// safety net — if this line ever regresses, the INSERT fails
	// loudly instead of producing a "private project" the user can't
	// share but the marketplace might one day try to surface.
	req.Visibility = store.ProjectVisibilityPrivate

	if req.ProgrammingLanguageID == "" {
		return fail(c, 400, "programmingLanguageId is required")
	}
	if err := store.ValidateProgrammingLanguageID(req.ProgrammingLanguageID); err != nil {
		return fail(c, 400, "invalid programmingLanguageId")
	}

	if req.UILanguageID == "" {
		return fail(c, 400, "uiLanguageId is required")
	}
	if err := store.ValidateUILanguageID(req.UILanguageID); err != nil {
		return fail(c, 400, "invalid uiLanguageId")
	}

	id, err := cryptoauth.NewID()
	if err != nil {
		c.Logger().Errorf("[projectapi/create] cryptoauth.NewID failed for user %s: %v",
			claims.UserID, err)
		return fail(c, 500, "internal error")
	}

	p := &store.Project{
		ID:                    id,
		UserID:                claims.UserID,
		Name:                  req.Name,
		Type:                  req.Type,
		Visibility:            req.Visibility,
		ProgrammingLanguageID: req.ProgrammingLanguageID,
		UILanguageID:          req.UILanguageID,
	}
	if err := store.CreateProject(p); err != nil {
		if err == store.ErrConflict {
			return fail(c, 409, "a project with this name already exists")
		}
		c.Logger().Errorf("[projectapi/create] store.CreateProject failed "+
			"(user=%s name=%q lang=%q uiLang=%q type=%q): %v",
			claims.UserID, req.Name, req.ProgrammingLanguageID,
			req.UILanguageID, req.Type, err)
		return fail(c, 500, "internal error")
	}

	cfg := config.Get()
	basePath := projectBasePath(cfg, claims.UserID, p.Type, p.ID)
	for _, dir := range []string{
		filepath.Join(basePath, store.ProjectFileSectionCode),
		filepath.Join(basePath, store.ProjectFileSectionImg),
		filepath.Join(basePath, store.ProjectFileSectionDocs),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			_ = store.DeleteProject(p.ID, claims.UserID)
			return fail(c, 500, "could not create project directory")
		}
	}

	// Auto-create readme.md in docs/. This file is the project's primary
	// documentation and cannot be deleted — only edited via the Monaco editor.
	readmePath := filepath.Join(basePath, store.ProjectFileSectionDocs, projectReadmeFilename)
	if writeErr := os.WriteFile(readmePath, []byte(buildDefaultReadme(p.Name)), 0644); writeErr != nil {
		c.Logger().Errorf("[projectapi] failed to create readme.md for project %s: %v", p.ID, writeErr)
	}

	created, err := store.GetProjectByID(p.ID)
	if err != nil {
		c.Logger().Errorf("[projectapi/create] GetProjectByID failed after insert (id=%s user=%s): %v",
			p.ID, claims.UserID, err)
		return fail(c, 500, "internal error")
	}

	// Log a feed event so the project appears in followers' Following tabs.
	// Non-fatal: a failed log never blocks the response.
	if p.Visibility == store.ProjectVisibilityPublic {
		if logErr := store.LogFeedEvent(p.ID, claims.UserID, store.FeedEventCreated); logErr != nil {
			c.Logger().Warnf("[projectapi] LogFeedEvent create %s: %v", p.ID, logErr)
		}
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"metadata": map[string]any{"status": 201},
		"data":     created,
	})
}

// handleUpdateProject updates the mutable metadata of a project: name,
// visibility and the three publishing flags (publish_to_feed,
// publish_to_search, ready_to_use).
//
// Publishing flags are forced to false when visibility is "private" — the
// handler enforces this rule before calling the store so the DB stays
// consistent even if a client sends stale data.
//
// Required JSON fields:
//
//	name            — project name (unique per user, ≤ 100 chars)
//	visibility      — "public" or "private"
//	publishToFeed   — bool; always false for private projects
//	publishToSearch — bool; always false for private projects
//	readyToUse      — bool; always false for private projects
func handleUpdateProject(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Name            string `json:"name"`
		Visibility      string `json:"visibility"`
		PublishToFeed   bool   `json:"publishToFeed"`
		PublishToSearch bool   `json:"publishToSearch"`
		ReadyToUse      bool   `json:"readyToUse"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return fail(c, 400, "project name is required")
	}
	if len(req.Name) > 100 {
		return fail(c, 400, "project name must be 100 characters or fewer")
	}
	if strings.ContainsAny(req.Name, `/\:*?"<>|`) {
		return fail(c, 400, `project name must not contain: / \ : * ? " < > |`)
	}

	// Visibility is always 'private' for projects, by design — see
	// handleCreateProject for the full rationale. The PUT endpoint
	// accepts the field for backward compatibility (older clients
	// still send it) but ignores whatever value comes in. This
	// silently coerces a stale "public" request to "private"
	// instead of returning an error: the server's invariant holds,
	// and the client will see the new visibility on the next read.
	req.Visibility = store.ProjectVisibilityPrivate

	// Publishing flags are meaningless — and potentially misleading — on a
	// private project. Force them to false regardless of what the client sent.
	req.PublishToFeed = false
	req.PublishToSearch = false
	req.ReadyToUse = false

	// Verify ownership before any mutation.
	_, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	upd := &store.ProjectUpdate{
		Name:            req.Name,
		Visibility:      req.Visibility,
		PublishToFeed:   req.PublishToFeed,
		PublishToSearch: req.PublishToSearch,
		ReadyToUse:      req.ReadyToUse,
	}
	if err := store.UpdateProject(projectID, claims.UserID, upd); err != nil {
		if err == store.ErrConflict {
			return fail(c, 409, "a project with this name already exists")
		}
		return fail(c, 500, "internal error")
	}

	// Return the full updated project so the frontend can refresh its state.
	updated, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	return ok(c, updated)
}

// handleDeleteProject permanently removes the project from the DB and from disk.
func handleDeleteProject(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	if err := store.DeleteProject(p.ID, claims.UserID); err != nil {
		return fail(c, 500, "internal error")
	}

	cfg := config.Get()
	basePath := projectBasePath(cfg, claims.UserID, p.Type, p.ID)
	if err := os.RemoveAll(basePath); err != nil {
		c.Logger().Errorf("[projectapi] failed to remove project dir %s: %v", basePath, err)
	}

	return ok(c, map[string]bool{"deleted": true})
}

// ─── File Listing ─────────────────────────────────────────────────────────────

func handleListProjectFiles(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	cfg := config.Get()
	basePath := projectBasePath(cfg, claims.UserID, p.Type, p.ID)

	result := &store.ProjectFiles{
		Code: []*store.ProjectFile{},
		Img:  []*store.ProjectFile{},
		Docs: []*store.ProjectFile{},
	}

	for _, s := range []struct {
		section string
		target  *[]*store.ProjectFile
	}{
		{store.ProjectFileSectionCode, &result.Code},
		{store.ProjectFileSectionImg, &result.Img},
		{store.ProjectFileSectionDocs, &result.Docs},
	} {
		entries, err := os.ReadDir(filepath.Join(basePath, s.section))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			url := fmt.Sprintf("/static/%s/project/%s/%s/%s/%s",
				claims.UserID, store.ProjectTypeSlug(p.Type), p.ID,
				s.section, entry.Name())

			protected := s.section == store.ProjectFileSectionDocs &&
				entry.Name() == projectReadmeFilename

			*s.target = append(*s.target, &store.ProjectFile{
				Name:      entry.Name(),
				URL:       url,
				Size:      info.Size(),
				Section:   s.section,
				Protected: protected,
			})
		}
	}

	return ok(c, result)
}

// ─── Code File: Read ──────────────────────────────────────────────────────────

func handleGetCodeFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	versions, _ := store.ListProjectCodeVersions(projectID)
	if versions == nil {
		versions = []*store.ProjectCodeVersion{}
	}

	latest, err := store.GetLatestProjectCodeVersion(projectID)
	if err == nil {
		return ok(c, map[string]any{
			"source":      latest.Source,
			"version":     latest.Version,
			"filename":    latest.Filename,
			"lastParseOk": latest.LastParseOk,
			"versions":    versions,
		})
	}
	if err != store.ErrNotFound {
		return fail(c, 500, "internal error")
	}

	cfg := config.Get()
	codeDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionCode)
	entries, readErr := os.ReadDir(codeDir)
	if readErr != nil || len(entries) == 0 {
		return ok(c, map[string]any{
			"source":   "",
			"version":  0,
			"filename": "",
			"versions": versions,
		})
	}
	content, readErr := os.ReadFile(filepath.Join(codeDir, entries[0].Name()))
	if readErr != nil {
		return fail(c, 500, "could not read code file")
	}
	return ok(c, map[string]any{
		"source":   string(content),
		"version":  0,
		"filename": entries[0].Name(),
		"versions": versions,
	})
}

// ─── Code File: Save from Monaco ─────────────────────────────────────────────

func handleSaveCodeVersion(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Source   string `json:"source"`
		Filename string `json:"filename"`
		// LastParseOk records whether the wizard's /parse endpoint
		// returned a successful BlackBoxDef for this exact source at
		// the moment the user clicked Save. The IDE uses this on
		// project open to decide whether to silently re-parse and
		// populate the Preview tab without user intervention.
		// Defaults to false when the field is missing — older
		// clients keep working, just without the silent-rehydrate
		// optimisation.
		LastParseOk bool `json:"lastParseOk,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	// Trim only the filename — the source must round-trip the user's
	// bytes verbatim. In particular, gofmt-formatted Go files always
	// end with a trailing '\n'; mutating req.Source via TrimSpace
	// would silently strip that newline on every save, so the next
	// open shows a one-byte diff against the user's local copy and a
	// re-format produces a perpetual "modified" state. The empty-
	// check below uses TrimSpace just for the test, not as a mutation.
	req.Filename = strings.TrimSpace(req.Filename)

	if strings.TrimSpace(req.Source) == "" {
		return fail(c, 400, "source is required")
	}
	if req.Filename == "" {
		req.Filename = "main.go"
	}
	if !strings.HasSuffix(strings.ToLower(req.Filename), ".go") {
		return fail(c, 400, "filename must have a .go extension")
	}
	if strings.ContainsAny(req.Filename, `/\:*?"<>|`) {
		return fail(c, 400, "invalid filename")
	}

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	nextVer, err := store.GetNextCodeVersionNumber(projectID)
	if err != nil {
		return fail(c, 500, "internal error")
	}

	vID, err := cryptoauth.NewID()
	if err != nil {
		return fail(c, 500, "internal error")
	}

	v := &store.ProjectCodeVersion{
		ID:          vID,
		ProjectID:   projectID,
		UserID:      claims.UserID,
		Version:     nextVer,
		Filename:    req.Filename,
		Source:      req.Source,
		LastParseOk: req.LastParseOk,
	}
	if err := store.CreateProjectCodeVersion(v); err != nil {
		if err == store.ErrConflict {
			return fail(c, 409, "version conflict — please retry")
		}
		return fail(c, 500, "could not save version")
	}

	cfg := config.Get()
	codeDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionCode)
	if mkErr := os.MkdirAll(codeDir, 0755); mkErr != nil {
		c.Logger().Errorf("[projectapi] failed to create code dir: %v", mkErr)
	}
	if err := clearDirectory(codeDir); err != nil {
		c.Logger().Errorf("[projectapi] failed to clear code dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codeDir, req.Filename), []byte(req.Source), 0644); err != nil {
		c.Logger().Errorf("[projectapi] failed to write code file to disk: %v", err)
	}

	// Log a code-update feed event for public projects.
	if p.Visibility == store.ProjectVisibilityPublic {
		if logErr := store.LogFeedEvent(projectID, claims.UserID, store.FeedEventCodeUpdated); logErr != nil {
			c.Logger().Warnf("[projectapi] LogFeedEvent code %s: %v", projectID, logErr)
		}
	}

	// Promotion: the working-source backup is now redundant — the
	// content is preserved as a real version. Drop the backup so the
	// frontend's red "pending" state clears immediately on next load.
	// Failure to delete is non-fatal: the next save will overwrite it,
	// or the next backup-empty trigger will clean it up.
	if err := store.DeleteProjectBackup(projectID); err != nil {
		c.Logger().Warnf("[projectapi] DeleteProjectBackup %s after save: %v", projectID, err)
	}

	return ok(c, map[string]any{
		"id":          v.ID,
		"version":     v.Version,
		"filename":    v.Filename,
		"lastParseOk": v.LastParseOk,
	})
}

func handleListCodeVersions(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	_, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	versions, err := store.ListProjectCodeVersions(projectID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	if versions == nil {
		versions = []*store.ProjectCodeVersion{}
	}
	return ok(c, versions)
}

// ─── Code File: Upload (multipart) ───────────────────────────────────────────

func handleUploadCodeFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return fail(c, 400, "file field is required")
	}
	if !strings.HasSuffix(strings.ToLower(fh.Filename), ".go") {
		return fail(c, 400, "only .go files are allowed in the code section")
	}

	cfg := config.Get()
	codeDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionCode)
	if err := os.MkdirAll(codeDir, 0755); err != nil {
		return fail(c, 500, "could not create code directory")
	}
	if err := clearDirectory(codeDir); err != nil {
		return fail(c, 500, "could not clear existing code file")
	}
	if err := saveUploadedFile(fh, filepath.Join(codeDir, filepath.Base(fh.Filename))); err != nil {
		return fail(c, 500, "could not save file")
	}

	return ok(c, map[string]string{
		"name": filepath.Base(fh.Filename),
		"url": fmt.Sprintf("/static/%s/project/%s/%s/%s/%s",
			claims.UserID, store.ProjectTypeSlug(p.Type), p.ID,
			store.ProjectFileSectionCode, filepath.Base(fh.Filename)),
	})
}

func handleDeleteCodeFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	cfg := config.Get()
	codeDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionCode)
	if err := clearDirectory(codeDir); err != nil {
		return fail(c, 500, "could not delete code file")
	}
	return ok(c, map[string]bool{"deleted": true})
}

func handleRenameCodeFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		NewName string `json:"newName"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}
	req.NewName = strings.TrimSpace(req.NewName)
	if req.NewName == "" {
		return fail(c, 400, "newName is required")
	}
	if !strings.HasSuffix(strings.ToLower(req.NewName), ".go") {
		return fail(c, 400, "code file must have a .go extension")
	}
	if strings.ContainsAny(req.NewName, `/\:*?"<>|`) {
		return fail(c, 400, "invalid file name")
	}

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	cfg := config.Get()
	codeDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionCode)
	entries, err := os.ReadDir(codeDir)
	if err != nil || len(entries) == 0 {
		return fail(c, 404, "no code file found in this project")
	}
	if err := os.Rename(filepath.Join(codeDir, entries[0].Name()), filepath.Join(codeDir, req.NewName)); err != nil {
		return fail(c, 500, "could not rename file")
	}
	return ok(c, map[string]string{"name": req.NewName})
}

// ─── Image Files ──────────────────────────────────────────────────────────────

func handleUploadImage(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return fail(c, 400, "file field is required")
	}

	lower := strings.ToLower(fh.Filename)
	if !strings.HasSuffix(lower, ".png") &&
		!strings.HasSuffix(lower, ".jpg") &&
		!strings.HasSuffix(lower, ".jpeg") &&
		!strings.HasSuffix(lower, ".gif") &&
		!strings.HasSuffix(lower, ".webp") {
		return fail(c, 400, "only PNG, JPG, GIF and WebP images are allowed")
	}
	if strings.ContainsAny(fh.Filename, `/\:*?"<>|`) {
		return fail(c, 400, "invalid file name")
	}

	cfg := config.Get()
	imgDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionImg)
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		return fail(c, 500, "could not create image directory")
	}
	destPath := filepath.Join(imgDir, filepath.Base(fh.Filename))
	if _, err := os.Stat(destPath); err == nil {
		return fail(c, 409, "an image with this name already exists in this project")
	}
	if err := saveUploadedFile(fh, destPath); err != nil {
		return fail(c, 500, "could not save image")
	}

	url := fmt.Sprintf("/static/%s/project/%s/%s/%s/%s",
		claims.UserID, store.ProjectTypeSlug(p.Type), p.ID,
		store.ProjectFileSectionImg, filepath.Base(fh.Filename))

	return ok(c, map[string]string{
		"name":         filepath.Base(fh.Filename),
		"url":          url,
		"markdownLink": fmt.Sprintf("![%s](%s)", filepath.Base(fh.Filename), url),
	})
}

func handleDeleteImage(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	filename := filepath.Base(c.Param("name"))

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	imgPath := filepath.Join(
		projectBasePath(config.Get(), claims.UserID, p.Type, p.ID),
		store.ProjectFileSectionImg, filename,
	)
	if err := os.Remove(imgPath); err != nil {
		if os.IsNotExist(err) {
			return fail(c, 404, "image not found")
		}
		return fail(c, 500, "could not delete image")
	}
	return ok(c, map[string]bool{"deleted": true})
}

// ─── Markdown Docs ────────────────────────────────────────────────────────────

func handleUploadMarkdown(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return fail(c, 400, "file field is required")
	}
	if !strings.HasSuffix(strings.ToLower(fh.Filename), ".md") {
		return fail(c, 400, "only .md files are allowed in the docs section")
	}
	if strings.ContainsAny(fh.Filename, `/\:*?"<>|`) {
		return fail(c, 400, "invalid file name")
	}

	cfg := config.Get()
	docsDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionDocs)
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return fail(c, 500, "could not create docs directory")
	}
	destPath := filepath.Join(docsDir, filepath.Base(fh.Filename))
	if _, err := os.Stat(destPath); err == nil {
		return fail(c, 409, "a file with this name already exists in docs")
	}
	if err := saveUploadedFile(fh, destPath); err != nil {
		return fail(c, 500, "could not save markdown file")
	}

	return ok(c, map[string]string{
		"name": filepath.Base(fh.Filename),
		"url": fmt.Sprintf("/static/%s/project/%s/%s/%s/%s",
			claims.UserID, store.ProjectTypeSlug(p.Type), p.ID,
			store.ProjectFileSectionDocs, filepath.Base(fh.Filename)),
	})
}

// handleUpdateMarkdown replaces the content of an existing .md file in docs/.
//
// When the file being updated is readme.md, the YAML frontmatter is parsed and
// the extracted card fields (title, image, description, keywords, category,
// subcategory) are persisted to the projects table. If the description exceeds
// the limit set in project_settings, it is silently truncated and a "warning"
// field is included in the response so the Monaco editor can display a notice.
//
// Returns 404 if the file does not yet exist (use POST to create it first).
//
// Multipart field: "file"
func handleUpdateMarkdown(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	filename := filepath.Base(c.Param("name"))

	if !strings.HasSuffix(strings.ToLower(filename), ".md") {
		return fail(c, 400, "only .md files are allowed in the docs section")
	}
	if strings.ContainsAny(filename, `/\:*?"<>|`) {
		return fail(c, 400, "invalid file name")
	}

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return fail(c, 400, "file field is required")
	}

	cfg := config.Get()
	docsDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionDocs)
	destPath := filepath.Join(docsDir, filename)

	if _, statErr := os.Stat(destPath); os.IsNotExist(statErr) {
		return fail(c, 404, "file not found — use POST to create a new markdown file")
	}

	// Read content into memory so we can parse frontmatter before writing.
	src, err := fh.Open()
	if err != nil {
		return fail(c, 500, "could not read uploaded file")
	}
	content, err := io.ReadAll(src)
	src.Close()
	if err != nil {
		return fail(c, 500, "could not read uploaded file")
	}

	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fail(c, 500, "could not update markdown file")
	}

	// For readme.md, parse the YAML frontmatter and update the project card
	// fields in the DB. This makes the data available to the feed and search
	// endpoints without reading disk files.
	var descTruncated bool
	if filename == projectReadmeFilename {
		descTruncated = applyFrontmatterToProject(c, projectID, string(content))
		// Log a readme-update feed event for public projects.
		if p.Visibility == store.ProjectVisibilityPublic {
			if logErr := store.LogFeedEvent(projectID, claims.UserID, store.FeedEventReadmeUpdated); logErr != nil {
				c.Logger().Warnf("[projectapi] LogFeedEvent readme %s: %v", projectID, logErr)
			}
		}
	}

	resp := map[string]any{
		"name": filename,
		"url": fmt.Sprintf("/static/%s/project/%s/%s/%s/%s",
			claims.UserID, store.ProjectTypeSlug(p.Type), p.ID,
			store.ProjectFileSectionDocs, filename),
	}
	if descTruncated {
		resp["warning"] = fmt.Sprintf(
			"description was truncated to the maximum of %d characters",
			store.GetSettingInt(store.SettingCardDescriptionMaxChars, 500),
		)
	}
	return ok(c, resp)
}

// handleDeleteMarkdown removes a single .md file from docs/.
// The auto-generated readme.md is protected and cannot be deleted.
func handleDeleteMarkdown(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")
	filename := filepath.Base(c.Param("name"))

	if filename == projectReadmeFilename {
		return fail(c, 403, "readme.md is automatically generated and cannot be deleted")
	}

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	docsPath := filepath.Join(
		projectBasePath(config.Get(), claims.UserID, p.Type, p.ID),
		store.ProjectFileSectionDocs, filename,
	)
	if err := os.Remove(docsPath); err != nil {
		if os.IsNotExist(err) {
			return fail(c, 404, "file not found")
		}
		return fail(c, 500, "could not delete file")
	}
	return ok(c, map[string]bool{"deleted": true})
}

// ─── Frontmatter ─────────────────────────────────────────────────────────────

// parseFrontmatter extracts the YAML front-matter block from a Markdown document.
//
// The expected format is a block delimited by "---" on its own line at the very
// start of the document:
//
//	---
//	title: My Sensor
//	image: /static/.../img/sensor.png
//	description: Short text for the feed card.
//	keywords: i2c, sensor, proximity
//	category: Sensors
//	subcategory: Optical
//	---
//
// The function returns a map of lowercase key → trimmed value. Lines that do not
// contain a colon, or lines starting with "#", are silently ignored. The body of
// the document (after the closing "---") is not returned.
func parseFrontmatter(content string) map[string]string {
	result := make(map[string]string)

	if !strings.HasPrefix(content, "---") {
		return result
	}

	// Skip the opening "---" and the newline that follows.
	rest := content[3:]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}

	// Find the closing "---" delimiter.
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return result
	}
	block := rest[:end]

	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
		val := strings.TrimSpace(line[colonIdx+1:])
		result[key] = val
	}
	return result
}

// applyFrontmatterToProject parses readme.md content, resolves taxonomy IDs,
// truncates the description to the configured limit, and calls UpdateProjectCard.
// Returns true if the description was truncated.
//
// Any error from the store is logged but not returned — a failed card update
// must never block the file save itself.
func applyFrontmatterToProject(c echo.Context, projectID, content string) bool {
	fm := parseFrontmatter(content)

	maxChars := store.GetSettingInt(store.SettingCardDescriptionMaxChars, 500)
	desc := fm["description"]
	truncated := false
	if utf8.RuneCountInString(desc) > maxChars {
		runes := []rune(desc)
		desc = string(runes[:maxChars])
		truncated = true
	}

	// Resolve "category: Sensors" → category ID in the taxonomy table.
	categoryID := ""
	if catName := fm["category"]; catName != "" {
		cat, err := store.GetCategoryByName(catName)
		if err == nil {
			categoryID = cat.ID
		} else {
			c.Logger().Warnf("[projectapi] unknown category %q for project %s", catName, projectID)
		}
	}

	// Resolve "subcategory: Optical" → subcategory ID, scoped to the resolved category.
	subcategoryID := ""
	if subName := fm["subcategory"]; subName != "" && categoryID != "" {
		sub, err := store.GetSubcategoryByNameAndCategoryID(subName, categoryID)
		if err == nil {
			subcategoryID = sub.ID
		} else {
			c.Logger().Warnf("[projectapi] unknown subcategory %q in category %s for project %s",
				subName, categoryID, projectID)
		}
	}

	card := &store.ProjectCard{
		CardTitle:       fm["title"],
		CardImage:       fm["image"],
		CardDescription: desc,
		CardKeywords:    fm["keywords"],
		CategoryID:      categoryID,
		SubcategoryID:   subcategoryID,
	}
	if err := store.UpdateProjectCard(projectID, card); err != nil {
		c.Logger().Errorf("[projectapi] UpdateProjectCard for %s: %v", projectID, err)
	}

	return truncated
}

// ─── Path helpers ─────────────────────────────────────────────────────────────

func projectBasePath(cfg *config.Config, userID, projectType, projectID string) string {
	return filepath.Join(
		cfg.UserFilesDir, userID, "project",
		store.ProjectTypeSlug(projectType), projectID,
	)
}

// buildDefaultReadme generates the initial content for the auto-created readme.md.
// The YAML frontmatter block at the top is pre-populated with placeholder values
// so the Monaco editor can show a meaningful card preview on first open.
func buildDefaultReadme(projectName string) string {
	return fmt.Sprintf(`---
title: %s
image:
description: Describe this component in one or two sentences.
keywords: keyword1, keyword2
category:
subcategory:
---

# %s

## Overview

Describe your device or project here.
What problem does it solve? How is it used?

## Connections

| Pin / Port | Type   | Description    |
|------------|--------|----------------|
|            |        |                |

## Settings

| Setting | Default | Options | Description |
|---------|---------|---------|-------------|
|         |         |         |             |

## Usage Example

Describe how to connect and configure this device.

## Notes

Add any additional notes, limitations, or references here.
`, projectName, projectName)
}

// ─── File helpers ─────────────────────────────────────────────────────────────

func saveUploadedFile(fh *multipart.FileHeader, destPath string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// clearDirectory removes all files in dir without removing dir itself.
func clearDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ─── Backup handlers ──────────────────────────────────────────────────────────
//
// The working-source backup is a single-slot snapshot of the project's
// editor content, separate from the versioned Save history. The frontend
// posts here on tab switches, wizard edits, and debounced Monaco edits
// so the user can recover their work after closing the browser without
// having clicked Save.
//
// Recovery semantics: on project open, the frontend GETs both the latest
// version and the backup (if any), compares timestamps, and uses
// whichever is newer. If the backup is newer, the Save button starts in
// the red "pending" state to remind the user there's unsaved work.

// handleGetCodeBackup returns the backup row for the project, or 404 if
// none exists. 404 is the normal "no unsaved work" response — clients
// fall back to the latest saved version.
func handleGetCodeBackup(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	// Ownership check — same pattern as the other code endpoints.
	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	b, err := store.GetProjectBackup(projectID)
	if err != nil {
		if err == store.ErrNoBackup {
			return fail(c, 404, "no backup")
		}
		return fail(c, 500, "internal error")
	}

	return ok(c, map[string]any{
		"source":    b.Source,
		"filename":  b.Filename,
		"updatedAt": b.UpdatedAt,
	})
}

// handleSaveCodeBackup overwrites the backup with the supplied source.
// Empty source (after whitespace trimming, done in the store layer)
// transparently deletes the row — see store.SaveProjectBackup for the
// rule. Returns 200 in either case so the client doesn't need to
// distinguish saved-vs-cleared.
func handleSaveCodeBackup(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		Source   string `json:"source"`
		Filename string `json:"filename"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	// Filename is optional — when absent the backup uses the empty
	// string and the frontend recovery path falls back to its own
	// default (usually "main.go"). We DON'T validate the .go suffix
	// here because the backup may legitimately store transient
	// non-.go content the user is mid-edit on.
	req.Filename = strings.TrimSpace(req.Filename)

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	if err := store.SaveProjectBackup(projectID, req.Source, req.Filename); err != nil {
		return fail(c, 500, "could not save backup")
	}

	return ok(c, map[string]any{"saved": true})
}

// handleDeleteCodeBackup explicitly clears the backup. The frontend
// uses this from "Clear parse" or any other operation that wants to
// promote the editor to a clean state. Idempotent: 200 even if no row
// existed.
func handleDeleteCodeBackup(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	if err := store.DeleteProjectBackup(projectID); err != nil {
		return fail(c, 500, "could not delete backup")
	}

	return ok(c, map[string]any{"deleted": true})
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
