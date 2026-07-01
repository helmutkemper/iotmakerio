// store/projects.go — Project CRUD, lookup-table queries, and code version management.
//
// This file is limited to database operations only.
// Filesystem operations live in handler/projectapi to keep concerns separated.
//
// NOTE on SELECT column order:
//
//	All project queries SELECT columns in the fixed order below so that
//	scanProject / scanProjectRow only need to be maintained in one place.
//	If you add a column here you MUST update both scan helpers.
//
//	Current order (24 columns):
//	  p.id, p.user_id, p.name, p.type, p.visibility,
//	  p.programming_language_id, p.ui_language_id,
//	  p.created_at, p.updated_at,
//	  p.card_title, p.card_image, p.card_description, p.card_keywords,
//	  p.category_id, p.subcategory_id,
//	  p.publish_to_feed, p.publish_to_search, p.ready_to_use,
//	  pl.name, pl.display, pl.sort_order,
//	  ul.code, ul.display, ul.sort_order
package store

import (
	"database/sql"
	"errors"
	"time"
)

// projectSelectCols is the fixed column list shared by all project SELECT queries.
// Keeping it as a constant prevents the queries from drifting out of sync with
// the scan helpers.
const projectSelectCols = `
		p.id, p.user_id, p.name, p.type, p.visibility,
		p.programming_language_id, p.ui_language_id,
		p.created_at, p.updated_at,
		p.card_title, p.card_image, p.card_description, p.card_keywords,
		p.category_id, p.subcategory_id,
		p.publish_to_feed, p.publish_to_search, p.ready_to_use,
		pl.name, pl.display, pl.sort_order,
		ul.code, ul.display, ul.sort_order`

// projectJoinCols is the FROM / JOIN block reused by all project SELECT queries.
const projectJoinCols = `
		FROM projects p
		JOIN programming_languages pl ON pl.id = p.programming_language_id
		JOIN project_ui_languages  ul ON ul.id = p.ui_language_id`

// ─── Project: Create ──────────────────────────────────────────────────────────

// CreateProject inserts a new project row.
// Publishing flags are intentionally excluded — a newly-created project is
// never published. The owner sets them later via handleUpdateProject.
// Returns ErrConflict if the user already has a project with the same name.
func CreateProject(p *Project) error {
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now

	_, err := DB.Exec(`
		INSERT INTO projects
			(id, user_id, name, type, visibility,
			 programming_language_id, ui_language_id,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.Name, p.Type, p.Visibility,
		p.ProgrammingLanguageID, p.UILanguageID,
		p.CreatedAt.Format(time.RFC3339),
		p.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// ─── Project: Read ────────────────────────────────────────────────────────────

// GetProjectByID returns the project with the given ID regardless of owner.
// Returns ErrNotFound if no such project exists.
func GetProjectByID(id string) (*Project, error) {
	row := DB.QueryRow(
		`SELECT`+projectSelectCols+projectJoinCols+`
		WHERE p.id = ?`, id)
	return scanProject(row)
}

// GetProjectByIDAndUser returns the project only if it belongs to the given user.
// Returns ErrNotFound if the project does not exist or belongs to a different user.
func GetProjectByIDAndUser(id, userID string) (*Project, error) {
	row := DB.QueryRow(
		`SELECT`+projectSelectCols+projectJoinCols+`
		WHERE p.id = ? AND p.user_id = ?`, id, userID)
	return scanProject(row)
}

// ListProjectsByUser returns all projects for the given user, ordered by
// programming language then project name to match the sidebar tree.
func ListProjectsByUser(userID string) ([]*Project, error) {
	rows, err := DB.Query(
		`SELECT`+projectSelectCols+projectJoinCols+`
		WHERE p.user_id = ?
		ORDER BY pl.sort_order ASC, pl.display ASC, p.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// ─── Project: Update ──────────────────────────────────────────────────────────

// UpdateProject applies the fields in upd to the project identified by id and
// owned by userID.  The caller (handler) is responsible for:
//   - Validating name length and forbidden characters.
//   - Setting all publish flags to false when visibility is "private".
//
// Returns ErrConflict when the new name collides with another project owned
// by the same user (DB UNIQUE constraint on user_id, name).
// Returns ErrNotFound when no row is affected (wrong id or wrong owner).
func UpdateProject(id, userID string, upd *ProjectUpdate) error {
	res, err := DB.Exec(`
		UPDATE projects SET
			name             = ?,
			visibility       = ?,
			publish_to_feed   = ?,
			publish_to_search = ?,
			ready_to_use      = ?,
			updated_at        = datetime('now')
		WHERE id = ? AND user_id = ?`,
		upd.Name,
		upd.Visibility,
		boolToInt(upd.PublishToFeed),
		boolToInt(upd.PublishToSearch),
		boolToInt(upd.ReadyToUse),
		id, userID,
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return requireAffected(res)
}

// ─── Project: Delete ──────────────────────────────────────────────────────────

// DeleteProject removes the project row (and its code versions via CASCADE).
// The userID check prevents cross-user deletion.
// The caller MUST also delete the project directory from the filesystem.
func DeleteProject(id, userID string) error {
	res, err := DB.Exec(
		`DELETE FROM projects WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Lookup Tables ────────────────────────────────────────────────────────────

// GetProgrammingLanguages returns all languages ordered by sort_order then name.
func GetProgrammingLanguages() ([]*ProgrammingLanguage, error) {
	rows, err := DB.Query(`
		SELECT id, name, display, sort_order
		FROM programming_languages
		ORDER BY sort_order ASC, display ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var langs []*ProgrammingLanguage
	for rows.Next() {
		var l ProgrammingLanguage
		if err := rows.Scan(&l.ID, &l.Name, &l.Display, &l.SortOrder); err != nil {
			return nil, err
		}
		langs = append(langs, &l)
	}
	return langs, rows.Err()
}

// GetProjectUILanguages returns all UI languages ordered by sort_order then display.
func GetProjectUILanguages() ([]*ProjectUILanguage, error) {
	rows, err := DB.Query(`
		SELECT id, code, display, sort_order
		FROM project_ui_languages
		ORDER BY sort_order ASC, display ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var langs []*ProjectUILanguage
	for rows.Next() {
		var l ProjectUILanguage
		if err := rows.Scan(&l.ID, &l.Code, &l.Display, &l.SortOrder); err != nil {
			return nil, err
		}
		langs = append(langs, &l)
	}
	return langs, rows.Err()
}

// ValidateProgrammingLanguageID returns ErrNotFound if the id is not in the table.
func ValidateProgrammingLanguageID(id string) error {
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM programming_languages WHERE id = ?`, id,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// ValidateUILanguageID returns ErrNotFound if the id is not in the table.
func ValidateUILanguageID(id string) error {
	var count int
	err := DB.QueryRow(
		`SELECT COUNT(*) FROM project_ui_languages WHERE id = ?`, id,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Project Code Versions ────────────────────────────────────────────────────

// CreateProjectCodeVersion inserts a new code version row.
// The version number must be set by the caller via GetNextCodeVersionNumber.
// Returns ErrConflict on duplicate (project_id, version).
func CreateProjectCodeVersion(v *ProjectCodeVersion) error {
	now := time.Now().UTC()
	v.CreatedAt = now

	// last_parse_ok is stored as INTEGER (0/1) — SQLite has no
	// dedicated boolean type. The Go side keeps it as bool and
	// we convert at the boundary.
	parseOk := 0
	if v.LastParseOk {
		parseOk = 1
	}

	_, err := DB.Exec(`
		INSERT INTO project_code_versions
			(id, project_id, user_id, version, filename, source, last_parse_ok, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.ProjectID, v.UserID, v.Version,
		v.Filename, v.Source, parseOk,
		v.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// GetLatestProjectCodeVersion returns the highest-version record for the project.
// Returns ErrNotFound if the project has no saved versions yet.
func GetLatestProjectCodeVersion(projectID string) (*ProjectCodeVersion, error) {
	var v ProjectCodeVersion
	var createdAt string
	var parseOk int
	err := DB.QueryRow(`
		SELECT id, project_id, user_id, version, filename, source, last_parse_ok, created_at
		FROM project_code_versions
		WHERE project_id = ?
		ORDER BY version DESC
		LIMIT 1`, projectID).Scan(
		&v.ID, &v.ProjectID, &v.UserID,
		&v.Version, &v.Filename, &v.Source, &parseOk, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.LastParseOk = parseOk != 0
	v.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &v, nil
}

// GetProjectCodeVersionByID returns a specific version by its primary key.
// Returns ErrNotFound if not found.
func GetProjectCodeVersionByID(id string) (*ProjectCodeVersion, error) {
	var v ProjectCodeVersion
	var createdAt string
	var parseOk int
	err := DB.QueryRow(`
		SELECT id, project_id, user_id, version, filename, source, last_parse_ok, created_at
		FROM project_code_versions
		WHERE id = ?`, id).Scan(
		&v.ID, &v.ProjectID, &v.UserID,
		&v.Version, &v.Filename, &v.Source, &parseOk, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.LastParseOk = parseOk != 0
	v.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &v, nil
}

// ListProjectCodeVersions returns all versions for a project, newest first.
// The full Source field is included so the diff tool can compare any two versions
// without a second round-trip. This is the same pattern used by the blackbox list.
func ListProjectCodeVersions(projectID string) ([]*ProjectCodeVersion, error) {
	rows, err := DB.Query(`
		SELECT id, project_id, user_id, version, filename, source, last_parse_ok, created_at
		FROM project_code_versions
		WHERE project_id = ?
		ORDER BY version DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []*ProjectCodeVersion
	for rows.Next() {
		var v ProjectCodeVersion
		var createdAt string
		var parseOk int
		if err := rows.Scan(
			&v.ID, &v.ProjectID, &v.UserID,
			&v.Version, &v.Filename, &v.Source, &parseOk, &createdAt,
		); err != nil {
			return nil, err
		}
		v.LastParseOk = parseOk != 0
		v.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		versions = append(versions, &v)
	}
	return versions, rows.Err()
}

// GetNextCodeVersionNumber returns MAX(version)+1 for the project.
// Returns 1 if no versions exist yet.
// This must be called immediately before CreateProjectCodeVersion to minimise
// the window for a race condition (mitigated by the UNIQUE constraint).
func GetNextCodeVersionNumber(projectID string) (int, error) {
	var maxVer int
	err := DB.QueryRow(`
		SELECT COALESCE(MAX(version), 0)
		FROM project_code_versions
		WHERE project_id = ?`, projectID).Scan(&maxVer)
	if err != nil {
		return 0, err
	}
	return maxVer + 1, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// scanProject scans a *sql.Row into a Project, populating the joined
// ProgrammingLanguage and UILanguage relations.
// Column order must match projectSelectCols exactly.
func scanProject(row *sql.Row) (*Project, error) {
	var (
		p                    Project
		pl                   ProgrammingLanguage
		ul                   ProjectUILanguage
		createdAt, updatedAt string
		catID, subID         sql.NullString
		publishToFeed        int
		publishToSearch      int
		readyToUse           int
	)
	err := row.Scan(
		&p.ID, &p.UserID, &p.Name, &p.Type, &p.Visibility,
		&p.ProgrammingLanguageID, &p.UILanguageID,
		&createdAt, &updatedAt,
		&p.CardTitle, &p.CardImage, &p.CardDescription, &p.CardKeywords,
		&catID, &subID,
		&publishToFeed, &publishToSearch, &readyToUse,
		&pl.Name, &pl.Display, &pl.SortOrder,
		&ul.Code, &ul.Display, &ul.SortOrder,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	pl.ID = p.ProgrammingLanguageID
	ul.ID = p.UILanguageID
	p.ProgrammingLanguage = &pl
	p.UILanguage = &ul
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	// Nullable taxonomy FK columns.
	if catID.Valid {
		p.CategoryID = catID.String
	}
	if subID.Valid {
		p.SubcategoryID = subID.String
	}

	// SQLite stores booleans as INTEGER (0/1).
	p.PublishToFeed = publishToFeed == 1
	p.PublishToSearch = publishToSearch == 1
	p.ReadyToUse = readyToUse == 1

	return &p, nil
}

// scanProjectRow scans a *sql.Rows cursor into a Project.
// Column order must match projectSelectCols exactly.
func scanProjectRow(rows *sql.Rows) (*Project, error) {
	var (
		p                    Project
		pl                   ProgrammingLanguage
		ul                   ProjectUILanguage
		createdAt, updatedAt string
		catID, subID         sql.NullString
		publishToFeed        int
		publishToSearch      int
		readyToUse           int
	)
	err := rows.Scan(
		&p.ID, &p.UserID, &p.Name, &p.Type, &p.Visibility,
		&p.ProgrammingLanguageID, &p.UILanguageID,
		&createdAt, &updatedAt,
		&p.CardTitle, &p.CardImage, &p.CardDescription, &p.CardKeywords,
		&catID, &subID,
		&publishToFeed, &publishToSearch, &readyToUse,
		&pl.Name, &pl.Display, &pl.SortOrder,
		&ul.Code, &ul.Display, &ul.SortOrder,
	)
	if err != nil {
		return nil, err
	}
	pl.ID = p.ProgrammingLanguageID
	ul.ID = p.UILanguageID
	p.ProgrammingLanguage = &pl
	p.UILanguage = &ul
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	// Nullable taxonomy FK columns.
	if catID.Valid {
		p.CategoryID = catID.String
	}
	if subID.Valid {
		p.SubcategoryID = subID.String
	}

	// SQLite stores booleans as INTEGER (0/1).
	p.PublishToFeed = publishToFeed == 1
	p.PublishToSearch = publishToSearch == 1
	p.ReadyToUse = readyToUse == 1

	return &p, nil
}

// ─── IDE listing ─────────────────────────────────────────────────────────────

// LatestProjectCode is the source code of the most recent saved version of a
// project. Used by the WASM IDE listing endpoint (GET /api/v1/blackbox) to
// build the component bank without reading the legacy blackboxes table.
type LatestProjectCode struct {
	ProjectID string
	Name      string // project name — used as display name in the IDE
	Source    string // Go source code from the latest project_code_versions row
	// Language is the project's programming-language token, taken directly
	// from projects.programming_language_id (e.g. "golang", "c"). The list
	// endpoint dispatches the parser on this — a C99 source must be parsed
	// by ParseC, not the Go parser, or it fails and the device silently
	// disappears from the catalog. NOT yet normalized to the stage token
	// space ("go"/"c"); the handler normalizes when stamping the DTO.
	Language string
}

// ListLatestProjectCodeVersions returns the most recent code version for every
// project that has at least one saved version, across all users.
//
// Only projects whose latest code version is non-empty are included — projects
// that were created but never had any code saved are silently skipped.
//
// The result is ordered by project name for deterministic IDE menu ordering.
// ListLatestProjectCodeVersions returns the most recent code version for
// each of the given user's projects. callerID MUST be non-empty — passing
// an empty string returns nil (fail-secure to prevent cross-user data leaks).
//
// For internal codegen use where ALL projects must be scanned (e.g., resolving
// a blackbox struct reference while compiling a scene), use the separate
// ListAllLatestProjectCodeVersions function which makes the "all" intent
// explicit.
func ListLatestProjectCodeVersions(callerID string) ([]*LatestProjectCode, error) {
	// Fail secure: an empty callerID must never return other users' data.
	if callerID == "" {
		return nil, nil
	}

	rows, err := DB.Query(`
		SELECT p.id, p.name, p.programming_language_id, pcv.source
		FROM projects p
		JOIN project_code_versions pcv ON pcv.project_id = p.id
		  AND pcv.version = (
		      SELECT MAX(v2.version)
		      FROM project_code_versions v2
		      WHERE v2.project_id = p.id
		  )
		WHERE pcv.source != ''
		  AND p.user_id = ?
		ORDER BY p.name ASC`, callerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*LatestProjectCode
	for rows.Next() {
		var item LatestProjectCode
		if err := rows.Scan(&item.ProjectID, &item.Name, &item.Language, &item.Source); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

// ListAllLatestProjectCodeVersions returns the most recent code version for
// EVERY project across all users. Reserved for internal server-side codegen
// operations that need to resolve BlackBox struct references regardless of
// ownership (e.g., a scene by user A referencing a blackbox defined by user B).
//
// Do NOT call this from any HTTP handler — it bypasses user isolation and
// would leak private source code if exposed.
func ListAllLatestProjectCodeVersions() ([]*LatestProjectCode, error) {
	rows, err := DB.Query(`
		SELECT p.id, p.name, COALESCE(p.programming_language_id,''), pcv.source
		FROM projects p
		JOIN project_code_versions pcv ON pcv.project_id = p.id
		  AND pcv.version = (
		      SELECT MAX(v2.version)
		      FROM project_code_versions v2
		      WHERE v2.project_id = p.id
		  )
		WHERE pcv.source != ''
		ORDER BY p.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*LatestProjectCode
	for rows.Next() {
		var item LatestProjectCode
		// Language rides along so callers can dispatch the right parser
		// (ParseC for "c", the Go parser otherwise). Without it a C99
		// black-box source is parsed as Go, fails, and is dropped — the
		// exact reason C99 function-devices never reached codegen.
		if err := rows.Scan(&item.ProjectID, &item.Name, &item.Language, &item.Source); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

// ─── Project Card ─────────────────────────────────────────────────────────────

// UpdateProjectCard persists the card fields extracted from readme.md frontmatter.
//
// The function performs a targeted UPDATE limited to the card and taxonomy
// columns — it never touches name, visibility, publishing flags, or other
// project metadata.
//
// When CategoryID or SubcategoryID is empty string, the corresponding column
// is set to NULL so that the project appears as "uncategorised" in the feed.
//
// This function is called by handleUpdateMarkdown whenever the file being saved
// is the protected readme.md.
func UpdateProjectCard(projectID string, card *ProjectCard) error {
	// Convert empty strings to nil for nullable FK columns.
	var catID, subID interface{}
	if card.CategoryID != "" {
		catID = card.CategoryID
	}
	if card.SubcategoryID != "" {
		subID = card.SubcategoryID
	}

	_, err := DB.Exec(`
		UPDATE projects SET
			card_title       = ?,
			card_image       = ?,
			card_description = ?,
			card_keywords    = ?,
			category_id      = ?,
			subcategory_id   = ?,
			updated_at       = datetime('now')
		WHERE id = ?`,
		card.CardTitle,
		card.CardImage,
		card.CardDescription,
		card.CardKeywords,
		catID,
		subID,
		projectID,
	)
	return err
}
