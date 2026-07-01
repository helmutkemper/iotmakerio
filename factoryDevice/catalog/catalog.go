// /factoryDevice/catalog/catalog.go

// Package catalog declares the static metadata for every primitive
// device the IDE knows about. The data here drives two filtering
// decisions:
//
//  1. The hex menu in ui/mainMenu: when the workspace is set to
//     project language "c", devices whose SupportedLanguages do not
//     include "c" are hidden from the menu. Same applies in reverse
//     for "go".
//
//  2. (Future) The codegen pipeline: a sanity check that no device
//     reaches the backend that cannot be compiled by that backend.
//     This is belt-and-suspenders — the menu filter prevents the
//     user from adding them in the first place — but the second
//     check protects against scenes loaded from older saves or
//     from outside the IDE.
//
// Why a SUB-PACKAGE inside factoryDevice rather than living in
// factoryDevice itself:
//
// The parent factoryDevice package imports syscall/js because its
// CreateXxx methods manipulate DOM/canvas elements. That makes the
// whole package WASM-only (build constraints exclude every file on
// other architectures). A catalogue of static metadata, on the
// other hand, has no platform dependencies — it is just slices and
// strings. Splitting it into a sub-package lets:
//
//   - server-side code (codegen pipeline, validation handlers) import
//     it without dragging in syscall/js;
//   - go test ./factoryDevice/catalog/ run on linux/amd64 directly,
//     no make test-wasm or headless Chrome required;
//   - the WASM-only parent factoryDevice still import catalog freely.
//
// Why a separate file rather than a method on each device:
//
// Devices are created on the canvas by ~30 distinct CreateXxx methods
// on the DeviceFactory. Hanging metadata off each of those methods
// would scatter the truth across 30 places. A single catalogue file
// answers "what does device X support?" with one grep. When a new
// device type lands, the developer adds one line here — and is
// forced to think about the language story before merging.
//
// Why black-boxes are NOT in the catalogue:
//
// Black-box device types are dynamic — a maker creates them at runtime from a
// project whose source language can be Go, C99, and later more. Their Type
// strings look like "BlackBoxInit:APDS9960" or "BlackBoxRun:APDS9960", and
// there is no way to enumerate them at compile time. Their language is also
// per-device data the catalogue cannot know, so it lives ON THE DEFINITION:
// BlackBoxDefClient.ProgrammingLanguage, stamped by the server and checked by
// the menu builder via BlackBoxDefClient.SupportsProjectLanguage. The
// catalogue therefore covers PRIMITIVES only — primitive devices and
// black-boxes are conceptually distinct sources of metadata.
//
// Português:
//
//	Catálogo central de metadata de devices PRIMITIVOS, num sub-pacote para
//	evitar arrastar syscall/js (que o pacote pai factoryDevice precisa).
//	TypeName tem que bater com SceneJSON.devices[].type. Black-boxes NÃO
//	estão aqui — são dinâmicos e sua linguagem vive no def
//	(BlackBoxDefClient.ProgrammingLanguage), checada pelo menu builder.
package catalog

// =====================================================================
//  Language token constants
// =====================================================================

// Language tokens. Mirror stagefileclient.StageFileLanguage* and
// server/store/stage_files.go's StageFileLanguage* — kept in sync
// across the three locations to avoid a runtime import cycle
// (catalog cannot import stagefileclient without pulling in a chain
// that loops back through ui/mainMenu).
//
// "c" is the storage/wire token for C99; "go" is for Go. UI surfaces
// translate to display labels ("C99", "Go").
//
// Português: Tokens de linguagem. Espelham as constantes do
// stagefileclient e do store; mantidas em sincronia entre os três
// lugares por causa de ciclos de import.
const (
	LanguageGo = "go"
	LanguageC  = "c"
)

// =====================================================================
//  Catalogue
// =====================================================================

// DeviceMetadata is the static metadata for one primitive device type.
//
// TypeName is the value that appears in SceneJSON.devices[].type for
// instances of this device. It must match exactly the strings used by
// factoryDevice.DeviceFactory.CreateByType() and by the IR emitter's
// switch in server/codegen/ir/emit.go — drift between these three
// would silently break either menu filtering or code generation.
//
// SupportedLanguages enumerates the project languages this device
// is valid in. A device may be in a project's menu (and therefore
// placeable on the canvas) only if the project's language is in
// this slice.
//
// Português: Metadata estática por tipo de device. TypeName tem que
// bater com SceneJSON.devices[].type e com o switch do IR. Drift
// entre os três quebra silenciosamente filtro ou geração.
type DeviceMetadata struct {
	TypeName           string
	SupportedLanguages []string
}

// universalLanguages is the slice used by every device that works
// in both backends. Most primitives and all frontend-only display
// devices fall here. Sharing a single slice rather than allocating
// new ones per device keeps allocations down and makes the catalogue
// easier to scan visually.
//
// Português: Slice compartilhado para devices que funcionam nos dois
// backends. Sem realocar por device — mais legível e barato.
var universalLanguages = []string{LanguageGo, LanguageC}

// catalog is the canonical list of every primitive device type the
// IDE supports. Order is grouped by category for readability — the
// menu builds its own grouping and does not depend on this order.
//
// To add a new primitive device:
//
//  1. Add a CreateXxx() method to factoryDevice.DeviceFactory.
//  2. Add a case to the switch in CreateByType() that calls it.
//  3. Add an entry here with the correct SupportedLanguages.
//  4. If the device generates code, add the IR emission case in
//     server/codegen/ir/emit.go.
//
// Forgetting step 3 means the device shows up in every project's
// menu (because LookupSupportedLanguages returns nil → SupportsLanguage
// returns false → it gets hidden in EVERY language). That is the
// safest failure mode — a missing entry produces a missing device,
// not a broken project — but the developer notices immediately.
//
// Português: Catálogo canônico. Para adicionar um device novo, são
// 4 passos. Esquecer o passo 3 (esta lista) significa device some
// do menu em qualquer linguagem — falha barulhenta e segura.
var catalog = []DeviceMetadata{

	// ── Arithmetic ──────────────────────────────────────────────────────
	// Both backends emit the corresponding OpAdd/Sub/Mul/Div sequences.
	{TypeName: "StatementAdd", SupportedLanguages: universalLanguages},
	{TypeName: "StatementSub", SupportedLanguages: universalLanguages},
	{TypeName: "StatementMul", SupportedLanguages: universalLanguages},
	{TypeName: "StatementDiv", SupportedLanguages: universalLanguages},

	// ── Comparisons ─────────────────────────────────────────────────────
	// Both backends emit OpCmp* with the C operator (==, !=, <, <=, >, >=).
	{TypeName: "StatementEqualTo", SupportedLanguages: universalLanguages},
	{TypeName: "StatementNotEqualTo", SupportedLanguages: universalLanguages},
	{TypeName: "StatementLessThan", SupportedLanguages: universalLanguages},
	{TypeName: "StatementLessThanOrEqualTo", SupportedLanguages: universalLanguages},
	{TypeName: "StatementGreaterThan", SupportedLanguages: universalLanguages},
	{TypeName: "StatementGreaterThanOrEqualTo", SupportedLanguages: universalLanguages},

	// ── Control flow ────────────────────────────────────────────────────
	// Loop → while(1)+break in C, for{} in Go.
	// LoopDuration → loop body ends with iotmaker_sleep_ns / time.Sleep.
	// IfElse → 4-form if/else with hasTrue/hasFalse metadata.
	// Case → N-way switch (bool selector lowers to if/else).
	{TypeName: "StatementLoop", SupportedLanguages: universalLanguages},
	{TypeName: "StatementLoopDuration", SupportedLanguages: universalLanguages},
	{TypeName: "StatementCase", SupportedLanguages: universalLanguages},

	// ── Constants ───────────────────────────────────────────────────────
	// ConstInt, Bool, ConstFloat, ConstString, ConstDuration — straight
	// OpConst emission with the appropriate type token.
	{TypeName: "StatementConstInt", SupportedLanguages: universalLanguages},
	{TypeName: "StatementBool", SupportedLanguages: universalLanguages},
	{TypeName: "StatementConstFloat", SupportedLanguages: universalLanguages},
	{TypeName: "StatementConstString", SupportedLanguages: universalLanguages},
	{TypeName: "StatementConstDuration", SupportedLanguages: universalLanguages},
	// ConstArrayInt / Float / String — fixed-size constant collections
	// (e.g. []int{1, 2, 3}), one device per element type, mirroring the
	// scalar const family. All three emit OpConstArray: a Go slice literal
	// / a C fixed array + explicit `_len` length companion. Both backends
	// covered (docs/claude_const_array_plan.md), hence universal.
	{TypeName: "StatementConstArrayInt", SupportedLanguages: universalLanguages},
	{TypeName: "StatementConstArrayFloat", SupportedLanguages: universalLanguages},
	{TypeName: "StatementConstArrayString", SupportedLanguages: universalLanguages},

	// ── Display / output devices ────────────────────────────────────────
	// These are frontend-only widgets (gauge, LED, charts...). They
	// occupy a place on the canvas and may be wired to backend nodes,
	// but they do NOT emit any backend code on their own — the C and
	// Go backends both treat OpOutput as a no-op. As a result they
	// are "universal" in the trivial sense: every project language
	// can place them, because every project language ignores them
	// at codegen time.
	//
	// Português: Widgets visuais. Não emitem código. Universais por
	// trivialidade — toda linguagem os ignora no codegen.
	{TypeName: "StatementGauge", SupportedLanguages: universalLanguages},
	{TypeName: "StatementLED", SupportedLanguages: universalLanguages},
	{TypeName: "StatementBarGraph", SupportedLanguages: universalLanguages},
	{TypeName: "StatementTextDisplay", SupportedLanguages: universalLanguages},
	{TypeName: "StatementSevenSeg", SupportedLanguages: universalLanguages},
	{TypeName: "StatementChart", SupportedLanguages: universalLanguages},
	{TypeName: "StatementChartPro", SupportedLanguages: universalLanguages},
	{TypeName: "StatementPieChart", SupportedLanguages: universalLanguages},
	{TypeName: "StatementBackgroundImage", SupportedLanguages: universalLanguages},

	// ── Input / interactive devices ─────────────────────────────────────
	// Button and Knob feed user-driven values into the backend scene
	// at runtime. The wiring side is handled by the backend (which
	// reads the produced register); the widget itself is frontend.
	// Same universal-by-triviality story as display devices.
	{TypeName: "StatementButton", SupportedLanguages: universalLanguages},
	{TypeName: "StatementKnob", SupportedLanguages: universalLanguages},

	// ── Communication status ────────────────────────────────────────────
	// CommStatus watches a connection and surfaces green/yellow/red.
	// Pure frontend; universal.
	{TypeName: "StatementCommStatus", SupportedLanguages: universalLanguages},
}

// =====================================================================
//  Lookup API
// =====================================================================

// LookupSupportedLanguages returns the languages a PRIMITIVE device type
// supports. Two branches:
//
//   - Known primitive (matches a catalogue entry by TypeName): the entry's
//     SupportedLanguages, returned directly. The slice is shared
//     (universalLanguages) for most devices — callers must not mutate it.
//
//   - Anything else (unknown type, or a black-box "BlackBox…" type): nil.
//     SupportsLanguage interprets nil as "no support in any language".
//     Black-boxes are intentionally not handled here — their language is
//     per-device data on BlackBoxDefClient.ProgrammingLanguage, checked by
//     the menu builder via SupportsProjectLanguage. Passing a black-box type
//     here returns nil by design; the menu never does.
//
// Português: Resolve as linguagens de um device PRIMITIVO. Type conhecido →
// entry do catálogo; qualquer outro (inclusive "BlackBox…") → nil. Black-box
// tem a linguagem no def (ProgrammingLanguage), não aqui.
func LookupSupportedLanguages(deviceType string) []string {
	for i := range catalog {
		if catalog[i].TypeName == deviceType {
			return catalog[i].SupportedLanguages
		}
	}
	return nil
}

// SupportsLanguage is a convenience predicate over LookupSupportedLanguages.
// Returns true iff the device type supports the given project language.
// An unknown type returns false — callers (mostly the menu builder)
// hide the device in that case.
//
// The function is allocation-free and runs in O(N+M) where N is the
// catalogue size (~30) and M is the language list size (at most 2
// today). Both constants are small enough that the menu can call this
// for every device on every render without measurable overhead.
//
// Português: Predicado de conveniência. true se o device suporta a
// linguagem do projeto, false caso contrário (inclusive para types
// desconhecidos). Sem alocação, O(N+M) com N e M pequenos.
func SupportsLanguage(deviceType, language string) bool {
	langs := LookupSupportedLanguages(deviceType)
	if langs == nil {
		return false
	}
	for _, l := range langs {
		if l == language {
			return true
		}
	}
	return false
}

// AllPrimitiveTypes returns every primitive device type in the
// catalogue. Useful for tests that want to iterate without depending
// on the internal catalog slice (which is unexported on purpose —
// the package controls the canonical list and consumers should ask
// via the lookup API).
//
// The returned slice is a fresh copy; callers can mutate it freely.
//
// Português: Lista todos os types primitivos. Para testes que
// iteram sem depender da slice interna. Retorna cópia.
func AllPrimitiveTypes() []string {
	out := make([]string, 0, len(catalog))
	for i := range catalog {
		out = append(out, catalog[i].TypeName)
	}
	return out
}
