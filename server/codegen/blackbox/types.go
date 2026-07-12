// server/codegen/blackbox/types.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package blackbox

// types.go — Black-box device definitions extracted from user Go source files.
//
// English:
//
//	A BlackBoxDef describes a hardware device or reusable code block written
//	by a specialist. The IDE parses the .go file and creates visual blocks for
//	each method found on the exported struct.
//
//	Naming conventions for methods:
//
//	  Init() — the only method with special semantics. When present, Init is
//	  always placed BEFORE all other methods in the generated code. It creates
//	  its own visual block type ("BlackBoxInit:StructName") in the scene.
//	  Init is optional — a component may have only regular methods.
//
//	  All other methods (Run, Log, Step, Read, Write, …) are "named methods".
//	  They are collected in Methods []NamedFuncDef, ordered by their position
//	  in the source file. Each creates a visual block of type
//	  "BlackBox{MethodName}:{StructName}" (e.g. "BlackBoxRun:APDS9960").
//
//	  At least one method (Init OR any named method) must be present.
//
//	Machine directives in doc comments (key:value. format):
//
//	  executionOrder:N — relative execution order hint (see FuncDef.ExecutionOrder).
//
//	  icon:name — FontAwesome icon name (kebab-case, e.g. "greater-than-equal").
//	              Written as "icon:name." in the struct or method doc comment.
//	              The IDE uses this icon in the hex menu and the visual block header.
//	              Falls back to "gear" when absent.
//
//	  label:text — Human-readable display name for the struct or method.
//	               Regex [a-zA-Z0-9_\s-]+, terminated by ".".
//	               The IDE combines StructLabel + MethodLabel as the block title.
//	               Falls back to StructName + MethodName when absent.
//
//	  menu:col,row — Explicit hex-menu grid position for this method's item,
//	                 expressed as a signed offset from the Back button center.
//	                 Written as "menu:col,row." e.g. "menu:-1,-1." or "menu:0,2.".
//	                 When absent the IDE places the item automatically using
//	                 the radial layout engine (rulesMainMenu.ApplyRadialLayout).
//	                 (0,0) is reserved for Back and must not be used.
//	                 See hexMenu readme for the flat-top grid coordinate system.
//
//	  All directives follow the IDS tag syntax (key:value.) and may appear on
//	  the same line or on separate lines in the doc comment. They are stripped
//	  from the human-readable Doc field before storage.
//
//	Struct fields with `prop` tags become editable properties in the IDE's
//	Inspect panel. The `options` tag provides a comma-separated list for
//	dropdown selection. The `connection` tag links a prop to an interactive
//	SVG diagram element (see docs/INTERACTIVE_DIAGRAM_SPEC.md).
//
// Português:
//
//	Init() é o único método com semântica especial — sempre roda antes dos
//	outros. Todos os demais métodos ficam em Methods []NamedFuncDef. Pelo
//	menos um método (Init ou qualquer outro) deve estar presente.
//
//	icon:, label: e menu: são diretivas visuais nos comentários de struct e
//	método. Servem ao menu hexagonal e ao cabeçalho do bloco na IDE WASM.
//	menu:col,row. permite ao especialista fixar a posição de um método no
//	menu radial. Quando ausente, o layout automático é aplicado.

// BlackBoxDef is the full definition of a black-box device.
// AuthorInfo is the attribution for a black-box: the specialist who wrote it,
// identified by their GitHub provenance. Both fields come from the device row
// (github_owner / github_url) at def-load time, never from the parsed source —
// the source cannot reliably carry its own canonical origin, but the repository
// it was captured from can. Used to stamp attribution into generated code.
//
// Português: Atribuição de uma black-box — o especialista que a escreveu,
// identificado pela proveniência do GitHub (github_owner / github_url),
// preenchida no load (não pelo parser). Usada para carimbar a autoria no código
// gerado.
type AuthorInfo struct {
	// Username is the GitHub account the code was captured from (github_owner).
	Username string `json:"username,omitempty"`

	// URL is the source repository URL (github_url) — the verifiable origin,
	// which also carries the component's own license.
	URL string `json:"url,omitempty"`
}

type BlackBoxDef struct {
	// Name is the struct name (e.g. "APDS9960"). Used as device label.
	Name string `json:"name"`

	// Doc is the package-level documentation comment.
	Doc string `json:"doc,omitempty"`

	// ID is the black-box's database id — the token minted when the black-box
	// is created (blackboxes.id for GitHub-sourced devices, projects.id for
	// devices born in the in-IDE wizard). Unique by construction, never reused
	// after deletion. It is NOT produced by the source parser — like Author it
	// is stitched in at def-load time (store.LoadBlackBoxDefsForScene), the one
	// place the parsed def and its database row meet.
	//
	// This id is the SINGLE source of uniqueness for the multi-file C output:
	// SymbolPrefix(ID) prefixes every exported C symbol and SourceDir(ID) names
	// the black-box's folder (see naming.go for why the id, and why the prefix
	// is applied unconditionally). Empty for a def that never touched the
	// database — the emitter must treat that as "no isolated identity" and fall
	// back to the single-file inline path, never invent an id of its own.
	//
	// SECURITY: the loader ALWAYS overwrites this field from the row, even when
	// the cached parsed_json already carries one. A stored blob is data, not
	// identity — trusting an id embedded in it would let a crafted blob claim
	// another black-box's folder and prefix. Same stance as the unconditional
	// symbol prefix: identity comes from the database, full stop.
	//
	// Português: ID da black-box no banco (blackboxes.id para devices do GitHub,
	// projects.id para devices criados no wizard da IDE). Único, nunca reusado.
	// NÃO vem do parser — como o Author, é costurado no load. É a ÚNICA fonte de
	// unicidade da saída C multiarquivo: SymbolPrefix(ID) prefixa os símbolos e
	// SourceDir(ID) nomeia a pasta (ver naming.go). Vazio = def que nunca passou
	// pelo banco; o emitter cai no caminho inline de arquivo único, nunca
	// inventa id. SEGURANÇA: o loader SEMPRE sobrescreve a partir da linha do
	// banco, mesmo que o parsed_json em cache traga um id — blob é dado, não
	// identidade.
	ID string `json:"id,omitempty"`

	// CodeID is the black-box's CODE NUMBER as a decimal string ("47") — the
	// short identity used in every generated-code name (folder iotm_47/,
	// files iotm_47.{c,h}, symbol prefix iotm_47_, guard IOTM_47_H; see
	// naming.go). Like ID it is stitched at def-load time by the store
	// loader, NEVER by the parser, and NEVER invented by the emitter: the
	// number comes from the central sequential allocator (the store's
	// CodeNumberAllocator contract — positive, strictly increasing, never
	// reused even after its owner is deleted).
	//
	// It is a STRING on purpose: the def is a JSON contract and the naming
	// layer is pure string composition; number-ness is an allocator concern.
	// The loader is the single writer and formats canonically (base-10, no
	// padding). Empty means "no number stitched" (legacy blob, test fixture,
	// registry miss) — CodeIdent then falls back to the full ID, producing
	// long-but-correct names in the same family. Serialized (omitempty) so
	// the handler→worker CodegenPayload round-trip preserves it.
	//
	// Português: NÚMERO DE CÓDIGO da black-box como string decimal ("47") —
	// a identidade curta de todos os nomes do código gerado. Costurado pelo
	// loader (nunca pelo parser, nunca inventado pelo emitter) a partir do
	// alocador sequencial central (contrato: positivo, crescente, nunca
	// reusado nem após deleção). É string de propósito: o def é contrato
	// JSON e o naming é composição de string. Vazio → CodeIdent cai no ID
	// completo (nomes longos, porém corretos).
	CodeID string `json:"codeId,omitempty"`

	// Author is the attribution for this black-box (see AuthorInfo). It is NOT
	// produced by the source parser — it is populated at def-load time
	// (store.LoadBlackBoxDefsForScene) from the device row's GitHub provenance.
	// Nil for the maker's OWN in-editor code (that code is the maker's, so it
	// carries no third-party attribution) and for any def whose row has no
	// provenance. The code generator reads this to stamp a contributor manifest
	// in the file header and an inline note on each emitted block.
	//
	// Português: Atribuição desta black-box (ver AuthorInfo). NÃO vem do parser
	// — é populada no load a partir da proveniência GitHub da linha do device.
	// Nil para o código próprio do maker no editor. O gerador a lê para carimbar
	// o manifesto de autores no header e a nota inline em cada bloco emitido.
	Author *AuthorInfo `json:"author,omitempty"`

	// StructIcon is the FontAwesome icon name (kebab-case) declared in the
	// struct doc comment with "icon:name.". Used in the Hardware menu and as
	// the fallback icon for methods that do not declare their own icon.
	//
	// Example: // icon:greater-than-equal.
	StructIcon string `json:"structIcon,omitempty"`

	// StructLabel is the human-readable display name declared in the struct
	// doc comment with "label:text.". Used as the first part of the block
	// header: "{StructLabel} {MethodLabel}" (e.g. "APDS9960 log").
	//
	// Example: // label:APDS9960.
	StructLabel string `json:"structLabel,omitempty"`

	// Interactive is the identifier of an SVG diagram declared in the struct
	// doc comment with "interactive:name." (e.g. "interactive:rp2040.").
	// The value must be a single token (no spaces) matching an SVG filename
	// in the root of the GitHub release ZIP (without the .svg extension).
	//
	// After the worker processes the ZIP it replaces this stem with the
	// resolved public URL of the saved file
	// (e.g. "/files/devices/owner/repo/rp2040.svg") so the WASM IDE can
	// fetch it directly.
	//
	// When set, the IDE Inspect panel's Help tab activates this SVG inline
	// inside the markdown content: elements whose data-id matches a prop's
	// current value are highlighted using the colour from the SVG's
	// data-palette attribute. The feature is not limited to hardware boards —
	// any SVG that follows the IoTMaker Interactive Diagram Specification
	// can be referenced here. See docs/INTERACTIVE_DIAGRAM_SPEC.md.
	//
	// Example: // interactive:rp2040.
	Interactive string `json:"interactive,omitempty"`

	// Imports are the Go import paths needed by this black-box.
	Imports []string `json:"imports"`

	// Init describes the Init() method signature. Nil when absent.
	// Init has special semantics: it always runs before all other methods.
	Init *FuncDef `json:"init,omitempty"`

	// Methods describes all non-Init methods in source-file order.
	// Each entry has its own Name (e.g. "Run", "Log", "Step").
	// The IDE creates one visual block per entry.
	Methods []NamedFuncDef `json:"methods,omitempty"`

	// Props are struct fields with `prop` tags — editable in Inspect panel.
	Props []PropDef `json:"props,omitempty"`

	// StructCode is the raw Go source of the struct type definition.
	StructCode string `json:"structCode"`

	// MethodsCode is the raw Go source of all methods (including Init).
	MethodsCode string `json:"methodsCode"`

	// ManualPages are kept for backward compatibility but are no longer populated
	// by the worker. Help is now sourced from markdown files in the GitHub repo.
	ManualPages []ManualPage `json:"manualPages,omitempty"`

	// Help contains the structured help content extracted from markdown files
	// in the root of the GitHub repository (readme.md, init.en.md, etc.).
	// Populated by the worker; never by the Go source parser.
	Help DeviceHelp `json:"help,omitempty"`

	// Structs is the additive multi-struct payload introduced in
	// Slice C99-2 (see docs/claude_c99_device_support.md §9.5).
	//
	// The Go parser today populates ONLY the legacy single-struct
	// fields above (Name, Doc, StructIcon, …, Init, Methods, Props,
	// StructCode). The C99 parser, starting in Slice 2, collects
	// every struct it finds in the source and puts the full set
	// here. Legacy fields above continue to mirror Structs[0] so
	// the existing SPA renders unchanged.
	//
	// JSON marshalling drops this field when empty, so projects
	// stored before the field existed deserialize identically.
	//
	// Starting in Slice C99-5, two kinds of entries can live here:
	//   1. Real structs declared in the source. IsFunctionGroup=false.
	//   2. Virtual "function-group" devices — sets of public
	//      functions that share a common prefix and don't belong to
	//      any explicit struct. IsFunctionGroup=true. These exist
	//      so module-pattern C code (static state + public
	//      functions) maps cleanly to a single visual device.
	Structs []StructDef `json:"structs,omitempty"`

	// Enums collects every C99 enum that is referenced in the
	// signature of at least one public function (Slice C99-6 — "enum
	// type devices", see docs/claude_c99_device_support.md §12.2).
	//
	// An enum that appears only inside `static` helpers is NOT
	// surfaced — the same "se é interno, não representa" gate that
	// applies to structs and functions.
	//
	// Enums are NOT executable devices: they carry no behaviour and
	// no ports. They exist so the specialist can attach a
	// human-readable label to each enumerator. In the IDE, a port
	// typed as the enum renders as a dropdown of those labels, with
	// the enumerator's integer value flowing to generated code.
	//
	// The Go parser never populates this field. JSON omitempty keeps
	// the wire shape identical for enum-free projects.
	Enums []EnumDef `json:"enums,omitempty"`

	// Functions collects each public C99 function that is NOT a
	// method of a real struct — i.e. a standalone function device
	// (Slice C99-8, "one device per function", §13.x). C99 is the
	// source of truth: a function has parameters and a return type;
	// there is no grouping by prefix and no "runs first" concept.
	//
	// Each entry's ports come straight from the signature: non-
	// pointer / const-pointer params are inputs, mutable-pointer
	// params are outputs, and a non-void return is one more typed
	// output named "return". The SPA renders these ports directly
	// on the device card — there is no method sub-level.
	//
	// The Go parser never populates this field. JSON omitempty keeps
	// the wire shape identical for projects without standalone
	// functions.
	Functions []NamedFuncDef `json:"functions,omitempty"`

	// WireTypes collects every C99 struct that is referenced in the
	// signature of at least one public function but is NOT itself an
	// executable device. Decision (b), 2026-05-25: C99 has no methods,
	// so a `struct Sensor *` parameter is just a typed wire — the
	// struct is the type carried on that wire, produced by some device
	// (e.g. a constructor returning `struct Sensor *`) and consumed by
	// another. The struct never becomes a device with methods; its
	// functions are all plain device-functions in Functions[] where the
	// former receiver is an ordinary input port.
	//
	// Internal structs (referenced only by `static` helpers, or not at
	// all) are NOT surfaced — same gate as enums and functions.
	//
	// The Go parser never populates this field; Go has real methods and
	// uses Structs[]/Methods[] instead.
	WireTypes []StructDef `json:"wireTypes,omitempty"`

	// Files is the authored source snapshot the def was parsed from: every
	// file of the specialist's device project, verbatim, in tab order. The
	// C parser sets it (ParseCFiles); the multi-file emitter ships each
	// entry into the box's folder — .c files wrapped by the rename
	// preamble/postamble, .h files untouched (they are renamed at inclusion
	// time by the including .c's defines). The implementation lives HERE,
	// not in the parsed metadata: the parser keeps signatures, not bodies.
	// A single-source flow (the marketplace worker parsing one file from a
	// release) is simply the one-entry case of the same representation —
	// there is deliberately no separate "raw source" field.
	//
	// Português: O snapshot autoral de onde o def foi parseado: todos os
	// arquivos do projeto do especialista, verbatim, na ordem das abas. O
	// emitter multiarquivo embarca cada entrada na pasta da caixa (.c com
	// preâmbulo/posâmbulo de renomeação; .h intocado — renomeia na inclusão
	// pelos defines do .c incluidor). Fluxo de fonte única é o caso de uma
	// entrada da MESMA representação — não existe campo "raw source"
	// separado, de propósito.
	Files []FileEntry `json:"files,omitempty"`

	// Assets are the def's non-source files (unified asset model):
	// attached by the store loader from the SAME snapshot Files came
	// from, after the parser dispatch filtered them out of Files —
	// Files stays source-only (the len()==1 dispatch, the validator and
	// the wizard depend on that invariant); Assets is the cargo lane.
	// The maker's C emitter ships each one into the box folder and
	// generates its companion header beside it.
	//
	// Português: Os não-fonte do def, anexados pelo loader do MESMO
	// snapshot — Files segue só-fonte (invariante do dispatch); Assets
	// é a faixa de carga. O emissor C embarca cada um na pasta da caixa
	// e gera o header companheiro ao lado.
	Assets []AssetEntry `json:"assets,omitempty"`

	// ExternalNames lists the non-static file-scope VARIABLE names found
	// across the authored files. Together with Functions (the parser
	// already excludes `static` functions) it completes the box's set of
	// external link symbols, which is exactly the RENAME set: internal
	// linkage across a specialist's own files requires non-static symbols,
	// and an unrenamed `util_state` in two different boxes would collide in
	// the maker's link. Rename ALL externals; the generated header still
	// exposes ONLY the IDS surface (rename-all, expose-some — see
	// csurface.go). Best-effort by the tolerant parser: exotic declarators
	// it cannot read are simply not captured (documented limitation).
	//
	// Português: Nomes das VARIÁVEIS file-scope não-static de todos os
	// arquivos. Com Functions (funções static já são excluídas) completa o
	// conjunto de símbolos externos da caixa = o conjunto de RENOMEAÇÃO:
	// linkage interna entre arquivos exige não-static, e um `util_state`
	// sem renome colidiria entre caixas no link do maker. Renomeia-se TUDO
	// que é externo; o header expõe SÓ a superfície IDS.
	ExternalNames []string `json:"externalNames,omitempty"`

	// CallbackTypes collects every function-pointer typedef declared in the
	// source (`typedef void (*sht3x_alert_cb_t)(float, void *);`). These are
	// the C99 equivalent of a LabVIEW strictly-typed VI reference: a parameter
	// of one of these types (e.g. `cb` in `sht3x_set_alert`) is not a value
	// the maker computes but a REFERENCE to a handler function, satisfied by
	// wiring a `// callback:<type>.` device into it. Recording the type here
	// lets the parser flag such parameters (PortDef.CallbackType) and lets the
	// IDE offer only signature-compatible handlers on the wire. The Go path
	// never populates this — Go uses first-class func values, not typedefs.
	CallbackTypes []CallbackTypeDef `json:"callbackTypes,omitempty"`
}

// CallbackTypeDef describes a single function-pointer typedef found in C99
// source — a "callback type" in the visual model. Its Name is what a
// parameter or handler-reference port carries on its wire; ReturnType and
// Params are kept verbatim for documentation and future structural matching
// (v1 matches handlers to callback parameters by Name alone and lets the C
// compiler catch signature divergence).
type CallbackTypeDef struct {
	// Name is the typedef alias (e.g. "sht3x_alert_cb_t").
	Name string `json:"name"`

	// ReturnType is the callback's return type, verbatim (e.g. "void").
	ReturnType string `json:"returnType,omitempty"`

	// Params is the raw parameter list between the parentheses, verbatim
	// (e.g. "float temperature_c, void *user_data"). Not split into ports —
	// these parameters are supplied by the caller at runtime, never wired.
	Params string `json:"params,omitempty"`

	// Icon and Label are the callback type's visual identity, read from the
	// leading comment above the typedef (`// icon:…. label:….`) — §12.3:
	// the wizard card for a callback type exists for the SOLE purpose of
	// setting these two; the signature is fixed by the typedef.
	//
	// Português: Identidade visual do callback type, lida do comentário
	// líder do typedef — §12.3: o card existe SÓ para icon/label.
	Icon  string `json:"icon,omitempty"`
	Label string `json:"label,omitempty"`

	// Doc is the human prose of the leading comment after directive
	// stripping — same trio as EnumDef.
	Doc string `json:"doc,omitempty"`

	// UsedAsParameter is §12.3's trigger rule: the wizard surfaces a
	// callback-type card ONLY when the typedef is consumed as a parameter
	// of a public function (an input port carries it). A typedef declared
	// but never consumed stays in this list (handler matching still needs
	// it) but does NOT earn a card — "interno não representa": an orphan
	// contract has no counterpart to wire to.
	//
	// Português: A regra de gatilho do §12.3: o card só aparece quando o
	// typedef é consumido como parâmetro de função pública. Declarado e
	// nunca consumido fica na lista (o matching de handlers precisa) mas
	// não ganha card.
	UsedAsParameter bool `json:"usedAsParameter,omitempty"`

	// DeclStart is the typedef keyword's byte offset in the original
	// source — parser-internal (leading-comment read and rewrite anchor);
	// never serialized.
	DeclStart int `json:"-"`
}

// StructDef carries everything the parser knows about a single
// device-like entity in the source.
//
// Two flavours exist (distinguished by IsFunctionGroup):
//
//  1. **Real struct** (IsFunctionGroup=false): the entity has an
//     explicit `struct Name { ... }` declaration in the source.
//     The field list (Props), Init, and Methods all come from
//     that struct + its `<Name>_<Method>(struct Name* s, ...)`
//     functions.
//
//  2. **Function-group** (IsFunctionGroup=true): there is no
//     `struct` declaration. The "device" is conceptual — a set of
//     public functions that share a common identifier prefix.
//     Props is always empty (no state struct to draw from); Init
//     and Methods come from the functions in the group, with
//     names derived by stripping the common prefix.
//     This shape was introduced in Slice C99-5 (2026-05-19) to
//     model the standard C module pattern: static state at file
//     scope plus public functions forming the module's API.
//
// All other fields (icon / label / interactive / methods / props
// semantics) use the same conventions for both flavours — see the
// type-level docs above.
type StructDef struct {
	// Name is the canonical device identifier. For real structs:
	//
	//   struct Tag { ... }                       → Tag
	//   typedef struct Tag { ... } Alias         → Tag    (tag wins)
	//   typedef struct      { ... } Alias        → Alias  (only name available)
	//
	// For function-groups: the longest common prefix of the
	// member function names, trimmed of trailing underscore. For
	// example `display_init`, `display_write`, `display_clear`
	// produce a function-group named `display`.
	//
	// See claude_c99_device_support.md §9.2 for the struct
	// resolution rationale, and §10/§11 for function-group naming.
	Name string `json:"name"`

	// Alias is the typedef alias for the struct, when one exists
	// (`typedef struct Tag { ... } Alias;` or a separate forward
	// `typedef struct Tag Alias;`). Name stays the tag so the rewrite
	// can locate the struct in the source, but the IDE prefers Alias
	// for display because that is the name the specialist writes in
	// public signatures (e.g. an opaque handle `sht3x_t`). Empty when
	// there is no typedef.
	Alias string `json:"alias,omitempty"`

	// Doc is the human-readable prose extracted from the struct's
	// leading comment block. IDS directives are stripped before
	// the value is stored.
	Doc string `json:"doc,omitempty"`

	// Icon is the FontAwesome icon name declared via `// icon:`.
	Icon string `json:"icon,omitempty"`

	// Label is the human-readable display name declared via `// label:`.
	Label string `json:"label,omitempty"`

	// Interactive is the SVG diagram identifier declared via
	// `// interactive:`. Same semantics as BlackBoxDef.Interactive.
	Interactive string `json:"interactive,omitempty"`

	// Init describes the <Struct>_Init function when present.
	Init *FuncDef `json:"init,omitempty"`

	// Methods describes <Struct>_<Method> functions (other than
	// Init) in source-file order.
	Methods []NamedFuncDef `json:"methods,omitempty"`

	// Props are the struct fields surfaced as configurable
	// properties. Includes both tagged (Untagged=false) and
	// untagged (Untagged=true) entries; the wizard renders both.
	Props []PropDef `json:"props,omitempty"`

	// StructCode is the raw source spanning the struct declaration
	// (the `struct ... { ... };` or `typedef struct ... { ... } X;`).
	StructCode string `json:"structCode,omitempty"`
}

// EnumDef carries everything the parser knows about a single C99
// enum that surfaced as a wizard card (Slice C99-6, §12.2).
//
// Naming follows the same precedence as StructDef: when both an
// enum tag and a typedef alias exist, the tag wins for Name. The
// trigger that decides whether an enum surfaces at all
// (referenced-in-public-signature) checks BOTH the tag and the
// alias, because public signatures almost always use the typedef
// alias (`display_color_t`), not the tag.
type EnumDef struct {
	// Name is the resolved identifier (tag if present, else alias).
	Name string `json:"name"`

	// Doc is human-readable prose from the enum's leading comment
	// block, with IDS directives stripped.
	Doc string `json:"doc,omitempty"`

	// Icon / Label come from `// icon:` / `// label:` directives in
	// the enum's leading comment block — the SAME directive syntax
	// and the SAME rewrite path (planCStructDirectives) used for
	// structs. Decision locked 2026-05-20.
	Icon  string `json:"icon,omitempty"`
	Label string `json:"label,omitempty"`

	// Values is the enumerator list in source order. The order is
	// significant: it is how the dropdown is presented in the IDE.
	Values []EnumValueDef `json:"values"`

	// EnumCode is the raw source spanning the enum declaration, for
	// the editor preview pane (mirrors StructDef.StructCode).
	EnumCode string `json:"enumCode,omitempty"`
}

// EnumValueDef is one enumerator inside an EnumDef.
type EnumValueDef struct {
	// Name is the enumerator identifier (e.g. DISPLAY_COLOR_WHITE).
	// This is fixed by the source and is NOT editable in the wizard.
	Name string `json:"name"`

	// Value is the integer the enumerator resolves to, computed per
	// C99 rules: an enumerator without an initialiser takes the
	// previous value + 1; the first without an initialiser is 0.
	// Explicit decimal and hexadecimal (`0x…`) initialisers are
	// honoured. Non-trivial constant expressions are not evaluated
	// in this slice (see ValueIsRaw).
	Value int `json:"value"`

	// ValueIsRaw is true when the enumerator's initialiser was a
	// constant expression the parser could not evaluate to an int
	// (e.g. `1 << 3`, `RED | GREEN`). In that case Value is best-
	// effort (often 0) and RawValue carries the source text so the
	// SPA can show it verbatim instead of a misleading number.
	ValueIsRaw bool `json:"valueIsRaw,omitempty"`

	// RawValue is the verbatim initialiser text when ValueIsRaw.
	RawValue string `json:"rawValue,omitempty"`

	// Label is the human-readable label the specialist assigns via
	// the wizard, persisted as a `// label:…` leading comment above
	// the enumerator (Decision 1A, 2026-05-20). Empty label = the
	// enum card shows "⚠ Incomplete ⚠".
	Label string `json:"label,omitempty"`
}

// HasInit returns true if this device has an Init() method.
func (d *BlackBoxDef) HasInit() bool { return d.Init != nil }

// CodeIdent returns the identity token the naming family composes names from:
// the short CodeID when the loader stitched one, else the full database ID.
// This is the ONE place the short-vs-fallback choice is made — every consumer
// (CSurface, the C emitter's file assembly) goes through it, so the two can
// never disagree about which identity a box exports under. An empty result
// means the def has no identity at all (never touched the database) and the
// emitter must route it through the single-file inline fallback instead.
//
// Português: Token de identidade de onde a família de nomes compõe: o CodeID
// curto quando costurado, senão o ID completo. Único lugar da escolha —
// todos os consumidores passam por aqui, então nunca divergem. Vazio = def
// sem identidade nenhuma → caminho inline.
func (d *BlackBoxDef) CodeIdent() string {
	if d.CodeID != "" {
		return d.CodeID
	}
	return d.ID
}

// HasMethods returns true if this device has at least one non-Init method.
func (d *BlackBoxDef) HasMethods() bool { return len(d.Methods) > 0 }

// GetMethod returns the NamedFuncDef for the given method name, or nil when absent.
// The lookup is case-sensitive because Go method names are case-sensitive.
//
// Português: Retorna o NamedFuncDef para o nome de método dado, ou nil se ausente.
func (d *BlackBoxDef) GetMethod(name string) *NamedFuncDef {
	for i := range d.Methods {
		if d.Methods[i].Name == name {
			return &d.Methods[i]
		}
	}
	return nil
}

// NamedFuncDef describes the signature of any non-Init method (Run, Log, Step, …).
// It embeds FuncDef and adds the method name so callers do not need to track
// the name separately.
//
// Português: Descreve a assinatura de qualquer método não-Init. Incorpora
// FuncDef e adiciona o nome do método.
type NamedFuncDef struct {
	// Name is the Go method name (e.g. "Run", "Log", "Step").
	// Used as the suffix of the visual block type: "BlackBox{Name}:{Struct}".
	Name string `json:"name"`

	// SourceFile is the authored path this function came from — the
	// def.Files entry whose parse produced it. Stamped by the multi-file
	// C parser (ParseCFiles) and by the single-file dispatch; when the
	// definition-upgrades-prototype merge fires, the entry carries the
	// DEFINITION's file, which is also where the IDS annotations live.
	// The wizard is the consumer: cards group and badge by file, and a
	// rewrite targets {files, file: card.sourceFile}. Empty means "the
	// def has a single source and this predates stamping" — consumers
	// fall back to Files[0].
	//
	// Português: O caminho autoral de onde a função veio — a entrada de
	// def.Files cujo parse a produziu. Carimbado pelo ParseCFiles e pelo
	// dispatch de arquivo único; no upgrade definição-sobre-protótipo, a
	// entrada carrega o arquivo da DEFINIÇÃO (onde moram as anotações).
	// O wizard consome: cards agrupam por arquivo e o rewrite mira
	// {files, file}. Vazio = fonte única pré-carimbo; fallback Files[0].
	SourceFile string `json:"sourceFile,omitempty"`

	FuncDef
}

// FuncDef describes the signature of a single method (Init or any named method).
type FuncDef struct {
	// HasBody reports whether this entry came from a function DEFINITION
	// (`{ ... }`) rather than a bare prototype (`;`). C-space only — the
	// Go parser never sets it (Go has no prototypes). The multi-file
	// merge is the consumer: `int probe_read(probe_t *);` in api.h and
	// the annotated definition in core.c are the SAME function seen
	// twice, and the definition must win regardless of tab order —
	// the standard C layout puts the header (prototypes) first, so a
	// naive keep-first would keep the bare, unannotated entry. Mirrors
	// the intra-file rule the scanner already applies (see
	// parser_c_func.go, "keep the one with HasBody=true").
	//
	// Português: Esta entrada veio de uma DEFINIÇÃO (corpo) ou de um
	// protótipo? Só espaço-C. O consumidor é o merge multiarquivo:
	// protótipo no api.h + definição anotada no core.c são a MESMA
	// função vista duas vezes, e a definição precisa vencer
	// independente da ordem das abas — o layout padrão do C põe o
	// header primeiro, e um keep-first ingênuo ficaria com a entrada
	// crua. Espelha a regra intra-arquivo que o scanner já aplica.
	HasBody bool `json:"hasBody,omitempty"`

	// Doc is the method documentation comment (machine directives stripped out).
	// The directives executionOrder:, icon:, label: are extracted before this
	// field is populated, so the value contains human-readable prose only.
	Doc string `json:"doc,omitempty"`

	// ExecutionOrder defines the relative execution position of this method
	// among methods in the same scope that are not connected by wires.
	// Value 0 means "not set" — unordered methods run after all ordered ones.
	// Integers do not need to be contiguous: 1, 2, 3 and 10, 20, 30 are equal.
	ExecutionOrder int `json:"executionOrder,omitempty"`

	// Icon is the FontAwesome icon name (kebab-case) for this specific method,
	// declared as "icon:name." in the method doc comment.
	// Used in the hex menu function submenu and at the top of the visual block.
	// When empty, the IDE falls back to the struct-level StructIcon.
	//
	// Example: // icon:greater-than-equal.
	Icon string `json:"icon,omitempty"`

	// Label is the human-readable display name for this method, declared as
	// "label:text." in the method doc comment.
	// Combined with the struct StructLabel to form the visual block header.
	// When empty, falls back to the Go method name.
	//
	// Example: // label:log.
	Label string `json:"label,omitempty"`

	// MenuCol is the column offset from the Back button center in the hex menu.
	// Declared as the first integer in "menu:col,row." in the method doc comment.
	// Signed: negative moves left, positive moves right.
	// Only meaningful when MenuPosSet is true; ignored otherwise.
	//
	// Example: // menu:-1,-1.  → one column left, one row up from Back.
	MenuCol int `json:"menuCol,omitempty"`

	// MenuRow is the row offset from the Back button center in the hex menu.
	// Declared as the second integer in "menu:col,row." in the method doc comment.
	// Signed: negative moves up, positive moves down.
	// Only meaningful when MenuPosSet is true; ignored otherwise.
	MenuRow int `json:"menuRow,omitempty"`

	// MenuPosSet is true when the specialist explicitly declared "menu:col,row."
	// in the method doc comment. When false, the IDE auto-places this item using
	// the radial layout engine (rulesMainMenu.ApplyRadialLayout).
	// Stored separately from MenuCol/MenuRow because (0,0) is a valid—but
	// reserved—position (Back button), and we must distinguish "not set" from
	// "intentionally set to zero".
	MenuPosSet bool `json:"menuPosSet,omitempty"`

	// Inputs are the function parameters — become input ports (wires arriving).
	Inputs []PortDef `json:"inputs,omitempty"`

	// Outputs are the named return values — become output ports (wires leaving).
	Outputs []PortDef `json:"outputs,omitempty"`

	// ConsumesHandle marks a C99 device-function that CONSUMES the
	// wire-type handle it receives and does NOT republish it — i.e. the
	// destructor (`sht3x_destroy`), the end of the LabVIEW-style resource
	// chain. It records the authored `// handle:consume.` directive only;
	// like label/icon/executionOrder it expresses specialist intent and
	// synthesizes nothing, so the parsed def stays faithful to the source.
	// FunctionSynthesizedOutputs skips the pass-through output when this is
	// set. The Go path never sets it. See docs/c99_ide_integration.md §2.2.
	ConsumesHandle bool `json:"consumesHandle,omitempty"`

	// HandlerType marks a C99 device-function as a CALLBACK HANDLER: it
	// records the type of `// callback:<callbackType>[:<mode>].`, naming the
	// function-pointer typedef whose signature this function implements
	// (e.g. "sht3x_alert_cb_t"). A handler can be passed by reference into a
	// callback parameter of that type (the LabVIEW static VI reference idiom):
	// its address is handed over and it is NOT called at that connection.
	// CallbackMode (below) decides which device variants the IDE offers. Codegen
	// never emits a BB_CALL for the CALLBACK reference device; it inlines the
	// body and passes the function name. Empty for ordinary functions. The Go
	// path never sets it.
	HandlerType string `json:"handlerType,omitempty"`

	// CallbackMode refines a callback handler (only meaningful when HandlerType
	// is set). It records the `<mode>` of `// callback:<type>:<mode>.` and is
	// METADATA only: the parsed def is ALWAYS the pure callable (its parameters
	// are kept; it is NEVER given a `callback` output). The callback REFERENCE is
	// a SEPARATE dedicated device (scene type "CallbackRef:<fn>") the IDE
	// synthesizes from the function name + HandlerType. The mode decides which
	// device variants the IDE offers:
	//
	//   - "both" (default; also the value for a bare `// callback:<type>.`) —
	//     BOTH the callable device (its parameters are inputs; codegen emits a
	//     direct call) and the separate callback reference device (passed by
	//     address) are offered.
	//   - "ref" — ONLY the callback reference device is offered (no callable
	//     variant). The def still keeps its parameters; "ref" is purely an
	//     IDE-level offering decision, not a port-shape change on the def.
	//
	// Empty for ordinary functions. The Go path never sets it. See the duality
	// section of docs/CODEGEN_C99_CALLBACKS.md.
	CallbackMode string `json:"callbackMode,omitempty"`

	// CompatibleCallbacks lists the callback typedef names (from
	// BlackBoxDef.CallbackTypes) whose C signature MATCHES this function's
	// signature — return type plus the ordered parameter types, parameter
	// names ignored. These are the only types this function may be marked a
	// handler of: a `// callback:T.` is valid only when T is in this list,
	// because the generated `consumer(fn)` is well-typed only when the
	// signatures match. Computed in ParseC once both the functions and the
	// typedefs are known (it is derived from the RAW signature, so it is
	// independent of whether the function is already a handler — a handler's
	// own type is therefore always present here). The wizard offers exactly
	// this set in its "Callback handler" dropdown and disables the control
	// when it is empty, so a signature-incompatible pick is impossible.
	// Empty for functions with no matching typedef. The Go path never sets it.
	CompatibleCallbacks []string `json:"compatibleCallbacks,omitempty"`

	// CReturnType / CParams preserve the authored C signature VERBATIM:
	// CReturnType is the raw return-type text ("int", "sht3x_t *", …) and
	// CParams the raw comma-separated parameter list exactly as written
	// between the parentheses (empty for `()` and `(void)`… the latter keeps
	// its literal "void"). Filled by ParseC only (funcDefFromRaw); the Go
	// path leaves both empty.
	//
	// They exist for the multi-file C output: the generated bb_<id>.h must
	// declare `<CReturnType> P<id>_<name>(<CParams>);`, and the port lists
	// cannot rebuild that faithfully — they transform the signature on
	// purpose (slice pairs collapse into one "[]T" port, out-params move to
	// Outputs, pass-throughs are synthesized). The definition shipped in
	// bb_<id>.c is the authored source, so the prototype must be authored
	// text too, or the compiler's declaration check would fight the ports'
	// visual model. See csurface.go for the composition.
	//
	// Português: Assinatura C autoral VERBATIM (retorno + lista de
	// parâmetros como escrita). Só o ParseC preenche. Existe para o header
	// gerado da saída multiarquivo — as portas transformam a assinatura de
	// propósito, então não servem de fonte para o protótipo.
	CReturnType string `json:"cReturnType,omitempty"`
	CParams     string `json:"cParams,omitempty"`
}

// CallbackMode values for FuncDef.CallbackMode — the `<mode>` of the
// `// callback:<type>:<mode>.` directive.
const (
	// CallbackModeBoth offers BOTH device variants: the callable device
	// (parameters as inputs, a direct call) and the separate callback reference
	// device (passed by address). It is the default — including for a bare
	// `// callback:<type>.` with no explicit mode.
	CallbackModeBoth = "both"
	// CallbackModeRef offers ONLY the separate callback reference device (no
	// callable variant). The parsed def still keeps its parameters; this is an
	// IDE-level offering decision, not a port-shape change on the def.
	CallbackModeRef = "ref"
)

// PortDef describes a single input or output port.
//
// IDS tag syntax for port metadata (written as line comments directly above
// each parameter or return value in the method signature):
//
//	// doc: Human-readable description of what this port carries.
//	// connection: mandatory   — wire must be connected; IDE shows a warning if not.
//	// connection: optional    — wire is optional (default when tag is absent).
//	// range: min..max         — allowed value range (e.g. "0..255", "0.0..1.0").
//	// rangeMin: N             — lower bound only (use instead of range: when max is open).
//	// rangeMax: N             — upper bound only (use instead of range: when min is open).
//	// unit: label             — physical unit (e.g. "°C", "Hz", "m/s²").
//	// encoding: label         — data encoding (e.g. "UTF-8", "I2C-7bit", "big-endian").
//	// default: value          — default value when port is unwired.
//	// bits: N                 — significant bit count for integer types (e.g. "16").
//
// All tags are dot-terminated and case-insensitive. They may appear on the
// same line or on separate lines in the comment block directly above the
// parameter list. Tags not listed above are treated as prose and stored in Doc.
//
// Example:
//
//	func (s *Sensor) Run(
//		// doc: I2C bus instance. connection: mandatory.
//		i2c machine.I2C,
//	) (
//		// doc: Luminosity in lux. unit: lux. range: 0..65535.
//		lux uint16,
//		err error,
//	)
type PortDef struct {
	// Name is the parameter/return name.
	Name string `json:"name"`

	// GoType is the full Go type string.
	GoType string `json:"goType"`
	// WireType, when set, is the CONNECTOR token this port exposes on the
	// stage — decoupled from GoType so the authored C type stays verbatim
	// for codegen (declarations, casts, the wizard card) while the wire
	// speaks the IDE's family vocabulary. Today it carries the scalar
	// pointer family tokens ("int*", "float*", "bool*", "byte*"); empty
	// means "use GoType", which is every port that existed before.
	// Português: Quando presente, é o token de CONECTOR que esta porta
	// expõe no stage — desacoplado do GoType para o tipo C autoral ficar
	// verbatim no codegen (declarações, casts, o card do wizard) enquanto
	// o fio fala o vocabulário de famílias da IDE. Hoje carrega os tokens
	// de ponteiro escalar ("int*", "float*", "bool*", "byte*"); vazio
	// significa "use GoType" — todo porto pré-existente.
	WireType string `json:"wireType,omitempty"`

	// IsError is true if this port is an error return.
	IsError bool `json:"isError,omitempty"`

	// Label is the human-readable display name set by the wizard
	// via the `label:` IDS directive in the port's godoc. Empty when
	// the directive is absent — the IDE falls back to Name in that
	// case. Setting this to a non-empty string is required by the
	// wizard's port-completeness rule (see completion.go).
	Label string `json:"label,omitempty"`

	// Doc is the human-readable port description extracted from the IDS
	// comment block directly above the parameter or return value.
	Doc string `json:"doc,omitempty"`

	// Connection declares whether a wire is required.
	// "mandatory" — IDE shows a warning when the port is not wired.
	// "optional"  — wire is optional (default when the tag is absent).
	// ""          — tag was absent; treated identically to "optional".
	Connection string `json:"connection,omitempty"`

	// MissingConn is true when the connection: tag was absent entirely.
	// The IDE uses this to show the ⚠ "connection: missing" badge.
	// Note: omitempty is intentionally absent — false must be serialized so
	// the frontend can distinguish "not missing" from "field not present".
	MissingConn bool `json:"missingConn"`

	// Range is the combined range string (e.g. "0..255").
	// Populated when the range: tag is present.
	Range string `json:"range,omitempty"`

	// RangeMin is the lower bound (e.g. "0") when only the lower limit is declared.
	RangeMin string `json:"rangeMin,omitempty"`

	// RangeMax is the upper bound (e.g. "255") when only the upper limit is declared.
	RangeMax string `json:"rangeMax,omitempty"`

	// Unit is the physical unit label (e.g. "°C", "Hz", "m/s²").
	Unit string `json:"unit,omitempty"`

	// Encoding is the data encoding label (e.g. "UTF-8", "I2C-7bit").
	Encoding string `json:"encoding,omitempty"`

	// Default is the default value string when the port is not wired.
	Default string `json:"default,omitempty"`

	// Bits is the significant bit count for integer types (e.g. "16").
	Bits string `json:"bits,omitempty"`

	// PassThrough marks a SYNTHESIZED output port: the republished copy
	// of a wire-type input, added so a C99 resource handle can be chained
	// block-to-block on the stage (the LabVIEW refnum idiom). It is never
	// produced by the C99 parser (ParseC keeps the def faithful — a C
	// function's only real output is its return). It is created on demand
	// by BlackBoxDef.FunctionSynthesizedOutputs and lives only in the DTO/
	// stage; codegen MUST NOT emit it as a return value — the generated
	// call passes the same handle variable, and the next call receives it.
	// See docs/c99_ide_integration.md §2.1.
	PassThrough bool `json:"passThrough,omitempty"`

	// ParamIndex is the zero-based position of this port in the C function's
	// parameter list. It is recorded so codegen can rebuild the call in source
	// order, since inputs and out-params interleave in the signature
	// (`read(dev, &temperature, &humidity)`): the def splits params into
	// Inputs[] and Outputs[], which alone loses the original ordering.
	//
	// Set ONLY for ports that ARE parameters. The synthesized "return" output
	// (no C name, appended after the parameter loop) and the synthesized
	// PassThrough leave it at zero; codegen never treats those as call args, so
	// the zero value is unambiguous in practice. The Go path leaves it zero.
	// omitempty is safe: index 0 round-trips to 0 (the first parameter).
	ParamIndex int `json:"paramIndex,omitempty"`

	// SliceLenName / SliceLenIndex describe a COLLAPSED COLLECTION port
	// (C99 `slice:` directive, const-array plan Task 7). The specialist
	// marks a pointer parameter with `slice:<lenParamName>.` in its
	// leading comment; the parser then pairs `const T* values` +
	// `size_t values_len` into THIS single input port typed "[]T",
	// removes the length parameter from the port list, and records here
	// the length parameter's name and its position in the C signature.
	// Codegen rebuilds the pair at the call site —
	// `f(constArray1, constArray1_len)` — placing each argument at its
	// recorded ParamIndex, so the two need not be adjacent. Empty/zero
	// for ordinary ports and for the whole Go path.
	//
	// Português: Porta de COLEÇÃO COLAPSADA (diretiva `slice:` do C99).
	// O par (ponteiro, tamanho) vira UMA porta "[]T"; o parâmetro de
	// tamanho some da lista e fica registrado aqui (nome + posição na
	// assinatura) para o codegen reconstruir a chamada.
	SliceLenName  string `json:"sliceLenName,omitempty"`
	SliceLenIndex int    `json:"sliceLenIndex,omitempty"`

	// CallbackType is set when this port carries a function-pointer typedef
	// (a callback type — see BlackBoxDef.CallbackTypes). On an INPUT it marks
	// a callback parameter (e.g. `cb` in `sht3x_set_alert`): the maker does
	// not compute a value here, but wires a reference from a compatible
	// handler device. On the single OUTPUT of a `// callback:<type>.` device
	// it marks the handler reference itself. The value is the callback type
	// name (e.g. "sht3x_alert_cb_t"); the IDE renders these ports distinctly
	// and allows a wire only between matching names (the strict-typing rule).
	// Empty for ordinary value/handle ports. The Go path never sets it.
	CallbackType string `json:"callbackType,omitempty"`
}

// PropDef describes an editable property derived from a struct field.
//
// Two categories of fields end up here, distinguished by the
// `Untagged` flag:
//
//   - Tagged fields (Untagged=false) — fields with a `prop:"..."`
//     struct tag. These were authored explicitly as configurable
//     properties. Label, Default, Options, etc. are all populated
//     from the tag.
//
//   - Untagged exported fields (Untagged=true) — public Go fields
//     (uppercase first letter) that have NO `prop:"..."` tag yet.
//     The wizard surfaces them so the user can either:
//     a) Promote them to props (the wizard adds the tag for them)
//     when the type is native — flagged with `NativeType=true`.
//     b) See them as inert rows (visible but not clickable) when
//     the type is non-native (pointers, slices, qualified
//     types like *machine.I2C). The wizard cannot generate a
//     UI for these — that decision belongs to the specialist
//     who knows what the field is for, so the wizard shows
//     them as informational rows without ⚠.
//
// Unexported (lowercase first letter) fields are filtered out by
// extractProps and never reach this struct — they are internal
// device state by Go convention.
type PropDef struct {
	FieldName string   `json:"fieldName"`
	GoType    string   `json:"goType"`
	Label     string   `json:"label"`
	Default   string   `json:"default,omitempty"`
	Options   []string `json:"options,omitempty"`

	// Connection is the diagram role identifier declared with the `connection`
	// struct tag (e.g. connection:"I2C_SDA"). When set, the IDE links this
	// prop to an interactive SVG diagram: the prop's current value is matched
	// against data-id attributes in the SVG, and the role is looked up in
	// the SVG's data-palette to determine the highlight colour.
	//
	// The colour is NOT stored in the Go struct — it comes from the SVG's
	// data-palette attribute at runtime.
	//
	// Example: sda string `prop:"SDA Pin" default:"GP4" connection:"I2C_SDA"`
	Connection string `json:"connection,omitempty"`

	// Doc is the field's leading godoc comment, stripped of any IDS
	// machine directives. Empty when the source has no comment for
	// the field. The wizard's Field modal hydrates its Comment input
	// from this.
	Doc string `json:"doc,omitempty"`

	// Untagged is true when the field has no `prop:"..."` struct tag.
	// Combined with NativeType this drives wizard rendering:
	//   - Untagged=false                   → tagged prop, fully editable
	//   - Untagged=true,  NativeType=true  → untagged native field,
	//                                        ⚠ in the wizard, clickable
	//                                        (saving the modal adds the
	//                                        prop:"..." tag)
	//   - Untagged=true,  NativeType=false → untagged non-native field,
	//                                        inert row, no ⚠
	Untagged bool `json:"untagged,omitempty"`

	// NativeType is true when GoType is a simple type the wizard can
	// generate a UI for (bool, int*, uint*, byte, rune, float*,
	// string). Pre-computed by the parser so the frontend does not
	// duplicate the type-name table. See completion.IsNativePropType.
	NativeType bool `json:"nativeType,omitempty"`

	// ─── Container shape (Slice 2.x — map/slice props) ───────────────────
	//
	// When the field's Go type is a composite (map[K]V or []T), the
	// parser breaks it apart into Container/KeyType/ValueType so the
	// renderer can build a row-based form without re-parsing the
	// `goType` string in the WASM client.
	//
	// Empty Container means "scalar" — the existing renderer path
	// (FieldText / FieldSelect) applies. Slice 2.1 only populates
	// these fields; Slice 2.2 will introduce FieldMap, and Slice 2.4
	// will add FieldSlice. Until those land, a non-empty Container is
	// silently ignored by the renderer (the row appears inert, same
	// as a non-native untagged discovery before this slice).

	// Container is the runtime shape of a non-scalar prop. One of:
	//
	//   ""      — scalar (default; the field is a native or native-
	//             like single value)
	//   "map"   — map[K]V; KeyType + ValueType are populated
	//   "slice" — []T;     ValueType is populated
	//
	// Other Go composite shapes (struct literal, channel, function,
	// array with fixed size) are deliberately not enumerated here:
	// they fall back to the empty Container so the renderer leaves
	// them inert. If a future slice supports them, add the discriminator
	// here and matching field types in overlay.FieldType.
	Container string `json:"container,omitempty"`

	// KeyType is the Go type name of the map key (e.g. "string", "int").
	// Empty unless Container=="map".
	KeyType string `json:"keyType,omitempty"`

	// ValueType is the Go type name of the map value or slice element
	// (e.g. "string", "byte", "float64"). Empty unless Container is
	// "map" or "slice".
	ValueType string `json:"valueType,omitempty"`

	// NativeKey is true when KeyType is something the renderer can
	// produce a usable input for. The Slice 2.2 renderer requires
	// NativeKey==true && KeyType=="string"; later slices may relax
	// this (allow int keys, etc.).
	NativeKey bool `json:"nativeKey,omitempty"`

	// NativeValue is true when ValueType is a native renderable type.
	// Renderers must check this before drawing an input — a value of
	// type *machine.I2C inside a map is technically syntactically
	// valid in Go but the form has no way to populate it. Tagged
	// non-native value types still surface as a PropDef (the
	// specialist's choice rules) but the renderer falls back to an
	// inert row when NativeValue is false.
	NativeValue bool `json:"nativeValue,omitempty"`
}

// ─── Help system — markdown files from GitHub repo ───────────────────────────
//
// When a specialist submits a GitHub release, the worker scans the repository
// root for markdown files following the naming convention:
//
//   readme.md            — device overview shown in the IDE menu (lang = "en")
//   readme.pt-br.md      — same, Portuguese
//   init.en.md           — single help tab for the Init method, English
//   init.1.en.md         — first tab for Init, English
//   init.2.en.md         — second tab for Init, English
//   run.pt-br.md         — single help tab for Run, Portuguese
//
// Rules:
//   - Method name matching is case-insensitive (init == Init).
//   - If init.en.md and init.1.en.md both exist, init.en.md becomes tab 0.
//   - Tab title = first "# Heading" in the file, truncated to HelpTabTitleMaxLen
//     chars respecting word boundaries (never cuts mid-word).
//   - GoDoc is always the last tab, generated from the Go source comment.
//   - Images (*.png, *.jpg, *.svg, *.gif, *.webp) are saved to disk and
//     ![](name.png) references are rewritten to the public URL.
//   - If only one language is available, no language selector is shown in the IDE.
//   - Language preference is stored per session; defaults to the user's
//     registration locale with English as ultimate fallback.

// HelpTab is a single documentation tab for a device method.
type HelpTab struct {
	// Order is the sort key. 0 means "no number in filename" (single tab).
	// Tabs are sorted ascending by Order before display.
	Order int `json:"order"`

	// Title is derived from the first "# Heading" in the markdown content,
	// truncated to blackbox.HelpTabTitleMaxLen characters at a word boundary.
	// Empty when the markdown has no "# heading" line — the WASM client
	// treats this as a sentinel and substitutes a localised "title not
	// found" message so the missing heading is surfaced to the author.
	Title string `json:"title"`

	// Content is the full markdown text with image paths rewritten to public URLs.
	Content string `json:"content"`
}

// MethodHelp groups the help tabs for one method, keyed by BCP-47 language code.
type MethodHelp struct {
	// Langs maps a lowercase BCP-47 language code (e.g. "en", "pt-br") to the
	// ordered list of help tabs for that language.
	Langs map[string][]HelpTab `json:"langs,omitempty"`
}

// DeviceHelp is the complete help payload for a device, embedded in BlackBoxDef.
type DeviceHelp struct {
	// Readme maps a lowercase BCP-47 language code to the ordered list of
	// readme tabs for that language. Each tab corresponds to one
	// `readme[.<N>].<lang>.md` file in the source tree (or a single
	// `readme.md` synthesised as `[{Order:0, Title:"", Content:...}]`).
	//
	// Tabs are sorted ascending by (Order, Title) before being placed in
	// the slice, mirroring MethodHelp.Langs. The renderer suppresses the
	// tab bar when len(tabs) == 1 so a device with a single readme keeps
	// the original "one continuous document" UX.
	//
	// Key "en" is the default / fallback language.
	Readme map[string][]HelpTab `json:"readme,omitempty"`

	// Methods maps a lowercase method name (e.g. "init", "run") to its
	// MethodHelp, which in turn maps language codes to ordered tab slices.
	Methods map[string]MethodHelp `json:"methods,omitempty"`
}

// ─── Manual pages ─────────────────────────────────────────────────────────────

// ManualShowIn describes which device block(s) a manual page appears in.
//
// With N methods, the value matches the method name (e.g. "run", "log", "step")
// or the special value "both" which means "appears in all blocks of this component".
// For the Init block the value "init" is still used.
type ManualShowIn string

const (
	ManualShowInit ManualShowIn = "init"
	ManualShowBoth ManualShowIn = "both"
)

// ManualPage is one documentation section extracted from a /* */ block.
type ManualPage struct {
	// Name is the page identifier, set with the manualName: tag.
	Name string `json:"name"`

	// Language is the BCP-47 language code (lowercase).
	Language string `json:"language"`

	// ShowIn controls which device block renders this page.
	// "init" → Init block only; "both" → all blocks; method name → that block.
	ShowIn ManualShowIn `json:"showIn"`

	// Content is the raw Markdown text extracted from the /* */ block.
	Content string `json:"content"`
}

// FileEntry is one authored file of a black-box source snapshot: a
// project-relative path and its verbatim content. Path rules (relative, no
// "..", extension whitelisted by language, unique case-insensitively) are
// enforced at the HTTP boundary before a snapshot is ever stored; by the
// time a def carries them they are trusted. The emitter still defends the
// two collisions only IT can know about (the generated header's name and
// the reserved main.c) at export-validation time.
//
// Português: Um arquivo autoral do snapshot: caminho relativo + conteúdo
// verbatim. Regras de caminho são impostas na borda HTTP; ao chegar no def,
// são confiáveis. O emitter ainda defende as duas colisões que só ele
// conhece (nome do header gerado e o main.c reservado) na validação do
// export.
type FileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// AssetEntry is one non-source file riding a black-box def: templates,
// images — cargo the device carries (unified asset model). Content is
// the STORED form: plain text, or base64 when Encoding says so (the
// def may be serialized as JSON — def_json — and raw binary breaks
// UTF-8; the same bridge the snapshot uses). Consumers that need real
// bytes (the ANSI C emitter) decode at their edge.
//
// Português: Um arquivo não-fonte viajando no def — carga do device.
// Content é a forma ARMAZENADA (texto, ou base64 quando Encoding diz;
// def_json é JSON e binário cru quebra UTF-8). Quem precisa de bytes
// reais decodifica na borda.
type AssetEntry struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"`
}
