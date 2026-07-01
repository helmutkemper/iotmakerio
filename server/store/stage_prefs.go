// server/store/stage_prefs.go — Per-user stage preference CRUD.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Stage preferences tune the IDE's visual workspace (zoom sensitivity,
// pan behaviour, cursor hints). The table stores sparse rows — a knob
// that the user never touched is stored as NULL and falls back to the
// compile-time default.
//
// English:
//
//	Public API is three functions: GetStagePrefs, UpdateStagePrefs,
//	ResetStagePrefs. The handler layer (editorapi) wraps these; the
//	portal and IDE clients consume the handler JSON.
//
// Português:
//
//	API pública: GetStagePrefs, UpdateStagePrefs, ResetStagePrefs.
//	O handler (editorapi) encapsula; portal e IDE consomem JSON.
package store

import (
	"database/sql"
	"fmt"
	"time"
)

// ─── Defaults ─────────────────────────────────────────────────────────────────

// The defaults below mirror rulesSprite.Camera* in the IDE. When the
// UI needs to show "reset to default", it reads these values. The
// server is the authority — the IDE should read the defaults the
// server reports rather than using its own compile-time constants,
// so UI and behaviour stay in sync when the defaults are ever tuned.
//
// Português: Os defaults espelham os de rulesSprite.Camera* do IDE.
// O servidor é a autoridade; o IDE deve usar o que o servidor
// reporta em vez de constantes locais, assim UI e comportamento
// ficam sincronizados se os defaults mudarem.
const (
	// DefaultStageZoomStep is the zoom change per scroll notch.
	// Calibrated for MacBook trackpad comfort (3% per notch).
	DefaultStageZoomStep = 0.03

	// DefaultStagePanEmptyArea enables left-click+drag on empty
	// stage area → camera pan. Desktop mouse only; touch is
	// reserved for future gestures.
	DefaultStagePanEmptyArea = true

	// DefaultStageShowGrabCursor toggles the grab cursor hint when
	// hovering empty stage area. Off by default because it requires
	// continuous hover tracking.
	DefaultStageShowGrabCursor = false
)

// ─── Model ────────────────────────────────────────────────────────────────────

// StagePrefs is the resolved preference set for a single user.
// Resolved means: every field has a concrete value — either what
// the user stored, or the compile-time default. Callers never see
// NULLs; GetStagePrefs does the merge internally.
//
// Português: Conjunto de preferências de um usuário, resolvido —
// todo campo tem valor concreto, seja o do usuário ou o default.
type StagePrefs struct {
	// ZoomStep: increment per wheel notch (0.01–0.15 typical).
	ZoomStep float64 `json:"zoomStep"`

	// PanEmptyArea: left-click+drag on empty stage → camera pan.
	PanEmptyArea bool `json:"panEmptyArea"`

	// ShowGrabCursor: cursor hint on hover over empty area.
	ShowGrabCursor bool `json:"showGrabCursor"`
}

// DefaultStagePrefs returns a StagePrefs populated with the
// compile-time defaults. Used by GetStagePrefs for users without a
// row, and by ResetStagePrefs as the response body after wipe.
func DefaultStagePrefs() StagePrefs {
	return StagePrefs{
		ZoomStep:       DefaultStageZoomStep,
		PanEmptyArea:   DefaultStagePanEmptyArea,
		ShowGrabCursor: DefaultStageShowGrabCursor,
	}
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

// GetStagePrefs returns the resolved preferences for a user. If the
// user has no row yet, all fields come from DefaultStagePrefs. If
// the row exists but some fields are NULL, those specific fields
// come from defaults — the rest come from the row.
//
// Per-field NULL-coalescing is important: we want to be able to
// change a single default later (e.g. DefaultStageZoomStep) without
// affecting users who only ever customised PanEmptyArea.
//
// Português: Resolve as preferências do usuário. Linhas ausentes
// ou campos NULL caem para os defaults. Mistura por campo.
func GetStagePrefs(userID string) (StagePrefs, error) {
	prefs := DefaultStagePrefs()

	var (
		zoomStep       sql.NullFloat64
		panEmptyArea   sql.NullInt64
		showGrabCursor sql.NullInt64
	)
	err := DB.QueryRow(`
		SELECT zoom_step, pan_empty_area, show_grab_cursor
		FROM stage_prefs
		WHERE user_id = ?`, userID,
	).Scan(&zoomStep, &panEmptyArea, &showGrabCursor)
	if err == sql.ErrNoRows {
		// No row yet — all defaults.
		return prefs, nil
	}
	if err != nil {
		return prefs, err
	}

	// Per-field coalesce.
	if zoomStep.Valid {
		prefs.ZoomStep = zoomStep.Float64
	}
	if panEmptyArea.Valid {
		prefs.PanEmptyArea = panEmptyArea.Int64 != 0
	}
	if showGrabCursor.Valid {
		prefs.ShowGrabCursor = showGrabCursor.Int64 != 0
	}
	return prefs, nil
}

// StagePrefsUpdate carries the fields a PUT request wants to
// change. Any pointer that is nil means "don't touch this field";
// a non-nil pointer sets the value (possibly to the default — we
// don't special-case that). This matches the "patch semantics" the
// portal UI uses: each slider/checkbox change sends only its own
// key, debounced.
//
// Português: Payload do PUT. Ponteiros nil = não mexer; não-nil =
// gravar. Semântica de patch por campo para casar com a UI que
// debounces mudança de cada controle individualmente.
type StagePrefsUpdate struct {
	ZoomStep       *float64 `json:"zoomStep,omitempty"`
	PanEmptyArea   *bool    `json:"panEmptyArea,omitempty"`
	ShowGrabCursor *bool    `json:"showGrabCursor,omitempty"`
}

// UpdateStagePrefs applies a partial update to the user's row.
// Creates the row if it doesn't exist; touches only the fields the
// patch carries; stamps updated_at every call. Returns the resolved
// prefs after the update so the handler can echo them back without
// a second query.
//
// Português: Aplica patch parcial. Cria a linha se necessário,
// mexe apenas nos campos do patch, estampa updated_at. Retorna
// as prefs resolvidas para o handler ecoar sem nova query.
func UpdateStagePrefs(userID string, patch StagePrefsUpdate) (StagePrefs, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// UPSERT: insert with the patch's non-nil fields, else update
	// only those. We hand-build the SET clause so we don't
	// accidentally overwrite untouched columns with NULL.
	setClauses := []string{"updated_at = ?"}
	setArgs := []any{now}

	insertCols := []string{"user_id", "updated_at"}
	insertVals := []any{userID, now}
	insertPlaceholders := []string{"?", "?"}

	if patch.ZoomStep != nil {
		setClauses = append(setClauses, "zoom_step = ?")
		setArgs = append(setArgs, *patch.ZoomStep)
		insertCols = append(insertCols, "zoom_step")
		insertVals = append(insertVals, *patch.ZoomStep)
		insertPlaceholders = append(insertPlaceholders, "?")
	}
	if patch.PanEmptyArea != nil {
		v := 0
		if *patch.PanEmptyArea {
			v = 1
		}
		setClauses = append(setClauses, "pan_empty_area = ?")
		setArgs = append(setArgs, v)
		insertCols = append(insertCols, "pan_empty_area")
		insertVals = append(insertVals, v)
		insertPlaceholders = append(insertPlaceholders, "?")
	}
	if patch.ShowGrabCursor != nil {
		v := 0
		if *patch.ShowGrabCursor {
			v = 1
		}
		setClauses = append(setClauses, "show_grab_cursor = ?")
		setArgs = append(setArgs, v)
		insertCols = append(insertCols, "show_grab_cursor")
		insertVals = append(insertVals, v)
		insertPlaceholders = append(insertPlaceholders, "?")
	}

	// Try UPDATE first; if it hit 0 rows, INSERT.
	// Can't use UPSERT syntax (ON CONFLICT) because we only want
	// to update the named columns, and the full UPSERT+DO UPDATE
	// form would reset untouched columns.
	setSQL := joinStrings(setClauses, ", ")
	res, err := DB.Exec(
		fmt.Sprintf("UPDATE stage_prefs SET %s WHERE user_id = ?", setSQL),
		append(setArgs, userID)...,
	)
	if err != nil {
		return DefaultStagePrefs(), err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Row didn't exist — insert it.
		insertSQL := fmt.Sprintf(
			"INSERT INTO stage_prefs (%s) VALUES (%s)",
			joinStrings(insertCols, ", "),
			joinStrings(insertPlaceholders, ", "),
		)
		if _, err := DB.Exec(insertSQL, insertVals...); err != nil {
			return DefaultStagePrefs(), err
		}
	}

	return GetStagePrefs(userID)
}

// ResetStagePrefs deletes the user's row entirely. Subsequent GETs
// will fall through to DefaultStagePrefs. Returns the resolved
// defaults so the handler can echo them back.
//
// Português: Apaga a linha do usuário. GETs subsequentes caem nos
// defaults. Retorna os defaults para o handler ecoar.
func ResetStagePrefs(userID string) (StagePrefs, error) {
	if _, err := DB.Exec(`DELETE FROM stage_prefs WHERE user_id = ?`, userID); err != nil {
		return DefaultStagePrefs(), err
	}
	return DefaultStagePrefs(), nil
}
