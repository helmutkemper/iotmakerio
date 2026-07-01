// ide/rulesServer/rules.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesServer

// rulesServer — Centralized server configuration constants.
//
// English:
//
//	Defines server URL, default locale, and API endpoint paths.
//	Used by the WASM client (translate package, etc) to communicate
//	with the backend server.
//
// Português:
//
//	Define URL do servidor, locale padrão e caminhos de endpoints da API.
//	Usado pelo cliente WASM (pacote translate, etc) para comunicar
//	com o servidor backend.

import "syscall/js"

// ServerURL is the base URL of the backend server.
//
// English:
//
//	Change this for production deployment or when the server
//	runs on a different host/port.
//
// Português:
//
//	Altere para deploy em produção ou quando o servidor
//	rodar em outro host/porta.
//
// ServerURL is resolved at runtime from window.location.origin so the WASM
// always calls back to the same host that served the page — localhost, LAN IP,
// or production domain — without any hardcoded value.
//
// window.location.origin returns the scheme + host + port with no trailing
// slash (e.g. "http://192.168.1.10:8080"). All endpoint constants start with
// "/" so concatenation is always correct: origin + "/api/v1/..." works on
// localhost, LAN, and production alike.
var ServerURL = func() string {
	origin := js.Global().Get("location").Get("origin").String()
	if origin == "" || origin == "null" {
		return "" // fallback: endpoints are already absolute-path relative
	}
	return origin
}()

// DefaultLocale is the fallback locale when the browser locale
// is not available on the server.
var DefaultLocale = "en-US"

// API endpoint paths (appended to ServerURL).
const (
	// EndpointTranslations is the i18n translation bundle endpoint.
	// Usage: ServerURL + EndpointTranslations + locale
	// Example: http://localhost:8080/api/v1/translations/pt-BR
	EndpointTranslations = "/api/v1/translations/"

	// EndpointBlackBox is the black-box device listing endpoint for the WASM IDE.
	// Returns a JSON array of all saved black-box definitions in the format
	// that StatementBlackBoxInit / StatementBlackBoxRun expect.
	//
	// Usage: GET ServerURL + EndpointBlackBox
	// Example: GET http://localhost:8080/api/v1/blackbox
	//
	// The response is intentionally lightweight — it omits StructCode,
	// MethodsCode and Imports because those are only needed by the codegen
	// backend. The IDE only needs port names, types, and property labels.
	EndpointBlackBox = "/api/v1/blackbox"

	// EndpointTemplates is the template package list endpoint.
	// Returns templates visible to the authenticated caller:
	//   - All of the caller's own templates (any status).
	//   - All public+ready templates from other specialists.
	//
	// Usage: GET ServerURL + EndpointTemplates
	// Example: GET http://localhost:8080/api/v1/templates
	//
	// Requires Authentication: Bearer <token> from window._ideAuthToken.
	EndpointTemplates = "/api/v1/templates"

	// EndpointTemplate is the single-template detail endpoint.
	// Returns the full template record and its parsed definition (when ready).
	//
	// Usage: GET ServerURL + EndpointTemplate + "/" + templateID
	// Example: GET http://localhost:8080/api/v1/templates/abc123
	//
	// Requires Authentication: Bearer <token> from window._ideAuthToken.
	EndpointTemplate = "/api/v1/templates"

	// EndpointTemplateGenerate is the template generation endpoint.
	// Accepts a config map and returns a configured project ZIP.
	//
	// Usage: POST ServerURL + EndpointTemplateGenerate + "/" + templateID + "/generate"
	// Body: {"config": {"VarName": "value", ...}}
	//
	// Requires Authentication: Bearer <token> from window._ideAuthToken.
	EndpointTemplateGenerate = "/api/v1/templates"

	// EndpointMenuSections is the dynamic menu sections endpoint.
	// Returns active sections visible to the requesting user, with their
	// items pre-loaded. Called once at IDE startup by LoadSections().
	//
	// Authentication: optional — anonymous users receive unrestricted sections.
	//
	// Usage: GET ServerURL + EndpointMenuSections
	EndpointMenuSections = "/api/v1/menu/sections"

	// EndpointMenuCategories returns all categories with icons for the IDE menu.
	// Usage: GET ServerURL + EndpointMenuCategories
	EndpointMenuCategories = "/api/v1/menu/categories"

	// EndpointMenuTree returns the complete resolved menu tree for the active
	// profile. The server resolves all cascades (labels, icons, help) so the
	// WASM receives ready-to-use data.
	//
	// Query params: ?locale=pt (browser locale for label/help resolution)
	//
	// Usage: GET ServerURL + EndpointMenuTree + "?locale=" + locale
	EndpointMenuTree = "/api/v1/menu/tree"

	// EndpointHelp is the base path for device help markdown files.
	// Help files are organised as:
	//   /help/devices/<category>/<itemID>/<locale>.md
	// where locale is the browser language tag lowercased with hyphen
	// (e.g. "pt-br", "en"). The WASM client tries the browser locale first,
	// then falls back to "en".
	//
	// Usage: GET ServerURL + EndpointHelp + "/devices/math/Add/pt-br.md"
	EndpointHelp = "/help"

	// ─── Stage files (saved IDE scenes) ──────────────────────────────────

	// EndpointStageFiles is the base path for stage file CRUD.
	// The WASM IDE uses this to save, load, rename, and delete scenes.
	//
	// Requires Authentication: Bearer <token> from window._ideAuthToken.
	//
	// Usage:
	//   GET    ServerURL + EndpointStageFiles              — list files
	//   POST   ServerURL + EndpointStageFiles              — create file
	//   GET    ServerURL + EndpointStageFiles + "/" + id    — load file
	//   PUT    ServerURL + EndpointStageFiles + "/" + id    — update file
	//   DELETE ServerURL + EndpointStageFiles + "/" + id    — delete file
	EndpointStageFiles = "/api/v1/stage-files"

	// EndpointStageFileFolders is the path for virtual folder CRUD.
	//
	// Usage:
	//   GET    ServerURL + EndpointStageFileFolders              — list folders
	//   POST   ServerURL + EndpointStageFileFolders              — create folder
	//   PUT    ServerURL + EndpointStageFileFolders + "/" + id    — rename/move
	//   DELETE ServerURL + EndpointStageFileFolders + "/" + id    — delete
	EndpointStageFileFolders = "/api/v1/stage-files/folders"

	// EndpointStageFileLimit returns the user's file limit and current usage.
	//
	// Usage: GET ServerURL + EndpointStageFileLimit
	EndpointStageFileLimit = "/api/v1/stage-files/limit"

	// EndpointPanelPrefs is the endpoint for saving/loading IDE panel column widths.
	// Widths are stored per user+OS+browser combination.
	//
	// Usage:
	//   GET ServerURL + EndpointPanelPrefs + "?os=macos&browser=chrome"
	//   PUT ServerURL + EndpointPanelPrefs  (body: { os, browser, rail_width, list_width })
	EndpointPanelPrefs = "/api/v1/profile/panel-prefs"

	// EndpointStagePrefs is the endpoint for per-user stage behaviour
	// preferences — zoom sensitivity, left-drag-to-pan toggle, and
	// cursor hints. Written by the portal's Editor Settings page and
	// read by the WASM IDE at workspace startup.
	//
	// The server merges any NULL fields with the compile-time defaults
	// before returning, so clients always receive concrete values.
	//
	// Português: Endpoint de preferências de comportamento da stage
	// (zoom, pan, cursor). Escrito pela página Editor Settings do
	// portal, lido pelo IDE no startup do workspace. O servidor
	// mescla defaults — clientes sempre recebem valores concretos.
	//
	// Usage:
	//   GET    ServerURL + EndpointStagePrefs  — resolved prefs + defaults
	//   PUT    ServerURL + EndpointStagePrefs  — patch (any subset of fields)
	//   DELETE ServerURL + EndpointStagePrefs  — reset user row, returns defaults
	EndpointStagePrefs = "/api/v1/editor/stage-prefs"
)

// GetAuthToken returns the Bearer authorization header value for authenticated
// API calls from the WASM IDE.
//
// The token is provided by the SPA page that embeds the WASM binary. When the
// user logs in through the SPA, the JavaScript side sets:
//
//	window._ideAuthToken = "Bearer " + jwtToken;
//
// This function reads that value. If the value is absent or empty (e.g. the
// WASM is running standalone without a logged-in SPA), it returns an empty
// string. Callers must handle the empty case (typically by degrading gracefully
// rather than sending an unauthenticated request that would return 401).
//
// Português:
//
//	Retorna o valor do header de autorização Bearer para chamadas autenticadas.
//	O token é definido pela página SPA que embute o WASM:
//	  window._ideAuthToken = "Bearer " + jwtToken;
//	Retorna "" se não disponível — chamadores devem degradar graciosamente.
func GetAuthToken() string {
	val := js.Global().Get("_ideAuthToken")
	if val.IsUndefined() || val.IsNull() {
		return ""
	}
	return val.String()
}
