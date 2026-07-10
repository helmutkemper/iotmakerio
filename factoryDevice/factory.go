// factoryDevice/factory.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryDevice

// factory.go — Device factory for creating IDE devices dynamically.
//
// English:
//
//	DeviceFactory encapsulates all shared dependencies and provides methods
//	to create each device type. It does NOT define menu structure — that
//	responsibility belongs to MenuBuilder.
//
// Português:
//
//	DeviceFactory encapsula todas as dependências compartilhadas e fornece
//	métodos para criar cada tipo de device. Ele NÃO define estrutura de
//	menu — essa responsabilidade pertence ao MenuBuilder.

import (
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/devices/compArray"
	"github.com/helmutkemper/iotmakerio/devices/compConsts"
	"github.com/helmutkemper/iotmakerio/devices/compDebug"
	"github.com/helmutkemper/iotmakerio/devices/compFlow"
	"github.com/helmutkemper/iotmakerio/devices/compFrontend"
	"github.com/helmutkemper/iotmakerio/devices/compLogic"
	"github.com/helmutkemper/iotmakerio/devices/compLoop"
	"github.com/helmutkemper/iotmakerio/devices/compMath"
	"github.com/helmutkemper/iotmakerio/devices/compVars"
	"github.com/helmutkemper/iotmakerio/grid"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/scene"
	"github.com/helmutkemper/iotmakerio/sprite"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
	"github.com/helmutkemper/iotmakerio/ui/mainMenu"
	"github.com/helmutkemper/iotmakerio/wire"
)

// DeviceFactory creates and initializes devices.
type DeviceFactory struct {
	Stage         sprite.Stage
	WireMgr       *wire.Manager
	SceneMgr      *scene.Serializer
	ResizeButton  block.ResizeButton
	DraggerButton block.ResizeButton
	GridAdjust    grid.Adjust
	SceneNotifyFn func()

	// Name is the workspace name ("frontend" or "backend").
	// Português: Nome do workspace ("frontend" ou "backend").
	Name string

	// HexMenu is the shared hex menu instance for this workspace.
	// All devices use this single menu — only one is visible at a time.
	//
	// Português: Instância compartilhada do menu hexagonal para este workspace.
	// Todos os devices usam este único menu — apenas um é visível por vez.
	HexMenu *mainMenu.SpriteHexMenu

	// ContextMenu is the linear context menu controller for the stage this
	// factory lives on (backend or frontend). Injected into pilot devices
	// during Delivery A; by Delivery B this replaces HexMenu for every
	// backend body menu and every frontend tap menu. HexMenu stays during
	// the hybrid period for port menus and the main-menu tutorial.
	//
	// Português: Controller do menu de contexto linear para o stage onde
	// este factory vive. Injetado nos pilotos durante a Delivery A.
	ContextMenu *contextMenu.Controller

	// OtherContextMenu is the controller of the OTHER stage's context menu —
	// used by dual devices (backend + frontend views) so that both halves
	// of the device can open menus on their respective stages. Resolved by
	// dualContextMenus() the same way OtherStage is resolved by dualStages().
	//
	// IMPORTANT: callers must populate this field as part of factory setup,
	// not by reaching into the struct directly. Use WireDualFactories() —
	// it cross-links both factories in one shot and never forgets one
	// direction. Forgetting OtherContextMenu (while remembering OtherStage)
	// is exactly how the §3.1 bug of CHARTPRO_REFACTOR.md happened: dual
	// devices' frontend context menus chose to silently no-op instead of
	// erroring loudly.
	//
	// Português: Controller do menu de contexto do OUTRO stage. Usado por
	// dispositivos duais. Setar via WireDualFactories() para evitar o §3.1.
	OtherContextMenu *contextMenu.Controller

	// CanvasEl is the <canvas> DOM element for the workspace.
	// Used by devices that need to create HTML overlays (e.g. inline editing).
	//
	// Português: Elemento DOM <canvas> do workspace.
	// Usado por devices que precisam criar overlays HTML (ex: edição inline).
	CanvasEl js.Value

	// PreviewCaseFn renders a StatementCase as source for its inspect-panel
	// Preview tab. Injected into each StatementCase via SetCodegenPreview in
	// CreateCase. Supplied by the Workspace (Workspace.previewCaseCode). nil
	// is tolerated by the device — the inspector then opens without a Preview
	// tab.
	//
	// Português: Renderiza um StatementCase como código para a aba Preview do
	// painel. Injetado em cada StatementCase via SetCodegenPreview no
	// CreateCase. Vem do Workspace. nil é tolerado pelo device.
	PreviewCaseFn compFlow.CodegenPreviewFunc

	// For dual devices (gauge, etc): reference to the OTHER workspace's stage.
	OtherStage sprite.Stage

	// LiveSendFunc is the callback for sending values to external hardware
	// via the live WebSocket connection. Set by main.go after the live client
	// is created. Passed to live-enabled devices (Gauge, etc.) at creation time.
	//
	// Signature: func(deviceID, port string, value interface{})
	//
	// Português: Callback para enviar valores ao hardware externo via WebSocket.
	// Definido pelo main.go após criar o cliente live.
	LiveSendFunc func(deviceID, port string, value interface{})

	// LiveReconnectRegistrar is the callback by which a device subscribes
	// to "WebSocket has just reconnected after a drop" notifications. The
	// caller (typically frontend display devices like ChartPro) passes a
	// nullary function; the registrar wires it into the live.Client's
	// listener list so it fires on every recovery.
	//
	// Set by main.go to liveClient.OnReconnect after the live client
	// is created. When nil, devices simply do not register — no harm.
	//
	// Signature: func(callback func())
	//
	// Português: Callback para registrar listeners de "WebSocket
	// reconectou". Devices de display (ChartPro) usam isso para marcar
	// FAIL na timeline quando a conexão é restaurada.
	LiveReconnectRegistrar func(callback func())

	// [PLACEMENT] Position and pending function for click-to-place mode.
	//
	// Português: Posição e função pendente para o modo click-to-place.
	nextPosX, nextPosY float64
	hasNextPos         bool

	placementPending bool
	placementName    string
	placementFn      func()

	// [HINT] true after the first placement hint has been shown.
	// The hint only appears once — the very first time a device is placed.
	hintShown bool

	// bbInstanceCache maps lowercase struct name → instanceId for black-box
	// devices. Ensures Init and Run of the same component always share one
	// instanceId even when placed via separate menu clicks.
	// See factoryBlackBox.go: bbInstanceId().
	bbInstanceCache map[string]string

	// bbDefs maps component name → definition for all loaded black-box devices.
	// Populated via SetBlackBoxDefs() at workspace startup. Used by CreateByType()
	// to reconstruct BlackBox devices during stage import.
	//
	// Português: Mapa nome→definição dos BlackBox carregados do servidor.
	// Usado por CreateByType() para reconstruir devices no import de stage.
	bbDefs map[string]*blackbox.BlackBoxDefClient
}

// dualStages resolves which sprite.Stage corresponds to the backend and which
// to the frontend. This is critical for dual devices (Gauge, LED, BarGraph,
// etc.) that must place one view on each workspace tab.
//
// When the factory belongs to the backend workspace:
//
//	f.Stage      = backend stage  → backendStage
//	f.OtherStage = frontend stage → frontendStage
//
// When the factory belongs to the frontend workspace:
//
//	f.Stage      = frontend stage → frontendStage
//	f.OtherStage = backend stage  → backendStage
//
// Without this resolution, components created from the frontend tab would
// have their views swapped — the backend SVG appearing on the frontend and
// vice-versa.
//
// Português: Resolve qual sprite.Stage é o backend e qual é o frontend.
// Sem essa resolução, componentes criados pela aba frontend teriam as views
// trocadas.
func (f *DeviceFactory) dualStages() (backendStage, frontendStage sprite.Stage) {
	if f.Name == "frontend" {
		// This factory lives on the frontend tab.
		// f.Stage IS the frontend; f.OtherStage IS the backend.
		return f.OtherStage, f.Stage
	}

	// Default: this factory lives on the backend tab (or any other name).
	// f.Stage IS the backend; f.OtherStage IS the frontend.
	return f.Stage, f.OtherStage
}

// dualContextMenus resolves which Controller is for the backend stage
// and which is for the frontend stage, mirroring dualStages exactly.
// Keeps the two helpers side-by-side so that adding a new dual-stage
// dependency in the future has an obvious shape to copy.
//
// Either return may be nil when only one workspace is active (the
// frontend-only or backend-only compile targets). Callers should nil-check
// before injecting into a device.
//
// Português: Resolve qual Controller é do backend e qual é do frontend,
// espelhando dualStages exatamente. Qualquer retorno pode ser nil quando
// só um workspace está ativo.
func (f *DeviceFactory) dualContextMenus() (backend, frontend *contextMenu.Controller) {
	if f.Name == "frontend" {
		return f.OtherContextMenu, f.ContextMenu
	}
	return f.ContextMenu, f.OtherContextMenu
}

// WireDualFactories cross-links a backend factory with a frontend
// factory so that dual devices (Gauge, ChartPro, BackgroundImage, …)
// can find both stages and both context-menu controllers no matter
// which tab the device was created from.
//
// Without this call dualStages() and dualContextMenus() will return
// nil on one side. The symptom is "frontend menu doesn't open after
// click on the device" — this is the §3.1 bug from
// CHARTPRO_REFACTOR.md, isolated to a single forgetful main.go line.
//
// Required usage in main.go (or wherever the workspaces are wired):
//
//	backendFactory := &factoryDevice.DeviceFactory{
//	    Name:        "backend",
//	    Stage:       backendStage,
//	    ContextMenu: backendCtxMenu,
//	    // ... other fields ...
//	}
//	frontendFactory := &factoryDevice.DeviceFactory{
//	    Name:        "frontend",
//	    Stage:       frontendStage,
//	    ContextMenu: frontendCtxMenu,
//	    // ... other fields ...
//	}
//	factoryDevice.WireDualFactories(backendFactory, frontendFactory)
//
// Call AFTER each factory has its own Stage and ContextMenu set, and
// BEFORE any device is created.
//
// Either argument may be nil for backend-only or frontend-only compile
// targets — the function is a safe no-op in that case.
//
// Adding new dual-stage fields: whenever a new pair Stage / OtherStage,
// ContextMenu / OtherContextMenu, etc. is added to the struct, mirror
// it inside this function. That way "I added a new dual field" stays
// a single editing point instead of two-and-pray.
//
// Português: Cruza referências entre as duas factories (backend e
// frontend) para que dispositivos duais encontrem tudo. Chamar uma
// vez, depois de cada factory já ter seus próprios Stage e
// ContextMenu setados. Argumentos nil são aceitos como no-op.
func WireDualFactories(backend, frontend *DeviceFactory) {
	if backend == nil || frontend == nil {
		// No-op when only one workspace is active. Dual devices in
		// such builds rely on the explicit nil branches inside
		// dualStages() and dualContextMenus().
		return
	}
	backend.OtherStage = frontend.Stage
	backend.OtherContextMenu = frontend.ContextMenu

	frontend.OtherStage = backend.Stage
	frontend.OtherContextMenu = backend.ContextMenu
}

// SetNextPosition stores the world coordinates where the next device should appear.
func (f *DeviceFactory) SetNextPosition(worldX, worldY float64) {
	f.nextPosX = worldX
	f.nextPosY = worldY
	f.hasNextPos = true
}

// devicePosition returns the position for a new device.
// If SetNextPosition was called, uses that position (and clears it).
// Otherwise falls back to the center of the visible canvas.
func (f *DeviceFactory) devicePosition() (x, y rulesDensity.Density) {
	if f.hasNextPos {
		f.hasNextPos = false
		return rulesDensity.Density(f.nextPosX), rulesDensity.Density(f.nextPosY)
	}
	return f.screenCenter()
}

// IsPlacing returns true if the factory is waiting for the user to click.
func (f *DeviceFactory) IsPlacing() bool {
	return f.placementPending
}

// ConfirmPlacement is called when the user clicks during placement mode.
// Sets the position, runs the pending creation function, and clears state.
//
// Português: Chamado quando o usuário clica durante o modo de posicionamento.
// Define a posição, executa a função de criação pendente, e limpa o estado.
func (f *DeviceFactory) ConfirmPlacement(worldX, worldY float64) {
	if !f.placementPending {
		return
	}
	f.SetNextPosition(worldX, worldY)
	fn := f.placementFn
	name := f.placementName

	// Clear state before running (prevents re-entry)
	f.placementPending = false
	f.placementFn = nil
	f.placementName = ""

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Factory] PANIC in %s: %v", name, r)
			}
		}()
		log.Printf("[Factory] Placing %s at (%.0f, %.0f)", name, worldX, worldY)
		fn()
	}()

	// Re-run the scene pass shortly after creation. A device's visual is an
	// SVG cached asynchronously (CacheFromSvg loads it via an image), so the
	// device's measured outer box is not final at the moment fn() fires its
	// own NotifyChange. The scenegraph therefore tests containment against a
	// not-yet-settled box and does NOT adopt a device dropped inside a
	// container — leaving it parented to root and visible in every case until
	// a manual drag triggers EndDrag and re-parents it. This delayed pass runs
	// once the render has settled: RefreshAll re-parents the device, which
	// fires OnParentChanged and lets the container assign it to the active
	// case. Mirrors the import path's delayed refresh, which exists for the
	// same "visuals settle later" reason.
	//
	// Português: Re-roda o scene pass logo após a criação. O visual do device é
	// um SVG cacheado de forma assíncrona (CacheFromSvg carrega via imagem),
	// então o box externo medido não está final quando o fn() dispara o
	// NotifyChange dele. O scenegraph testa contenção contra um box ainda não
	// assentado e NÃO adota um device solto dentro de um container — deixando-o
	// parenteado na raiz e visível em todos os cases até um drag manual disparar
	// o EndDrag e re-parentear. Este pass atrasado roda com o render já
	// assentado: RefreshAll re-parenteia o device, dispara o OnParentChanged e
	// deixa o container atribuí-lo ao case ativo. Espelha o refresh atrasado do
	// import, que existe pela mesma razão ("visuais assentam depois").
	go func() {
		time.Sleep(300 * time.Millisecond)
		if f.SceneMgr != nil {
			f.SceneMgr.NotifyChange()
		}
	}()
}

// CancelPlacement exits placement mode without creating a device.
func (f *DeviceFactory) CancelPlacement() {
	f.placementPending = false
	f.placementFn = nil
	f.placementName = ""
}

// screenCenter returns the center of the visible canvas in world coordinates.
func (f *DeviceFactory) screenCenter() (x, y rulesDensity.Density) {
	canvasW, canvasH := f.Stage.GetCanvasSize()
	cam := f.Stage.GetCamera()

	cx := float64(canvasW) / 2.0
	cy := float64(canvasH) / 2.0

	if cam != nil {
		zoom := cam.Zoom
		if zoom <= 0 {
			zoom = 1.0
		}
		cx = (cx - cam.OffsetX) / zoom
		cy = (cy - cam.OffsetY) / zoom
	}

	return rulesDensity.Density(cx), rulesDensity.Density(cy)
}

// makeOnRemove creates a callback that unregisters a device from the scene
// serializer when it is deleted. Called by each CreateXxx method.
//
// Português: Cria um callback que desregistra um device do scene serializer
// quando ele é excluído. Chamado por cada método CreateXxx.
func (f *DeviceFactory) makeOnRemove() func(id string) {
	return func(id string) {
		f.SceneMgr.Unregister(id)
		f.WireMgr.UnregisterContainer(id)

		// A delete IS a scene change, so notify: NotifyChange → OnExport →
		// ScheduleBackupSave persists the removal to the change-based backup.
		// Without this call the delete left NO new backup, so the removed device
		// reappeared on the next reload. This mirrors the NotifyChange every
		// CreateXxx method fires on add; the OnExport handler's `if w.importing`
		// guard keeps it from writing a backup during scene import (the only
		// other context, and one where Unregister is not called anyway).
		//
		// Português: Um delete É uma mudança de cena, então notifica:
		// NotifyChange → OnExport → ScheduleBackupSave persiste a remoção no
		// backup baseado em mudança. Sem esta chamada o delete não gerava backup
		// novo e o device removido voltava no reload. Espelha o NotifyChange que
		// todo CreateXxx dispara no add; a guarda `if w.importing` no OnExport
		// impede backup durante import.
		f.SceneMgr.NotifyChange()

		log.Printf("[Factory] Unregistered %s from scene", id)
	}
}

// SafeRun executes fn in a goroutine with panic recovery and a delay.
//
// In DevicePlaceOnClick mode, the function is saved and waits for the
// user to click on the canvas. The workspace calls ConfirmPlacement(x,y)
// which sets the position and runs the saved function.
//
// In DevicePlaceImmediate mode, the function runs immediately (legacy).
//
// Português: No modo DevicePlaceOnClick, a função é salva e espera o
// usuário clicar no canvas. O workspace chama ConfirmPlacement(x,y).
func (f *DeviceFactory) SafeRun(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Factory] PANIC in %s: %v", name, r)
			}
		}()
		time.Sleep(150 * time.Millisecond)

		if mainMenu.DevicePlacement == mainMenu.DevicePlaceOnClick {
			f.placementPending = true
			f.placementName = name
			f.placementFn = fn

			// Show hint only the very first time
			if !f.hintShown {
				f.hintShown = true
				f.showPlacementHint()
			}

			log.Printf("[Factory] Waiting for click to place %s", name)
			return
		}

		// Immediate mode (legacy)
		log.Printf("[Factory] Starting %s...", name)
		fn()
		log.Printf("[Factory] Completed %s", name)
	}()
}

// showPlacementHint flashes a hand/pointer icon at the center of the screen
// to tell the user "click somewhere to place the device". Shown only once.
// The icon flashes twice (fade in → fade out → fade in → fade out) then
// removes itself. Uses pointer-events:none so it never blocks clicks.
//
// Português: Pisca um ícone de mão/ponteiro no centro da tela para dizer
// ao usuário "clique em algum lugar para posicionar o device". Mostrado
// apenas uma vez. O ícone pisca duas vezes e depois se remove.
func (f *DeviceFactory) showPlacementHint() {
	doc := js.Global().Get("document")

	div := doc.Call("createElement", "div")
	div.Get("style").Set("cssText",
		"position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);"+
			"width:80px;height:80px;pointer-events:none;z-index:9998;opacity:0;"+
			"filter:drop-shadow(0 0 8px rgba(68,136,204,0.6));")

	// Hand/pointer SVG (Font Awesome Free v7.2.0)
	div.Set("innerHTML", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 640" width="80" height="80" fill="#4488CC">`+
		`<path d="M256 128C256 119.2 263.2 112 272 112C280.8 112 288 119.2 288 128L288 264C288 274.3 `+
		`294.6 283.5 304.4 286.8C314.2 290.1 325 286.7 331.2 278.5C334.2 274.6 338.8 272.1 344 272.1C352.8 `+
		`272.1 360 279.3 360 288.1C360 298.4 366.6 307.6 376.4 310.9C386.2 314.2 397 310.8 403.2 302.6C406.2 `+
		`298.7 410.8 296.2 416 296.2C423.8 296.2 430.3 301.8 431.7 309.2C433.3 317.4 439 324.3 446.8 `+
		`327.2C454.6 330.1 463.5 328.8 470.1 323.6C472.8 321.5 476.2 320.2 480 320.2C488.8 320.2 496 `+
		`327.4 496 336.2L496 456.2C496 496 463.8 528.2 424 528.2L307.4 528.2C270 528.2 235 509.5 214.2 `+
		`478.3L146.7 376.9C141.8 369.5 143.8 359.6 151.1 354.7C158.4 349.8 168.4 351.8 173.3 359.1L212 `+
		`417.2C217.9 426 228.8 429.9 238.9 426.9C249 423.9 255.9 414.5 255.9 403.9L256 128zM272 `+
		`64C236.7 64 208 92.7 208 128L208 325.7C187.2 302 151.5 296.8 124.5 314.7C95.1 334.4 87.1 374.1 `+
		`106.8 403.5L174.3 504.8C204 549.3 253.9 576 307.4 576L424 576C490.3 576 544 522.3 544 `+
		`456L544 336C544 300.7 515.3 272 480 272C475.5 272 471.2 272.5 467 273.3C455.3 257.9 436.8 248 `+
		`416 248C409.1 248 402.5 249.1 396.3 251.1C384.7 234.7 365.6 224 344 224C341.3 224 338.6 224.2 `+
		`336 224.5L336 128C336 92.7 307.3 64 272 64zM320 368C320 359.2 312.8 352 304 352C295.2 352 288 `+
		`359.2 288 368L288 464C288 472.8 295.2 480 304 480C312.8 480 320 472.8 320 464L320 368zM368 `+
		`352C359.2 352 352 359.2 352 368L352 464C352 472.8 359.2 480 368 480C376.8 480 384 472.8 384 `+
		`464L384 368C384 359.2 376.8 352 368 352zM448 368C448 359.2 440.8 352 432 352C423.2 352 416 `+
		`359.2 416 368L416 464C416 472.8 423.2 480 432 480C440.8 480 448 472.8 448 464L448 368z"/></svg>`)

	doc.Get("body").Call("appendChild", div)

	// Animate: 2 flashes (fade in 300ms → hold 200ms → fade out 300ms) × 2
	// Then remove the element.
	//
	// Português: Anima: 2 piscadas (fade in 300ms → hold 200ms → fade out 300ms) × 2
	// Depois remove o elemento.
	go func() {
		style := div.Get("style")
		flash := func() {
			style.Set("transition", "opacity 300ms ease-in")
			style.Set("opacity", "0.7")
			time.Sleep(500 * time.Millisecond)
			style.Set("transition", "opacity 300ms ease-out")
			style.Set("opacity", "0")
			time.Sleep(400 * time.Millisecond)
		}
		flash()
		flash()

		// Remove from DOM
		parent := div.Get("parentNode")
		if !parent.IsNull() && !parent.IsUndefined() {
			parent.Call("removeChild", div)
		}
	}()
}

// =====================================================================
//  Create methods
// =====================================================================

func (f *DeviceFactory) CreateLoop() {
	stm := new(compLoop.StatementLoop)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetSceneMgr(f.SceneMgr)
	// Delivery B: linear context menu replaces hex — no tutorial on this device.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementLoop.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementLoop at (%v, %v)", cx, cy)
	// Refresh the scenegraph — the device was registered at (0,0)
	// before SetPosition moved its sprite to the click location; without
	// this refresh the graph would keep the stale geometry and raise
	// phantom conflicts against whatever is at (0,0).
	f.SceneMgr.NotifyChange()
}

// CreateLoopDuration creates a timed infinite loop with a time.Duration interval.
// Unlike CreateLoop (which has a bool stop port), this loop runs forever with
// time.Sleep(interval) at the end of each iteration.
//
// Português: Cria um loop infinito temporizado com intervalo time.Duration.
func (f *DeviceFactory) CreateLoopDuration() {
	stm := new(compLoop.StatementLoopDuration)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetSceneMgr(f.SceneMgr)
	// Delivery B: linear context menu replaces hex — no tutorial on this device.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementLoopDuration.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementLoopDuration at (%v, %v)", cx, cy)
	// Refresh the scenegraph — the device was registered at (0,0)
	// before SetPosition moved its sprite to the click location; without
	// this refresh the graph would keep the stale geometry and raise
	// phantom conflicts against whatever is at (0,0).
	f.SceneMgr.NotifyChange()
}

// CreateCase creates an N-way case container. The maker selects the active
// case via the pill and places devices inside each one. The codegen emits a
// switch — or, for a bool selector, an if/else — based on which devices are in
// each case.
//
// Português: Cria um container case de N vias. O maker escolhe o case ativo
// pelo pill e coloca devices em cada um.
func (f *DeviceFactory) CreateCase() {
	stm := new(compFlow.StatementCase)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetSceneMgr(f.SceneMgr)
	stm.SetContextMenu(f.ContextMenu)
	stm.SetCanvasEl(f.CanvasEl)
	stm.SetCodegenPreview(f.PreviewCaseFn)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementCase.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementCase at (%v, %v)", cx, cy)
	f.SceneMgr.NotifyChange()
}

func (f *DeviceFactory) CreateAdd() {
	stm := new(compMath.StatementAdd)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery A pilot: body menu goes through the new linear context
	// menu; port menus (inputX, inputY, output) stay on hex for one
	// delivery because the output click opens a tutorial that will be
	// re-homed in Delivery C.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementAdd.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementAdd at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateEqualTo() {
	stm := new(compLogic.StatementEqualTo)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body/ports;
	// hex stays for the first-time output tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementEqualTo.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementEqualTo at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateNotEqualTo() {
	stm := new(compLogic.StatementNotEqualTo)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body/ports;
	// hex stays for the first-time output tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementNotEqualTo.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementNotEqualTo at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateGreaterThan() {
	stm := new(compLogic.StatementGreaterThan)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body/ports;
	// hex stays for the first-time output tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementGreaterThan.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementGreaterThan at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateLessThanOrEqualTo() {
	stm := new(compLogic.StatementLessThanOrEqualTo)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body/ports;
	// hex stays for the first-time output tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementLessThanOrEqualTo.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementLessThanOrEqualTo at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateGreaterThanOrEqualTo() {
	stm := new(compLogic.StatementGreaterThanOrEqualTo)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body/ports;
	// hex stays for the first-time output tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementGreaterThanOrEqualTo.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementGreaterThanOrEqualTo at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateLessThan() {
	stm := new(compLogic.StatementLessThan)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body/ports;
	// hex stays for the first-time output tutorial.
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementLessThan.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementLessThan at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateSub() {
	stm := new(compMath.StatementSub)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body and port menus.
	// Hex stays for the first-time output tutorial (Delivery C).
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementSub.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementSub at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateMul() {
	stm := new(compMath.StatementMul)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body and port menus.
	// Hex stays for the first-time output tutorial (Delivery C).
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementMul.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementMul at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateDiv() {
	stm := new(compMath.StatementDiv)
	stm.SetStage(f.Stage)
	stm.SetHexMenu(f.HexMenu)
	// Delivery B: linear context menu handles body and port menus.
	// Hex stays for the first-time output tutorial (Delivery C).
	stm.SetContextMenu(f.ContextMenu)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementDiv.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementDiv at (%v, %v)", cx, cy)
}

func (f *DeviceFactory) CreateConstInt() {
	stm := new(compConsts.StatementConstInt)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementConstInt.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementConstInt at (%v, %v)", cx, cy)
}

// CreateGetVarInt creates a get-variable device (int) on the stage.
// Português: Cria um device "ler variável" (int) na stage.
func (f *DeviceFactory) CreateGetVarInt() {
	stm := new(compVars.StatementGetVarInt)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementGetVarInt.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementGetVarInt at (%v, %v)", cx, cy)
}

// CreateGetVarFloat places a GetVarFloat device — reads a float variable and
// emits its value on a single output connector (peach/float-typed). Mirrors
// CreateGetVarInt; the only difference is the concrete device type.
//
// Português: Coloca um device GetVarFloat — lê uma variável float e emite seu
// valor num único conector de saída (pêssego/tipo float). Espelha
// CreateGetVarInt; só muda o tipo concreto do device.
func (f *DeviceFactory) CreateGetVarFloat() {
	stm := new(compVars.StatementGetVarFloat)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementGetVarFloat.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementGetVarFloat at (%v, %v)", cx, cy)
}

// CreateSetVarInt places a SetVarInt device — a sink that assigns the value
// wired into its input to an int variable (no output). Mirrors the GetVar
// factories; only the concrete device type differs.
//
// Português: Coloca um device SetVarInt — um sink que atribui o valor ligado na
// entrada a uma variável int (sem saída). Espelha as fábricas de GetVar; só
// muda o tipo concreto do device.
func (f *DeviceFactory) CreateSetVarInt() {
	stm := new(compVars.StatementSetVarInt)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementSetVarInt.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementSetVarInt at (%v, %v)", cx, cy)
}

// CreateSetVarFloat places a SetVarFloat device — the float counterpart of
// CreateSetVarInt (sink, peach/float-typed input, no output).
//
// Português: Coloca um device SetVarFloat — contraparte float do CreateSetVarInt
// (sink, entrada pêssego/tipo float, sem saída).
func (f *DeviceFactory) CreateSetVarFloat() {
	stm := new(compVars.StatementSetVarFloat)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementSetVarFloat.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementSetVarFloat at (%v, %v)", cx, cy)
}

// CreateGetVarString places a GetVarString device — reads a string variable and
// emits its value on the output (green/string-typed). Mirrors CreateGetVarInt.
//
// Português: Coloca um device GetVarString — lê uma variável string e emite o
// valor na saída (verde/tipo string). Espelha CreateGetVarInt.
func (f *DeviceFactory) CreateGetVarString() {
	stm := new(compVars.StatementGetVarString)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementGetVarString.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementGetVarString at (%v, %v)", cx, cy)
}

// CreateSetVarString places a SetVarString device — a sink that assigns the
// value wired into its input to a string variable (no output).
//
// Português: Coloca um device SetVarString — um sink que atribui o valor ligado
// na entrada a uma variável string (sem saída).
func (f *DeviceFactory) CreateSetVarString() {
	stm := new(compVars.StatementSetVarString)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementSetVarString.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementSetVarString at (%v, %v)", cx, cy)
}

// CreatePrintInt places the debug-print sink device (int) on the stage.
// Same wiring as every rectangular sink (SetVar family); the print itself
// happens at code generation via ir.OpPrint — fmt.Printf in Go, printf in
// C99.
//
// Português: Coloca o sink de print (int) na stage. Mesma fiação dos
// sinks retangulares (família SetVar); o print acontece na geração de
// código via ir.OpPrint — fmt.Printf no Go, printf no C99.
func (f *DeviceFactory) CreatePrintInt() {
	stm := new(compDebug.StatementPrintInt)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementPrintInt.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementPrintInt at (%v, %v)", cx, cy)
}

// CreatePrintFloat places the debug-print sink device (float) on the stage.
// Same wiring as every rectangular sink (SetVar family); the print itself
// happens at code generation via ir.OpPrint — fmt.Printf in Go, printf in
// C99.
//
// Português: Coloca o sink de print (float) na stage. Mesma fiação dos
// sinks retangulares (família SetVar); o print acontece na geração de
// código via ir.OpPrint — fmt.Printf no Go, printf no C99.
func (f *DeviceFactory) CreatePrintFloat() {
	stm := new(compDebug.StatementPrintFloat)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementPrintFloat.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementPrintFloat at (%v, %v)", cx, cy)
}

// CreatePrintString places the debug-print sink device (string) on the stage.
// Same wiring as every rectangular sink (SetVar family); the print itself
// happens at code generation via ir.OpPrint — fmt.Printf in Go, printf in
// C99.
//
// Português: Coloca o sink de print (string) na stage. Mesma fiação dos
// sinks retangulares (família SetVar); o print acontece na geração de
// código via ir.OpPrint — fmt.Printf no Go, printf no C99.
func (f *DeviceFactory) CreatePrintString() {
	stm := new(compDebug.StatementPrintString)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementPrintString.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementPrintString at (%v, %v)", cx, cy)
}

// CreatePrintBool places the debug-print sink device (bool) on the stage.
// Same wiring as every rectangular sink (SetVar family); the print itself
// happens at code generation via ir.OpPrint — fmt.Printf in Go, printf in
// C99.
//
// Português: Coloca o sink de print (bool) na stage. Mesma fiação dos
// sinks retangulares (família SetVar); o print acontece na geração de
// código via ir.OpPrint — fmt.Printf no Go, printf no C99.
func (f *DeviceFactory) CreatePrintBool() {
	stm := new(compDebug.StatementPrintBool)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementPrintBool.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementPrintBool at (%v, %v)", cx, cy)
}

// CreatePrintByte places the debug-print sink device (byte) on the stage.
// Same wiring as every rectangular sink (SetVar family); the print itself
// happens at code generation via ir.OpPrint — fmt.Printf in Go, printf in
// C99.
//
// Português: Coloca o sink de print (byte) na stage. Mesma fiação dos
// sinks retangulares (família SetVar); o print acontece na geração de
// código via ir.OpPrint — fmt.Printf no Go, printf no C99.
func (f *DeviceFactory) CreatePrintByte() {
	stm := new(compDebug.StatementPrintByte)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementPrintByte.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementPrintByte at (%v, %v)", cx, cy)
}

// CreatePrintByteArray places the debug-print sink device ([]byte) on the stage.
// Same wiring as every rectangular sink (SetVar family); the print itself
// happens at code generation via ir.OpPrint — fmt.Printf in Go, printf in
// C99.
//
// Português: Coloca o sink de print ([]byte) na stage. Mesma fiação dos
// sinks retangulares (família SetVar); o print acontece na geração de
// código via ir.OpPrint — fmt.Printf no Go, printf no C99.
func (f *DeviceFactory) CreatePrintByteArray() {
	stm := new(compDebug.StatementPrintByteArray)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementPrintByteArray.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementPrintByteArray at (%v, %v)", cx, cy)
}

// CreateGauge creates a dual device: backend data node + frontend gauge dial.
// Uses dualStages() to correctly resolve which stage is backend vs frontend,
// regardless of which workspace tab the factory belongs to.
func (f *DeviceFactory) CreateGauge() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, _ := f.dualContextMenus()

	stm := new(compFrontend.StatementGauge)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementGauge.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	// Wire the live communication callback so the gauge can send values
	// back to external hardware when the user interacts with the frontend.
	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementGauge at (%v, %v)", cx, cy)
}

// CreateLED creates a dual device: backend data node + frontend LED indicator.
func (f *DeviceFactory) CreateLED() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, _ := f.dualContextMenus()

	stm := new(compFrontend.StatementLED)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementLED.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementLED at (%v, %v)", cx, cy)
}

// CreateBarGraph creates a dual device: backend data node + frontend vertical bar.
func (f *DeviceFactory) CreateBarGraph() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, _ := f.dualContextMenus()

	stm := new(compFrontend.StatementBarGraph)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementBarGraph.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementBarGraph at (%v, %v)", cx, cy)
}

// CreateTextDisplay creates a dual device: backend data node + frontend text label.
func (f *DeviceFactory) CreateTextDisplay() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, frontendCtx := f.dualContextMenus()

	stm := new(compFrontend.StatementTextDisplay)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	if frontendCtx != nil {
		stm.SetFrontendContextMenu(frontendCtx)
	}
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementTextDisplay.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementTextDisplay at (%v, %v)", cx, cy)
}

// CreateButton creates a dual device: backend data node + frontend clickable button.
func (f *DeviceFactory) CreateButton() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, _ := f.dualContextMenus()

	stm := new(compFrontend.StatementButton)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementButton.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementButton at (%v, %v)", cx, cy)
}

// CreateSevenSeg creates a dual device: backend data node + frontend 7-segment display.
func (f *DeviceFactory) CreateSevenSeg() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, _ := f.dualContextMenus()

	stm := new(compFrontend.StatementSevenSeg)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementSevenSeg.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementSevenSeg at (%v, %v)", cx, cy)
}

// CreateKnob creates a dual device: backend data node + frontend rotary knob.
func (f *DeviceFactory) CreateKnob() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, _ := f.dualContextMenus()

	stm := new(compFrontend.StatementKnob)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementKnob.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementKnob at (%v, %v)", cx, cy)
}

// CreateChart creates a dual device: backend data node + frontend line chart.
func (f *DeviceFactory) CreateChart() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, frontendCtx := f.dualContextMenus()

	stm := new(compFrontend.StatementChart)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	if frontendCtx != nil {
		stm.SetFrontendContextMenu(frontendCtx)
	}
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementChart.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementChart at (%v, %v)", cx, cy)
}

// CreateChartPro creates a dual device: backend data node with N input
// connectors + frontend multi-series real-time line chart.
// Each series has its own ring buffer, color, and label.
func (f *DeviceFactory) CreateChartPro() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, frontendCtx := f.dualContextMenus()

	stm := new(compFrontend.StatementChartPro)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// Delivery A pilot: backend body menu + frontend resize menu both
	// use the new context menu package. Port menus on the backend side
	// still route through hex (migrated in Delivery B).
	stm.SetBackendContextMenu(backendCtx)
	if frontendCtx != nil {
		stm.SetFrontendContextMenu(frontendCtx)
	}
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementChartPro.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	// ChartPro is a read-only display — it does not send. The
	// LiveSendFunc plumbing was removed (decision §2.4 of
	// CHARTPRO_REFACTOR.md).
	//
	// Instead, the reconnect registrar gives the chart a way to mark
	// FAIL events on its timeline when the WebSocket recovers after a
	// drop, so the operator can distinguish hardware failure (RESET)
	// from infrastructure failure (FAIL).
	if f.LiveReconnectRegistrar != nil {
		stm.SetReconnectRegistrar(f.LiveReconnectRegistrar)
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementChartPro at (%v, %v)", cx, cy)
}

// CreatePieChart creates a dual device: backend data node with N input
// connectors + frontend pie/donut chart. Each input becomes one slice.
func (f *DeviceFactory) CreatePieChart() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, frontendCtx := f.dualContextMenus()

	stm := new(compFrontend.StatementPieChart)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	if frontendCtx != nil {
		stm.SetFrontendContextMenu(frontendCtx)
	}
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementPieChart.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementPieChart at (%v, %v)", cx, cy)
}

// CreateBackgroundImage creates a dual device: backend config node +
// frontend background image layer. Uses dualStages() to correctly resolve
// which stage is backend vs frontend.
func (f *DeviceFactory) CreateBackgroundImage() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, frontendCtx := f.dualContextMenus()

	stm := new(compFrontend.StatementBackgroundImage)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	if frontendCtx != nil {
		stm.SetFrontendContextMenu(frontendCtx)
	}
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementBackgroundImage.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementBackgroundImage at (%v, %v)", cx, cy)
}

//func (f *DeviceFactory) CreateCompare() {
//	log.Println("[Factory] Starting CreateCompare...")
//	stm := new(devices.StatementCompare)
//	stm.SetStage(f.Stage)
//	stm.SetWireManager(f.WireMgr)
//	stm.SetResizerButton(f.ResizeButton)
//	stm.SetDraggerButton(f.DraggerButton)
//	stm.SetGridAdjust(f.GridAdjust)
//	stm.SetHexMenu(f.HexMenu)
//
//	if err := stm.Init(); err != nil {
//		log.Printf("[Factory] StatementCompare.Init: %v", err)
//		return
//	}
//
//	stm.RegisterConnectors()
////	f.SceneMgr.Register(stm)
//	stm.SetSceneNotify(f.SceneNotifyFn)
//	stm.SetOnRemove(f.makeOnRemove())
//
//	cx, cy := f.devicePosition()
//	stm.SetPosition(cx, cy)
//	stm.SetDragEnable(true)
//	stm.Append()
//	log.Printf("[Factory] Created StatementCompare at (%v, %v)", cx, cy)
//	log.Println("[Factory] Completed CreateCompare")
//}

func (f *DeviceFactory) CreateBool() {
	log.Println("[Factory] Starting CreateBool...")
	stm := new(compConsts.StatementBool)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetDraggerButton(f.DraggerButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementBool.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementBool at (%v, %v)", cx, cy)
	log.Println("[Factory] Completed CreateBool")
}

// CreateConstFloat places a floating-point constant device on the canvas.
// The default precision is float64; the user can change it in the Inspect panel.
func (f *DeviceFactory) CreateConstFloat() {
	stm := new(compConsts.StatementConstFloat)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementConstFloat.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementConstFloat at (%v, %v)", cx, cy)
}

// CreateConstString places a string constant device on the canvas.
func (f *DeviceFactory) CreateConstString() {
	stm := new(compConsts.StatementConstString)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementConstString.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementConstString at (%v, %v)", cx, cy)
}

// CreateConstDuration creates a time.Duration constant device.
// Outputs a fixed duration value through a single output connector.
// The user selects the unit (Nanosecond..Hour) and amount in the Inspect panel.
//
// Português: Cria um dispositivo constante time.Duration.
func (f *DeviceFactory) CreateConstDuration() {
	stm := new(compConsts.StatementConstDuration)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementConstDuration.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementConstDuration at (%v, %v)", cx, cy)
}

// CreateConstArrayInt places a constant fixed-size INT collection device on the
// stage (e.g. []int{1, 2, 3} — Go slice literal / C fixed array + `_len`
// companion). One of the three sibling collection devices (Int / Float /
// String), mirroring the scalar const family. Values are typed in the
// Inspect overlay, comma-separated. The output port registers with
// AcceptNotConnected:false, so a dangling collection is flagged by stage
// validation before codegen (plan decision 5 — see
// docs/claude_const_array_plan.md).
//
// Português: Cria o device de coleção constante de inteiros de tamanho fixo. Valores no
// Inspect, separados por vírgula. A porta de saída exige conexão — coleção solta é erro
// de autoria sinalizado antes do codegen.
func (f *DeviceFactory) CreateConstArrayInt() {
	stm := new(compConsts.StatementConstArrayInt)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementConstArrayInt.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementConstArrayInt at (%v, %v)", cx, cy)
}

// CreateIndexInt places the array index reader device (int element) on the
// stage: two inputs (array, index) and two outputs (value, ok). The array,
// index and value ports require connection; ok is OPTIONAL (the graphical
// comma-ok). The safe, bounds-checked read lives entirely in the offline-tested
// codegen (ir.OpIndex) — an out-of-range or negative index yields the type's
// zero, never a panic (Go) or undefined read (C).
//
// Português: Coloca o leitor de índice (int) na stage: 2 entradas (array, index)
// e 2 saídas (value, ok). array/index/value exigem conexão; ok é OPCIONAL (o
// comma-ok gráfico). A leitura segura e checada vive no codegen testado offline
// (ir.OpIndex) — fora do range devolve o zero do tipo, nunca panic/UB.
func (f *DeviceFactory) CreateIndexInt() {
	stm := new(compArray.StatementIndexInt)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementIndexInt.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementIndexInt at (%v, %v)", cx, cy)
}

// CreateIndexFloat places the array index reader device (float element) on the
// stage. Identical wiring to CreateIndexInt; only the element type differs. The
// safe, bounds-checked read lives in ir.OpIndex, shared by all three readers.
//
// Português: Coloca o leitor de índice (float) na stage. Fiação idêntica ao
// CreateIndexInt; só muda o tipo do elemento.
func (f *DeviceFactory) CreateIndexFloat() {
	stm := new(compArray.StatementIndexFloat)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementIndexFloat.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementIndexFloat at (%v, %v)", cx, cy)
}

// CreateIndexString places the array index reader device (string element) on
// the stage. Identical wiring to CreateIndexInt; in C99 the out-of-range result
// is the empty string "" (never NULL), handled in ir.OpIndex.
//
// Português: Coloca o leitor de índice (string) na stage. Fiação idêntica ao
// CreateIndexInt; no C99, fora do range devolve a string vazia "" (nunca NULL).
func (f *DeviceFactory) CreateIndexString() {
	stm := new(compArray.StatementIndexString)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementIndexString.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementIndexString at (%v, %v)", cx, cy)
}

// CreateConstArrayFloat places a constant fixed-size FLOAT collection device on the
// stage (e.g. []float32{0.5, 1.5} — Go slice literal / C fixed array + `_len`
// companion). One of the three sibling collection devices (Int / Float /
// String), mirroring the scalar const family. Values are typed in the
// Inspect overlay, comma-separated, with a float32/float64 precision select that re-registers the port type. The output port registers with
// AcceptNotConnected:false, so a dangling collection is flagged by stage
// validation before codegen (plan decision 5 — see
// docs/claude_const_array_plan.md).
//
// Português: Cria o device de coleção constante de floats de tamanho fixo. Valores no
// Inspect, com select de precisão float32/float64. A porta de saída exige conexão — coleção solta é erro
// de autoria sinalizado antes do codegen.
func (f *DeviceFactory) CreateConstArrayFloat() {
	stm := new(compConsts.StatementConstArrayFloat)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementConstArrayFloat.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementConstArrayFloat at (%v, %v)", cx, cy)
}

// CreateConstArrayString places a constant fixed-size STRING collection device on the
// stage (e.g. []string{"red", "green"} — Go slice literal / C fixed array + `_len`
// companion). One of the three sibling collection devices (Int / Float /
// String), mirroring the scalar const family. Values are typed in the
// Inspect overlay as a TEXTAREA, one element per line (a comma is string content, not a separator). The output port registers with
// AcceptNotConnected:false, so a dangling collection is flagged by stage
// validation before codegen (plan decision 5 — see
// docs/claude_const_array_plan.md).
//
// Português: Cria o device de coleção constante de strings de tamanho fixo. Valores no
// Inspect, um elemento por linha. A porta de saída exige conexão — coleção solta é erro
// de autoria sinalizado antes do codegen.
func (f *DeviceFactory) CreateConstArrayString() {
	stm := new(compConsts.StatementConstArrayString)
	stm.SetStage(f.Stage)
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	// compConsts has no tutorial — linear context menu fully supersedes hex.
	stm.SetContextMenu(f.ContextMenu)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementConstArrayString.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementConstArrayString at (%v, %v)", cx, cy)
}

// CreateByType creates a device by its type string at the given position.
// Used by the stage import system to reconstruct saved scenes from JSON.
// Returns false if the type is not recognized (e.g. BlackBox devices that
// require a definition — those will be supported in a future iteration).
//
// Português: Cria um device pelo tipo string na posição dada.
// Usado pelo import de stage para reconstruir cenas salvas.
func (f *DeviceFactory) CreateByType(deviceType string, x, y float64) bool {
	f.SetNextPosition(x, y)

	switch deviceType {
	case "StatementAdd":
		f.CreateAdd()
	case "StatementSub":
		f.CreateSub()
	case "StatementMul":
		f.CreateMul()
	case "StatementDiv":
		f.CreateDiv()
	case "StatementLoop":
		f.CreateLoop()
	case "StatementLoopDuration":
		f.CreateLoopDuration()
	case "StatementCase":
		f.CreateCase()
	case "StatementConstInt":
		f.CreateConstInt()
	case "StatementBool":
		f.CreateBool()
	case "StatementConstFloat":
		f.CreateConstFloat()
	case "StatementConstString":
		f.CreateConstString()
	case "StatementConstDuration":
		f.CreateConstDuration()
	case "StatementConstArrayInt":
		f.CreateConstArrayInt()
	case "StatementConstArrayFloat":
		f.CreateConstArrayFloat()
	case "StatementConstArrayString":
		f.CreateConstArrayString()
	case "StatementIndexInt":
		f.CreateIndexInt()
	case "StatementIndexFloat":
		f.CreateIndexFloat()
	case "StatementIndexString":
		f.CreateIndexString()
	// Variable devices (get/set × int/float/string). These were previously
	// ABSENT from this switch, so on stage import (importScene → CreateByType)
	// they fell through to the BlackBox default, failed to resolve, and were
	// silently dropped — the "variables are not saved" bug. The scene JSON
	// always carried them (buildDeviceJSON + the "variables" array), so the
	// fault was purely on this reconstruction side; both the normal saved
	// file and the steganographic image restore through here, so this single
	// mapping fixes both. varName/label are restored right after by
	// ApplyProperties, exactly like every other device.
	//
	// Português: Devices de variável (get/set × int/float/string). Faltavam
	// neste switch, então no import caíam no default de BlackBox, não
	// resolviam e eram descartados — o bug "não salva variáveis". O JSON
	// sempre os carregou; o defeito era só aqui na reconstrução. Os dois
	// caminhos de load (arquivo e imagem stego) passam por aqui.
	case "StatementGetVarInt":
		f.CreateGetVarInt()
	case "StatementGetVarFloat":
		f.CreateGetVarFloat()
	case "StatementGetVarString":
		f.CreateGetVarString()
	case "StatementSetVarInt":
		f.CreateSetVarInt()
	case "StatementSetVarFloat":
		f.CreateSetVarFloat()
	case "StatementSetVarString":
		f.CreateSetVarString()
	// Debug-print sinks. Registered here from day one so scene import
	// (importScene → CreateByType) reconstructs them — the SetVar family
	// above documents the silent-drop bug this switch previously caused.
	// Português: Sinks de print. Registrados aqui desde o primeiro dia para
	// o import de cena reconstruí-los — a família SetVar acima documenta o
	// bug de descarte silencioso que este switch já causou.
	case "StatementPrintInt":
		f.CreatePrintInt()
	case "StatementPrintFloat":
		f.CreatePrintFloat()
	case "StatementPrintString":
		f.CreatePrintString()
	case "StatementPrintBool":
		f.CreatePrintBool()
	case "StatementPrintByte":
		f.CreatePrintByte()
	case "StatementPrintByteArray":
		f.CreatePrintByteArray()
	case "StatementGauge":
		f.CreateGauge()
	case "StatementLED":
		f.CreateLED()
	case "StatementBarGraph":
		f.CreateBarGraph()
	case "StatementTextDisplay":
		f.CreateTextDisplay()
	case "StatementButton":
		f.CreateButton()
	case "StatementSevenSeg":
		f.CreateSevenSeg()
	case "StatementKnob":
		f.CreateKnob()
	case "StatementChart":
		f.CreateChart()
	case "StatementChartPro":
		f.CreateChartPro()
	case "StatementPieChart":
		f.CreatePieChart()
	case "StatementBackgroundImage":
		f.CreateBackgroundImage()
	case "StatementEqualTo":
		f.CreateEqualTo()
	case "StatementNotEqualTo":
		f.CreateNotEqualTo()
	case "StatementLessThan":
		f.CreateLessThan()
	case "StatementLessThanOrEqualTo":
		f.CreateLessThanOrEqualTo()
	case "StatementGreaterThan":
		f.CreateGreaterThan()
	case "StatementGreaterThanOrEqualTo":
		f.CreateGreaterThanOrEqualTo()
	case "StatementCommStatus":
		f.CreateCommStatus()
	default:
		// Try to parse as a BlackBox device type.
		// Format: "BlackBoxInit:ComponentName" or "BlackBoxMethodName:ComponentName"
		return f.createBlackBoxByType(deviceType)
	}
	return true
}

// CreateCopy duplicates a device from the context-menu "copy" action. It reuses
// the SAME click-to-place flow as a sidebar item: SafeRun arms the placement,
// the user clicks the stage, and the workspace calls ConfirmPlacement, which
// runs the pending function at the click position. The ONLY difference from a
// plain sidebar placement is that the newly-placed device is pre-configured
// with the SOURCE device's data.
//
// On placement it creates a brand-new device of deviceType (fresh id, DEFAULT
// size, no wires) and replays the captured properties onto it via the shared
// scene.ReplayProperties — the same path the scene import uses — so a copy is
// configured exactly like a reloaded device. props is the snapshot taken when
// the menu opened (the source's GetProperties()); an unknown deviceType places
// nothing and logs.
//
// Português: Duplica um device pela ação "copy" do menu de contexto, reusando o
// mesmo fluxo de click-to-place de um item do menu lateral (SafeRun arma; o
// usuário clica; ConfirmPlacement roda a função na posição do clique). No
// clique, cria um device novo do tipo dado (id novo, tamanho padrão, sem fios)
// e replica os dados da origem via scene.ReplayProperties — mesmo caminho do
// import, então a cópia fica idêntica a um device recarregado.
func (f *DeviceFactory) CreateCopy(deviceType string, props map[string]interface{}) {
	f.SafeRun("CopyDevice:"+deviceType, func() {
		// ConfirmPlacement already set the next position to the click point;
		// pass it through CreateByType (which re-applies it via SetNextPosition)
		// so the copy lands under the cursor. The new device then becomes the
		// serializer's LastDevice.
		if !f.CreateByType(deviceType, f.nextPosX, f.nextPosY) {
			log.Printf("[Factory] CreateCopy: unknown device type %q — nothing placed", deviceType)
			return
		}

		newDev := f.SceneMgr.LastDevice()
		if newDev == nil {
			log.Printf("[Factory] CreateCopy: no device created for %q", deviceType)
			return
		}

		// Replay the source's data (value / varName / label / BlackBox props)
		// onto the fresh copy. Wires are intentionally NOT copied — a copy is a
		// standalone device the user wires up themselves.
		if insp, ok := newDev.(scene.Inspectable); ok {
			scene.ReplayProperties(insp, props)
		}
	})
}

// SetBlackBoxDefs stores the loaded BlackBox definitions indexed by component
// name. Called at workspace startup after blackbox.LoadDefs(). This map is used
// by CreateByType to resolve BlackBox device types during stage import.
//
// Português: Armazena as definições de BlackBox indexadas por nome do componente.
// Chamado no startup do workspace. Usado por CreateByType para resolver tipos
// BlackBox durante import de stage.
func (f *DeviceFactory) SetBlackBoxDefs(defs []*blackbox.BlackBoxDefClient) {
	f.bbDefs = make(map[string]*blackbox.BlackBoxDefClient, len(defs))
	for _, d := range defs {
		f.bbDefs[d.Name] = d
	}
	log.Printf("[Factory] BlackBox defs indexed: %d component(s)", len(f.bbDefs))
}

// callbackRefDevicePrefix is the GetDeviceType() prefix of the C99 callback-
// reference (ƒ) device. It MUST match StatementBlackBoxMethod.GetDeviceType
// and the server-side IR emitter (emit.go callbackRefTypePrefix).
const callbackRefDevicePrefix = "CallbackRef:"

// createBlackBoxByType parses a BlackBox device type string and creates the
// corresponding device. Returns false if the type is not a BlackBox format
// or the component definition is not loaded.
//
// Type string formats (from GetDeviceType()):
//   - "BlackBoxInit:ComponentName"         → Init device
//   - "BlackBoxMethodName:ComponentName"   → Method device (e.g. "BlackBoxRun:APDS9960")
//   - "CallbackRef:FunctionName"          → C99 callback-reference (ƒ) device
//     (e.g. "CallbackRef:displayWrite")
//
// The CallbackRef form carries only the function name — no component name. The
// C function namespace is flat, so the name is unique across loaded defs; the
// owning component is resolved by searching for the callback function. The ƒ's
// callback wire is restored by the generic wire-reconnect pass in importScene,
// so no wire-specific handling is needed here.
//
// Português: Parseia o type string de BlackBox/CallbackRef e cria o device.
func (f *DeviceFactory) createBlackBoxByType(deviceType string) bool {
	// C99 callback-reference (ƒ) device — "CallbackRef:<fn>". GetDeviceType
	// emits only the function name (the C namespace is flat → names are
	// globally unique), so the owning component is found by locating the
	// callback function across the loaded defs.
	if strings.HasPrefix(deviceType, callbackRefDevicePrefix) {
		fnName := deviceType[len(callbackRefDevicePrefix):]
		if f.bbDefs == nil {
			log.Printf("[Factory] CreateByType: BlackBox defs not loaded — cannot create %q", deviceType)
			return false
		}
		for _, def := range f.bbDefs {
			if fn := def.GetFunction(fnName); fn != nil && fn.HandlerType != "" {
				f.CreateBlackBoxCallbackRef(def, fnName)
				return true
			}
		}
		log.Printf("[Factory] CreateByType: callback function %q not found in any loaded def — skipping", fnName)
		return false
	}

	// Must start with "BlackBox" and contain ":"
	if !strings.HasPrefix(deviceType, "BlackBox") {
		log.Printf("[Factory] CreateByType: unknown type %q — skipping", deviceType)
		return false
	}

	colonIdx := strings.Index(deviceType, ":")
	if colonIdx < 0 {
		log.Printf("[Factory] CreateByType: malformed BlackBox type %q (no ':') — skipping", deviceType)
		return false
	}

	componentName := deviceType[colonIdx+1:]
	prefix := deviceType[len("BlackBox"):colonIdx] // "Init", a Go method name, or a C99 function name

	if f.bbDefs == nil {
		log.Printf("[Factory] CreateByType: BlackBox defs not loaded — cannot create %q", deviceType)
		return false
	}

	// C99 function device — "BlackBox<fn>:" with an EMPTY struct segment.
	// C99 follows device-per-function (no struct receiver), so def.Name is ""
	// and GetDeviceType emits nothing after the colon. The function lives in
	// def.Functions, reached via GetFunction and built with CreateBlackBoxFunction
	// — NOT def.Methods / GetMethod / CreateBlackBoxMethod, which is the Go-only
	// path and is exactly what made the import fail with "has no method" for a
	// C99 block (it looked up a Go method that does not exist). The empty
	// component segment is the discriminator (see factoryC99.go); as with the
	// callback reference, the C namespace is flat, so the owning def is found by
	// searching for the function name across the loaded defs.
	if componentName == "" {
		fnName := prefix
		for _, def := range f.bbDefs {
			if def.GetFunction(fnName) != nil {
				f.CreateBlackBoxFunction(def, fnName)
				return true
			}
		}
		log.Printf("[Factory] CreateByType: C99 function %q not found in any loaded def — skipping", fnName)
		return false
	}

	// Go black-box: resolve the component by name.
	def, ok := f.bbDefs[componentName]
	if !ok {
		log.Printf("[Factory] CreateByType: BlackBox component %q not found in loaded defs — skipping", componentName)
		return false
	}

	if prefix == "Init" {
		// Init device.
		if !def.HasInit() {
			log.Printf("[Factory] CreateByType: %s has no Init() — skipping", componentName)
			return false
		}
		f.CreateBlackBoxInit(def)
		return true
	}

	// Named method device (Run, Log, Step, etc.).
	f.CreateBlackBoxMethod(def, prefix)
	return true
}

func (f *DeviceFactory) CreateCommStatus() {
	backendStg, frontendStg := f.dualStages()
	backendCtx, _ := f.dualContextMenus()

	stm := new(compFrontend.StatementCommStatus)
	stm.SetBackendStage(backendStg)
	if frontendStg != nil {
		stm.SetFrontendStage(frontendStg)
	}
	stm.SetWireManager(f.WireMgr)
	stm.SetResizerButton(f.ResizeButton)
	stm.SetGridAdjust(f.GridAdjust)
	stm.SetBackendContextMenu(backendCtx)
	stm.SetCanvasEl(f.CanvasEl)

	if err := stm.Init(); err != nil {
		log.Printf("[Factory] StatementCommStatus.Init: %v", err)
		return
	}

	stm.RegisterConnectors()
	f.SceneMgr.Register(stm)
	stm.SetSceneNotify(f.SceneNotifyFn)
	stm.SetOnRemove(f.makeOnRemove())

	if f.LiveSendFunc != nil {
		stm.SendFunc = f.LiveSendFunc
	}

	cx, cy := f.devicePosition()
	stm.SetPosition(cx, cy)
	stm.SetFrontendPosition(cx, cy)
	stm.SetDragEnable(true)
	stm.Append()
	log.Printf("[Factory] Created StatementCommStatus at (%v, %v)", cx, cy)
}
