// server/handler/blackboxapi/handler.go — Implementation of GET /api/v1/blackbox.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// This endpoint feeds the WASM IDE component bank.
// It reads from two sources:
//
//  1. project_code_versions — Go source saved by the maker in the Monaco editor.
//     Parsed on-the-fly (source → BlackBoxDef).
//
//  2. blackboxes — GitHub-sourced devices submitted by specialists.
//     Already parsed by the worker; stored as parsed_json (BlackBoxDef JSON).
//
// Nothing else changes:
//   - Same route:   GET /api/v1/blackbox
//   - Same parser:  server/codegen/blackbox
//   - Same DTOs:    clientBlackBoxDef and friends
//   - Same contract with the WASM client (blackbox.BlackBoxDefClient)
//
// The WASM binary does NOT need to be recompiled unless it wants to consume
// the new ownership fields — it keeps calling the same URL and receives the
// same JSON shape it always expected, with two additional optional fields.
//
// Soft-failure policy: projects whose source fails to parse are logged and
// skipped. One broken project does not prevent the rest from loading.
//
// Security — Doc string sanitization:
//
//	Doc strings (package godoc, method comments, port descriptions) are written
//	by specialists and rendered by the IDE via marked.js → innerHTML. Without
//	sanitization a malicious specialist could inject arbitrary HTML/JS into the
//	doc overlay of any maker who opens the component.
//
//	sanitizeDoc() strips every HTML tag from any doc string before it leaves
//	the server. This is intentionally aggressive: doc strings are plain-text
//	prose, never HTML. Any angle-bracket content in a godoc comment is either
//	accidental or malicious.
//
//	Manual page Content is NOT stripped here — manual pages are authored in
//	Markdown and are expected to contain code fences and occasional inline HTML.
//	The overlay renders them inside a sandboxed <div> with marked.js, which has
//	its own sanitization layer.
//
// Ownership fields (Origin, IsOwn):
//
//	Every item this endpoint returns belongs to the authenticated caller —
//	the endpoint is filtered by user_id at the store layer. Therefore every
//	response row is stamped with Origin = originOwn and IsOwn = true.
//
//	This matters because the WASM client receives devices from TWO sources
//	in a single boot: this endpoint (always "own") AND the menu tree endpoint
//	which embeds curated devices from other specialists (stamped "curated"
//	on the client side, see stageWorkspace/workspace.go extractEmbeddedDefs).
//	The client uses IsOwn to decide what appears under "My Items".
//
//	The "curated" string is intentionally NOT used here — it is a contextual
//	attribute, not a property of the stored device row. See the Phase 1 doc
//	at /ide/docs/tasks/REFACTOR_MY_ITEMS_PHASE_1.md, section "Why curated is
//	stamped on the client, not the server".
//
// Parser limits:
//
//	This endpoint lists all public components regardless of who owns them.
//	There is no authenticated user in this context so the parser uses
//	store.GetParserLimits("") which applies global limits and compile-time
//	fallbacks. Stored source code has already passed the upload-time limit
//	check so DefaultParserLimits would also be correct; using GetParserLimits("")
//	ensures any admin-lowered global limit is still respected for display.
//
// Response shape:
//
//	[
//	  {
//	    "name": "MyDevice",
//	    "structIcon": "gear",
//	    "structLabel": "My Device",
//	    "doc": "Package blackbox — describes the device.",
//	    "origin": "own",
//	    "isOwn":  true,
//	    "init": { ... },
//	    "methods": [ ... ]
//	  },
//	  ...
//	]
package blackboxapi

import (
	"encoding/json"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"

	bbparser "server/codegen/blackbox"
	"server/middleware"
	"server/store"
)

// ─── Ownership origin constants ────────────────────────────────────────────────
//
// These strings travel in the JSON response as the "origin" field. The WASM
// client mirrors them in /ide/blackbox/clientTypes.go. Keep the two sides in
// sync — a typo here produces silent "My Items" filtering bugs on the client.
//
// "curated" is intentionally absent from this file: devices reach the client
// via TWO channels, and this endpoint is the "always own" channel. The other
// channel (menu tree embeds) is stamped "curated" on the client, not here.
const (
	// originOwn marks a device owned by the authenticated caller.
	originOwn = "own"
)

// htmlTagRe matches any HTML/XML tag: <tag>, </tag>, <tag/>, <tag attr="val">.
// Used by sanitizeDoc to strip tags from plain-text doc strings.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// sanitizeDoc strips HTML tags from a plain-text doc string and unescapes any
// existing HTML entities so the result is clean prose.
//
// This prevents a malicious specialist from injecting HTML or JavaScript into
// godoc comments that are rendered by the IDE's marked.js → innerHTML pipeline.
//
// The function is intentionally aggressive: legitimate godoc strings never
// contain HTML tags. Anything inside angle brackets in a Go comment is either
// a generic type parameter (e.g. map[string]any) or injection — both cases
// are safe to strip because the WASM IDE only uses the doc for display text,
// not for type information.
func sanitizeDoc(s string) string {
	// Strip all HTML/XML tags first.
	stripped := htmlTagRe.ReplaceAllString(s, "")
	// Unescape HTML entities so "&lt;script&gt;" becomes "<script>" and gets
	// re-stripped on the next pass — defence-in-depth against double-encoding.
	unescaped := html.UnescapeString(stripped)
	if unescaped != stripped {
		unescaped = htmlTagRe.ReplaceAllString(unescaped, "")
	}
	return unescaped
}

// ─── Response DTOs ─────────────────────────────────────────────────────────────
//
// These types mirror the WASM-side blackbox.BlackBoxDefClient types exactly.
// Field names and JSON tags must not change — the WASM json.Unmarshal relies
// on them. Kept in this file so the package compiles without the legacy
// blackboxes table.

type clientPortDef struct {
	Name    string `json:"name"`
	GoType  string `json:"goType"`
	IsError bool   `json:"isError,omitempty"`
	Doc     string `json:"doc,omitempty"`
	// PassThrough marks a synthesized C99 handle pass-through output (the
	// republished wire-type input, for chaining resource blocks LabVIEW-
	// style). Set only by the Functions synthesis in toClientDef via
	// bbparser.BlackBoxDef.FunctionSynthesizedOutputs; the Go method/init
	// path never sets it. See docs/c99_ide_integration.md §2.1.
	PassThrough bool `json:"passThrough,omitempty"`
	// CallbackType is the function-pointer typedef a callback PARAMETER expects
	// (e.g. "display_write_fn"), set by the parser on a C99 consumer's callback
	// input port. The WASM uses it to enforce the strict ƒ-wire type rule (only
	// a callback reference of the SAME type may be wired in) and to render the ƒ
	// glyph. Empty on Go ports and on ordinary C99 ports. See the duality
	// section of docs/CODEGEN_C99_CALLBACKS.md.
	CallbackType string `json:"callbackType,omitempty"`
}

type clientFuncDef struct {
	Doc            string          `json:"doc,omitempty"`
	ExecutionOrder int             `json:"executionOrder,omitempty"`
	Icon           string          `json:"icon,omitempty"`
	Label          string          `json:"label,omitempty"`
	MenuCol        int             `json:"menuCol,omitempty"`
	MenuRow        int             `json:"menuRow,omitempty"`
	MenuPosSet     bool            `json:"menuPosSet,omitempty"`
	Inputs         []clientPortDef `json:"inputs,omitempty"`
	Outputs        []clientPortDef `json:"outputs,omitempty"`
}

type clientPropDef struct {
	FieldName string   `json:"fieldName"`
	GoType    string   `json:"goType"`
	Label     string   `json:"label"`
	Default   string   `json:"default,omitempty"`
	Options   []string `json:"options,omitempty"`

	// Connection is the diagram role identifier from the `connection` struct
	// tag (e.g. "I2C_SDA"). When non-empty, the IDE links this prop to an
	// interactive SVG diagram — the prop's value is matched against data-id
	// attributes and the role's colour comes from the SVG's data-palette.
	Connection string `json:"connection,omitempty"`

	// Container shape (Slice 2.x). Empty for scalars; "map" or "slice"
	// when the field's Go type is a composite. The renderer uses these
	// to choose between the existing FieldText/FieldSelect path and the
	// future FieldMap / FieldSlice paths. See bbparser.PropDef for the
	// full grammar.
	Container   string `json:"container,omitempty"`
	KeyType     string `json:"keyType,omitempty"`
	ValueType   string `json:"valueType,omitempty"`
	NativeKey   bool   `json:"nativeKey,omitempty"`
	NativeValue bool   `json:"nativeValue,omitempty"`
}

type clientManualPage struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	ShowIn   string `json:"showIn"`
	Content  string `json:"content"`
}

// clientHelpTab mirrors bbparser.HelpTab for the WASM client.
type clientHelpTab struct {
	Order   int    `json:"order"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// clientMethodHelp mirrors bbparser.MethodHelp for the WASM client.
type clientMethodHelp struct {
	Langs map[string][]clientHelpTab `json:"langs,omitempty"`
}

// clientDeviceHelp mirrors bbparser.DeviceHelp for the WASM client.
type clientDeviceHelp struct {
	// Readme is the per-language ordered tab slice for the device readme.
	// Same shape as MethodHelp.Langs — see types.go DeviceHelp.Readme for
	// why the readme dropped its single-string form in favour of tabs.
	Readme  map[string][]clientHelpTab  `json:"readme,omitempty"`
	Methods map[string]clientMethodHelp `json:"methods,omitempty"`
}

type clientMethodDef struct {
	Name           string          `json:"name"`
	Doc            string          `json:"doc,omitempty"`
	ExecutionOrder int             `json:"executionOrder,omitempty"`
	Icon           string          `json:"icon,omitempty"`
	Label          string          `json:"label,omitempty"`
	MenuCol        int             `json:"menuCol,omitempty"`
	MenuRow        int             `json:"menuRow,omitempty"`
	MenuPosSet     bool            `json:"menuPosSet,omitempty"`
	Inputs         []clientPortDef `json:"inputs,omitempty"`
	Outputs        []clientPortDef `json:"outputs,omitempty"`
}

// clientFunctionDef is the DTO for a C99 device-function (decision b: every
// public function is a standalone device, no method/instance). It mirrors
// clientMethodDef field-for-field; the difference is purely routing — the
// WASM factory creates an independent block with no shared instanceId, and
// Outputs already include the synthesized handle pass-through (see
// toClientDef and docs/c99_ide_integration.md). Go projects never populate
// it; C99 projects never populate Init/Methods, so the two paths stay
// disjoint on the wire.
type clientFunctionDef struct {
	Name           string          `json:"name"`
	Doc            string          `json:"doc,omitempty"`
	ExecutionOrder int             `json:"executionOrder,omitempty"`
	Icon           string          `json:"icon,omitempty"`
	Label          string          `json:"label,omitempty"`
	MenuCol        int             `json:"menuCol,omitempty"`
	MenuRow        int             `json:"menuRow,omitempty"`
	MenuPosSet     bool            `json:"menuPosSet,omitempty"`
	Inputs         []clientPortDef `json:"inputs,omitempty"`
	Outputs        []clientPortDef `json:"outputs,omitempty"`
	// HandlerType marks this C99 function as a CALLBACK HANDLER (the value is
	// the function-pointer typedef it implements). CallbackMode refines it:
	// "both" → the IDE offers BOTH a callable device and a separate callback
	// reference device (scene type "CallbackRef:<fn>"); "ref" → only the
	// reference device. Both empty for ordinary functions. See the duality
	// section of docs/CODEGEN_C99_CALLBACKS.md.
	HandlerType  string `json:"handlerType,omitempty"`
	CallbackMode string `json:"callbackMode,omitempty"`
}

type clientBlackBoxDef struct {
	Name        string `json:"name"`
	Doc         string `json:"doc,omitempty"`
	StructIcon  string `json:"structIcon,omitempty"`
	StructLabel string `json:"structLabel,omitempty"`

	// Interactive is the public URL of the dual-mode SVG diagram resolved by
	// the worker (e.g. "/static/devices/owner/repo/rp2040.svg"). When non-empty,
	// the WASM Inspect panel activates this SVG within Help markdown content,
	// highlighting elements whose data-id matches props with a connection: tag.
	Interactive string `json:"interactive,omitempty"`

	// MenuName is the display name chosen in the portal (New Project modal).
	// Used as the label in the IDE menu entry. Falls back to StructLabel/Name.
	MenuName string `json:"menuName,omitempty"`
	// MenuCategory and MenuSubcategory drive the category→subcategory→name
	// menu hierarchy. Both hold the human-readable category names (not IDs)
	// so the WASM does not need to resolve them separately.
	MenuCategory    string            `json:"menuCategory,omitempty"`
	MenuSubcategory string            `json:"menuSubcategory,omitempty"`
	Init            *clientFuncDef    `json:"init,omitempty"`
	Methods         []clientMethodDef `json:"methods,omitempty"`
	// Functions carries C99 device-functions (decision b). Additive and
	// disjoint from the Go path (Init/Methods/Props): a Go black-box leaves
	// it empty, a C99 one leaves Init/Methods empty. See
	// docs/c99_ide_integration.md.
	Functions   []clientFunctionDef `json:"functions,omitempty"`
	Props       []clientPropDef     `json:"props,omitempty"`
	ManualPages []clientManualPage  `json:"manualPages,omitempty"`
	Help        clientDeviceHelp    `json:"help,omitempty"`

	// Origin identifies how this device reached the client. On this endpoint
	// the value is always "own" because the underlying query is filtered by
	// the caller's user id. Curated devices come through a different path
	// (embedded in the menu tree) and are stamped on the client side.
	//
	// Omitempty is intentional: unset means "unknown provenance", a safe
	// default for any future caller that forgets to populate it.
	Origin string `json:"origin,omitempty"`

	// IsOwn is the boolean shortcut for Origin == "own". Having both a
	// discriminator (Origin) and a flag (IsOwn) costs two bytes per item
	// and removes an entire class of "did you remember to compare to the
	// right constant?" bugs at the call site on the WASM client.
	IsOwn bool `json:"isOwn,omitempty"`

	// ProgrammingLanguage is the source language this device implements, as a
	// stage token ("go", "c", …). Stamped from the source project's
	// programming_language_id (Source 1) or the blackboxes row (Source 2),
	// normalized to the stage token space by normalizeLangToken. The WASM
	// menu uses it to hide devices of another language in the current
	// project (BlackBoxDefClient.SupportsProjectLanguage).
	ProgrammingLanguage string `json:"programmingLanguage,omitempty"`
}

// ─── Handler ──────────────────────────────────────────────────────────────────

// normalizeLangToken maps the programming_languages.id token space
// ("golang"/"c") to the stage/menu token space ("go"/"c") used by the WASM
// Workspace.Language and the device catalogue. It bridges a pre-existing
// dual-token split: projects.programming_language_id stores "golang", while
// stage_files.language and the catalogue use "go". Only "golang" needs
// mapping; every other token — "c" today, "arduino"/"python" later — is
// assumed to already match across both spaces and passes through (trimmed,
// lower-cased so the client receives a clean token for its equality check).
// Future work: align the two token spaces and retire this normalizer.
//
// Português: Normaliza o token de programming_languages ("golang"/"c") para o
// espaço do stage/menu ("go"/"c"). Só "golang" mapeia; o resto passa.
func normalizeLangToken(token string) string {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "golang" {
		return "go"
	}
	return t
}

func (h *handler) handleList(c echo.Context) error {
	limits := store.GetParserLimits("")
	defs := make([]clientBlackBoxDef, 0, 32)

	// Extract the authenticated user (set by OptionalAuth middleware).
	// This endpoint returns the caller's own private devices (source 1 =
	// project code versions, source 2 = ready devices). Both are filtered
	// by user_id, so an unauthenticated caller receives an empty array.
	// Returning early here prevents any future refactor from accidentally
	// leaking data when callerID is empty.
	var callerID string
	if user := middleware.UserFromContext(c); user != nil {
		callerID = user.ID
	}
	if callerID == "" {
		return c.JSON(http.StatusOK, defs) // empty array — never null
	}

	// ── Source 1: project_code_versions (maker's own code in Monaco editor) ───
	items, err := store.ListLatestProjectCodeVersions(callerID)
	if err != nil {
		c.Logger().Errorf("[blackboxapi/list] project versions DB error: %v", err)
	} else {
		for _, item := range items {
			// Dispatch on the project's own language token. Parsing a C99
			// source with the Go parser fails ("no exported struct") and the
			// continue below would silently drop the device from the catalog —
			// the bug this fixes. ParseForLanguage accepts the
			// programming_languages.id tokens directly ("golang"/"c").
			def, parseErr := bbparser.ParseForLanguage(item.Language, []byte(item.Source), limits)
			if parseErr != nil {
				log.Printf("[blackboxapi] skipping project %q (%s): parse error: %v",
					item.Name, item.ProjectID, parseErr)
				continue
			}

			// Attach help markdown from the project's File Manager.
			//
			// Why this lives here and not in toClientDef:
			//
			//   - The shape of where help comes from depends on the source.
			//     Source 1 (this loop) uses project_help_files (SQLite blobs).
			//     Source 2 below uses parsed_json that the worker already filled
			//     when ingesting a GitHub release.
			//
			//   - toClientDef is a pure converter: it copies whatever is on the
			//     server-side def. Putting the data fetch inside it would couple
			//     the converter to the database, which the second source already
			//     correctly avoids.
			//
			// Errors are non-fatal — a project without help files still appears
			// in the sidebar with a blank readme, exactly as before this change.
			// Logging the failure once per project gives us visibility without
			// blocking the listing.
			//
			// item.ProjectID is the project's UUID. The store call uses it as
			// the foreign key directly; no per-row UUID validation happens here
			// because every projects.id is generated by the same crypto/rand
			// pipeline as users.id, etc.
			attachProjectHelpFiles(def, item.ProjectID)

			cd := toClientDef(def)
			if cd.StructLabel == "" {
				cd.StructLabel = item.Name
			}
			// Stamp ownership — see package doc "Ownership fields" for the
			// rationale. Every item from this endpoint is by definition "own".
			cd.Origin = originOwn
			cd.IsOwn = true
			// Stamp the source language so the WASM menu can filter by it.
			// item.Language is the project's programming_language_id token
			// ("golang"/"c"); normalize to the stage space ("go"/"c").
			cd.ProgrammingLanguage = normalizeLangToken(item.Language)
			defs = append(defs, cd)
		}
	}

	// ── Source 2: blackboxes (GitHub-sourced devices) ─────────────────────────
	// The parsed_json stored by the worker is already a serialised BlackBoxDef.
	// No re-parse needed — unmarshal and convert to the client DTO directly.
	//
	// Category and subcategory names are resolved here (ID → human name) so the
	// WASM client receives strings it can use directly as menu labels without
	// any extra round-trips.
	devices, err := store.ListReadyDevices(callerID)
	if err != nil {
		c.Logger().Errorf("[blackboxapi/list] devices DB error: %v", err)
	} else {
		for _, d := range devices {
			if d.ParsedJSON == "" || d.ParsedJSON == "{}" {
				continue
			}
			var def bbparser.BlackBoxDef
			if err := json.Unmarshal([]byte(d.ParsedJSON), &def); err != nil {
				log.Printf("[blackboxapi] skipping device %q: unmarshal error: %v",
					d.DisplayName, err)
				continue
			}
			cd := toClientDef(&def)

			// Populate menu placement fields from the blackboxes row.
			// DisplayNameHuman is the name the specialist chose in the portal.
			if d.DisplayNameHuman != "" {
				cd.MenuName = d.DisplayNameHuman
			}
			if d.CategoryID != "" {
				cat, catErr := store.GetCategoryByID(d.CategoryID)
				if catErr == nil {
					cd.MenuCategory = cat.Name
				}
			}
			if d.SubcategoryID != "" {
				sub, subErr := store.GetSubcategoryByID(d.SubcategoryID)
				if subErr == nil {
					cd.MenuSubcategory = sub.Name
				}
			}

			// Stamp ownership — see Source 1 comment. ListReadyDevices(callerID)
			// is caller-scoped by the same contract as project code versions.
			cd.Origin = originOwn
			cd.IsOwn = true
			// Stamp the source language from the blackboxes row. The worker
			// sets programming_language_id when it ingests a release (Go-only
			// today → "golang"); normalize to the stage space ("go"/"c").
			cd.ProgrammingLanguage = normalizeLangToken(d.ProgrammingLanguageID)

			defs = append(defs, cd)
		}
	}

	// Always return an array, never null — the WASM range loop depends on this.
	return c.JSON(http.StatusOK, defs)
}

// ─── Conversion helpers ────────────────────────────────────────────────────────

// toClientDef converts a full server-side BlackBoxDef to the lean client DTO.
// Heavy fields (StructCode, MethodsCode, Imports) are dropped.
// All Doc strings are passed through sanitizeDoc() before being sent to the
// client — this prevents HTML/JS injection via godoc comments.
//
// Ownership fields (Origin, IsOwn) are NOT set by this function — they are
// filled in by the handler after the conversion, once the caller context is
// known. Keeping them out of toClientDef makes this converter reusable from
// any future endpoint that needs a different Origin value.
func toClientDef(def *bbparser.BlackBoxDef) clientBlackBoxDef {
	cd := clientBlackBoxDef{
		Name:        def.Name,
		Doc:         sanitizeDoc(def.Doc),
		StructIcon:  def.StructIcon,
		StructLabel: def.StructLabel,
		Interactive: def.Interactive,
	}

	if def.Init != nil {
		init := toClientFuncDef(*def.Init)
		cd.Init = &init
	}

	if len(def.Methods) > 0 {
		cd.Methods = make([]clientMethodDef, len(def.Methods))
		for i, m := range def.Methods {
			cd.Methods[i] = clientMethodDef{
				Name:           m.Name,
				Doc:            sanitizeDoc(m.FuncDef.Doc),
				ExecutionOrder: m.FuncDef.ExecutionOrder,
				Icon:           m.FuncDef.Icon,
				Label:          m.FuncDef.Label,
				MenuCol:        m.FuncDef.MenuCol,
				MenuRow:        m.FuncDef.MenuRow,
				MenuPosSet:     m.FuncDef.MenuPosSet,
			}
			if len(m.FuncDef.Inputs) > 0 {
				cd.Methods[i].Inputs = make([]clientPortDef, len(m.FuncDef.Inputs))
				for j, p := range m.FuncDef.Inputs {
					cd.Methods[i].Inputs[j] = clientPortDef{
						Name:    p.Name,
						GoType:  p.GoType,
						IsError: p.IsError,
						Doc:     sanitizeDoc(p.Doc),
					}
				}
			}
			if len(m.FuncDef.Outputs) > 0 {
				cd.Methods[i].Outputs = make([]clientPortDef, len(m.FuncDef.Outputs))
				for j, p := range m.FuncDef.Outputs {
					cd.Methods[i].Outputs[j] = clientPortDef{
						Name:    p.Name,
						GoType:  p.GoType,
						IsError: p.IsError,
						Doc:     sanitizeDoc(p.Doc),
					}
				}
			}
		}
	}

	// C99 device-functions (decision b). toClientDef stays a pure converter:
	// the only "logic" here is that Outputs are taken from
	// def.FunctionSynthesizedOutputs, which appends the handle pass-through
	// computed from def.WireTypes (the LabVIEW refnum idiom). ParseC itself
	// stays faithful — it never invents the pass-through. The Go method/init
	// path above is untouched. See docs/c99_ide_integration.md §2.1/§5.2.
	if len(def.Functions) > 0 {
		cd.Functions = make([]clientFunctionDef, len(def.Functions))
		for i, fn := range def.Functions {
			cd.Functions[i] = clientFunctionDef{
				Name:           fn.Name,
				Doc:            sanitizeDoc(fn.FuncDef.Doc),
				ExecutionOrder: fn.FuncDef.ExecutionOrder,
				Icon:           fn.FuncDef.Icon,
				Label:          fn.FuncDef.Label,
				MenuCol:        fn.FuncDef.MenuCol,
				MenuRow:        fn.FuncDef.MenuRow,
				MenuPosSet:     fn.FuncDef.MenuPosSet,
				HandlerType:    fn.FuncDef.HandlerType,
				CallbackMode:   fn.FuncDef.CallbackMode,
			}
			if len(fn.FuncDef.Inputs) > 0 {
				cd.Functions[i].Inputs = make([]clientPortDef, len(fn.FuncDef.Inputs))
				for j, p := range fn.FuncDef.Inputs {
					cd.Functions[i].Inputs[j] = clientPortDef{
						Name:         p.Name,
						GoType:       p.GoType,
						IsError:      p.IsError,
						Doc:          sanitizeDoc(p.Doc),
						CallbackType: p.CallbackType,
					}
				}
			}
			outs := def.FunctionSynthesizedOutputs(fn.FuncDef)
			if len(outs) > 0 {
				cd.Functions[i].Outputs = make([]clientPortDef, len(outs))
				for j, p := range outs {
					cd.Functions[i].Outputs[j] = clientPortDef{
						Name:         p.Name,
						GoType:       p.GoType,
						IsError:      p.IsError,
						Doc:          sanitizeDoc(p.Doc),
						PassThrough:  p.PassThrough,
						CallbackType: p.CallbackType,
					}
				}
			}
		}
	}

	if len(def.Props) > 0 {
		cd.Props = make([]clientPropDef, len(def.Props))
		for i, p := range def.Props {
			cd.Props[i] = clientPropDef{
				FieldName:   p.FieldName,
				GoType:      p.GoType,
				Label:       p.Label,
				Default:     p.Default,
				Options:     p.Options,
				Connection:  p.Connection,
				Container:   p.Container,
				KeyType:     p.KeyType,
				ValueType:   p.ValueType,
				NativeKey:   p.NativeKey,
				NativeValue: p.NativeValue,
			}
		}
	}

	if len(def.ManualPages) > 0 {
		cd.ManualPages = make([]clientManualPage, len(def.ManualPages))
		for i, p := range def.ManualPages {
			// Manual page Content is Markdown — NOT stripped here.
			// It is rendered by marked.js which has its own sanitization.
			cd.ManualPages[i] = clientManualPage{
				Name:     p.Name,
				Language: p.Language,
				ShowIn:   string(p.ShowIn),
				Content:  p.Content,
			}
		}
	}

	// Help — markdown-based tabs from the GitHub repo.
	// Readme and method tabs are passed through with the same per-tab
	// projection: the server's HelpTab (Order/Title/Content) → the
	// client's clientHelpTab (same fields). Content is Markdown rendered
	// by marked.js which handles its own sanitization.
	if len(def.Help.Readme) > 0 || len(def.Help.Methods) > 0 {
		cd.Help = clientDeviceHelp{
			Readme:  make(map[string][]clientHelpTab, len(def.Help.Readme)),
			Methods: make(map[string]clientMethodHelp, len(def.Help.Methods)),
		}
		// Project readme tabs.
		for lang, tabs := range def.Help.Readme {
			clientTabs := make([]clientHelpTab, len(tabs))
			for i, t := range tabs {
				clientTabs[i] = clientHelpTab{
					Order:   t.Order,
					Title:   t.Title,
					Content: t.Content,
				}
			}
			cd.Help.Readme[lang] = clientTabs
		}
		// Project method tabs.
		for method, mh := range def.Help.Methods {
			cmh := clientMethodHelp{
				Langs: make(map[string][]clientHelpTab, len(mh.Langs)),
			}
			for lang, tabs := range mh.Langs {
				clientTabs := make([]clientHelpTab, len(tabs))
				for i, t := range tabs {
					clientTabs[i] = clientHelpTab{
						Order:   t.Order,
						Title:   t.Title,
						Content: t.Content,
					}
				}
				cmh.Langs[lang] = clientTabs
			}
			cd.Help.Methods[method] = cmh
		}
	}

	return cd
}

// toClientFuncDef converts a server FuncDef to its client counterpart.
// Doc strings are sanitized to strip HTML tags.
func toClientFuncDef(fd bbparser.FuncDef) clientFuncDef {
	cf := clientFuncDef{
		Doc:            sanitizeDoc(fd.Doc),
		ExecutionOrder: fd.ExecutionOrder,
		Icon:           fd.Icon,
		Label:          fd.Label,
		MenuCol:        fd.MenuCol,
		MenuRow:        fd.MenuRow,
		MenuPosSet:     fd.MenuPosSet,
	}
	if len(fd.Inputs) > 0 {
		cf.Inputs = make([]clientPortDef, len(fd.Inputs))
		for i, p := range fd.Inputs {
			cf.Inputs[i] = clientPortDef{
				Name:    p.Name,
				GoType:  p.GoType,
				IsError: p.IsError,
				Doc:     sanitizeDoc(p.Doc),
			}
		}
	}
	if len(fd.Outputs) > 0 {
		cf.Outputs = make([]clientPortDef, len(fd.Outputs))
		for i, p := range fd.Outputs {
			cf.Outputs[i] = clientPortDef{
				Name:    p.Name,
				GoType:  p.GoType,
				IsError: p.IsError,
				Doc:     sanitizeDoc(p.Doc),
			}
		}
	}
	return cf
}
