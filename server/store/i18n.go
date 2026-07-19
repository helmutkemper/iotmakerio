// server/store/i18n.go — Translation (i18n) persistence for the IoTMaker portal.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Overview
// --------
//
// This file owns every database interaction related to translations. The public
// API of the package is intentionally small and stable so that the HTTP layer
// (public read routes in server/handler/i18n and private write routes in
// server/handler/controlapi) can be evolved independently:
//
//	GetBundle(locale)                  → read one locale bundle
//	ListLocales()                      → enumerate all registered locales
//	ReplaceBundle(locale, messages)    → atomic bulk replace (admin save)
//	SeedTranslations()                 → bootstrap the two default locales
//	InsertMissingMessage(TrMessage)    → telemetry from the IDE WASM runtime
//
// Schema (created in db.go migrate())
// -----------------------------------
//
//	i18n_bundles   — one row per locale (locale, bundle_id, display, updated_at).
//	i18n_messages  — one row per (locale, message_id) with other / one / description.
//
// Design decisions
// ----------------
//
//   - Messages live in their own table so a single string can be updated or
//     inserted without rewriting the whole bundle; the ReplaceBundle operation
//     still wraps the bulk case in a single transaction for atomicity.
//   - The JSON produced by GetBundle matches the legacy format used by the IDE
//     WASM client and the old standalone admin page; the new control-panel UI
//     also consumes it unchanged.
//   - Seeding is idempotent: SeedTranslations only inserts for a locale if that
//     locale has zero rows in i18n_messages. That way, restarting the server
//     never clobbers admin edits.
//
// NOTE: single-message upsert was intentionally removed. The control panel
// always saves a complete bundle; bulk ReplaceBundle is the only write path.
// If a single-key API is ever needed again, add it behind /api/control/v1 with
// OTP, never under /api/v1.
package store

import (
	"database/sql"
	"errors"
	"time"
)

// ─── Models ───────────────────────────────────────────────────────────────────

// TrMessage is one localised string.
//
//	Other       — singular / default form (required).
//	One         — optional plural form (e.g. "1 item" vs "3 items").
//	Description — free-text note shown to translators; not rendered to end users.
type TrMessage struct {
	ID          string `json:"id"`
	Other       string `json:"other"`
	One         string `json:"one,omitempty"`
	Description string `json:"description,omitempty"`
}

// TrBundle is the full translation bundle for one locale.
// It is the top-level JSON returned to the IDE WASM client and the admin UI.
type TrBundle struct {
	BundleID  string      `json:"bundleId"`
	Locale    string      `json:"locale"`
	UpdatedAt time.Time   `json:"updatedAt"`
	Messages  []TrMessage `json:"messages"`
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// GetBundle returns the full translation bundle for the given locale.
// Returns ErrNotFound if the locale does not exist.
func GetBundle(locale string) (*TrBundle, error) {
	var b TrBundle
	var updatedAt string

	err := DB.QueryRow(`
		SELECT locale, bundle_id, updated_at
		FROM   i18n_bundles
		WHERE  locale = ?`, locale,
	).Scan(&b.Locale, &b.BundleID, &updatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	b.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	rows, err := DB.Query(`
		SELECT message_id, other, one, description
		FROM   i18n_messages
		WHERE  locale = ?
		ORDER  BY message_id`,
		locale,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	b.Messages = []TrMessage{}
	for rows.Next() {
		var m TrMessage
		if err := rows.Scan(&m.ID, &m.Other, &m.One, &m.Description); err != nil {
			return nil, err
		}
		b.Messages = append(b.Messages, m)
	}
	return &b, rows.Err()
}

// ListLocales returns the list of all registered locales (e.g. ["en-US","pt-BR"]).
func ListLocales() ([]string, error) {
	rows, err := DB.Query(`SELECT locale FROM i18n_bundles ORDER BY locale`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locales []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		locales = append(locales, l)
	}
	return locales, rows.Err()
}

// ─── Write ────────────────────────────────────────────────────────────────────

// ReplaceBundle atomically replaces ALL messages for a locale.
//
// Steps inside a single transaction:
//  1. Ensure the bundle row exists (INSERT OR IGNORE).
//  2. Delete all existing messages for the locale.
//  3. Insert the new message set.
//  4. Bump the bundle's updated_at timestamp.
//
// This is the only admin-facing write path. The control-panel UI always sends
// the full bundle; missing entries are deleted on save. If the admin wants to
// preserve an existing key, they must include it in the request body.
func ReplaceBundle(locale string, messages []TrMessage) (*TrBundle, error) {
	now := time.Now().UTC()

	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Ensure bundle row exists.
	_, err = tx.Exec(`
		INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
		VALUES (?, ?, ?)`,
		locale, locale+"-custom", now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}

	// Delete every existing message for this locale.
	if _, err = tx.Exec(`DELETE FROM i18n_messages WHERE locale = ?`, locale); err != nil {
		return nil, err
	}

	// Insert the new message set.
	stmt, err := tx.Prepare(`
		INSERT INTO i18n_messages (locale, message_id, other, one, description)
		VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	for _, m := range messages {
		if _, err = stmt.Exec(locale, m.ID, m.Other, m.One, m.Description); err != nil {
			return nil, err
		}
	}

	// Bump updated_at.
	if _, err = tx.Exec(`
		UPDATE i18n_bundles SET updated_at = ? WHERE locale = ?`,
		now.Format(time.RFC3339), locale,
	); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return GetBundle(locale)
}

// ─── Seed ─────────────────────────────────────────────────────────────────────

// seedEntry is the row-shaped seed record used to bootstrap the two default
// locales. Keeping English and Portuguese in the same struct forces them to
// stay in sync: you cannot add a key without providing both translations,
// and the compiler will complain if a locale diverges from the canonical list.
type seedEntry struct {
	ID          string // message identifier used in the code
	EnUS        string // English (US) value for the "other" form
	PtBR        string // Portuguese (BR) value for the "other" form
	Description string // optional translator note — same in both locales
}

// seedTranslations is the single source of truth for the bootstrap translation
// set. The list is grouped by feature for human readability; order has no
// runtime effect — GetBundle re-sorts by message_id on read.
//
// How to add a translation
// ------------------------
//
//  1. Append a new seedEntry to the block below (both EnUS and PtBR required).
//  2. On next server start the key is inserted into every locale that still
//     has zero messages.
//  3. If the database already has the locale seeded (production), the new key
//     is NOT auto-inserted — the admin must add it via the control panel. This
//     is intentional: we never want a restart to rewrite production strings.
//
// Exported via SeedTranslations for db.go migrate() to call on startup.
var seedTranslations = []seedEntry{
	// ── Site identity ────────────────────────────────────────────────────────
	{ID: "site.title", EnUS: "IoTMaker", PtBR: "IoTMaker", Description: "Site title"},

	// ── Top navigation ───────────────────────────────────────────────────────
	{ID: "nav.home", EnUS: "Home", PtBR: "Início"},
	{ID: "nav.dashboard", EnUS: "Dashboard", PtBR: "Painel"},
	{ID: "nav.blackbox", EnUS: "Black Box", PtBR: "Black Box"},
	{ID: "nav.login", EnUS: "Log in", PtBR: "Entrar"},
	{ID: "nav.logout", EnUS: "Log out", PtBR: "Sair"},
	{ID: "nav.register", EnUS: "Register", PtBR: "Cadastro"},

	// ── Authentication pages ─────────────────────────────────────────────────
	{ID: "auth.register", EnUS: "Create account", PtBR: "Criar conta"},
	{ID: "auth.login", EnUS: "Log in", PtBR: "Entrar"},
	{ID: "auth.verify", EnUS: "Verify email", PtBR: "Verificar e-mail"},
	{ID: "auth.forgot", EnUS: "Forgot password", PtBR: "Esqueci a senha"},
	{ID: "auth.reset", EnUS: "Reset password", PtBR: "Redefinir senha"},

	// ── Black-box library page ───────────────────────────────────────────────
	{ID: "bb.title", EnUS: "Black Box Library", PtBR: "Biblioteca Black Box"},
	{ID: "bb.submit", EnUS: "Parse & Save", PtBR: "Processar & Salvar"},
	{ID: "bb.analyze", EnUS: "Analyze", PtBR: "Analisar"},
	{ID: "bb.empty", EnUS: "No components yet.", PtBR: "Nenhum componente ainda."},
	{ID: "bb.error.empty", EnUS: "Code cannot be empty.", PtBR: "O código não pode estar vazio."},
	{ID: "bb.error.parse", EnUS: "Parse error", PtBR: "Erro no parse"},

	// ── Generic error strings ────────────────────────────────────────────────
	{ID: "error.internal", EnUS: "An internal error occurred. Please try again.", PtBR: "Erro interno. Tente novamente."},
	{ID: "error.unauthorized", EnUS: "You must be logged in.", PtBR: "Você precisa estar logado."},

	// ── IDE stage view — tab headers ─────────────────────────────────────────
	{ID: "stageViewTabFrontend", EnUS: "Frontend", PtBR: "Frontend"},
	{ID: "stageViewTabBackend", EnUS: "Backend", PtBR: "Backend"},
	{ID: "tabProperties", EnUS: "Properties", PtBR: "Propriedades"},

	// ── IDE help overlay (camera / keyboard) ─────────────────────────────────
	{ID: "helpViewCameraControls", EnUS: "Camera Controls", PtBR: "Controles de câmera"},
	{ID: "helpViewPanText", EnUS: "Pan: Middle mouse + drag / Arrow keys", PtBR: "Pan: Botão do meio + arrastar / Setas"},
	{ID: "helpViewZoomInText", EnUS: "Zoom in: Scroll up / + key", PtBR: "Zoom in: Scroll cima / tecla +"},
	{ID: "helpViewZoomOutText", EnUS: "Zoom out: Scroll down / - key", PtBR: "Zoom out: Scroll baixo / tecla -"},
	{ID: "helpViewGoHomeText", EnUS: "Go home: Home / ctrl+h", PtBR: "Início: Home / ctrl+h"},
	{ID: "helpViewFitAll", EnUS: "Fit all: f key", PtBR: "Ajustar tudo: tecla f"},
	{ID: "helpViewThisHelp", EnUS: "This help: ? key", PtBR: "Esta ajuda: tecla ?"},

	// ── IDE main (hex) menu — top-level sections ─────────────────────────────
	{ID: "menuMainBack", EnUS: "Back", PtBR: "Voltar"},
	{ID: "menuMainExit", EnUS: "Exit", PtBR: "Sair"},
	{ID: "menuMainMenuIcon", EnUS: "Menu", PtBR: "Menu"},
	{ID: "menuMainMyItems", EnUS: "My Items", PtBR: "Meus Itens"},
	{ID: "menuMainFiles", EnUS: "Files", PtBR: "Arquivos"},
	{ID: "menuMainSettings", EnUS: "Settings", PtBR: "Configurações"},
	{ID: "menuMainHardware", EnUS: "Hardware", PtBR: "Hardware"},
	{ID: "menuMainNoHardware", EnUS: "No devices", PtBR: "Sem dispositivos"},

	// ── IDE main menu — Math ─────────────────────────────────────────────────
	{ID: "menuMainMath", EnUS: "Math", PtBR: "Matemática"},
	{ID: "menuMainAdd", EnUS: "Add", PtBR: "Somar"},
	{ID: "menuMainSub", EnUS: "Sub", PtBR: "Subtrair"},
	{ID: "menuMainMul", EnUS: "Mul", PtBR: "Multiplicar"},
	{ID: "menuMainDiv", EnUS: "Div", PtBR: "Dividir"},

	// ── IDE main menu — Logic ────────────────────────────────────────────────
	{ID: "menuMainLogic", EnUS: "Logic", PtBR: "Lógica"},
	{ID: "menuMainEqualTo", EnUS: "Equal to", PtBR: "Igual a"},
	{ID: "menuMainNotEqualTo", EnUS: "Not equal to", PtBR: "Diferente de"},
	{ID: "menuMainLessThan", EnUS: "Less than", PtBR: "Menor que"},
	{ID: "menuMainLessThanOrEqualTo", EnUS: "Less than or equal to", PtBR: "Menor ou igual a"},
	{ID: "menuMainGreaterThan", EnUS: "Greater than", PtBR: "Maior que"},
	{ID: "menuMainGreaterThanOrEqualTo", EnUS: "Greater than or equal to", PtBR: "Maior ou igual a"},
	{ID: "menuMainLoop", EnUS: "Loop", PtBR: "Loop"},

	// ── IDE main menu — Constants ────────────────────────────────────────────
	{ID: "menuMainConst", EnUS: "Const", PtBR: "Constante"},
	{ID: "menuMainConstInt", EnUS: "Int", PtBR: "Inteiro"},
	{ID: "menuMainVar", EnUS: "Variables", PtBR: "Variáveis"},
	{ID: "menuMainGetVarInt", EnUS: "Get Int", PtBR: "Ler Int"},
	{ID: "menuMainGetVarFloat", EnUS: "Get Float", PtBR: "Ler Float"},
	{ID: "menuMainSetVarInt", EnUS: "Set Int", PtBR: "Gravar Int"},
	{ID: "menuMainSetVarFloat", EnUS: "Set Float", PtBR: "Gravar Float"},
	{ID: "menuMainGetVarString", EnUS: "Get String", PtBR: "Ler String"},
	{ID: "menuMainSetVarString", EnUS: "Set String", PtBR: "Gravar String"},
	{ID: "menuMainConstFloat", EnUS: "Float", PtBR: "Ponto flutuante"},
	{ID: "menuMainConstBool", EnUS: "Bool", PtBR: "Booleano"},
	{ID: "menuMainConstString", EnUS: "String", PtBR: "String"},
	{ID: "menuMainConstArrayInt", EnUS: "Int Array", PtBR: "Vetor Int"},
	{ID: "menuMainConstArrayFloat", EnUS: "Float Array", PtBR: "Vetor Float"},
	{ID: "menuMainConstArrayString", EnUS: "String Array", PtBR: "Vetor String"},

	// ── IDE main menu — Frontend widgets ─────────────────────────────────────
	{ID: "menuMainDisplay", EnUS: "Display", PtBR: "Display"},
	{ID: "menuMainGauge", EnUS: "Gauge", PtBR: "Medidor"},
	{ID: "menuMainLED", EnUS: "LED", PtBR: "LED"},
	{ID: "menuMainBarGraph", EnUS: "Bar", PtBR: "Barra"},
	{ID: "menuMainTextDisplay", EnUS: "Text", PtBR: "Texto"},
	{ID: "menuMainButton", EnUS: "Button", PtBR: "Botão"},
	{ID: "menuMainSevenSeg", EnUS: "7-Seg", PtBR: "7-Seg"},
	{ID: "menuMainKnob", EnUS: "Knob", PtBR: "Knob"},
	{ID: "menuMainChart", EnUS: "Chart", PtBR: "Gráfico"},
	{ID: "menuMainChartPro", EnUS: "Chart Pro", PtBR: "Gráfico Pro"},
	{ID: "menuMainPieChart", EnUS: "Pie Chart", PtBR: "Gráfico Pizza"},
	{ID: "menuMainBackgroundImage", EnUS: "Background", PtBR: "Fundo"},
	{ID: "menuMainCommStatus", EnUS: "Comm", PtBR: "Comunicação"},
	{ID: "menuMainImage", EnUS: "Image", PtBR: "Imagem"},

	// ── IDE main menu — Export ───────────────────────────────────────────────
	{ID: "menuMainExport", EnUS: "Export", PtBR: "Exportar"},
	{ID: "menuMainExportJSON", EnUS: "JSON", PtBR: "JSON"},
	// menuMainExportGo removed in May 2026 — the export menu no longer
	// names a language. See migrateMenuTreeLabels() in menu_tree_seed.go
	// for the UPDATE that points the legacy SysExportGo slot at
	// menuMainExport instead. Existing translations of the removed key
	// remain in i18n_messages on upgraded installations but are no
	// longer referenced by any menu item, so they're effectively dead.

	// ── IDE menu categories (dynamic menu — admin-managed) ───────────────────
	{ID: "menuCat_Sensors", EnUS: "Sensors", PtBR: "Sensores"},
	{ID: "menuCat_Other", EnUS: "Other", PtBR: "Outros"},
	{ID: "menuSubCat_Optical", EnUS: "Optical", PtBR: "Ótico"},

	// ── IDE — example loader button ──────────────────────────────────────────
	{ID: "loadExample", EnUS: "▶ Load Example", PtBR: "▶ Carregar exemplo"},

	// ── Live communication dialog ────────────────────────────────────────────
	{ID: "liveTitle", EnUS: "Live Communication", PtBR: "Comunicação Live"},
	{ID: "liveStatusDisconnected", EnUS: "Disconnected", PtBR: "Desconectado"},
	{ID: "liveStatusConnected", EnUS: "Connected to", PtBR: "Conectado a"},
	{ID: "liveProjectName", EnUS: "Project Name", PtBR: "Nome do Projeto"},
	{ID: "liveProjectNamePlaceholder", EnUS: "My Temperature Sensor", PtBR: "Meu Sensor de Temperatura"},
	{ID: "liveProjectId", EnUS: "Project ID", PtBR: "ID do Projeto"},
	{ID: "liveProjectIdHint", EnUS: "Save a project name to generate the ID", PtBR: "Salve um nome de projeto para gerar o ID"},
	{ID: "liveProjectHint", EnUS: "Identifies your project in webhook URLs and WebSocket connections. Saved in browser storage.", PtBR: "Identifica seu projeto nas URLs de webhook e conexões WebSocket. Salvo no navegador."},
	{ID: "liveSave", EnUS: "Save", PtBR: "Salvar"},
	{ID: "liveSaved", EnUS: "Saved", PtBR: "Salvo"},
	{ID: "liveConnect", EnUS: "Connect", PtBR: "Conectar"},
	{ID: "liveApiKeys", EnUS: "API Keys", PtBR: "Chaves de API"},
	{ID: "liveNewKey", EnUS: "New Key", PtBR: "Nova Chave"},
	{ID: "liveLoadingKeys", EnUS: "Loading keys...", PtBR: "Carregando chaves..."},
	{ID: "liveLoadingHelp", EnUS: "Loading help...", PtBR: "Carregando ajuda..."},
	{ID: "liveKeysNoProject", EnUS: "Set a project ID first.", PtBR: "Defina um Project ID primeiro."},
	{ID: "liveKeysError", EnUS: "Error loading keys", PtBR: "Erro ao carregar chaves"},
	{ID: "liveKeysParseError", EnUS: "Failed to parse key list.", PtBR: "Falha ao processar lista de chaves."},
	{ID: "liveKeysEmpty", EnUS: "No active keys. Click [+ New Key] to create one.", PtBR: "Nenhuma chave ativa. Clique [+ Nova Chave] para criar uma."},
	{ID: "liveKeyRevoke", EnUS: "Revoke", PtBR: "Revogar"},
	{ID: "liveNewKeyTitle", EnUS: "New API Key", PtBR: "Nova Chave de API"},
	{ID: "liveDeviceId", EnUS: "Device ID", PtBR: "ID do Dispositivo"},
	{ID: "liveKeyLabel", EnUS: "Key Name", PtBR: "Nome da Chave"},
	{ID: "liveKeyLabelPlaceholder", EnUS: "My temperature sensor", PtBR: "Meu sensor de temperatura"},
	{ID: "liveCreateKey", EnUS: "Create Key", PtBR: "Criar Chave"},
	{ID: "liveKeyCreated", EnUS: "API Key Created", PtBR: "Chave de API Criada"},
	{ID: "liveKeyWarning", EnUS: "Save this key now. It will not be shown again.", PtBR: "Salve esta chave agora. Ela não será exibida novamente."},
	{ID: "liveKeyCopy", EnUS: "Copy to clipboard", PtBR: "Copiar"},
	{ID: "liveKeyCopied", EnUS: "Copied!", PtBR: "Copiado!"},
	{ID: "liveKeyCreateError", EnUS: "Error creating key", PtBR: "Erro ao criar chave"},

	// ── Unsaved-changes confirm dialogs ──────────────────────────────────────
	//
	// Used by showUnsavedConfirm in utils.js. Triggered whenever the user is
	// about to leave a buffer with pending edits — the editor on the Devices
	// & Templates page (close, navigate away, version switch) and the help
	// files modal. The three button labels are shared across every caller;
	// the title and body strings are caller-specific.
	{ID: "unsaved.save", EnUS: "Save", PtBR: "Salvar"},
	{ID: "unsaved.discard", EnUS: "Discard", PtBR: "Descartar"},
	{ID: "unsaved.cancel", EnUS: "Cancel", PtBR: "Cancelar"},
	{ID: "unsaved.editor.title", EnUS: "Unsaved changes in the editor", PtBR: "Alterações não salvas no editor"},
	{ID: "unsaved.editor.leave", EnUS: "The editor has unsaved changes. Leaving now will discard all the work since the last save.", PtBR: "O editor tem alterações não salvas. Se você sair agora, todo o trabalho desde o último save será perdido."},
	{ID: "unsaved.editor.back", EnUS: "The editor has unsaved changes. Going back now will discard the work since the last save.", PtBR: "O editor tem alterações não salvas. Voltar agora vai descartar o trabalho desde o último save."},
	{ID: "unsaved.editor.navigate", EnUS: "The editor has unsaved changes. Leaving now will discard the work since the last save.", PtBR: "O editor tem alterações não salvas. Sair agora vai descartar o trabalho desde o último save."},
	{ID: "unsaved.version.title", EnUS: "Switch version", PtBR: "Trocar de versão"},
	{ID: "unsaved.version.body", EnUS: "The editor has unsaved changes. Switching versions will replace the current content.", PtBR: "O editor tem alterações não salvas. Trocar de versão vai substituir o conteúdo atual."},

	// ── Help / readme tab fallback ───────────────────────────────────────────
	//
	// Used by the WASM IDE main menu panel when a readme tab has no
	// "# heading" line — the parser leaves the Title field empty as a
	// sentinel and the renderer substitutes this localised string. Also
	// covers admin-edited menu-tree help blobs, which carry no title at
	// all and are wrapped as a single empty-titled tab.
	{ID: "help.title.notFound", EnUS: "# title not found", PtBR: "# título não encontrado"},
}

// SeedTranslations inserts the bootstrap translation set for en-US and pt-BR
// only when a locale has no messages yet. Safe to call on every startup.
//
// This is the single seeding entry point for translations. The call site is
// db.go migrate(), right after seedLocales() creates the bundle rows.
func SeedTranslations() error {
	// Build a locale → slice map from the canonical seed list so we insert
	// once per locale inside a single prepared statement.
	byLocale := map[string][]TrMessage{
		"en-US": make([]TrMessage, 0, len(seedTranslations)),
		"pt-BR": make([]TrMessage, 0, len(seedTranslations)),
	}
	for _, e := range seedTranslations {
		byLocale["en-US"] = append(byLocale["en-US"], TrMessage{
			ID: e.ID, Other: e.EnUS, Description: e.Description,
		})
		byLocale["pt-BR"] = append(byLocale["pt-BR"], TrMessage{
			ID: e.ID, Other: e.PtBR, Description: e.Description,
		})
	}

	for _, locale := range []string{"en-US", "pt-BR"} {
		// Only seed if the locale has no messages yet. This keeps admin edits
		// safe across restarts: once a locale exists, the seed never touches it.
		var count int
		if err := DB.QueryRow(`
			SELECT COUNT(*) FROM i18n_messages WHERE locale = ?`, locale,
		).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		stmt, err := DB.Prepare(`
			INSERT OR IGNORE INTO i18n_messages
				(locale, message_id, other, one, description)
			VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		for _, m := range byLocale[locale] {
			if _, err := stmt.Exec(locale, m.ID, m.Other, m.One, m.Description); err != nil {
				stmt.Close()
				return err
			}
		}
		stmt.Close()
	}

	return nil
}

// ─── Missing key telemetry (public endpoint) ──────────────────────────────────

// InsertMissingMessage records a translation key the IDE WASM runtime failed
// to resolve. The key is inserted into EVERY registered locale with the caller-
// supplied value (prefixed by "*" at the handler) so the admin can see which
// locales still need a real translation.
//
// Contract:
//   - Never overwrites an existing translation (INSERT OR IGNORE).
//   - Ensures the i18n_bundles row exists for each locale before inserting.
//   - Message order is irrelevant on read — GetBundle sorts by message_id.
//
// This endpoint is open (no OTP, no auth) because any maker running the IDE
// legitimately hits missing keys during normal use; the cost of telling them
// to log in first would outweigh the telemetry value.
func InsertMissingMessage(m TrMessage) error {
	locales, err := ListLocales()
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, locale := range locales {
		// Ensure the bundle row exists.
		if _, err = tx.Exec(`
			INSERT OR IGNORE INTO i18n_bundles (locale, bundle_id, updated_at)
			VALUES (?, ?, ?)`,
			locale, locale+"-custom", now,
		); err != nil {
			return err
		}

		// INSERT OR IGNORE — never overwrites an existing translation.
		if _, err = tx.Exec(`
			INSERT OR IGNORE INTO i18n_messages (locale, message_id, other, one, description)
			VALUES (?, ?, ?, '', '')`,
			locale, m.ID, m.Other,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// MigrateSeqRemovePhaseText — 2026-07-19. The Sequence's phase-removal
// menu item generalized from "remove the LAST phase" to "remove the
// phase ON SCREEN" (phase surgery), but the auto-registered i18n row
// (INSERT OR IGNORE above: first sighting wins, fallbacks never
// overwrite) still carried the old label, so the field kept seeing
// "− Remove last phase" over the new behavior. SURGICAL update: only
// rows still holding the exact old English fallback flip to the new
// one — any human-edited translation is untouched, in every locale.
// Português: O item de remoção de fase generalizou de "última" para
// "a fase em cena", mas a linha auto-registrada (primeira aparição
// vence; fallback nunca sobrescreve) ainda carregava o rótulo antigo.
// Atualização CIRÚRGICA: só linhas com o fallback inglês antigo exato
// mudam — tradução editada por humano fica intacta, em todo locale.
func MigrateSeqRemovePhaseText() error {
	_, err := DB.Exec(`
		UPDATE i18n_messages
		   SET other = '− Remove this phase'
		 WHERE message_id = 'seqRemovePhase'
		   AND other      = '− Remove last phase'`)
	return err
}
