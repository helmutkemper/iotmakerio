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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

	// The database snapshot is the single source of truth; the on-disk
	// code section is a derived mirror for /static viewing. The old
	// read-from-disk fallback served databases that predated code
	// versions — a population that no longer exists (pre-release, no
	// legacy data by decision, 2026-07) — so "no version yet" is simply
	// an empty snapshot.
	//
	// Português: O snapshot no banco é a única fonte de verdade; o disco
	// é espelho derivado. O fallback de leitura do disco atendia bancos
	// pré-versionamento — população que não existe mais (pré-release, sem
	// legado): "sem versão" é snapshot vazio.
	latest, err := store.GetLatestProjectCodeVersion(projectID)
	if err == nil {
		return ok(c, map[string]any{
			"files":       latest.Files,
			"version":     latest.Version,
			"lastParseOk": latest.LastParseOk,
			"versions":    versions,
		})
	}
	if err != store.ErrNotFound {
		return fail(c, 500, "internal error")
	}
	_ = p // ownership already verified above; the mirror is not consulted
	return ok(c, map[string]any{
		"files":    []store.CodeFileEntry{},
		"version":  0,
		"versions": versions,
	})
}

// ─── Code File: Save from Monaco ─────────────────────────────────────────────

func handleSaveCodeVersion(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	projectID := c.Param("id")

	var req struct {
		// Files is the complete snapshot being saved — every open tab,
		// in tab order. A save is atomic over the SET: there is no
		// per-file save, exactly as there is no per-file version.
		//
		// Português: O snapshot completo sendo salvo — toda aba, na
		// ordem das abas. Save é atômico sobre o CONJUNTO.
		Files []store.CodeFileEntry `json:"files"`
		// LastParseOk records whether the wizard's /parse endpoint
		// returned a successful BlackBoxDef for this exact snapshot at
		// the moment the user clicked Save. The IDE uses this on
		// project open to decide whether to silently re-parse and
		// populate the Preview tab without user intervention.
		LastParseOk bool `json:"lastParseOk,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	// Contents round-trip the user's bytes VERBATIM — gofmt-formatted Go
	// files end with a trailing '\n'; trimming would produce a perpetual
	// one-byte "modified" state on every reopen. Only PATHS are trimmed.
	// Validation of the set (count, path spelling, extension-per-language,
	// uniqueness) lives in validateCodeFileSet so the upload and rename
	// endpoints enforce the exact same contract.
	//
	// Português: Conteúdo viaja VERBATIM (só caminhos são trimados); a
	// validação do conjunto mora em validateCodeFileSet para upload e
	// rename imporem o MESMO contrato.
	for i := range req.Files {
		req.Files[i].Path = strings.TrimSpace(req.Files[i].Path)
	}
	if msg := validateCodeFileSet(req.Files, p.ProgrammingLanguageID); msg != "" {
		return fail(c, 400, msg)
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
		Files:       req.Files,
		LastParseOk: req.LastParseOk,
	}
	if err := store.CreateProjectCodeVersion(v); err != nil {
		if err == store.ErrConflict {
			return fail(c, 409, "version conflict — please retry")
		}
		return fail(c, 500, "could not save version")
	}

	// Mirror the whole snapshot to the disk code section (derived data for
	// /static viewing — the database row set is the source of truth).
	// Mirror failures are logged, never fatal: the version IS saved.
	//
	// Português: Espelha o snapshot inteiro no disco (dado derivado; o
	// banco é a verdade). Falha de espelho loga e segue: a versão FOI salva.
	cfg := config.Get()
	codeDir := filepath.Join(projectBasePath(cfg, claims.UserID, p.Type, p.ID), store.ProjectFileSectionCode)
	if mkErr := os.MkdirAll(codeDir, 0755); mkErr != nil {
		c.Logger().Errorf("[projectapi] failed to create code dir: %v", mkErr)
	}
	if err := clearDirectory(codeDir); err != nil {
		c.Logger().Errorf("[projectapi] failed to clear code dir: %v", err)
	}
	mirrorCodeSnapshot(c, codeDir, req.Files)

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
		"files":       v.Files,
		"lastParseOk": v.LastParseOk,
	})
}

// ─── Code snapshot: shared contract ──────────────────────────────────────────

// maxCodeFiles caps a snapshot's file count. Sixteen is generous for a
// device project (a real specialist project is api.h + a handful of .c) and
// small enough that the wizard's tab strip, the version-history payload and
// the export ZIP all stay human-scaled. Raising it is a one-line change —
// the cap exists to make "someone pasted a whole repository" a 400, not a
// megabyte of tabs.
//
// Português: Teto de arquivos por snapshot. Dezesseis é folgado para um
// projeto de device e pequeno o bastante para abas, histórico e ZIP
// continuarem humanos; o teto transforma "colaram um repositório" em 400.
const maxCodeFiles = 16

// codeFileSegment validates one path segment: starts with a letter, digit
// or underscore (no hidden dot-files, no "-rf" lookalikes), then letters,
// digits, dot, underscore or hyphen.
var codeFileSegment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)

// Asset extensions (the unified asset model — docs/tasks/
// ASSETS_UNIFIED_MODEL.md): a WHITELIST, not "everything that isn't
// source" — marketplace ZIPs must not smuggle .so/.exe (supply chain).
// Text assets are editable tabs; binary assets travel base64 (the
// snapshot is JSON — raw binary breaks UTF-8) and render as placeholder
// tabs. Both classes are shared by Go and C projects alike.
//
// Português: Extensões de asset — WHITELIST, não "tudo que não é fonte"
// (supply chain). Texto = aba editável; binário = base64 (snapshot é
// JSON) com aba placeholder. As duas classes valem para Go e C.
var textAssetExts = map[string]bool{
	".html": true, ".htm": true, ".tmpl": true, ".txt": true, ".json": true,
	".csv": true, ".svg": true, ".md": true, ".css": true,
}
var binaryAssetExts = map[string]bool{
	".gif": true, ".png": true, ".jpg": true, ".jpeg": true,
}

// Size caps: assets live inside the SQLite snapshot — a video is not a
// device asset. Per-asset cap is on DECODED bytes (base64 inflates 4/3);
// the project cap sums every file's stored content.
const (
	maxAssetBytes    = 512 << 10 // 512 KB decoded, per asset
	maxSnapshotBytes = 4 << 20   // 4 MB stored, whole project
)

// validateCodeFileSet enforces the snapshot contract shared by save, upload
// and rename. Returns "" when valid, else the 400 message.
//
// The rules, and why each exists:
//
//   - 1..maxCodeFiles entries — an empty save is a no-op the UI should not
//     ship; the cap is explained above.
//   - every path: relative, '/'-separated, ≤160 chars, ≤4 segments, each
//     segment matching codeFileSegment. No "..", no absolute root, no
//     backslash — these are ZIP keys and #include operands downstream, and
//     the emitter's zip-slip guard should stay unreachable.
//   - paths unique CASE-INSENSITIVELY — the maker may unzip on a
//     case-folding filesystem (Windows, macOS default), where Util.c and
//     util.c silently become one file.
//   - extension rules: SOURCE by language (go → .go; c → .c/.h with at
//     least one .c — a header-only device has no definitions for the
//     generated header to promise), plus the shared ASSET whitelist
//     (textAssetExts editable, binaryAssetExts base64-only). Anything
//     else is rejected by name. (The GoMF-era "exactly one .go" rule is
//     gone — this paragraph was stale after GoMF and was corrected with
//     the asset slice, 2026-07-08.)
//   - encoding: "" or "base64". Binary assets REQUIRE base64 (raw bytes
//     break the JSON transport); everything else FORBIDS it (a source
//     file arriving base64 would hide from review and diff); base64
//     content must actually decode.
//   - size caps: ≤512 KB decoded per asset; ≤4 MB stored per snapshot.
//   - at least one SOURCE file with non-blank content — assets alone do
//     not make a device.
//
// Português: Contrato do snapshot compartilhado por save, upload e rename.
// Caminho relativo simples; unicidade case-insensitive; FONTE por
// linguagem (go = .go; c = .c/.h com ≥1 .c) + whitelist de ASSETS
// compartilhada (texto editável; binário só-base64). Encoding: binário
// EXIGE base64, o resto PROÍBE, e base64 tem que decodificar. Tetos:
// ≤512 KB por asset (decodificado), ≤4 MB por snapshot. ≥1 arquivo
// FONTE com conteúdo — assets sozinhos não fazem um device.
func validateCodeFileSet(files []store.CodeFileEntry, languageID string) string {
	if len(files) == 0 {
		return "files is required (at least one file)"
	}
	if len(files) > maxCodeFiles {
		return fmt.Sprintf("too many files: %d (max %d)", len(files), maxCodeFiles)
	}

	lang := strings.ToLower(strings.TrimSpace(languageID))
	isGo := lang == "" || lang == "go" || lang == "golang"

	seen := make(map[string]bool, len(files))
	hasSourceContent := false
	hasC := false
	totalStored := 0
	for _, f := range files {
		if f.Path == "" {
			return "every file needs a path"
		}
		if len(f.Path) > 160 {
			return fmt.Sprintf("path too long: %q (max 160)", f.Path)
		}
		if strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, `\`) {
			return fmt.Sprintf("invalid path %q: must be relative with '/' separators", f.Path)
		}
		segs := strings.Split(f.Path, "/")
		if len(segs) > 4 {
			return fmt.Sprintf("path too deep: %q (max 4 segments)", f.Path)
		}
		for _, seg := range segs {
			if !codeFileSegment.MatchString(seg) {
				return fmt.Sprintf("invalid path segment %q in %q", seg, f.Path)
			}
		}
		lower := strings.ToLower(f.Path)
		if seen[lower] {
			return fmt.Sprintf("duplicate path (case-insensitive): %q", f.Path)
		}
		seen[lower] = true

		ext := strings.ToLower(filepath.Ext(lower))
		isSource := (isGo && ext == ".go") || (!isGo && (ext == ".c" || ext == ".h"))
		isTextAsset := textAssetExts[ext]
		isBinaryAsset := binaryAssetExts[ext]
		switch {
		case isSource:
			if !isGo && ext == ".c" {
				hasC = true
			}
		case isTextAsset, isBinaryAsset:
			// Shared asset whitelist — same for both languages.
		default:
			if isGo {
				return fmt.Sprintf("invalid extension for a Go project: %q (.go source, or assets: html htm tmpl txt json csv svg md css gif png jpg)", f.Path)
			}
			return fmt.Sprintf("invalid extension for a C project: %q (.c/.h source, or assets: html htm tmpl txt json csv svg md css gif png jpg)", f.Path)
		}

		// Encoding rules (see the doc above): binary REQUIRES base64, the
		// rest FORBIDS it, and base64 must decode — a corrupt payload dies
		// at the gate, not inside the disk mirror or the export builder.
		switch f.Encoding {
		case "":
			if isBinaryAsset {
				return fmt.Sprintf("%q is a binary asset and must be uploaded (encoding base64), not typed", f.Path)
			}
			totalStored += len(f.Content)
		case "base64":
			if !isBinaryAsset {
				return fmt.Sprintf("%q must be plain text (base64 is for binary assets only)", f.Path)
			}
			decoded, decErr := base64.StdEncoding.DecodeString(f.Content)
			if decErr != nil {
				return fmt.Sprintf("%q: invalid base64 content", f.Path)
			}
			if len(decoded) > maxAssetBytes {
				return fmt.Sprintf("%q too large: %d KB (max %d KB per asset)", f.Path, len(decoded)>>10, maxAssetBytes>>10)
			}
			totalStored += len(f.Content)
		default:
			return fmt.Sprintf("%q: unknown encoding %q (\"\" or \"base64\")", f.Path, f.Encoding)
		}
		if isTextAsset && len(f.Content) > maxAssetBytes {
			return fmt.Sprintf("%q too large: %d KB (max %d KB per asset)", f.Path, len(f.Content)>>10, maxAssetBytes>>10)
		}

		if isSource && strings.TrimSpace(f.Content) != "" {
			hasSourceContent = true
		}
	}
	if totalStored > maxSnapshotBytes {
		return fmt.Sprintf("project too large: %d KB stored (max %d KB)", totalStored>>10, maxSnapshotBytes>>10)
	}
	// Multi-file Go shipped with GoMF (2026-07-08): a Go project is a Go
	// PACKAGE — the struct in one file, methods across siblings — so the
	// count rule is the same 1..maxCodeFiles the C side has. The Go-shaped
	// rules (one exported struct across the set, same package name, no
	// method redeclaration) belong to the PARSER, which sees semantics;
	// this gate only owns paths and extensions.
	//
	// Português: Go multiarquivo chegou com o GoMF: projeto Go é um
	// PACOTE Go, então a regra de contagem é a mesma do C. As regras com
	// forma de Go (um struct exportado no conjunto, mesmo pacote, sem
	// redeclaração) são do PARSER, que enxerga semântica; este portão só
	// cuida de caminhos e extensões.
	if !isGo && !hasC {
		return "a C project needs at least one .c file (headers alone carry no definitions)"
	}
	if !hasSourceContent {
		return "at least one source file must have content (assets alone do not make a device)"
	}
	return ""
}

// mirrorCodeSnapshot writes the snapshot into the on-disk code section —
// derived data for /static viewing; the database is the source of truth,
// so failures log and never abort the request. Subdirectories are created
// per file (paths were validated: plain relative, bounded depth).
//
// Português: Espelha o snapshot no disco (dado derivado para /static; o
// banco é a verdade — falha loga e segue). Cria subpastas por arquivo.
func mirrorCodeSnapshot(c echo.Context, codeDir string, files []store.CodeFileEntry) {
	for _, f := range files {
		full := filepath.Join(codeDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			c.Logger().Errorf("[projectapi] mirror mkdir %s: %v", f.Path, err)
			continue
		}
		// The mirror holds REAL bytes — a base64 asset decodes at this
		// edge so /static serves the actual gif/png, not its text form.
		// The gate already proved the payload decodes; a failure here
		// would mean the snapshot was corrupted after validation, which
		// is worth a loud log line and a skipped file, never an abort
		// (derived data — the database stays the truth).
		//
		// Português: O espelho guarda bytes REAIS — base64 decodifica
		// nesta borda. O portão já provou que decodifica; falhar aqui é
		// corrupção pós-validação: loga alto e pula, nunca aborta.
		data := []byte(f.Content)
		if f.Encoding == "base64" {
			decoded, decErr := base64.StdEncoding.DecodeString(f.Content)
			if decErr != nil {
				c.Logger().Errorf("[projectapi] mirror decode %s: %v", f.Path, decErr)
				continue
			}
			data = decoded
		}
		if err := os.WriteFile(full, data, 0644); err != nil {
			c.Logger().Errorf("[projectapi] mirror write %s: %v", f.Path, err)
		}
	}
}

// snapshotNextVersion clones the latest snapshot's file set (empty when no
// version exists yet), hands it to mutate, validates the result and writes
// it as a NEW version — the one write path upload, delete and rename share,
// so a file-manager operation is exactly a save with a computed set.
//
// Português: Clona o conjunto do último snapshot, aplica mutate, valida e
// grava como versão NOVA — o único caminho de escrita que upload, delete e
// rename compartilham: operação de arquivo é um save com conjunto calculado.
func snapshotNextVersion(c echo.Context, p *store.Project, userID string,
	mutate func(files []store.CodeFileEntry) ([]store.CodeFileEntry, string)) error {

	var files []store.CodeFileEntry
	if latest, err := store.GetLatestProjectCodeVersion(p.ID); err == nil {
		files = append(files, latest.Files...)
	} else if err != store.ErrNotFound {
		return fail(c, 500, "internal error")
	}

	next, msg := mutate(files)
	if msg != "" {
		return fail(c, 400, msg)
	}
	if valMsg := validateCodeFileSet(next, p.ProgrammingLanguageID); valMsg != "" {
		return fail(c, 400, valMsg)
	}

	nextVer, err := store.GetNextCodeVersionNumber(p.ID)
	if err != nil {
		return fail(c, 500, "internal error")
	}
	vID, err := cryptoauth.NewID()
	if err != nil {
		return fail(c, 500, "internal error")
	}
	v := &store.ProjectCodeVersion{
		ID: vID, ProjectID: p.ID, UserID: userID, Version: nextVer, Files: next,
	}
	if err := store.CreateProjectCodeVersion(v); err != nil {
		if err == store.ErrConflict {
			return fail(c, 409, "version conflict — please retry")
		}
		return fail(c, 500, "could not save version")
	}

	cfg := config.Get()
	codeDir := filepath.Join(projectBasePath(cfg, userID, p.Type, p.ID), store.ProjectFileSectionCode)
	if mkErr := os.MkdirAll(codeDir, 0755); mkErr == nil {
		if clrErr := clearDirectory(codeDir); clrErr != nil {
			c.Logger().Errorf("[projectapi] mirror clear: %v", clrErr)
		}
		mirrorCodeSnapshot(c, codeDir, next)
	}
	return ok(c, map[string]any{"version": v.Version, "files": v.Files})
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

	// Read the upload into memory — the snapshot lives in the database
	// (the disk section is a derived mirror), and code files are text
	// measured in kilobytes. Extension/path legality is enforced by
	// validateCodeFileSet against the PROJECT's language inside
	// snapshotNextVersion, the same contract as a Monaco save.
	//
	// Português: Lê o upload em memória — o snapshot mora no banco (disco
	// é espelho) e código se mede em kilobytes. Extensão/caminho passam
	// pelo MESMO contrato do save, dentro do snapshotNextVersion.
	src, err := fh.Open()
	if err != nil {
		return fail(c, 500, "could not read uploaded file")
	}
	content, err := io.ReadAll(src)
	_ = src.Close()
	if err != nil {
		return fail(c, 500, "could not read uploaded file")
	}
	name := filepath.Base(strings.TrimSpace(fh.Filename))

	// Classify by extension — the same three-way split the gate enforces.
	// Binary assets are base64-encoded HERE (the transport and the snapshot
	// are JSON text); text files must be valid UTF-8, otherwise the honest
	// answer is "this is not the text file its extension claims", not a
	// silently mangled snapshot.
	//
	// Português: Classifica pela extensão — o mesmo corte do portão.
	// Binário vira base64 AQUI; texto tem que ser UTF-8 válido — senão a
	// resposta honesta é rejeitar, não mutilar o snapshot em silêncio.
	encoding := ""
	stored := string(content)
	if binaryAssetExts[strings.ToLower(filepath.Ext(name))] {
		encoding = "base64"
		stored = base64.StdEncoding.EncodeToString(content)
	} else if !utf8.Valid(content) {
		return fail(c, 400, fmt.Sprintf("%q is not valid UTF-8 text", name))
	}

	return snapshotNextVersion(c, p, claims.UserID,
		func(files []store.CodeFileEntry) ([]store.CodeFileEntry, string) {
			// One semantics for both languages since GoMF: replace by path
			// when the file already exists (re-upload is an update, not a
			// duplicate); append otherwise. The Go whole-set replacement of
			// the single-file era died with that era — a Go project is a Go
			// PACKAGE now, and "uploading helpers.go" must not silently
			// delete device.go.
			//
			// Português: Uma semântica só desde o GoMF: substitui por
			// caminho ou anexa. A substituição-do-conjunto do Go morreu com
			// a era single-file — subir helpers.go não pode apagar
			// device.go em silêncio.
			for i := range files {
				if strings.EqualFold(files[i].Path, name) {
					files[i].Content = stored
					files[i].Encoding = encoding
					return files, ""
				}
			}
			return append(files, store.CodeFileEntry{Path: name, Content: stored, Encoding: encoding}), ""
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

	// ?path=util.c removes ONE file from the snapshot (a new version with
	// the file gone — history keeps it, exactly like any other save). No
	// query parameter keeps the single-file era's meaning: wipe the code
	// section, here spelled "clear the disk mirror" — the version history
	// is the user's data and survives; the next save starts a fresh
	// snapshot.
	//
	// Português: ?path= remove UM arquivo (versão nova sem ele — o
	// histórico preserva). Sem parâmetro mantém o sentido antigo: limpa o
	// espelho em disco; o histórico sobrevive e o próximo save recomeça.
	if path := strings.TrimSpace(c.QueryParam("path")); path != "" {
		return snapshotNextVersion(c, p, claims.UserID,
			func(files []store.CodeFileEntry) ([]store.CodeFileEntry, string) {
				out := files[:0]
				found := false
				for _, f := range files {
					if strings.EqualFold(f.Path, path) {
						found = true
						continue
					}
					out = append(out, f)
				}
				if !found {
					return nil, fmt.Sprintf("no file named %q in the current snapshot", path)
				}
				return out, ""
			})
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
		// OldPath selects which file to rename. Optional for the
		// single-file editor of today: when empty, the snapshot's first
		// file is the target (there is exactly one). The multi-tab UI
		// (next slice) always sends it.
		//
		// Português: Qual arquivo renomear. Opcional no editor de arquivo
		// único de hoje (vazio = primeiro arquivo); a UI multiabas sempre
		// envia.
		OldPath string `json:"oldPath"`
		NewName string `json:"newName"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}
	req.OldPath = strings.TrimSpace(req.OldPath)
	req.NewName = strings.TrimSpace(req.NewName)
	if req.NewName == "" {
		return fail(c, 400, "newName is required")
	}

	p, err := store.GetProjectByIDAndUser(projectID, claims.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	// The rename itself is "a save with one path changed": spelling and
	// extension legality come from the same validateCodeFileSet contract
	// as every other write, so a rename can never smuggle in a path a
	// save would reject.
	//
	// Português: Rename é "um save com um caminho trocado" — passa pelo
	// MESMO contrato de validação; rename nunca contrabandeia caminho que
	// o save recusaria.
	return snapshotNextVersion(c, p, claims.UserID,
		func(files []store.CodeFileEntry) ([]store.CodeFileEntry, string) {
			if len(files) == 0 {
				return nil, "no code file found in this project"
			}
			idx := 0
			if req.OldPath != "" {
				idx = -1
				for i := range files {
					if strings.EqualFold(files[i].Path, req.OldPath) {
						idx = i
						break
					}
				}
				if idx < 0 {
					return nil, fmt.Sprintf("no file named %q in the current snapshot", req.OldPath)
				}
			}
			files[idx].Path = req.NewName
			return files, ""
		})
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

	// The row stores the working copy as a JSON files blob in the
	// source column and the active tab's path in filename (see the
	// scratchpad-format doctrine in store/project_backups.go). An
	// undecodable blob is treated as "no usable backup" — backup is
	// scratch, and refusing to open a project over corrupt scratch
	// would invert its purpose.
	//
	// Português: A linha guarda a cópia como blob JSON de arquivos no
	// source e a aba ativa no filename. Blob indecifrável = "sem backup
	// utilizável" — backup é rascunho; recusar abrir o projeto por
	// rascunho corrompido inverteria o propósito.
	var files []store.CodeFileEntry
	if err := json.Unmarshal([]byte(b.Source), &files); err != nil || len(files) == 0 {
		return fail(c, 404, "no backup")
	}
	return ok(c, map[string]any{
		"files":      files,
		"activePath": b.Filename,
		"updatedAt":  b.UpdatedAt,
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
		// Files is the WHOLE working copy — every open tab, strip
		// order. The backup exists to survive a crash, and with tabs a
		// single-slot backup would let that crash eat every sibling of
		// the active file. Paths are NOT validated against the save
		// contract: backup legitimately stores transient mid-edit state
		// (a half-typed rename, an extension about to change).
		//
		// Português: A cópia de trabalho INTEIRA — toda aba, na ordem
		// da faixa. Caminhos NÃO passam pelo contrato do save: backup
		// guarda estado transitório de meio-de-edição por legítimo
		// direito.
		Files      []store.CodeFileEntry `json:"files"`
		ActivePath string                `json:"activePath"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, 400, "invalid request body")
	}
	if len(req.Files) > maxCodeFiles {
		return fail(c, 400, fmt.Sprintf("too many files: %d (max %d)", len(req.Files), maxCodeFiles))
	}
	req.ActivePath = strings.TrimSpace(req.ActivePath)

	if _, err := store.GetProjectByIDAndUser(projectID, claims.UserID); err != nil {
		if err == store.ErrNotFound {
			return fail(c, 404, "project not found")
		}
		return fail(c, 500, "internal error")
	}

	// All-blank set → empty blob → the store's "empty source deletes"
	// rule fires, generalised to the set: clearing every tab clears the
	// backup, so reopens don't restore emptiness.
	blob := ""
	for _, f := range req.Files {
		if strings.TrimSpace(f.Content) != "" {
			enc, mErr := json.Marshal(req.Files)
			if mErr != nil {
				return fail(c, 500, "could not encode backup")
			}
			blob = string(enc)
			break
		}
	}
	if err := store.SaveProjectBackup(projectID, blob, req.ActivePath); err != nil {
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
