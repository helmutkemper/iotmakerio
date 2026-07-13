// ui/mainMenu/menuBuilder.go — Defines the main hex menu structure and hierarchy.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	MenuBuilder is responsible for defining the menu layout, labels, icons,
//	and submenu structure. It references the DeviceFactory only for OnClick
//	callbacks. This separation ensures the factory handles only device
//	creation while the menu structure is independently maintained.
//
//	All labels use translate.T(id, fallback) for i18n support. The fallback
//	is the English label, used only when the translation server is unreachable.
//
//	Menu hierarchy (Build output):
//	  ├─ Math      (hardcoded) → Add, Sub, Mul, Div
//	  ├─ Logic     (hardcoded) → compare operators
//	  ├─ Loop      (hardcoded) → Loop
//	  ├─ Const     (hardcoded) → Int, Bool, Float, String
//	  ├─ Display   (hardcoded) → Gauge, LED, Bar, Text, Button, 7-Seg, Knob, Chart, Background
//	  ├─ Export    (hardcoded) → JSON, Go Code
//	  ├─ Settings  (hardcoded) → action
//	  ├─ <section> (tree)    → category → subcategory → device  [0..N]
//	  ├─ <category>(tree)   → subcategory → device              [0..N]
//	  └─ <device>  (tree)   → Init, Methods                     [0..N]
//
//	When the server tree is available, the entire menu is driven by the database.
//	Dynamic categories, branded sections, and individual devices are all tree nodes.
//	The WASM matches device nodes against loaded BlackBoxDefClient entries by
//	device_struct_name to build Init/method submenus.
//
//	When the server tree is unavailable, the legacy hardcoded layout is used as
//	fallback. This only provides system items and category-grouped devices; branded
//	sections are not available in fallback mode since they require server data.
//
//	Export design — single exit point:
//	  The "Export → Go Code" item calls exportFn, which the Workspace
//	  implements as export(). That function inspects the scene and decides
//	  whether to generate a template ZIP (wire-resolved config) or to run
//	  the regular SSE codegen pipeline for custom devices.
//	  There is NO "Generate ZIP" button in the Templates submenu.
//
// Português:
//
//	MenuBuilder define layout, labels, ícones e submenus do menu principal.
//	Referencia o DeviceFactory apenas para callbacks OnClick.
//	Quando o server tree está disponível, toda a estrutura vem do banco de dados.
//	No fallback (server offline), usa o layout hardcoded legado.
package mainMenu

import (
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/hexMenu"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesMainMenu"
	"github.com/helmutkemper/iotmakerio/templateclient"
	"github.com/helmutkemper/iotmakerio/translate"
)

// DeviceCreator defines the interface that MenuBuilder needs from the factory.
// This avoids a direct import of factoryDevice, preventing circular dependencies.
type DeviceCreator interface {
	SafeRun(name string, fn func())
	CreateAdd()
	CreateSub()
	CreateMul()
	CreateDiv()
	CreateLoop()
	CreateLoopDuration()
	CreateCase()
	CreateConstInt()
	CreateDataFile()
	CreateDataText()
	CreateBool()
	CreateConstFloat()
	CreateConstString()
	CreateConstDuration()
	CreateConstArrayInt()
	CreateConstArrayFloat()
	CreateConstArrayString()
	CreateIndexInt()
	CreateIndexFloat()
	CreateIndexString()
	CreateGetVarInt()
	CreateGetVarFloat()
	CreateSetVarInt()
	CreateSetVarFloat()
	CreateGetVarString()
	CreateSetVarString()
	// The six debug-print sinks (Debug group, SysDebug). Part of the same
	// contract as every Create* above: the concrete implementation lives in
	// factoryDevice.DeviceFactory; the SysPrint* factories in this file call
	// them through this interface.
	// Português: Os seis sinks de print (grupo Debug, SysDebug). Mesmo
	// contrato dos demais Create*: a implementação concreta vive em
	// factoryDevice.DeviceFactory; as factories SysPrint* deste arquivo
	// chamam por esta interface.
	CreatePrintInt()
	CreatePrintFloat()
	CreatePrintString()
	CreatePrintBool()
	CreatePrintByte()
	CreatePrintByteArray()
	CreateGauge()
	CreateLED()
	CreateBarGraph()
	CreateTextDisplay()
	CreateButton()
	CreateSevenSeg()
	CreateKnob()
	CreateChart()
	CreateChartPro()
	CreatePieChart()
	CreateBackgroundImage()
	CreateEqualTo()
	CreateNotEqualTo()
	CreateLessThan()
	CreateLessThanOrEqualTo()
	CreateGreaterThan()
	CreateGreaterThanOrEqualTo()
	// CreateBlackBoxInit places the Init() device for the given black-box.
	CreateBlackBoxInit(def *blackbox.BlackBoxDefClient)
	// CreateBlackBoxMethod places a named method device for the given black-box.
	CreateBlackBoxMethod(def *blackbox.BlackBoxDefClient, methodName string)
	// CreateBlackBoxFunction places a C99 device-function block (decision b)
	// for the given black-box. Each block is independent (no shared instance).
	CreateBlackBoxFunction(def *blackbox.BlackBoxDefClient, fnName string)
	// CreateBlackBoxCallbackRef places the dedicated callback-reference (ƒ)
	// device for a C99 callback-handler function (the wire-ƒ): no inputs, one
	// `callback` output typed as the handler's function-pointer type, wired by
	// address into a consumer's callback input. A SEPARATE device from the
	// callable (the duality). See docs/c99_ide_integration.md.
	CreateBlackBoxCallbackRef(def *blackbox.BlackBoxDefClient, fnName string)
	CreateCommStatus()
}

// fnMenuVariants reports which device entries a parsed C99 function offers in
// the menu — the callback duality, mirroring the Preview's _fnPreviewVariants:
//   - not a handler        → callable only
//   - handler, mode "both" → callable + reference (ƒ)
//   - handler, mode "ref"  → reference (ƒ) only
//
// A handler is a function with HandlerType set; CallbackMode defaults to "both"
// when empty (a bare `callback:T.`). "ref" is the value the parser writes for
// ref-only mode. See docs/c99_ide_integration.md.
func fnMenuVariants(fn blackbox.FunctionDefClient) (callable, ref bool) {
	if fn.HandlerType == "" {
		return true, false
	}
	if fn.CallbackMode == "ref" {
		return false, true
	}
	return true, true
}

// MenuBuilder builds the main IDE menu hierarchy.
type MenuBuilder struct {
	factory      DeviceCreator
	exportJSONFn func() // Export → JSON (scene serializer download)
	exportFn     func() // Export → Go Code (single unified export exit point)
	filesFn      func() // Export → Files (opens stage file manager overlay)
	imageFn      func() // Export → Image (PNG screenshot with embedded stage JSON)
	liveConfigFn func() // Settings → Live Config (opens live communication dialog)

	// language is the fixed project language ("c" or "go") that the
	// workspace was created with. Read by buildDeviceNode to filter
	// out black-boxes that have no implementation for this language
	// — Phase 1: any black-box in a C99 project is hidden, because
	// black-box code today is Go-only. Set once in NewMenuBuilder and
	// never mutated afterwards (the project language is irreversible
	// by design — see /server/store/stage_files.go for the rationale).
	//
	// Primitives are NOT filtered against this field today because
	// the catalog declares every primitive as universal (Go + C99).
	// When the first language-restricted primitive lands, the place
	// to add the filter is registerFactories / the hardcoded
	// MenuItem builders — not here.
	//
	// Português: Linguagem fixa do projeto, usada por buildDeviceNode
	// pra esconder black-boxes que não têm implementação. Set uma vez
	// no NewMenuBuilder; nunca muda (linguagem é irreversível).
	// Primitives não são filtrados hoje porque o catálogo os declara
	// universais; quando o primeiro primitive tiver restrição, o
	// filtro vai em registerFactories, não aqui.
	language string

	// blackBoxDefs holds ALL device definitions the IDE can instantiate on
	// the canvas: the authenticated user's own devices (from /api/v1/blackbox,
	// stamped Origin="own" by the server) plus any devices embedded in
	// admin-curated sections of the menu tree (stamped Origin="curated" on
	// the client by stageWorkspace/workspace.go::extractEmbeddedDefs).
	//
	// Used by: factory creation paths, buildDeviceNode (tree resolution),
	// hardwareSubmenu (legacy fallback), buildCategoryMenuItems (legacy).
	//
	// Anywhere that asks "can this device be placed on the canvas?" consults
	// this list. It is NOT used to decide what appears under "My Items" —
	// see ownBlackBoxIndex for that.
	blackBoxDefs []*blackbox.BlackBoxDefClient

	// ownBlackBoxIndex is a derived view of blackBoxDefs: pointers to every
	// entry with IsOwn == true. Populated inside SetBlackBoxDefs, never
	// assigned from outside. The index exists for two reasons:
	//
	//  1. Correctness by construction. There is no public setter that can be
	//     called out of order to mislabel the "mine?" collection — the index
	//     is always derived from the current definitions list.
	//
	//  2. Performance. buildMyItems iterates this index directly in
	//     O(owned items) instead of scanning the full catalog every call.
	//     Matters when the catalog grows past a few thousand entries.
	//
	// For a brand-new user with zero owned devices this slice is nil; the
	// pointer equality to nil is also the guard that keeps "My Items"
	// hidden from the menu.
	ownBlackBoxIndex []*blackbox.BlackBoxDefClient

	// templateDefs holds ALL template definitions visible to the caller:
	// own templates plus public templates from other specialists, as
	// returned by the server's /api/v1/templates endpoint (ordered own
	// first, then public, deduplicated by id).
	//
	// Used by: buildTemplateNode (tree resolution) and the legacy template
	// listing. Not used directly for "My Items" — see ownTemplateIndex.
	templateDefs []*templateclient.TemplateFullClient

	// ownTemplateIndex mirrors ownBlackBoxIndex for templates: a derived
	// slice of pointers to every TemplateFullClient with IsOwn == true.
	// Populated inside SetTemplateDefs, never assigned from outside.
	//
	// See ownBlackBoxIndex for the rationale.
	ownTemplateIndex []*templateclient.TemplateFullClient

	// railTree holds the menu tree fetched from the server via LoadMenuTree().
	// When non-nil, Build() uses this tree to determine the order, visibility,
	// and labels of system items instead of the hardcoded layout.
	// Set via SetRailTree() before calling Build().
	railTree []RailSlot

	// factories maps slot_id → factory function for all system menu items.
	// Each factory creates a hexMenu.MenuItem with the correct OnClick callback
	// and default icon. The label is passed in from the server tree.
	// Populated by registerFactories() called from NewMenuBuilder.
	factories map[string]func(string) hexMenu.MenuItem
}

// NewMenuBuilder creates a MenuBuilder for the given factory.
//
// exportJSONFn — called when the maker clicks "Export → JSON". Downloads the
//
//	raw scene JSON (useful for debugging or re-loading the scene).
//
// exportFn — called when the maker clicks "Export → Go Code". This is the
//
//	single exit point for all project generation. The Workspace.export()
//	implementation detects template devices on the canvas and either:
//	  • Resolves wire+prop values and downloads a configured project ZIP, or
//	  • Runs the SSE codegen pipeline and displays the generated Go source.
//
// filesFn — called when the maker clicks "Export → Files". Opens the stage
//
//	file manager overlay for saving, loading, and organising stage files.
//
// language — the fixed project language ("c" or "go") this workspace was
//
//	created with. Drives the device filter in buildDeviceNode: black-boxes
//	without an implementation for this language are silently hidden from
//	the menu. Pass the empty string and the filter rejects every black-box
//	(catalog.SupportsLanguage returns false for "") — practically equivalent
//	to "do not show black-boxes at all", which is the safest fail mode.
func NewMenuBuilder(factory DeviceCreator, exportJSONFn func(), exportFn func(), filesFn func(), language string) *MenuBuilder {
	b := &MenuBuilder{
		factory:      factory,
		exportJSONFn: exportJSONFn,
		exportFn:     exportFn,
		filesFn:      filesFn,
		language:     language,
		factories:    make(map[string]func(string) hexMenu.MenuItem),
	}
	b.registerFactories()
	return b
}

// SetBlackBoxDefs replaces the full catalog of black-box definitions.
//
// The caller passes the complete list — own devices AND curated devices
// merged — with every entry already carrying an Origin / IsOwn marker
// (the server stamps "own", stageWorkspace/workspace.go stamps "curated"
// for items that arrived through the menu tree).
//
// This method rebuilds the derived ownBlackBoxIndex in a single pass. The
// index IS NOT exposed by a public setter — there is no way to feed it
// inconsistent data from outside, and no way to forget to update it.
//
// Passing nil or an empty slice resets both the catalog and the index,
// which is the correct behaviour on logout before a subsequent login
// with a different user.
func (b *MenuBuilder) SetBlackBoxDefs(defs []*blackbox.BlackBoxDefClient) {
	b.blackBoxDefs = defs
	b.ownBlackBoxIndex = buildOwnBlackBoxIndex(defs)
}

// buildOwnBlackBoxIndex returns a derived slice of pointers to every entry
// in defs whose IsOwn flag is true. Returns nil (not an empty slice) when
// no entries match, so "My Items" visibility checks can use a simple
// len() == 0 test without distinguishing nil from empty.
//
// The index stores pointers, not copies. Mutations to the underlying
// definition objects are visible here automatically — correct by
// construction, since every caller of SetBlackBoxDefs hands over
// ownership of the slice.
func buildOwnBlackBoxIndex(defs []*blackbox.BlackBoxDefClient) []*blackbox.BlackBoxDefClient {
	if len(defs) == 0 {
		return nil
	}
	// Upper-bound capacity at len(defs) to avoid reallocation when the
	// caller is an owner of most of their catalog (typical case).
	idx := make([]*blackbox.BlackBoxDefClient, 0, len(defs))
	for _, d := range defs {
		if d != nil && d.IsOwn {
			idx = append(idx, d)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	return idx
}

// SetImageFn sets the callback for Export → Image (PNG+Stage).
// Called by the workspace after Init to wire up the image export handler.
func (b *MenuBuilder) SetImageFn(fn func()) {
	b.imageFn = fn
}

// SetLiveConfigFn sets the callback for Settings → Live Config dialog.
// Called by the workspace after Init to wire up the live communication settings.
//
// Português: Define o callback para abrir o diálogo de configuração Live.
func (b *MenuBuilder) SetLiveConfigFn(fn func()) {
	b.liveConfigFn = fn
}

// SetTemplateDefs replaces the full catalog of template definitions.
//
// Just like SetBlackBoxDefs, the caller hands over a complete list with
// every TemplateMetaClient already carrying an Origin / IsOwn marker set
// by the server. This method rebuilds the derived ownTemplateIndex.
//
// Design note: there is intentionally NO "Generate ZIP" action in the
// submenu. The single export path is "Export → Go Code" (exportFn). At
// that point, the Workspace reads the current canvas state (wires +
// props) via sceneresolver and produces the correctly configured output.
func (b *MenuBuilder) SetTemplateDefs(defs []*templateclient.TemplateFullClient) {
	b.templateDefs = defs
	b.ownTemplateIndex = buildOwnTemplateIndex(defs)
}

// buildOwnTemplateIndex mirrors buildOwnBlackBoxIndex for templates.
// See that function's doc for the rationale.
func buildOwnTemplateIndex(defs []*templateclient.TemplateFullClient) []*templateclient.TemplateFullClient {
	if len(defs) == 0 {
		return nil
	}
	idx := make([]*templateclient.TemplateFullClient, 0, len(defs))
	for _, t := range defs {
		if t != nil && t.Meta.IsOwn {
			idx = append(idx, t)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	return idx
}

// SetRailTree sets the menu tree fetched from the server.
// When set, Build() uses this tree to control order, visibility, and labels
// of system items. When nil, Build() falls back to the legacy hardcoded layout.
func (b *MenuBuilder) SetRailTree(tree []RailSlot) {
	b.railTree = tree
	if len(tree) > 0 {
		log.Printf("[MenuBuilder] SetRailTree: %d root item(s) from server", len(tree))
	}
}

// categoryIcon returns the default icon for a category. The tree-based flow
// resolves icons server-side; this is only used by the legacy fallback path.
func (b *MenuBuilder) categoryIcon(catName string) (string, string) {
	return rulesIcon.KFAGear, "0 0 512 512"
}

// subcategoryIcon returns the default icon for a subcategory.
func (b *MenuBuilder) subcategoryIcon(catName, subName string) (string, string) {
	return rulesIcon.KFAGear, "0 0 512 512"
}

// Build returns the top-level hex menu items for the main IDE menu.
//
// When railTree is set (fetched from the server), Build uses it to determine
// the order, visibility, and labels of system items. Dynamic categories and
// branded sections are still appended using the existing code path since they
// require BlackBoxDefClient data not present in the tree.
//
// When railTree is nil (server unreachable or not yet migrated), Build falls
// back to the legacy hardcoded layout so the IDE always works.
func (b *MenuBuilder) Build() []hexMenu.MenuItem {
	if len(b.railTree) > 0 {
		items := b.buildFromTree()
		if len(items) > 0 {
			return items
		}
		log.Printf("[MenuBuilder] buildFromTree returned empty, falling back to legacy")
	}
	return b.buildLegacy()
}

// buildFromTree walks the server-provided menu tree and builds MenuItem items.
// The tree now contains ALL items: system, sections, categories, devices, and
// templates. The WASM uses device_struct_name to match devices against loaded
// bbDefs for generating Init/method submenus.
//
// Exit is treated specially: if it's in the tree, it's placed at the very end
// regardless of its position in the tree, to prevent the maker from getting
// stuck in the IDE.
func (b *MenuBuilder) buildFromTree() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	items := make([]hexMenu.MenuItem, 0, len(b.railTree)+8)

	var exitSlot *RailSlot
	var myItemsSlot *RailSlot

	// Walk root-level tree nodes.
	for i := range b.railTree {
		slot := &b.railTree[i]

		// Defer Exit to the very end.
		if slot.SlotID == "SysExit" {
			exitSlot = slot
			continue
		}

		// Defer SysMyItems — always placed just before Exit.
		if slot.SlotID == "SysMyItems" {
			myItemsSlot = slot
			continue
		}

		if item := b.buildNodeFromTree(slot, styles); item != nil {
			items = append(items, *item)
		}
	}

	// ── My Items — just before Exit ─────────────────────────────────────
	if myItemsSlot != nil {
		if myItem := b.buildMyItems(myItemsSlot, styles); myItem != nil {
			items = append(items, *myItem)
		}
	}

	// ── Exit — always last ─────────────────────────────────────────────────
	if exitSlot != nil {
		if item := b.buildNodeFromTree(exitSlot, styles); item != nil {
			items = append(items, *item)
		}
	}

	log.Printf("[MenuBuilder] buildFromTree: %d total item(s), %d devices, %d templates",
		len(items), len(b.blackBoxDefs), len(b.templateDefs))
	return items
}

// buildMyItems builds the "My Items" submenu from the user's own devices
// and templates. Returns nil if the user has no items (the menu entry
// is hidden entirely).
//
// IMPORTANT: this function consults ownBlackBoxIndex / ownTemplateIndex,
// NOT the full blackBoxDefs / templateDefs. The full lists include devices
// embedded in admin-curated sections and public templates from other
// specialists — those must NEVER appear under "My Items". The indexes are
// maintained automatically by SetBlackBoxDefs / SetTemplateDefs by
// filtering for IsOwn == true.
//
// For a new user with no own content, both indexes are nil and we return
// nil so the menu entry is entirely hidden. This is the same defence
// against the bug originally documented in CLAUDE_KNOWN_ISSUES.md §2.2.
func (b *MenuBuilder) buildMyItems(slot *RailSlot, styles [5]hexMenu.IconStyle) *hexMenu.MenuItem {
	if len(b.ownBlackBoxIndex) == 0 && len(b.ownTemplateIndex) == 0 {
		return nil
	}

	// Resolve the display label from the tree node.
	label := slot.Label
	if !slot.HasCustomLabel && slot.LabelKey != "" {
		label = translate.T(slot.LabelKey, slot.LabelFallback)
	}
	if label == "" {
		label = "My Items"
	}

	// Resolve icon from the tree node or use a default.
	iconPath := rulesIcon.KFAGear
	iconVB := "0 0 512 512"
	if slot.IconFA != "" {
		ic := rulesIcon.IconByNameOrDefault(slot.IconFA, "box-open")
		iconPath = ic.Path
		iconVB = ic.ViewBox
	} else {
		ic := rulesIcon.IconByNameOrDefault("box-open", "box-open")
		iconPath = ic.Path
		iconVB = ic.ViewBox
	}

	// ── Group devices by MenuCategory → MenuSubcategory ──────────────────
	// Each device's bbDef carries MenuCategory and MenuSubcategory strings
	// set by the specialist when submitting the device.
	type subGroup struct {
		name  string
		items []hexMenu.MenuItem
	}
	type catGroup struct {
		name     string
		subcats  map[string]*subGroup // subcat name → items
		subOrder []string             // insertion order
		direct   []hexMenu.MenuItem   // devices without subcategory
	}

	cats := map[string]*catGroup{}
	var catOrder []string

	for _, def := range b.ownBlackBoxIndex {
		d := def
		// Language filter: hide an own black-box whose source language is
		// not this project's. This loop previously had no filter — the gap
		// that let Go devices show in a C99 project (and vice versa). The
		// hardware/curated path (buildDeviceNode) filters too; both now use
		// the def's own ProgrammingLanguage, not the catalogue.
		if !d.SupportsProjectLanguage(b.language) {
			continue
		}
		submenu := b.blackBoxFuncSubmenu(d)
		for i := range submenu {
			submenu[i].Styles = styles
		}
		devIcon := rulesIcon.IconByNameOrDefault(d.EffectiveStructIcon(), "gear")
		devItem := hexMenu.MenuItem{
			ID:              "bb_" + d.Name,
			Label:           d.EffectiveStructLabel(),
			FontAwesomePath: devIcon.Path,
			ViewBox:         devIcon.ViewBox,
			Type:            hexMenu.ItemSubmenu,
			Submenu:         submenu,
			Styles:          styles,
		}

		catName := d.MenuCategory
		if catName == "" {
			catName = "Other"
		}

		if cats[catName] == nil {
			cats[catName] = &catGroup{name: catName, subcats: map[string]*subGroup{}}
			catOrder = append(catOrder, catName)
		}

		subName := d.MenuSubcategory
		if subName == "" {
			// No subcategory — device goes directly under category.
			cats[catName].direct = append(cats[catName].direct, devItem)
		} else {
			if cats[catName].subcats[subName] == nil {
				cats[catName].subcats[subName] = &subGroup{name: subName}
				cats[catName].subOrder = append(cats[catName].subOrder, subName)
			}
			cats[catName].subcats[subName].items = append(cats[catName].subcats[subName].items, devItem)
		}
	}

	// ── Build the category/subcategory hierarchy ─────────────────────────
	topChildren := make([]hexMenu.MenuItem, 0, len(cats)+len(b.ownTemplateIndex)+4)
	topBack := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
	topBack.Styles = styles
	topChildren = append(topChildren, topBack)

	for _, catName := range catOrder {
		cg := cats[catName]
		catLabel := translate.T("menuCat_"+catName, catName)
		catIcon := rulesIcon.IconByNameOrDefault("microchip", "microchip")

		// Build the category's children: Back + subcategory submenus + direct devices.
		catChildren := make([]hexMenu.MenuItem, 0, len(cg.subcats)+len(cg.direct)+2)
		catBack := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
		catBack.Styles = styles
		catChildren = append(catChildren, catBack)

		// Subcategory submenus.
		for _, subName := range cg.subOrder {
			sg := cg.subcats[subName]
			subLabel := translate.T("menuSubCat_"+subName, subName)
			subIcon := rulesIcon.IconByNameOrDefault("cubes", "cubes")

			subItems := make([]hexMenu.MenuItem, 0, len(sg.items)+1)
			subBack := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
			subBack.Styles = styles
			subItems = append(subItems, subBack)
			subItems = append(subItems, sg.items...)

			catChildren = append(catChildren, hexMenu.MenuItem{
				ID:              "myitems_sub_" + catName + "_" + subName,
				Label:           subLabel,
				FontAwesomePath: subIcon.Path,
				ViewBox:         subIcon.ViewBox,
				Type:            hexMenu.ItemSubmenu,
				Submenu:         rulesMainMenu.ApplyRadialLayout(subItems, styles),
				Styles:          styles,
			})
		}

		// Direct devices (no subcategory).
		catChildren = append(catChildren, cg.direct...)

		// If the category has only one device and no subcategories,
		// skip the category wrapper and add the device directly to top level.
		totalDevices := len(cg.direct)
		for _, subName := range cg.subOrder {
			totalDevices += len(cg.subcats[subName].items)
		}
		if totalDevices == 1 && len(cg.subOrder) == 0 && len(catOrder) == 1 {
			// Single device, single category — no need for nesting.
			topChildren = append(topChildren, cg.direct...)
		} else {
			topChildren = append(topChildren, hexMenu.MenuItem{
				ID:              "myitems_cat_" + catName,
				Label:           catLabel,
				FontAwesomePath: catIcon.Path,
				ViewBox:         catIcon.ViewBox,
				Type:            hexMenu.ItemSubmenu,
				Submenu:         rulesMainMenu.ApplyRadialLayout(catChildren, styles),
				Styles:          styles,
			})
		}
	}

	// ── Templates ────────────────────────────────────────────────────────
	// Only templates owned by the authenticated user appear here. The full
	// b.templateDefs (own + public) is used elsewhere for tree resolution.
	if len(b.ownTemplateIndex) > 0 {
		tmplChildren := make([]hexMenu.MenuItem, 0, len(b.ownTemplateIndex)+1)
		tmplBack := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
		tmplBack.Styles = styles
		tmplChildren = append(tmplChildren, tmplBack)

		for _, tmpl := range b.ownTemplateIndex {
			t := tmpl
			submenu := b.templateDeviceSubmenu(t)
			for i := range submenu {
				submenu[i].Styles = styles
			}
			tmplIcon := rulesIcon.IconByNameOrDefault("box-open", "box-open")
			tmplChildren = append(tmplChildren, hexMenu.MenuItem{
				ID:              "tmpl_" + t.Meta.ID,
				Label:           t.Meta.Name,
				FontAwesomePath: tmplIcon.Path,
				ViewBox:         tmplIcon.ViewBox,
				Type:            hexMenu.ItemSubmenu,
				Submenu:         submenu,
				Styles:          styles,
			})
		}

		tmplGroupIcon := rulesIcon.IconByNameOrDefault("box-open", "box-open")
		topChildren = append(topChildren, hexMenu.MenuItem{
			ID:              "myitems_templates",
			Label:           translate.T("menuMainTemplates", "Templates"),
			FontAwesomePath: tmplGroupIcon.Path,
			ViewBox:         tmplGroupIcon.ViewBox,
			Type:            hexMenu.ItemSubmenu,
			Submenu:         rulesMainMenu.ApplyRadialLayout(tmplChildren, styles),
			Styles:          styles,
		})
	}

	return &hexMenu.MenuItem{
		ID:              slot.SlotID,
		Label:           label,
		FontAwesomePath: iconPath,
		ViewBox:         iconVB,
		Type:            hexMenu.ItemSubmenu,
		Submenu:         rulesMainMenu.ApplyRadialLayout(topChildren, styles),
		Styles:          styles,
	}
}

// buildNodeFromTree converts one RailSlot into a hexMenu.MenuItem.
// Handles all slot types: system, section, device, category, template.
//
// For system items: uses the registered factory for OnClick + default icon.
// For device items: matches bbDef by DeviceStructName, builds Init/method submenu.
// For section items: creates brand-colored submenu, recurses into children.
// For category items: creates submenu, recurses into children.
// For template items: matches templateDef, builds device submenu.
//
// The styles parameter carries the parent's styles. Section nodes override
// this with brand colors for their children.
func (b *MenuBuilder) buildNodeFromTree(slot *RailSlot, parentStyles [5]hexMenu.IconStyle) *hexMenu.MenuItem {
	// ── Resolve the display label ────────────────────────────────────────
	// If the admin set a custom label (via menu_layout or menu_layout_labels),
	// has_custom_label is true → use the server-resolved label directly.
	// Otherwise, use translate.T(label_key, label_fallback) for i18n.
	label := slot.Label
	if !slot.HasCustomLabel && slot.LabelKey != "" {
		label = translate.T(slot.LabelKey, slot.LabelFallback)
	}

	// ── Route by slot type ───────────────────────────────────────────────

	switch slot.SlotType {

	case "system":
		return b.buildSystemNode(slot, label, parentStyles)

	case "device":
		return b.buildDeviceNode(slot, label, parentStyles)

	case "template":
		return b.buildTemplateNode(slot, label, parentStyles)

	case "section":
		return b.buildSectionOrCategoryNode(slot, label, parentStyles, true)

	case "category":
		return b.buildSectionOrCategoryNode(slot, label, parentStyles, false)

	default:
		log.Printf("[MenuBuilder] unknown slot_type=%q for %s, skipping", slot.SlotType, slot.SlotID)
		return nil
	}
}

// buildSystemNode creates a menu item for a system slot using the factory registry.
// System items have pre-registered factories that provide OnClick callbacks.
func (b *MenuBuilder) buildSystemNode(slot *RailSlot, label string, styles [5]hexMenu.IconStyle) *hexMenu.MenuItem {
	fn, ok := b.factories[slot.SlotID]
	if !ok {
		log.Printf("[MenuBuilder] no factory for system slot_id=%s, skipping", slot.SlotID)
		return nil
	}

	item := fn(label)

	// Build children recursively for submenu items.
	//
	// [FIX 2026-07] The gate is len(slot.Children) alone — the DB's
	// item_type is NOT consulted. Databases seeded before the linear panel
	// existed carry item_type='action' on group slots that were direct
	// actions in the old design (SysExport, SysDisplay); gating on that
	// stale flag left their Submenu empty, so the panel fell back to
	// showing the group itself as its only row ("Export ›" going nowhere).
	// The tree's parent/child structure is the ground truth the server
	// already maintains; the client now follows it. The server-side
	// counterpart (migrateMenuTreeItemTypes) heals the stale rows so the
	// Control Panel shows the right type too.
	//
	// Português: [FIX 2026-07] O gate é só len(slot.Children) — o item_type
	// do banco NÃO é consultado. Bancos populados antes do painel linear
	// carregam item_type='action' em grupos que eram ações diretas no
	// design antigo (SysExport, SysDisplay); condicionar nessa flag velha
	// deixava o Submenu vazio e o painel caía no fallback de mostrar o
	// próprio grupo como única linha ("Export ›" sem destino). A estrutura
	// pai/filho da árvore é a verdade que o server já mantém; o cliente
	// agora a segue. O par no server (migrateMenuTreeItemTypes) corrige as
	// linhas velhas para o Control Panel também mostrar o tipo certo.
	if len(slot.Children) > 0 {
		sub := make([]hexMenu.MenuItem, 0, len(slot.Children)+1)
		back := hexMenu.GoBackItem(3, 3)
		back.Styles = styles
		sub = append(sub, back)

		for i := range slot.Children {
			child := &slot.Children[i]
			if ci := b.buildNodeFromTree(child, styles); ci != nil {
				sub = append(sub, *ci)
			}
		}
		item.Submenu = sub
		// A node with children must behave as a submenu even when its
		// factory (or a stale catalog) says otherwise — the panel's
		// navigation keys off item.Type.
		// Português: Nó com filhos precisa se comportar como submenu mesmo
		// quando a factory (ou catálogo velho) diz outra coisa — a
		// navegação do painel decide por item.Type.
		item.Type = hexMenu.ItemSubmenu
	}

	return &item
}

// buildDeviceNode creates a menu item for a device slot.
// Matches the loaded BlackBoxDefClient by DeviceStructName and generates
// the Init/method submenu — the same submenu that the hardware menu uses.
// Returns nil if the device definition is not loaded (device not available)
// or if the device's supported languages do not include the project's
// language.
func (b *MenuBuilder) buildDeviceNode(slot *RailSlot, label string, styles [5]hexMenu.IconStyle) *hexMenu.MenuItem {
	if slot.DeviceStructName == "" {
		log.Printf("[MenuBuilder] device %s has no struct name, skipping", slot.SlotID)
		return nil
	}

	// Find the matching BlackBoxDefClient by Go struct name.
	var def *blackbox.BlackBoxDefClient
	for _, d := range b.blackBoxDefs {
		if d.Name == slot.DeviceStructName {
			def = d
			break
		}
	}
	if def == nil {
		// Device definition not loaded — the specialist may have unpublished it
		// or it's not available in the current session. Skip silently.
		return nil
	}

	// ── Language filter ──────────────────────────────────────────────────
	//
	// Hide a black-box whose source language is not this project's. The
	// decision now comes from the definition itself
	// (def.ProgrammingLanguage, stamped by the server), via
	// SupportsProjectLanguage — replacing the old Parcela-5 rule that asked
	// the catalogue and got a hardcoded {"go"} for every black-box (which
	// wrongly hid C99 devices in C99 projects). The catalogue covers
	// primitives only now; black-box language is per-device data it cannot
	// know. Returning nil reuses the existing "definition not loaded" path:
	// callers already skip nil items, so no extra plumbing and no log line.
	//
	// Português: Filtro de linguagem — esconde o black-box cuja linguagem
	// difere da do projeto, consultando o def (ProgrammingLanguage), não o
	// catálogo. nil é o contrato existente pra "device indisponível".
	if !def.SupportsProjectLanguage(b.language) {
		return nil
	}

	// Build the Init/method submenu (same as regular hardware menu).
	submenu := b.blackBoxFuncSubmenu(def)

	// Apply the parent's styles to all submenu items (Init, methods, Back).
	for i := range submenu {
		submenu[i].Styles = styles
	}

	// Resolve the device icon from the definition.
	devIcon := rulesIcon.IconByNameOrDefault(def.EffectiveStructIcon(), "gear")

	return &hexMenu.MenuItem{
		// Use the same "bb_" + Name ID format as the legacy hardware menu
		// so the panel's readme and method-help lookup matches. The workspace
		// registers readmes as bbReadmes["bb_"+def.Name].
		ID:              "bb_" + def.Name,
		Label:           label,
		FontAwesomePath: devIcon.Path,
		ViewBox:         devIcon.ViewBox,
		Type:            hexMenu.ItemSubmenu,
		Submenu:         submenu,
		Styles:          styles,
	}
}

// buildTemplateNode creates a menu item for a template slot.
// Matches the loaded TemplateFullClient and generates the device submenu.
// Returns nil if the template definition is not loaded.
func (b *MenuBuilder) buildTemplateNode(slot *RailSlot, label string, styles [5]hexMenu.IconStyle) *hexMenu.MenuItem {
	// Templates are matched by their ID (stored in slot_id as "Tmpl_<id>").
	// Extract the template ID by removing the "Tmpl_" prefix.
	tmplID := slot.SlotID
	if len(tmplID) > 5 && tmplID[:5] == "Tmpl_" {
		tmplID = tmplID[5:]
	}

	var tmpl *templateclient.TemplateFullClient
	for _, t := range b.templateDefs {
		if t.Meta.ID == tmplID {
			tmpl = t
			break
		}
	}
	if tmpl == nil {
		return nil
	}

	submenu := b.templateDeviceSubmenu(tmpl)
	for i := range submenu {
		submenu[i].Styles = styles
	}

	return &hexMenu.MenuItem{
		// Use the same "tmpl_" + ID format as the legacy template menu
		// so the panel's readme lookup matches. The workspace registers
		// readmes as bbReadmes["tmpl_"+t.Meta.ID].
		ID:              "tmpl_" + tmplID,
		Label:           label,
		FontAwesomePath: rulesIcon.KFAFileExport,
		ViewBox:         "0 0 576 512",
		Type:            hexMenu.ItemSubmenu,
		Submenu:         submenu,
		Styles:          styles,
	}
}

// buildSectionOrCategoryNode creates a submenu item for a section or category.
// Sections have brand colors that are propagated to all children.
// Categories use the standard menu styles.
// Both recurse into their tree children.
func (b *MenuBuilder) buildSectionOrCategoryNode(slot *RailSlot, label string, parentStyles [5]hexMenu.IconStyle, isSection bool) *hexMenu.MenuItem {
	// Determine styles for this node and its children.
	childStyles := parentStyles
	if isSection && (slot.ColorNormal != "" || slot.ColorAttention != "" || slot.ColorFeatured != "") {
		// Build brand-colored styles from the section's 3 pipeline colors.
		childStyles = sectionStyles(slot.ColorNormal, slot.ColorAttention, slot.ColorFeatured)
	}

	// Resolve icon.
	iconDef := rulesIcon.IconByNameOrDefault(slot.IconFA, "folder")

	// Build children recursively.
	var childItems []hexMenu.MenuItem
	if len(slot.Children) > 0 {
		childItems = make([]hexMenu.MenuItem, 0, len(slot.Children))
		for i := range slot.Children {
			child := &slot.Children[i]
			if ci := b.buildNodeFromTree(child, childStyles); ci != nil {
				childItems = append(childItems, *ci)
			}
		}
	}

	if len(childItems) == 0 {
		// Empty section/category — no devices loaded or all children filtered.
		// Skip to avoid showing an empty submenu.
		return nil
	}

	// Use ApplyRadialLayout to add the Back button and arrange items.
	submenu := rulesMainMenu.ApplyRadialLayout(childItems, childStyles)

	item := &hexMenu.MenuItem{
		ID:              slot.SlotID,
		Label:           label,
		FontAwesomePath: iconDef.Path,
		ViewBox:         iconDef.ViewBox,
		Type:            hexMenu.ItemSubmenu,
		Submenu:         submenu,
		Styles:          childStyles,
	}

	// Store the brand color for the Panel's section-header rendering.
	if isSection && slot.ColorNormal != "" {
		item.BrandColor = slot.ColorNormal
	}

	return item
}

// ─── Brand color styles ─────────────────────────────────────────────────────

// sectionStyles converts three brand colors from the database into a complete
// [5]hexMenu.IconStyle array. When any color is empty, the standard menu
// colors are used as fallback so the admin is not forced to choose colors.
//
// State mapping:
//
//	PipelineNormal     → color_normal
//	PipelineDisabled   → standard gray (tutorial mode)
//	PipelineSelected   → color_featured
//	PipelineAttention1 → color_attention
//	PipelineAttention2 → color_attention + white border
func sectionStyles(colorNormal, colorAttention, colorFeatured string) [5]hexMenu.IconStyle {
	// Fallback to default menu colors when the admin did not set them.
	if colorNormal == "" && colorAttention == "" && colorFeatured == "" {
		return rulesMainMenu.MenuStyles()
	}

	defaults := rulesMainMenu.MenuStyles()
	if colorNormal == "" {
		colorNormal = defaults[0].ColorBackground
	}
	if colorAttention == "" {
		colorAttention = defaults[3].ColorBackground
	}
	if colorFeatured == "" {
		colorFeatured = defaults[2].ColorBackground
	}

	const (
		iconColor  = "#FFFFFF"
		labelColor = "#FFFFFF"
	)
	return [5]hexMenu.IconStyle{
		// PipelineNormal
		{ColorIcon: iconColor, ColorBorder: colorNormal, ColorLabel: labelColor, ColorBackground: colorNormal},
		// PipelineDisabled — same gray used by all disabled items
		{ColorIcon: "#777777", ColorBorder: "#666666", ColorLabel: "#888888", ColorBackground: "#2A2A40"},
		// PipelineSelected
		{ColorIcon: iconColor, ColorBorder: colorFeatured, ColorLabel: labelColor, ColorBackground: colorFeatured},
		// PipelineAttention1
		{ColorIcon: iconColor, ColorBorder: colorAttention, ColorLabel: labelColor, ColorBackground: colorAttention},
		// PipelineAttention2 — same color, white border for contrast
		{ColorIcon: iconColor, ColorBorder: "#FFFFFF", ColorLabel: labelColor, ColorBackground: colorAttention},
	}
}

// buildLegacy is the original hardcoded Build() logic.
// Used as fallback when the server tree is not available.
func (b *MenuBuilder) buildLegacy() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()

	items := []hexMenu.MenuItem{
		{
			ID:              "SysMath",
			Col:             1,
			Row:             1,
			Label:           translate.T("menuMainMath", "Math"),
			FontAwesomePath: rulesIcon.KFASquareRootVariable,
			ViewBox:         "0 0 640 640",
			Type:            hexMenu.ItemSubmenu,
			Submenu:         b.mathSubmenu(),
			Styles:          styles,
		},
		{
			ID:              "SysLogic",
			Col:             2,
			Row:             2,
			AdjustIconY:     12,
			AdjustLabelY:    6,
			Label:           translate.T("menuMainLogic", "Logic"),
			FontAwesomePath: rulesIcon.KFaSquareBinary,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemSubmenu,
			Submenu:         b.logicSubmenu(),
			Styles:          styles,
		},
		{
			ID:              "SysLoop",
			Col:             3,
			Row:             1,
			Label:           translate.T("menuMainLoop", "Loop"),
			FontAwesomePath: rulesIcon.KFARepeat,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemSubmenu,
			Submenu:         b.loopSubmenu(),
			Styles:          styles,
		},
		{
			ID:              "SysConst",
			Col:             1,
			Row:             3,
			Label:           translate.T("menuMainConst", "Const"),
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemSubmenu,
			Submenu:         b.constSubmenu(),
			Styles:          styles,
		},
		{
			ID:              "SysDisplay",
			Col:             3,
			Row:             3,
			Label:           translate.T("menuMainDisplay", "Display"),
			FontAwesomePath: rulesIcon.KFADesktop,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemSubmenu,
			Submenu:         b.displaySubmenu(),
			Styles:          styles,
		},
		{
			ID:              "SysExport",
			Col:             2,
			Row:             4,
			Label:           translate.T("menuMainExport", "Export"),
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemSubmenu,
			Submenu:         b.exportSubmenu(),
			Styles:          styles,
		},
		{
			// Data — maker data sources (File upload, Text/Monaco), the
			// category approved on 2026-07-12: files and authored text as
			// first-class wireable devices. Português: Fontes de dados do
			// maker (upload de arquivo, texto/Monaco), a categoria
			// aprovada em 2026-07-12: arquivos e texto autorado como
			// devices de primeira classe ligáveis por fio.
			ID:              "SysData",
			Col:             3,
			Row:             5,
			Label:           translate.T("menuMainData", "Data"),
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemSubmenu,
			Submenu:         b.dataSubmenu(),
			Styles:          styles,
		},
		{
			ID:              "SysSettings",
			Col:             1,
			Row:             5,
			Label:           translate.T("menuMainSettings", "Settings"),
			FontAwesomePath: rulesIcon.KFAGear,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				if b.liveConfigFn != nil {
					go b.liveConfigFn()
				} else {
					log.Printf("[MainMenu] Settings clicked (no handler)")
				}
			},
			Styles: styles,
		},
	}

	// ── Dynamic category menus ────────────────────────────────────────────────
	//
	// Devices and templates are grouped by MenuCategory → MenuSubcategory.
	// The hardcoded "Hardware" and "Templates" items are gone — every device
	// and template appears under its chosen category instead.
	//
	// Items without a category are placed under "Other".
	// The key for i18n is built as "menuCat_" + slugified category name so
	// the translation server can supply localised names.
	//
	// Gear icon is used for all categories until per-category icon support
	// is implemented.
	categoryItems := b.buildCategoryMenuItems()
	items = append(items, categoryItems...)

	// Exit button — always the last item in the menu.
	// Calls window._ideExit() which is exposed by ide.js and navigates
	// the SPA back to the home page.
	exitStyles := [5]hexMenu.IconStyle{
		// PipelineNormal — muted red
		{ColorIcon: "#FFFFFF", ColorBorder: "#7a2020", ColorLabel: "#cc6666", ColorBackground: "#3a1515"},
		// PipelineDisabled — standard gray
		{ColorIcon: "#777777", ColorBorder: "#666666", ColorLabel: "#888888", ColorBackground: "#2A2A40"},
		// PipelineSelected — brighter red
		{ColorIcon: "#FFFFFF", ColorBorder: "#cc3333", ColorLabel: "#FFFFFF", ColorBackground: "#661111"},
		// PipelineAttention1
		{ColorIcon: "#FFFFFF", ColorBorder: "#cc3333", ColorLabel: "#FFFFFF", ColorBackground: "#661111"},
		// PipelineAttention2
		{ColorIcon: "#FFFFFF", ColorBorder: "#FFFFFF", ColorLabel: "#FFFFFF", ColorBackground: "#cc3333"},
	}
	items = append(items, hexMenu.MenuItem{
		ID:              "SysExit",
		Label:           translate.T("menuMainExit", "Exit"),
		FontAwesomePath: "M505 273c9.4-9.4 9.4-24.6 0-33.9L361 95c-6.9-6.9-17.2-8.9-26.2-5.2S320 102.3 320 112l0 80-112 0c-26.5 0-48 21.5-48 48l0 32c0 26.5 21.5 48 48 48l112 0 0 80c0 9.7 5.8 18.5 14.8 22.2s19.3 1.7 26.2-5.2L505 273zM160 96c17.7 0 32-14.3 32-32s-14.3-32-32-32L96 32C43 32 0 75 0 128L0 384c0 53 43 96 96 96l64 0c17.7 0 32-14.3 32-32s-14.3-32-32-32l-64 0c-17.7 0-32-14.3-32-32l0-256c0-17.7 14.3-32 32-32l64 0z",
		ViewBox:         "0 0 512 512",
		Type:            hexMenu.ItemAction,
		OnClick: func() {
			// The WASM runs inside an iframe. _ideExit lives in the parent window,
			// not in the iframe's window, so we cannot call it directly.
			// postMessage to the parent is the correct cross-frame mechanism.
			// ide.js listens for { type: "IDE_EXIT" } and calls nav('home').
			parent := js.Global().Get("parent")
			if parent.Truthy() && !parent.Equal(js.Global()) {
				// Running inside an iframe — notify the parent SPA.
				msg := js.Global().Get("Object").New()
				msg.Set("type", "IDE_EXIT")
				parent.Call("postMessage", msg, js.Global().Get("location").Get("origin"))
			} else {
				// Fallback: running standalone (dev/test), try _ideExit directly.
				exitFn := js.Global().Get("_ideExit")
				if exitFn.Truthy() {
					exitFn.Invoke()
				}
			}
		},
		Styles: exitStyles,
	})

	return items
}

// =====================================================================
//
//	Hardcoded submenus
//
// =====================================================================
func (b *MenuBuilder) mathSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(3, 3)
	back.Styles = styles

	return []hexMenu.MenuItem{
		back,
		{
			ID:              "Add",
			Col:             2,
			Row:             2,
			Label:           translate.T("menuMainAdd", "Add"),
			FontAwesomePath: rulesIcon.KFAPlus,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateAdd", b.factory.CreateAdd) },
			Styles:          styles,
		},
		{
			ID:              "Sub",
			Col:             2,
			Row:             4,
			Label:           translate.T("menuMainSub", "Sub"),
			FontAwesomePath: rulesIcon.KFAMinus,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateSub", b.factory.CreateSub) },
			Styles:          styles,
		},
		{
			ID:              "Mul",
			Col:             4,
			Row:             2,
			Label:           translate.T("menuMainMul", "Mul"),
			FontAwesomePath: rulesIcon.KFAXmark,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateMul", b.factory.CreateMul) },
			Styles:          styles,
		},
		{
			ID:              "Div",
			Col:             4,
			Row:             4,
			Label:           translate.T("menuMainDiv", "Div"),
			FontAwesomePath: rulesIcon.KFADivide,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateDiv", b.factory.CreateDiv) },
			Styles:          styles,
		},
	}
}

func (b *MenuBuilder) logicSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(2, 4)
	back.Styles = styles

	return []hexMenu.MenuItem{
		back,
		{
			ID:              "EqualTo",
			Col:             2,
			Row:             2,
			AdjustIconY:     10,
			AdjustLabelY:    6,
			Label:           translate.T("menuMainEqualTo", "Equal to"),
			FontAwesomePath: rulesIcon.KFaEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateEqualTo", b.factory.CreateEqualTo) },
			Styles:          styles,
		},
		{
			ID:              "LessThan",
			Col:             1,
			Row:             3,
			AdjustIconY:     10,
			AdjustLabelY:    6,
			Label:           translate.T("menuMainLessThan", "Less than"),
			FontAwesomePath: rulesIcon.KFaLessThan,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLessThan", b.factory.CreateLessThan) },
			Styles:          styles,
		},
		{
			ID:              "LessThanOrEqualTo",
			Col:             1,
			Row:             5,
			AdjustIconY:     16,
			AdjustLabelY:    6,
			Label:           translate.T("menuMainLessThanOrEqualTo", "Less than or\nequal to"),
			FontAwesomePath: rulesIcon.KFaLessThanEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLessThanOrEqualTo", b.factory.CreateLessThanOrEqualTo) },
			Styles:          styles,
		},
		{
			ID:              "notEqualTo",
			Col:             2,
			Row:             6,
			AdjustIconY:     12,
			AdjustLabelY:    6,
			Label:           translate.T("menuMainNotEqualTo", "Not equal to"),
			FontAwesomePath: rulesIcon.KFaNotEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateNotEqualTo", b.factory.CreateNotEqualTo) },
			Styles:          styles,
		},
		{
			ID:              "GreaterThan",
			Col:             3,
			Row:             5,
			AdjustIconY:     12,
			AdjustLabelY:    6,
			Label:           translate.T("menuMainGreaterThan", "Greater than"),
			FontAwesomePath: rulesIcon.KFaGreaterThan,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGreaterThan", b.factory.CreateGreaterThan) },
			Styles:          styles,
		},
		{
			ID:              "GreaterThanOrEqualTo",
			Col:             3,
			Row:             3,
			AdjustIconY:     12,
			AdjustLabelY:    6,
			Label:           translate.T("menuMainGreaterThanOrEqualTo", "Greater than or\nequal to"),
			FontAwesomePath: rulesIcon.KFaGreaterThanEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGreaterThanOrEqualTo", b.factory.CreateGreaterThanOrEqualTo) },
			Styles:          styles,
		},
		{
			// Case — N-way selector container (switch); replaces nested If/Else
			ID:              "Case",
			Col:             3,
			Row:             1,
			Label:           translate.T("menuMainCase", "Case"),
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateCase", b.factory.CreateCase) },
			Styles:          styles,
		},
	}
}

func (b *MenuBuilder) loopSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(2, 4)
	back.Styles = styles

	return []hexMenu.MenuItem{
		back,
		{
			ID:              "Loop",
			Col:             1,
			Row:             3,
			Label:           translate.T("menuMainLoop", "Loop"),
			FontAwesomePath: rulesIcon.KFARepeat,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLoop", b.factory.CreateLoop) },
			Styles:          styles,
		},
		{
			// LoopDuration — timed infinite loop with time.Sleep interval
			ID:              "LoopDuration",
			Col:             1,
			Row:             5,
			Label:           translate.T("menuMainLoopDuration", "Timed"),
			FontAwesomePath: rulesIcon.KFAHourglassHalf,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLoopDuration", b.factory.CreateLoopDuration) },
			Styles:          styles,
		},
	}
}

// dataSubmenu is the DATA category page: maker data sources emitted as
// wires. v1 ships File (upload → []uint8); Text (Monaco) follows.
// Português: A página da categoria DATA: fontes de dados do maker
// emitidas como fios. v1 traz File (upload → []uint8); Text (Monaco) vem
// a seguir.
func (b *MenuBuilder) dataSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(3, 3)
	back.Styles = styles

	return []hexMenu.MenuItem{
		back,
		{
			// File — a maker-uploaded file as []uint8 bytes.
			ID:              "DataFile",
			Col:             2,
			Row:             2,
			Label:           translate.T("menuDataFile", "File"),
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateDataFile", b.factory.CreateDataFile) },
			Styles:          styles,
		},
		{
			// Text — maker-authored text (Monaco) as []uint8 bytes.
			ID:              "DataText",
			Col:             3,
			Row:             2,
			Label:           translate.T("menuDataText", "Text"),
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateDataText", b.factory.CreateDataText) },
			Styles:          styles,
		},
	}
}

func (b *MenuBuilder) constSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(3, 3)
	back.Styles = styles

	return []hexMenu.MenuItem{
		back,
		{
			// Int — integer literal, blue border
			ID:              "ConstInt",
			Col:             2,
			Row:             2,
			Label:           translate.T("menuMainConstInt", "Int"),
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstInt", b.factory.CreateConstInt) },
			Styles:          styles,
		},
		{
			// Bool — boolean literal, orange border
			ID:              "ConstBool",
			Col:             2,
			Row:             4,
			Label:           translate.T("menuMainConstBool", "Bool"),
			FontAwesomePath: rulesIcon.KFAToggleOn,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateBool", b.factory.CreateBool) },
			Styles:          styles,
		},
		{
			// Float — floating-point literal, green/teal border
			ID:              "ConstFloat",
			Col:             4,
			Row:             2,
			Label:           translate.T("menuMainConstFloat", "Float"),
			FontAwesomePath: rulesIcon.KFADivide,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstFloat", b.factory.CreateConstFloat) },
			Styles:          styles,
		},
		{
			// String — text literal, amber border
			ID:              "ConstString",
			Col:             4,
			Row:             4,
			Label:           translate.T("menuMainConstString", "String"),
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstString", b.factory.CreateConstString) },
			Styles:          styles,
		},
		{
			// Duration — time.Duration literal, cyan border
			ID:              "ConstDuration",
			Col:             3,
			Row:             1,
			Label:           translate.T("menuMainConstDuration", "Duration"),
			FontAwesomePath: rulesIcon.KFAHourglassHalf,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstDuration", b.factory.CreateConstDuration) },
			Styles:          styles,
		},
		{
			// Int Array — constant fixed-size int collection
			// (e.g. []int{1, 2, 3}), border/wire in the int blue family.
			// The three collection hexes form the bottom arc of the
			// submenu: (3,5) center, (2,6) and (4,6) flanking — mirroring
			// the scalar consts above them.
			ID:              "ConstArrayInt",
			Col:             3,
			Row:             5,
			Label:           translate.T("menuMainConstArrayInt", "Int Array"),
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstArrayInt", b.factory.CreateConstArrayInt) },
			Styles:          styles,
		},
		{
			// Float Array — constant fixed-size float collection with a
			// float32/float64 precision select (mirrors the scalar Float).
			ID:              "ConstArrayFloat",
			Col:             2,
			Row:             6,
			Label:           translate.T("menuMainConstArrayFloat", "Float Array"),
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstArrayFloat", b.factory.CreateConstArrayFloat) },
			Styles:          styles,
		},
		{
			// String Array — constant fixed-size string collection,
			// authored one element per line (a comma is string content).
			ID:              "ConstArrayString",
			Col:             4,
			Row:             6,
			Label:           translate.T("menuMainConstArrayString", "String Array"),
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstArrayString", b.factory.CreateConstArrayString) },
			Styles:          styles,
		},
	}
}

func (b *MenuBuilder) displaySubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(2, 4)
	back.Styles = styles

	return []hexMenu.MenuItem{
		back,
		{
			ID:              "Gauge",
			Col:             1,
			Row:             3,
			Label:           translate.T("menuMainGauge", "Gauge"),
			FontAwesomePath: rulesIcon.KFAGear,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGauge", b.factory.CreateGauge) },
			Styles:          styles,
		},
		{
			ID:              "LED",
			Col:             2,
			Row:             3,
			Label:           translate.T("menuMainLED", "LED"),
			FontAwesomePath: rulesIcon.KFAEye,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLED", b.factory.CreateLED) },
			Styles:          styles,
		},
		{
			ID:              "BarGraph",
			Col:             1,
			Row:             4,
			Label:           translate.T("menuMainBarGraph", "Bar"),
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateBarGraph", b.factory.CreateBarGraph) },
			Styles:          styles,
		},
		{
			ID:              "TextDisplay",
			Col:             2,
			Row:             4,
			Label:           translate.T("menuMainTextDisplay", "Text"),
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateTextDisplay", b.factory.CreateTextDisplay) },
			Styles:          styles,
		},
		{
			ID:              "Button",
			Col:             1,
			Row:             5,
			Label:           translate.T("menuMainButton", "Button"),
			FontAwesomePath: rulesIcon.KFAPlay,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateButton", b.factory.CreateButton) },
			Styles:          styles,
		},
		{
			ID:              "SevenSeg",
			Col:             2,
			Row:             5,
			Label:           translate.T("menuMainSevenSeg", "7-Seg"),
			FontAwesomePath: rulesIcon.KFADesktop,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateSevenSeg", b.factory.CreateSevenSeg) },
			Styles:          styles,
		},
		{
			ID:              "Knob",
			Col:             1,
			Row:             6,
			Label:           translate.T("menuMainKnob", "Knob"),
			FontAwesomePath: rulesIcon.KFAGear,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateKnob", b.factory.CreateKnob) },
			Styles:          styles,
		},
		{
			ID:              "Chart",
			Col:             2,
			Row:             6,
			Label:           translate.T("menuMainChart", "Chart"),
			FontAwesomePath: rulesIcon.KFASerial,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateChart", b.factory.CreateChart) },
			Styles:          styles,
		},
		{
			// BackgroundImage — uploads a PNG/SVG background that renders
			// below all other frontend components. Ideal for industrial
			// plant layouts with indicators on top.
			ID:              "BackgroundImage",
			Col:             1,
			Row:             7,
			Label:           translate.T("menuMainBackgroundImage", "Background"),
			FontAwesomePath: rulesIcon.KFAImage,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateBackgroundImage", b.factory.CreateBackgroundImage) },
			Styles:          styles,
		},
		{
			ID:              "CommStatus",
			Col:             2,
			Row:             7,
			Label:           translate.T("menuMainCommStatus", "Comm"),
			FontAwesomePath: rulesIcon.IconByNameOrDefault("network-wired", "gear").Path,
			ViewBox:         rulesIcon.IconByNameOrDefault("network-wired", "gear").ViewBox,
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateCommStatus", b.factory.CreateCommStatus) },
			Styles:          styles,
		},
	}
}

// exportSubmenu builds the Export submenu.
//
// "Export → JSON"   — downloads the raw scene JSON (debugging / backup).
// "Export → Export" — single exit point for project generation. The
//
//	callback reads w.Language and hits the matching
//	codegen endpoint (POST /api/v1/codegen/go for Go
//	projects, POST /api/v1/codegen/c for C99). The
//	label deliberately does NOT name the language —
//	the maker chose it at project creation and the
//	UI should not second-guess that choice.
//
// "Export → Files"  — opens the stage file manager overlay.
// "Export → Image"  — PNG screenshot with embedded scene JSON.
//
//	The Workspace.export() method detects what is on the canvas:
//	  • Template devices present → sceneresolver traces wires and props
//	    → templateclient.GenerateAndDownload → browser downloads ZIP.
//	  • Custom devices only → SSE codegen pipeline → Monaco overlay.
//	    Backend language picked from w.Language.
//
// There is intentionally NO "Generate ZIP" action here or in the Templates
// submenu. The export decision is made at click time by the Workspace, not
// at menu build time.
func (b *MenuBuilder) exportSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(3, 3)
	back.Styles = styles

	return []hexMenu.MenuItem{
		back,
		{
			ID:              "ExportJSON",
			Col:             2,
			Row:             2,
			Label:           translate.T("menuMainExportJSON", "JSON"),
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Export JSON clicked")
				if b.exportJSONFn != nil {
					b.exportJSONFn()
				}
			},
			Styles: styles,
		},
		{
			// Single export item — label does NOT name the
			// language. The maker chose the language in the
			// welcome modal at project creation; surfacing it
			// again on every export click is redundant and
			// breaks the abstraction that "the project knows
			// what it is".
			//
			// The OnClick callback (b.exportFn) routes through
			// Workspace.export(), which reads w.Language and
			// hits POST /api/v1/codegen/:language — Go projects
			// receive Go source, C99 projects receive C source,
			// no choice on this side.
			//
			// Português: Item único de export — label NÃO
			// menciona a linguagem. O maker já escolheu no
			// welcome modal; expor de novo aqui é redundante.
			// Roteamento por linguagem acontece silenciosamente
			// no Workspace.export() via w.Language.
			ID:              "ExportCode",
			Col:             4,
			Row:             2,
			Label:           translate.T("menuMainExport", "Export"),
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Export clicked")
				if b.exportFn != nil {
					b.exportFn()
				}
			},
			Styles: styles,
		},
		{
			// Files — opens the stage file manager overlay for saving,
			// loading, renaming, and deleting saved IDE scenes.
			ID:              "ExportFiles",
			Col:             2,
			Row:             4,
			Label:           translate.T("menuMainFiles", "Files"),
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Files clicked")
				if b.filesFn != nil {
					b.filesFn()
				}
			},
			Styles: styles,
		},
		{
			// Image — captures a PNG screenshot of the entire stage with
			// the scene JSON embedded via LSB steganography. The image
			// can be imported back to reconstruct the stage.
			ID:              "ExportImage",
			Col:             4,
			Row:             4,
			Label:           translate.T("menuMainImage", "Image"),
			FontAwesomePath: rulesIcon.KFAImage,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Export → Image clicked")
				if b.imageFn != nil {
					b.imageFn()
				}
			},
			Styles: styles,
		},
	}
}

// ─── Icon helper ─────────────────────────────────────────────────────────────

// applyIconToMenuItem fills the icon fields of a hexMenu.MenuItem using an
// `icon:` tag value string from the IDS doc comment.
func applyIconToMenuItem(item *hexMenu.MenuItem, iconValue, fallbackName string) {
	iv := rulesIcon.ParseIconValue(iconValue)
	switch iv.Kind {
	case rulesIcon.IconKindUnicode:
		item.FontAwesomeUnicode = iv.Codepoint
		item.FontAwesomeBrands = iv.Style == rulesIcon.IconFontBrands
		item.FontAwesomePath = ""
		item.ViewBox = ""
	default: // IconKindName
		def := rulesIcon.IconByNameOrDefault(iv.Name, fallbackName)
		item.FontAwesomePath = def.Path
		item.ViewBox = def.ViewBox
		item.FontAwesomeUnicode = 0
	}
}

// ─── Category-based menu building ─────────────────────────────────────────────

// menuEntry holds one device or template normalised for grouping.
type menuEntry struct {
	// itemID is the canonical MenuItem.ID — the same one that hardwareSubmenu
	// and templateDeviceSubmenu generate ("bb_{Name}" or "tmpl_{tmplID}").
	// The panel uses this ID to look up readmes and method help tabs, so it
	// must match exactly what workspace.go registers in bbReadmes/bbMethodHelps.
	itemID       string
	label        string                    // MenuName → StructLabel → Name
	category     string                    // MenuCategory, "" → "Other"
	subcategory  string                    // MenuSubcategory, "" → direct under category
	buildSubmenu func() []hexMenu.MenuItem // returns the function sub-items

	// iconValue is the raw IDS icon: tag value (e.g. "microchip", "f287",
	// "gear"). Used by buildCategoryMenuItems to resolve the correct
	// FontAwesome icon for each device/template in the category menu.
	// When empty, the fallback "gear" icon is used.
	iconValue string
}

// buildCategoryMenuItems groups all devices and templates by category and
// subcategory, then builds one hexMenu.MenuItem per unique category.
//
// Hierarchy:
//
//	category (top-level hex) → [subcategory (hex) →] device/template (hex)
//
// When subcategory is empty the device/template appears directly in the
// category submenu (no extra level). Items with no category land under "Other".
//
// NOTE: ApplyRadialLayout inserts the Back button automatically. Never pass
// a manually-constructed Back item to it — that produces duplicates.
func (b *MenuBuilder) buildCategoryMenuItems() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()

	// Collect all entries from devices and templates in one pass.
	var entries []menuEntry

	for _, def := range b.blackBoxDefs {
		d := def

		cat := d.MenuCategory
		if cat == "" {
			cat = "Other"
		}
		label := d.MenuName
		if label == "" {
			label = d.EffectiveStructLabel()
		}
		entries = append(entries, menuEntry{
			itemID:      "bb_" + d.Name,
			label:       label,
			category:    cat,
			subcategory: d.MenuSubcategory,
			iconValue:   d.EffectiveStructIcon(),
			buildSubmenu: func() []hexMenu.MenuItem {
				return b.blackBoxFuncSubmenu(d)
			},
		})
	}

	for _, tmpl := range b.templateDefs {
		t := tmpl
		cat := t.Meta.MenuCategory
		if cat == "" {
			cat = "Other"
		}
		entries = append(entries, menuEntry{
			itemID:      "tmpl_" + t.Meta.ID,
			label:       t.Meta.Name,
			category:    cat,
			subcategory: t.Meta.MenuSubcategory,
			buildSubmenu: func() []hexMenu.MenuItem {
				return b.templateDeviceSubmenu(t)
			},
		})
	}

	if len(entries) == 0 {
		return nil
	}

	// Group: category → subcategory → []menuEntry.
	// Slices of keys preserve insertion (first-seen) order.
	type subMap struct {
		keys    []string
		entries map[string][]menuEntry
	}
	var catKeys []string
	catMap := map[string]*subMap{}

	for _, e := range entries {
		sm, ok := catMap[e.category]
		if !ok {
			sm = &subMap{entries: map[string][]menuEntry{}}
			catMap[e.category] = sm
			catKeys = append(catKeys, e.category)
		}
		if _, exists := sm.entries[e.subcategory]; !exists {
			sm.keys = append(sm.keys, e.subcategory)
		}
		sm.entries[e.subcategory] = append(sm.entries[e.subcategory], e)
	}

	// Build one top-level MenuItem per category.
	// ApplyRadialLayout handles Back button insertion and pagination — we only
	// pass the user-visible items to it, never a manual Back item.
	catItems := make([]hexMenu.MenuItem, 0, len(catKeys))
	for _, catName := range catKeys {
		sm := catMap[catName]

		// i18n: "menuCat_Sensors" → translated name; fallback is catName itself.
		catLabel := translate.T("menuCat_"+catName, catName)

		// Collect the items that appear inside this category's submenu.
		// Each element is either a leaf (device/template) or a subcategory hex.
		var catChildren []hexMenu.MenuItem

		for _, subKey := range sm.keys {
			subEntries := sm.entries[subKey]

			if subKey == "" {
				// No subcategory — device/template appears directly under category.
				for _, e := range subEntries {
					en := e
					item := hexMenu.MenuItem{
						ID:      en.itemID,
						Label:   en.label,
						Type:    hexMenu.ItemSubmenu,
						Submenu: en.buildSubmenu(),
						Styles:  styles,
					}
					applyIconToMenuItem(&item, en.iconValue, "gear")
					catChildren = append(catChildren, item)
				}
			} else {
				// Has subcategory — one extra level.
				subLabel := translate.T("menuSubcat_"+subKey, subKey)

				// Children of the subcategory submenu (no manual Back).
				subChildren := make([]hexMenu.MenuItem, 0, len(subEntries))
				for _, e := range subEntries {
					en := e
					item := hexMenu.MenuItem{
						ID:      en.itemID,
						Label:   en.label,
						Type:    hexMenu.ItemSubmenu,
						Submenu: en.buildSubmenu(),
						Styles:  styles,
					}
					applyIconToMenuItem(&item, en.iconValue, "gear")
					subChildren = append(subChildren, item)
				}

				subIconPath, subIconVB := b.subcategoryIcon(catName, subKey)
				catChildren = append(catChildren, hexMenu.MenuItem{
					ID:    "subcat_" + catName + "_" + subKey,
					Label: subLabel,
					Type:  hexMenu.ItemSubmenu,
					// ApplyRadialLayout adds Back + paginates for us.
					Submenu:         rulesMainMenu.ApplyRadialLayout(subChildren, styles),
					FontAwesomePath: subIconPath,
					ViewBox:         subIconVB,
					Styles:          styles,
				})
			}
		}

		catIconPath, catIconVB := b.categoryIcon(catName)
		catItems = append(catItems, hexMenu.MenuItem{
			ID:              "cat_" + catName,
			Label:           catLabel,
			Type:            hexMenu.ItemSubmenu,
			Submenu:         rulesMainMenu.ApplyRadialLayout(catChildren, styles),
			FontAwesomePath: catIconPath,
			ViewBox:         catIconVB,
			Styles:          styles,
		})
	}

	// Top-level category items are added directly to the main menu alongside
	// the hardcoded items (Math, Logic, etc.). ApplyRadialLayout is for
	// submenus only — it always injects a Back button which must not appear
	// at the top level. The main menu positions items by Col/Row; items with
	// Col=0/Row=0 are auto-placed by the menu renderer, same as sections.
	return catItems
}

// ─── Hardware submenu ─────────────────────────────────────────────────────────

func (b *MenuBuilder) hardwareSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	back := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
	back.Styles = styles

	items := []hexMenu.MenuItem{back}

	if len(b.blackBoxDefs) == 0 {
		items = append(items, hexMenu.MenuItem{
			ID:              "noHardware",
			Col:             rulesMainMenu.BackCenterCol + 1,
			Row:             rulesMainMenu.BackCenterRow - 1,
			Label:           translate.T("menuMainNoHardware", "No devices"),
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() {},
			Styles:          styles,
		})
		return items
	}

	componentItems := make([]hexMenu.MenuItem, 0, len(b.blackBoxDefs))
	for _, def := range b.blackBoxDefs {
		d := def
		item := hexMenu.MenuItem{
			ID:      "bb_" + d.Name,
			Label:   d.EffectiveStructLabel(),
			Type:    hexMenu.ItemSubmenu,
			Submenu: b.blackBoxFuncSubmenu(d),
			Styles:  styles,
		}
		applyIconToMenuItem(&item, d.EffectiveStructIcon(), "gear")
		componentItems = append(componentItems, item)
	}

	return rulesMainMenu.ApplyRadialLayout(componentItems, styles)
}

// blackBoxFuncSubmenu builds the second-level submenu for one black-box component.
// Contains Init (when present) and all named methods in source-file order.
func (b *MenuBuilder) blackBoxFuncSubmenu(def *blackbox.BlackBoxDefClient) []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()
	userItems := make([]hexMenu.MenuItem, 0, 1+len(def.Methods))

	if def.HasInit() {
		d := def
		item := hexMenu.MenuItem{
			ID:         "bb_" + d.Name + "_init",
			Label:      d.Init.EffectiveLabel(),
			Type:       hexMenu.ItemAction,
			MenuCol:    d.Init.MenuCol,
			MenuRow:    d.Init.MenuRow,
			MenuPosSet: d.Init.MenuPosSet,
			OnClick: func() {
				b.factory.SafeRun("CreateBBInit_"+d.Name, func() {
					b.factory.CreateBlackBoxInit(d)
				})
			},
			Styles: styles,
		}
		applyIconToMenuItem(&item, d.Init.EffectiveIcon(d), "gear")
		userItems = append(userItems, item)
	}

	for _, method := range def.Methods {
		d := def
		m := method
		item := hexMenu.MenuItem{
			ID:         "bb_" + d.Name + "_" + m.Name,
			Label:      m.EffectiveLabel(),
			Type:       hexMenu.ItemAction,
			MenuCol:    m.MenuCol,
			MenuRow:    m.MenuRow,
			MenuPosSet: m.MenuPosSet,
			OnClick: func() {
				b.factory.SafeRun("CreateBBMethod_"+d.Name+"_"+m.Name, func() {
					b.factory.CreateBlackBoxMethod(d, m.Name)
				})
			},
			Styles: styles,
		}
		applyIconToMenuItem(&item, m.EffectiveIcon(d), "play")
		userItems = append(userItems, item)
	}

	// C99 device-functions (decision b). Sibling loop to the Methods loop
	// above — additive, byte-identical pattern, routed to the independent
	// CreateBlackBoxFunction factory. A Go black-box has an empty Functions
	// slice (this loop is a no-op); a C99 black-box has empty Init/Methods.
	// See docs/c99_ide_integration.md §5.5.
	for _, fn := range def.Functions {
		d := def
		fnc := fn
		// `device:false.` — a public helper the specialist opted out of
		// device generation: the maker's menu never offers it (its source
		// still ships in the export for the sibling devices to call).
		// Português: Helper público com opt-out: o menu do maker nunca o
		// oferece (o fonte embarca no export para os irmãos chamarem).
		if fnc.NoDevice {
			continue
		}
		// Callback duality: a handler in "both" mode offers BOTH the callable
		// and the reference (ƒ); "ref" mode offers only the ƒ; a plain function
		// offers only the callable. Mirrors the Preview (_fnPreviewVariants).
		showCallable, showRef := fnMenuVariants(fnc)

		if showCallable {
			item := hexMenu.MenuItem{
				ID:         "bb_" + d.Name + "_" + fnc.Name,
				Label:      fnc.EffectiveLabel(),
				Type:       hexMenu.ItemAction,
				MenuCol:    fnc.MenuCol,
				MenuRow:    fnc.MenuRow,
				MenuPosSet: fnc.MenuPosSet,
				// Min-target gate input — the sprite menu turns this into
				// the disabled state. Português: Entrada do portão de
				// min-target — o sprite converte em estado desabilitado.
				MinTarget: fnc.MinTarget,
				OnClick: func() {
					b.factory.SafeRun("CreateBBFunction_"+d.Name+"_"+fnc.Name, func() {
						b.factory.CreateBlackBoxFunction(d, fnc.Name)
					})
				},
				Styles: styles,
			}
			applyIconToMenuItem(&item, fnc.EffectiveIcon(d), "play")
			userItems = append(userItems, item)
		}

		if showRef {
			// The wire-ƒ reference device. Distinct ID and a " ƒ" label suffix
			// (matching the device the factory creates). No fixed hex position
			// (MenuPosSet stays false) so it auto-places beside the callable
			// rather than colliding with the callable's cell in "both" mode.
			refItem := hexMenu.MenuItem{
				ID:        "bb_" + d.Name + "_" + fnc.Name + "_cbref",
				Label:     fnc.EffectiveLabel() + " \u0192",
				Type:      hexMenu.ItemAction,
				MinTarget: fnc.MinTarget,
				OnClick: func() {
					b.factory.SafeRun("CreateBBCallbackRef_"+d.Name+"_"+fnc.Name, func() {
						b.factory.CreateBlackBoxCallbackRef(d, fnc.Name)
					})
				},
				Styles: styles,
			}
			applyIconToMenuItem(&refItem, fnc.EffectiveIcon(d), "play")
			userItems = append(userItems, refItem)
		}
	}

	if len(userItems) == 0 {
		log.Printf("[MenuBuilder] black-box %q has no methods — empty function submenu", def.Name)
		back := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
		back.Styles = styles
		return []hexMenu.MenuItem{back}
	}

	return rulesMainMenu.ApplyRadialLayout(userItems, styles)
}

// ─── Templates submenu ────────────────────────────────────────────────────────

// templatesSubmenu builds the top-level Templates submenu.
//
// Each template appears as a nested submenu containing its device functions
// (Init + methods), identical to the Hardware submenu layout. The maker drags
// devices from here onto the canvas and connects them with wires.
//
// There is NO "Generate ZIP" entry here. Generation happens via
// "Export → Go Code" at the top level, which reads the live canvas state.
func (b *MenuBuilder) templatesSubmenu() []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()

	if len(b.templateDefs) == 0 {
		back := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
		back.Styles = styles
		return []hexMenu.MenuItem{
			back,
			{
				ID:              "noTemplates",
				Col:             rulesMainMenu.BackCenterCol + 1,
				Row:             rulesMainMenu.BackCenterRow - 1,
				Label:           translate.T("menuMainNoTemplates", "No templates"),
				FontAwesomePath: rulesIcon.KFABars,
				ViewBox:         "0 0 448 512",
				Type:            hexMenu.ItemAction,
				OnClick:         func() {},
				Styles:          styles,
			},
		}
	}

	// One submenu entry per template, each expanding into its device functions.
	templateItems := make([]hexMenu.MenuItem, 0, len(b.templateDefs))
	for _, tmpl := range b.templateDefs {
		t := tmpl
		item := hexMenu.MenuItem{
			ID:      "tmpl_" + t.Meta.ID,
			Label:   t.Meta.Name,
			Type:    hexMenu.ItemSubmenu,
			Submenu: b.templateDeviceSubmenu(t),
			Styles:  styles,
		}
		// Use a generic "file" icon for templates — the specialist hasn't
		// declared a per-template icon in the current API response.
		item.FontAwesomePath = rulesIcon.KFAFileExport
		item.ViewBox = "0 0 576 512"
		templateItems = append(templateItems, item)
	}

	return rulesMainMenu.ApplyRadialLayout(templateItems, styles)
}

// templateDeviceSubmenu builds the second-level submenu for one template.
//
// The template's devices appear as standard black-box devices with full wire
// support. The maker places them on the canvas, wires port inputs (like Port),
// sets props in the Inspect panel, and then uses "Export → Go Code" to
// generate the configured project ZIP.
//
// No "Generate ZIP" action is present here. The export decision is made at
// click time by Workspace.export(), which uses sceneresolver to read the
// current wire/prop state.
func (b *MenuBuilder) templateDeviceSubmenu(tmpl *templateclient.TemplateFullClient) []hexMenu.MenuItem {
	styles := rulesMainMenu.MenuStyles()

	if tmpl.Def == nil || len(tmpl.Def.Devices) == 0 {
		log.Printf("[MenuBuilder] template %q has no devices — empty submenu", tmpl.Meta.Name)
		back := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
		back.Styles = styles
		return []hexMenu.MenuItem{back}
	}

	// tmpl.Def.Devices is already []*blackbox.BlackBoxDefClient — the server
	// serializes the same BlackBoxDef type that the IDE uses for hardware
	// black-boxes. No conversion is needed; use each device pointer directly.
	var userItems []hexMenu.MenuItem
	for _, dev := range tmpl.Def.Devices {
		if dev == nil {
			continue
		}
		d := dev

		if d.HasInit() {
			item := hexMenu.MenuItem{
				ID:    "tmpl_" + tmpl.Meta.ID + "_" + d.Name + "_init",
				Label: d.Init.EffectiveLabel(),
				Type:  hexMenu.ItemAction,
				OnClick: func() {
					b.factory.SafeRun("CreateTmplInit_"+d.Name, func() {
						b.factory.CreateBlackBoxInit(d)
					})
				},
				Styles: styles,
			}
			applyIconToMenuItem(&item, d.Init.EffectiveIcon(d), "gear")
			userItems = append(userItems, item)
		}

		for _, method := range d.Methods {
			d := d
			m := method
			item := hexMenu.MenuItem{
				ID:    "tmpl_" + tmpl.Meta.ID + "_" + d.Name + "_" + m.Name,
				Label: m.EffectiveLabel(),
				Type:  hexMenu.ItemAction,
				OnClick: func() {
					b.factory.SafeRun("CreateTmplMethod_"+d.Name+"_"+m.Name, func() {
						b.factory.CreateBlackBoxMethod(d, m.Name)
					})
				},
				Styles: styles,
			}
			applyIconToMenuItem(&item, m.EffectiveIcon(d), "play")
			userItems = append(userItems, item)
		}

		// C99 device-functions inside a template (decision b). Sibling loop
		// to the Methods loop above, same pattern, routed to the independent
		// CreateBlackBoxFunction. No-op for Go devices (empty Functions).
		// See docs/c99_ide_integration.md §5.5.
		for _, fn := range d.Functions {
			d := d
			fnc := fn
			// Callback duality (see fnMenuVariants), same as the device menu.
			showCallable, showRef := fnMenuVariants(fnc)

			if showCallable {
				item := hexMenu.MenuItem{
					ID:    "tmpl_" + tmpl.Meta.ID + "_" + d.Name + "_" + fnc.Name,
					Label: fnc.EffectiveLabel(),
					Type:  hexMenu.ItemAction,
					OnClick: func() {
						b.factory.SafeRun("CreateTmplFunction_"+d.Name+"_"+fnc.Name, func() {
							b.factory.CreateBlackBoxFunction(d, fnc.Name)
						})
					},
					Styles: styles,
				}
				applyIconToMenuItem(&item, fnc.EffectiveIcon(d), "play")
				userItems = append(userItems, item)
			}

			if showRef {
				refItem := hexMenu.MenuItem{
					ID:    "tmpl_" + tmpl.Meta.ID + "_" + d.Name + "_" + fnc.Name + "_cbref",
					Label: fnc.EffectiveLabel() + " \u0192",
					Type:  hexMenu.ItemAction,
					OnClick: func() {
						b.factory.SafeRun("CreateTmplCallbackRef_"+d.Name+"_"+fnc.Name, func() {
							b.factory.CreateBlackBoxCallbackRef(d, fnc.Name)
						})
					},
					Styles: styles,
				}
				applyIconToMenuItem(&refItem, fnc.EffectiveIcon(d), "play")
				userItems = append(userItems, refItem)
			}
		}
	}

	if len(userItems) == 0 {
		log.Printf("[MenuBuilder] template %q devices have no methods", tmpl.Meta.Name)
		back := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
		back.Styles = styles
		return []hexMenu.MenuItem{back}
	}

	return rulesMainMenu.ApplyRadialLayout(userItems, styles)
}

// =====================================================================
//
//	Factory registry — maps slot_id to MenuItem factory functions.
//
// =====================================================================

// registerFactories populates b.factories with a factory function for every
// system menu item. Each factory creates a hexMenu.MenuItem with the correct
// OnClick callback and default icon. The label is passed in from the server
// tree — if empty, the factory uses the English fallback.
//
// The factory does NOT set Submenu — that is built by buildNodeFromTree
// from the tree's Children. The factory also does not set Col/Row since
// the DOM-based Panel ignores those (they were for the old hex grid).
//
// Called once from NewMenuBuilder.
func (b *MenuBuilder) registerFactories() {
	styles := rulesMainMenu.MenuStyles()

	// ── Root-level submenus ──────────────────────────────────────────────
	b.factories["SysMath"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysMath", Label: label,
			FontAwesomePath: rulesIcon.KFASquareRootVariable,
			ViewBox:         "0 0 640 640",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	b.factories["SysLogic"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysLogic", Label: label,
			FontAwesomePath: rulesIcon.KFaSquareBinary,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	b.factories["SysLoop"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysLoop", Label: label,
			FontAwesomePath: rulesIcon.KFARepeat,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	// Data — the maker-data category PARENT. System SUBMENU slots need a
	// factory too (buildSystemNode skips unknown slot_ids — the 2026-07-13
	// field lesson: only the leaf was registered and the whole category
	// vanished from the rail); children are attached from the tree.
	// Português: O PAI da categoria de dados. Submenus de sistema também
	// exigem factory (buildSystemNode pula slot_ids desconhecidos — a
	// lição de campo de 2026-07-13: só a folha foi registrada e a
	// categoria inteira sumiu do rail); os filhos vêm da árvore.
	b.factories["SysData"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysData", Label: label,
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	b.factories["SysConst"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConst", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	b.factories["SysDisplay"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysDisplay", Label: label,
			FontAwesomePath: rulesIcon.KFADesktop,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	b.factories["SysExport"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysExport", Label: label,
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}

	// ── Root-level action items ──────────────────────────────────────────
	b.factories["SysSettings"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysSettings", Label: label,
			FontAwesomePath: rulesIcon.KFAGear,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				if b.liveConfigFn != nil {
					go b.liveConfigFn()
				} else {
					log.Printf("[MainMenu] Settings clicked (no handler)")
				}
			},
			Styles: styles,
		}
	}

	// Exit uses special red styles.
	exitStyles := [5]hexMenu.IconStyle{
		{ColorIcon: "#FFFFFF", ColorBorder: "#7a2020", ColorLabel: "#cc6666", ColorBackground: "#3a1515"},
		{ColorIcon: "#777777", ColorBorder: "#666666", ColorLabel: "#888888", ColorBackground: "#2A2A40"},
		{ColorIcon: "#FFFFFF", ColorBorder: "#cc3333", ColorLabel: "#FFFFFF", ColorBackground: "#661111"},
		{ColorIcon: "#FFFFFF", ColorBorder: "#cc3333", ColorLabel: "#FFFFFF", ColorBackground: "#661111"},
		{ColorIcon: "#FFFFFF", ColorBorder: "#FFFFFF", ColorLabel: "#FFFFFF", ColorBackground: "#cc3333"},
	}
	b.factories["SysExit"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID:              "SysExit",
			Label:           label,
			FontAwesomePath: "M505 273c9.4-9.4 9.4-24.6 0-33.9L361 95c-6.9-6.9-17.2-8.9-26.2-5.2S320 102.3 320 112l0 80-112 0c-26.5 0-48 21.5-48 48l0 32c0 26.5 21.5 48 48 48l112 0 0 80c0 9.7 5.8 18.5 14.8 22.2s19.3 1.7 26.2-5.2L505 273zM160 96c17.7 0 32-14.3 32-32s-14.3-32-32-32L96 32C43 32 0 75 0 128L0 384c0 53 43 96 96 96l64 0c17.7 0 32-14.3 32-32s-14.3-32-32-32l-64 0c-17.7 0-32-14.3-32-32l0-256c0-17.7 14.3-32 32-32l64 0z",
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				parent := js.Global().Get("parent")
				if parent.Truthy() && !parent.Equal(js.Global()) {
					msg := js.Global().Get("Object").New()
					msg.Set("type", "IDE_EXIT")
					parent.Call("postMessage", msg, js.Global().Get("location").Get("origin"))
				} else {
					exitFn := js.Global().Get("_ideExit")
					if exitFn.Truthy() {
						exitFn.Invoke()
					}
				}
			},
			Styles: exitStyles,
		}
	}

	// SysMyItems — "My Items" submenu. The actual children (user's devices and
	// templates) are populated by buildMyItems() when encountered at root level.
	// This factory provides a base item for the edge case where the admin nests
	// SysMyItems inside another submenu — buildSystemNode would call it.
	b.factories["SysMyItems"] = func(label string) hexMenu.MenuItem {
		ic := rulesIcon.IconByNameOrDefault("box-open", "box-open")
		return hexMenu.MenuItem{
			ID:              "SysMyItems",
			Label:           label,
			FontAwesomePath: ic.Path,
			ViewBox:         ic.ViewBox,
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}

	// ── Math children ────────────────────────────────────────────────────
	b.factories["SysAdd"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysAdd", Label: label,
			FontAwesomePath: rulesIcon.KFAPlus,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateAdd", b.factory.CreateAdd) },
			Styles:          styles,
		}
	}
	b.factories["SysSub"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysSub", Label: label,
			FontAwesomePath: rulesIcon.KFAMinus,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateSub", b.factory.CreateSub) },
			Styles:          styles,
		}
	}
	b.factories["SysMul"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysMul", Label: label,
			FontAwesomePath: rulesIcon.KFAXmark,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateMul", b.factory.CreateMul) },
			Styles:          styles,
		}
	}
	b.factories["SysDiv"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysDiv", Label: label,
			FontAwesomePath: rulesIcon.KFADivide,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateDiv", b.factory.CreateDiv) },
			Styles:          styles,
		}
	}

	// ── Logic children ───────────────────────────────────────────────────
	b.factories["SysEqualTo"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysEqualTo", Label: label,
			FontAwesomePath: rulesIcon.KFaEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateEqualTo", b.factory.CreateEqualTo) },
			Styles:          styles,
		}
	}
	b.factories["SysNotEqualTo"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysNotEqualTo", Label: label,
			FontAwesomePath: rulesIcon.KFaNotEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateNotEqualTo", b.factory.CreateNotEqualTo) },
			Styles:          styles,
		}
	}
	b.factories["SysLessThan"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysLessThan", Label: label,
			FontAwesomePath: rulesIcon.KFaLessThan,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLessThan", b.factory.CreateLessThan) },
			Styles:          styles,
		}
	}
	b.factories["SysLessThanOrEqualTo"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysLessThanOrEqualTo", Label: label,
			FontAwesomePath: rulesIcon.KFaLessThanEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLessThanOrEqualTo", b.factory.CreateLessThanOrEqualTo) },
			Styles:          styles,
		}
	}
	b.factories["SysGreaterThan"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysGreaterThan", Label: label,
			FontAwesomePath: rulesIcon.KFaGreaterThan,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGreaterThan", b.factory.CreateGreaterThan) },
			Styles:          styles,
		}
	}
	b.factories["SysGreaterThanOrEqualTo"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysGreaterThanOrEqualTo", Label: label,
			FontAwesomePath: rulesIcon.KFaGreaterThanEqual,
			ViewBox:         "0 0 640 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGreaterThanOrEqualTo", b.factory.CreateGreaterThanOrEqualTo) },
			Styles:          styles,
		}
	}
	b.factories["SysCaseItem"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysCaseItem", Label: label,
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateCase", b.factory.CreateCase) },
			Styles:          styles,
		}
	}

	// ── Loop children ────────────────────────────────────────────────────
	b.factories["SysLoopItem"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysLoopItem", Label: label,
			FontAwesomePath: rulesIcon.KFARepeat,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLoop", b.factory.CreateLoop) },
			Styles:          styles,
		}
	}
	b.factories["SysLoopDurationItem"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysLoopDurationItem", Label: label,
			FontAwesomePath: rulesIcon.KFAHourglassHalf,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLoopDuration", b.factory.CreateLoopDuration) },
			Styles:          styles,
		}
	}

	// ── Data children ─────────────────────────────────────────────────────
	b.factories["SysDataFile"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysDataFile", Label: label,
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateDataFile", b.factory.CreateDataFile) },
			Styles:          styles,
		}
	}
	b.factories["SysDataText"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysDataText", Label: label,
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateDataText", b.factory.CreateDataText) },
			Styles:          styles,
		}
	}

	// ── Const children ───────────────────────────────────────────────────
	b.factories["SysConstInt"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstInt", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstInt", b.factory.CreateConstInt) },
			Styles:          styles,
		}
	}
	b.factories["SysConstBool"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstBool", Label: label,
			FontAwesomePath: rulesIcon.KFAToggleOn,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateBool", b.factory.CreateBool) },
			Styles:          styles,
		}
	}
	b.factories["SysConstFloat"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstFloat", Label: label,
			FontAwesomePath: rulesIcon.KFADivide,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstFloat", b.factory.CreateConstFloat) },
			Styles:          styles,
		}
	}
	b.factories["SysConstString"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstString", Label: label,
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstString", b.factory.CreateConstString) },
			Styles:          styles,
		}
	}
	b.factories["SysConstDuration"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstDuration", Label: label,
			FontAwesomePath: rulesIcon.KFAHourglassHalf,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstDuration", b.factory.CreateConstDuration) },
			Styles:          styles,
		}
	}
	b.factories["SysConstArrayInt"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstArrayInt", Label: label,
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstArrayInt", b.factory.CreateConstArrayInt) },
			Styles:          styles,
		}
	}
	b.factories["SysConstArrayFloat"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstArrayFloat", Label: label,
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstArrayFloat", b.factory.CreateConstArrayFloat) },
			Styles:          styles,
		}
	}
	b.factories["SysConstArrayString"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysConstArrayString", Label: label,
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateConstArrayString", b.factory.CreateConstArrayString) },
			Styles:          styles,
		}
	}

	// ── Array readers ─────────────────────────────────────────────────────
	// The index reader (SysIndexInt) is a system slot the DB seeds under
	// SysConst (MigrateMenuTreeIndex). Without this factory the DB-driven
	// builder would skip the slot (the "no factory for system slot_id" path).
	// Reuses the collection glyph for now, matching the seed's icon_fa.
	b.factories["SysIndexInt"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysIndexInt", Label: label,
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateIndexInt", b.factory.CreateIndexInt) },
			Styles:          styles,
		}
	}
	b.factories["SysIndexFloat"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysIndexFloat", Label: label,
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateIndexFloat", b.factory.CreateIndexFloat) },
			Styles:          styles,
		}
	}
	b.factories["SysIndexString"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysIndexString", Label: label,
			FontAwesomePath: rulesIcon.KFALayerGroup,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateIndexString", b.factory.CreateIndexString) },
			Styles:          styles,
		}
	}

	// ── Debug ─────────────────────────────────────────────────────────────
	// The Debug group (SysDebug) and its six Print sinks are system slots
	// the DB seeds at the ROOT level (MigrateMenuTreePrint). Without these
	// factories the DB-driven builder would skip the slots (the "no factory
	// for system slot_id" path) — the group node itself needs one too, its
	// children then come generically from the DB tree. Glyphs match the
	// seed's icon_fa: KFABug for the group, KFAPrint for the sinks (both
	// copied verbatim from the generated FA registry).
	//
	// Português: O grupo Debug (SysDebug) e seus seis sinks Print são slots
	// de sistema que o banco semeia na RAIZ (MigrateMenuTreePrint). Sem
	// estas factories o builder pularia os slots — o nó do grupo também
	// precisa de uma; os filhos vêm genericamente da árvore do banco.
	// Glifos casam com o icon_fa do seed: KFABug no grupo, KFAPrint nos
	// sinks (ambos verbatim do registry FA gerado).
	b.factories["SysDebug"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysDebug", Label: label,
			FontAwesomePath: rulesIcon.KFABug,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	b.factories["SysPrintInt"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysPrintInt", Label: label,
			FontAwesomePath: rulesIcon.KFAPrint,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreatePrintInt", b.factory.CreatePrintInt) },
			Styles:          styles,
		}
	}
	b.factories["SysPrintFloat"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysPrintFloat", Label: label,
			FontAwesomePath: rulesIcon.KFAPrint,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreatePrintFloat", b.factory.CreatePrintFloat) },
			Styles:          styles,
		}
	}
	b.factories["SysPrintString"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysPrintString", Label: label,
			FontAwesomePath: rulesIcon.KFAPrint,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreatePrintString", b.factory.CreatePrintString) },
			Styles:          styles,
		}
	}
	b.factories["SysPrintBool"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysPrintBool", Label: label,
			FontAwesomePath: rulesIcon.KFAPrint,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreatePrintBool", b.factory.CreatePrintBool) },
			Styles:          styles,
		}
	}
	b.factories["SysPrintByte"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysPrintByte", Label: label,
			FontAwesomePath: rulesIcon.KFAPrint,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreatePrintByte", b.factory.CreatePrintByte) },
			Styles:          styles,
		}
	}
	b.factories["SysPrintByteArray"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysPrintByteArray", Label: label,
			FontAwesomePath: rulesIcon.KFAPrint,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreatePrintByteArray", b.factory.CreatePrintByteArray) },
			Styles:          styles,
		}
	}

	// ── Variables ─────────────────────────────────────────────────────────
	b.factories["SysVar"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysVar", Label: label,
			FontAwesomePath: rulesIcon.KFASuitcase,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemSubmenu,
			Styles:          styles,
		}
	}
	b.factories["SysGetVarInt"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysGetVarInt", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGetVarInt", b.factory.CreateGetVarInt) },
			Styles:          styles,
		}
	}
	b.factories["SysGetVarFloat"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysGetVarFloat", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGetVarFloat", b.factory.CreateGetVarFloat) },
			Styles:          styles,
		}
	}
	b.factories["SysSetVarInt"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysSetVarInt", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateSetVarInt", b.factory.CreateSetVarInt) },
			Styles:          styles,
		}
	}
	b.factories["SysSetVarFloat"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysSetVarFloat", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateSetVarFloat", b.factory.CreateSetVarFloat) },
			Styles:          styles,
		}
	}
	b.factories["SysGetVarString"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysGetVarString", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGetVarString", b.factory.CreateGetVarString) },
			Styles:          styles,
		}
	}
	b.factories["SysSetVarString"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysSetVarString", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateSetVarString", b.factory.CreateSetVarString) },
			Styles:          styles,
		}
	}

	// ── Display children ─────────────────────────────────────────────────
	b.factories["SysGauge"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysGauge", Label: label,
			FontAwesomePath: rulesIcon.KFAGear,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateGauge", b.factory.CreateGauge) },
			Styles:          styles,
		}
	}
	b.factories["SysLED"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysLED", Label: label,
			FontAwesomePath: rulesIcon.KFAEye,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateLED", b.factory.CreateLED) },
			Styles:          styles,
		}
	}
	b.factories["SysBarGraph"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysBarGraph", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateBarGraph", b.factory.CreateBarGraph) },
			Styles:          styles,
		}
	}
	b.factories["SysTextDisplay"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysTextDisplay", Label: label,
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateTextDisplay", b.factory.CreateTextDisplay) },
			Styles:          styles,
		}
	}
	b.factories["SysButton"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysButton", Label: label,
			FontAwesomePath: rulesIcon.KFAPlay,
			ViewBox:         "0 0 384 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateButton", b.factory.CreateButton) },
			Styles:          styles,
		}
	}
	b.factories["SysSevenSeg"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysSevenSeg", Label: label,
			FontAwesomePath: rulesIcon.KFADesktop,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateSevenSeg", b.factory.CreateSevenSeg) },
			Styles:          styles,
		}
	}
	b.factories["SysKnob"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysKnob", Label: label,
			FontAwesomePath: rulesIcon.KFAGear,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateKnob", b.factory.CreateKnob) },
			Styles:          styles,
		}
	}
	b.factories["SysChart"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysChart", Label: label,
			FontAwesomePath: rulesIcon.KFASerial,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateChart", b.factory.CreateChart) },
			Styles:          styles,
		}
	}
	b.factories["SysChartPro"] = func(label string) hexMenu.MenuItem {
		ic := rulesIcon.IconByNameOrDefault("chart-area", "chart-area")
		return hexMenu.MenuItem{
			ID: "SysChartPro", Label: label,
			FontAwesomePath: ic.Path,
			ViewBox:         ic.ViewBox,
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateChartPro", b.factory.CreateChartPro) },
			Styles:          styles,
		}
	}
	b.factories["SysPieChart"] = func(label string) hexMenu.MenuItem {
		ic := rulesIcon.IconByNameOrDefault("chart-pie", "chart-pie")
		return hexMenu.MenuItem{
			ID: "SysPieChart", Label: label,
			FontAwesomePath: ic.Path,
			ViewBox:         ic.ViewBox,
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreatePieChart", b.factory.CreatePieChart) },
			Styles:          styles,
		}
	}
	b.factories["SysBgImage"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysBgImage", Label: label,
			FontAwesomePath: rulesIcon.KFAImage,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateBackgroundImage", b.factory.CreateBackgroundImage) },
			Styles:          styles,
		}
	}
	b.factories["SysCommStatus"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysCommStatus", Label: label,
			FontAwesomePath: rulesIcon.IconByNameOrDefault("network-wired", "gear").Path,
			ViewBox:         rulesIcon.IconByNameOrDefault("network-wired", "gear").ViewBox,
			Type:            hexMenu.ItemAction,
			OnClick:         func() { b.factory.SafeRun("CreateCommStatus", b.factory.CreateCommStatus) },
			Styles:          styles,
		}
	}

	// ── Export children ──────────────────────────────────────────────────
	b.factories["SysExportJSON"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysExportJSON", Label: label,
			FontAwesomePath: rulesIcon.KFAFileExport,
			ViewBox:         "0 0 576 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Export JSON clicked")
				if b.exportJSONFn != nil {
					b.exportJSONFn()
				}
			},
			Styles: styles,
		}
	}
	b.factories["SysExportGo"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysExportGo", Label: label,
			FontAwesomePath: rulesIcon.KFAPen,
			ViewBox:         "0 0 512 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Export → Go Code clicked")
				if b.exportFn != nil {
					b.exportFn()
				}
			},
			Styles: styles,
		}
	}
	b.factories["SysExportFiles"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysExportFiles", Label: label,
			FontAwesomePath: rulesIcon.KFABars,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Files clicked")
				if b.filesFn != nil {
					b.filesFn()
				}
			},
			Styles: styles,
		}
	}
	b.factories["SysExportImage"] = func(label string) hexMenu.MenuItem {
		return hexMenu.MenuItem{
			ID: "SysExportImage", Label: label,
			FontAwesomePath: rulesIcon.KFAImage,
			ViewBox:         "0 0 448 512",
			Type:            hexMenu.ItemAction,
			OnClick: func() {
				log.Printf("[MainMenu] Export → Image clicked")
				if b.imageFn != nil {
					b.imageFn()
				}
			},
			Styles: styles,
		}
	}

	log.Printf("[MenuBuilder] Registered %d system factories", len(b.factories))
}
