// ide/stagefileclient/client.go — HTTP client for the stage file API.
//
// Provides blocking fetch functions for the WASM IDE to communicate with the
// server's stage file endpoints. Every function blocks on a channel until the
// JavaScript fetch Promise resolves, following the same pattern used by
// mainMenu/sections.go and blackbox/loader.go.
//
// All functions must be called from a goroutine — they block the calling
// goroutine (not the main thread) until the network round-trip completes.
//
// Authentication: Bearer token from rulesServer.GetAuthToken(). When the token
// is empty (standalone dev mode), requests are sent without auth and the server
// returns 401 — callers degrade gracefully.
//
// Português:
//
//	Funções de fetch bloqueantes para o cliente WASM da IDE. Cada função
//	bloqueia via channel até o Promise do fetch resolver. Devem ser chamadas
//	de goroutines.
package stagefileclient

import (
	"encoding/json"
	"fmt"
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// ─── Client types ─────────────────────────────────────────────────────────────

// StageFileKind values mirror the server-side constants. See
// server/store/stage_files.go for the canonical list and
// /ide/docs/DELIVERY_C_TUTORIAL_DESIGN.md for the semantics.
//
// Português: Valores do campo `kind`. Espelham as constantes do
// server; use estas ao construir request bodies.
const (
	// StageFileKindStage — regular saved scene. The default.
	StageFileKindStage = "stage"

	// StageFileKindTutorial — guided tutorial file. The file manager
	// renders a book icon and a Start button for these rows.
	StageFileKindTutorial = "tutorial"
)

// StageFileLanguage values mirror the server-side constants. See
// server/store/stage_files.go for the canonical list and the design
// document for the rationale of why language is irreversible.
//
// Português: Valores do campo `language`. Espelham o server. A
// escolha é fixada na criação do projeto e nunca muda — não há
// função UpdateFile para "trocar" a linguagem.
const (
	// StageFileLanguageGo — project compiles to Go via the codegen
	// Go backend. Full feature set, including black-boxes.
	StageFileLanguageGo = "go"

	// StageFileLanguageC — project compiles to ANSI C99 via the
	// codegen C backend. Default when the user closes the welcome
	// modal without picking (X / ESC). Phase 1: primitives only,
	// no black-boxes.
	StageFileLanguageC = "c"
)

// StageFileEntry is a single file returned by the API (list view — no sceneJson).
//
// The Kind field discriminates between regular scenes ("stage", the
// default) and tutorial files ("tutorial"). File manager consumers
// read this to choose the icon and action label on each row.
//
// The Language field carries the project's compile target — "c"
// (C99) or "go". The welcome modal reads it to render a chip next
// to each row; the workspace reads it to filter the device menu
// and the export menu. The value is fixed at creation and never
// changes — see the API documentation for handleUpdateFile.
type StageFileEntry struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	FolderID string `json:"folderId,omitempty"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`     // "stage" (default) | "tutorial"
	Language string `json:"language,omitempty"` // "c" (default) | "go"
	// IconID is the FontAwesome Free icon name chosen by the maker
	// (e.g. "cpu", "thermometer"). Empty means "no choice" — the UI
	// substitutes its default at render time. The server validates
	// only the format; whether the value resolves to a real icon
	// is the client's responsibility (the picker filters against
	// window.FA_FREE_STYLES).
	IconID      string `json:"iconId,omitempty"`
	DeviceCount int    `json:"deviceCount"`
	IsBackup    bool   `json:"isBackup"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// StageFileFull is a single file returned by the API (detail view — includes sceneJson).
type StageFileFull struct {
	StageFileEntry
	SceneJSON string `json:"sceneJson"`
}

// StageFolderEntry is a single folder returned by the API.
type StageFolderEntry struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	ParentID  string `json:"parentId,omitempty"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// LimitInfo is the usage vs capacity info returned by GET /limit.
type LimitInfo struct {
	MaxFiles    int    `json:"maxFiles"`
	UsedFiles   int    `json:"usedFiles"`
	LimitSource string `json:"limitSource"`
}

// ─── Files ────────────────────────────────────────────────────────────────────

// ListFiles returns all stage files for the authenticated user.
// When folderID is non-empty, only files in that folder are returned.
func ListFiles(folderID string) ([]StageFileEntry, error) {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFiles
	if folderID != "" {
		url += "?folderId=" + folderID
	}

	raw, err := doFetch("GET", url, "")
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data struct {
			Files []StageFileEntry `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return envelope.Data.Files, nil
}

// LoadFile returns a single file including the full scene_json.
func LoadFile(fileID string) (*StageFileFull, error) {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFiles + "/" + fileID

	raw, err := doFetch("GET", url, "")
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data struct {
			File StageFileFull `json:"file"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &envelope.Data.File, nil
}

// SaveFile creates a new stage file (kind = "stage") in the given
// folder, with the supplied project language, optional icon and the
// scene JSON snapshot. Passing language = "" lets the server resolve
// the default ("c"). Passing iconID = "" persists NULL — the UI
// applies its default icon at render time.
//
// Argument order: strings grouped first (name → sceneJson → iconId),
// numeric tail last. Same shape as UpdateFile.
//
// Português: Cria arquivo com kind "stage". Passe "" em language
// para o default ("c"); passe "" em iconID para que a UI use o
// ícone padrão no render.
func SaveFile(name, folderID, language, sceneJSON, iconID string, deviceCount int) (*StageFileEntry, error) {
	return saveFileInternal(name, folderID, StageFileKindStage, language, sceneJSON, iconID, deviceCount, false)
}

// SaveFileWithKind creates a new stage file with an explicit kind
// ("stage" or "tutorial"), the supplied project language, and an
// optional icon. Pass empty strings to let the server resolve the
// defaults ("stage" and "c" respectively). Pass iconID = "" to
// persist NULL — the UI uses its default at render time.
//
// Português: Cria arquivo com kind e linguagem explícitos. Use
// "tutorial" para publicar um tutorial pela UI de autoria
// (Delivery C-2). iconID = "" significa "sem escolha".
func SaveFileWithKind(name, folderID, kind, language, sceneJSON, iconID string, deviceCount int) (*StageFileEntry, error) {
	return saveFileInternal(name, folderID, kind, language, sceneJSON, iconID, deviceCount, false)
}

// SaveBackupFile creates a new backup file that does not count against the
// user's file limit. The server marks it with is_backup=1 so it is excluded
// from the limit count. Always created as a regular stage (kind = "stage").
//
// Backups intentionally do not carry an icon — they are transient
// recovery rows that never surface in the file manager listing, so
// the icon column is wasted metadata. The wire format omits iconId
// entirely; the server stores NULL.
//
// The backup inherits the parent project's language so a Go project's
// backup is itself a Go backup — preserves the language chip on
// "(backup)" rows in the welcome modal.
//
// Português: Cria backup que não conta no limite. Backups não têm
// ícone (transitórios, nunca aparecem no file manager).
func SaveBackupFile(name, folderID, language, sceneJSON string, deviceCount int) (*StageFileEntry, error) {
	return saveFileInternal(name, folderID, StageFileKindStage, language, sceneJSON, "", deviceCount, true)
}

// saveFileInternal is the shared implementation for SaveFile,
// SaveFileWithKind, and SaveBackupFile.
func saveFileInternal(name, folderID, kind, language, sceneJSON, iconID string, deviceCount int, isBackup bool) (*StageFileEntry, error) {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFiles

	// Only emit the kind field when non-empty — the server defaults
	// empty to "stage", so omitting keeps the wire format smaller
	// for the common case of a regular save.
	kindFragment := ""
	if kind != "" {
		kindFragment = fmt.Sprintf(`"kind":%q,`, kind)
	}

	// Same lazy-emit rule for language: server defaults empty to
	// "c" (C99). Most calls pass a non-empty value (the workspace's
	// fixed project language), so the fragment is usually present.
	languageFragment := ""
	if language != "" {
		languageFragment = fmt.Sprintf(`"language":%q,`, language)
	}

	// iconId follows the same lazy-emit rule. Empty omits the field
	// entirely, which the server reads as "no icon chosen" and
	// stores NULL. Non-empty values pass through and are format-
	// validated server-side ([a-z0-9-]+).
	iconFragment := ""
	if iconID != "" {
		iconFragment = fmt.Sprintf(`"iconId":%q,`, iconID)
	}

	body := fmt.Sprintf(
		`{"name":%q,"folderId":%q,%s%s%s"sceneJson":%s,"deviceCount":%d,"isBackup":%t}`,
		name, folderID, kindFragment, languageFragment, iconFragment, quoteJSON(sceneJSON), deviceCount, isBackup,
	)

	raw, err := doFetch("POST", url, body)
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data struct {
			File StageFileEntry `json:"file"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &envelope.Data.File, nil
}

// UpdateFile updates an existing file. All fields are optional —
// empty strings preserve the current value on the server. To convert
// a regular stage into a tutorial (or vice versa), pass the new value
// in `kind`; pass empty string to leave the current value untouched.
//
// iconID follows the same "empty = no change" rule, plus a sentinel:
//
//   - iconID == ""           → leave the current icon alone
//   - iconID == "__clear__"  → reset to NULL (UI uses its default)
//   - iconID == "cube" (etc) → set to that name
//
// The sentinel is needed because empty already means "no change", so
// without it there would be no way to clear a previously chosen icon
// — only to overwrite it.
//
// Português: Atualiza campos. "" = manter; "__clear__" em iconID
// reseta para NULL (UI aplica ícone default no render).
func UpdateFile(fileID, name, folderID, kind, sceneJSON, iconID string, deviceCount int) error {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFiles + "/" + fileID

	// Build JSON body with only non-empty fields. Writes look like:
	//   {"name":"Foo"}                       — rename only
	//   {"kind":"tutorial"}                  — convert to tutorial only
	//   {"iconId":"cpu"}                     — change icon only
	//   {"iconId":"__clear__"}               — reset icon to default
	//   {"name":"Foo","iconId":"cpu"}        — rename + change icon (Edit dialog)
	//   {"sceneJson":"{...}","deviceCount":N}  — save only
	fields := "{"
	first := true
	addField := func(key, val string) {
		if val == "" {
			return
		}
		if !first {
			fields += ","
		}
		fields += fmt.Sprintf("%q:%q", key, val)
		first = false
	}
	addField("name", name)
	addField("folderId", folderID)
	addField("kind", kind)
	addField("iconId", iconID)

	if sceneJSON != "" {
		if !first {
			fields += ","
		}
		fields += fmt.Sprintf(`"sceneJson":%s,"deviceCount":%d`, quoteJSON(sceneJSON), deviceCount)
	}
	fields += "}"

	_, err := doFetch("PUT", url, fields)
	return err
}

// DeleteFile deletes a stage file.
func DeleteFile(fileID string) error {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFiles + "/" + fileID
	_, err := doFetch("DELETE", url, "")
	return err
}

// GetLimit returns the user's file limit and current usage.
func GetLimit() (*LimitInfo, error) {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFileLimit

	raw, err := doFetch("GET", url, "")
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data LimitInfo `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &envelope.Data, nil
}

// ─── Folders ──────────────────────────────────────────────────────────────────

// ListFolders returns all folders for the authenticated user.
func ListFolders() ([]StageFolderEntry, error) {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFileFolders

	raw, err := doFetch("GET", url, "")
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data struct {
			Folders []StageFolderEntry `json:"folders"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return envelope.Data.Folders, nil
}

// CreateFolder creates a new virtual folder. Returns the created entry.
func CreateFolder(name, parentID string) (*StageFolderEntry, error) {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFileFolders

	body := fmt.Sprintf(`{"name":%q,"parentId":%q}`, name, parentID)

	raw, err := doFetch("POST", url, body)
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data struct {
			Folder StageFolderEntry `json:"folder"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &envelope.Data.Folder, nil
}

// RenameFolder renames a folder.
func RenameFolder(folderID, newName string) error {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFileFolders + "/" + folderID
	body := fmt.Sprintf(`{"name":%q}`, newName)
	_, err := doFetch("PUT", url, body)
	return err
}

// DeleteFolder deletes a folder and all its contents (CASCADE).
func DeleteFolder(folderID string) error {
	url := rulesServer.ServerURL + rulesServer.EndpointStageFileFolders + "/" + folderID
	_, err := doFetch("DELETE", url, "")
	return err
}

// ─── HTTP fetch (blocking, runs from goroutine) ──────────────────────────────

// doFetch performs a synchronous HTTP request using the browser's fetch API.
// Blocks the calling goroutine until the Promise resolves. Must NOT be called
// from the main goroutine — always call from go func() { ... }().
//
// Returns the raw JSON response bytes. On HTTP errors (non-2xx), returns an
// error with the server's error message when available.
func doFetch(method, url, jsonBody string) ([]byte, error) {
	token := rulesServer.GetAuthToken()

	type result struct {
		raw []byte
		err string
	}
	ch := make(chan result, 1)

	// Build fetch options.
	opts := js.Global().Get("Object").New()
	opts.Set("method", method)

	headers := js.Global().Get("Object").New()
	headers.Set("Content-Type", "application/json")
	if token != "" {
		headers.Set("Authorization", token)
	}
	opts.Set("headers", headers)

	if jsonBody != "" && method != "GET" {
		opts.Set("body", jsonBody)
	}

	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			// Try to parse error from response body.
			return resp.Call("json")
		}
		return resp.Call("json")
	})

	thenParse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			ch <- result{err: "server returned null body"}
			return nil
		}
		raw := js.Global().Get("JSON").Call("stringify", args[0]).String()

		// Check for API error envelope.
		var envelope struct {
			Metadata struct {
				Status int    `json:"status"`
				Error  string `json:"error"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err == nil {
			if envelope.Metadata.Status >= 400 {
				errMsg := envelope.Metadata.Error
				if errMsg == "" {
					errMsg = fmt.Sprintf("HTTP %d", envelope.Metadata.Status)
				}
				ch <- result{err: errMsg}
				return nil
			}
		}

		ch <- result{raw: []byte(raw)}
		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := "network error"
		if len(args) > 0 && args[0].Get("message").Truthy() {
			msg = args[0].Get("message").String()
		}
		ch <- result{err: msg}
		return nil
	})

	js.Global().Call("fetch", url, opts).
		Call("then", thenResponse).
		Call("then", thenParse).
		Call("catch", catchFn)

	res := <-ch
	thenResponse.Release()
	thenParse.Release()
	catchFn.Release()

	if res.err != "" {
		log.Printf("[stagefileclient] %s %s → error: %s", method, url, res.err)
		return nil, fmt.Errorf("%s", res.err)
	}

	log.Printf("[stagefileclient] %s %s → %d bytes", method, url, len(res.raw))
	return res.raw, nil
}

// quoteJSON encodes a raw JSON string as a JSON string value for embedding
// inside a larger JSON body. The server's struct field is `sceneJson string`,
// so the value must be a quoted, escaped string — NOT an embedded object.
//
// Example: `{"key":"value"}` → `"{\"key\":\"value\"}"`
func quoteJSON(s string) string {
	if s == "" {
		s = "{}"
	}
	b, _ := json.Marshal(s)
	return string(b)
}
