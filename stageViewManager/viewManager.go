// stageViewManager/viewManager.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package stageViewManager

// stageViewManager — Manages workspaces with configurable view mode.
//
// English:
//
//	ViewManager owns up to two stageWorkspace.Workspace instances (frontend
//	and backend). The WorkspaceMode determines which workspaces are created:
//
//	  - WorkspaceModeBoth: both frontend and backend, with tab bar for
//	    switching, side-by-side, or stacked layout.
//	  - WorkspaceModeFrontendOnly: only frontend, full screen, no tab bar.
//	  - WorkspaceModeBackendOnly: only backend, full screen, no tab bar.
//
//	When both are enabled, three view modes are available: tabs (one at a
//	time), side-by-side (vertical split), and stacked (horizontal split).
//
// Português:
//
//	ViewManager possui até dois stageWorkspace.Workspace (frontend e backend).
//	O WorkspaceMode determina quais workspaces são criados:
//
//	  - WorkspaceModeBoth: ambos, com barra de abas.
//	  - WorkspaceModeFrontendOnly: apenas frontend, tela inteira, sem abas.
//	  - WorkspaceModeBackendOnly: apenas backend, tela inteira, sem abas.

import (
	"fmt"
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/factoryDevice"
	"github.com/helmutkemper/iotmakerio/rulesViewManager"
	"github.com/helmutkemper/iotmakerio/stageWorkspace"
	"github.com/helmutkemper/iotmakerio/stagefileclient"
	"github.com/helmutkemper/iotmakerio/translate"
)

// ViewMode defines how the two workspaces are displayed.
type ViewMode int

const (
	// ViewModeTabs shows one workspace at a time with tab switching.
	ViewModeTabs ViewMode = iota

	// ViewModeSideBySide shows both workspaces side by side (vertical split).
	ViewModeSideBySide

	// ViewModeStacked shows both workspaces stacked (horizontal split).
	ViewModeStacked
)

// WorkspaceMode defines which workspaces are created and visible.
//
// English:
//
//	Controls whether the IDE shows both workspaces (frontend + backend) with
//	tab/split switching, or a single workspace occupying the full screen
//	without a tab bar.
//
// Português:
//
//	Controla se o IDE mostra ambos os workspaces (frontend + backend) com
//	alternância de abas/split, ou um único workspace ocupando a tela inteira
//	sem barra de abas.
type WorkspaceMode int

const (
	// WorkspaceModeBoth shows both frontend and backend workspaces with
	// tab bar for switching between them. This is the default.
	//
	// Português: Mostra ambos os workspaces com barra de abas. Padrão.
	WorkspaceModeBoth WorkspaceMode = iota

	// WorkspaceModeFrontendOnly shows only the frontend workspace.
	// No tab bar is displayed. The workspace occupies the full screen.
	//
	// Português: Mostra apenas o workspace frontend. Sem barra de abas.
	// O workspace ocupa a tela inteira.
	WorkspaceModeFrontendOnly

	// WorkspaceModeBackendOnly shows only the backend workspace.
	// No tab bar is displayed. The workspace occupies the full screen.
	//
	// Português: Mostra apenas o workspace backend. Sem barra de abas.
	// O workspace ocupa a tela inteira.
	WorkspaceModeBackendOnly
)

// TabBarHeight is the pixel height of the tab bar at the top.
const TabBarHeight = 36

// ViewManager manages workspaces and their view layout.
//
// English:
//
//	Frontend and Backend may be nil if the corresponding workspace is not
//	enabled by the mode. Always check before use.
//
// Português:
//
//	Frontend e Backend podem ser nil se o workspace correspondente não estiver
//	habilitado pelo mode. Sempre verifique antes de usar.
type ViewManager struct {
	Frontend *stageWorkspace.Workspace
	Backend  *stageWorkspace.Workspace

	mode      WorkspaceMode
	activeTab string // "frontend" or "backend"
	viewMode  ViewMode

	// Tab bar DOM elements (only created in WorkspaceModeBoth)
	tabBarEl       js.Value // container div
	tabFrontEl     js.Value // frontend tab button
	tabBackEl      js.Value // backend tab button
	tabSplitEl     js.Value // split toggle button
	tabStackEl     js.Value // stack toggle button
	tabLangBadgeEl js.Value // language chip on the right side of the bar

	// Screen dimensions
	screenWidth  int
	screenHeight int

	// language is the project's fixed compile target — mirrors
	// Workspace.Language on every workspace this manager owns. Set
	// once during Init from Config.Language and used by
	// createTabBar to render the chip at the right edge of the bar.
	// Tokens "c" (C99) or "go" (Go); see stagefileclient.StageFileLanguage*.
	//
	// Português: Linguagem fixa do projeto, espelhada em todos os
	// workspaces. Setada no Init e usada pelo createTabBar pra
	// renderizar o chip no canto direito.
	language string
}

// NewViewManager creates the manager. Call Init() next.
//
// English:
//
//	The mode parameter controls which workspaces are created:
//	  - WorkspaceModeBoth: both frontend and backend (with tab bar)
//	  - WorkspaceModeFrontendOnly: only frontend (full screen, no tab bar)
//	  - WorkspaceModeBackendOnly: only backend (full screen, no tab bar)
//
// Português:
//
//	O parâmetro mode controla quais workspaces são criados:
//	  - WorkspaceModeBoth: ambos frontend e backend (com barra de abas)
//	  - WorkspaceModeFrontendOnly: apenas frontend (tela inteira, sem abas)
//	  - WorkspaceModeBackendOnly: apenas backend (tela inteira, sem abas)
func NewViewManager(screenWidth, screenHeight int, mode WorkspaceMode) *ViewManager {
	vm := &ViewManager{
		mode:         mode,
		activeTab:    "backend",
		viewMode:     ViewModeTabs,
		screenWidth:  screenWidth,
		screenHeight: screenHeight,
	}

	// Only allocate workspaces that are enabled by the mode.
	// Português: Aloca apenas os workspaces habilitados pelo mode.
	if mode == WorkspaceModeBoth || mode == WorkspaceModeFrontendOnly {
		vm.Frontend = &stageWorkspace.Workspace{}
	}
	if mode == WorkspaceModeBoth || mode == WorkspaceModeBackendOnly {
		vm.Backend = &stageWorkspace.Workspace{}
	}

	// Single workspace mode: set activeTab to match.
	// Português: Modo workspace único: define activeTab correspondente.
	if mode == WorkspaceModeFrontendOnly {
		vm.activeTab = "frontend"
	}

	return vm
}

// tabBarHeight returns the effective tab bar height: TabBarHeight when both
// workspaces are enabled, 0 when only one workspace (no tab bar needed).
//
// Português: Retorna a altura efetiva da barra de abas: TabBarHeight quando
// ambos os workspaces estão habilitados, 0 quando apenas um (sem barra).
func (vm *ViewManager) tabBarHeight() int {
	if vm.mode == WorkspaceModeBoth {
		return TabBarHeight
	}
	return 0
}

// HasFrontend returns true if the frontend workspace is enabled.
//
// Português: Retorna true se o workspace frontend está habilitado.
func (vm *ViewManager) HasFrontend() bool {
	return vm.Frontend != nil
}

// HasBackend returns true if the backend workspace is enabled.
//
// Português: Retorna true se o workspace backend está habilitado.
func (vm *ViewManager) HasBackend() bool {
	return vm.Backend != nil
}

// Init creates the enabled workspaces and the tab bar (if both are enabled).
// Must be called from a goroutine (workspace Init blocks).
//
// Português: Cria os workspaces habilitados e a barra de abas (se ambos habilitados).
func (vm *ViewManager) Init(cfg stageWorkspace.Config) error {
	topOffset := vm.tabBarHeight()
	canvasH := vm.screenHeight - topOffset

	// Capture the project language up front so createTabBar (called
	// below for WorkspaceModeBoth) can render the chip. We mirror the
	// value from Config rather than reading it back from
	// Frontend.Language / Backend.Language because (a) those fields
	// are populated by their own Init calls — order would matter —
	// and (b) the two workspaces always share the same language by
	// construction, so the manager-level field is the natural owner.
	vm.language = cfg.Language

	// --- Frontend workspace ---
	if vm.Frontend != nil {
		frontCfg := cfg
		frontCfg.Name = "frontend"
		frontCfg.Label = translate.T("stageViewTabFrontend", "Frontend")
		frontCfg.Width = vm.screenWidth
		frontCfg.Height = canvasH
		frontCfg.TopOffset = topOffset

		if err := vm.Frontend.Init(frontCfg); err != nil {
			return err
		}
	}

	// --- Backend workspace ---
	if vm.Backend != nil {
		backCfg := cfg
		backCfg.Name = "backend"
		backCfg.Label = translate.T("stageViewTabBackend", "Backend")
		backCfg.Width = vm.screenWidth
		backCfg.Height = canvasH
		backCfg.TopOffset = topOffset

		if err := vm.Backend.Init(backCfg); err != nil {
			return err
		}
	}

	// Cross-wire dual-stage device support: each factory needs a
	// reference to the OTHER workspace's stage AND context-menu
	// controller. Dual devices like Gauge and ChartPro use this to
	// place sub-views on the opposite tab regardless of which side
	// they were created from.
	//
	// Why WireDualFactories instead of explicit assignments here:
	//
	//	Earlier this block set Factory.OtherStage on both sides but
	//	silently forgot Factory.OtherContextMenu — the §3.1 bug in
	//	CHARTPRO_REFACTOR.md, where the frontend context menu of
	//	ChartPro never opened because dualContextMenus() returned
	//	(backend, nil). The helper now does both pairings (and any
	//	future "Other*" field added to DeviceFactory) in one shot.
	//
	// Português: Cruza referências de stage e context-menu entre as
	// duas factories. Antes faltava OtherContextMenu — fechou o
	// §3.1.
	if vm.Frontend != nil && vm.Backend != nil {
		factoryDevice.WireDualFactories(vm.Backend.Factory, vm.Frontend.Factory)

		// Cross-wire scene export so either workspace can persist the
		// COMBINED document (both stages) on save — backup, named-file save,
		// and PNG export all fold in the sibling's devices. Each device is
		// already stage-stamped by its own serializer; on load, importScene
		// routes each one back to the workspace its Stage tag names. This is
		// what makes a project's backend logic and frontend dashboard save
		// and restore as a single unit instead of fighting over one backup.
		backend, frontend := vm.Backend, vm.Frontend
		backend.SetSiblingSceneFn(func() string { return frontend.SceneMgr.Export() })
		frontend.SetSiblingSceneFn(func() string { return backend.SceneMgr.Export() })

		// Image (PNG) import is triggered on a single workspace but must reach
		// both stages, like a backup restore: fan the extracted scene out to
		// backend then frontend, each replaying its own stage and restoring its
		// own camera. Both workspaces share the broadcaster so it works whichever
		// stage the user imports the image from.
		broadcast := func(sceneJSON string) {
			backend.ImportSceneOnly(sceneJSON)
			frontend.ImportSceneOnly(sceneJSON)
		}
		backend.SetImportBroadcastFn(broadcast)
		frontend.SetImportBroadcastFn(broadcast)

		// One backup owner: route every workspace's scene-change to the backend's
		// debounced combined save, so a change on either stage produces exactly
		// ONE backup write (the backend's) instead of two stages racing to create
		// the same "unsaved (backup)" file — which surfaced as a 409 Conflict.
		// The backend's save already folds in the frontend via captureCombinedScene.
		backend.SetBackupScheduler(backend.ScheduleBackupSave)
		frontend.SetBackupScheduler(backend.ScheduleBackupSave)
	}

	// --- Tab bar (only when both workspaces are enabled) ---
	if vm.mode == WorkspaceModeBoth {
		vm.createTabBar()
	}

	// --- Window resize listener ---
	vm.attachWindowResize()

	// --- Apply initial layout ---
	vm.applyLayout()

	return nil
}

// SetViewMode changes the view mode and re-applies layout.
// Only has effect in WorkspaceModeBoth.
//
// Português: Muda o modo de visualização. Só tem efeito em WorkspaceModeBoth.
func (vm *ViewManager) SetViewMode(mode ViewMode) {
	if vm.mode != WorkspaceModeBoth {
		return
	}
	vm.viewMode = mode
	vm.applyLayout()
	vm.updateTabBarHighlight()
}

// SetActiveTab switches the visible tab (only in ViewModeTabs with WorkspaceModeBoth).
//
// Português: Alterna a aba visível (apenas em ViewModeTabs com WorkspaceModeBoth).
func (vm *ViewManager) SetActiveTab(name string) {
	if vm.mode != WorkspaceModeBoth {
		return
	}
	if name != "frontend" && name != "backend" {
		return
	}
	vm.activeTab = name
	vm.applyLayout()
	vm.updateTabBarHighlight()
}

// ToggleTab switches to the other tab.
// Only has effect in WorkspaceModeBoth.
//
// Português: Alterna para a outra aba. Só tem efeito em WorkspaceModeBoth.
func (vm *ViewManager) ToggleTab() {
	if vm.mode != WorkspaceModeBoth {
		return
	}
	if vm.activeTab == "frontend" {
		vm.SetActiveTab("backend")
	} else {
		vm.SetActiveTab("frontend")
	}
}

// GetMode returns the current workspace mode.
//
// Português: Retorna o modo de workspace atual.
func (vm *ViewManager) GetMode() WorkspaceMode {
	return vm.mode
}

// OpenFile loads the scene file with the given ID into every
// workspace this manager owns (frontend and/or backend, depending on
// the mode). Returns an error if the file cannot be fetched from the
// server; per-workspace import errors are logged but do not stop the
// other workspace from loading.
//
// Why one fetch + broadcast instead of one fetch per workspace:
//
// The scene JSON is a single document. Each workspace's importScene
// reads it and creates only the devices that belong on its own
// stage — frontend takes the visual widgets, backend takes the
// logic nodes. Fetching twice would be a wasted network round-trip;
// fetching once and broadcasting is correct AND faster.
//
// This method is the public surface for any code path that wants to
// open a project without going through the file manager UI overlay.
// Today the welcome modal (main.go, Parcela 2b) is the only caller;
// future "Open by URL" or "Open recent" surfaces should funnel
// through here too rather than re-implementing the fan-out.
//
// Português: Faz UM LoadFile e distribui o scene JSON pra todos os
// workspaces. Cada workspace pega só os devices que cabem nele.
// API pública pra abrir projeto sem ir pelo file manager overlay.
func (vm *ViewManager) OpenFile(fileID string) error {
	loaded, err := stagefileclient.LoadFile(fileID)
	if err != nil {
		return err
	}

	// Broadcast to whichever workspaces are alive. Nil checks mirror
	// the rest of this file — Frontend / Backend may be nil in
	// single-pane modes (WorkspaceModeFrontendOnly /
	// WorkspaceModeBackendOnly).
	if vm.Backend != nil {
		vm.Backend.OpenWithSceneJSON(loaded.ID, loaded.Name, loaded.SceneJSON)
	}
	if vm.Frontend != nil {
		vm.Frontend.OpenWithSceneJSON(loaded.ID, loaded.Name, loaded.SceneJSON)
	}
	return nil
}

// RestoreBackup loads a backup file's scene into every workspace this
// manager owns and points each workspace's "current file" tracking
// at the ORIGINAL file the backup was made from (not at the backup
// itself). The next Ctrl+S therefore writes to the original row,
// and the backup is cleaned up by the existing OnAfterSave → delete
// flow.
//
// Same one-fetch-then-broadcast pattern as OpenFile: a single
// LoadFile pulls the backup's scene JSON, then each Workspace
// resolves the original name on its own via RestoreBackupWithSceneJSON.
//
// Per-workspace original-lookup is slightly wasteful (each Workspace
// does its own ListFiles to find the original) but kept that way for
// symmetry with the rest of the file's broadcast pattern. The cost is
// one extra list call per workspace at boot, on the slow path
// (manual restore) — negligible compared to the user's reaction time.
//
// Português: Carrega scene do backup, distribui pros workspaces.
// Cada workspace aponta currentFileID pro ORIGINAL (não pro backup);
// Ctrl+S sobrescreve original e backup é apagado no OnAfterSave.
func (vm *ViewManager) RestoreBackup(backupID string) error {
	loaded, err := stagefileclient.LoadFile(backupID)
	if err != nil {
		return err
	}

	if vm.Backend != nil {
		vm.Backend.RestoreBackupWithSceneJSON(loaded.ID, loaded.Name, loaded.SceneJSON)
	}
	if vm.Frontend != nil {
		vm.Frontend.RestoreBackupWithSceneJSON(loaded.ID, loaded.Name, loaded.SceneJSON)
	}
	return nil
}

// applyLayout sets canvas visibility, position, and size based on current mode.
//
// English:
//
//	In single workspace mode, the active workspace gets the full screen
//	(no tab bar offset). In both mode, the layout depends on the ViewMode.
//
// Português:
//
//	Em modo workspace único, o workspace ativo ocupa a tela inteira (sem offset
//	da barra de abas). Em modo ambos, o layout depende do ViewMode.
func (vm *ViewManager) applyLayout() {
	topOffset := vm.tabBarHeight()
	canvasH := vm.screenHeight - topOffset

	// Single workspace modes: full screen, no tab bar.
	// Português: Modos workspace único: tela inteira, sem barra de abas.
	if vm.mode == WorkspaceModeFrontendOnly {
		vm.Frontend.Resize(vm.screenWidth, canvasH, topOffset, "0")
		vm.Frontend.SetVisible(true)
		return
	}
	if vm.mode == WorkspaceModeBackendOnly {
		vm.Backend.Resize(vm.screenWidth, canvasH, topOffset, "0")
		vm.Backend.SetVisible(true)
		return
	}

	// Both mode: layout depends on ViewMode.
	// Português: Modo ambos: layout depende do ViewMode.
	switch vm.viewMode {
	case ViewModeTabs:
		if vm.activeTab == "frontend" {
			vm.Frontend.Resize(vm.screenWidth, canvasH, topOffset, "0")
			vm.Frontend.SetVisible(true)
			vm.Backend.SetVisible(false)
		} else {
			vm.Backend.Resize(vm.screenWidth, canvasH, topOffset, "0")
			vm.Backend.SetVisible(true)
			vm.Frontend.SetVisible(false)
		}

	case ViewModeSideBySide:
		// Backend LEFT, Frontend RIGHT.
		halfW := vm.screenWidth / 2
		vm.Backend.Resize(halfW, canvasH, topOffset, "0")
		vm.Frontend.Resize(vm.screenWidth-halfW, canvasH, topOffset, intToPx(halfW))
		vm.Backend.SetVisible(true)
		vm.Frontend.SetVisible(true)

	case ViewModeStacked:
		// Backend TOP, Frontend BOTTOM.
		halfH := canvasH / 2
		vm.Backend.Resize(vm.screenWidth, halfH, topOffset, "0")
		vm.Frontend.Resize(vm.screenWidth, canvasH-halfH, topOffset+halfH, "0")
		vm.Backend.SetVisible(true)
		vm.Frontend.SetVisible(true)
	}
}

// createTabBar builds the tab bar DOM elements.
// Only called in WorkspaceModeBoth.
func (vm *ViewManager) createTabBar() {
	doc := js.Global().Get("document")

	// Container
	vm.tabBarEl = doc.Call("createElement", "div")
	vm.tabBarEl.Set("id", "viewTabBar")
	style := vm.tabBarEl.Get("style")
	style.Set("position", "absolute")
	style.Set("top", "0")
	style.Set("left", "0")
	style.Set("width", "100%")
	style.Set("height", intToPx(TabBarHeight))
	style.Set("zIndex", rulesViewManager.TabBarZIndex)
	style.Set("display", "flex")
	style.Set("alignItems", "center")
	style.Set("gap", rulesViewManager.TabBarGap)
	style.Set("padding", rulesViewManager.TabBarPadding)
	style.Set("background", rulesViewManager.TabBarBackground)
	style.Set("borderBottom", rulesViewManager.TabBarBorderBottom)
	style.Set("fontFamily", rulesViewManager.TabBarFontFamily)
	style.Set("fontSize", rulesViewManager.TabBarFontSize)
	style.Set("userSelect", "none")

	// Frontend tab
	vm.tabFrontEl = vm.createTabButton("Frontend", func() {
		vm.SetActiveTab("frontend")
	})

	// Backend tab
	vm.tabBackEl = vm.createTabButton("Backend", func() {
		vm.SetActiveTab("backend")
	})

	// Spacer
	spacer := doc.Call("createElement", "div")
	spacer.Get("style").Set("flex", "1")

	// NOTE: the split and stack view-mode buttons used to live here.
	// They were removed 2026-04-23 per user request — the side-by-
	// side and stacked layouts turned out not to fit the typical
	// workflow where backend and frontend are edited at different
	// times, and the icons added visual noise to the tab bar.
	//
	// The ViewMode enum (ViewModeSideBySide, ViewModeStacked) and
	// the layoutWorkspaces() branches that implement them are kept
	// intentionally — they are still correct code, just not
	// triggerable from the UI. If we ever want to bring the feature
	// back (e.g. as an advanced option in Editor Settings → Stage),
	// it is a matter of recreating the two createIconButton calls
	// and appending them here. Nothing else needs to change.
	//
	// Português: Os dois botões de split/stack foram removidos em
	// 2026-04-23 a pedido do usuário. O enum ViewMode e o código
	// de layout foram mantidos de propósito — é só recriar os
	// botões aqui para reativar a funcionalidade no futuro.

	// Backend LEFT, Frontend RIGHT.
	vm.tabBarEl.Call("appendChild", vm.tabBackEl)
	vm.tabBarEl.Call("appendChild", vm.tabFrontEl)
	vm.tabBarEl.Call("appendChild", spacer)

	// Language chip — right edge of the tab bar.
	//
	// The chip is read-only: it shows the fixed project language and
	// is never clickable. It exists so the maker always sees, at a
	// glance, which backend their work is committed to. The colour
	// signals which it is: blue for Go, coral for C99, neutral grey
	// if for any reason vm.language is empty (defensive, since the
	// welcome modal always sets one).
	//
	// Why we build the chip here rather than in a separate helper:
	//
	// The chip lives only on the tab bar, is created exactly once
	// (right after createTabBar runs), and its visual rules are tied
	// to the bar's font and height. Inlining keeps the styling
	// decisions next to the surrounding elements and makes the
	// element's lifecycle easy to follow — born here, hosted on
	// vm.tabBarEl, no later mutation.
	//
	// Português: Chip de linguagem no canto direito. Read-only,
	// criado uma única vez. Inline pra manter o styling junto da
	// barra.
	vm.tabLangBadgeEl = vm.createLanguageBadge()
	vm.tabBarEl.Call("appendChild", vm.tabLangBadgeEl)

	doc.Get("body").Call("appendChild", vm.tabBarEl)

	vm.updateTabBarHighlight()

	// Keyboard shortcut: Ctrl+Tab to toggle tabs
	doc.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			evt := args[0]
			if evt.Get("ctrlKey").Bool() && evt.Get("key").String() == "Tab" {
				evt.Call("preventDefault")
				vm.ToggleTab()
			}
			return nil
		}),
	)

	log.Printf("[ViewManager] Tab bar created")
}

func (vm *ViewManager) createTabButton(label string, onClick func()) js.Value {
	doc := js.Global().Get("document")
	btn := doc.Call("createElement", "button")
	btn.Set("textContent", label)
	style := btn.Get("style")
	style.Set("padding", rulesViewManager.TabBtnPadding)
	style.Set("border", rulesViewManager.TabBtnBorder)
	style.Set("borderRadius", rulesViewManager.TabBtnBorderRadius)
	style.Set("background", rulesViewManager.TabBtnBackground)
	style.Set("color", rulesViewManager.TabBtnColor)
	style.Set("cursor", "pointer")
	style.Set("fontFamily", rulesViewManager.TabBtnFontFamily)
	style.Set("fontSize", rulesViewManager.TabBtnFontSize)
	style.Set("outline", "none")

	btn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			onClick()
			return nil
		}),
	)

	return btn
}

// createLanguageBadge builds the small coloured pill that sits at the
// right edge of the tab bar and shows the project's fixed language.
//
// Visual mapping:
//
//   - "go"           → blue chip, label "Go"
//   - "c"            → coral chip, label "C99"  (display label, not the token)
//   - "" / unknown   → neutral grey, label "?"  (defensive — the welcome
//     modal always sets one)
//
// The function reads vm.language directly rather than taking a
// parameter because the badge is created exactly once and the
// language is fixed for the workspace lifetime. If a future feature
// needs to change the chip mid-session (which would mean the user
// changed projects — a different flow), the caller would re-create
// the badge by detaching and rebuilding the tab bar.
//
// The chip is purely informational — no click handler, no hover
// effects, no menu. Read-only visual state.
//
// Português: Monta o chip de linguagem que vai no canto direito da
// tab bar. Mapeamento: "go" → azul/"Go", "c" → coral/"C99",
// vazio/desconhecido → cinza/"?". Lê vm.language direto porque o
// chip é criado uma única vez e a linguagem é fixa. Puramente
// informativo — sem click, sem hover, sem menu.
func (vm *ViewManager) createLanguageBadge() js.Value {
	doc := js.Global().Get("document")

	// Decide label and colours from vm.language. The fallback case
	// is unreachable in normal operation (welcome modal sets the
	// language before the tab bar exists) but rendering "?" in a
	// neutral colour makes a missed wiring obvious during
	// development rather than producing a silent blank chip.
	var label, bg, color string
	switch vm.language {
	case stagefileclient.StageFileLanguageGo:
		label = "Go"
		bg = rulesViewManager.LangBadgeBgGo
		color = rulesViewManager.LangBadgeColorGo
	case stagefileclient.StageFileLanguageC:
		// "C99" is the display label; the storage token is "c". The
		// translation happens here because this is a UI surface; the
		// rest of the system never sees "C99".
		label = "C99"
		bg = rulesViewManager.LangBadgeBgC
		color = rulesViewManager.LangBadgeColorC
	default:
		label = "?"
		bg = rulesViewManager.LangBadgeBgUnknown
		color = rulesViewManager.LangBadgeColorUnknown
	}

	badge := doc.Call("createElement", "span")
	badge.Set("textContent", label)
	badge.Set("id", "viewTabLangBadge")
	style := badge.Get("style")
	style.Set("padding", rulesViewManager.LangBadgePadding)
	style.Set("borderRadius", rulesViewManager.LangBadgeBorderRadius)
	style.Set("fontSize", rulesViewManager.LangBadgeFontSize)
	style.Set("fontWeight", rulesViewManager.LangBadgeFontWeight)
	style.Set("marginRight", rulesViewManager.LangBadgeMarginRight)
	style.Set("background", bg)
	style.Set("color", color)
	style.Set("userSelect", "none")
	// Defensive: explicitly mark non-interactive so any future
	// global keyboard or click logic skips it without surprise.
	style.Set("pointerEvents", "none")

	return badge
}

func (vm *ViewManager) createIconButton(icon, title string, onClick func()) js.Value {
	doc := js.Global().Get("document")
	btn := doc.Call("createElement", "button")
	btn.Set("textContent", icon)
	btn.Set("title", title)
	style := btn.Get("style")
	style.Set("padding", rulesViewManager.IconBtnPadding)
	style.Set("border", rulesViewManager.IconBtnBorder)
	style.Set("borderRadius", rulesViewManager.IconBtnBorderRadius)
	style.Set("background", rulesViewManager.IconBtnBackground)
	style.Set("color", rulesViewManager.IconBtnColor)
	style.Set("cursor", "pointer")
	style.Set("fontSize", rulesViewManager.IconBtnFontSize)
	style.Set("outline", "none")

	btn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			onClick()
			return nil
		}),
	)

	return btn
}

// updateTabBarHighlight updates tab button styles to reflect active state.
func (vm *ViewManager) updateTabBarHighlight() {
	activeStyle := func(el js.Value, label string) {
		s := el.Get("style")
		s.Set("background", rulesViewManager.TabActiveBackground)
		s.Set("color", rulesViewManager.TabActiveColor)
		s.Set("borderBottom", rulesViewManager.TabActiveBorderBottom)
		s.Set("fontWeight", rulesViewManager.TabActiveFontWeight)
		s.Set("fontSize", rulesViewManager.TabActiveFontSize)
		s.Set("padding", rulesViewManager.TabActivePadding)
		el.Set("textContent", rulesViewManager.TabActivePrefix+label)
	}
	inactiveStyle := func(el js.Value, label string) {
		s := el.Get("style")
		s.Set("background", rulesViewManager.TabInactiveBackground)
		s.Set("color", rulesViewManager.TabInactiveColor)
		s.Set("borderBottom", rulesViewManager.TabInactiveBorderBottom)
		s.Set("fontWeight", rulesViewManager.TabInactiveFontWeight)
		s.Set("fontSize", rulesViewManager.TabInactiveFontSize)
		s.Set("padding", rulesViewManager.TabInactivePadding)
		el.Set("textContent", label)
	}
	bothActiveStyle := func(el js.Value, label string) {
		s := el.Get("style")
		s.Set("background", rulesViewManager.TabBothBackground)
		s.Set("color", rulesViewManager.TabBothColor)
		s.Set("borderBottom", rulesViewManager.TabBothBorderBottom)
		s.Set("fontWeight", rulesViewManager.TabBothFontWeight)
		s.Set("fontSize", rulesViewManager.TabBothFontSize)
		s.Set("padding", rulesViewManager.TabBothPadding)
		el.Set("textContent", label)
	}
	toggleActiveStyle := func(el js.Value, active bool) {
		if active {
			el.Get("style").Set("background", rulesViewManager.ToggleActiveBackground)
			el.Get("style").Set("color", rulesViewManager.ToggleActiveColor)
		} else {
			el.Get("style").Set("background", rulesViewManager.ToggleInactiveBackground)
			el.Get("style").Set("color", rulesViewManager.ToggleInactiveColor)
		}
	}

	if vm.viewMode == ViewModeTabs {
		if vm.activeTab == "frontend" {
			activeStyle(vm.tabFrontEl, translate.T("stageViewTabFrontend", "Frontend"))
			inactiveStyle(vm.tabBackEl, translate.T("stageViewTabBackend", "Backend"))
		} else {
			inactiveStyle(vm.tabFrontEl, translate.T("stageViewTabFrontend", "Frontend"))
			activeStyle(vm.tabBackEl, translate.T("stageViewTabBackend", "Backend"))
		}
	} else {
		bothActiveStyle(vm.tabFrontEl, translate.T("stageViewTabFrontend", "Frontend"))
		bothActiveStyle(vm.tabBackEl, translate.T("stageViewTabBackend", "Backend"))
	}

	// Split and stack buttons were removed from the tab bar (see
	// comment near createIconButton calls above), so tabSplitEl and
	// tabStackEl are zero-valued js.Value{}. Guard with Truthy() to
	// avoid a runtime panic if some future code path still calls
	// this method in a non-Tabs mode. The fields themselves are
	// kept on the struct so re-enabling the buttons later is a
	// purely local change.
	//
	// Português: Os botões foram removidos da tab bar, mas os
	// campos permanecem no struct. O guard de Truthy evita panic
	// caso este método seja chamado fora do modo Tabs no futuro.
	if vm.tabSplitEl.Truthy() {
		toggleActiveStyle(vm.tabSplitEl, vm.viewMode == ViewModeSideBySide)
	}
	if vm.tabStackEl.Truthy() {
		toggleActiveStyle(vm.tabStackEl, vm.viewMode == ViewModeStacked)
	}
}

// attachWindowResize listens for window resize events and re-applies the layout.
//
// Português:
//
//	Usa uma abordagem simples: a cada evento resize, atualiza as dimensões da tela
//	e chama applyLayout().
func (vm *ViewManager) attachWindowResize() {
	js.Global().Call("addEventListener", "resize",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			w := js.Global().Get("innerWidth").Int()
			h := js.Global().Get("innerHeight").Int()

			if w == vm.screenWidth && h == vm.screenHeight {
				return nil
			}

			vm.screenWidth = w
			vm.screenHeight = h
			vm.applyLayout()

			return nil
		}),
	)
}

// intToPx converts int to CSS "Npx" string.
func intToPx(n int) string {
	return fmt.Sprintf("%dpx", n)
}
