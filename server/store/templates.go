// server/store/templates.go — Template Package persistence.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// A template package is a Go project published by a specialist on GitHub.
// The specialist submits a GitHub release URL; the worker downloads the ZIP,
// parses IDS structs and the full project, and stores the result.
// No files are stored on disk — only the parsed definition (def_json).
//
// Two-table design:
//
//	template_packages         — parent record (name, description, visibility,
//	                            GitHub fields, publishing flags).
//	template_package_versions — one row per GitHub release submission.
//	                            The worker promotes def_json / parse_errors /
//	                            status back to the parent after each parse.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"server/auth"
)

// ─── Package CRUD ─────────────────────────────────────────────────────────────

// CreateTemplatePkg inserts a new template_packages row with status=no_version.
// The specialist submits a GitHub release URL separately via the Templates tab.
func CreateTemplatePkg(pkg *TemplatePackage) error {
	now := time.Now().UTC()

	if pkg.ID == "" {
		id, err := auth.NewID()
		if err != nil {
			return err
		}
		pkg.ID = id
	}

	pkg.CreatedAt = now
	pkg.UpdatedAt = now
	pkg.Status = TemplatePkgStatusNoVersion
	if pkg.Visibility == "" {
		pkg.Visibility = TemplatePkgVisibilityPrivate
	}

	_, err := DB.Exec(`
		INSERT INTO template_packages
			(id, user_id, name, description,
			 visibility, status, latest_version,
			 github_url, github_owner, github_repo, github_tag,
			 tags, blocked, parse_errors,
			 display_name_human, category_id, subcategory_id,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, 0, '[]', ?, ?, ?, ?, ?)`,
		pkg.ID, pkg.UserID, pkg.Name, pkg.Description,
		pkg.Visibility, pkg.Status,
		pkg.GithubURL, pkg.GithubOwner, pkg.GithubRepo, pkg.GithubTag,
		pkg.Tags,
		pkg.DisplayNameHuman,
		nullableTemplateString(pkg.CategoryID),
		nullableTemplateString(pkg.SubcategoryID),
		pkg.CreatedAt.Format(time.RFC3339),
		pkg.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// nullableTemplateString returns nil for empty strings so optional FK columns
// (category_id, subcategory_id) store NULL rather than an empty string.
func nullableTemplateString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// GetTemplatePkg returns the full record for the given template ID.
// Returns ErrNotFound if no row exists with that ID.
func GetTemplatePkg(id string) (*TemplatePackage, error) {
	return scanTemplatePkg(DB.QueryRow(`
		SELECT id, user_id, name, description,
		       visibility, status, latest_version,
		       COALESCE(github_url,''),   COALESCE(github_owner,''),
		       COALESCE(github_repo,''),  COALESCE(github_tag,''),
		       COALESCE(tags,''),         COALESCE(blocked,0),
		       parse_errors,
		       COALESCE(publish_to_feed,0),   COALESCE(publish_to_search,0),
		       COALESCE(ready_to_use,0),
		       COALESCE(display_name_human,''), COALESCE(category_id,''), COALESCE(subcategory_id,''),
		       created_at, updated_at
		FROM   template_packages
		WHERE  id = ?`, id))
}

// GetTemplatePkgDef returns the raw def_json from the latest ready version.
// The def_json is produced by the worker: a JSON object containing parsed
// IDS structs and the full project file tree.
// Returns ErrNotFound if the template does not exist.
// Returns an error if status != "ready".
func GetTemplatePkgDef(id string) (json.RawMessage, error) {
	pkg, err := GetTemplatePkg(id)
	if err != nil {
		return nil, err
	}
	if pkg.Status != TemplatePkgStatusReady {
		return nil, errors.New("template package is not ready: status=" + pkg.Status)
	}
	var defJSON string
	err = DB.QueryRow(`
		SELECT def_json FROM template_package_versions
		WHERE  pkg_id  = ?
		  AND  version = ?`,
		id, pkg.LatestVersion,
	).Scan(&defJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("template version not found")
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(defJSON), nil
}

// ListTemplatePkgsByUser returns all template packages owned by userID,
// newest first. All statuses are included.
func ListTemplatePkgsByUser(userID string) ([]TemplatePackage, error) {
	return queryTemplatePkgs(`
		SELECT id, user_id, name, description,
		       visibility, status, latest_version,
		       COALESCE(github_url,''),   COALESCE(github_owner,''),
		       COALESCE(github_repo,''),  COALESCE(github_tag,''),
		       COALESCE(tags,''),         COALESCE(blocked,0),
		       parse_errors,
		       COALESCE(publish_to_feed,0),   COALESCE(publish_to_search,0),
		       COALESCE(ready_to_use,0),
		       COALESCE(display_name_human,''), COALESCE(category_id,''), COALESCE(subcategory_id,''),
		       created_at, updated_at
		FROM   template_packages
		WHERE  user_id = ?
		ORDER  BY updated_at DESC`, userID)
}

// ListPublicTemplatePkgs returns all ready, public, non-blocked template
// packages, newest first.
func ListPublicTemplatePkgs() ([]TemplatePackage, error) {
	return queryTemplatePkgs(`
		SELECT id, user_id, name, description,
		       visibility, status, latest_version,
		       COALESCE(github_url,''),   COALESCE(github_owner,''),
		       COALESCE(github_repo,''),  COALESCE(github_tag,''),
		       COALESCE(tags,''),         COALESCE(blocked,0),
		       parse_errors,
		       COALESCE(publish_to_feed,0),   COALESCE(publish_to_search,0),
		       COALESCE(ready_to_use,0),
		       COALESCE(display_name_human,''), COALESCE(category_id,''), COALESCE(subcategory_id,''),
		       created_at, updated_at
		FROM   template_packages
		WHERE  visibility          = 'public'
		  AND  status              = 'ready'
		  AND  COALESCE(blocked,0) = 0
		ORDER  BY updated_at DESC`)
}

// UpdateTemplatePkgVisibility changes the visibility of a template package.
// Only the owner can change it. Returns ErrNotFound if no row matched.
func UpdateTemplatePkgVisibility(id, userID, visibility string) error {
	res, err := DB.Exec(`
		UPDATE template_packages
		SET    visibility  = ?,
		       updated_at = datetime('now')
		WHERE  id      = ?
		  AND  user_id = ?`,
		visibility, id, userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// UpdateTemplatePkgTags updates the tags field on a template package.
func UpdateTemplatePkgTags(id, userID, tags string) error {
	_, err := DB.Exec(`
		UPDATE template_packages
		SET    tags       = ?,
		       updated_at = datetime('now')
		WHERE  id      = ?
		  AND  user_id = ?`,
		tags, id, userID,
	)
	return err
}

// UpdateTemplatePkgMeta is called by the worker after parsing a GitHub release.
// It sets the human-readable display name (from readme.md first # heading),
// the search tags, and the IDE menu category/subcategory.
// Uses the package ID only (no user_id check) because it is called from the
// worker process which does not have a user context.
// UpdateTemplatePkgMeta updates the display name, tags, and menu placement for
// a template package.
//
// When displayNameHuman is empty (""), the name column is NOT updated — this
// preserves the name the specialist set at creation time when re-submitting a
// new version. The name is always set on first creation (handleCreate passes
// pkgName which defaults to the repo slug at minimum).
func UpdateTemplatePkgMeta(id, displayNameHuman, tags, categoryID, subcategoryID string) error {
	if displayNameHuman == "" {
		// Re-submit path: only update tags and category, leave name untouched.
		_, err := DB.Exec(`
			UPDATE template_packages
			SET    tags               = ?,
			       category_id        = ?,
			       subcategory_id     = ?,
			       updated_at         = datetime('now')
			WHERE  id = ?`,
			tags,
			nullableTemplateString(categoryID),
			nullableTemplateString(subcategoryID),
			id,
		)
		return err
	}
	_, err := DB.Exec(`
		UPDATE template_packages
		SET    display_name_human = ?,
		       tags               = ?,
		       category_id        = ?,
		       subcategory_id     = ?,
		       updated_at         = datetime('now')
		WHERE  id = ?`,
		displayNameHuman, tags,
		nullableTemplateString(categoryID),
		nullableTemplateString(subcategoryID),
		id,
	)
	return err
}

// UpdateTemplatePkgPublishing sets the three community publishing flags.
// Business rules (visibility=public AND status=ready) are enforced at the
// handler layer. Returns ErrNotFound if no row matched.
func UpdateTemplatePkgPublishing(id, userID string, upd *TemplatePublishingUpdate) error {
	res, err := DB.Exec(`
		UPDATE template_packages
		SET    publish_to_feed   = ?,
		       publish_to_search = ?,
		       ready_to_use      = ?,
		       updated_at        = datetime('now')
		WHERE  id      = ?
		  AND  user_id = ?`,
		boolToInt(upd.PublishToFeed),
		boolToInt(upd.PublishToSearch),
		boolToInt(upd.ReadyToUse),
		id, userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// DeleteTemplatePkg removes the template_packages row and cascades to versions.
// Returns ErrNotFound if no matching row exists.
func DeleteTemplatePkg(id, userID string) error {
	res, err := DB.Exec(`
		DELETE FROM template_packages
		WHERE id      = ?
		  AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Version CRUD ─────────────────────────────────────────────────────────────

// CreateTemplatePkgVersionGitHub inserts a new template_package_versions row
// sourced from a GitHub release (no ZIP file).
// Advances the parent status to "pending" and updates latest_version.
func CreateTemplatePkgVersionGitHub(v *TemplatePackageVersion) error {
	now := time.Now().UTC()

	if v.ID == "" {
		id, err := auth.NewID()
		if err != nil {
			return err
		}
		v.ID = id
	}
	v.CreatedAt = now

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var maxVer int
	err = tx.QueryRow(`
		SELECT COALESCE(MAX(version), 0)
		FROM   template_package_versions
		WHERE  pkg_id = ?`, v.PkgID).Scan(&maxVer)
	if err != nil {
		return err
	}
	v.Version = maxVer + 1

	_, err = tx.Exec(`
		INSERT INTO template_package_versions
			(id, pkg_id, user_id, version, github_url, github_tag,
			 def_json, parse_errors, created_at)
		VALUES (?, ?, ?, ?, ?, ?, '{}', '[]', ?)`,
		v.ID, v.PkgID, v.UserID, v.Version,
		v.GithubURL, v.GithubTag,
		v.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isSQLiteConstraint(err) {
			return ErrConflict
		}
		return err
	}

	res, err := tx.Exec(`
		UPDATE template_packages
		SET    status         = 'pending',
		       latest_version = ?,
		       github_url     = ?,
		       github_tag     = ?,
		       parse_errors   = '[]',
		       updated_at     = ?
		WHERE  id = ?`,
		v.Version, v.GithubURL, v.GithubTag,
		now.Format(time.RFC3339),
		v.PkgID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	return tx.Commit()
}

// GetNextTemplateVersionNumber returns MAX(version)+1 for the given package.
// Returns 1 if no versions exist yet.
func GetNextTemplateVersionNumber(pkgID string) (int, error) {
	var maxVer int
	err := DB.QueryRow(`
		SELECT COALESCE(MAX(version), 0)
		FROM   template_package_versions
		WHERE  pkg_id = ?`, pkgID).Scan(&maxVer)
	if err != nil {
		return 0, err
	}
	return maxVer + 1, nil
}

// UpdateTemplatePkgVersionReady is called by the worker after a successful parse.
//
// defJSON is the serialised template definition produced by the worker.
// Writes def_json and parse_errors to the version row, then promotes
// status=ready to the parent row (only if this is still the latest version).
func UpdateTemplatePkgVersionReady(versionID string, defJSON string, parseWarnings []string) error {
	if defJSON == "" {
		return errors.New("store.UpdateTemplatePkgVersionReady: defJSON must not be empty")
	}

	warnings := parseWarnings
	if warnings == nil {
		warnings = []string{}
	}
	errorsJSON, err := json.Marshal(warnings)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`
		UPDATE template_package_versions
		SET    def_json     = ?,
		       parse_errors = ?
		WHERE  id = ?`,
		defJSON, string(errorsJSON), versionID,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE template_packages
		SET    status       = 'ready',
		       parse_errors = ?,
		       updated_at   = ?
		WHERE  id = (
		    SELECT pkg_id FROM template_package_versions WHERE id = ?
		)
		AND latest_version = (
		    SELECT version FROM template_package_versions WHERE id = ?
		)`,
		string(errorsJSON),
		now.Format(time.RFC3339),
		versionID, versionID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// UpdateTemplatePkgVersionError is called by the worker when parsing fails.
// Records fatal errors and promotes status=error to the parent row.
func UpdateTemplatePkgVersionError(versionID string, parseErrors []string) error {
	errs := parseErrors
	if errs == nil {
		errs = []string{}
	}
	errorsJSON, err := json.Marshal(errs)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`
		UPDATE template_package_versions
		SET    parse_errors = ?
		WHERE  id = ?`,
		string(errorsJSON), versionID,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE template_packages
		SET    status       = 'error',
		       parse_errors = ?,
		       updated_at   = ?
		WHERE  id = (
		    SELECT pkg_id FROM template_package_versions WHERE id = ?
		)
		AND latest_version = (
		    SELECT version FROM template_package_versions WHERE id = ?
		)`,
		string(errorsJSON),
		now.Format(time.RFC3339),
		versionID, versionID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetTemplatePkgVersionByID returns a single version row by its primary key.
// Returns ErrNotFound if no matching row exists.
func GetTemplatePkgVersionByID(id string) (*TemplatePackageVersion, error) {
	var v TemplatePackageVersion
	var defJSON, errorsJSON, createdAt string

	err := DB.QueryRow(`
		SELECT id, pkg_id, user_id, version,
		       COALESCE(github_url,''), COALESCE(github_tag,''),
		       def_json, parse_errors, created_at
		FROM   template_package_versions
		WHERE  id = ?`, id).Scan(
		&v.ID, &v.PkgID, &v.UserID, &v.Version,
		&v.GithubURL, &v.GithubTag,
		&defJSON, &errorsJSON, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	v.DefJSON = defJSON
	if errorsJSON != "" && errorsJSON != "[]" {
		_ = json.Unmarshal([]byte(errorsJSON), &v.ParseErrors)
	}
	if v.ParseErrors == nil {
		v.ParseErrors = []string{}
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &v, nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// scanTemplatePkg scans a single *sql.Row into a TemplatePackage.
// Column order must match the SELECT in GetTemplatePkg and queryTemplatePkgs.
func scanTemplatePkg(row *sql.Row) (*TemplatePackage, error) {
	var pkg TemplatePackage
	var errorsJSON, createdAt, updatedAt string
	var publishToFeed, publishToSearch, readyToUse int

	err := row.Scan(
		&pkg.ID, &pkg.UserID, &pkg.Name, &pkg.Description,
		&pkg.Visibility, &pkg.Status, &pkg.LatestVersion,
		&pkg.GithubURL, &pkg.GithubOwner, &pkg.GithubRepo, &pkg.GithubTag,
		&pkg.Tags, &pkg.Blocked,
		&errorsJSON,
		&publishToFeed, &publishToSearch, &readyToUse,
		&pkg.DisplayNameHuman, &pkg.CategoryID, &pkg.SubcategoryID,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if errorsJSON != "" && errorsJSON != "[]" {
		_ = json.Unmarshal([]byte(errorsJSON), &pkg.ParseErrors)
	}
	if pkg.ParseErrors == nil {
		pkg.ParseErrors = []string{}
	}

	pkg.PublishToFeed = publishToFeed == 1
	pkg.PublishToSearch = publishToSearch == 1
	pkg.ReadyToUse = readyToUse == 1

	pkg.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	pkg.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &pkg, nil
}

// queryTemplatePkgs executes a query and scans all rows into a TemplatePackage slice.
func queryTemplatePkgs(query string, args ...interface{}) ([]TemplatePackage, error) {
	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pkgs []TemplatePackage
	for rows.Next() {
		var pkg TemplatePackage
		var errorsJSON, createdAt, updatedAt string
		var publishToFeed, publishToSearch, readyToUse int

		if err := rows.Scan(
			&pkg.ID, &pkg.UserID, &pkg.Name, &pkg.Description,
			&pkg.Visibility, &pkg.Status, &pkg.LatestVersion,
			&pkg.GithubURL, &pkg.GithubOwner, &pkg.GithubRepo, &pkg.GithubTag,
			&pkg.Tags, &pkg.Blocked,
			&errorsJSON,
			&publishToFeed, &publishToSearch, &readyToUse,
			&pkg.DisplayNameHuman, &pkg.CategoryID, &pkg.SubcategoryID,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}

		if errorsJSON != "" && errorsJSON != "[]" {
			_ = json.Unmarshal([]byte(errorsJSON), &pkg.ParseErrors)
		}
		if pkg.ParseErrors == nil {
			pkg.ParseErrors = []string{}
		}

		pkg.PublishToFeed = publishToFeed == 1
		pkg.PublishToSearch = publishToSearch == 1
		pkg.ReadyToUse = readyToUse == 1

		pkg.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		pkg.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

		pkgs = append(pkgs, pkg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if pkgs == nil {
		pkgs = []TemplatePackage{}
	}
	return pkgs, nil
}
