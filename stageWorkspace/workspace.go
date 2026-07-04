// stageWorkspace/workspace.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package stageWorkspace

// workspace.go — Encapsulates one complete IDE workspace.
//
// English:
//
//	A Workspace bundles: canvas DOM element, sprite.Stage, Camera, WireManager,
//	SceneSerializer, DeviceFactory, and MainMenu Button. Each workspace is fully
//	independent — no shared state with other workspaces.
//
//	The ViewManager creates two Workspace instances (frontend and backend) and
//	controls their visibility/layout.
//
// Export flow (single exit point — "Export" menu item, language-neutral):
//
//	export() reads the scene JSON and decides what to do:
//	  ├─ template devices on canvas?
//	  │     → sceneresolver.Resolve() traces wires and reads props
//	  │     → templateclient.GenerateAndDownload() per template found
//	  │     → browser downloads a configured project ZIP
//	  └─ custom devices only?
//	        → existing SSE codegen pipeline
//	        → generated source displayed in Monaco overlay
//
//	Wire values always take priority over Inspect panel prop values.
//
// Português:
//
//	Um Workspace agrupa: elemento canvas DOM, sprite.Stage, Camera, WireManager,
//	SceneSerializer, DeviceFactory e MainMenu Button. Cada workspace é totalmente
//	independente — sem estado compartilhado com outros workspaces.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/factoryDevice"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/rulesMainMenu"
	"github.com/helmutkemper/iotmakerio/rulesServer"
	"github.com/helmutkemper/iotmakerio/rulesSprite"
	"github.com/helmutkemper/iotmakerio/scene"
	"github.com/helmutkemper/iotmakerio/sceneresolver"
	"github.com/helmutkemper/iotmakerio/sprite"
	"github.com/helmutkemper/iotmakerio/stagePrefsClient"
	"github.com/helmutkemper/iotmakerio/stagefileclient"
	"github.com/helmutkemper/iotmakerio/stagefileui"
	"github.com/helmutkemper/iotmakerio/steganography"
	"github.com/helmutkemper/iotmakerio/templateclient"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
	"github.com/helmutkemper/iotmakerio/ui/mainMenu"
	"github.com/helmutkemper/iotmakerio/ui/overlay"
	"github.com/helmutkemper/iotmakerio/wire"
)

// Workspace holds all state for one independent IDE view (frontend or backend).
type Workspace struct {
	// Identity
	Name  string // "frontend" or "backend"
	Label string // Display label for tab: "Frontend" or "Backend"

	// DOM
	CanvasEl js.Value // <canvas> DOM element
	CanvasID string   // DOM id: "spriteCanvas_frontend"

	// canvasCtx is the 2D rendering context of CanvasEl, cached at setup
	// time. Used by the conflict-highlight pass which draws directly
	// onto the canvas during the render callback, the same way the wire
	// manager does. Keeping the reference avoids a DOM lookup per frame.
	//
	// Português: Contexto 2D do canvas, cacheado no setup. Usado pelo
	// passo de highlight de conflitos, que desenha direto no canvas.
	canvasCtx js.Value

	// Sprite system
	Stage  sprite.Stage
	Camera *sprite.Camera

	// Managers
	WireMgr  *wire.Manager
	SceneMgr *scene.Serializer

	// UI
	Menu    *mainMenu.Button
	Factory *factoryDevice.DeviceFactory

	// CtxMenu is the linear two-column context menu controller for this
	// workspace — replaces the hex menu for device body and port menus.
	// Created in Init() and handed to Factory via Factory.ContextMenu.
	// The companion workspace (frontend ↔ backend) receives it via
	// Factory.OtherContextMenu, wired from stageViewManager after both
	// Init() calls complete.
	//
	// Português: Controller do menu de contexto linear para este
	// workspace. Referenciado pelo factory para que cada device receba
	// o controller correto e, em devices duais, também o controller
	// do workspace irmão.
	CtxMenu *contextMenu.Controller

	// Shared templates (same across workspaces)
	ResizeButton  block.ResizeButton
	DraggerButton block.ResizeButton
	GridAdjust    grid.Adjust

	// loadedTemplates is the set of template definitions loaded at startup.
	// Keyed by template ID. Used by export() for canvas-based config resolution.
	loadedTemplates map[string]*templateclient.TemplateFullClient

	// connectInterceptFn is a capture-phase pointerdown interceptor that fires
	// BEFORE the sprite's event handlers during wire connect / device placement.
	connectInterceptFn js.Func

	// sharedMenu is the single SpriteHexMenu instance shared by the main menu
	// button, all device body menus, and connector menus in this workspace.
	// Stored here so connectInterceptFn can close it when a placement click
	// would otherwise be blocked from reaching the menu's own click handlers.
	//
	// Português: Instância única de SpriteHexMenu compartilhada por toda a
	// workspace. Armazenada aqui para que connectInterceptFn possa fechar o
	// menu quando um clique de posicionamento precisar atingir os handlers
	// do próprio menu.
	sharedMenu *mainMenu.SpriteHexMenu

	// ── Stage file tracking (quick save / Ctrl+S) ─────────────────────────
	//
	// Stores the ID and name of the currently open file so that "Save current"
	// and Ctrl+S can update directly without showing the name dialog.
	// Set by OnFileOpened callback when a file is opened or saved.
	//
	// Português: Armazena ID e nome do arquivo aberto para save rápido.
	currentFileID   string
	currentFileName string

	// backupFileID is the ID of the auto-created backup file.
	// When the user modifies the stage and a file is open, changes are saved
	// to "OriginalName (backup)" instead of the original. This lets the user
	// experiment freely. On manual save (Ctrl+S / Save button), the backup is
	// deleted and the original is updated.
	//
	// Português: ID do arquivo de backup automático. Mudanças no stage são
	// salvas em "NomeOriginal (backup)". No save manual, o backup é apagado.
	backupFileID string

	// backupDebounce is a timer that delays backup saves. Each scene change
	// resets the timer so we don't save on every pixel of a drag — only after
	// 3 seconds of inactivity.
	backupDebounce *time.Timer

	// siblingSceneFn, when set, returns the OTHER workspace's exported scene
	// JSON (already stage-stamped by its own serializer). The ViewManager
	// wires it at boot (WorkspaceModeBoth only) so this workspace can persist
	// the COMBINED document — both backend logic and frontend dashboard — as
	// a single unit on save. Nil in single-workspace modes and tests, in
	// which case captureCombinedScene falls back to this workspace's own scene.
	//
	// Português: Retorna o JSON da cena do OUTRO workspace (já carimbado).
	// Setado pelo ViewManager (só no modo "ambos"). Nil = modo único.
	siblingSceneFn func() string

	// importBroadcastFn, when set, replays a scene into BOTH workspaces (each
	// filtering by stage and restoring its own camera). The ViewManager wires
	// it at boot. It exists because image (PNG) import is triggered on a single
	// workspace — the steganography extraction needs that workspace's canvas —
	// but the resulting combined scene must reach both stages, exactly like a
	// backup restore. Nil in single-workspace modes, where importFromImageFile
	// falls back to a local import.
	//
	// Português: Replica uma cena nos DOIS workspaces (cada um filtra por stage
	// e restaura sua câmera). Setado pelo ViewManager. Usado no import de imagem,
	// que dispara num workspace só mas precisa alcançar os dois stages.
	importBroadcastFn func(sceneJSON string)

	// backupScheduler, when set, schedules the project's single combined backup
	// save (owned by one workspace). The ViewManager points every workspace at
	// the backend's scheduler so a change on any stage triggers exactly ONE
	// backup write, instead of two stages racing to create the same file (409).
	// Nil → this workspace schedules its own save (single-workspace mode).
	backupScheduler func()

	// stageFileCfg is the Config passed to stagefileui.Show and QuickSave.
	// Stored so Ctrl+S can call QuickSave with the same callbacks.
	stageFileCfg stagefileui.Config

	// State
	initialized bool

	// importing is true while importScene is running (including the delayed
	// refresh). Backup creation is suppressed during import — opening a file
	// should not immediately create a backup of its own content.
	importing bool

	// Language is the project's fixed compile target — "c" (C99) or
	// "go" (Go). Set once during Init from Config.Language and never
	// changed afterwards (the language of a project is irreversible
	// by design — see /server/store/stage_files.go for the
	// rationale). Read by:
	//
	//   - saveBackup, when calling stagefileclient.SaveBackupFile,
	//     so the backup row carries the same language as its parent
	//     scene (preserves the language chip on "(backup)" rows in
	//     the welcome modal);
	//
	//   - the file manager's GetProjectLanguage callback, so newly
	//     saved files inherit the workspace language;
	//
	//   - the menu builder (Parcela 5), so devices whose
	//     SupportedLanguages do not include this value are hidden
	//     from the hex menu.
	//
	// The token follows the convention used everywhere else: "c" for
	// C99 (storage), "go" for Go. UI surfaces translate to display
	// labels ("C99", "Go").
	//
	// Português: Linguagem fixa do projeto, setada no Init e nunca
	// alterada. Lida pelo saveBackup (preserva chip no backup),
	// pelo file manager (arquivos novos herdam), e pelo menu builder
	// (filtra devices incompatíveis). Token "c" ou "go".
	Language string

	// selectedTarget is the id of the hardware target the maker picked in the
	// board dropdown (e.g. "arduino_uno"), or "" when none has been picked. It
	// is stamped into the exported scene metadata by the SetTargetFunc callback,
	// and the C codegen resolves it to a type profile + string-buffer size.
	// Unlike Language (a fixed, irreversible project property), the target is a
	// changeable choice — the board picker updates it via SetSelectedTarget.
	//
	// Português: Id do target de hardware que o maker escolheu no dropdown (ex.
	// "arduino_uno"), ou "" quando nenhum. É carimbado no metadata exportado
	// pelo callback do SetTargetFunc, e o codegen C o resolve para profile +
	// buffer. Ao contrário do Language (propriedade fixa e irreversível), o
	// target é uma escolha mutável — o picker o atualiza via SetSelectedTarget.
	selectedTarget string

	// selectedBufferSize is the maker's string-buffer override, in bytes, from
	// the selected board's advanced panel, or 0 when the maker did not override
	// (the common case — the codegen keeps the board's default). Stamped into
	// the exported metadata by the SetBufferSizeFunc callback; the board picker
	// sets it via SetSelectedBufferSize.
	//
	// Português: Override do buffer do maker, em bytes, do painel avançado da
	// placa selecionada, ou 0 quando não sobrescreveu. Carimbado no export pelo
	// callback do SetBufferSizeFunc; o picker o seta via SetSelectedBufferSize.
	selectedBufferSize int
}

// Config configures a workspace before Init().
type Config struct {
	Name  string // "frontend" or "backend"
	Label string // Display label

	// Canvas dimensions (physical pixels)
	Width  int
	Height int

	// Canvas Y offset from top of viewport (for tab bar)
	TopOffset int

	// Shared templates (created once, reused)
	ResizeButton  block.ResizeButton
	DraggerButton block.ResizeButton
	GridAdjust    grid.Adjust

	// LiveConfigFn is the callback for the Settings → Live Config dialog.
	// Set by main.go before vm.Init(). Opens the live communication setup.
	// Nil when live communication is not available.
	LiveConfigFn func()

	// Language is the project's compile target, set by the welcome
	// modal flow. Tokens "c" (C99 — the default when the user closes
	// the welcome modal without choosing) or "go". Empty string is
	// tolerated and treated as "c" downstream to keep things working
	// during the bring-up of the welcome modal feature (Parcela 2),
	// but every code path that creates a Workspace should pass a
	// non-empty value once the modal lands.
	//
	// Português: Linguagem do projeto vinda do welcome modal.
	// "c" ou "go". Vazio é tolerado e resolve pra "c" — tolerância
	// pra fase de bring-up; produção deve sempre informar.
	Language string
}

// Init creates the canvas, stage, all managers, factory, and menu.
// Must be called from a goroutine (blocks during SVG caching and network fetches).
func (w *Workspace) Init(cfg Config) error {
	w.Name = cfg.Name
	w.Label = cfg.Label
	w.CanvasID = "spriteCanvas_" + cfg.Name
	w.ResizeButton = cfg.ResizeButton
	w.DraggerButton = cfg.DraggerButton
	w.GridAdjust = cfg.GridAdjust
	w.loadedTemplates = make(map[string]*templateclient.TemplateFullClient)

	// Resolve project language. An empty Config.Language is tolerated
	// during bring-up of the welcome modal (Parcela 2 wires it) and
	// defaults to "c" — same fallback the server applies when the
	// stage_files.language column is empty. Once the welcome modal
	// is in place every call site should pass a non-empty value;
	// the default here is the safety net, not the intended path.
	w.Language = cfg.Language
	if w.Language == "" {
		w.Language = stagefileclient.StageFileLanguageC
	}

	// --- Create canvas DOM element ---
	doc := js.Global().Get("document")
	w.CanvasEl = doc.Call("createElement", "canvas")
	w.CanvasEl.Set("id", w.CanvasID)
	w.CanvasEl.Set("width", cfg.Width)
	w.CanvasEl.Set("height", cfg.Height)
	w.CanvasEl.Get("style").Set("position", "absolute")
	w.CanvasEl.Get("style").Set("top", intToPx(cfg.TopOffset))
	w.CanvasEl.Get("style").Set("left", "0")
	w.CanvasEl.Get("style").Set("zIndex", "200")
	w.CanvasEl.Get("style").Set("display", "none")
	doc.Get("body").Call("appendChild", w.CanvasEl)

	// --- Load per-user stage preferences ---
	// Applies the user's saved zoom sensitivity, pan toggles, and
	// cursor hints to the rulesSprite globals before the stage and
	// camera read them. Called before sprite.NewStage so that any
	// code path that reads rulesSprite during stage construction
	// (including NewCamera, which seeds Camera.ZoomStep from the
	// global) sees the user's values.
	//
	// On ANY failure (network error, 401, server down, bad JSON)
	// the compile-time defaults in rulesSprite stay in effect.
	// The IDE must open successfully even when prefs are
	// unreachable — a bad prefs endpoint cannot be allowed to
	// lock the user out of their stage.
	//
	// Runs synchronously here because the call is already inside
	// a goroutine (per Init's contract — see the function
	// docstring), and we need the values applied before the stage
	// below reads them.
	//
	// Português: Carrega preferências do usuário antes de criar a
	// stage/câmera. Qualquer falha mantém os defaults; o IDE não
	// pode deixar de abrir por causa das prefs. Executa síncrono
	// pois Init já roda em goroutine.
	if loaded, err := stagePrefsClient.LoadStagePrefs(); err == nil && loaded != nil {
		rulesSprite.CameraZoomStep = loaded.Prefs.ZoomStep
		rulesSprite.CameraPanEmptyArea = loaded.Prefs.PanEmptyArea
		rulesSprite.CameraShowGrabCursor = loaded.Prefs.ShowGrabCursor
		log.Printf(
			"[Workspace] Applied stage prefs: zoom=%.2f panEmptyArea=%v showGrabCursor=%v",
			loaded.Prefs.ZoomStep,
			loaded.Prefs.PanEmptyArea,
			loaded.Prefs.ShowGrabCursor,
		)
	} else if err != nil {
		log.Printf("[Workspace] Stage prefs load failed, using defaults: %v", err)
	}

	// --- Create Stage ---
	var err error
	w.Stage, err = sprite.NewStage(sprite.StageConfig{
		CanvasID: w.CanvasID,
		Width:    cfg.Width,
		Height:   cfg.Height,
	})
	if err != nil {
		return err
	}

	if err = w.Stage.Start(); err != nil {
		return err
	}

	// --- Camera ---
	w.Camera = sprite.NewCamera()
	w.Camera.SetMinimapEnabled(true)
	w.Camera.SetHelpEnabled(false)
	w.Camera.SetActionEnabled(sprite.CameraActionPanLeft, true)
	w.Camera.SetActionEnabled(sprite.CameraActionPanRight, true)
	w.Camera.SetActionEnabled(sprite.CameraActionPanUp, true)
	w.Camera.SetActionEnabled(sprite.CameraActionPanDown, true)
	w.Camera.SetActionKeys(sprite.CameraActionGoToOrigin, []sprite.KeyBinding{
		{Key: "h", Ctrl: true},
	})
	w.Camera.SetKeysEnabled(true)
	w.Camera.SetKeyPanStep(80)
	w.Stage.SetCamera(w.Camera)

	// --- Wire Manager ---
	w.WireMgr = wire.NewManager()

	ctx := doc.Call("getElementById", w.CanvasID).Call("getContext", "2d")
	w.canvasCtx = ctx
	w.WireMgr.SetRenderContext(ctx)
	w.WireMgr.SetCameraFunc(func() (float64, float64, float64) {
		c := w.Stage.GetCamera()
		if c == nil {
			return 0, 0, 1
		}
		return c.OffsetX, c.OffsetY, c.Zoom
	})
	w.WireMgr.MarkDirtyFunc = func() { w.Stage.MarkDirty() }

	w.Stage.SetRenderCallback(func() {
		w.WireMgr.Draw()
		w.drawConflictHighlights()
	})

	// --- IoTMaker image example loading ---
	// When the Help tab in the Inspect panel renders markdown containing PNG
	// images with embedded stage data, a "Load Example" button appears.
	// Clicking it extracts the JSON and imports the stage.
	overlay.SetOnLoadExample(func(sceneJSON string) {
		log.Printf("[Workspace:%s] Loading example from image (%d bytes)", w.Name, len(sceneJSON))
		w.importScene(sceneJSON)
	})
	overlay.SetLoadExampleLabel(translate.T("loadExample", "▶ Load Example"))
	overlay.SetOnBeforeLoadExample(func() {
		// Close the hardware menu panel before loading the example.
		// nil check: only backend has a menu (see main-menu conditional
		// construction further down).
		if w.Menu != nil {
			w.Menu.ClosePanel()
		}
	})

	// --- Scene Serializer ---
	w.SceneMgr = scene.NewSerializer()
	w.SceneMgr.SetWireManager(w.WireMgr)
	// Stamp this workspace's identity ("backend" / "frontend") onto every
	// exported device so the combined scene document records which stage
	// each device lives on; importScene routes a device to the matching
	// workspace by this tag. Set here, before any export can run.
	w.SceneMgr.SetStage(w.Name)
	w.SceneMgr.OnExport = func(jsonStr string) {
		// Skip backup during import — opening a file should not create a
		// backup of its own content.
		if w.importing {
			return
		}

		// Route the backup save through the project's single backup owner.
		// The combined backup (both stages) is identical no matter which stage
		// changed, so one workspace owns the write; the ViewManager points every
		// workspace's scheduler at that owner. This stops the two stages from
		// racing to CREATE the same "unsaved (backup)" file (the 409 collision).
		// Unset (single-workspace mode, tests) → schedule locally.
		if w.backupScheduler != nil {
			w.backupScheduler()
			return
		}
		w.ScheduleBackupSave()
	}
	w.SceneMgr.SetCameraFunc(func() (float64, float64, float64) {
		c := w.Stage.GetCamera()
		if c == nil {
			return 0, 0, 1
		}
		return c.OffsetX, c.OffsetY, c.Zoom
	})
	w.SceneMgr.SetCanvasSizeFunc(func() (int, int) {
		return w.Stage.GetCanvasSize()
	})
	// Stamp the maker's selected hardware target onto every export. The picker
	// sets w.selectedTarget (SetSelectedTarget); empty means none, which the
	// codegen treats as the Arduino UNO default. Mirrors the camera/canvas
	// callbacks above so the serializer never reaches up into the workspace.
	//
	// Português: Carimba o target de hardware escolhido pelo maker em todo
	// export. O picker seta w.selectedTarget (SetSelectedTarget); vazio = nenhum,
	// que o codegen trata como default Arduino UNO. Espelha os callbacks de
	// câmera/canvas acima.
	w.SceneMgr.SetTargetFunc(func() string {
		return w.selectedTarget
	})
	// Stamp the maker's string-buffer override onto every export (0 when none;
	// the codegen then keeps the board's default). Mirrors SetTargetFunc above.
	w.SceneMgr.SetBufferSizeFunc(func() int {
		return w.selectedBufferSize
	})

	// Invalidate codegen diagnostic highlights the moment the scene
	// changes. Any drag / resize / wire / add / remove / import fires
	// NotifyChange, which in turn fires this callback. The render
	// loop then sees an empty codegen set on the next frame and the
	// red borders vanish — a clean visual signal that the user needs
	// to re-run codegen to validate the new state.
	//
	// A MarkDirty forces the next render tick even if no other dirty
	// source is active, so the stale highlights disappear without
	// waiting for the next frame-causing event.
	//
	// Português: Limpa highlights de codegen a qualquer mudança na
	// cena. MarkDirty força o próximo frame pra bordas vermelhas
	// sumirem imediatamente.
	w.SceneMgr.SetOnSceneChanged(func() {
		if w.SceneMgr != nil {
			w.SceneMgr.SetCodegenDiagnosticDevices(nil)
		}
		if w.Stage != nil {
			w.Stage.MarkDirty()
		}
	})

	// A device's parent changes when it is dropped into — or dragged out of —
	// a container (the scenegraph re-parents it geometrically and fires this
	// hook). Containers derive per-case membership and visibility from that
	// parentage, but Serializer.NotifyChange does not fan out to them and a
	// container's own assignNewChildren runs only on its own edits — so a
	// freshly-placed child stayed parented-but-unassigned and leaked into every
	// case until a later cycle/drag happened to run assignNewChildren. Refresh
	// BOTH the old and the new parent (the old loses the child, the new gains
	// it) so the change lands on placement instead of needing a nudge. Skipped
	// during import, where restoreImportedCases/Branches is the authority on
	// membership and would otherwise race this hook.
	//
	// Português: O pai de um device muda quando ele entra — ou sai — de um
	// container (o scenegraph re-parenteia geometricamente e dispara este hook).
	// Containers derivam a membership/visibilidade por case dessa relação, mas o
	// NotifyChange do Serializer não faz fan-out pra eles e o assignNewChildren
	// do container só roda nos eventos dele — então um filho recém-colocado
	// ficava parenteado-mas-sem-atribuição e vazava pra todos os cases até um
	// cycle/arraste. Atualiza o pai antigo E o novo (um perde o filho, o outro
	// ganha) pra mudança valer no placement. Pulado no import, onde
	// restoreImportedCases/Branches é a autoridade da membership.
	w.SceneMgr.SetOnParentChanged(func(deviceID, oldParentID, newParentID string) {
		if w.importing {
			return
		}
		for _, pid := range []string{oldParentID, newParentID} {
			if pid == "" {
				continue
			}
			if dev := w.SceneMgr.FindDevice(pid); dev != nil {
				if r, ok := dev.(interface{ RefreshMembership() }); ok {
					r.RefreshMembership()
				}
			}
		}
	})

	// Camera changes (pan, zoom, animations) trigger NotifyChange.
	// The serializer's 3-second debounce coalesces the rapid-fire
	// callbacks produced during a mouse drag or wheel zoom into a
	// single backup write — the user has to stop moving for 3
	// seconds before anything hits the network.
	//
	// Why camera-driven backups: the scene JSON's metadata block
	// already stores Camera.OffsetX/OffsetY/Zoom, so the viewport
	// IS part of the project's saved state. Without this hook, a
	// user who zoomed deep into one corner and then crashed the
	// browser would lose the zoom on restore. The previous design's
	// claim that "viewport-only changes do not affect the project"
	// was wrong — that comment lived here until the welcome-modal /
	// project-language work surfaced the regression (CLAUDE_KNOWN_ISSUES
	// §10.1).
	//
	// Português: Mudanças de camera disparam NotifyChange. Debounce
	// de 3s do serializer agrupa os callbacks frenéticos durante
	// mouse-drag/wheel-zoom em um único save. Camera (offset/zoom)
	// faz parte do scene JSON, então é parte do projeto — backup
	// tem que cobri-la.

	sceneNotifyFn := func() {
		w.SceneMgr.NotifyChange()
	}

	w.Camera.OnChange = sceneNotifyFn

	// --- Wire events ---
	w.WireMgr.OnWireCreated = func(wr *wire.Wire) {
		log.Printf("[WIRE:%s] Created: %v → %v (type: %v, id: %v)",
			w.Name, wr.From, wr.To, wr.DataType, wr.ID)
		w.SceneMgr.NotifyChange()
		// Let the destination device react to a wire landing on one of its
		// input ports. Devices that adapt to the connected wire — e.g.
		// StatementCase inferring its selector type from a bool/int/string/
		// float source — implement wireConnectObserver.
		//
		// Português: Deixa o device de destino reagir a um fio que chega numa
		// porta de entrada. Devices que se adaptam ao fio — ex.: StatementCase
		// inferindo o tipo do seletor — implementam wireConnectObserver.
		if dev := w.SceneMgr.FindDevice(wr.To.ElementID); dev != nil {
			if obs, ok := dev.(wireConnectObserver); ok {
				obs.OnWireConnected(wr.To.PortName, wr.DataType)
			}
		}
	}
	w.WireMgr.OnWireDeleted = func(wr *wire.Wire) {
		log.Printf("[WIRE:%s] Deleted: %v (was: %v → %v)",
			w.Name, wr.ID, wr.From, wr.To)
		w.SceneMgr.NotifyChange()
	}
	// OnWireRetyped mirrors OnWireCreated for an existing wire whose resolved
	// DataType changed in place (upstream device switched its output type).
	// The destination device re-infers from the new type, exactly as it would
	// on a fresh connection — this is what lets a StatementCase follow an Add
	// that flips int -> string after the wire is already connected.
	//
	// Português: OnWireRetyped espelha o OnWireCreated para um fio existente
	// cujo DataType resolvido mudou no lugar (device a montante trocou o tipo
	// de saída). O device de destino re-infere a partir do novo tipo, igual a
	// uma conexão nova — é o que permite um StatementCase acompanhar um Add
	// que vira int -> string depois do fio já estar conectado.
	w.WireMgr.OnWireRetyped = func(wr *wire.Wire) {
		log.Printf("[WIRE:%s] Retyped: %v → %v (type: %v, id: %v)",
			w.Name, wr.From, wr.To, wr.DataType, wr.ID)
		w.SceneMgr.NotifyChange()
		if dev := w.SceneMgr.FindDevice(wr.To.ElementID); dev != nil {
			if obs, ok := dev.(wireConnectObserver); ok {
				obs.OnWireConnected(wr.To.PortName, wr.DataType)
			}
		}
	}

	w.Stage.SetOnClickStage(func(event sprite.PointerEvent) {
		if w.WireMgr.IsConnecting() {
			return
		}
		wr := w.WireMgr.HitTest(event.CanvasX, event.CanvasY)
		if wr != nil {
			if wr.Selected {
				w.WireMgr.DeleteWire(wr.ID)
			} else {
				w.WireMgr.SelectWire(wr.ID)
			}
			return
		}
		w.WireMgr.DeselectAll()
	})

	w.Stage.SetOnPointerMoveStage(func(event sprite.PointerEvent) {
		// Tunnel-move mode: slide the tunnel along its border under the pointer
		// (the manager constrains it to the locked edge's axis).
		// Português: Modo de mover túnel: desliza o túnel pela borda sob o
		// ponteiro (o manager prende ao eixo da borda travada).
		if w.WireMgr.IsMovingTunnel() {
			worldX, worldY := w.screenToWorld(event.CanvasX, event.CanvasY)
			w.WireMgr.UpdateMoveTunnel(worldX, worldY)
			w.CanvasEl.Get("style").Set("cursor", "move")
			return
		}
		if !w.WireMgr.IsConnecting() {
			return
		}
		worldX, worldY := w.screenToWorld(event.CanvasX, event.CanvasY)
		w.WireMgr.SetDraftEndpoint(worldX, worldY)
		hit := w.WireMgr.HitTestConnector(worldX, worldY)
		if hit != nil {
			w.CanvasEl.Get("style").Set("cursor", "pointer")
		} else {
			w.CanvasEl.Get("style").Set("cursor", "crosshair")
		}
	})

	w.connectInterceptFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		isConnecting := w.WireMgr.IsConnecting()
		isPlacing := w.Factory != nil && w.Factory.IsPlacing()

		domEvent := args[0]
		if domEvent.Get("button").Int() != 0 {
			return nil
		}

		// Pointer position in world coordinates (shared by the branches below).
		// Português: Posição do ponteiro em coordenadas de mundo (compartilhada).
		rect := w.CanvasEl.Call("getBoundingClientRect")
		canvasW, canvasH := w.Stage.GetCanvasSize()
		cssWidth := rect.Get("width").Float()
		cssHeight := rect.Get("height").Float()
		scaleX := float64(canvasW) / cssWidth
		scaleY := float64(canvasH) / cssHeight
		cx := (domEvent.Get("clientX").Float() - rect.Get("left").Float()) * scaleX
		cy := (domEvent.Get("clientY").Float() - rect.Get("top").Float()) * scaleY
		worldX, worldY := w.screenToWorld(cx, cy)

		// Tunnel-move mode: this click drops the tunnel. Set the final position
		// and leave move mode, intercepting so it does not also open the tunnel
		// menu or start a connection.
		// Português: Modo de mover túnel: este clique solta o túnel. Fixa a
		// posição final e sai do modo, interceptando para não abrir o menu nem
		// iniciar uma conexão.
		if w.WireMgr.IsMovingTunnel() {
			domEvent.Call("stopImmediatePropagation")
			domEvent.Call("preventDefault")
			w.WireMgr.UpdateMoveTunnel(worldX, worldY)
			w.WireMgr.EndMoveTunnel()
			w.CanvasEl.Get("style").Set("cursor", "")
			return nil
		}

		if !isConnecting && !isPlacing {
			// Not connecting/placing: a click on a tunnel marker opens the
			// tunnel's context menu (Connect / Delete) — a tunnel behaves like
			// any other element in the IDE. This is a capture-phase handler, so
			// it runs before the container sprite: intercept (stop propagation)
			// only when a tunnel is actually hit, otherwise let the click
			// through to normal stage/element handling.
			//
			// Português: Sem conectar/colocar: clique no marcador de um túnel
			// abre o menu de contexto dele (Connect / Delete) — o túnel se
			// comporta como qualquer elemento da IDE. Handler em fase de captura
			// (antes do sprite do container): só intercepta quando um túnel é
			// atingido; caso contrário deixa o clique seguir normal.
			if feeder, containerID, ok := w.WireMgr.TunnelAt(worldX, worldY); ok {
				domEvent.Call("stopImmediatePropagation")
				domEvent.Call("preventDefault")
				w.openTunnelMenu(feeder, containerID, worldX, worldY)
			}
			return nil
		}

		domEvent.Call("stopImmediatePropagation")
		domEvent.Call("preventDefault")

		if isPlacing {
			// [GHOST-FIX] If the shared hex menu is open at placement time,
			// the user clicked on the menu (or near it) intending to dismiss
			// it, not to place a device. Close the menu and let the event
			// propagate so the menu's own handlers can fire normally.
			// Without this check, stopImmediatePropagation would swallow the
			// click before the menu's backdrop/item handlers could respond,
			// leaving the menu visible as a ghost on screen.
			//
			// Português: Se o menu estiver aberto durante o placement, o clique
			// era destinado ao menu. Fecha o menu e retorna sem interceptar.
			if w.sharedMenu != nil && w.sharedMenu.IsVisible() {
				w.sharedMenu.Close()
				return nil
			}
			w.Factory.ConfirmPlacement(worldX, worldY)
			w.CanvasEl.Get("style").Set("cursor", "default")
			return nil
		}

		hit := w.WireMgr.HitTestConnector(worldX, worldY)
		if hit != nil {
			wr, err := w.WireMgr.FinishConnect(*hit)
			if err != nil {
				log.Printf("[WIRE:%s] FinishConnect failed: %v", w.Name, err)
			} else {
				log.Printf("[WIRE:%s] Connected: %v → %v (type: %v)",
					w.Name, wr.From, wr.To, wr.DataType)
			}
		} else {
			w.WireMgr.CancelConnect()
			log.Printf("[WIRE:%s] Connect cancelled by user", w.Name)
		}
		return nil
	})

	w.CanvasEl.Call("addEventListener", "pointerdown", w.connectInterceptFn, true)

	w.WireMgr.OnConnectStart = func() {
		w.CanvasEl.Get("style").Set("cursor", "crosshair")
		log.Printf("[WIRE:%s] Visual connect started", w.Name)
	}
	w.WireMgr.OnConnectEnd = func() {
		w.CanvasEl.Get("style").Set("cursor", "")
		log.Printf("[WIRE:%s] Visual connect ended", w.Name)
	}
	w.WireMgr.OnConnectRejected = func(reason string) {
		// The connect found no compatible target and self-cancelled. Tell the
		// maker instead of failing silently. The raw reason (with port IDs)
		// goes to the log for debugging; the toast shows a clean message.
		log.Printf("[WIRE:%s] connect rejected: %s", w.Name, reason)
		w.showWireToast(translate.T("wireNoCompatibleTarget",
			"No compatible connection target"))
	}

	escapeFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("key").String() != "Escape" {
			return nil
		}
		if w.WireMgr.IsConnecting() {
			w.WireMgr.CancelConnect()
			log.Printf("[WIRE:%s] Connect cancelled via Escape", w.Name)
			return nil
		}
		if w.Factory != nil && w.Factory.IsPlacing() {
			w.Factory.CancelPlacement()
			w.CanvasEl.Get("style").Set("cursor", "default")
			log.Printf("[Factory:%s] Placement cancelled via Escape", w.Name)
			return nil
		}
		// Escape dismisses every kind of open menu. The three menu surfaces are
		// independent and normally only one is open at a time, but Escape closes
		// whichever are showing — no early return between them, so a single press
		// clears all of them:
		//   - the sidebar list panel (mainMenu.Button.ClosePanel, already a no-op
		//     when the panel is not open);
		//   - the radial hex menu on the stage;
		//   - the linear context menu (tunnels, frontend components).
		// Português: Esc fecha qualquer menu aberto. As três superfícies são
		// independentes e normalmente só uma fica aberta, mas o Esc fecha todas as
		// que estiverem visíveis (sem return antecipado entre elas): o painel
		// lateral, o hex menu da stage e o menu de contexto.
		if w.Menu != nil {
			w.Menu.ClosePanel()
		}
		if w.sharedMenu != nil && w.sharedMenu.IsVisible() {
			w.sharedMenu.Close()
		}
		if w.CtxMenu != nil && w.CtxMenu.IsOpen() {
			w.CtxMenu.Close()
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", escapeFunc)

	// --- Shared Hex Menu ---
	sharedMenu := mainMenu.NewSpriteHexMenu(w.Stage, rulesMainMenu.MenuConfig())
	w.sharedMenu = sharedMenu // stored for ghost-close in connectInterceptFn

	// --- Linear Context Menu controller ---
	// Replaces hex for device body and port menus. Sidebar main menu
	// still uses sharedMenu (hex). The sibling workspace's controller
	// is wired in later by stageViewManager (see OtherContextMenu).
	//
	// The canvas element is passed so the controller can translate
	// world coordinates into viewport-fixed coordinates (covering
	// the offset introduced by the sidebar and any other chrome
	// between the viewport edge and the canvas).
	//
	// Português: Controller do menu de contexto linear. O canvas é
	// necessário para traduzir coordenadas de mundo em coordenadas
	// da viewport (considerando sidebar e header).
	w.CtxMenu = contextMenu.New(w.Stage, w.CanvasEl)

	// --- Device Factory ---
	w.Factory = &factoryDevice.DeviceFactory{
		Stage:         w.Stage,
		WireMgr:       w.WireMgr,
		SceneMgr:      w.SceneMgr,
		ResizeButton:  w.ResizeButton,
		DraggerButton: w.DraggerButton,
		GridAdjust:    w.GridAdjust,
		SceneNotifyFn: sceneNotifyFn,
		Name:          w.Name,
		HexMenu:       sharedMenu,
		ContextMenu:   w.CtxMenu,
		CanvasEl:      w.CanvasEl,
		PreviewCaseFn: w.previewCaseCode,
	}

	// Wire the context-menu "copy" action to this stage's factory. Init runs
	// once per workspace (backend and frontend each have their own CtxMenu and
	// Factory), so each controller gets a handler bound to ITS factory — a copy
	// started on a backend device is placed by the backend factory, and vice
	// versa. See ui/contextMenu/copy.go and DeviceFactory.CreateCopy.
	//
	// Português: Liga a ação "copy" do menu de contexto à factory deste stage.
	// Init roda uma vez por workspace (backend e frontend têm seu próprio
	// CtxMenu e Factory), então cada controller recebe um handler ligado à SUA
	// factory.
	w.CtxMenu.SetCopyHandler(w.Factory.CreateCopy)

	// --- Main Menu ---
	exportJSONFn := func() {
		w.SceneMgr.DownloadJSON()
	}
	// exportFn is the single exit point for all code/project generation.
	// It detects what is on the canvas and routes accordingly.
	exportFn := func() {
		go w.export()
	}
	// filesFn opens the stage file manager overlay for saving, loading,
	// renaming, and deleting saved IDE scenes.
	w.stageFileCfg = stagefileui.Config{
		GetSceneJSON: func() string {
			return w.captureCombinedScene()
		},
		GetDeviceCount: func() int {
			sceneJSON := w.captureCombinedScene()
			return countDevicesInJSON(sceneJSON)
		},
		OnLoad: func(sceneJSON string) {
			log.Printf("[Workspace:%s] Importing stage (%d bytes)", w.Name, len(sceneJSON))
			w.importScene(sceneJSON)
		},
		OnFileOpened: func(fileID, fileName string) {
			// Detect backup files: when the user opens "Name (backup)", saves
			// should go to the original "Name" file, and the backup should be
			// deleted after save. This makes the backup a transparent recovery
			// mechanism — the user opens the backup to recover, then Ctrl+S
			// saves to the original as if nothing happened.
			if strings.HasSuffix(fileName, " (backup)") {
				originalName := strings.TrimSuffix(fileName, " (backup)")

				// The opened backup becomes the tracked backup for cleanup.
				w.backupFileID = fileID

				// Find the original file by name.
				files, err := stagefileclient.ListFiles("")
				if err == nil {
					for _, f := range files {
						if f.Name == originalName {
							w.currentFileID = f.ID
							w.currentFileName = originalName
							log.Printf("[Workspace:%s] Backup opened — saves redirect to original: %s (%s)",
								w.Name, originalName, f.ID)
							return
						}
					}
				}

				// Original not found (deleted?) — next Ctrl+S will open the
				// file manager dialog where the user can choose a new name.
				w.currentFileID = ""
				w.currentFileName = originalName
				log.Printf("[Workspace:%s] Backup opened — original %q not found, will prompt on save",
					w.Name, originalName)
				return
			}

			// Normal file opened.
			w.currentFileID = fileID
			w.currentFileName = fileName
			// Clear any existing backup — the user just opened a fresh file.
			w.deleteBackup()
			log.Printf("[Workspace:%s] Current file set: %s (%s)", w.Name, fileName, fileID)
		},
		GetCurrentFileID: func() string {
			return w.currentFileID
		},
		GetCurrentFileName: func() string {
			return w.currentFileName
		},
		GetProjectLanguage: func() string {
			// Forwards the workspace's fixed project language to the
			// file manager so newly saved files inherit it. The value
			// is set once during Init and never changes — this
			// closure could capture it by value, but accessing
			// w.Language keeps the read-site uniform with the rest
			// of the callbacks above (all of which dereference w).
			return w.Language
		},
		OnAfterSave: func() {
			// Manual save completed — delete the backup file since the
			// original now has the latest state.
			w.deleteBackup()
		},
		OnImportImage: func() {
			go w.openImageFilePicker()
		},
	}
	filesFn := func() {
		go stagefileui.Show(w.stageFileCfg)
	}
	menuBuilder := mainMenu.NewMenuBuilder(w.Factory, exportJSONFn, exportFn, filesFn, w.Language)
	menuBuilder.SetImageFn(func() {
		go w.exportStageImage()
	})
	if cfg.LiveConfigFn != nil {
		menuBuilder.SetLiveConfigFn(cfg.LiveConfigFn)
	}

	// --- Black-box + template + menu tree load ---
	//
	// Flow (after Phase 2 — see /ide/docs/tasks/REFACTOR_MY_ITEMS_PHASE_2.md):
	//
	//   1. Fetch the caller's own devices from /api/v1/blackbox.
	//      Every item arrives stamped Origin="own", IsOwn=true (Phase 1).
	//   2. Fetch the templates (own + public, deduplicated server-side).
	//      Each TemplateMetaClient arrives with Origin and IsOwn populated.
	//   3. Fetch the menu tree. Admin-curated sections embed device
	//      parsed_json blobs inside their device slots.
	//   4. Extract those curated devices on the client, stamping them
	//      Origin="curated" (see extractEmbeddedDefs below).
	//   5. Merge curated into the own list, deduplicating by struct name —
	//      the own record wins, so a specialist who promotes their own
	//      device into a section still sees it under "My Items".
	//   6. Call SetBlackBoxDefs and SetTemplateDefs EXACTLY ONCE each,
	//      with the full merged lists. The MenuBuilder derives its own-
	//      index from the IsOwn flags at setter time — no separate setter
	//      exists for the "My Items" list anymore. This eliminates the
	//      ordering constraint that Phase 1 had to defend with a comment.
	//
	// The Factory keeps its own map[string]*Def copy (SetBlackBoxDefs on
	// the factory side). See /ide/factoryDevice/readme.md and the Phase 2
	// design doc for the rationale behind keeping two copies — short
	// answer: two slices of pointers is trivially correct and avoids a
	// cross-package shared-source refactor.

	// Step 1 — caller's own devices.
	bbDefs := blackbox.LoadDefs()

	// Step 2 — templates. Fetched before the menu tree so their
	// per-template def is available when the tree walker builds the
	// Templates submenu.
	templateDefs := templateclient.LoadAllTemplates()
	for _, t := range templateDefs {
		w.loadedTemplates[t.Meta.ID] = t
	}

	// Step 3 — menu tree.
	railTree, _ := mainMenu.LoadMenuTree()
	if len(railTree) > 0 {
		menuBuilder.SetRailTree(railTree)
	} else {
		log.Printf("[Workspace:%s] No menu tree available, using legacy hardcoded layout",
			w.Name)
	}

	// Step 4 + 5 — extract curated devices from the tree and merge into bbDefs.
	//
	// The dedup loop below is load-bearing: it keeps the own-record first
	// for any device that appears through BOTH channels (an Official
	// Specialist promoting their own device into a section). The own
	// record has IsOwn=true; the curated record has IsOwn=false. Keeping
	// the own record wins is what makes the specialist see their promoted
	// device under "My Items" AND under the promoted section — the same
	// pointer is reachable through both menu paths.
	//
	// Do NOT remove this dedup. See
	// /ide/docs/tasks/REFACTOR_MY_ITEMS_INDEX.md decision #7.
	if len(railTree) > 0 {
		embeddedDefs := extractEmbeddedDefs(railTree)
		if len(embeddedDefs) > 0 {
			existing := make(map[string]bool, len(bbDefs))
			for _, d := range bbDefs {
				existing[d.Name] = true
			}
			added := 0
			for _, d := range embeddedDefs {
				if !existing[d.Name] {
					bbDefs = append(bbDefs, d)
					existing[d.Name] = true
					added++
				}
			}
			if added > 0 {
				log.Printf("[Workspace:%s] Merged %d embedded device def(s) from curated sections",
					w.Name, added)
			}
		}
	}

	// Step 6 — hand the final lists to the MenuBuilder and the Factory.
	//
	// SetBlackBoxDefs rebuilds the own-index internally by filtering for
	// IsOwn == true. Passing a nil/empty slice correctly resets the index,
	// which is the safe default on logout before a subsequent login.
	menuBuilder.SetBlackBoxDefs(bbDefs)
	menuBuilder.SetTemplateDefs(templateDefs)
	w.Factory.SetBlackBoxDefs(bbDefs)

	// Logging separated so the operator can correlate counts after the fact.
	// The success counts are already emitted by [blackbox/loader] Loaded N defs
	// and [Factory] BlackBox defs indexed; we only log the empty case here as
	// it indicates a misconfiguration that the operator should notice.
	if len(bbDefs) == 0 {
		log.Printf("[Workspace:%s] No black-box devices available (server unreachable or empty)",
			w.Name)
	}
	if len(templateDefs) > 0 {
		// Count owned templates for operational visibility — not a functional
		// dependency (the count is derived from IsOwn inside the builder).
		ownCount := 0
		for _, t := range templateDefs {
			if t != nil && t.Meta.IsOwn {
				ownCount++
			}
		}
		log.Printf("[Workspace:%s] Registered %d template(s) in Templates menu (%d owned by current user)",
			w.Name, len(templateDefs), ownCount)
	} else {
		log.Printf("[Workspace:%s] No templates available (auth missing or server empty)",
			w.Name)
	}

	// Wire template and device-level help to the panel — done below, after
	// bbReadmes and bbMethodHelps are declared.

	// ── Main menu button (backend-only) ───────────────────────────────────
	//
	// Only the backend workspace owns a main menu button. The frontend is
	// strictly for placing/arranging the visual dashboard — devices are
	// authored from the backend side where the wiring lives. Creating the
	// menu on the frontend would be confusing (two identical sidebars,
	// depending on which tab is active), visually noisy, and
	// architecturally misleading (it would suggest the frontend can
	// create devices independently of the backend, which is not how the
	// dual-device pattern works).
	//
	// The help-data blocks below (bbReadmes, bbMethodHelps, helpLang)
	// exist only to feed SetItemReadmes/SetItemMethodHelps on w.Menu, so
	// they live inside the same conditional — no reason to spend CPU
	// building help maps the frontend will never consume.
	//
	// When w.Name != "backend", w.Menu stays nil. Every caller of w.Menu
	// outside this block guards with `if w.Menu != nil` — see
	// ClosePanel callsite in the example-load callback above and the
	// SetVisible(true/false) callsites in exportStageImage below.
	//
	// Português: Apenas o workspace backend tem o botão de menu
	// principal. O frontend é só para posicionar o dashboard — devices
	// são criados no backend, onde o cabeamento vive. Os blocos de
	// help existem só para alimentar w.Menu, então também ficam aqui
	// dentro do condicional. Quando w.Name != "backend", w.Menu fica
	// nil, e todos os callers guardam com if w.Menu != nil.
	if w.Name == "backend" {
		// Resolve language for help content.
		// Must follow the same resolution order as helpSessionLang() in
		// devices/compBlackBox/helpLang.go so the menu and the inspect
		// overlay always agree on which language to show.
		//
		// Order: sessionStorage → localStorage "locale" → navigator.language → "en"
		helpLang := func() string {
			// 1. Session preference set by the language selector in the help deck.
			ss := js.Global().Get("sessionStorage")
			if ss.Truthy() {
				v := ss.Call("getItem", "iotmaker_help_lang")
				if v.Truthy() {
					if s := v.String(); s != "" {
						return s
					}
				}
			}

			// 2. User's explicit SPA preference from localStorage.
			ls := js.Global().Get("localStorage")
			if ls.Truthy() {
				v := ls.Call("getItem", "locale")
				if v.Truthy() {
					if s := v.String(); s != "" {
						b := []byte(s)
						for i, c := range b {
							if c >= 'A' && c <= 'Z' {
								b[i] = c + 32
							}
						}
						return string(b)
					}
				}
			}

			// 3. Browser locale from navigator.language.
			if nav := js.Global().Get("navigator"); nav.Truthy() {
				if l := nav.Get("language"); l.Truthy() {
					if s := l.String(); s != "" {
						b := []byte(s)
						for i, c := range b {
							if c >= 'A' && c <= 'Z' {
								b[i] = c + 32
							}
						}
						return string(b)
					}
				}
			}

			// 4. Hard fallback.
			return "en"
		}()

		// Build readme tabs map: "bb_{Name}" → ordered tab slice.
		// The tab slice can be empty (no readme), single (one tab,
		// rendered without bar), or multi-tab when the device ships
		// `readme.<N>.<lang>.md` files alongside the unnumbered one.
		bbReadmes := make(map[string][]blackbox.HelpTabClient, len(bbDefs))
		for _, def := range bbDefs {
			if tabs := def.HelpReadmeTabs(helpLang); len(tabs) > 0 {
				bbReadmes["bb_"+def.Name] = tabs
			}
		}

		// Build method help tabs map: "bb_{Name}_init" / "bb_{Name}_{method}" → tabs.
		// GitHub markdown tabs come first; GoDoc is always appended last.
		//
		// For the init method, when a help tab contains PlaceholderMarker, the
		// HTML comment is replaced with a static disabled-inputs preview of the
		// device's configurable properties. This gives the maker a visual preview
		// of available options before placing the component on the canvas.
		// The original tab content is NOT modified — a copy is made so the
		// overlay's live embedded form still works independently.
		bbMethodHelps := make(map[string][]blackbox.HelpTabClient)
		for _, def := range bbDefs {
			if def.HasInit() {
				tabs := def.HelpTabsFor("init", helpLang)
				if t := def.GoDocTab("Init"); t != nil {
					tabs = append(tabs, *t)
				}
				// Check if any init tab has the embedded panel placeholder.
				// If so, copy the tabs and replace the comment with a static
				// disabled-inputs preview for the menu panel.
				if len(def.Props) > 0 {
					propsHTML := buildPropsPreviewHTML(def.Props)
					menuTabs := make([]blackbox.HelpTabClient, len(tabs))
					for i, t := range tabs {
						menuTabs[i] = t
						if strings.Contains(t.Content, overlay.PlaceholderMarker) {
							menuTabs[i].Content = overlay.ReplaceHTMLCommentContaining(
								t.Content, overlay.PlaceholderMarker, propsHTML)
						}
					}
					tabs = menuTabs
				}
				if len(tabs) > 0 {
					bbMethodHelps["bb_"+def.Name+"_init"] = tabs
				}
			}
			for _, m := range def.Methods {
				tabs := def.HelpTabsFor(m.Name, helpLang)
				if t := def.GoDocTab(m.Name); t != nil {
					tabs = append(tabs, *t)
				}
				if len(tabs) > 0 {
					bbMethodHelps["bb_"+def.Name+"_"+m.Name] = tabs
				}
			}
			// C99 device-functions (decision b). Sibling loop to the Methods
			// loop above: a Go black-box has an empty Functions slice (no-op),
			// a C99 black-box has empty Init/Methods. Without this, the
			// function blocks the menu builder renders from def.Functions had
			// no help wired — selecting one showed a blank card. Keyed
			// "bb_{Name}_{fn}" to match the block ID in blackBoxFuncSubmenu.
			// HelpTabsFor matches the per-function markdown named "<fn>.<lang>.md"
			// (case-insensitive, by the function's name — not its label).
			for _, fn := range def.Functions {
				tabs := def.HelpTabsFor(fn.Name, helpLang)
				if t := def.GoDocTab(fn.Name); t != nil {
					tabs = append(tabs, *t)
				}
				if len(tabs) > 0 {
					bbMethodHelps["bb_"+def.Name+"_"+fn.Name] = tabs
				}
			}
		}

		// Wire template and device-level help into the same maps.
		//
		// Two layers per template:
		//   1. Device-level: each device inside the template has its own Help
		//      (readme + method tabs), keyed as "tmpl_{tmplID}_{devName}[_{block}]".
		//      This mirrors the "bb_{Name}" scheme for hardware devices.
		//   2. Template-level: the template itself has a Help field built from
		//      root markdown files — keyed as "tmpl_{tmplID}[_{block}]".
		for _, t := range templateDefs {
			if t.Def == nil {
				continue
			}

			// Device-level help.
			for _, dev := range t.Def.Devices {
				if dev == nil {
					continue
				}
				if tabs := dev.HelpReadmeTabs(helpLang); len(tabs) > 0 {
					bbReadmes["tmpl_"+t.Meta.ID+"_"+dev.Name] = tabs
				}
				if dev.HasInit() {
					tabs := dev.HelpTabsFor("init", helpLang)
					if tab := dev.GoDocTab("Init"); tab != nil {
						tabs = append(tabs, *tab)
					}
					if len(tabs) > 0 {
						bbMethodHelps["tmpl_"+t.Meta.ID+"_"+dev.Name+"_init"] = tabs
					}
				}
				for _, m := range dev.Methods {
					tabs := dev.HelpTabsFor(m.Name, helpLang)
					if tab := dev.GoDocTab(m.Name); tab != nil {
						tabs = append(tabs, *tab)
					}
					if len(tabs) > 0 {
						bbMethodHelps["tmpl_"+t.Meta.ID+"_"+dev.Name+"_"+m.Name] = tabs
					}
				}
			}

			// Template-level help — use a synthetic BlackBoxDefClient wrapping
			// t.Def.Help to reuse the exported HelpReadmeTabs/HelpTabsFor methods.
			tmplProxy := &blackbox.BlackBoxDefClient{Help: t.Def.Help}
			if tabs := tmplProxy.HelpReadmeTabs(helpLang); len(tabs) > 0 {
				bbReadmes["tmpl_"+t.Meta.ID] = tabs
			}
			for methodKey := range t.Def.Help.Methods {
				tabs := tmplProxy.HelpTabsFor(methodKey, helpLang)
				if len(tabs) > 0 {
					bbMethodHelps["tmpl_"+t.Meta.ID+"_"+methodKey] = tabs
				}
			}
		}

		w.Menu = new(mainMenu.Button)
		w.Menu.SetStage(w.Stage)
		w.Menu.SetCorner(mainMenu.CornerTopLeft)
		w.Menu.SetAttentionDuration(3000)
		w.Menu.SetNormalDuration(15000)
		w.Menu.SetFlashInterval(500)

		// Merge help tabs from the menu tree into bbReadmes so that
		// section-scoped categories, subcategories and any admin-edited
		// help text is available to the panel. Tree help uses the same
		// item ID that the menu builder assigns to each node.
		mergeTreeHelp(railTree, bbReadmes)

		w.Menu.SetItemReadmes(bbReadmes)
		w.Menu.SetItemMethodHelps(bbMethodHelps)
		w.Menu.SetMenuItems(menuBuilder.Build())
		if err = w.Menu.Init(); err != nil {
			return err
		}
	} else {
		log.Printf("[Workspace:%s] Main menu skipped — frontend does not own a sidebar",
			w.Name)
	}

	// ── Ctrl+S: quick save ────────────────────────────────────────────────
	// Saves the current scene to the currently open file without opening the
	// file manager overlay. If no file is open, opens the file manager.
	//
	// Only the VISIBLE workspace handles the event — both frontend and backend
	// register this listener, so without the visibility check, the inactive
	// workspace would fire first and open an unwanted dialog.
	//
	// preventDefault stops the browser's native "Save page" dialog.
	// The handler ignores keypresses when an INPUT or TEXTAREA is focused
	// so the user can still type Ctrl+S in Monaco or form fields.
	//
	// Português: Ctrl+S salva direto no arquivo aberto. Apenas o workspace
	// visível processa o evento para evitar que o workspace inativo abra
	// o dialog de save.
	doc.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			e := args[0]
			key := e.Get("key").String()
			if (key == "s" || key == "S") && e.Get("ctrlKey").Bool() {
				// Only handle if this workspace's canvas is visible.
				display := w.CanvasEl.Get("style").Get("display").String()
				if display == "none" {
					return nil
				}

				tagName := e.Get("target").Get("tagName").String()
				if tagName != "INPUT" && tagName != "TEXTAREA" {
					e.Call("preventDefault")
					go func() {
						if stagefileui.QuickSave(w.stageFileCfg) {
							// Save succeeded — delete backup.
							w.deleteBackup()
						} else {
							// No file open — open the file manager.
							stagefileui.Show(w.stageFileCfg)
						}
					}()
				}
			}
			return nil
		}))

	// ── Drag-and-drop image import ────────────────────────────────────────
	// When the user drops a PNG file onto the canvas, extract the embedded
	// scene JSON (if any) and reconstruct the stage. This enables sharing
	// stage layouts as simple image files.
	//
	// Only the visible workspace handles drops (same pattern as Ctrl+S).
	//
	// Português: Quando o usuário arrasta um PNG para o canvas, extrai o
	// JSON embutido e reconstrói o stage.
	//
	// A single document-level listener catches drops anywhere on the page
	// (canvas, dark background, etc.). The visibility check ensures only
	// the active workspace processes the drop. Canvas-level listeners are
	// not needed — they would cause double processing because the event
	// bubbles from canvas to document.
	doc.Call("addEventListener", "dragover",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			args[0].Call("preventDefault")
			return nil
		}))
	doc.Call("addEventListener", "drop",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			e := args[0]
			e.Call("preventDefault")

			display := w.CanvasEl.Get("style").Get("display").String()
			if display == "none" {
				return nil
			}

			files := e.Get("dataTransfer").Get("files")
			if files.Length() == 0 {
				return nil
			}
			file := files.Index(0)
			if file.Get("type").String() != "image/png" {
				return nil
			}

			go w.importFromImageFile(file)
			return nil
		}))

	w.initialized = true

	// ── Backup recovery is handled by the welcome modal ───────────────────
	//
	// The previous design auto-triggered checkAndRestoreBackup here in a
	// goroutine, which loaded the most recent backup and showed a popup
	// once the user had landed in the workspace. With the welcome modal
	// (Parcela 2c) the backup detection moved upstream: the modal
	// surfaces the backup as a card BEFORE the workspace is even built,
	// and the maker decides explicitly whether to restore it.
	//
	// We deliberately do NOT call checkAndRestoreBackup here anymore.
	// The function still exists in this file for now as a reference
	// for the restore steps (LoadFile → find original by name →
	// importScene → set currentFile) — RestoreBackupWithSceneJSON
	// below implements the same flow as a public entry point, fed by
	// the welcome modal via ViewManager.RestoreBackup.
	//
	// Português: A recuperação de backup migrou pro welcome modal
	// (Parcela 2c). Ele detecta o backup ANTES do workspace existir
	// e o usuário decide explicitamente se restaura. Não chamamos
	// mais o auto-restore aqui — RestoreBackupWithSceneJSON é o
	// novo entry point, alimentado por ViewManager.RestoreBackup.

	return nil
}

// ── Visibility / Resize ───────────────────────────────────────────────────────

func (w *Workspace) SetVisible(visible bool) {
	if visible {
		w.CanvasEl.Get("style").Set("display", "block")
		if w.Stage != nil {
			w.Stage.MarkDirty()
		}
	} else {
		w.CanvasEl.Get("style").Set("display", "none")
	}
}

func (w *Workspace) Resize(width, height, topOffset int, cssLeft string) {
	curW := w.CanvasEl.Get("width").Int()
	curH := w.CanvasEl.Get("height").Int()
	dimensionsChanged := curW != width || curH != height

	w.CanvasEl.Get("style").Set("width", intToPx(width))
	w.CanvasEl.Get("style").Set("height", intToPx(height))
	w.CanvasEl.Get("style").Set("top", intToPx(topOffset))
	w.CanvasEl.Get("style").Set("left", cssLeft)

	if dimensionsChanged && w.Stage != nil {
		w.Stage.SetCanvasSize(width, height)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func intToPx(n int) string {
	return js.ValueOf(n).String() + "px"
}

// openTunnelMenu shows the context menu for a tunnel at its world position. A
// tunnel is a frontend-only routing point on a container border; it behaves like
// an element here, offering Connect (start a wire from the tunnel's feeder
// source, so the maker can route it into a device in the active case) and Delete
// (remove every wire through the tunnel). The wires are logical source→device
// connections, so codegen is unaffected either way.
//
// Português: Mostra o menu de contexto de um túnel na sua posição de mundo. O
// túnel é um ponto de roteamento só de frontend na borda de um container; aqui
// ele se comporta como elemento, com Connect (inicia um fio a partir da fonte
// que o alimenta, para o maker ligar a um device da condição ativa) e Delete
// (remove todos os fios do túnel). Os fios são conexões lógicas fonte→device,
// então o codegen não muda.
func (w *Workspace) openTunnelMenu(feeder wire.ConnectorID, containerID string, worldX, worldY float64) {
	if w.CtxMenu == nil {
		return
	}
	items := []contextMenu.Item{
		{
			ID:              "tunnel_connect",
			Label:           translate.T("menuDeviceConnect", "Connect"),
			FontAwesomePath: rulesIcon.KFALink,
			ViewBox:         "0 0 640 512",
			HelpKey:         "helpMenuTunnelConnect",
			HelpFallback:    "Starts visual connect mode from this tunnel. Compatible connectors light up on the stage; click one to wire it through the tunnel.",
			OnClick: func() {
				if candidates := w.WireMgr.StartConnect(feeder); len(candidates) == 0 {
					w.WireMgr.CancelConnect()
					log.Printf("[WIRE:%s] No compatible target for tunnel %v.%v",
						w.Name, feeder.ElementID, feeder.PortName)
				}
			},
		},
		{
			ID:              "tunnel_move",
			Label:           translate.T("menuTunnelMove", "Move"),
			FontAwesomePath: rulesIcon.KFAArrowsUpDownLeftRight,
			ViewBox:         "0 0 512 512",
			HelpKey:         "helpMenuTunnelMove",
			HelpFallback:    "Drag the tunnel along its border — up/down on the side edges, left/right on the top and bottom. Click to drop it.",
			OnClick: func() {
				w.WireMgr.StartMoveTunnel(feeder, containerID)
			},
		},
		{
			ID:              "tunnel_delete",
			Label:           translate.T("menuTunnelDelete", "Delete"),
			FontAwesomePath: rulesIcon.KFATrashCan,
			ViewBox:         "0 0 448 512",
			Danger:          true,
			HelpKey:         "helpMenuTunnelDelete",
			HelpFallback:    "Removes every wire routed through this tunnel.",
			OnClick: func() {
				n := w.WireMgr.DeleteTunnelWires(feeder, containerID)
				log.Printf("[WIRE:%s] Tunnel deleted: %d wire(s) from %v.%v",
					w.Name, n, feeder.ElementID, feeder.PortName)
			},
		},
	}
	w.CtxMenu.OpenAtWorld(items, worldX, worldY)
}

func (w *Workspace) screenToWorld(screenX, screenY float64) (worldX, worldY float64) {
	cam := w.Stage.GetCamera()
	if cam == nil {
		return screenX, screenY
	}
	zoom := cam.Zoom
	if zoom == 0 {
		zoom = 1
	}
	worldX = screenX/zoom + cam.OffsetX
	worldY = screenY/zoom + cam.OffsetY
	return
}

func NewSharedResizeButton() block.ResizeButton {
	btn := new(block.ResizeButtonHexagon)
	btn.SetSize(10)
	btn.SetSpace(30)
	btn.SetSides(6)
	btn.SetFillColor("red")
	btn.SetStrokeColor("green")
	btn.SetStrokeWidth(2)
	return btn
}

func NewSharedDraggerButton() block.ResizeButton {
	btn := new(block.ResizeButtonHexagon)
	btn.SetSize(20)
	btn.SetSpace(10)
	btn.SetSides(3)
	btn.SetFillColor(color.RGBA{R: 0x00, G: 0x80, B: 0x00, A: 0x20})
	btn.SetStrokeColor("none")
	btn.SetStrokeWidth(2)
	return btn
}

// =====================================================================
//  Export — single exit point for all code / project generation.
//
//  Called when the maker clicks "Export". The menu label does not name
//  the language — that choice was made in the welcome modal at project
//  creation and surfaces here only via w.Language.
//
//  Detection:
//    The scene JSON is inspected once. If any device on the canvas
//    belongs to a loaded template, the template path is taken.
//    Otherwise the regular SSE codegen pipeline runs.
//
//  Template path:
//    sceneresolver.Resolve() traces each template variable through the
//    canvas graph: wire value > Inspect panel prop > template default.
//    For each resolved template a ZIP is downloaded immediately.
//
//  Custom device path:
//    The SSE codegen pipeline runs against POST /api/v1/codegen/:language
//    (language picked from w.Language), and the generated source is
//    displayed in a Monaco overlay with the matching syntax highlighter.
// =====================================================================

// export is the single export entry point, called from the "Export"
// menu item. Must be called from a goroutine.
func (w *Workspace) export() {
	sceneJSON := w.SceneMgr.Export()
	if sceneJSON == "{}" {
		log.Printf("[Export] Scene is empty, nothing to export")
		overlay.ShowError("Export", "Scene is empty — add devices and connect wires first.")
		return
	}

	// Detect whether any template device is present on the canvas.
	if sceneresolver.HasTemplateDevices(sceneJSON, w.loadedTemplates) {
		w.exportTemplates(sceneJSON)
		return
	}

	// For a C project, let the maker confirm (or change) the target board before
	// generating. The picker pre-selects their last choice, so the common case
	// is a single "Generate" click. On choosing, we stamp the board onto the
	// workspace and RE-EXPORT the scene — the fresh JSON now carries
	// Metadata.Target, which the C backend resolves to a type profile plus a
	// RAM-sized string buffer. A Go project skips the picker entirely (Go uses
	// native types and ignores the target); and if the picker cannot open it
	// generates anyway with the existing choice, so it never blocks generation.
	//
	// Português: Num projeto C, deixa o maker confirmar (ou trocar) a placa antes
	// de gerar. O picker pré-seleciona a última escolha — no caso comum é um
	// clique em "Generate". Ao escolher, carimba a placa no workspace e
	// RE-EXPORTA a cena — o JSON novo carrega Metadata.Target, que o backend C
	// resolve para profile + buffer. Projeto Go pula o picker (usa tipos nativos,
	// ignora o target); e se o picker não abrir, gera com a escolha atual, então
	// nunca bloqueia a geração.
	if w.Language == "c" {
		mainMenu.ShowTargetPicker(w.SelectedTarget(), func(id string, bufferBytes int) {
			w.SetSelectedTarget(id)
			// The maker's string-buffer override, in bytes (0 = keep the board's
			// default). Set before the re-export below so it lands in the scene
			// metadata next to the target, and the codegen applies it.
			w.SetSelectedBufferSize(bufferBytes)
			// generateCode blocks waiting on the codegen SSE stream. This
			// callback fires from the picker's "Generate" button — i.e. on the
			// browser's event loop, NOT the goroutine export() was called from.
			// Blocking the event loop here stalls the very fetch generateCode
			// issues, surfacing as a network error. Build the scene here (a cheap
			// synchronous read) and run generateCode on its own goroutine so the
			// event loop stays free to drive the fetch.
			//
			// Português: generateCode bloqueia esperando o stream SSE do codegen.
			// Este callback vem do botão "Generate" do picker — ou seja, no event
			// loop do browser, NÃO na goroutine de onde export() foi chamado.
			// Bloquear o event loop aqui trava o próprio fetch do generateCode e
			// vira erro de rede. Constrói a cena aqui (leitura síncrona barata) e
			// roda o generateCode em goroutine própria para o event loop ficar
			// livre pro fetch.
			sceneJSON := w.SceneMgr.Export()
			go w.generateCode(sceneJSON)
		})
		return
	}

	// Go project — generate directly. Language is read from w.Language inside
	// generateCode.
	w.generateCode(sceneJSON)
}

// exportTemplates resolves each template's configuration from the canvas and
// triggers a browser download per template found.
//
// Wire values take priority over Inspect panel props for each variable.
func (w *Workspace) exportTemplates(sceneJSON string) {
	resolved := sceneresolver.Resolve(sceneJSON, w.loadedTemplates)
	if len(resolved) == 0 {
		log.Printf("[Export] No resolved templates — falling back to codegen")
		w.generateCode(sceneJSON)
		return
	}

	for _, r := range resolved {
		log.Printf("[Export] Generating ZIP for template: %s (id: %s)", r.TemplateName, r.TemplateID)
		templateclient.GenerateAndDownload(r.TemplateID, r.TemplateName, r.Config)
	}
}

// sseResult is the terminal outcome of one codegen run, as observed by
// the main generateCode goroutine waiting on its sseDone channel.
// Exactly one field describes the meaningful payload:
//
//	cancelled=true       — user clicked the Cancel button in the overlay
//	infraErr != ""       — pipeline failed (server error, network drop,
//	                       stream parse error, etc.); message is user-visible
//	code != "" or
//	  diagnostics != nil — happy path or warnings-only path; the
//	                       diagnostics slice may carry severity="error"
//	                       items, in which case code will be empty
//
// Exported within the package because streamCodegenResult writes to a
// channel of this type from a separate goroutine.
//
// Português:
//
//	Resultado terminal de uma execução de codegen, vindo da goroutine do
//	stream para a goroutine principal via canal sseDone. Apenas um dos
//	campos descreve o resultado relevante; o resto fica zerado.
type sseResult struct {
	code string
	// files holds multi-file output from backends that produce more
	// than one source artefact per generation. Today only the C99
	// backend uses this — it returns main.c plus runtime header and
	// stub files (see server/codegen/backend/ansic/emit.go). The Go
	// backend leaves files nil and writes everything into code.
	//
	// The two paths are mutually exclusive on the server side, so
	// the client treats them as alternatives: when files is
	// populated, the display layer reads from it and ignores code.
	files       map[string]string
	ir          string
	errors      string
	infraErr    string
	diagnostics []overlay.Diagnostic
	cancelled   bool // set when the Cancel button fired
}

// generateCode sends the scene JSON to the server's codegen endpoint and
// displays the generated source in a Monaco overlay. The endpoint is
// picked by the workspace's project language — POST /api/v1/codegen/go
// for Go projects, POST /api/v1/codegen/c for C99 projects. The
// language is fixed at project creation and stored on Workspace.Language,
// so a single click invokes the correct backend without prompting.
//
// This is the single export exit point for projects with only custom
// devices (no template devices). Template projects go through
// exportTemplates instead; they own their own ZIP pipeline that
// doesn't run through SSE codegen at all.
//
// Previously named generateGoCode, when the only supported target was
// Go. Renamed in Parcela 6 (Export C99) — the body is otherwise
// unchanged except for the submitURL line below and the log labels.
//
// Authentication:
//
//	All four codegen routes (POST submit, POST cancel, GET stream, GET status)
//	live behind spaauth.RequireBearerToken on the server. We read the JWT from
//	rulesServer.GetAuthToken() — which itself reads window._ideAuthToken set
//	by the SPA host page after login — and include it as the Authorization
//	header on every codegen call. Empty token short-circuits to a user-facing
//	overlay error rather than letting the request bounce off the middleware
//	with a useless 401.
//
// Result delivery — fetch + ReadableStream, NOT EventSource:
//
//	The native EventSource API cannot send custom request headers. We
//	previously used it because SSE auto-reconnect was nice to have, but
//	now that the server gates the stream behind Bearer auth, EventSource
//	is no longer viable. We replace it with fetch() + body.getReader(),
//	which:
//	  • supports the Authorization header (our reason for switching),
//	  • lets us own the read loop (cancel via reader.cancel() is direct),
//	  • loses native auto-reconnect (acceptable — the previous flow
//	    treated every disconnect as cancel anyway, so reconnect was
//	    cosmetic at best).
//
//	The SSE wire format on the server stays identical; only the parser
//	moves to our side. See streamCodegenResult and parseSSEFrame below.
//
// Português:
//
//	Autentica via Bearer JWT em todas as quatro rotas de codegen. Troca
//	EventSource por fetch + ReadableStream porque o EventSource não
//	suporta headers customizados, e agora todas as rotas exigem
//	Authorization. A perda de auto-reconexão do EventSource é aceitável
//	(o fluxo já cancelava em qualquer disconnect).
func (w *Workspace) generateCode(sceneJSON string) {
	// Pick the codegen endpoint from the project's language. The
	// server mounts POST /api/v1/codegen/:language as the single
	// submit handler (see handler/codegen/register.go); both "go"
	// and "c" route to backends shipped by Tasks 1–11 of the C99
	// codegen plan, so the WASM side just needs to point at the
	// right path. An empty language falls back to "go" — matches
	// the schema default in stage_files.language and keeps older
	// projects (created before Parcela 1) working without a
	// migration.
	//
	// Português: Endpoint do codegen vem da linguagem do projeto.
	// Server tem rota parametrizada; vazio cai em "go" (default
	// do schema), pra compatibilidade com projetos antigos.
	language := w.Language
	if language == "" {
		language = "go"
	}
	submitURL := "/api/v1/codegen/" + language

	log.Printf("[Codegen:%s] Submitting %d bytes to %s", language, len(sceneJSON), submitURL)
	reqBody := fmt.Sprintf(`{"scene":%s}`, sceneJSON)

	// Submit + stream through the shared codegen core. runCodegenJob owns
	// authentication, the POST, the progress overlay with its Cancel button,
	// and the SSE read loop; it returns the terminal result (or ok=false when
	// it already surfaced a pre-stream failure such as "not logged in" or a
	// submit error, in which case there is nothing left to dispatch). The
	// export-specific dispatch below is unchanged from before the extraction.
	res, jobID, ok := w.runCodegenJob(
		submitURL,
		reqBody,
		translate.T("codegenGenerating", "Generating Code"),
		translate.T("codegenGeneratingDetail", "Building source from your scene…"),
	)
	if !ok {
		return
	}

	// Cancelled-by-user path: the overlay is already closed and the
	// server has been told. Nothing else to surface — log it and
	// return silently. No error toast, no Monaco — the user asked
	// to stop and we delivered exactly that.
	if res.cancelled {
		log.Printf("[Codegen] Job %s cancelled by user", jobID)
		return
	}

	if res.infraErr != "" {
		log.Printf("[Codegen] Infra error: %s", res.infraErr)
		overlay.ShowError("Codegen — internal error", res.infraErr)
		return
	}

	// Dispatch on the shape of what the server returned:
	//
	//   - Diagnostics contain at least one ERROR: show the structured
	//     overlay with NO "proceed" button. The maker must fix the
	//     scene before code generation can continue — res.code is
	//     empty in this branch anyway.
	//
	//   - Diagnostics contain only WARNINGS: code WAS generated. Show
	//     the overlay so the maker reads the warnings, with a button
	//     that dismisses the panel and opens the code window. Gives
	//     the maker agency ("I saw the advisories, proceed") without
	//     silently bypassing.
	//
	//   - No diagnostics: open the code window directly (old path).
	//
	// Português: Três caminhos: erro → painel sem botão; warning →
	// painel com botão "ignorar avisos e gerar"; nada → abre código.
	if len(res.diagnostics) > 0 {
		log.Printf("[Codegen] %d diagnostics", len(res.diagnostics))
		hasError := false
		for _, d := range res.diagnostics {
			if d.Severity == "error" {
				hasError = true
				break
			}
		}
		if hasError {
			w.showDiagnosticsOverlay(res.diagnostics, "", nil)
			return
		}
		// Warning-only. Capture res by value so the proceed callback
		// can open the code window after the diagnostics overlay
		// closes. showCodeForResult normalises the backend shape and
		// always renders through ShowCodeMulti with a Download .zip
		// action, so the same call works for Go (single-file) and
		// C99 (single or multi-file) without branching here.
		resCopy := res
		w.showDiagnosticsOverlay(
			res.diagnostics,
			translate.T("codegenProceedAnyway", "Ignore warnings and generate"),
			func() {
				log.Printf("[Codegen:%s] Proceeding with warnings (%d files, code=%d bytes)",
					w.Language, len(resCopy.files), len(resCopy.code))
				showCodeForResult(resCopy, w.Language)
			},
		)
		return
	}

	if res.errors != "" {
		log.Printf("[Codegen] Errors:\n%s", res.errors)
		overlay.ShowError("Codegen — errors", res.errors)
		return
	}

	log.Printf("[Codegen:%s] Success: %d files, %d code bytes",
		w.Language, len(res.files), len(res.code))
	showCodeForResult(res, w.Language)
}

// runCodegenJob is the shared async codegen core: it authenticates, submits
// reqBody to submitURL, drives the progress overlay (spinner + elapsed time +
// Cancel), runs the SSE read loop in its own goroutine, and returns the
// terminal sseResult. It performs NO result-specific UI — the caller decides
// what to do with the returned result (open the code window, fill the
// StatementCase preview tab, gate an Apply, …). This is what lets the export
// flow (reqBody {"scene":…}) and the inspect-panel preview (reqBody
// {"previewCase":…}) share one transport: same queue, same Redis result keys,
// same SSE stream, same Cancel.
//
// Return contract:
//
//	ok=false — the run never produced a stream result and the reason has
//	           ALREADY been surfaced to the user here (empty token →
//	           "not logged in"; submit error → ShowError). The caller has
//	           nothing to dispatch and should simply return.
//	ok=true  — res is the terminal outcome (cancelled / infraErr / code /
//	           diagnostics) and jobID is the job's id (for the caller's logs).
//
// Authentication:
//
//	All four codegen routes (POST submit, POST cancel, GET stream, GET status)
//	live behind spaauth.RequireBearerToken on the server. We read the JWT from
//	rulesServer.GetAuthToken() — which itself reads window._ideAuthToken set
//	by the SPA host page after login — and include it as the Authorization
//	header on every codegen call. Empty token short-circuits to a user-facing
//	overlay error rather than letting the request bounce off the middleware
//	with a useless 401.
//
// Result delivery — fetch + ReadableStream, NOT EventSource:
//
//	The native EventSource API cannot send custom request headers. We
//	previously used it because SSE auto-reconnect was nice to have, but
//	now that the server gates the stream behind Bearer auth, EventSource
//	is no longer viable. We replace it with fetch() + body.getReader(),
//	which:
//	  • supports the Authorization header (our reason for switching),
//	  • lets us own the read loop (cancel via reader.cancel() is direct),
//	  • loses native auto-reconnect (acceptable — the previous flow
//	    treated every disconnect as cancel anyway, so reconnect was
//	    cosmetic at best).
//
//	The SSE wire format on the server stays identical; only the parser
//	moves to our side. See streamCodegenResult and parseSSEFrame below.
//
// Português:
//
//	Núcleo async compartilhado do codegen: autentica, faz o POST de reqBody,
//	conduz o overlay de progresso com Cancel, roda o loop de SSE em goroutine
//	e devolve o sseResult terminal. NÃO faz nenhuma UI de resultado — quem
//	chama decide o que fazer (abrir o código, preencher a aba de preview do
//	StatementCase, barrar um Apply, …). É o que permite export ({"scene":…}) e
//	preview ({"previewCase":…}) compartilharem um transporte só. ok=false já
//	mostrou o erro (sem token / erro de submit) e não há nada a despachar.
func (w *Workspace) runCodegenJob(submitURL, reqBody, progressTitle, progressDetail string) (res sseResult, jobID string, ok bool) {
	// Authenticate via Bearer JWT. The same token is reused for the
	// cancel and stream calls below — all four codegen routes are
	// protected by spaauth.RequireBearerToken on the server.
	//
	// An empty token means no SPA session has handed us a JWT yet
	// (window._ideAuthToken absent). The request would just bounce
	// off the middleware with HTTP 401, so we short-circuit here and
	// give the user a useful overlay error instead of a generic
	// "expected 202" deep in the response handling.
	token := rulesServer.GetAuthToken()
	if token == "" {
		log.Printf("[Codegen] No auth token — refusing to submit")
		overlay.ShowError(
			translate.T("codegenTitle", "Codegen"),
			translate.T("codegenNotLoggedIn",
				"You must be logged in to generate code. Please reload the page after signing in."),
		)
		return sseResult{}, "", false
	}

	headers := js.Global().Get("Object").New()
	headers.Set("Content-Type", "application/json")
	headers.Set("Authorization", token)

	opts := js.Global().Get("Object").New()
	opts.Set("method", "POST")
	opts.Set("headers", headers)
	opts.Set("body", reqBody)

	type submitResult struct {
		streamURL string
		err       string
	}
	submitDone := make(chan submitResult, 1)

	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		status := resp.Get("status").Int()
		if status != 202 {
			submitDone <- submitResult{err: fmt.Sprintf("HTTP %d: expected 202", status)}
			return js.Null()
		}
		return resp.Call("json")
	})

	thenParse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			submitDone <- submitResult{err: "empty server response"}
			return nil
		}
		streamURL := args[0].Get("stream_url").String()
		if streamURL == "" {
			submitDone <- submitResult{err: "stream_url missing from response"}
			return nil
		}
		submitDone <- submitResult{streamURL: streamURL}
		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		errMsg := "unknown error"
		if args[0].Get("message").Truthy() {
			errMsg = args[0].Get("message").String()
		}
		submitDone <- submitResult{err: "Network error: " + errMsg}
		return nil
	})

	js.Global().Call("fetch", submitURL, opts).
		Call("then", thenResponse).
		Call("then", thenParse).
		Call("catch", catchFn)

	sr := <-submitDone
	thenResponse.Release()
	thenParse.Release()
	catchFn.Release()

	if sr.err != "" {
		log.Printf("[Codegen] Submit error: %s", sr.err)
		overlay.ShowError("Codegen", sr.err)
		return sseResult{}, "", false
	}

	log.Printf("[Codegen] Job queued, opening stream: %s", sr.streamURL)

	// Extract the job ID from the stream URL — needed for the Cancel
	// button to call POST /api/v1/codegen/jobs/{id}/cancel. The URL
	// shape is "/api/v1/codegen/jobs/{id}/stream"; if the server ever
	// changes that shape this parse breaks silently and Cancel becomes
	// a no-op, so the prefix/suffix mismatch logs a warning rather
	// than crashing the IDE.
	jobID = extractJobIDFromStreamURL(sr.streamURL)
	if jobID == "" {
		log.Printf("[Codegen] WARN: could not parse jobID from stream URL %q — Cancel button will be inert", sr.streamURL)
	}

	sseDone := make(chan sseResult, 1)

	// cancelSignal carries one bit from the overlay's Cancel button to
	// the stream goroutine. Buffered (cap 1) so the overlay callback
	// never blocks: the worst case — user clicks Cancel twice — drops
	// the second click on the floor via the default branch in the
	// non-blocking send below.
	//
	// Only the stream goroutine writes to sseDone. The overlay's Cancel
	// callback used to write {cancelled: true} directly to sseDone; now
	// it signals here instead, the goroutine sees the signal, closes
	// its reader, and posts the cancelled result. Single-writer keeps
	// the receive side simple and removes the race between two
	// goroutines competing to deliver the terminal result.
	cancelSignal := make(chan struct{}, 1)

	// Open the progress overlay now — between submit succeeding and
	// the stream goroutine starting. The overlay carries a spinner, an
	// elapsed-time counter that ticks every second, and a Cancel button.
	//
	// The overlay handle is declared before construction so the
	// onCancel closure can call progress.Close() on itself without
	// a forward-reference compile error. Handle.Close is idempotent.
	var progress overlay.Handle
	progress = overlay.ShowProgress(
		progressTitle,
		progressDetail,
		translate.T("codegenCancel", "Cancel"),
		func() {
			// 1. Close the overlay immediately. The user clicked
			//    Cancel and expects the UI to react now, not after
			//    the server round-trip.
			progress.Close()
			// 2. Tell the server to abort the task. Fire-and-forget:
			//    we don't wait for the response. The cancel endpoint
			//    is idempotent so a network failure here is harmless;
			//    the stream disconnect (triggered by reader.cancel()
			//    inside the goroutine when it sees cancelSignal) is
			//    the server-side backup that also cancels.
			if jobID != "" {
				cancelURL := "/api/v1/codegen/jobs/" + jobID + "/cancel"
				cancelOpts := js.Global().Get("Object").New()
				cancelOpts.Set("method", "POST")
				cancelHeaders := js.Global().Get("Object").New()
				cancelHeaders.Set("Authorization", token)
				cancelOpts.Set("headers", cancelHeaders)
				js.Global().Call("fetch", cancelURL, cancelOpts)
			}
			// 3. Signal the stream goroutine to abort. The goroutine
			//    closes its reader and posts {cancelled: true} on
			//    sseDone. Non-blocking send — second click is dropped.
			select {
			case cancelSignal <- struct{}{}:
			default:
			}
		},
	)
	// Belt-and-braces close — covers result/error paths where the
	// onCancel closure does not run. Handle.Close is idempotent.
	defer progress.Close()

	// Run the SSE stream in its own goroutine so the main goroutine can
	// block on sseDone while the Cancel overlay is still responsive
	// (the JS event loop runs the overlay callbacks in parallel).
	go streamCodegenResult(sr.streamURL, token, cancelSignal, sseDone)

	res = <-sseDone

	// Close progress overlay BEFORE the caller shows the next overlay
	// (Monaco for success, ShowError for failure, or the preview tab).
	// The defer above is a safety net; this explicit close keeps the
	// visual transition clean (no flicker of two overlays stacked).
	progress.Close()

	return res, jobID, true
}

// previewCaseCode runs a StatementCase preview through the shared codegen core
// and returns the generated snippet plus its cross-case diagnostics. It is the
// inspect-panel counterpart to generateCode: the SAME async transport (queue,
// Redis result, SSE) via runCodegenJob, but the request body carries the draft
// cases ({"previewCase":…}) instead of the whole scene, and the result is
// handed back to the caller (the StatementCase device) to render in its
// Preview tab rather than opening the export code window.
//
// casesJSON is the device's case array exactly as the inspect form carries it
// (the caseInspectRow shape — matchKind/values/isDefault); the server converts
// it to graph.CaseDef. It is already valid JSON (an array), so it is
// interpolated raw into the request body.
//
// ok=false means the run produced no result — not logged in, a submit error,
// or the user cancelled the progress overlay — and runCodegenJob (or this
// method, for infra errors) already surfaced the reason. The caller shows no
// preview in that case. On ok=true the snippet is returned even when diags
// carry warnings or errors: PreviewCase always renders the decision structure,
// and the caller decides how to surface the diagnostics.
//
// Português: Contraparte do generateCode para o painel de inspeção. Mesmo
// transporte async (runCodegenJob), mas o corpo leva os cases em rascunho
// ({"previewCase":…}) e o resultado volta para quem chamou (o device do
// StatementCase) renderizar na aba Preview. casesJSON é o array de cases no
// formato do form (matchKind/values/isDefault); o servidor converte para
// graph.CaseDef. ok=false → sem resultado (motivo já mostrado); ok=true → o
// snippet volta mesmo com diagnósticos, pois PreviewCase sempre desenha a
// estrutura.
func (w *Workspace) previewCaseCode(scopeID, selectorType, casesJSON string) (code string, diags []overlay.Diagnostic, monacoLang string, ok bool) {
	language := w.Language
	if language == "" {
		language = "go"
	}
	submitURL := "/api/v1/codegen/" + language

	reqBody := fmt.Sprintf(
		`{"previewCase":{"scopeId":%q,"selectorType":%q,"cases":%s}}`,
		scopeID, selectorType, casesJSON,
	)

	res, _, runOK := w.runCodegenJob(
		submitURL,
		reqBody,
		translate.T("casePreviewGenerating", "Generating preview"),
		translate.T("casePreviewGeneratingDetail", "Rendering this Case as source…"),
	)
	if !runOK || res.cancelled {
		return "", nil, "", false
	}
	if res.infraErr != "" {
		overlay.ShowError(translate.T("casePreviewTitle", "Case preview"), res.infraErr)
		return "", nil, "", false
	}
	// monacoLanguageHint maps the project language ("go"/"c") to the Monaco
	// highlighter id, so the device can set the Preview tab's syntax without
	// knowing the mapping (which lives here, next to showCodeForResult).
	return res.code, res.diagnostics, monacoLanguageHint(language), true
}

// showCodeForResult opens the code overlay for a successful codegen
// result. The overlay always carries a "Download .zip" action in the
// title bar so the maker can take the source home with a single
// click — regardless of how many files the backend emitted.
//
// Backend-shape normalisation:
//
//   - Go backend writes everything into res.code; res.files is nil.
//     We synthesise a single-entry map {"main.go": res.code} so the
//     downstream path (overlay + ZIP) stays uniform.
//   - C99 backend writes nothing into res.code and populates res.files
//     with main.c plus, when the runtime is used, the header and
//     stub files. We pass res.files through unchanged.
//
// After normalisation the overlay always uses ShowCodeMulti — even
// for a single-file project. The tab strip with one entry looks
// slightly odd, but the trade is buying a unified Download action
// and a future-proof shape that supports more files appearing in
// either backend without UI changes here.
//
// The ZIP filename is namespaced by language so a maker who exports
// both Go and C versions of the same scene ends up with two
// distinguishable archives on their disk.
//
// Português: Abre o overlay de código com botão Download .zip
// sempre presente. Para Go (que retorna só res.code), sintetiza
// um map {"main.go": code}; para C99 usa res.files direto. O
// caminho único pra ShowCodeMulti garante que a UI fique igual
// quando o backend Go for atualizado pra também emitir multi-file.
func showCodeForResult(res sseResult, language string) {
	defaultLang := monacoLanguageHint(language)

	// Normalise into a files map. The Go backend today returns a
	// single source via res.code; synthesise an entry so the
	// overlay/ZIP paths don't need a "if Go else C" branch.
	files := res.files
	if len(files) == 0 && res.code != "" {
		files = map[string]string{
			mainFilenameFor(language): res.code,
		}
	}
	if len(files) == 0 {
		// Empty success — shouldn't happen in practice, but log
		// and bail rather than show a blank overlay.
		log.Printf("[Codegen:%s] empty result (no code, no files) — nothing to show", language)
		return
	}

	zipName := "iotmaker-code.zip"
	if language != "" {
		zipName = "iotmaker-" + language + ".zip"
	}

	// Capture by value — actions outlive this function's stack frame.
	filesForAction := files
	actions := []overlay.Action{
		{
			Label: translate.T("codegenDownloadZip", "Download .zip"),
			Icon:  "download",
			OnClick: func() {
				if err := downloadFilesAsZip(filesForAction, zipName); err != nil {
					log.Printf("[Codegen:zip] download failed: %v", err)
					overlay.ShowError(
						translate.T("codegenZipErrorTitle", "Download failed"),
						err.Error())
				}
			},
		},
	}
	overlay.ShowCodeMulti(
		translate.T("codegenCodeTitle", "Generated Code"),
		files,
		defaultLang,
		actions,
	)
}

// mainFilenameFor returns the conventional "main" filename for a
// given language token. Used by showCodeForResult to synthesise a
// single-entry files map for backends (today: Go) that still return
// a flat code string instead of a files map.
//
// The map is intentionally tiny — it covers exactly the languages
// the codegen pipeline emits today. Adding a new language is a
// one-line edit when the backend lands.
//
// Português: Devolve o nome canônico do arquivo "main" pra cada
// linguagem. Pequeno por design — uma linha por linguagem suportada.
func mainFilenameFor(language string) string {
	switch language {
	case "c":
		return "main.c"
	case "go", "":
		return "main.go"
	}
	return "main.txt"
}

// monacoLanguageHint translates the project's language token into the
// identifier Monaco uses to pick a syntax-highlighting tokenizer.
//
// The project language tokens are storage-layer values that don't
// need to match an editor's grammar names by accident, so the
// translation is explicit. Today the map is small ("go"→"go",
// "c"→"c") but breaking the layers apart now means a future change
// to either side stays a one-line edit.
//
// Empty / unknown tokens fall back to "go" — same default the
// schema applies when stage_files.language is missing — so projects
// created before Parcela 1 still get syntax highlighting on
// generated code instead of a plain-text dump.
//
// Português: Mapeia o token de linguagem do projeto pro identificador
// que o Monaco usa pra highlight. Hoje é trivial; existe pra desacoplar
// camadas. Default "go" — bate com o default do schema.
func monacoLanguageHint(language string) string {
	switch language {
	case "c":
		return "c"
	case "go", "":
		return "go"
	default:
		return "go"
	}
}

// showDiagnosticsOverlay opens the structured diagnostics panel and
// wires its click handler to pan+zoom the canvas onto the clicked
// item. The handler receives every device named in the diagnostic —
// a single row may reference several — and computes one enclosing
// rect that covers them all so FitAll shows the full context at once.
//
// Diagnostics with just one device degenerate naturally: the union
// of a single rect is itself.
//
// Português: Abre o painel de diagnósticos e amarra o click ao
// pan+zoom combinado. O handler recebe todos os devices mencionados
// na linha clicada e calcula um rect que os envolve para enquadrar
// tudo junto.
// extractJobIDFromStreamURL parses a codegen stream URL of the form
// "/api/v1/codegen/jobs/{id}/stream" and returns the {id} segment.
// Returns "" when the URL does not match — caller must handle that
// (the Cancel button becomes inert; the codegen still completes, the
// only loss is the user's ability to abort it early).
//
// Português:
//
//	Extrai o {id} da URL "/api/v1/codegen/jobs/{id}/stream". Vazio
//	se a URL não casar; nesse caso o botão Cancel fica inerte mas
//	o codegen segue normalmente.
func extractJobIDFromStreamURL(streamURL string) string {
	const prefix = "/api/v1/codegen/jobs/"
	const suffix = "/stream"
	if !strings.HasPrefix(streamURL, prefix) || !strings.HasSuffix(streamURL, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(streamURL, prefix), suffix)
}

// streamCodegenResult opens the SSE stream at streamURL, parses incoming
// frames in our own loop (because the native EventSource API cannot send
// the Authorization header we need), and posts the terminal sseResult to
// done. Designed to run as its own goroutine; the caller blocks on done
// and stays free to respond to overlay events.
//
// Control flow:
//
//   - fetch with method=GET, Authorization=token. Non-200 → infraErr,
//     return.
//   - response.body.getReader() gives us the ReadableStream reader.
//   - Inner loop: kick off reader.read() (returns a Promise), and in the
//     same iteration select on either (a) the read settling, or
//     (b) cancelSignal firing. The Promise's .then/.catch callbacks
//     funnel into chunkCh — a small channel that lets us bridge the
//     JS event loop into Go's select.
//   - Each chunk is decoded with TextDecoder({stream:true}) so multi-
//     byte UTF-8 split across chunks is handled. The decoded text is
//     appended to a buffer; we split frames on "\n\n", parse each one
//     via parseSSEFrame, and dispatch on the event name. The buffer
//     may end with a partial frame which we leave in place for the
//     next iteration.
//   - "result" / "error" events terminate the stream: we post the
//     corresponding sseResult, call reader.cancel() to release the
//     connection, and return.
//   - cancelSignal arriving from the overlay's Cancel button calls
//     reader.cancel() and posts {cancelled: true}.
//
// Why a goroutine instead of the previous addEventListener model:
//
//	EventSource fired callbacks in the JS event loop, and those callbacks
//	wrote to sseDone via the Go runtime's syscall/js bridge. fetch's
//	reader.read() also resolves on the JS event loop, but we need a tight
//	loop to consume chunks back-to-back — running that loop synchronously
//	in the request handler would block any other Go work. The goroutine
//	keeps the bridge clean: JS resolves a Promise → callback writes one
//	chunk → goroutine wakes up → processes → loops.
//
// Português:
//
//	Lê o stream SSE via fetch+ReadableStream em sua própria goroutine.
//	Necessário porque EventSource não aceita header Authorization. O
//	caller bloqueia em done; o overlay sinaliza via cancelSignal.
func streamCodegenResult(
	streamURL string,
	token string,
	cancelSignal <-chan struct{},
	done chan<- sseResult,
) {
	// Build the fetch request. The same Authorization header that
	// passed the submit middleware passes the stream middleware.
	headers := js.Global().Get("Object").New()
	headers.Set("Authorization", token)
	headers.Set("Accept", "text/event-stream")

	opts := js.Global().Get("Object").New()
	opts.Set("method", "GET")
	opts.Set("headers", headers)

	// fetchCh: receives the initial Response (or a network error).
	type fetchOutcome struct {
		response js.Value
		err      string
	}
	fetchCh := make(chan fetchOutcome, 1)

	thenFetch := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		fetchCh <- fetchOutcome{response: args[0]}
		return nil
	})
	catchFetch := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := "network error"
		if args[0].Get("message").Truthy() {
			msg = args[0].Get("message").String()
		}
		fetchCh <- fetchOutcome{err: msg}
		return nil
	})
	js.Global().Call("fetch", streamURL, opts).
		Call("then", thenFetch).
		Call("catch", catchFetch)

	var resp js.Value
	select {
	case fo := <-fetchCh:
		thenFetch.Release()
		catchFetch.Release()
		if fo.err != "" {
			done <- sseResult{infraErr: fo.err}
			return
		}
		resp = fo.response
	case <-cancelSignal:
		// Overlay cancelled before the connection even opened.
		// We have no reader yet, so just close the channel.
		thenFetch.Release()
		catchFetch.Release()
		done <- sseResult{cancelled: true}
		return
	}

	status := resp.Get("status").Int()
	if status != 200 {
		done <- sseResult{infraErr: fmt.Sprintf("stream HTTP %d", status)}
		return
	}

	// TextDecoder turns Uint8Array chunks into JS strings. {stream:true}
	// keeps internal state so a UTF-8 sequence split across two chunks
	// is recovered correctly. We allocate once and reuse for the life
	// of the stream.
	decoder := js.Global().Get("TextDecoder").New("utf-8")
	decoderOpts := js.Global().Get("Object").New()
	decoderOpts.Set("stream", true)

	reader := resp.Get("body").Call("getReader")

	// readChunk reads one chunk by attaching one-shot .then/.catch
	// handlers to reader.read(). Returns via the chunkCh channel.
	// The js.Funcs are Released by the caller (this function lives
	// in a hot loop — releasing on each iteration is the simplest
	// way to avoid leaking JS function references over a long stream).
	type chunkOutcome struct {
		done  bool
		text  string
		errMs string
	}

	var buffer strings.Builder

	for {
		chunkCh := make(chan chunkOutcome, 1)
		thenRead := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			result := args[0]
			if result.Get("done").Bool() {
				chunkCh <- chunkOutcome{done: true}
				return nil
			}
			value := result.Get("value") // Uint8Array
			text := decoder.Call("decode", value, decoderOpts).String()
			chunkCh <- chunkOutcome{text: text}
			return nil
		})
		catchRead := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			msg := "reader error"
			if args[0].Get("message").Truthy() {
				msg = args[0].Get("message").String()
			}
			chunkCh <- chunkOutcome{errMs: msg}
			return nil
		})
		reader.Call("read").
			Call("then", thenRead).
			Call("catch", catchRead)

		var co chunkOutcome
		select {
		case co = <-chunkCh:
		case <-cancelSignal:
			// User clicked Cancel. Tell the reader to close (its
			// pending read will resolve with done=true or reject
			// with AbortError — either way we don't care; we're
			// already on our way out).
			reader.Call("cancel")
			thenRead.Release()
			catchRead.Release()
			done <- sseResult{cancelled: true}
			return
		}

		thenRead.Release()
		catchRead.Release()

		if co.errMs != "" {
			done <- sseResult{infraErr: co.errMs}
			return
		}
		if co.done {
			// Stream closed without delivering a terminal event.
			// Either the server gave up mid-flight or the network
			// dropped. Report it as infra error so the user sees
			// something — silent close would just hang the spinner.
			done <- sseResult{infraErr: "stream closed before a result was delivered"}
			return
		}

		buffer.WriteString(co.text)

		// Drain whole frames from the buffer. A frame ends with the
		// SSE separator "\n\n"; a partial frame after the last
		// separator stays in the buffer for the next iteration.
		raw := buffer.String()
		for {
			sep := strings.Index(raw, "\n\n")
			if sep < 0 {
				break
			}
			frame := raw[:sep]
			raw = raw[sep+2:]

			event, data := parseSSEFrame(frame)
			switch event {
			case "":
				// Comment-only frame (": connected job=...") or
				// malformed frame — keep reading.
			case "result":
				reader.Call("cancel")
				done <- buildSSEResultFromData(data)
				return
			case "error":
				reader.Call("cancel")
				done <- buildSSEErrorFromData(data)
				return
			default:
				// Unknown event — log and ignore. The server should
				// never emit this; if it starts to, we want to know
				// without crashing the IDE.
				log.Printf("[Codegen] Unknown SSE event %q (data=%q)", event, data)
			}
		}
		// Re-write the remainder back into the buffer. strings.Builder
		// has no Reset+WriteString shortcut, so we replace it.
		buffer.Reset()
		buffer.WriteString(raw)
	}
}

// parseSSEFrame splits a single SSE frame into its event name and data
// payload. A frame is the text between two "\n\n" separators on the
// wire, with these line types relevant to us:
//
//	"event: <name>"  — name of the event (only one per frame)
//	"data: <body>"   — the payload (only one per frame in our protocol)
//	":<anything>"    — SSE comment, ignored
//
// Returns ("", "") for a comment-only frame; the caller treats that as
// "keep reading".
//
// We do NOT support the full SSE spec (multi-line data, id:, retry:);
// the server we talk to only ever emits the simple shapes above, and
// keeping the parser minimal makes its correctness easy to eyeball.
//
// Português:
//
//	Quebra um frame SSE em (event, data). Suporta apenas o subconjunto
//	que nosso servidor emite — uma linha event:, uma linha data:, e
//	comentários iniciados por ":". Multi-line data e outros campos da
//	spec não são tratados porque o servidor não os usa.
func parseSSEFrame(frame string) (event, data string) {
	for _, line := range strings.Split(frame, "\n") {
		switch {
		case strings.HasPrefix(line, ":"):
			// Comment line — ignore.
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	return event, data
}

// buildSSEResultFromData parses the JSON payload of an "event: result"
// frame and packs it into an sseResult. The shape it expects is the
// codegen.Response struct from the server:
//
//	{ "code": "...", "ir": "...", "errors": [...], "warnings": [...],
//	  "diagnostics": [{kind, severity, devices, scope, message}, ...] }
//
// "warnings" are not surfaced as diagnostics — they get logged and the
// happy-path overlay shows the generated code. Real warnings now flow
// through the "diagnostics" channel with severity="warning"; the legacy
// "warnings" field is preserved for older servers that did not emit
// structured diagnostics yet.
//
// Português:
//
//	Constrói sseResult a partir do JSON do evento "result". O shape é o
//	codegen.Response do servidor: code, ir, errors, warnings,
//	diagnostics. Diagnostics estruturados são preferenciais; warnings
//	textuais só são logados (fallback de servidores antigos).
func buildSSEResultFromData(data string) sseResult {
	parsed := js.Global().Get("JSON").Call("parse", data)

	code := parsed.Get("code").String()
	ir := parsed.Get("ir").String()

	var errStr string
	errArr := parsed.Get("errors")
	if errArr.Truthy() {
		for i := 0; i < errArr.Length(); i++ {
			if errStr != "" {
				errStr += "\n"
			}
			errStr += errArr.Index(i).String()
		}
	}

	warnArr := parsed.Get("warnings")
	if warnArr.Truthy() {
		for i := 0; i < warnArr.Length(); i++ {
			log.Printf("[Codegen] Warning: %s", warnArr.Index(i).String())
		}
	}

	// Structured diagnostics — new field carrying kind/severity/
	// devices/scope. Older servers omit this field; we fall back
	// to the flat errors string in that case so the UI still has
	// something to show.
	var diags []overlay.Diagnostic
	diagArr := parsed.Get("diagnostics")
	if diagArr.Truthy() && !diagArr.IsUndefined() {
		for i := 0; i < diagArr.Length(); i++ {
			item := diagArr.Index(i)
			d := overlay.Diagnostic{
				Kind:     item.Get("kind").String(),
				Severity: item.Get("severity").String(),
				Scope:    item.Get("scope").String(),
				Message:  item.Get("message").String(),
			}
			devsArr := item.Get("devices")
			if devsArr.Truthy() {
				for j := 0; j < devsArr.Length(); j++ {
					d.Devices = append(d.Devices, devsArr.Index(j).String())
				}
			}
			diags = append(diags, d)
		}
	}

	// Multi-file output — populated by backends that emit more than one
	// artefact per generation (today: C99 → main.c, iotmaker_runtime.h,
	// iotmaker_runtime_stub.c). The shape is a plain JSON object whose
	// keys are file names and values are file contents. Object.keys lets
	// us iterate without hard-coding the file set on this side, so a
	// future backend that adds (say) a Makefile arrives in the map
	// automatically.
	//
	// nil-on-empty (rather than an empty map) lets the consumer use
	// len(res.files) == 0 to decide between the single-code path and
	// the multi-file path without an extra flag.
	var files map[string]string
	filesObj := parsed.Get("files")
	if filesObj.Truthy() && !filesObj.IsUndefined() {
		keys := js.Global().Get("Object").Call("keys", filesObj)
		for i := 0; i < keys.Length(); i++ {
			name := keys.Index(i).String()
			if files == nil {
				files = make(map[string]string)
			}
			files[name] = filesObj.Get(name).String()
		}
	}

	return sseResult{
		code:        code,
		files:       files,
		ir:          ir,
		errors:      errStr,
		diagnostics: diags,
	}
}

// buildSSEErrorFromData parses the JSON payload of an "event: error"
// frame into an sseResult carrying infraErr. The shape is
// { "message": "...", "infraError": "..." } — we prefer the user-facing
// message and fall back to infraError when message is empty.
//
// "infraError" (camelCase) is the renamed field — see
// server/handler/codegen/stream.go writeSSEError. The previous tag was
// "infra_error" (snake_case); both sides were renamed together to
// honour the project-wide invariant on Go→JS JSON tags.
//
// Português:
//
//	Constrói sseResult de erro a partir do JSON do evento "error".
//	Prefere "message"; cai em "infraError" quando vazio.
func buildSSEErrorFromData(data string) sseResult {
	parsed := js.Global().Get("JSON").Call("parse", data)
	msg := parsed.Get("message").String()
	if msg == "" {
		msg = parsed.Get("infraError").String()
	}
	return sseResult{infraErr: msg}
}

func (w *Workspace) showDiagnosticsOverlay(
	diags []overlay.Diagnostic,
	proceedLabel string,
	onProceed func(),
) {
	// Collect every device ID mentioned in the diagnostics so we can
	// highlight them on the canvas with the same red border used for
	// geometric conflicts. Deduplication is handled by the scene
	// manager itself.
	//
	// Português: Coleta todos os IDs dos diagnósticos e passa pro
	// scene — ele marca os devices com a borda vermelha no canvas.
	var allIDs []string
	for _, d := range diags {
		allIDs = append(allIDs, d.Devices...)
	}
	if w.SceneMgr != nil {
		w.SceneMgr.SetCodegenDiagnosticDevices(allIDs)
	}
	if w.Stage != nil {
		w.Stage.MarkDirty()
	}

	overlay.ShowDiagnostics(
		translate.T("codegenIssuesTitle", "Codegen — issues"),
		diags,
		func(deviceIDs []string) {
			bbox, ok := w.unionDeviceBBox(deviceIDs)
			if !ok {
				log.Printf("[Diagnostics] no known devices in click target: %v", deviceIDs)
				return
			}
			canvasW, canvasH := w.Stage.GetCanvasSize()
			// 40px padding keeps devices comfortably off the edge
			// and leaves room for the overlay if the user has
			// dragged it to the side. 500ms animation matches the
			// duration used elsewhere so the feel is consistent.
			w.Camera.FitAll(
				bbox.X, bbox.Y, bbox.Width, bbox.Height,
				canvasW, canvasH,
				40, 500,
			)
		},
		proceedLabel,
		onProceed,
	)
}

// unionDeviceBBox computes the smallest rect enclosing the outer
// bboxes of every device in ids. Unknown IDs are skipped silently —
// a scene may have been edited between the codegen run and the click,
// and we want the remaining devices to still frame usefully.
//
// Returns ok=false when no ID resolves to a known device.
//
// Português: Rect mínimo que envolve todos os devices listados. IDs
// desconhecidos são ignorados; ok=false só quando nenhum resolve.
func (w *Workspace) unionDeviceBBox(ids []string) (scene.Rect, bool) {
	var union scene.Rect
	seeded := false
	for _, id := range ids {
		dev := w.SceneMgr.FindDevice(id)
		if dev == nil {
			continue
		}
		b := dev.GetOuterBBox()
		if !seeded {
			union = b
			seeded = true
			continue
		}
		// Expand union to cover b.
		if b.X < union.X {
			union.Width += union.X - b.X
			union.X = b.X
		}
		if b.Y < union.Y {
			union.Height += union.Y - b.Y
			union.Y = b.Y
		}
		if r := b.X + b.Width; r > union.X+union.Width {
			union.Width = r - union.X
		}
		if btm := b.Y + b.Height; btm > union.Y+union.Height {
			union.Height = btm - union.Y
		}
	}
	return union, seeded
}

// buildPropsPreviewHTML generates a static HTML preview of the device's
// configurable properties for the main menu panel. All inputs are disabled
// — this is a read-only informational display so the maker can see what
// options are available before placing the component on the canvas.
//
// The visual style matches the overlay's embedded form (Catppuccin Mocha
// palette) for consistency across the IDE.
//
// Returns empty string when there are no props to display.
//
// Português: Gera HTML estático dos props do device para o menu principal.
// Todos os inputs são disabled — apenas informativo, sem interatividade.
func buildPropsPreviewHTML(props []blackbox.PropDefClient) string {
	if len(props) == 0 {
		return ""
	}

	// Catppuccin Mocha colours (same as overlay/overlay.go)
	const (
		colMantle   = "#181825"
		colSurface1 = "#45475a"
		colPeach    = "#fab387"
		colText     = "#cdd6f4"
	)

	html := fmt.Sprintf(
		`<div style="background:%s;border:1px solid %s;border-radius:6px;`+
			`padding:16px;margin:12px 0;display:flex;flex-direction:column;gap:10px;">`,
		colMantle, colSurface1)

	// Section header — translated via i18n
	html += fmt.Sprintf(
		`<div style="color:%s;font-size:12px;font-weight:600;margin-bottom:4px;`+
			`text-transform:uppercase;letter-spacing:0.5px;">%s</div>`,
		colPeach, escHTMLAttr(translate.T("tabProperties", "Properties")))

	for _, p := range props {
		html += `<div style="display:flex;align-items:center;gap:10px;">`

		// Label
		html += fmt.Sprintf(
			`<label style="color:%s;font-size:12px;font-weight:500;min-width:90px;`+
				`text-align:right;flex-shrink:0;">%s</label>`,
			colText, escHTMLAttr(p.Label))

		// Input — disabled, showing the default value
		val := p.Default
		inputCSS := fmt.Sprintf(
			"flex:1;background:%s;color:%s;border:1px solid %s;"+
				"border-radius:4px;padding:5px 8px;font-size:13px;"+
				"opacity:0.6;cursor:not-allowed;",
			colMantle, colText, colSurface1)

		if len(p.Options) > 0 {
			// Select dropdown (disabled)
			html += fmt.Sprintf(
				`<select disabled style="%sfont-family:sans-serif;appearance:auto;">`,
				inputCSS)
			for _, opt := range p.Options {
				sel := ""
				if opt == val {
					sel = " selected"
				}
				html += fmt.Sprintf(`<option value="%s"%s>%s</option>`,
					escHTMLAttr(opt), sel, escHTMLAttr(opt))
			}
			html += `</select>`
		} else {
			// Text input (disabled)
			html += fmt.Sprintf(
				`<input type="text" disabled value="%s" style="%sfont-family:monospace;">`,
				escHTMLAttr(val), inputCSS)
		}

		html += `</div>`
	}

	html += `</div>`
	return html
}

// escHTMLAttr escapes a string for safe use inside HTML attributes.
func escHTMLAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// countDevicesInJSON returns a rough count of devices in a scene JSON string.
// It counts occurrences of the "type" key inside device objects. This is a
// lightweight approach that avoids a full JSON unmarshal — good enough for the
// device_count metadata shown in the file manager.
//
// Português: Conta aproximadamente quantos devices existem no JSON do scene.
// Abordagem leve sem unmarshal completo.
func countDevicesInJSON(sceneJSON string) int {
	count := 0
	needle := `"devices":`
	idx := strings.Index(sceneJSON, needle)
	if idx < 0 {
		return 0
	}

	// Count occurrences of "id": inside the devices array.
	// Each device has exactly one "id" field.
	sub := sceneJSON[idx:]
	search := `"id":`
	for i := 0; i <= len(sub)-len(search); i++ {
		if sub[i:i+len(search)] == search {
			count++
		}
	}
	// Subtract 1 because the top-level JSON may also have an "id" match
	// outside the devices array, but this is close enough for display.
	if count > 0 {
		return count
	}
	return 0
}

// triggerJSONDownload creates a temporary Blob and triggers a browser download
// of the given JSON string with the specified filename.
//
// This is the Phase 1 "Open" behaviour — the user gets a local file download
// of the saved scene. Phase 2 will replace this with canvas reconstruction.
//
// Português: Cria um Blob temporário e dispara o download no navegador.
// Comportamento da Fase 1 para "Open" — download local do JSON salvo.
func triggerJSONDownload(jsonStr, filename string) {
	doc := js.Global().Get("document")
	blob := js.Global().Get("Blob").New(
		js.Global().Get("Array").New(js.ValueOf(jsonStr)),
		map[string]interface{}{"type": "application/json"},
	)
	url := js.Global().Get("URL").Call("createObjectURL", blob)
	a := doc.Call("createElement", "a")
	a.Set("href", url)
	a.Set("download", filename)
	doc.Get("body").Call("appendChild", a)
	a.Call("click")
	doc.Get("body").Call("removeChild", a)
	js.Global().Get("URL").Call("revokeObjectURL", url)
	log.Printf("[Workspace] JSON downloaded: %s (%d bytes)", filename, len(jsonStr))
}

// =====================================================================
//  Stage file backup — auto-save on change
//
//  Every time the stage changes, a backup is saved after a 3-second debounce.
//
//  Two scenarios:
//    1. File is open (currentFileID set) → backup name: "OriginalName (backup)"
//    2. No file saved yet (currentFileID empty) → backup name: "unsaved files backup"
//
//  On manual save (Ctrl+S, Save button), the backup is deleted.
//  On file open, any existing backup is deleted.
//
//  Português: Backup automático em cada mudança. Se tem arquivo aberto, salva
//  como "Nome (backup)". Se não, salva como "unsaved files backup".
// =====================================================================

// saveBackup saves the given scene JSON to the backup file.
// Creates the backup on first call, updates on subsequent calls.
//
// When no file is open yet (first-time use), the backup is named
// "unsaved files backup" so the user can recover work even before
// their first manual save.
// SetSiblingSceneFn injects the sibling workspace's scene-export function
// so this workspace can persist the combined (both-stage) document on
// save. See captureCombinedScene. Called once by the ViewManager after
// both workspaces finish Init (WorkspaceModeBoth only).
//
// Português: Injeta a função de export do workspace irmão, pra este
// workspace salvar o documento combinado. Chamada uma vez pelo ViewManager.
func (w *Workspace) SetSiblingSceneFn(fn func() string) {
	w.siblingSceneFn = fn
}

// SetSelectedTarget records the hardware target the maker picked in the board
// dropdown. The next export stamps it into the scene metadata (Metadata.Target)
// via the SetTargetFunc callback, and the C codegen resolves it to a type
// profile + string-buffer size. Passing "" clears the choice back to the
// default (Arduino UNO). Idempotent — the board picker calls this on each pick.
//
// Português: Registra o target de hardware que o maker escolheu no dropdown. O
// próximo export o carimba no metadata (Metadata.Target) via o callback do
// SetTargetFunc, e o codegen C o resolve para profile + buffer. "" limpa a
// escolha de volta ao default (Arduino UNO). Idempotente.
func (w *Workspace) SetSelectedTarget(id string) {
	w.selectedTarget = id
}

// SelectedTarget returns the currently-selected hardware-target id, or "" when
// none has been picked. The board picker reads it to show the current choice as
// selected when it opens.
//
// Português: Retorna o id do target de hardware selecionado, ou "" quando
// nenhum. O picker o lê para marcar a escolha atual ao abrir.
func (w *Workspace) SelectedTarget() string {
	return w.selectedTarget
}

// SetSelectedBufferSize records the maker's string-buffer override, in bytes,
// from the selected board's advanced panel. Zero clears it (the codegen keeps
// the board's default). The next export stamps it into
// Metadata.StringBufferSize via the SetBufferSizeFunc callback.
//
// Português: Registra o override do buffer do maker, em bytes, do painel
// avançado da placa. Zero limpa (o codegen mantém o default). O próximo export
// o carimba em Metadata.StringBufferSize via o callback do SetBufferSizeFunc.
func (w *Workspace) SetSelectedBufferSize(bytes int) {
	w.selectedBufferSize = bytes
}

// SelectedBufferSize returns the maker's current string-buffer override in
// bytes, or 0 when none.
//
// Português: Retorna o override atual do buffer em bytes, ou 0 quando nenhum.
func (w *Workspace) SelectedBufferSize() int {
	return w.selectedBufferSize
}

// SetImportBroadcastFn injects the function that fans an extracted scene out
// to every workspace. The ViewManager wires it so image (PNG) import — which
// is triggered on a single workspace — reaches both stages. See
// importBroadcastFn and importFromImageFile.
func (w *Workspace) SetImportBroadcastFn(fn func(sceneJSON string)) {
	w.importBroadcastFn = fn
}

// SetBackupScheduler injects the function that schedules the project's single
// combined backup save. The ViewManager points every workspace at the backend's
// ScheduleBackupSave so any stage's change produces ONE backup write — see
// OnExport and backupScheduler, and the 409 collision this prevents.
func (w *Workspace) SetBackupScheduler(fn func()) {
	w.backupScheduler = fn
}

// ImportSceneOnly replays a scene into THIS workspace (clear, recreate this
// stage's devices, reconnect wires, restore this stage's camera) without
// touching the current-file identity — the right primitive for loading a scene
// that has no saved-file identity, such as an imported PNG. The ViewManager
// calls it on each workspace to fan an image import out to both stages.
//
// Português: Importa uma cena só NESTE workspace, sem mexer no arquivo atual —
// usado pelo ViewManager pra espalhar o import de imagem pros dois stages.
func (w *Workspace) ImportSceneOnly(sceneJSON string) {
	w.importScene(sceneJSON)
}

// captureCombinedScene returns the scene JSON to persist: this workspace's
// devices PLUS the sibling workspace's devices, each already stamped with
// its owning stage (DeviceJSON.Stage). Persisting both stages in one
// document keeps a project's backend logic and frontend dashboard together
// — they save and restore as a unit, and importScene replays each device
// only on the workspace its Stage tag names.
//
// With no sibling wired (single-workspace modes, tests) it returns this
// workspace's own scene unchanged, so behaviour there is identical to before.
//
// Português: Retorna o JSON da cena COMBINADA (devices deste workspace +
// do irmão, cada um carimbado com seu stage). Sem irmão, retorna só a
// própria cena.
func (w *Workspace) captureCombinedScene() string {
	own := w.SceneMgr.Export()
	if w.siblingSceneFn == nil {
		return own
	}
	merged, err := mergeScenes(own, w.siblingSceneFn())
	if err != nil {
		log.Printf("[Workspace:%s] Combined-scene merge failed (%v) — persisting own stage only",
			w.Name, err)
		return own
	}
	return merged
}

// mergeScenes folds two scene documents into one: the device and wire
// lists are concatenated, and metadata (camera, canvas size, version) is
// taken from the primary document. Each device keeps the Stage its own
// serializer stamped, so the merged document is self-describing and each
// workspace's importScene can pick out exactly its own devices.
//
// Note: only one camera survives in the merged metadata (the primary's).
// Per-stage camera is a deliberate, minor trade-off of the single-document
// model — the device/wire data, which matters, is preserved in full.
//
// Português: Junta dois documentos de cena em um (devices e fios
// concatenados; metadata vem do primário). Cada device mantém seu Stage.
func mergeScenes(primaryJSON, secondaryJSON string) (string, error) {
	var primary, secondary scene.SceneJSON
	if err := json.Unmarshal([]byte(primaryJSON), &primary); err != nil {
		return "", err
	}
	if err := json.Unmarshal([]byte(secondaryJSON), &secondary); err != nil {
		return "", err
	}
	primary.Devices = append(primary.Devices, secondary.Devices...)
	primary.Wires = append(primary.Wires, secondary.Wires...)

	// Preserve BOTH stages' cameras (keyed by stage) so each workspace
	// restores its own viewport on import; without this the secondary stage
	// inherits the primary's zoom/offset (the frontend-loads-with-backend-zoom
	// bug). The legacy single Camera stays = primary's (the saving workspace).
	if len(secondary.Metadata.Cameras) > 0 {
		if primary.Metadata.Cameras == nil {
			primary.Metadata.Cameras = make(map[string]scene.CameraJSON, len(secondary.Metadata.Cameras))
		}
		for stage, cam := range secondary.Metadata.Cameras {
			primary.Metadata.Cameras[stage] = cam
		}
	}

	out, err := json.Marshal(primary)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ScheduleBackupSave debounces a combined backup save. Each scene change resets
// the 3s timer so a drag does not save on every frame; when it fires, saveBackup
// persists the combined document (both stages). In dual mode the ViewManager
// makes ONE workspace's ScheduleBackupSave the project's backup owner and points
// every workspace at it (see OnExport / backupScheduler), so the two stages do
// not race to create the backup file.
func (w *Workspace) ScheduleBackupSave() {
	if w.backupDebounce != nil {
		w.backupDebounce.Stop()
	}
	// saveBackup re-captures the live combined scene, so the argument is unused.
	w.backupDebounce = time.AfterFunc(3*time.Second, func() {
		w.saveBackup("")
	})
}

func (w *Workspace) saveBackup(sceneJSON string) {
	// Persist the COMBINED document (both stages) so a restore brings back
	// backend logic and frontend dashboard together. The sceneJSON arg is
	// this workspace's own scene (captured by OnExport); captureCombinedScene
	// re-reads the live state and folds in the sibling stage.
	sceneJSON = w.captureCombinedScene()
	deviceCount := countDevicesInJSON(sceneJSON)

	// Determine backup name based on whether a file is currently open.
	backupName := "unsaved (backup)"
	if w.currentFileName != "" {
		backupName = w.currentFileName + " (backup)"
	}

	if w.backupFileID != "" {
		// Update existing backup.
		err := stagefileclient.UpdateFile(w.backupFileID, "", "", "", sceneJSON, "", deviceCount)
		if err != nil {
			log.Printf("[Workspace:%s] Backup update error: %v", w.Name, err)
			// Backup might have been deleted by user — clear ID and retry as create.
			w.backupFileID = ""
			w.saveBackup(sceneJSON)
			return
		}
		log.Printf("[Workspace:%s] Backup updated: %s", w.Name, backupName)
		return
	}

	// No known backup ID yet. Find an existing backup by name BEFORE creating:
	// the row may persist from a previous session (server-side backups outlive
	// an IDE reload) or have just been written by the other stage. Updating the
	// existing row avoids the 409 Conflict a blind create returns when the name
	// is already taken. Only create when the row is truly absent.
	if files, listErr := stagefileclient.ListFiles(""); listErr == nil {
		for _, f := range files {
			if f.Name == backupName {
				w.backupFileID = f.ID
				if err := stagefileclient.UpdateFile(w.backupFileID, "", "", "", sceneJSON, "", deviceCount); err != nil {
					log.Printf("[Workspace:%s] Backup update error: %v", w.Name, err)
					w.backupFileID = ""
					return
				}
				log.Printf("[Workspace:%s] Backup updated (adopted existing): %s", w.Name, backupName)
				return
			}
		}
	}

	// Truly absent — create a new backup file.
	//
	// The backup inherits the workspace's project language so its row in the
	// welcome modal shows the same chip as the parent scene would. w.Language
	// is set in Init from Config.Language.
	entry, err := stagefileclient.SaveBackupFile(backupName, "", w.Language, sceneJSON, deviceCount)
	if err != nil {
		log.Printf("[Workspace:%s] Backup create error: %v", w.Name, err)
		return
	}

	w.backupFileID = entry.ID
	log.Printf("[Workspace:%s] Backup created: %s (%s)", w.Name, backupName, entry.ID)
}

// deleteBackup deletes the backup file if one exists and cancels any pending
// debounce timer. Without cancelling the timer, a pending saveBackup() would
// fire after the delete and recreate an empty backup.
// Called after manual save (Ctrl+S, Save button) and when a new file is opened.
//
// Race with the sibling workspace.
//
// Backend and Frontend share a SINGLE backup row in the DB — the
// collision handler in saveBackup (which finds the existing row by
// name and reuses its ID) makes both workspaces converge on the
// same backupFileID. So when a manual save fires OnAfterSave in
// both workspaces, the FIRST deleteBackup call deletes the row and
// the SECOND gets a 404 from the server. That 404 is benign — the
// row IS gone, which is exactly the state we wanted — but the
// network tab shows a red entry and the log got a scary error
// line.
//
// Fix here: treat 404 as success. The contract of deleteBackup is
// "the backup row no longer exists by the time we return"; a 404
// from the server already satisfies that. The string match below
// is the surface the server presents today via fail() in
// stagefileapi/handlers.go (returns 404 with message "file not
// found"); if that envelope ever changes the test breaks visibly
// in dev and we can adjust. A proper sentinel error type in
// stagefileclient would be cleaner — deferred until other clients
// need the same distinction.
//
// Português: Race entre Backend e Frontend que compartilham o
// MESMO backup row. Primeiro DELETE: 200. Segundo: 404. 404 é
// estado correto (row sumiu) — tratamos como sucesso. Fix por
// string match na mensagem do server por enquanto; sentinel error
// no client pode vir depois se outros clients precisarem.
func (w *Workspace) deleteBackup() {
	// Cancel any pending debounce — prevents a queued saveBackup from
	// recreating the backup we are about to delete.
	if w.backupDebounce != nil {
		w.backupDebounce.Stop()
		w.backupDebounce = nil
	}

	if w.backupFileID == "" {
		return
	}

	err := stagefileclient.DeleteFile(w.backupFileID)
	switch {
	case err == nil:
		log.Printf("[Workspace:%s] Backup deleted: %s", w.Name, w.backupFileID)
	case strings.Contains(err.Error(), "not found"):
		// Sibling workspace already deleted it — see comment block
		// above. Log at info level so the event is still visible
		// during development but doesn't shout "error".
		log.Printf("[Workspace:%s] Backup already gone (sibling cleanup): %s",
			w.Name, w.backupFileID)
	default:
		log.Printf("[Workspace:%s] Backup delete error: %v", w.Name, err)
	}
	w.backupFileID = ""
}

// OpenWithSceneJSON loads a scene into this workspace using a scene
// JSON that the caller has already fetched. It reuses the same path
// the file manager UI follows for "Open" clicks (importScene +
// currentFile bookkeeping), so the workspace ends up in exactly the
// state it would be in if the user had opened the file manually.
//
// Why this exists as a public method instead of letting callers do
// the work themselves:
//
//   - importScene is unexported and intentionally so — every code
//     path that touches the stage state should funnel through one
//     surface that knows about the importing flag, the delayed
//     refresh, and the wire reconnection bookkeeping.
//
//   - The currentFile + backup cleanup state is private. A caller
//     that called importScene directly would leave the workspace
//     believing no file is open, breaking Ctrl+S and the backup
//     debounce contract on the very next change.
//
// This method exists for two callers today:
//
//  1. The welcome modal flow in main.go (Parcela 2b): when the
//     user picks an existing project from the recent list, main.go
//     calls ViewManager.OpenFile, which fans the scene JSON out to
//     each Workspace via this method.
//
//  2. (Future) Any in-IDE "Recent projects" or "Open by URL"
//     surface that needs to load by ID without going through the
//     file manager overlay.
//
// The fileID and fileName are mirrored into the workspace's
// currentFile tracking so Ctrl+S writes back to the same row.
//
// Português: Carrega um scene JSON já buscado pelo caller, reusando
// o pipeline do file manager (importScene + bookkeeping de
// currentFile e backup). Existe como público porque o caller
// (welcome modal via ViewManager) precisa carregar sem ir pela
// overlay do file manager, e importScene é privado pra forçar o
// fluxo único.
func (w *Workspace) OpenWithSceneJSON(fileID, fileName, sceneJSON string) {
	// Same two callbacks the file manager invokes on its own "Open"
	// path. Calling them in this order matches the contract every
	// other code path follows — importScene first builds the new
	// state, then OnFileOpened records what was opened. The
	// callback indirection (vs. inlining) is deliberate so that any
	// future enrichment to OnLoad / OnFileOpened (telemetry,
	// validation, last-used persistence) lives in exactly one place.
	if w.stageFileCfg.OnLoad != nil {
		w.stageFileCfg.OnLoad(sceneJSON)
	}
	if w.stageFileCfg.OnFileOpened != nil {
		w.stageFileCfg.OnFileOpened(fileID, fileName)
	}
}

// RestoreBackupWithSceneJSON loads a backup's scene into this workspace
// and points the workspace's "current file" tracking at the ORIGINAL
// file the backup was made from — not at the backup itself. The next
// Ctrl+S therefore writes back to the original row, and the
// OnAfterSave hook will delete the backup row as part of its usual
// cleanup.
//
// Why we use the original file ID (not the backup ID) as currentFileID:
//
// A backup is always written as "OriginalName (backup)". The user's
// intent when clicking "Restore" is "recover the in-progress state I
// had in OriginalName". If currentFileID pointed at the backup row,
// the next save would overwrite the BACKUP file and the original
// would never see the new content — exactly the opposite of what the
// user expects. Aiming Ctrl+S at the original makes the backup a
// transparent recovery mechanism: open the modal, click restore, work
// continues against the same file.
//
// When the original file cannot be found (deleted while the user was
// away, perhaps), we leave currentFileID empty. The next Ctrl+S then
// opens the file manager dialog so the maker can choose a new name
// — better than silently saving on top of the backup row.
//
// The backup row itself is left intact by this method. It will be
// deleted on the next manual save via the existing OnAfterSave →
// deleteBackup pipeline. Deleting it immediately would discard the
// safety net in a window where the user has restored but not yet
// re-saved.
//
// Português: Carrega scene do backup e aponta currentFileID pro
// arquivo ORIGINAL (não pro backup). Ctrl+S sobrescreve o original;
// o backup é apagado no OnAfterSave. Se o original sumiu, deixa
// currentFileID vazio (próximo Ctrl+S abre dialog).
func (w *Workspace) RestoreBackupWithSceneJSON(backupID, backupName, sceneJSON string) {
	// Step 1: import the scene first so the workspace shows the
	// restored content immediately, even if the original-file lookup
	// below fails. UX-wise the maker sees their work back regardless
	// of any naming edge case.
	if w.stageFileCfg.OnLoad != nil {
		w.stageFileCfg.OnLoad(sceneJSON)
	}

	// Step 2: figure out the original-file name by stripping the
	// " (backup)" suffix. This is the same convention saveBackup
	// uses when creating the row, so trimming reliably recovers
	// the intended target.
	originalName := strings.TrimSuffix(backupName, " (backup)")

	// Step 3: find the original by name. We accept that this is an
	// extra list round-trip — restore is a one-shot action at boot,
	// not a hot path, and the network cost is dwarfed by the user's
	// own reaction time on the modal.
	files, err := stagefileclient.ListFiles("")
	if err != nil {
		log.Printf("[Workspace:%s] Restore: list error: %v — leaving currentFile empty",
			w.Name, err)
		// Step 4a: record the backup itself as the active row. This
		// captures backupFileID so a subsequent deleteBackup call
		// can clean it up; currentFile stays empty so Ctrl+S opens
		// the save dialog.
		w.backupFileID = backupID
		w.currentFileID = ""
		w.currentFileName = originalName
		return
	}

	// Step 4b: look up by name. The first match wins — duplicate
	// names within the same folder are forbidden by the server's
	// unique constraint, so this is unambiguous in practice.
	var original *stagefileclient.StageFileEntry
	for i := range files {
		if !files[i].IsBackup && files[i].Name == originalName {
			original = &files[i]
			break
		}
	}

	if original != nil {
		// Step 5a: original found — Ctrl+S goes to original; the
		// backup is tracked so OnAfterSave deletes it.
		if w.stageFileCfg.OnFileOpened != nil {
			w.stageFileCfg.OnFileOpened(original.ID, original.Name)
		}
		// OnFileOpened resets backupFileID via deleteBackup in its
		// "fresh file opened" path, so we set backupFileID AFTER
		// the callback to ensure the restore's backup is the one
		// that gets cleaned up on the next save.
		w.backupFileID = backupID
		log.Printf("[Workspace:%s] Restored backup, Ctrl+S goes to original %q (%s)",
			w.Name, original.Name, original.ID)
		return
	}

	// Step 5b: original gone — track backup for cleanup, leave
	// currentFile empty so the next save shows the dialog.
	w.backupFileID = backupID
	w.currentFileID = ""
	w.currentFileName = originalName
	log.Printf("[Workspace:%s] Restored backup, original %q not found — next Ctrl+S will prompt",
		w.Name, originalName)
}

// checkAndRestoreBackup looks for backup files from a previous session.
// If one is found, it loads the most recent backup, imports the scene,
// and shows an informational popup to the user.
//
// This runs once at workspace startup (from Init, in a goroutine).
// Only the visible workspace restores — the invisible one skips silently.
//
// Português: Procura backups de uma sessão anterior. Se encontrar, carrega
// o mais recente, reconstrói o stage e mostra um alerta ao usuário.
func (w *Workspace) checkAndRestoreBackup() {
	// Only restore on the visible workspace.
	display := w.CanvasEl.Get("style").Get("display").String()
	if display == "none" {
		return
	}

	// List all files and find backups.
	files, err := stagefileclient.ListFiles("")
	if err != nil {
		log.Printf("[Workspace:%s] Backup check: list error: %v", w.Name, err)
		return
	}

	// Find the most recent backup (list is ordered by updated_at DESC,
	// with backups after normal files due to ORDER BY is_backup ASC).
	var backup *stagefileclient.StageFileEntry
	for i := range files {
		if files[i].IsBackup {
			backup = &files[i]
			break // first backup found = most recent
		}
	}

	if backup == nil {
		log.Printf("[Workspace:%s] Backup check: no backups found", w.Name)
		return
	}

	log.Printf("[Workspace:%s] Backup check: found %q (%s) — restoring", w.Name, backup.Name, backup.ID)

	// Load the full backup (including scene_json).
	full, err := stagefileclient.LoadFile(backup.ID)
	if err != nil {
		log.Printf("[Workspace:%s] Backup restore: load error: %v", w.Name, err)
		return
	}

	// Track the backup ID so it can be deleted on manual save.
	w.backupFileID = backup.ID

	// If the backup has an original file reference (e.g. "Robot arm (backup)"),
	// set the current file to the original so Ctrl+S saves to the right place.
	if strings.HasSuffix(backup.Name, " (backup)") {
		originalName := strings.TrimSuffix(backup.Name, " (backup)")
		for i := range files {
			if !files[i].IsBackup && files[i].Name == originalName {
				w.currentFileID = files[i].ID
				w.currentFileName = originalName
				log.Printf("[Workspace:%s] Backup restore: original file found: %s (%s)",
					w.Name, originalName, files[i].ID)
				break
			}
		}
		// If original not found, currentFileID stays empty — Ctrl+S will prompt.
		if w.currentFileID == "" {
			w.currentFileName = originalName
		}
	}

	// Import the scene from the backup.
	w.importScene(full.SceneJSON)

	// Show informational popup.
	backupLabel := backup.Name
	w.showBackupAlert(backupLabel)
}

// showBackupAlert displays a non-blocking informational popup telling the
// user that a backup from a previous session was restored.
//
// The popup is styled to match the IDE's Catppuccin Mocha theme and has
// a single OK button (44px min-height for tablet). Dismissible via OK,
// Escape, or backdrop click.
//
// Português: Exibe um popup informativo dizendo que um backup foi restaurado.
func (w *Workspace) showBackupAlert(backupName string) {
	doc := js.Global().Get("document")

	backdrop := doc.Call("createElement", "div")
	backdrop.Get("style").Set("cssText",
		"position:fixed;top:0;left:0;width:100vw;height:100vh;"+
			"background:rgba(0,0,0,0.45);z-index:99999;"+
			"display:flex;align-items:center;justify-content:center;")

	card := doc.Call("createElement", "div")
	card.Get("style").Set("cssText",
		"background:#1e1e2e;border:1px solid #45475a;border-radius:6px;"+
			"width:90%;max-width:400px;overflow:hidden;"+
			"box-shadow:0 12px 40px rgba(0,0,0,0.6);font-family:sans-serif;")

	// Header.
	header := doc.Call("createElement", "div")
	header.Get("style").Set("cssText",
		"height:36px;background:#313244;border-bottom:1px solid #45475a;"+
			"display:flex;align-items:center;padding:0 12px;")
	headerText := doc.Call("createElement", "span")
	headerText.Get("style").Set("cssText",
		"color:#89b4fa;font-size:13px;font-weight:600;")
	headerText.Set("textContent", translate.T("backupRestoredTitle", "Backup restored"))
	header.Call("appendChild", headerText)
	card.Call("appendChild", header)

	// Body.
	body := doc.Call("createElement", "div")
	body.Get("style").Set("cssText", "padding:20px;")

	msg := doc.Call("createElement", "p")
	msg.Get("style").Set("cssText",
		"color:#cdd6f4;font-size:14px;margin:0 0 8px;line-height:1.5;")
	msg.Set("textContent", translate.T("backupRestoredMsg",
		"Your previous unsaved work has been restored from a backup."))
	body.Call("appendChild", msg)

	nameEl := doc.Call("createElement", "p")
	nameEl.Get("style").Set("cssText",
		"color:#fab387;font-size:13px;font-weight:600;margin:0 0 8px;")
	nameEl.Set("textContent", backupName)
	body.Call("appendChild", nameEl)

	hint := doc.Call("createElement", "p")
	hint.Get("style").Set("cssText",
		"color:#a6adc8;font-size:12px;margin:0 0 20px;line-height:1.5;")
	hint.Set("textContent", translate.T("backupRestoredHint",
		"Press Ctrl+S to save your work, or continue editing."))
	body.Call("appendChild", hint)

	// OK button.
	btnRow := doc.Call("createElement", "div")
	btnRow.Get("style").Set("cssText",
		"display:flex;justify-content:flex-end;border-top:1px solid #45475a;padding-top:16px;")

	okBtn := doc.Call("createElement", "button")
	okBtn.Get("style").Set("cssText",
		"background:#89b4fa;color:#1e1e2e;border:none;border-radius:4px;"+
			"padding:10px 28px;font-size:13px;font-weight:600;cursor:pointer;"+
			"min-height:44px;")
	okBtn.Set("textContent", "OK")

	closeFn := func() {
		if backdrop.Get("parentNode").Truthy() {
			doc.Get("body").Call("removeChild", backdrop)
		}
	}

	okBtn.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			closeFn()
			return nil
		}))
	btnRow.Call("appendChild", okBtn)
	body.Call("appendChild", btnRow)
	card.Call("appendChild", body)

	backdrop.Call("addEventListener", "click",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("target").Equal(backdrop) {
				closeFn()
			}
			return nil
		}))

	doc.Call("addEventListener", "keydown",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Get("key").String() == "Escape" {
				closeFn()
			}
			return nil
		}))

	backdrop.Call("appendChild", card)
	doc.Get("body").Call("appendChild", backdrop)

	log.Printf("[Workspace:%s] Backup restored alert shown: %s", w.Name, backupName)
}

// =====================================================================
//  Stage image export — PNG screenshot with embedded scene JSON
//
//  Captures a screenshot of the entire stage (FitAll), embeds the scene
//  JSON using LSB steganography, and triggers a PNG download.
//
//  Português: Captura screenshot do stage inteiro, embute o JSON da cena
//  via esteganografia LSB, e dispara download do PNG.
// =====================================================================

// exportStageImage captures a PNG screenshot of the full stage with the
// scene JSON embedded via steganography. Must run in a goroutine.
//
// Steps:
//  1. Export compact JSON (no indentation).
//  2. Save current camera state.
//  3. FitAll (instant) to show all devices.
//  4. Wait two animation frames for render to complete.
//  5. getImageData from canvas.
//  6. Embed compressed JSON in pixel LSBs.
//  7. Create temp canvas, write modified pixels, toDataURL.
//  8. Trigger browser download.
//  9. Restore original camera state.
//
// Português: Captura PNG do stage completo com JSON embutido.
func (w *Workspace) exportStageImage() {
	// Step 1: export compact JSON.
	// Combined document (both stages) so the PNG round-trips backend logic
	// AND frontend dashboard — importScene re-routes each device by its Stage.
	sceneJSON := w.captureCombinedScene()
	var compactBuf bytes.Buffer
	if err := json.Compact(&compactBuf, []byte(sceneJSON)); err != nil {
		log.Printf("[Workspace:%s] Image export: compact error: %v", w.Name, err)
		return
	}
	compactJSON := compactBuf.Bytes()
	log.Printf("[Workspace:%s] Image export: compact JSON = %d bytes", w.Name, len(compactJSON))

	// Check that the stage has devices to capture.
	bounds := w.SceneMgr.GetWorldBounds()
	if bounds == nil {
		log.Printf("[Workspace:%s] Image export: no devices on stage", w.Name)
		return
	}

	// Pre-check capacity: the canvas must have enough pixels to hold the payload.
	// Each pixel stores 3 bits (R, G, B LSBs), and the steganography header adds
	// 10 bytes. Gzip typically achieves ~80% compression, but we use the raw size
	// as a conservative upper bound for the check.
	canvasW, canvasH := w.Stage.GetCanvasSize()
	pixelCount := canvasW * canvasH
	capacityBytes := pixelCount * 3 / 8
	// After gzip the payload shrinks, but the header (10 bytes) is always present.
	// Use a rough 50% estimate for gzip to warn early.
	estimatedPayload := len(compactJSON)/2 + 10
	if estimatedPayload > capacityBytes {
		log.Printf("[Workspace:%s] Image export: project too large (~%d KB) for canvas (%dx%d, ~%d KB capacity)",
			w.Name, len(compactJSON)/1024, canvasW, canvasH, capacityBytes/1024)
		overlay.ShowError("Image Export",
			fmt.Sprintf("Project too large for this canvas size.\nJSON: %d KB, Capacity: ~%d KB",
				len(compactJSON)/1024, capacityBytes/1024))
		return
	}

	// Step 2: save all visual state that will be modified for capture.
	savedOffsetX := w.Camera.OffsetX
	savedOffsetY := w.Camera.OffsetY
	savedZoom := w.Camera.Zoom
	savedMinimap := w.Camera.IsMinimapEnabled()
	savedInfo := w.Camera.InfoEnabled
	savedOrigin := w.Camera.OriginEnabled
	savedGrid := w.Camera.GridEnabled

	// Step 3: configure for clean capture — white background, no overlays.
	w.Stage.SetBackgroundColor("#FFFFFF")
	w.Camera.SetMinimapEnabled(false)
	w.Camera.InfoEnabled = false
	w.Camera.OriginEnabled = false
	w.Camera.GridEnabled = false
	// The menu only exists on the backend workspace (see main-menu
	// conditional construction in Init). Guard for nil so the
	// frontend can still export screenshots cleanly.
	if w.Menu != nil {
		w.Menu.SetVisible(false)
	}

	// Step 4: FitAll to show every device with padding.
	w.Camera.FitAll(bounds.X, bounds.Y, bounds.Width, bounds.Height,
		canvasW, canvasH, 40, 0) // 40px padding, instant (0ms)
	w.Stage.MarkDirty()

	// Step 5: wait for render — two animation frames to ensure the camera
	// change is applied and all elements are redrawn with white background.
	waitFrame := func() {
		ch := make(chan struct{})
		var fn js.Func
		fn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			fn.Release()
			close(ch)
			return nil
		})
		js.Global().Call("requestAnimationFrame", fn)
		<-ch
	}
	waitFrame()
	waitFrame()

	// Step 6: getImageData from canvas.
	ctx := w.CanvasEl.Call("getContext", "2d")
	imgData := ctx.Call("getImageData", 0, 0, canvasW, canvasH)
	dataArray := imgData.Get("data") // Uint8ClampedArray

	pixelLen := dataArray.Get("length").Int()
	pixels := make([]byte, pixelLen)
	js.CopyBytesToGo(pixels, dataArray)

	log.Printf("[Workspace:%s] Image export: canvas %dx%d, %d bytes of pixel data",
		w.Name, canvasW, canvasH, pixelLen)

	// Step 7: embed compressed JSON in pixel LSBs.
	if err := steganography.Embed(pixels, compactJSON); err != nil {
		log.Printf("[Workspace:%s] Image export: embed error: %v", w.Name, err)
		overlay.ShowError("Image Export", fmt.Sprintf("Failed to embed data:\n%v", err))
		w.restoreAfterCapture(savedOffsetX, savedOffsetY, savedZoom,
			savedMinimap, savedInfo, savedOrigin, savedGrid)
		return
	}

	// Step 8: write modified pixels to a temporary canvas and export as PNG.
	js.CopyBytesToJS(dataArray, pixels)

	doc := js.Global().Get("document")
	tempCanvas := doc.Call("createElement", "canvas")
	tempCanvas.Set("width", canvasW)
	tempCanvas.Set("height", canvasH)
	tempCtx := tempCanvas.Call("getContext", "2d")
	tempCtx.Call("putImageData", imgData, 0, 0)

	dataURL := tempCanvas.Call("toDataURL", "image/png").String()

	// Step 9: trigger browser download.
	filename := "iotmaker-stage.png"
	if w.currentFileName != "" {
		filename = w.currentFileName + ".png"
	}
	a := doc.Call("createElement", "a")
	a.Set("href", dataURL)
	a.Set("download", filename)
	doc.Get("body").Call("appendChild", a)
	a.Call("click")
	doc.Get("body").Call("removeChild", a)

	log.Printf("[Workspace:%s] Image export: PNG downloaded (%s, %d bytes JSON, %d bytes gzipped est.)",
		w.Name, filename, len(compactJSON), estimatedPayload)

	// Step 10: restore all visual state.
	w.restoreAfterCapture(savedOffsetX, savedOffsetY, savedZoom,
		savedMinimap, savedInfo, savedOrigin, savedGrid)
}

// restoreAfterCapture restores all visual state modified by exportStageImage.
// Extracted into a helper so it can be called from both the success and error paths.
func (w *Workspace) restoreAfterCapture(
	offsetX, offsetY, zoom float64,
	minimap, info, origin, grid bool,
) {
	w.Stage.SetBackgroundColor("transparent")
	w.Camera.OffsetX = offsetX
	w.Camera.OffsetY = offsetY
	w.Camera.Zoom = zoom
	w.Camera.SetMinimapEnabled(minimap)
	w.Camera.InfoEnabled = info
	w.Camera.OriginEnabled = origin
	w.Camera.GridEnabled = grid
	// Guard matches the SetVisible(false) above — frontend has no menu.
	if w.Menu != nil {
		w.Menu.SetVisible(true)
	}
	w.Stage.MarkDirty()
}

// =====================================================================
//  Stage image import — extract scene JSON from PNG steganography
//
//  Reads a PNG file (dropped or selected via file picker), draws it onto
//  a temporary canvas to obtain pixel data, extracts the embedded scene
//  JSON via steganography, and calls importScene() to reconstruct the stage.
//
//  Português: Lê um PNG, extrai o JSON embutido via esteganografia,
//  e reconstrói o stage.
// =====================================================================

// importFromImageFile reads a JS File object (from drag-and-drop or file input),
// extracts the embedded scene JSON, and imports it.
// Must be called from a goroutine.
func (w *Workspace) importFromImageFile(file js.Value) {
	fileName := file.Get("name").String()
	log.Printf("[Workspace:%s] Image import: reading %s", w.Name, fileName)

	// Read file as ArrayBuffer using FileReader.
	ch := make(chan js.Value, 1)
	reader := js.Global().Get("FileReader").New()

	var onLoad js.Func
	onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onLoad.Release()
		ch <- reader.Get("result")
		return nil
	})
	var onError js.Func
	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onError.Release()
		ch <- js.Null()
		return nil
	})

	reader.Call("addEventListener", "load", onLoad)
	reader.Call("addEventListener", "error", onError)
	reader.Call("readAsArrayBuffer", file)

	result := <-ch
	if result.IsNull() || result.IsUndefined() {
		log.Printf("[Workspace:%s] Image import: FileReader error", w.Name)
		return
	}

	// Create a Blob URL from the ArrayBuffer and load as Image.
	blob := js.Global().Get("Blob").New(
		js.Global().Get("Array").New(result),
		map[string]interface{}{"type": "image/png"},
	)
	blobURL := js.Global().Get("URL").Call("createObjectURL", blob)

	w.importFromImageURL(blobURL.String(), fileName)

	js.Global().Get("URL").Call("revokeObjectURL", blobURL)
}

// importFromImageURL loads a PNG from a URL (blob: or http:), extracts the
// embedded JSON, and imports it. Must be called from a goroutine.
func (w *Workspace) importFromImageURL(url string, label string) {
	doc := js.Global().Get("document")

	// Load the image.
	img := doc.Call("createElement", "img")

	ch := make(chan bool, 1)
	var onLoad js.Func
	onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onLoad.Release()
		ch <- true
		return nil
	})
	var onError js.Func
	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onError.Release()
		ch <- false
		return nil
	})
	img.Call("addEventListener", "load", onLoad)
	img.Call("addEventListener", "error", onError)
	img.Set("src", url)

	if ok := <-ch; !ok {
		log.Printf("[Workspace:%s] Image import: failed to load image", w.Name)
		return
	}

	// Draw to a temporary canvas to get pixel data.
	imgW := img.Get("naturalWidth").Int()
	imgH := img.Get("naturalHeight").Int()

	tempCanvas := doc.Call("createElement", "canvas")
	tempCanvas.Set("width", imgW)
	tempCanvas.Set("height", imgH)
	tempCtx := tempCanvas.Call("getContext", "2d")
	tempCtx.Call("drawImage", img, 0, 0)

	imgData := tempCtx.Call("getImageData", 0, 0, imgW, imgH)
	dataArray := imgData.Get("data")

	pixelLen := dataArray.Get("length").Int()
	pixels := make([]byte, pixelLen)
	js.CopyBytesToGo(pixels, dataArray)

	log.Printf("[Workspace:%s] Image import: %dx%d, %d bytes pixel data",
		w.Name, imgW, imgH, pixelLen)

	// Extract embedded JSON.
	jsonBytes, err := steganography.Extract(pixels)
	if err != nil {
		log.Printf("[Workspace:%s] Image import: no embedded data in %s: %v", w.Name, label, err)
		overlay.ShowError(
			translate.T("imageImportErrorTitle", "Image Import"),
			translate.T("imageImportErrorMsg", "This image does not contain embedded stage data."),
		)
		return
	}

	log.Printf("[Workspace:%s] Image import: extracted %d bytes of JSON from %s",
		w.Name, len(jsonBytes), label)

	// Reconstruct the stage. In dual-workspace mode the scene must reach BOTH
	// workspaces (each replays its own stage's devices and restores its own
	// camera), exactly like backup-restore — the steganography extraction is
	// per-workspace (it needs this workspace's canvas) but the import must fan
	// out, otherwise the sibling stage keeps stale devices and the wrong camera.
	// importBroadcastFn is wired by the ViewManager; unset = single-workspace.
	if w.importBroadcastFn != nil {
		w.importBroadcastFn(string(jsonBytes))
	} else {
		w.importScene(string(jsonBytes))
	}

	// Show success toast-style notification via a brief overlay.
	log.Printf("[Workspace:%s] Image import: stage reconstructed from %s", w.Name, label)
}

// showWireToast shows a brief, self-contained notification near the top of the
// stage. Used to explain a connect rejection (no compatible target) so the
// click is not a silent dead-end. Self-removing after ~3s; no external toast
// dependency (the IDE page does not load the portal's utils.js toast, and that
// toast is an ES-module export rather than a global anyway).
//
// Português: Mostra uma notificação breve e autocontida no topo da stage,
// explicando uma rejeição de conexão (sem alvo compatível) — evita o clique
// silencioso. Auto-removível (~3s); sem dependência externa de toast.
func (w *Workspace) showWireToast(message string) {
	doc := js.Global().Get("document")
	if doc.IsUndefined() {
		return
	}
	el := doc.Call("createElement", "div")
	el.Set("textContent", message)
	// Catppuccin Mocha: surface0 background, red text + border for a rejection,
	// rounded, fixed near the top-centre, above everything, click-through.
	el.Get("style").Set("cssText",
		"position:fixed;top:64px;left:50%;transform:translateX(-50%);"+
			"background:#313244;color:#f38ba8;border:1px solid #f38ba8;"+
			"padding:10px 16px;border-radius:8px;"+
			"font:13px -apple-system,system-ui,sans-serif;"+
			"z-index:100000;box-shadow:0 4px 12px rgba(0,0,0,0.4);"+
			"pointer-events:none;")
	doc.Get("body").Call("appendChild", el)

	// Detach after a few seconds; the single callback releases itself.
	var remove js.Func
	remove = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		remove.Release()
		if el.Truthy() {
			el.Call("remove")
		}
		return nil
	})
	js.Global().Call("setTimeout", remove, 3000)
}

// openImageFilePicker opens a hidden <input type="file"> picker for PNG files.
// When the user selects a file, it is processed the same way as a drag-and-drop.
// Called from the file manager's "Import Image" button.
// Must be called from a goroutine.
//
// Português: Abre um seletor de arquivo PNG oculto. Quando o usuário seleciona
// um arquivo, ele é processado da mesma forma que um drag-and-drop.
func (w *Workspace) openImageFilePicker() {
	doc := js.Global().Get("document")

	input := doc.Call("createElement", "input")
	input.Set("type", "file")
	input.Set("accept", "image/png")
	input.Get("style").Set("display", "none")
	doc.Get("body").Call("appendChild", input)

	ch := make(chan js.Value, 1)
	var onChange js.Func
	onChange = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onChange.Release()
		files := input.Get("files")
		if files.Length() > 0 {
			ch <- files.Index(0)
		} else {
			ch <- js.Null()
		}
		// Clean up the hidden input.
		doc.Get("body").Call("removeChild", input)
		return nil
	})
	input.Call("addEventListener", "change", onChange)
	input.Call("click")

	file := <-ch
	if file.IsNull() || file.IsUndefined() {
		return
	}

	w.importFromImageFile(file)
}

// =====================================================================
//  Stage import — reconstruct canvas from saved JSON
//
//  Português: Import de stage — reconstrói o canvas a partir do JSON salvo
// =====================================================================

// importScene clears the current stage and reconstructs devices and wires
// from a saved SceneJSON string.
//
// Steps:
//  1. Parse the JSON into a SceneJSON struct.
//  2. Remove all existing devices (visual elements, connectors, wires).
//  3. Create each device at its saved position via Factory.CreateByType.
//  4. Build oldID→newID mapping (devices get new sequential IDs on creation).
//  5. Reconnect wires using WireMgr.ConnectDirect with translated IDs.
//  6. Notify the scene serializer to update its state.
//
// ID mapping is necessary because each device generates a new sequential ID
// inside its Init() method (e.g. "stmAdd_1" → "stmAdd_3"). The saved JSON
// wires reference the original IDs, so they must be translated.
//
// BlackBox devices are logged and skipped — their import requires the
// matching definition to be loaded from the server.
//
// Português:
//
//	Limpa o stage atual e reconstrói devices e fios a partir de um JSON salvo.
//	Mapeamento de IDs é necessário porque cada device gera um novo ID sequencial.
func (w *Workspace) importScene(sceneJSON string) {
	w.importing = true

	var sc scene.SceneJSON
	if err := json.Unmarshal([]byte(sceneJSON), &sc); err != nil {
		log.Printf("[Workspace:%s] Import parse error: %v", w.Name, err)
		overlay.ShowError("Import Error", fmt.Sprintf("Failed to parse scene JSON:\n%v", err))
		w.importing = false
		return
	}

	// Step 1: clear existing stage — removes all devices, connectors, and wires.
	w.SceneMgr.RemoveAll()

	// Brief pause to let destroy animations and DOM cleanup finish.
	time.Sleep(100 * time.Millisecond)

	// Step 2: create devices at their saved positions and build ID mapping.
	// oldID → newID so wires can be reconnected with the correct references.
	// Each device gets a new sequential ID in Init(), so the mapping is essential.
	idMap := make(map[string]string, len(sc.Devices))
	created := 0
	skipped := 0
	foreign := 0 // devices owned by the OTHER stage — replayed by that workspace's own pass

	for _, dev := range sc.Devices {
		// Stage routing for the combined document: each device carries the
		// stage it belongs to (DeviceJSON.Stage). A device tagged for the
		// OTHER workspace is not ours to recreate — the sibling's import
		// pass handles it — so skip it here. This is what stops the Frontend
		// from materialising backend logic nodes (and vice versa). An empty
		// Stage is a legacy scene saved before stage tagging; those import
		// unfiltered so old backups and PNGs still load.
		if dev.Stage != "" && dev.Stage != w.Name {
			foreign++
			continue
		}

		ok := w.Factory.CreateByType(dev.Type, dev.Position.X, dev.Position.Y)
		if !ok {
			skipped++
			continue
		}

		// Get the SceneDevice just created for ID mapping, property and size restoration.
		lastDev := w.SceneMgr.LastDevice()
		if lastDev == nil {
			continue
		}
		newID := lastDev.GetID()
		idMap[dev.ID] = newID
		log.Printf("[Workspace:%s] Import device: %s (%s) → %s", w.Name, dev.ID, dev.Type, newID)

		// Restore size — only if it differs from the default created by Init.
		// This is critical for two reasons:
		//   1. Loop containers need explicit resize (user-resized dimensions).
		//   2. Const devices (Bool, ConstInt, etc.) add KLabelHeight internally
		//      in SetSize, but the saved outerBBox already includes it. Calling
		//      SetSize with the outerBBox height would add KLabelHeight twice.
		//      Since Const devices have resize disabled, their saved size always
		//      equals the default — comparing catches this and skips the call.
		if dev.Size.Width > 0 && dev.Size.Height > 0 {
			currentBBox := lastDev.GetOuterBBox()
			widthDiff := dev.Size.Width - currentBBox.Width
			heightDiff := dev.Size.Height - currentBBox.Height
			if widthDiff > 2 || widthDiff < -2 || heightDiff > 2 || heightDiff < -2 {
				if sizable, sok := lastDev.(interface {
					SetSize(w, h rulesDensity.Density)
				}); sok {
					sizable.SetSize(
						rulesDensity.Density(dev.Size.Width),
						rulesDensity.Density(dev.Size.Height),
					)
					log.Printf("[Workspace:%s] Import: resized %s to (%.0f, %.0f)",
						w.Name, newID, dev.Size.Width, dev.Size.Height)
				}
			}
		}

		// Restore the frontend (dashboard) node's position for dual devices.
		// Position above placed the backend node; CreateByType set BOTH the
		// backend and frontend nodes to it, so without this the dashboard node
		// would sit at the backend coordinates. FrontendPosition, when present,
		// carries the dashboard node's own x,y — apply it so each stage keeps
		// the spot the maker chose. Non-dual devices never carry it and never
		// satisfy the interface, so this is a no-op for them.
		if dev.FrontendPosition != nil {
			if fp, fok := lastDev.(interface {
				SetFrontendPosition(x, y rulesDensity.Density)
			}); fok {
				fp.SetFrontendPosition(
					rulesDensity.Density(dev.FrontendPosition.X),
					rulesDensity.Density(dev.FrontendPosition.Y),
				)
				log.Printf("[Workspace:%s] Import: frontend position of %s set to (%.0f, %.0f)",
					w.Name, newID, dev.FrontendPosition.X, dev.FrontendPosition.Y)
			}
		}

		// Restore properties (value, label, dataType, etc.). The conversion from
		// the JSON map[string]interface{} to the flat map[string]string that
		// ApplyProperties expects lives in scene.ReplayProperties — the SAME
		// helper the context-menu "copy" action uses, so a reloaded device and a
		// copied device are configured identically. See that function for the
		// flattening rule (scalars, and BlackBox nested "props" → "prop_<key>").
		if inspectable, iok := lastDev.(scene.Inspectable); iok {
			scene.ReplayProperties(inspectable, dev.Properties)
		}

		// RefreshVisual for devices that need explicit visual recache after
		// SetSize (e.g. Loop — its ornament SVG must be regenerated to match
		// the new dimensions). Devices with ApplyProperties handle this via
		// their internal recacheSVG goroutine; Loop does not have ApplyProperties.
		if refreshable, rok := lastDev.(interface{ RefreshVisual() }); rok {
			go refreshable.RefreshVisual()
		}

		created++
	}

	// Step 3: wait for connectors to register before wiring.
	// Device creation is synchronous but connector registration involves DOM
	// manipulation that may need a tick to settle.
	time.Sleep(50 * time.Millisecond)

	// Step 4: reconnect wires using translated IDs.
	wired := 0
	for _, wr := range sc.Wires {
		// Translate old device IDs to new ones.
		fromDevice, fromOK := idMap[wr.From.Device]
		toDevice, toOK := idMap[wr.To.Device]

		if !fromOK {
			log.Printf("[Workspace:%s] Import wire %s: source device %s not found in ID map — skipping",
				w.Name, wr.ID, wr.From.Device)
			continue
		}
		if !toOK {
			log.Printf("[Workspace:%s] Import wire %s: target device %s not found in ID map — skipping",
				w.Name, wr.ID, wr.To.Device)
			continue
		}

		fromID := wire.ConnectorID{ElementID: fromDevice, PortName: wr.From.Port}
		toID := wire.ConnectorID{ElementID: toDevice, PortName: wr.To.Port}

		_, err := w.WireMgr.ConnectDirect(fromID, toID)
		if err != nil {
			log.Printf("[Workspace:%s] Import wire %s error: %v", w.Name, wr.ID, err)
		} else {
			wired++
		}
	}

	// Step 5: restore camera position and zoom.
	// The saved JSON stores the camera state at the time of export. Prefer
	// THIS workspace's own camera from the combined document (keyed by stage)
	// so each stage keeps its own viewport — the frontend must not inherit the
	// backend's zoom/offset. Fall back to the single Camera for legacy scenes
	// saved before per-stage cameras existed.
	cam := sc.Metadata.Camera
	if staged, ok := sc.Metadata.Cameras[w.Name]; ok {
		cam = staged
	}
	if cam.Zoom > 0 {
		w.Camera.OffsetX = cam.OffsetX
		w.Camera.OffsetY = cam.OffsetY
		w.Camera.Zoom = cam.Zoom
		w.Stage.MarkDirty()
		log.Printf("[Workspace:%s] Camera restored: offset=(%.0f,%.0f) zoom=%.2f",
			w.Name, w.Camera.OffsetX, w.Camera.OffsetY, w.Camera.Zoom)
	}

	// Restore per-case child membership now that idMap is complete. For the
	// N-way StatementCase, which contained child belongs to which case is not
	// carried by ApplyProperties — StatementCase is not scene.Inspectable, and
	// the properties map holds neither the cases' []string members nor the
	// import's old→new ID remap. Without this every contained child is dumped
	// into the first case on reload. Run it BEFORE the NotifyChange below so the
	// assignNewChildren that NotifyChange triggers becomes a no-op.
	w.restoreImportedCases(sc.Devices, idMap)

	// Step 6: update scene state.
	w.SceneMgr.NotifyChange()

	// Step 7: delayed refresh — ApplyProperties runs in goroutines with 200ms
	// delay, so the initial NotifyChange above captures pre-property state.
	// This second pass ensures the scene JSON reflects all restored properties
	// and the visuals are fully recached.
	go func() {
		time.Sleep(500 * time.Millisecond)
		w.SceneMgr.NotifyChange()
		w.Stage.MarkDirty()
		w.importing = false
		log.Printf("[Workspace:%s] Import: delayed refresh complete", w.Name)
	}()

	log.Printf("[Workspace:%s] Import complete: %d devices created, %d skipped, %d routed to other stage, %d wires connected",
		w.Name, created, skipped, foreign, wired)
}

// mergeTreeHelp walks the menu tree recursively and adds every node's
// help tabs into the readmes map. The key matches the MenuItem.ID
// that the menu builder assigns:
//   - devices:  "bb_" + DeviceStructName
//   - templates: "tmpl_" + id (lowercase)
//   - others:   SlotID as-is (sections, categories, system items)
//
// Tree help takes lower priority — it does not overwrite readmes that
// were already registered from BlackBoxDefClient.Help.Readme. Devices'
// own source-code-embedded help wins because the specialist who wrote
// the device is the authority on its behaviour; the admin's
// admin-edited menu help is a fallback for system / section / category
// items that have no specialist behind them.
//
// HelpTabs is the ordered slice resolved by the server's cascade —
// one element per markdown tab the admin authored in the (profile,
// locale) bucket that won the cascade. The server already truncated
// titles and rewrote image references. We just copy the slice through
// to the panel, where its length drives whether a tab bar is rendered
// (len > 1) or a single page (len == 1).
func mergeTreeHelp(nodes []mainMenu.RailSlot, readmes map[string][]blackbox.HelpTabClient) {
	for _, node := range nodes {
		if len(node.HelpTabs) > 0 {
			var key string
			switch node.SlotType {
			case "device":
				if node.DeviceStructName != "" {
					key = "bb_" + node.DeviceStructName
				} else {
					key = node.SlotID
				}
			case "template":
				// SlotID is "Tmpl_abc123" → need "tmpl_abc123"
				key = "tmpl_" + strings.TrimPrefix(node.SlotID, "Tmpl_")
			default:
				key = node.SlotID
			}

			// Do not overwrite device readmes from the BlackBoxDefClient —
			// those come from the specialist's source code and are more
			// authoritative than admin-edited tree help.
			if _, exists := readmes[key]; !exists {
				// Copy through; the panel reads node.HelpTabs by
				// value, and Go slices share backing arrays so this
				// is a cheap reference assignment, not a deep copy.
				readmes[key] = node.HelpTabs
			}
		}

		// Recurse into children.
		if len(node.Children) > 0 {
			mergeTreeHelp(node.Children, readmes)
		}
	}
}

// extractEmbeddedDefs recursively walks the menu tree and parses every
// device node's DeviceParsedJSON into a BlackBoxDefClient. These are
// devices from admin-curated sections (Sparkfun, etc.) that belong to
// other users — the current user's own devices come from /api/v1/blackbox.
//
// The returned defs can be merged into bbDefs so both the menu builder
// and the device factory have access to them. This lets curated section
// devices appear in the IDE menu AND be placed on the canvas.
//
// Nodes with empty DeviceParsedJSON or invalid JSON are silently skipped.
//
// ── Ownership stamping ──────────────────────────────────────────────────
//
// Every def returned here is stamped with Origin = blackbox.OriginCurated
// and IsOwn = false. The rationale is subtle: the DeviceParsedJSON blob
// is the worker's neutral serialisation of the device, shared across
// contexts — the same blob represents "mine" when fetched from
// /api/v1/blackbox and "curated" when embedded in a promoted section.
// The server cannot write "curated" into the stored parsed_json without
// corrupting the owner's own view of their device. Therefore the
// "curated" marker is assigned here, on the client, after unmarshal.
//
// For the rare case of an Official Specialist promoting THEIR OWN device
// into a curated section, the same device arrives through both channels:
// once from /api/v1/blackbox (stamped "own") and once embedded here
// (stamped "curated"). The caller (workspace.Init, below) deduplicates
// by Name and keeps the "own" record — the device correctly surfaces
// under BOTH "My Items" and the promoted section. Do NOT remove that
// dedup; it is the safety net that makes this stamping correct in edge
// cases.
//
// See /ide/docs/tasks/REFACTOR_MY_ITEMS_PHASE_1.md for the full migration
// rationale.
func extractEmbeddedDefs(nodes []mainMenu.RailSlot) []*blackbox.BlackBoxDefClient {
	var defs []*blackbox.BlackBoxDefClient

	for _, node := range nodes {
		if node.SlotType == "device" && node.DeviceParsedJSON != "" && node.DeviceParsedJSON != "{}" {
			var def blackbox.BlackBoxDefClient
			if err := json.Unmarshal([]byte(node.DeviceParsedJSON), &def); err != nil {
				log.Printf("[extractEmbeddedDefs] skipping %s: unmarshal error: %v",
					node.SlotID, err)
				continue
			}
			// Sanity check: the def must have a name that matches the tree node.
			if def.Name == "" {
				log.Printf("[extractEmbeddedDefs] skipping %s: parsed def has empty name",
					node.SlotID)
				continue
			}
			if node.DeviceStructName != "" && def.Name != node.DeviceStructName {
				log.Printf("[extractEmbeddedDefs] warning: %s struct name mismatch: tree=%s, def=%s",
					node.SlotID, node.DeviceStructName, def.Name)
			}

			// Stamp ownership. The stored parsed_json is context-neutral;
			// provenance is assigned by the channel through which the def
			// arrived. Anything coming through this function is, by
			// definition, curated. IsOwn=false is asserted explicitly
			// (not left to the json zero value) so a server that one day
			// starts populating the field cannot accidentally flip it on.
			def.Origin = blackbox.OriginCurated
			def.IsOwn = false

			defs = append(defs, &def)
		}

		// Recurse into children (sections → categories → subcategories → devices).
		if len(node.Children) > 0 {
			defs = append(defs, extractEmbeddedDefs(node.Children)...)
		}
	}

	return defs
}

// drawConflictHighlights paints visual feedback for every device
// currently in conflict — the scenegraph is consulted fresh each
// frame, so when the user moves the offending device back into place
// the highlight vanishes on the very next render without any listener
// bookkeeping.
//
// Two strokes per container-involving conflict (Straddle, PiercedOuter):
//
//   - Solid red border around the offending device (its OuterBBox).
//     This is the "where is the error" answer — the first thing the
//     user needs to spot.
//   - Dashed colored border around the container's inner area.
//     This is the "where it should go" answer — the usable zone the
//     device was supposed to fit into.
//
// For Simple-Simple Overlap (no container), only the solid red border
// is drawn on the offender.
//
// The pattern mirrors wire.Manager.Draw: save the current canvas
// transform, re-apply the camera matrix so coordinates are read in
// world space, emit the strokes, then restore. This keeps the draw
// calls self-contained and avoids any interaction with the sprite
// engine's own transform lifecycle.
//
// Português:
//
//	Desenha feedback visual de cada conflito. Dois traços: vermelho
//	sólido no device em erro (onde está o problema) e tracejado colorido
//	no inner do container (onde deveria estar). Overlap Simple-Simple
//	só desenha o device. Transformação de câmera aplicada localmente.
func (w *Workspace) drawConflictHighlights() {
	if w.SceneMgr == nil || !w.canvasCtx.Truthy() {
		return
	}

	highlights := w.SceneMgr.ConflictHighlights()
	if len(highlights) == 0 {
		return
	}

	cam := w.Stage.GetCamera()
	offX, offY, zoom := 0.0, 0.0, 1.0
	if cam != nil {
		offX, offY, zoom = cam.OffsetX, cam.OffsetY, cam.Zoom
	}
	if zoom <= 0 {
		zoom = 1
	}

	ctx := w.canvasCtx
	ctx.Call("save")
	ctx.Call("setTransform", zoom, 0, 0, zoom, -offX*zoom, -offY*zoom)

	solidDash := js.Global().Get("Array").New()
	dashedPattern := js.Global().Get("Array").New()
	dashedPattern.Call("push", 6/zoom)
	dashedPattern.Call("push", 4/zoom)

	const deviceErrorColor = "#f38ba8" // Catppuccin Mocha red

	for _, h := range highlights {
		// Container: dashed, themed by kind, drawn first so the
		// device's solid outline sits visibly on top if they overlap.
		if h.ContainerRect != nil {
			ctx.Call("setLineDash", dashedPattern)
			ctx.Set("strokeStyle", conflictStrokeColor(h.Kind))
			ctx.Set("lineWidth", 2.0/zoom)
			ctx.Call("strokeRect",
				h.ContainerRect.X, h.ContainerRect.Y,
				h.ContainerRect.Width, h.ContainerRect.Height,
			)
		}

		// Device: solid red outline, thicker, to dominate attention.
		ctx.Call("setLineDash", solidDash)
		ctx.Set("strokeStyle", deviceErrorColor)
		ctx.Set("lineWidth", 3.0/zoom)
		ctx.Call("strokeRect",
			h.DeviceRect.X, h.DeviceRect.Y,
			h.DeviceRect.Width, h.DeviceRect.Height,
		)
	}

	// Reset dash to solid so nothing downstream inherits our pattern.
	ctx.Call("setLineDash", solidDash)
	ctx.Call("restore")
}

// conflictStrokeColor picks a stroke colour for a container highlight
// based on the conflict kind. Device highlights always use red; this
// is specifically for the container's "where it should go" border.
// Kept as a standalone helper so future tweaks (new kinds, theme
// variants) live in one place.
//
// Português: Cor do traço do container por tipo de conflito. Device
// sempre vermelho; esta função é só pro container.
func conflictStrokeColor(kind string) string {
	switch kind {
	case "straddle":
		return "#f38ba8" // red — most ambiguous
	case "pierced_outer":
		return "#fab387" // peach — less severe
	case "codegen_error":
		return "#f38ba8" // red — matches geometric severity
	default:
		return "#f9e2af" // yellow fallback
	}
}

// wireConnectObserver is implemented by devices that need to react when a wire
// is connected to one of their input ports — e.g. StatementCase, which infers
// its selector type from the connected wire's resolved data type.
//
// Português: Implementado por devices que reagem quando um fio é conectado a uma
// porta de entrada — ex.: StatementCase, que infere o tipo do seletor pelo tipo
// resolvido do fio.
type wireConnectObserver interface {
	OnWireConnected(portName, dataType string)
}
