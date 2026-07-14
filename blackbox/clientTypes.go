// blackbox/clientTypes.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

import "strings"

// clientTypes.go — Black-box type definitions for the WASM IDE client.
//
// English:
//
//	Lightweight mirrors of the server-side BlackBoxDef types. Does NOT
//	include StructCode, MethodsCode, or Imports (not needed in the IDE —
//	only the server uses them for code generation).
//
//	These types are populated by fetching GET /api/v1/blackbox on startup
//	and used to configure generic black-box devices dynamically.
//
//	Visual metadata fields (icon / label):
//
//	  StructIcon, StructLabel — declared on the exported struct.
//	  FuncDefClient.Icon, .Label — declared on Init().
//	  MethodDefClient.Icon, .Label — declared on each named method.
//
//	Visual block header rendering:
//	  - Icon used: method.Icon if non-empty, else def.StructIcon.
//	  - Title used: "<StructLabel> <MethodLabel>" or "<StructName> <MethodName>"
//	    when the label fields are absent.
//
//	Menu rendering (hex Hardware submenu):
//	  - Component entry icon: def.StructIcon (fallback: "gear").
//	  - Method entry icon: method.Icon (fallback: def.StructIcon, then "gear").
//
//	Ownership (Origin, IsOwn):
//
//	  Every BlackBoxDefClient that reaches the IDE carries an Origin string
//	  that identifies how it got here. The value drives the "My Items" menu
//	  filter in ui/mainMenu/menuBuilder.go. See the constants below and the
//	  Phase 1 design doc at /ide/docs/tasks/REFACTOR_MY_ITEMS_PHASE_1.md.
//
// Português:
//
//	Init tem semântica especial (sempre roda primeiro, cria bloco visual
//	próprio). Todos os outros métodos ficam em Methods []MethodDefClient,
//	cada um criando um bloco "BlackBox{Nome}:{Struct}".
//
//	StructIcon/StructLabel: nível do struct.
//	Icon/Label nos métodos: nível do método.
//
//	Ownership (Origin, IsOwn): cada definição carrega um marcador que
//	indica como ela chegou até a IDE — "own" (o caller é o autor),
//	"curated" (o admin promoveu para uma section) ou "public" (publicada
//	por outro especialista, visível no catálogo mas não em "My Items").

// ─── Ownership origin constants ──────────────────────────────────────────────
//
// These string values are the on-wire contract between the server and the
// WASM client. They mirror the constants declared in:
//
//	/ide/server/handler/blackboxapi/handler.go (originOwn only — the
//	endpoint is caller-filtered, so no other origin is ever produced there)
//
//	/ide/server/handler/templateapi/handlers.go (originOwn + originPublic —
//	templates can be owned or public; "curated" does not apply because
//	templates are never embedded in curated menu sections today)
//
// "curated" is a client-stamped value, assigned in
// stageWorkspace/workspace.go::extractEmbeddedDefs when a device arrives
// through the menu tree endpoint instead of /api/v1/blackbox. The server
// stores no notion of "curated" because the same parsed_json blob can be
// "own" in one request context and "curated" in another.
//
// If a typo or rename drifts one side of the contract, "My Items" silently
// misbehaves on the client. The server-side TestOwnershipConstantsAreStable
// tests pin the server values; this file is the lock on the WASM side.
const (
	// OriginOwn marks a definition authored by the authenticated caller.
	// Items with this origin appear in both the main menu AND "My Items".
	OriginOwn = "own"

	// OriginCurated marks a definition that reached the client embedded in
	// an admin-curated section of the menu tree. Items with this origin
	// appear in the main menu but NEVER in "My Items". This value is
	// assigned on the client side — the server never produces it.
	OriginCurated = "curated"

	// OriginPublic marks a definition authored by another specialist and
	// shared publicly (applies to templates today; may apply to devices in
	// the future). Items with this origin appear in the main menu but
	// NEVER in "My Items".
	OriginPublic = "public"
)

// ManualPageClient is one documentation page extracted from a /* */ block.
type ManualPageClient struct {
	// Name identifies the page (e.g. "wiring-guide", "init", "datasheet").
	Name string `json:"name"`

	// Language is the BCP-47 language code (e.g. "en", "pt-br").
	Language string `json:"language"`

	// ShowIn controls which device block renders this page.
	// "init" → Init block only.
	// "both" → all blocks of this component.
	// Any other value → the block whose method name matches (case-insensitive).
	ShowIn string `json:"showIn"`

	// Content is the raw Markdown text ready for rendering.
	Content string `json:"content"`
}

// BlackBoxDefClient describes a black-box device as seen by the IDE.
// Fetched from GET /api/v1/blackbox or extracted from the menu tree
// response; ownership fields (Origin, IsOwn) tell the IDE which source
// this particular instance came from.
type BlackBoxDefClient struct {
	// Name is the struct name (e.g. "Test", "APDS9960"). Used as the device label.
	Name string `json:"name"`

	// Doc is the package-level documentation comment.
	Doc string `json:"doc,omitempty"`

	// StructIcon is the FontAwesome icon name (kebab-case) for the component.
	// Used in the Hardware submenu entry and as fallback for method-level icons.
	// Falls back to "gear" when empty.
	StructIcon string `json:"structIcon,omitempty"`

	// StructLabel is the human-readable display name for the component.
	// Used as the first part of each visual block header title.
	// Falls back to Name when empty.
	StructLabel string `json:"structLabel,omitempty"`

	// Interactive is the public URL of an SVG diagram declared with
	// "interactive:name." in the struct doc comment (e.g. "rp2040").
	// When non-empty, the Inspect panel's Help tab activates this SVG
	// inline within markdown content: elements whose data-id matches a
	// prop's current value are highlighted using the colour from the
	// SVG's data-palette attribute. Not limited to hardware — any SVG
	// following the IoTMaker Interactive Diagram Specification works.
	// See docs/INTERACTIVE_DIAGRAM_SPEC.md.
	Interactive string `json:"interactive,omitempty"`

	// MenuName is the display name chosen by the specialist in the portal
	// ("New Project" modal). Used as the menu entry label.
	// Falls back to StructLabel, then Name when empty.
	MenuName string `json:"menuName,omitempty"`

	// MenuCategory is the name of the top-level IDE menu category chosen
	// by the specialist (e.g. "Sensors"). Empty means "Other".
	MenuCategory string `json:"menuCategory,omitempty"`

	// MenuSubcategory is the name of the submenu within MenuCategory.
	// Empty means the device appears directly under MenuCategory.
	MenuSubcategory string `json:"menuSubcategory,omitempty"`

	// Init describes the Init() method. Nil when the device has no Init().
	Init *FuncDefClient `json:"init,omitempty"`

	// Methods describes all non-Init methods in source-file order.
	// Each entry creates one visual block in the IDE Hardware menu.
	Methods []MethodDefClient `json:"methods,omitempty"`

	// Functions describes C99 device-functions (decision b: every public
	// function is a standalone block, no method/instance). Disjoint from the
	// Go path: a Go device leaves this empty, a C99 device leaves Init/Methods
	// empty. Each entry's Outputs already include the synthesized handle
	// pass-through (added by the server). FunctionDefClient is a type alias of
	// MethodDefClient, so the menu/factory reuse the method helpers and the
	// StatementBlackBoxMethod block. See docs/c99_ide_integration.md.
	Functions []FunctionDefClient `json:"functions,omitempty"`

	// Props are configurable struct fields, shown in the Inspect panel.
	Props []PropDefClient `json:"props,omitempty"`

	// ManualPages are kept for backward compatibility but are no longer populated
	// by the new GitHub-based submission flow. Use Help instead.
	ManualPages []ManualPageClient `json:"manualPages,omitempty"`

	// Help contains structured help content extracted from markdown files
	// in the root of the GitHub repository (readme.md, init.en.md, etc.).
	// Populated by the worker. Empty for legacy components.
	Help DeviceHelpClient `json:"help,omitempty"`

	// Origin identifies how this definition reached the client:
	//   - OriginOwn      → fetched from /api/v1/blackbox (caller is the owner).
	//   - OriginCurated  → extracted from an admin-promoted section of the menu
	//                      tree. Set by stageWorkspace/workspace.go, not the server.
	//   - OriginPublic   → not currently used for devices (reserved for future use).
	//
	// Omitempty is intentional: zero value means "unknown provenance", which
	// is the safe default — unknown items never appear in "My Items".
	Origin string `json:"origin,omitempty"`

	// IsOwn is the boolean shortcut for Origin == OriginOwn. Having both a
	// discriminator (Origin) and a flag (IsOwn) costs two bytes per item but
	// removes a class of "did I compare against the right string constant?"
	// bugs at every call site on the client side.
	//
	// For a definition with an unknown Origin (empty string), IsOwn is false
	// — the safe default.
	IsOwn bool `json:"isOwn,omitempty"`

	// ProgrammingLanguage is the source language this black-box implements,
	// as a stage token: "go", "c" (and, later, "arduino", "python"). The
	// server stamps it (handler/blackboxapi) from the project's
	// programming_language_id, normalized to the stage token space. The menu
	// uses it to hide a device whose language differs from the current
	// project's — a black-box implements exactly one language. Empty means
	// the server did not stamp it; SupportsProjectLanguage treats empty as
	// visible (the safe default — a missing stamp must not vanish a device).
	ProgrammingLanguage string `json:"programmingLanguage,omitempty"`
}

// MethodDefClient describes one non-Init method (Run, Log, Step, …).
type MethodDefClient struct {
	// Name is the Go method name (e.g. "Run", "Log", "Step").
	Name string `json:"name"`

	// Doc is the method documentation comment.
	Doc string `json:"doc,omitempty"`

	// ExecutionOrder is the ordering hint. 0 = unordered.
	ExecutionOrder int `json:"executionOrder,omitempty"`

	// Icon is the FontAwesome icon name (kebab-case) for this method.
	// Used in the hex menu and at the top of the visual block header.
	// Falls back to the component's StructIcon when empty.
	Icon string `json:"icon,omitempty"`

	// Label is the human-readable display name for this method.
	// Combined with StructLabel to form the visual block title.
	// Falls back to the Go method Name when empty.
	Label string `json:"label,omitempty"`

	// MinTarget mirrors the server FuncDef.MinTarget (avr|mcu32|posix,
	// ""=anywhere) — menu gating input. Rides the FunctionDefClient
	// alias; Go methods never set it. Português: Espelha o MinTarget —
	// entrada do gating do menu. Viaja pelo alias FunctionDefClient.
	MinTarget string `json:"minTarget,omitempty"`

	// NoDevice mirrors the server flag: public helper, never a menu item.
	// Português: Espelha o flag do server: helper público, nunca item de menu.
	NoDevice bool `json:"noDevice,omitempty"`

	// MenuCol is the column offset from the Back button center in the hex menu.
	// Populated from the "menu:col,row." directive in the method doc comment.
	// Only meaningful when MenuPosSet is true.
	MenuCol int `json:"menuCol,omitempty"`

	// MenuRow is the row offset from the Back button center in the hex menu.
	// Populated from the "menu:col,row." directive in the method doc comment.
	// Only meaningful when MenuPosSet is true.
	MenuRow int `json:"menuRow,omitempty"`

	// MenuPosSet is true when the specialist explicitly declared "menu:col,row."
	// in this method's doc comment. When false, the IDE auto-places this item
	// using the radial layout engine (rulesMainMenu.ApplyRadialLayout).
	MenuPosSet bool `json:"menuPosSet,omitempty"`

	// Inputs become left-side connector ports on the visual block.
	Inputs []PortDefClient `json:"inputs,omitempty"`

	// Outputs become right-side connector ports on the visual block.
	Outputs []PortDefClient `json:"outputs,omitempty"`

	// HandlerType and CallbackMode are populated ONLY for C99 device-functions
	// (FunctionDefClient is a type alias of this struct, so the fields ride
	// along; a Go method never sets them). HandlerType marks the function as a
	// callback HANDLER — the function-pointer typedef it implements (e.g.
	// "display_write_fn"). CallbackMode is "both" (the IDE offers BOTH a
	// callable device AND a separate callback reference device, scene type
	// "CallbackRef:<fn>") or "ref" (only the reference device). Both empty for
	// Go methods and for ordinary C99 functions. See the duality section of
	// docs/CODEGEN_C99_CALLBACKS.md.
	HandlerType  string `json:"handlerType,omitempty"`
	CallbackMode string `json:"callbackMode,omitempty"`
}

// FunctionDefClient describes one C99 device-function (decision b). It is a
// type ALIAS of MethodDefClient — the server sends the same shape (name, doc,
// executionOrder, icon, label, menu position, inputs, outputs), and the alias
// lets the menu and factory reuse the method helpers (EffectiveLabel /
// EffectiveIcon) and the StatementBlackBoxMethod visual block unchanged. The
// only behavioural difference lives in the factory: a function block gets its
// own independent instanceId (no shared receiver). See
// docs/c99_ide_integration.md §5.3/§5.4.
type FunctionDefClient = MethodDefClient

// FuncDefClient describes the Init() method signature.
type FuncDefClient struct {
	// Doc is the method documentation comment.
	Doc string `json:"doc,omitempty"`

	// ExecutionOrder is the user-defined ordering hint.
	ExecutionOrder int `json:"executionOrder,omitempty"`

	// Icon is the FontAwesome icon name for the Init block.
	// Falls back to the component's StructIcon when empty.
	Icon string `json:"icon,omitempty"`

	// Label is the human-readable display name for the Init block.
	// Falls back to "Init" when empty.
	Label string `json:"label,omitempty"`

	// MenuCol is the column offset from the Back button center in the hex menu.
	// Populated from the "menu:col,row." directive in the Init doc comment.
	// Only meaningful when MenuPosSet is true.
	MenuCol int `json:"menuCol,omitempty"`

	// MenuRow is the row offset from the Back button center in the hex menu.
	// Populated from the "menu:col,row." directive in the Init doc comment.
	// Only meaningful when MenuPosSet is true.
	MenuRow int `json:"menuRow,omitempty"`

	// MenuPosSet is true when the specialist explicitly declared "menu:col,row."
	// in the Init doc comment. When false, the IDE auto-places this item using
	// the radial layout engine (rulesMainMenu.ApplyRadialLayout).
	MenuPosSet bool `json:"menuPosSet,omitempty"`

	// Inputs become left-side connector ports on the visual block.
	Inputs []PortDefClient `json:"inputs,omitempty"`

	// Outputs become right-side connector ports on the visual block.
	Outputs []PortDefClient `json:"outputs,omitempty"`
}

// PortDefClient describes a connector port.
type PortDefClient struct {
	// Name is the port name (e.g. "a", "i2c", "err").
	Name string `json:"name"`

	// GoType is the Go type string (e.g. "int", "*machine.I2C", "error").
	GoType string `json:"goType"`
	// WireType mirrors PortDef.WireType (server): the connector token this
	// port exposes when it differs from the authored GoType — today the
	// scalar-pointer family tokens. Empty = use GoType.
	// Português: Espelha PortDef.WireType (server): o token de conector
	// que a porta expõe quando difere do GoType autoral — hoje os tokens
	// de família ponteiro-escalar. Vazio = usar GoType.
	WireType string `json:"wireType,omitempty"`

	// IsError marks error return ports. Error ports are always optional.
	IsError bool `json:"isError,omitempty"`

	// Doc is the port description from the IDS comment section.
	Doc string `json:"doc,omitempty"`

	// PassThrough marks a synthesized C99 handle pass-through output: the
	// republished wire-type input that lets the maker chain resource blocks
	// in series (the LabVIEW refnum idiom). Set by the server's Functions
	// synthesis (toClientDef → FunctionSynthesizedOutputs); empty on every
	// Go port and on real C99 returns/results. Fatia 2 renders it as an
	// ordinary output port; codegen (Fatia 4) must treat it as the same
	// handle variable, not a return. See docs/c99_ide_integration.md.
	PassThrough bool `json:"passThrough,omitempty"`

	// CallbackType is the function-pointer typedef a callback PARAMETER expects
	// (e.g. "display_write_fn"), set by the server on a C99 consumer's callback
	// input port. The IDE uses it to enforce the strict ƒ-wire type rule (only a
	// callback reference of the SAME type may be wired in) and to render the ƒ
	// glyph. Empty on Go ports and on ordinary C99 ports. See the duality
	// section of docs/CODEGEN_C99_CALLBACKS.md.
	CallbackType string `json:"callbackType,omitempty"`
	// EditorLang / EditorDictJSON mirror the server's Phase B editor
	// config: the Monaco language for a wired Data · Text and the
	// RESOLVED completion dictionary (JSON [{"label","insert","doc"}]).
	// Português: Espelham a config de editor da Fase B: linguagem do
	// Monaco e dicionário de autocompletar já RESOLVIDO.
	EditorLang     string `json:"editorLang,omitempty"`
	EditorDictJSON string `json:"editorDictJson,omitempty"`
}

// PropDefClient describes an editable property field.
type PropDefClient struct {
	// FieldName is the Go struct field name (e.g. "addr", "freq").
	FieldName string `json:"fieldName"`

	// GoType is the Go type for validation (e.g. "uint8", "string").
	GoType string `json:"goType"`

	// Label is the human-readable name shown in the Inspect panel.
	Label string `json:"label"`

	// Default is the initial value pre-filled in the Inspect panel.
	Default string `json:"default,omitempty"`

	// Options, when non-empty, renders a dropdown instead of a text input.
	Options []string `json:"options,omitempty"`

	// Connection is the diagram role identifier from the `connection` struct
	// tag (e.g. "I2C_SDA"). When non-empty, this prop is linked to an
	// interactive SVG diagram: the prop's current value is matched against
	// data-id attributes in the SVG, and the role is looked up in the SVG's
	// data-palette to determine the highlight colour and form field accent.
	//
	// The colour comes from the SVG at runtime — no colour tag exists in
	// the Go struct. See docs/INTERACTIVE_DIAGRAM_SPEC.md.
	Connection string `json:"connection,omitempty"`

	// ─── Container shape — Slice 2.x (map/slice props) ───────────────────
	//
	// Mirrors the same-named fields on the server-side PropDef. Carried on
	// the wire so the WASM renderer does not need to re-parse the GoType
	// string — the parser already did, and the answer is more reliable
	// here than in client code that has to reproduce Go's type grammar.
	//
	// Slice 2.1 ships these fields populated by the parser; the renderer
	// ignores them until Slice 2.2 (map) and Slice 2.4 (slice) land.

	// Container is the runtime shape: "" for scalar, "map", "slice".
	Container string `json:"container,omitempty"`

	// KeyType is the Go type name of the map key. Empty for non-maps.
	KeyType string `json:"keyType,omitempty"`

	// ValueType is the Go type name of the map value or slice element.
	// Empty for scalars.
	ValueType string `json:"valueType,omitempty"`

	// NativeKey is true when KeyType is something the renderer can build
	// an input for. The Slice 2.2 renderer additionally requires
	// KeyType=="string"; later slices may relax this.
	NativeKey bool `json:"nativeKey,omitempty"`

	// NativeValue is true when ValueType is a native renderable type.
	// A non-native value type makes the row inert in the renderer.
	NativeValue bool `json:"nativeValue,omitempty"`
}

// ─── Help system — markdown-based tabs ───────────────────────────────────────
//
// These types mirror DeviceHelp / MethodHelp / HelpTab on the server side.
// They are populated from the "help" field inside parsed_json when the
// worker processes a GitHub release that contains markdown files.
//
// The IDE uses HelpTabsFor() instead of the legacy PagesFor() method to
// build the Help deck in the overlay panel.

// HelpTabClient is one documentation tab for a device method.
type HelpTabClient struct {
	// Order is the original sort key from the filename (0 = no number).
	Order int `json:"order"`
	// Title comes from the first "# Heading" in the markdown file,
	// truncated to HelpTabTitleMaxLen runes at a word boundary. Empty
	// when no heading is present — render as a localised "title not
	// found" message via translate.T("help.title.notFound", ...).
	Title string `json:"title"`
	// Content is the full markdown text with image paths rewritten to
	// public URLs by the worker.
	Content string `json:"content"`
}

// MethodHelpClient groups the help tabs for one method, keyed by BCP-47
// language code (lowercase, e.g. "en", "pt-br").
type MethodHelpClient struct {
	// Langs maps a language code to the ordered list of help tabs.
	Langs map[string][]HelpTabClient `json:"langs,omitempty"`
}

// DeviceHelpClient is the complete help payload for a device.
// Embedded in BlackBoxDefClient as the Help field.
type DeviceHelpClient struct {
	// Readme maps a language code to the ordered list of readme tabs.
	// Same shape as MethodHelpClient.Langs — a device readme can be
	// composed of multiple `readme[.<N>].<lang>.md` files, sorted by
	// (Order, Title) at parse time. The renderer suppresses the tab
	// bar when len(tabs) == 1 so a single-readme device keeps its
	// original "one continuous document" UX.
	Readme map[string][]HelpTabClient `json:"readme,omitempty"`

	// Methods maps a lowercase method name (e.g. "init", "run") to its
	// MethodHelpClient.
	Methods map[string]MethodHelpClient `json:"methods,omitempty"`
}

// ─── Convenience methods ───────────────────────────────────────────────────────

// HasInit returns true if this device has an Init() method.
func (d *BlackBoxDefClient) HasInit() bool { return d.Init != nil }

// HasMethods returns true if this device has at least one non-Init method.
func (d *BlackBoxDefClient) HasMethods() bool { return len(d.Methods) > 0 }

// GetMethod returns the MethodDefClient for the given method name, or nil.
// The lookup is case-sensitive.
//
// Português: Retorna o MethodDefClient para o nome de método dado, ou nil.
func (d *BlackBoxDefClient) GetMethod(name string) *MethodDefClient {
	for i := range d.Methods {
		if d.Methods[i].Name == name {
			return &d.Methods[i]
		}
	}
	return nil
}

// HasFunctions returns true if this device has at least one C99
// device-function (decision b). True only for C99 devices.
func (d *BlackBoxDefClient) HasFunctions() bool { return len(d.Functions) > 0 }

// GetFunction returns the FunctionDefClient for the given function name, or
// nil. The lookup is case-sensitive. Because FunctionDefClient aliases
// MethodDefClient, the result is directly usable wherever a *MethodDefClient
// is expected (e.g. the factory's createBlackBoxMethod).
//
// Português: Retorna o FunctionDefClient para o nome dado, ou nil.
func (d *BlackBoxDefClient) GetFunction(name string) *FunctionDefClient {
	for i := range d.Functions {
		if d.Functions[i].Name == name {
			return &d.Functions[i]
		}
	}
	return nil
}

// SupportsProjectLanguage reports whether this black-box may appear in a
// project of the given language token ("go", "c", …). A black-box implements
// exactly one source language, so the test is plain equality against
// ProgrammingLanguage. An unstamped device (empty ProgrammingLanguage) is
// treated as visible: hiding a device over a missing stamp is the worse
// failure mode (it disappears with no obvious cause), and the server stamps
// the field for every device it serves.
//
// This is the menu's language filter for black-boxes. Primitive devices keep
// their own (static) language metadata in factoryDevice/catalog; black-box
// language lives here on the def because it is dynamic, per-device data the
// catalog cannot know at compile time.
//
// Português: Diz se este black-box pode aparecer num projeto da linguagem
// dada. Black-box tem uma única linguagem → igualdade. Vazio = visível
// (esconder por carimbo faltando é o pior modo de falha).
func (d *BlackBoxDefClient) SupportsProjectLanguage(projectLanguage string) bool {
	if d.ProgrammingLanguage == "" {
		return true
	}
	return d.ProgrammingLanguage == projectLanguage
}

// EffectiveStructLabel returns StructLabel when set, otherwise the struct Name.
// Use this whenever the UI needs a human-readable component name.
func (d *BlackBoxDefClient) EffectiveStructLabel() string {
	if d.StructLabel != "" {
		return d.StructLabel
	}
	return d.Name
}

// EffectiveStructIcon returns StructIcon when set, otherwise "gear".
// Use this whenever an icon is required for the component as a whole.
func (d *BlackBoxDefClient) EffectiveStructIcon() string {
	if d.StructIcon != "" {
		return d.StructIcon
	}
	return "gear"
}

// EffectiveIcon returns the method icon when set, falling back to the
// component StructIcon, and finally to "gear".
func (m *MethodDefClient) EffectiveIcon(def *BlackBoxDefClient) string {
	if m.Icon != "" {
		return m.Icon
	}
	return def.EffectiveStructIcon()
}

// EffectiveLabel returns the method label when set, falling back to the
// Go method Name.
func (m *MethodDefClient) EffectiveLabel() string {
	if m.Label != "" {
		return m.Label
	}
	return m.Name
}

// EffectiveIcon returns the Init icon when set, falling back to the component
// StructIcon, and finally to "gear".
func (f *FuncDefClient) EffectiveIcon(def *BlackBoxDefClient) string {
	if f.Icon != "" {
		return f.Icon
	}
	return def.EffectiveStructIcon()
}

// EffectiveLabel returns the Init label when set, falling back to "Init".
func (f *FuncDefClient) EffectiveLabel() string {
	if f.Label != "" {
		return f.Label
	}
	return "Init"
}

// HelpTabsFor returns the help tabs for a given method and language code.
//
// Resolution order:
//  1. Exact language match (e.g. "pt-br")
//  2. Primary-tag match (e.g. "pt" matches "pt-br" tabs)
//  3. English fallback ("en")
//  4. Any available language (deterministic: sorted keys, first wins)
//
// Returns nil when no markdown help is available for the method — the
// caller should then fall back to the GoDoc card only.
//
// blockName is "init" or the method name in any case (matched
// case-insensitively against the keys in Help.Methods).
func (d *BlackBoxDefClient) HelpTabsFor(blockName, lang string) []HelpTabClient {
	if d.Help.Methods == nil {
		return nil
	}
	key := toLower(blockName)
	mh, ok := d.Help.Methods[key]
	if !ok || mh.Langs == nil {
		return nil
	}
	return resolveHelpLang(mh.Langs, lang)
}

// HelpReadmeTabs returns the ordered readme tab slice for the given
// language using the same resolution order as HelpTabsFor. Returns nil
// when no readme is available.
//
// Replaces the older HelpReadme (which returned a single string) — the
// readme grew the ability to contain multiple tabs in slice 7 of the
// wizard work. Callers that just want "one blob of markdown" can
// concatenate the Content fields, but the IDE renders them as tabs
// when len > 1 and as a single document when len == 1.
func (d *BlackBoxDefClient) HelpReadmeTabs(lang string) []HelpTabClient {
	if d.Help.Readme == nil {
		return nil
	}
	return resolveHelpLang(d.Help.Readme, lang)
}

// resolveHelpLang picks the best available language from a
// map[langCode][]HelpTabClient using the priority order documented on HelpTabsFor.
func resolveHelpLang(langs map[string][]HelpTabClient, want string) []HelpTabClient {
	want = toLower(want)
	// 1. Exact match.
	if tabs, ok := langs[want]; ok {
		return tabs
	}
	// 2. Primary subtag match: "pt" should match "pt-br".
	primary := want
	if idx := indexOf(want, '-'); idx >= 0 {
		primary = want[:idx]
	}
	for k, tabs := range langs {
		kPrimary := k
		if idx := indexOf(k, '-'); idx >= 0 {
			kPrimary = k[:idx]
		}
		if kPrimary == primary {
			return tabs
		}
	}
	// 3. English fallback.
	if tabs, ok := langs["en"]; ok {
		return tabs
	}
	// 4. Any available (sorted for determinism).
	keys := sortedKeys(langs)
	if len(keys) > 0 {
		return langs[keys[0]]
	}
	return nil
}

// indexOf returns the index of byte b in s, or -1.
func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// sortedKeys returns sorted keys of a map[string][]HelpTabClient.
func sortedKeys(m map[string][]HelpTabClient) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort — map is tiny (< 10 languages).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// AvailableHelpLangs returns all language codes for which this method has
// help tabs. Returns nil when no markdown help is available.
// Used by the overlay to decide whether to show a language selector.
func (d *BlackBoxDefClient) AvailableHelpLangs(blockName string) []string {
	if d.Help.Methods == nil {
		return nil
	}
	mh, ok := d.Help.Methods[toLower(blockName)]
	if !ok || mh.Langs == nil {
		return nil
	}
	return sortedKeys(mh.Langs)
}

// GoDocTab generates a HelpTabClient from the Go doc comments (component + method).
// Returns nil when both docs are empty — callers should skip appending it.
//
// This mirrors the behaviour of buildGodocMarkdown() in devices/compBlackBox,
// keeping the logic in one place inside the blackbox package so workspace.go
// and any other caller can produce the GoDoc tab without importing compBlackBox.
//
// The tab title is always "source doc" and it must be appended last by the caller.
func (d *BlackBoxDefClient) GoDocTab(methodName string) *HelpTabClient {
	compDoc := strings.TrimSpace(d.Doc)

	// Resolve the block's own doc — case-insensitive. Init and Methods are
	// the Go shapes; Functions is the C99 device-per-function shape. A def is
	// one or the other, so the lookups are mutually exclusive. isFunction
	// records that the block came from the C99 Functions slice.
	var methodDoc string
	isFunction := false
	lower := toLower(methodName)
	if lower == "init" {
		if d.Init != nil {
			methodDoc = strings.TrimSpace(d.Init.Doc)
		}
	} else {
		for _, m := range d.Methods {
			if toLower(m.Name) == lower {
				methodDoc = strings.TrimSpace(m.Doc)
				break
			}
		}
		if methodDoc == "" {
			for _, fn := range d.Functions {
				if toLower(fn.Name) == lower {
					methodDoc = strings.TrimSpace(fn.Doc)
					isFunction = true
					break
				}
			}
		}
	}

	// For a C99 device-function the component doc is the whole-file header,
	// which already serves as the device-level readme; repeating it inside
	// every function's go-doc tab is noise (and is exactly the "source code
	// in the menu" the maker does not want). Suppress it for functions. Go
	// methods keep the component (struct) doc as before.
	if isFunction {
		compDoc = ""
	}

	if compDoc == "" && methodDoc == "" {
		return nil
	}

	// Heading: the device name. C99 device-functions have no primary struct
	// (empty Name), so fall back to the block name to avoid an empty "# ".
	heading := d.Name
	if heading == "" {
		heading = methodName
	}

	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(heading)
	sb.WriteString("\n\n")
	if compDoc != "" {
		sb.WriteString(compDoc)
		sb.WriteString("\n\n")
	}
	if methodDoc != "" {
		sb.WriteString("---\n\n## ")
		sb.WriteString(methodName)
		sb.WriteString("()\n\n```\n")
		sb.WriteString(methodDoc)
		sb.WriteString("\n```\n")
	}

	content := strings.TrimSpace(sb.String())
	return &HelpTabClient{
		Order: 9999, // sentinel: always sorted last by the caller
		// "source doc" (not "go doc"): the tab carries documentation pulled
		// from the source comments, and the source is Go OR C99 — a
		// language-neutral label fits both.
		Title:   "source doc",
		Content: content,
	}
}

// PagesFor returns the manual pages that should appear in the given device
// block. The blockName is either "init" or the method name (e.g. "run", "log").
// Pages with showIn="both" appear in every block.
// Matching is case-insensitive.
//
// Returns nil when no pages match, so callers can use len(defs) > 0 safely.
//
// Português: Retorna as páginas de manual para o bloco dado. blockName é
// "init" ou o nome do método. Correspondência é case-insensitive.
func (d *BlackBoxDefClient) PagesFor(blockName string) []ManualPageClient {
	lower := toLower(blockName)
	var result []ManualPageClient
	for _, p := range d.ManualPages {
		showIn := toLower(p.ShowIn)
		if showIn == "both" || showIn == lower {
			result = append(result, p)
		}
	}
	return result
}

// AllInitInputs returns all Init input ports (for RegisterConnectors).
// Returns nil when Init is absent.
func (d *BlackBoxDefClient) AllInitInputs() []PortDefClient {
	if d.Init == nil {
		return nil
	}
	return d.Init.Inputs
}

// AllInitOutputs returns all Init output ports (for RegisterConnectors).
// Returns nil when Init is absent.
func (d *BlackBoxDefClient) AllInitOutputs() []PortDefClient {
	if d.Init == nil {
		return nil
	}
	return d.Init.Outputs
}

// toLower is a simple ASCII lowercase helper used by PagesFor.
// Avoids importing "strings" solely for this purpose.
func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
