// server/store/blackbox.go — Device (black-box) persistence for the IoTMaker portal.
//
// A device is an IDS-annotated Go struct published by a specialist on GitHub.
// The specialist submits a GitHub release URL; the worker downloads the release
// ZIP, finds every .go file with IDS tags, parses each struct, and calls
// UpsertDevice for each one found.
//
// One GitHub repository can produce multiple device rows — one per IDS struct.
// All rows from the same repo share the same github_url.
//
// The server never stores Go source code — only the parsed definition (parsed_json).
// If a re-parse is needed, the worker downloads from GitHub again.
//
// LoadBlackBoxDefsForScene is still needed by the codegen handler to resolve
// device definitions when generating code for a maker's scene.
// It reads from two sources in priority order:
//  1. project_code_versions — the Monaco editor (maker's own project code)
//  2. blackboxes — GitHub-sourced devices (parsed_json, not source code)
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	cryptoauth "server/auth"
	bbparser "server/codegen/blackbox"
)

// ─── Model ────────────────────────────────────────────────────────────────────

// Device is the GitHub-sourced device record stored in the blackboxes table.
type Device struct {
	ID              string `json:"id"`
	UserID          string `json:"userId"`
	GithubURL       string `json:"githubUrl"`
	GithubOwner     string `json:"githubOwner"`
	GithubRepo      string `json:"githubRepo"`
	GithubTag       string `json:"githubTag"`
	DisplayName     string `json:"displayName"` // struct name, e.g. "TemperatureSensor"
	Tags            string `json:"tags"`        // comma-separated, e.g. "math,signal"
	Blocked         int    `json:"blocked"`     // 0 = active, 1 = blocked by admin
	Status          string `json:"status"`      // "pending" | "ready" | "error"
	Visibility      string `json:"visibility"`  // "public" | "private"
	PublishToFeed   bool   `json:"publishToFeed"`
	PublishToSearch bool   `json:"publishToSearch"`
	ReadyToUse      bool   `json:"readyToUse"`
	// DisplayNameHuman is extracted from the first # heading in readme.md inside
	// the GitHub release ZIP. Falls back to "owner/repo" when no readme.md exists.
	DisplayNameHuman string `json:"displayNameHuman"`
	CategoryID       string `json:"categoryId,omitempty"`
	SubcategoryID    string `json:"subcategoryId,omitempty"`
	// ProgrammingLanguageID is the source language token for this device,
	// matching programming_languages.id ("golang", "c", …). Set by the import
	// worker. The list endpoint reads it to stamp the client DTO so the WASM
	// menu can hide devices of another language. Go-only today (the worker
	// parses only .go files), so the worker stamps "golang".
	ProgrammingLanguageID string    `json:"programmingLanguageId,omitempty"`
	ParsedJSON            string    `json:"parsedJson"` // BlackBoxDef serialised by the worker
	ParseErrors           []string  `json:"parseErrors"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

// DeviceSummary is the lean view used by list endpoints.
type DeviceSummary struct {
	ID               string    `json:"id"`
	UserID           string    `json:"userId"`
	GithubURL        string    `json:"githubUrl"`
	GithubOwner      string    `json:"githubOwner"`
	GithubRepo       string    `json:"githubRepo"`
	GithubTag        string    `json:"githubTag"`
	DisplayName      string    `json:"displayName"`
	Tags             string    `json:"tags"`
	Blocked          int       `json:"blocked"`
	Status           string    `json:"status"`
	Visibility       string    `json:"visibility"` // "public" | "private"
	PublishToFeed    bool      `json:"publishToFeed"`
	PublishToSearch  bool      `json:"publishToSearch"`
	ReadyToUse       bool      `json:"readyToUse"`
	DisplayNameHuman string    `json:"displayNameHuman"`
	CategoryID       string    `json:"categoryId,omitempty"`
	SubcategoryID    string    `json:"subcategoryId,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// ─── Write ────────────────────────────────────────────────────────────────────

// UpsertDevice inserts a new device or updates the existing one that matches
// (user_id, github_owner, github_repo, display_name). Called by the worker
// once per IDS struct found in the GitHub release ZIP.
// If d.ID is empty, a new ID is generated.
func UpsertDevice(d *Device) error {
	now := time.Now().UTC()
	if d.ID == "" {
		id, err := cryptoauth.NewID()
		if err != nil {
			return err
		}
		d.ID = id
		d.CreatedAt = now
	}
	d.UpdatedAt = now

	parseErrorsJSON, _ := json.Marshal(d.ParseErrors)

	var userIDVal any
	if d.UserID != "" {
		userIDVal = d.UserID
	}

	_, err := DB.Exec(`
		INSERT INTO blackboxes
			(id, user_id, github_url, github_owner, github_repo, github_tag,
			 display_name, tags, blocked, status, parsed_json, parse_errors,
			 visibility, display_name_human, category_id, subcategory_id,
			 programming_language_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			github_url              = excluded.github_url,
			github_tag              = excluded.github_tag,
			display_name            = excluded.display_name,
			tags                    = excluded.tags,
			status                  = excluded.status,
			parsed_json             = excluded.parsed_json,
			parse_errors            = excluded.parse_errors,
			display_name_human      = excluded.display_name_human,
			category_id             = excluded.category_id,
			subcategory_id          = excluded.subcategory_id,
			programming_language_id = excluded.programming_language_id,
			updated_at              = excluded.updated_at`,
		d.ID, userIDVal,
		d.GithubURL, d.GithubOwner, d.GithubRepo, d.GithubTag,
		d.DisplayName, d.Tags,
		d.Status, d.ParsedJSON, string(parseErrorsJSON),
		d.Visibility, d.DisplayNameHuman,
		nullableString(d.CategoryID), nullableString(d.SubcategoryID),
		nullableString(d.ProgrammingLanguageID),
		d.CreatedAt.Format(time.RFC3339), d.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// UpdateDeviceReady marks a device as successfully parsed.
// Called by the worker after parsing the GitHub release ZIP.
func UpdateDeviceReady(id, displayName, parsedJSON string, parseErrors []string) error {
	errJSON, _ := json.Marshal(parseErrors)
	_, err := DB.Exec(`
		UPDATE blackboxes
		SET    display_name = ?,
		       status       = 'ready',
		       parsed_json  = ?,
		       parse_errors = ?,
		       updated_at   = datetime('now')
		WHERE  id = ?`,
		displayName, parsedJSON, string(errJSON), id,
	)
	return err
}

// UpdateDeviceError marks a device as failed to parse.
func UpdateDeviceError(id string, errs []string) error {
	errJSON, _ := json.Marshal(errs)
	_, err := DB.Exec(`
		UPDATE blackboxes
		SET    status       = 'error',
		       parse_errors = ?,
		       updated_at   = datetime('now')
		WHERE  id = ?`,
		string(errJSON), id,
	)
	return err
}

// BlockDevice sets or clears the blocked flag on a device.
// Blocked devices return 403 on all endpoints and are hidden from menus.
func BlockDevice(id string, blocked bool) error {
	v := 0
	if blocked {
		v = 1
	}
	res, err := DB.Exec(
		`UPDATE blackboxes SET blocked = ?, updated_at = datetime('now') WHERE id = ?`,
		v, id,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetDeviceByGithubURL returns the existing device row for (userID, githubURL,
// displayName). Used by the submit handler to detect re-parse requests.
// Returns ErrNotFound when no row matches.
// GetDeviceForOwner returns the full Device row (including parsed_json) for
// the given id, restricted to the authenticated owner. Used by the
// GET /api/v1/blackbox/:id endpoint so the Projects page can render the
// visual block preview without loading every device's parsed JSON in the list.
// Returns ErrNotFound when the device does not exist or belongs to a different user.
func GetDeviceForOwner(id, userID string) (*Device, error) {
	row := DB.QueryRow(`
		SELECT id, user_id, github_url, github_owner, github_repo, github_tag,
		       display_name, tags, blocked, status, parsed_json, parse_errors,
		       visibility, publish_to_feed, publish_to_search, ready_to_use,
		       COALESCE(display_name_human,''), COALESCE(category_id,''), COALESCE(subcategory_id,''),
		       COALESCE(programming_language_id,''),
		       created_at, updated_at
		FROM   blackboxes
		WHERE  id      = ?
		  AND  user_id = ?`,
		id, userID,
	)
	return scanDevice(row)
}

func GetDeviceByGithubURL(userID, githubURL, displayName string) (*Device, error) {
	row := DB.QueryRow(`
		SELECT id, user_id, github_url, github_owner, github_repo, github_tag,
		       display_name, tags, blocked, status, parsed_json, parse_errors,
		       visibility, publish_to_feed, publish_to_search, ready_to_use,
		       COALESCE(display_name_human,''), COALESCE(category_id,''), COALESCE(subcategory_id,''),
		       COALESCE(programming_language_id,''),
		       created_at, updated_at
		FROM   blackboxes
		WHERE  user_id      = ?
		  AND  github_url   = ?
		  AND  display_name = ?`,
		userID, githubURL, displayName,
	)
	return scanDevice(row)
}

// GetDeviceIDsByGithubURL returns a map of display_name → id for all devices
// submitted by userID from the given githubURL. Used by the submit handler to
// detect re-parse requests and pass existing IDs to the worker.
// Returns an empty map if no matching devices exist.
func GetDeviceIDsByGithubURL(userID, githubURL string) (map[string]string, error) {
	rows, err := DB.Query(`
		SELECT id, display_name
		FROM   blackboxes
		WHERE  user_id    = ?
		  AND  github_url = ?`,
		userID, githubURL,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var id, displayName string
		if err := rows.Scan(&id, &displayName); err != nil {
			return nil, err
		}
		result[displayName] = id
	}
	return result, rows.Err()
}

// UpdateDeviceMeta updates tags, visibility, category, and publishing flags of a device.
// Only the owner can update it.
func UpdateDeviceMeta(id, userID, tags, visibility, categoryID, subcategoryID string,
	publishToFeed, publishToSearch, readyToUse bool) error {
	if visibility != "public" {
		visibility = "private"
	}
	res, err := DB.Exec(`
		UPDATE blackboxes
		SET    tags             = ?,
		       visibility       = ?,
		       category_id      = ?,
		       subcategory_id   = ?,
		       publish_to_feed   = ?,
		       publish_to_search = ?,
		       ready_to_use      = ?,
		       updated_at        = datetime('now')
		WHERE  id      = ?
		  AND  user_id = ?`,
		tags, visibility,
		nullableString(categoryID),
		nullableString(subcategoryID),
		boolToInt(publishToFeed),
		boolToInt(publishToSearch),
		boolToInt(readyToUse),
		id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDevice permanently deletes a device row owned by userID.
// Returns ErrNotFound when the device does not exist or belongs to another user.
func DeleteDevice(id, userID string) error {
	res, err := DB.Exec(
		`DELETE FROM blackboxes WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// UpdateDeviceVisibility changes the visibility of a device.
// Only the owner can change it. Returns ErrNotFound if no row matched.
func UpdateDeviceVisibility(id, userID, visibility string) error {
	if visibility != "public" {
		visibility = "private"
	}
	res, err := DB.Exec(`
		UPDATE blackboxes
		SET    visibility = ?,
		       updated_at = datetime('now')
		WHERE  id      = ?
		  AND  user_id = ?`,
		visibility, id, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// nullableString returns nil for empty strings (stores NULL in SQLite)
// and the string value otherwise. Used for optional FK columns like
// category_id and subcategory_id that reference project_categories.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// DevicePublishingUpdate holds the three community flags that can be changed
// via PUT /api/v1/blackbox/:id/publishing.
type DevicePublishingUpdate struct {
	PublishToFeed   bool
	PublishToSearch bool
	ReadyToUse      bool
}

// UpdateDevicePublishing sets the three community publishing flags.
// Business rules (visibility=public AND status=ready) are enforced at the
// handler layer.
func UpdateDevicePublishing(id, userID string, upd *DevicePublishingUpdate) error {
	_, err := DB.Exec(`
		UPDATE blackboxes
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
	return err
}

// GetDeviceIDsByGithubRepo returns a map of display_name → id for all devices
// submitted by userID from the given owner/repo, regardless of tag/version.
// Used by the submit handler so a new release updates existing rows
// instead of creating duplicates.
func GetDeviceIDsByGithubRepo(userID, owner, repo string) (map[string]string, error) {
	rows, err := DB.Query(`
		SELECT id, display_name
		FROM   blackboxes
		WHERE  user_id      = ?
		  AND  github_owner = ?
		  AND  github_repo  = ?`,
		userID, owner, repo,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var id, displayName string
		if err := rows.Scan(&id, &displayName); err != nil {
			return nil, err
		}
		result[displayName] = id
	}
	return result, rows.Err()
}

// ListDevicesByUser returns all devices owned by userID, newest first.
func ListDevicesByUser(userID string) ([]DeviceSummary, error) {
	return queryDeviceSummaries(`
		SELECT id, user_id, github_url, github_owner, github_repo, github_tag,
		       display_name, tags, blocked, status,
		       visibility, publish_to_feed, publish_to_search, ready_to_use,
		       COALESCE(display_name_human,''), COALESCE(category_id,''), COALESCE(subcategory_id,''),
		       created_at, updated_at
		FROM   blackboxes
		WHERE  user_id = ?
		ORDER  BY updated_at DESC`, userID)
}

// ListReadyDevices returns all devices belonging to the authenticated user,
// regardless of status or visibility. The sidebar of the IDE WASM shows only
// the user's own devices. Public devices from other users are provided by the
// menu tree system (admin-curated sections and categories).
//
// When callerID is empty (anonymous / not logged in), returns nothing.
func ListReadyDevices(callerID string) ([]Device, error) {
	if callerID == "" {
		return nil, nil
	}

	rows, err := DB.Query(`
		SELECT id, user_id, github_url, github_owner, github_repo, github_tag,
		       display_name, tags, blocked, status, parsed_json, parse_errors,
		       visibility, publish_to_feed, publish_to_search, ready_to_use,
		       COALESCE(display_name_human,''), COALESCE(category_id,''), COALESCE(subcategory_id,''),
		       COALESCE(programming_language_id,''),
		       created_at, updated_at
		FROM   blackboxes
		WHERE  user_id = ?
		ORDER  BY display_name ASC`, callerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Device
	for rows.Next() {
		d, err := scanDeviceRow(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *d)
	}
	if list == nil {
		list = []Device{}
	}
	return list, rows.Err()
}

// ─── Scene loading (used by codegen handler) ──────────────────────────────────

// LoadBlackBoxDefsForScene scans the scene JSON for any device whose type
// follows the pattern "BlackBox{Method}:{StructName}" and returns a
// map[structName]*BlackBoxDef for each one found.
//
// Source priority:
//  1. project_code_versions — maker's own Go source saved in the Monaco editor.
//     Parsed on-the-fly (source → BlackBoxDef via bbparser.Parse).
//  2. blackboxes — GitHub-sourced devices. The parsed_json is already a
//     serialised BlackBoxDef — no re-parse needed, just unmarshal.
//
// Used by the codegen handler to inject device definitions into the pipeline.
func LoadBlackBoxDefsForScene(sceneJSON []byte) (map[string]*bbparser.BlackBoxDef, error) {
	var scene struct {
		Devices []struct {
			Type string `json:"type"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(sceneJSON, &scene); err != nil {
		return nil, err
	}

	// Collect the BlackBox references the scene needs, split by shape:
	//   - Go struct devices:   "BlackBox<Method>:<Struct>" → struct name (after colon)
	//   - C99 function-devices: "BlackBox<fn>:"            → function name (empty struct)
	// They are keyed differently downstream: Go defs by struct name, C99
	// function-device defs by function name (see Source 1 below).
	needed := make(map[string]bool)      // Go struct names
	neededFuncs := make(map[string]bool) // C99 function names
	for _, d := range scene.Devices {
		if !strings.HasPrefix(d.Type, "BlackBox") {
			continue
		}
		colonIdx := strings.Index(d.Type, ":")
		if colonIdx < 0 {
			continue
		}
		if structName := d.Type[colonIdx+1:]; structName != "" {
			needed[structName] = true
			continue
		}
		// Empty struct part → C99 function-device. The function name is the
		// span between "BlackBox" and the colon.
		if fnName := d.Type[len("BlackBox"):colonIdx]; fnName != "" {
			neededFuncs[fnName] = true
		}
	}
	if len(needed) == 0 && len(neededFuncs) == 0 {
		return nil, nil
	}

	limits := bbparser.DefaultParserLimits()
	defs := make(map[string]*bbparser.BlackBoxDef, len(needed)+len(neededFuncs))

	// ── Source 1: project_code_versions (maker's own code) ───────────────────
	projectVersions, err := ListAllLatestProjectCodeVersions()
	if err == nil {
		for _, pv := range projectVersions {
			if pv.Source == "" {
				continue
			}
			// Dispatch on the project's language: a C99 source must be parsed
			// by ParseC, not the Go parser. ParseForLanguage routes ""/"go"/
			// "golang" → Go parser and "c"/"c99" → ParseC.
			def, parseErr := bbparser.ParseForLanguage(pv.Language, []byte(pv.Source), limits)
			if parseErr != nil || def == nil {
				continue
			}
			if def.Name != "" {
				// Go struct device (def.Name is the struct name).
				if needed[def.Name] {
					defs[def.Name] = def
				}
				continue
			}
			// C99 function-devices: the def has no struct name (def.Name == "");
			// each public function is its own device. Key the def by every
			// function name the scene references, so the validation and the
			// backend can resolve the "<fn>" in "BlackBox<fn>:". Multiple
			// function-devices from one source share the same *def pointer.
			//
			// Carry the verbatim source so the ANSI C backend can inline the
			// authored functions into main.c (the parsed metadata has
			// signatures, not bodies). See BlackBoxDef.RawSource.
			def.RawSource = pv.Source
			for i := range def.Functions {
				if neededFuncs[def.Functions[i].Name] {
					defs[def.Functions[i].Name] = def
				}
			}
		}
	}

	// ── Source 2: blackboxes (GitHub-sourced devices) ─────────────────────────
	// For structs not resolved above, unmarshal the cached parsed_json.
	// No re-parse needed — the worker already validated and stored it.
	for structName := range needed {
		if defs[structName] != nil {
			continue
		}
		var parsedJSON string
		err := DB.QueryRow(`
			SELECT parsed_json FROM blackboxes
			WHERE  display_name = ?
			  AND  status       = 'ready'
			  AND  blocked      = 0
			ORDER  BY updated_at DESC
			LIMIT  1`, structName,
		).Scan(&parsedJSON)
		if errors.Is(err, sql.ErrNoRows) || err != nil {
			continue
		}
		var def bbparser.BlackBoxDef
		if err := json.Unmarshal([]byte(parsedJSON), &def); err != nil {
			continue
		}
		defs[structName] = &def
	}

	return defs, nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func scanDevice(row *sql.Row) (*Device, error) {
	var d Device
	var userID sql.NullString
	var peJSON, cat, uat string

	err := row.Scan(
		&d.ID, &userID,
		&d.GithubURL, &d.GithubOwner, &d.GithubRepo, &d.GithubTag,
		&d.DisplayName, &d.Tags, &d.Blocked, &d.Status,
		&d.ParsedJSON, &peJSON,
		&d.Visibility, &d.PublishToFeed, &d.PublishToSearch, &d.ReadyToUse,
		&d.DisplayNameHuman, &d.CategoryID, &d.SubcategoryID,
		&d.ProgrammingLanguageID,
		&cat, &uat,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		d.UserID = userID.String
	}
	_ = json.Unmarshal([]byte(peJSON), &d.ParseErrors)
	d.CreatedAt, _ = time.Parse(time.RFC3339, cat)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, uat)
	return &d, nil
}

func scanDeviceRow(rows *sql.Rows) (*Device, error) {
	var d Device
	var userID sql.NullString
	var peJSON, cat, uat string

	err := rows.Scan(
		&d.ID, &userID,
		&d.GithubURL, &d.GithubOwner, &d.GithubRepo, &d.GithubTag,
		&d.DisplayName, &d.Tags, &d.Blocked, &d.Status,
		&d.ParsedJSON, &peJSON,
		&d.Visibility, &d.PublishToFeed, &d.PublishToSearch, &d.ReadyToUse,
		&d.DisplayNameHuman, &d.CategoryID, &d.SubcategoryID,
		&d.ProgrammingLanguageID,
		&cat, &uat,
	)
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		d.UserID = userID.String
	}
	_ = json.Unmarshal([]byte(peJSON), &d.ParseErrors)
	d.CreatedAt, _ = time.Parse(time.RFC3339, cat)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, uat)
	return &d, nil
}

func queryDeviceSummaries(query string, args ...any) ([]DeviceSummary, error) {
	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []DeviceSummary
	for rows.Next() {
		var d DeviceSummary
		var userID sql.NullString
		var cat, uat string
		if err := rows.Scan(
			&d.ID, &userID,
			&d.GithubURL, &d.GithubOwner, &d.GithubRepo, &d.GithubTag,
			&d.DisplayName, &d.Tags, &d.Blocked, &d.Status,
			&d.Visibility, &d.PublishToFeed, &d.PublishToSearch, &d.ReadyToUse,
			&d.DisplayNameHuman, &d.CategoryID, &d.SubcategoryID,
			&cat, &uat,
		); err != nil {
			return nil, err
		}
		if userID.Valid {
			d.UserID = userID.String
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, cat)
		d.UpdatedAt, _ = time.Parse(time.RFC3339, uat)
		list = append(list, d)
	}
	if list == nil {
		list = []DeviceSummary{}
	}
	return list, rows.Err()
}
