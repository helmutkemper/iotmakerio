// server/store/stage_files.go — CRUD and limit resolution for stage files.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Stage files are saved IDE scenes. Each file belongs to one user and
// optionally lives inside a virtual folder. Files are private — only
// the owning user can list, read, create, update, or delete them.
//
// Limit resolution follows a three-layer cascade:
//
//  1. Per-user override   (stage_file_user_limits)
//  2. Per-group override  (stage_file_group_limits via user_group_members)
//  3. Global setting      (project_settings key "stage_file_max_per_user")
//  4. Hard fallback       (DefaultStageFileMaxPerUser = 50)
//
// All functions receive userID as the first parameter and use it for
// ownership verification. A mismatched userID always returns "not found"
// rather than "forbidden" to avoid information leakage.
package store

import (
	"database/sql"
	"fmt"
	"time"
)

// DefaultStageFileMaxPerUser is the compile-time fallback when neither the
// database setting nor any override is configured. Matches the seed value
// in db_stage_files.go so behaviour is identical before and after first boot.
const DefaultStageFileMaxPerUser = 50

// StageFileKind values for the `kind` column. The column is a free-form
// TEXT so new kinds can be added without a migration, but client code
// should only set values from this list to stay interoperable.
//
// Português: Valores aceitos pela coluna `kind`. A coluna é TEXT livre,
// mas o client só deve atribuir valores desta lista.
const (
	// StageFileKindStage — regular saved scene. The default.
	StageFileKindStage = "stage"

	// StageFileKindTutorial — a guided tutorial file. Opened via the
	// file manager's Start button; the player reads the `tutorial`
	// object embedded inside scene_json. See
	// /ide/docs/DELIVERY_C_TUTORIAL_DESIGN.md for the file schema.
	StageFileKindTutorial = "tutorial"
)

// ─── Models ───────────────────────────────────────────────────────────────────

// StageFolder represents a virtual folder for organising stage files.
type StageFolder struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	ParentID  string `json:"parentId,omitempty"` // empty string = root
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// StageFile represents a saved IDE scene.
//
// The Kind field discriminates between regular scenes ("stage") and
// tutorial files ("tutorial"). The file manager renders different
// icons and action buttons per kind. For tutorial files, SceneJSON
// is expected to carry a top-level `tutorial` object alongside the
// usual `scene` section — see DELIVERY_C_TUTORIAL_DESIGN.md §3.
//
// When Kind is omitted on write (empty string), handlers default it
// to StageFileKindStage. This keeps old clients that never learned
// about the field fully compatible.
// Stage file language constants. These mirror the values stored in
// the stage_files.language column and the Request.Language values
// accepted by server/codegen.
//
// The token "c" rather than "c99" is deliberate: it matches the
// existing codegen pipeline (server/codegen/codeGen.go uses `case
// "c":` to route to the ANSI C backend) and keeps wire/JSON values
// short. UI surfaces translate "c" to the display label "C99" — the
// token-vs-label split is the same pattern used for kind ("stage"
// stored, "Stage" displayed).
//
// Why an irreversible language choice:
//
// A device's runtime behaviour depends on which backend will compile
// it. Black-boxes ship with hand-written Go code today and only Go;
// no C99 implementation exists, and Phase 1 has no plans for one.
// Mixing the two inside a single project would mean some devices
// compile and others silently fail at export. Locking the language
// at creation forces the choice up front and keeps the device menu
// honest — the user sees only what will work.
//
// Português: Valores armazenados na coluna `language` de stage_files.
// "c" é o token (curto, alinhado com o codegen); "C99" é o label de
// UI. A escolha é irreversível: o projeto é 100% Go ou 100% C99
// desde a criação. Garante que todo device no menu compila no
// backend escolhido.
const (
	// StageFileLanguageGo — project that compiles to Go via the
	// codegen Go backend. Full feature set, including black-boxes.
	StageFileLanguageGo = "go"

	// StageFileLanguageC — project that compiles to ANSI C99 via the
	// codegen C backend. Phase 1: primitives only, no black-boxes.
	// This is the default chosen when the user closes the welcome
	// modal without picking (X / ESC) — see welcome modal flow.
	StageFileLanguageC = "c"
)

// StageFile is the canonical server-side representation of a saved
// scene. The JSON tags drive both the REST wire format and the
// fields the WASM client mirrors in stagefileclient.StageFileEntry —
// see that file for the client-side twin.
//
// The Kind field discriminates between regular scenes ("stage") and
// tutorial files ("tutorial"). The file manager renders different
// icons and action buttons per kind. For tutorial files, SceneJSON
// is expected to carry a top-level `tutorial` object alongside the
// usual `scene` section — see DELIVERY_C_TUTORIAL_DESIGN.md §3.
//
// The Language field is fixed at creation. CreateStageFile defaults
// an empty value to StageFileLanguageC so clients that omit the
// field still get a valid row; UpdateStageFile deliberately does
// NOT update Language — the column is set on insert and never moved.
//
// When Kind is omitted on write (empty string), handlers default it
// to StageFileKindStage. This keeps old clients that never learned
// about the field fully compatible.
type StageFile struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	FolderID string `json:"folderId,omitempty"` // empty string = root
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`     // "stage" (default) | "tutorial"
	Language string `json:"language,omitempty"` // "c" (default) | "go"
	// IconID is a FontAwesome Free icon name chosen by the maker
	// (e.g. "cpu", "thermometer", "lightbulb"). Empty means "no
	// choice" — the UI substitutes its own default at render time
	// rather than the server invent one here, so a future change
	// to the default doesn't require a data migration.
	//
	// The server validates only the format ([a-z0-9-]+). Whether
	// the value names an icon that actually ships in FA Free is
	// the client's responsibility: the picker filters against the
	// generated window.FA_FREE_STYLES so a typical user can only
	// pick valid names. A direct API call with a bogus name yields
	// a tofu glyph the maker can fix in the file's edit dialog.
	IconID      string `json:"iconId,omitempty"`
	SceneJSON   string `json:"sceneJson,omitempty"` // omitted in list responses
	DeviceCount int    `json:"deviceCount"`
	IsBackup    bool   `json:"isBackup"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// StageFileLimitInfo is returned by GetStageFileLimit to show usage vs capacity.
type StageFileLimitInfo struct {
	MaxFiles    int    `json:"maxFiles"`
	UsedFiles   int    `json:"usedFiles"`
	LimitSource string `json:"limitSource"` // "user" | "group" | "global" | "default"
}

// ─── Limit resolution ─────────────────────────────────────────────────────────

// GetStageFileLimit resolves the effective file limit for the given user and
// counts how many files they currently own. Returns a StageFileLimitInfo that
// the API can return to the frontend for the "5 of 50 files used" indicator.
func GetStageFileLimit(userID string) StageFileLimitInfo {
	maxFiles, source := resolveStageFileMax(userID)
	used := countStageFiles(userID)
	return StageFileLimitInfo{
		MaxFiles:    maxFiles,
		UsedFiles:   used,
		LimitSource: source,
	}
}

// resolveStageFileMax returns (limit, source) using the cascade:
// user override → group override → global setting → hard fallback.
func resolveStageFileMax(userID string) (int, string) {
	// Layer 1: per-user override.
	var userMax int
	err := DB.QueryRow(
		`SELECT max_files FROM stage_file_user_limits WHERE user_id = ?`, userID,
	).Scan(&userMax)
	if err == nil && userMax > 0 {
		return userMax, "user"
	}

	// Layer 2: per-group override — pick the highest limit among the user's groups.
	var groupMax int
	err = DB.QueryRow(`
		SELECT COALESCE(MAX(g.max_files), 0)
		FROM stage_file_group_limits g
		JOIN user_group_members m ON m.group_id = g.group_id
		WHERE m.user_id = ?`, userID,
	).Scan(&groupMax)
	if err == nil && groupMax > 0 {
		return groupMax, "group"
	}

	// Layer 3: global setting.
	globalMax := GetSettingInt(SettingStageFileMaxPerUser, 0)
	if globalMax > 0 {
		return globalMax, "global"
	}

	// Layer 4: hard fallback.
	return DefaultStageFileMaxPerUser, "default"
}

// countStageFiles returns how many non-backup stage files the user currently owns.
// Backup files (is_backup = 1) do not count against the limit.
func countStageFiles(userID string) int {
	var count int
	_ = DB.QueryRow(
		`SELECT COUNT(*) FROM stage_files WHERE user_id = ? AND is_backup = 0`, userID,
	).Scan(&count)
	return count
}

// ─── Folders CRUD ─────────────────────────────────────────────────────────────

// CreateStageFolder creates a new virtual folder. Returns ErrDuplicateName when
// a folder with the same name already exists at the same level.
func CreateStageFolder(f *StageFolder) error {
	now := time.Now().UTC().Format(time.RFC3339)
	f.CreatedAt = now
	f.UpdatedAt = now

	parentID := sql.NullString{String: f.ParentID, Valid: f.ParentID != ""}

	_, err := DB.Exec(`
		INSERT INTO stage_folders (id, user_id, parent_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		f.ID, f.UserID, parentID, f.Name, f.CreatedAt, f.UpdatedAt,
	)
	if err != nil && isDuplicateError(err) {
		return fmt.Errorf("a folder named %q already exists at this level", f.Name)
	}
	return err
}

// ListStageFolders returns all folders owned by the user, ordered by name.
// The full tree can be reconstructed client-side using the parentId field.
func ListStageFolders(userID string) ([]StageFolder, error) {
	rows, err := DB.Query(`
		SELECT id, user_id, COALESCE(parent_id, ''), name, created_at, updated_at
		FROM stage_folders
		WHERE user_id = ?
		ORDER BY name ASC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []StageFolder
	for rows.Next() {
		var f StageFolder
		if err := rows.Scan(&f.ID, &f.UserID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// RenameStageFolder renames a folder. Returns an error if the new name conflicts.
func RenameStageFolder(userID, folderID, newName string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := DB.Exec(`
		UPDATE stage_folders SET name = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		newName, now, folderID, userID,
	)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("a folder named %q already exists at this level", newName)
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("folder not found")
	}
	return nil
}

// DeleteStageFolder deletes a folder and all its contents (files and sub-folders)
// via the ON DELETE CASCADE foreign key. Returns an error if the folder does not
// exist or does not belong to the user.
func DeleteStageFolder(userID, folderID string) error {
	res, err := DB.Exec(
		`DELETE FROM stage_folders WHERE id = ? AND user_id = ?`,
		folderID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("folder not found")
	}
	return nil
}

// MoveStageFolder moves a folder to a different parent (or root when newParentID
// is empty). Prevents moving a folder into itself or into a descendant.
func MoveStageFolder(userID, folderID, newParentID string) error {
	// Prevent moving into self.
	if folderID == newParentID {
		return fmt.Errorf("cannot move a folder into itself")
	}

	// Prevent moving into a descendant by walking up from newParentID.
	if newParentID != "" {
		if isDescendant(folderID, newParentID) {
			return fmt.Errorf("cannot move a folder into one of its descendants")
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	parentVal := sql.NullString{String: newParentID, Valid: newParentID != ""}
	res, err := DB.Exec(`
		UPDATE stage_folders SET parent_id = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		parentVal, now, folderID, userID,
	)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("a folder with this name already exists at the target level")
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("folder not found")
	}
	return nil
}

// isDescendant returns true if ancestorID is an ancestor of folderID.
// Walks up the parent chain from folderID looking for ancestorID.
func isDescendant(ancestorID, folderID string) bool {
	current := folderID
	for i := 0; i < 100; i++ { // depth guard
		var parentID sql.NullString
		err := DB.QueryRow(
			`SELECT parent_id FROM stage_folders WHERE id = ?`, current,
		).Scan(&parentID)
		if err != nil || !parentID.Valid {
			return false
		}
		if parentID.String == ancestorID {
			return true
		}
		current = parentID.String
	}
	return false
}

// ─── Files CRUD ───────────────────────────────────────────────────────────────

// CreateStageFile creates a new stage file. Checks the file limit before insert.
// Backup files (IsBackup = true) bypass the limit check — they are transient
// recovery files that are deleted on manual save.
// Returns an error when the user has reached their maximum (non-backup files only).
func CreateStageFile(f *StageFile) error {
	// Check limit before insert — backups are exempt.
	if !f.IsBackup {
		limit := GetStageFileLimit(f.UserID)
		if limit.UsedFiles >= limit.MaxFiles {
			return fmt.Errorf("file limit reached (%d/%d)", limit.UsedFiles, limit.MaxFiles)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	f.CreatedAt = now
	f.UpdatedAt = now

	folderID := sql.NullString{String: f.FolderID, Valid: f.FolderID != ""}
	isBackup := 0
	if f.IsBackup {
		isBackup = 1
	}

	// Default kind to "stage" when the caller omits it. Keeps old clients
	// and direct SQL tools compatible with the new column.
	kind := f.Kind
	if kind == "" {
		kind = StageFileKindStage
	}

	// Default language to "c" (C99) when the caller omits it. Same
	// rationale as kind — the column has a NOT NULL DEFAULT 'c' in
	// the schema, so this fallback keeps behaviour identical whether
	// the caller passes an empty Language or relies on the schema
	// default. We resolve it here too so the value reflected back
	// into f.Language is always non-empty for the caller.
	language := f.Language
	if language == "" {
		language = StageFileLanguageC
	}

	// icon_id: stored as NULL when empty, as text otherwise. NULL is
	// the canonical "no choice" state; storing an empty string would
	// require a UNIQUE-aware fallback in queries that don't exist
	// today. The client always treats NULL identically to an empty
	// string (no icon → default at render time).
	iconID := sql.NullString{String: f.IconID, Valid: f.IconID != ""}

	_, err := DB.Exec(`
		INSERT INTO stage_files (id, user_id, folder_id, name, kind, language, icon_id, scene_json, device_count, is_backup, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.UserID, folderID, f.Name, kind, language, iconID, f.SceneJSON, f.DeviceCount, isBackup, f.CreatedAt, f.UpdatedAt,
	)
	if err != nil && isDuplicateError(err) {
		return fmt.Errorf("a file named %q already exists in this folder", f.Name)
	}
	// Reflect the resolved values back into the input struct so the caller
	// sees the authoritative state without another query.
	f.Kind = kind
	f.Language = language
	return err
}

// ListStageFiles returns all files owned by the user, without scene_json (too large
// for list responses). Ordered by updated_at descending (most recent first).
// When folderID is non-empty, only files in that folder are returned.
// When folderID is empty, ALL files are returned (flat view across all folders).
//
// The `kind` column is included in the SELECT so the file manager can
// render a book icon and a Start button on tutorial rows without
// needing a second round-trip per file.
// ListStageFiles returns all files owned by the user, without scene_json (too large
// for list responses). Ordered by updated_at descending (most recent first).
// When folderID is non-empty, only files in that folder are returned.
// When folderID is empty, ALL files are returned (flat view across all folders).
//
// The `kind` and `language` columns are included in the SELECT so the
// file manager can render the right icon (book for tutorials), action
// button (Start for tutorials), and language chip (Go / C99) without
// needing a second round-trip per file.
//
// Português: Lista arquivos do usuário sem o scene_json (lista enxuta).
// Inclui kind e language no SELECT para que a UI possa renderizar
// ícone, ação e chip de linguagem sem requisições adicionais.
func ListStageFiles(userID, folderID string) ([]StageFile, error) {
	var rows *sql.Rows
	var err error

	if folderID != "" {
		rows, err = DB.Query(`
			SELECT id, user_id, COALESCE(folder_id, ''), name, kind, language, icon_id, device_count, is_backup, created_at, updated_at
			FROM stage_files
			WHERE user_id = ? AND folder_id = ?
			ORDER BY is_backup ASC, updated_at DESC`, userID, folderID,
		)
	} else {
		rows, err = DB.Query(`
			SELECT id, user_id, COALESCE(folder_id, ''), name, kind, language, icon_id, device_count, is_backup, created_at, updated_at
			FROM stage_files
			WHERE user_id = ?
			ORDER BY is_backup ASC, updated_at DESC`, userID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []StageFile
	for rows.Next() {
		var f StageFile
		var iconID sql.NullString
		var isBackup int
		if err := rows.Scan(&f.ID, &f.UserID, &f.FolderID, &f.Name, &f.Kind, &f.Language, &iconID, &f.DeviceCount, &isBackup, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if iconID.Valid {
			f.IconID = iconID.String
		}
		f.IsBackup = isBackup == 1
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetStageFile returns a single file including scene_json. Returns an error
// when the file does not exist or does not belong to the user.
func GetStageFile(userID, fileID string) (*StageFile, error) {
	var f StageFile
	var folderID sql.NullString
	var iconID sql.NullString
	var isBackup int
	err := DB.QueryRow(`
		SELECT id, user_id, folder_id, name, kind, language, icon_id, scene_json, device_count, is_backup, created_at, updated_at
		FROM stage_files
		WHERE id = ? AND user_id = ?`, fileID, userID,
	).Scan(&f.ID, &f.UserID, &folderID, &f.Name, &f.Kind, &f.Language, &iconID, &f.SceneJSON, &f.DeviceCount, &isBackup, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("file not found")
	}
	if folderID.Valid {
		f.FolderID = folderID.String
	}
	if iconID.Valid {
		f.IconID = iconID.String
	}
	f.IsBackup = isBackup == 1
	return &f, nil
}

// UpdateStageFile updates the name, folder, scene_json, device_count, kind
// and icon_id of an existing file. Only non-empty fields are updated —
// pass empty string to keep the current value. This allows rename-only,
// move-only, save-only, change-icon-only, and "convert-to-tutorial"-only
// operations without requiring the caller to fetch the full record first.
//
// Kind follows the same "empty means no-change" rule as name. Passing
// "stage" or "tutorial" updates the column; an empty string leaves it
// alone. This lets a client convert a regular stage file into a tutorial
// (or vice versa) without a custom endpoint.
//
// Two columns need a way to be CLEARED (not just changed) and use a
// sentinel since empty already means "no change":
//
//   - folderID == "__root__"  → move to root (set folder_id = NULL)
//   - iconID   == "__clear__" → reset icon to default (set icon_id = NULL)
//
// Language is INTENTIONALLY ABSENT from the update signature. The
// language of a project is fixed at creation and irreversible: a
// project is 100% Go or 100% C99 from the moment it is created.
// Switching mid-project would invalidate every device on the canvas
// (Go-only black-boxes would become incompatible; C-only future
// primitives would similarly stop working). If a maker wants to
// "convert" a project, they create a new project in the target
// language and rebuild — there is no shortcut.
//
// Português: Atualiza campos de um arquivo. "" = manter; "__root__" e
// "__clear__" são sentinelas para limpar folder_id e icon_id
// respectivamente. Language não está aqui — é fixa por design.
func UpdateStageFile(userID, fileID string, name, folderID, kind, sceneJSON, iconID string, deviceCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Build the SET clause dynamically so we only touch changed columns.
	setClauses := []string{"updated_at = ?"}
	args := []any{now}

	if name != "" {
		setClauses = append(setClauses, "name = ?")
		args = append(args, name)
	}
	if folderID == "__root__" {
		// Special sentinel: move to root (set folder_id to NULL).
		setClauses = append(setClauses, "folder_id = NULL")
	} else if folderID != "" {
		setClauses = append(setClauses, "folder_id = ?")
		args = append(args, folderID)
	}
	if kind != "" {
		setClauses = append(setClauses, "kind = ?")
		args = append(args, kind)
	}
	if sceneJSON != "" {
		setClauses = append(setClauses, "scene_json = ?", "device_count = ?")
		args = append(args, sceneJSON, deviceCount)
	}
	if iconID == "__clear__" {
		// Reset to NULL so the UI falls back to its default icon.
		setClauses = append(setClauses, "icon_id = NULL")
	} else if iconID != "" {
		setClauses = append(setClauses, "icon_id = ?")
		args = append(args, iconID)
	}

	args = append(args, fileID, userID)

	query := fmt.Sprintf(
		"UPDATE stage_files SET %s WHERE id = ? AND user_id = ?",
		joinStrings(setClauses, ", "),
	)

	res, err := DB.Exec(query, args...)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("a file with this name already exists in the target folder")
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("file not found")
	}
	return nil
}

// DeleteStageFile deletes a file. Returns an error if not found.
func DeleteStageFile(userID, fileID string) error {
	res, err := DB.Exec(
		`DELETE FROM stage_files WHERE id = ? AND user_id = ?`,
		fileID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("file not found")
	}
	return nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// isDuplicateError checks if an error is a UNIQUE constraint violation.
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// contains() is defined in users.go (same package).
	return contains(s, "UNIQUE constraint failed") || contains(s, "duplicate")
}

// NOTE: contains() and joinStrings() are defined in other files within this
// package (users.go and menu_sections.go respectively). They are reused here
// without redeclaration.
