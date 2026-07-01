// server/handler/stagefileapi/handlers.go — Stage file management API handlers.
//
// Stage files are saved IDE scenes (JSON snapshots of the canvas). They are
// private per-user and completely independent from the portal's "projects".
//
// All handlers require a valid Bearer token. The authenticated user ID is used
// as the ownership scope — users can only access their own files and folders.
//
// Virtual folders give the user a familiar directory structure. Folders can
// be nested to arbitrary depth and are deleted with CASCADE (deleting a folder
// removes all files and sub-folders inside it).
//
// Endpoints:
//
//	GET    /api/v1/stage-files              — list files (optional ?folderId filter)
//	POST   /api/v1/stage-files              — create file
//	GET    /api/v1/stage-files/limit        — get usage vs capacity
//	GET    /api/v1/stage-files/:id          — load file with scene_json
//	PUT    /api/v1/stage-files/:id          — update file (rename, move, save scene)
//	DELETE /api/v1/stage-files/:id          — delete file
//
//	GET    /api/v1/stage-files/folders      — list all folders (flat, build tree client-side)
//	POST   /api/v1/stage-files/folders      — create folder
//	PUT    /api/v1/stage-files/folders/:id  — rename or move folder
//	DELETE /api/v1/stage-files/folders/:id  — delete folder (CASCADE: files + sub-folders)
package stagefileapi

import (
	"net/http"
	"regexp"

	cryptoauth "server/auth"
	"server/handler/spaauth"
	"server/store"

	"github.com/labstack/echo/v4"
)

// iconIDPattern is the format gate for stage_files.icon_id values. It
// matches FontAwesome Free icon names like "cube", "thermometer", or
// "screen-share". The validator runs server-side as defence against
// direct API calls — the WASM client filters its picker against the
// generated window.FA_FREE_STYLES catalogue, so a normal maker can
// only pick names that pass this regex anyway.
//
// The pattern is deliberately permissive: it accepts ANY [a-z0-9-]
// string of length ≥ 1, not the closed set of names. Loading the full
// FA catalogue server-side would couple the server to the client's
// icon tooling (cmd/gen-fa-icons) every release. An invalid name
// stored here produces a tofu glyph in the UI — visible, harmless,
// and fixable in one click via the Edit dialog.
//
// "__clear__" is reserved for the UpdateStageFile sentinel meaning
// "reset to NULL". Callers that genuinely want to clear the icon pass
// "__clear__"; the validator accepts it as a special case so the
// regex itself stays simple.
var iconIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// validIconID returns true for the empty string (no change / not set),
// the "__clear__" sentinel, and any string matching iconIDPattern.
// Any other value is rejected at the handler boundary with 400.
func validIconID(s string) bool {
	if s == "" || s == "__clear__" {
		return true
	}
	return iconIDPattern.MatchString(s)
}

// ─── Response helpers (project-standard envelope) ─────────────────────────────

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

// ─── Files ────────────────────────────────────────────────────────────────────

// handleListFiles returns all files for the authenticated user.
// Optional query param ?folderId=xxx filters to a specific folder.
// When folderId is omitted, all files across all folders are returned.
func handleListFiles(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	folderID := c.QueryParam("folderId")

	files, err := store.ListStageFiles(claims.UserID, folderID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "failed to list files")
	}
	if files == nil {
		files = []store.StageFile{}
	}
	return ok(c, map[string]any{"files": files})
}

// handleGetFile returns a single file including the full scene_json.
func handleGetFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	fileID := c.Param("id")

	f, err := store.GetStageFile(claims.UserID, fileID)
	if err != nil {
		return fail(c, http.StatusNotFound, "file not found")
	}
	return ok(c, map[string]any{"file": f})
}

// handleCreateFile creates a new stage file.
// Body: { "name": "...", "folderId": "...", "kind": "stage"|"tutorial",
//
//	"language": "c"|"go", "sceneJson": "...", "deviceCount": N,
//	"isBackup": false }
//
// The "kind" field is optional and defaults to "stage" when omitted.
// Any value other than "stage" or "tutorial" is rejected to prevent
// silent typos from producing files the file manager cannot display.
//
// The "language" field is optional and defaults to "c" (C99) when
// omitted. Accepted values: "c" or "go". The language is fixed at
// creation — see handleUpdateFile for why we never let it change.
func handleCreateFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var body struct {
		Name        string `json:"name"`
		FolderID    string `json:"folderId"`
		Kind        string `json:"kind"`
		Language    string `json:"language"`
		IconID      string `json:"iconId"`
		SceneJSON   string `json:"sceneJson"`
		DeviceCount int    `json:"deviceCount"`
		IsBackup    bool   `json:"isBackup"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	if body.Name == "" {
		return fail(c, http.StatusBadRequest, "name is required")
	}
	if body.SceneJSON == "" {
		body.SceneJSON = "{}"
	}
	// Validate kind. Empty = server-side default to "stage".
	switch body.Kind {
	case "", store.StageFileKindStage, store.StageFileKindTutorial:
		// accepted
	default:
		return fail(c, http.StatusBadRequest,
			"kind must be \"stage\" or \"tutorial\"")
	}
	// Validate language. Empty = server-side default to "c" (C99).
	// Any other value is rejected to prevent silent typos that
	// would produce a row the device filter cannot honour.
	switch body.Language {
	case "", store.StageFileLanguageC, store.StageFileLanguageGo:
		// accepted
	default:
		return fail(c, http.StatusBadRequest,
			"language must be \"c\" or \"go\"")
	}
	// Validate icon_id format. Empty = no icon chosen (UI uses its
	// default at render time). "__clear__" is reserved for the
	// update sentinel and is meaningless on create — reject it
	// loudly rather than silently treat it as "no icon".
	if body.IconID == "__clear__" {
		return fail(c, http.StatusBadRequest,
			"iconId \"__clear__\" is reserved for update operations")
	}
	if !validIconID(body.IconID) {
		return fail(c, http.StatusBadRequest,
			"iconId must match [a-z0-9-]+")
	}

	f := &store.StageFile{
		ID:          cryptoauth.MustNewID(),
		UserID:      claims.UserID,
		FolderID:    body.FolderID,
		Name:        body.Name,
		Kind:        body.Kind,     // empty string is allowed; store defaults to "stage"
		Language:    body.Language, // empty string is allowed; store defaults to "c"
		IconID:      body.IconID,   // empty string is allowed; UI applies default
		SceneJSON:   body.SceneJSON,
		DeviceCount: body.DeviceCount,
		IsBackup:    body.IsBackup,
	}

	if err := store.CreateStageFile(f); err != nil {
		// Check for limit reached vs duplicate name.
		errMsg := err.Error()
		if contains(errMsg, "limit reached") {
			return fail(c, http.StatusForbidden, errMsg)
		}
		if contains(errMsg, "already exists") {
			return fail(c, http.StatusConflict, errMsg)
		}
		return fail(c, http.StatusInternalServerError, "failed to create file")
	}

	// Return without scene_json to keep response light.
	f.SceneJSON = ""
	return ok(c, map[string]any{"file": f})
}

// handleUpdateFile updates an existing file (rename, move, save scene,
// change icon, or convert between stage and tutorial).
// Body: { "name": "...", "folderId": "...", "kind": "...",
//
//	"iconId": "...", "sceneJson": "...", "deviceCount": N }
//
// All fields are optional — omit a field to keep its current value.
// To move to root, send folderId: "__root__".
// To clear the icon (back to UI default), send iconId: "__clear__".
//
// iconId is validated server-side against a permissive regex
// ([a-z0-9-]+). Whether the value names an icon that actually ships
// in FA Free is the client's responsibility — the WASM picker
// filters against window.FA_FREE_STYLES, so only valid names reach
// this endpoint under normal use.
//
// The "language" field is INTENTIONALLY NOT bound here. The
// language of a project is fixed at creation (see handleCreateFile)
// and irreversible. If a client erroneously sends a "language"
// field, it is silently dropped by Echo's JSON binder because the
// body struct below does not declare it — there is no need to
// reject the request loudly, because keeping behaviour unchanged
// is exactly what the caller would want.
func handleUpdateFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	fileID := c.Param("id")

	var body struct {
		Name        string `json:"name"`
		FolderID    string `json:"folderId"`
		Kind        string `json:"kind"`
		IconID      string `json:"iconId"`
		SceneJSON   string `json:"sceneJson"`
		DeviceCount int    `json:"deviceCount"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	// Validate kind on write. Empty = no change.
	switch body.Kind {
	case "", store.StageFileKindStage, store.StageFileKindTutorial:
		// accepted
	default:
		return fail(c, http.StatusBadRequest,
			"kind must be \"stage\" or \"tutorial\"")
	}
	// Validate icon_id format. Empty = no change. "__clear__" =
	// reset to NULL. Any non-empty string must match [a-z0-9-]+.
	if !validIconID(body.IconID) {
		return fail(c, http.StatusBadRequest,
			"iconId must match [a-z0-9-]+ or be \"__clear__\"")
	}

	if err := store.UpdateStageFile(
		claims.UserID, fileID,
		body.Name, body.FolderID, body.Kind, body.SceneJSON, body.IconID, body.DeviceCount,
	); err != nil {
		errMsg := err.Error()
		if contains(errMsg, "not found") {
			return fail(c, http.StatusNotFound, errMsg)
		}
		if contains(errMsg, "already exists") {
			return fail(c, http.StatusConflict, errMsg)
		}
		return fail(c, http.StatusInternalServerError, "failed to update file")
	}
	return ok(c, map[string]any{"updated": true})
}

// handleDeleteFile deletes a file.
func handleDeleteFile(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	fileID := c.Param("id")

	if err := store.DeleteStageFile(claims.UserID, fileID); err != nil {
		return fail(c, http.StatusNotFound, "file not found")
	}
	return ok(c, map[string]any{"deleted": true})
}

// handleGetLimit returns the user's file limit and current usage.
func handleGetLimit(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	info := store.GetStageFileLimit(claims.UserID)
	return ok(c, info)
}

// ─── Folders ──────────────────────────────────────────────────────────────────

// handleListFolders returns all folders owned by the user (flat list).
// The client builds the tree using parentId.
func handleListFolders(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	folders, err := store.ListStageFolders(claims.UserID)
	if err != nil {
		return fail(c, http.StatusInternalServerError, "failed to list folders")
	}
	if folders == nil {
		folders = []store.StageFolder{}
	}
	return ok(c, map[string]any{"folders": folders})
}

// handleCreateFolder creates a new virtual folder.
// Body: { "name": "...", "parentId": "..." }
// parentId is optional — omit or empty for root-level folder.
func handleCreateFolder(c echo.Context) error {
	claims := spaauth.BearerClaims(c)

	var body struct {
		Name     string `json:"name"`
		ParentID string `json:"parentId"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	if body.Name == "" {
		return fail(c, http.StatusBadRequest, "name is required")
	}

	f := &store.StageFolder{
		ID:       cryptoauth.MustNewID(),
		UserID:   claims.UserID,
		ParentID: body.ParentID,
		Name:     body.Name,
	}

	if err := store.CreateStageFolder(f); err != nil {
		if contains(err.Error(), "already exists") {
			return fail(c, http.StatusConflict, err.Error())
		}
		return fail(c, http.StatusInternalServerError, "failed to create folder")
	}
	return ok(c, map[string]any{"folder": f})
}

// handleUpdateFolder renames or moves a folder.
// Body: { "name": "...", "parentId": "..." }
// To move to root, send parentId: "__root__".
func handleUpdateFolder(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	folderID := c.Param("id")

	var body struct {
		Name     string `json:"name"`
		ParentID string `json:"parentId"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}

	// Rename if name is provided.
	if body.Name != "" {
		if err := store.RenameStageFolder(claims.UserID, folderID, body.Name); err != nil {
			errMsg := err.Error()
			if contains(errMsg, "not found") {
				return fail(c, http.StatusNotFound, errMsg)
			}
			if contains(errMsg, "already exists") {
				return fail(c, http.StatusConflict, errMsg)
			}
			return fail(c, http.StatusInternalServerError, "failed to rename folder")
		}
	}

	// Move if parentId is provided.
	if body.ParentID != "" {
		target := body.ParentID
		if target == "__root__" {
			target = ""
		}
		if err := store.MoveStageFolder(claims.UserID, folderID, target); err != nil {
			errMsg := err.Error()
			if contains(errMsg, "not found") {
				return fail(c, http.StatusNotFound, errMsg)
			}
			if contains(errMsg, "cannot move") || contains(errMsg, "already exists") {
				return fail(c, http.StatusConflict, errMsg)
			}
			return fail(c, http.StatusInternalServerError, "failed to move folder")
		}
	}

	return ok(c, map[string]any{"updated": true})
}

// handleDeleteFolder deletes a folder and all its contents (CASCADE).
func handleDeleteFolder(c echo.Context) error {
	claims := spaauth.BearerClaims(c)
	folderID := c.Param("id")

	if err := store.DeleteStageFolder(claims.UserID, folderID); err != nil {
		return fail(c, http.StatusNotFound, "folder not found")
	}
	return ok(c, map[string]any{"deleted": true})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
