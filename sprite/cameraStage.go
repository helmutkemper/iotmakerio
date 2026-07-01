// sprite/cameraStage.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

import (
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesSprite"
)

// =====================================================================
//  Camera Integration — Stage Methods | Integração da Câmera — Métodos do Stage
// =====================================================================

// GetCamera
//
// English:
//
//	Returns the Stage's Camera. If no Camera has been set, returns nil.
//	Use SetCamera(NewCamera()) to enable camera features.
//
// Português:
//
//	Retorna a Camera do Stage. Se nenhuma Camera foi definida, retorna nil.
//	Use SetCamera(NewCamera()) para habilitar features da câmera.
func (e *stage) GetCamera() *Camera {
	return e.camera
}

// SetCamera
//
// English:
//
//	Sets the Stage's Camera. Pass nil to disable camera features (elements
//	render at their absolute world coordinates, same as without camera).
//	If the Stage is already running, camera listeners are attached immediately.
//
// Português:
//
//	Define a Camera do Stage. Passe nil para desabilitar features da câmera.
//	Se o Stage já estiver rodando, os listeners da câmera são anexados imediatamente.
func (e *stage) SetCamera(camera *Camera) {
	// Detach old camera listeners if running.
	// Português: Remove listeners da câmera anterior se estiver rodando.
	if e.running && e.camera != nil {
		e.detachCameraListeners()
	}

	e.camera = camera

	// Attach new camera listeners if running.
	// Português: Anexa listeners da nova câmera se estiver rodando.
	if e.running && e.camera != nil {
		e.attachCameraListeners()
	}

	e.dirty = true
}

// =====================================================================
//  Render Pipeline — Camera Transform | Pipeline de Render
// =====================================================================

// renderWithCamera
//
// English:
//
//	Draws grid, applies camera transform, draws all elements in world space,
//	restores context, then draws screen-space overlays (origin, info).
//	Called from renderInternal when camera is active.
//
// Português:
//
//	Desenha grid, aplica transformação da câmera, desenha todos os elementos em
//	espaço mundo, restaura contexto, depois desenha overlays em espaço de tela.
//	Chamado de renderInternal quando a câmera está ativa.
func (e *stage) renderWithCamera() {
	cam := e.camera

	// Tick animation before drawing.
	// Português: Tick da animação antes de desenhar.
	if cam.Tick() {
		e.dirty = true
	}

	// Grid — screen space.
	// Português: Grid — espaço de tela.
	cam.DrawGrid(e.ctx, e.canvasWidth, e.canvasHeight)

	// Apply camera transform: world → screen.
	// screenX = (worldX - offsetX) * zoom  ⇒  setTransform(zoom,0,0,zoom,-offX*zoom,-offY*zoom)
	//
	// Português: Aplica transformação da câmera: mundo → tela.
	e.ctx.Call("save")
	e.ctx.Call("setTransform",
		cam.Zoom, 0,
		0, cam.Zoom,
		-cam.OffsetX*cam.Zoom,
		-cam.OffsetY*cam.Zoom,
	)

	// Draw WORLD-SPACE elements (unchanged — they don't know about camera).
	// Português: Desenha elementos em ESPAÇO MUNDO (inalterado).
	e.ensureSorted()
	for _, elem := range e.drawOrder {
		if elem.visible && elem.isCachedVal && !elem.screenSpace {
			elem.Draw(e.ctx)
		}
	}

	// Restore — back to screen coordinates.
	// Português: Restaura — volta para coordenadas de tela.
	e.ctx.Call("restore")

	// Render callback — runs after world elements but BEFORE screen-space
	// elements (hex menus, backdrops). This is where wires are drawn so
	// they appear above devices but below menus.
	//
	// Português: Callback de renderização — executa após elementos mundo
	// mas ANTES de elementos screen-space (menus hex, backdrops). Aqui é
	// onde os wires são desenhados: acima dos devices, abaixo dos menus.
	if e.renderCallback != nil {
		e.renderCallback()
	}

	// Draw SCREEN-SPACE elements (fixed on screen, ignore camera).
	// These are drawn after restore so they appear on top of world elements
	// and at their exact screen pixel coordinates.
	//
	// Português: Desenha elementos em ESPAÇO DE TELA (fixos na tela, ignoram câmera).
	// São desenhados após restore para aparecerem acima dos elementos mundo
	// e nas coordenadas exatas de pixels de tela.
	for _, elem := range e.drawOrder {
		if elem.visible && elem.isCachedVal && elem.screenSpace {
			elem.Draw(e.ctx)
		}
	}

	// Screen-space overlays (camera info, minimap, help).
	// Português: Overlays em espaço de tela (info da câmera, minimapa, ajuda).
	cam.DrawOrigin(e.ctx, e.canvasWidth, e.canvasHeight)
	cam.DrawInfo(e.ctx, e.canvasWidth, e.canvasHeight)
	cam.DrawMinimap(e.ctx, e.canvasWidth, e.canvasHeight, e.elements)
	cam.DrawHelp(e.ctx, e.canvasWidth, e.canvasHeight)
}

// =====================================================================
//  Wheel Event — Zoom | Evento de Scroll — Zoom
// =====================================================================

// handleWheel
//
// English:
//
//	Handles the wheel event for camera zoom. Zoom is centered on the cursor.
//
// Português:
//
//	Trata o evento wheel para zoom da câmera. Zoom é centrado no cursor.
func (e *stage) handleWheel(this js.Value, args []js.Value) interface{} {
	domEvent := args[0]
	domEvent.Call("preventDefault")

	cam := e.camera
	if cam == nil || !cam.ZoomEnabled {
		return nil
	}

	cx, cy := canvasCoords(e.canvas, e.canvasWidth, e.canvasHeight, domEvent)
	deltaY := domEvent.Get("deltaY").Float()

	var delta float64
	if deltaY < 0 {
		delta = cam.ZoomStep // scroll up = zoom in
	} else {
		delta = -cam.ZoomStep // scroll down = zoom out
	}

	cam.ZoomAt(cx, cy, delta)
	e.dirty = true
	return nil
}

// =====================================================================
//  Keyboard — Home / F | Teclado — Home / F
// =====================================================================

// handleKeyDown
//
// English:
//
//	Handles keyboard events for camera control using configurable key bindings.
//	Checks each CameraAction against the pressed key via Camera.matchKeyEvent.
//
// Português:
//
//	Trata eventos de teclado para controle da câmera usando bindings configuráveis.
//	Verifica cada CameraAction contra a tecla pressionada via Camera.matchKeyEvent.
func (e *stage) handleKeyDown(this js.Value, args []js.Value) interface{} {
	domEvent := args[0]

	cam := e.camera
	if cam == nil {
		return nil
	}

	// Keyboard listeners are attached to the document (for
	// focus-independence), so every Stage in the page receives
	// every key event. When the IDE runs two stages side-by-side
	// (backend + frontend, toggled via tabs), each stage gets the
	// global keydown, and without this guard BOTH cameras would
	// pan/zoom on every Home/arrow/+/- press — making the hidden
	// tab drift out of sync with what the user sees.
	//
	// Check: if our own <canvas> has display:none, the user is
	// looking at a different stage right now and our camera must
	// stay put. getComputedStyle is cheap for this one property.
	//
	// Português: Listeners de teclado vivem no document para ser
	// independentes de foco — consequentemente TODO stage recebe
	// TODA tecla. Sem este guard, em uma IDE com backend + frontend,
	// as DUAS câmeras reagiriam à mesma tecla, e a aba oculta
	// sairia do lugar em relação ao que o usuário está vendo.
	// Se o canvas deste stage tem display:none, o usuário está em
	// outra aba e nossa câmera tem que ficar parada.
	if e.canvas.Truthy() {
		style := js.Global().Call("getComputedStyle", e.canvas)
		if style.Truthy() && style.Get("display").String() == "none" {
			return nil
		}
	}

	// Ensure key config is initialized.
	// Português: Garante que a config de teclas está inicializada.
	cam.ensureKeys()

	if !cam.keys.enabled {
		return nil
	}

	// Skip when input element is focused (unless configured otherwise).
	// Português: Pula quando elemento de input está focado (a menos que configurado diferente).
	if cam.shouldSkipKeyEvent() {
		return nil
	}

	// Test each action.
	// Português: Testa cada ação.
	switch {
	case cam.matchKeyEvent(CameraActionGoToOrigin, domEvent):
		domEvent.Call("preventDefault")
		cam.GoToOrigin(400)
		e.dirty = true

	case cam.matchKeyEvent(CameraActionFitAll, domEvent):
		domEvent.Call("preventDefault")
		bx, by, bw, bh := e.GetAllElementsBounds()
		if bw > 0 && bh > 0 {
			cam.FitAll(bx, by, bw, bh, e.canvasWidth, e.canvasHeight, 40, 400)
			e.dirty = true
		}

	case cam.matchKeyEvent(CameraActionZoomIn, domEvent):
		domEvent.Call("preventDefault")
		centerX := float64(e.canvasWidth) / 2
		centerY := float64(e.canvasHeight) / 2
		cam.ZoomAt(centerX, centerY, cam.ZoomStep)
		e.dirty = true

	case cam.matchKeyEvent(CameraActionZoomOut, domEvent):
		domEvent.Call("preventDefault")
		centerX := float64(e.canvasWidth) / 2
		centerY := float64(e.canvasHeight) / 2
		cam.ZoomAt(centerX, centerY, -cam.ZoomStep)
		e.dirty = true

	case cam.matchKeyEvent(CameraActionPanLeft, domEvent):
		domEvent.Call("preventDefault")
		cam.PanScreen(cam.keys.panStep, 0)
		e.dirty = true

	case cam.matchKeyEvent(CameraActionPanRight, domEvent):
		domEvent.Call("preventDefault")
		cam.PanScreen(-cam.keys.panStep, 0)
		e.dirty = true

	case cam.matchKeyEvent(CameraActionPanUp, domEvent):
		domEvent.Call("preventDefault")
		cam.PanScreen(0, cam.keys.panStep)
		e.dirty = true

	case cam.matchKeyEvent(CameraActionPanDown, domEvent):
		domEvent.Call("preventDefault")
		cam.PanScreen(0, -cam.keys.panStep)
		e.dirty = true

	case cam.matchKeyEvent(CameraActionToggleHelp, domEvent):
		domEvent.Call("preventDefault")
		if cam.help != nil {
			cam.help.enabled = !cam.help.enabled
		} else {
			cam.SetHelpEnabled(true)
		}
		e.dirty = true
	}

	return nil
}

// =====================================================================
//  Camera Event Listeners | Event Listeners da Câmera
// =====================================================================

// attachCameraListeners attaches wheel and keydown listeners.
// Português: Anexa listeners de wheel e keydown.
func (e *stage) attachCameraListeners() {
	// Wheel — zoom (passive: false to allow preventDefault).
	// Português: Wheel — zoom (passive: false para permitir preventDefault).
	e.listeners["wheel"] = js.FuncOf(e.handleWheel)
	opts := js.Global().Get("Object").New()
	opts.Set("passive", false)
	e.canvas.Call("addEventListener", "wheel", e.listeners["wheel"], opts)

	// Keydown — Home, F (attached to document for focus-independence).
	// Português: Keydown — Home, F (anexado ao document para independência de foco).
	e.listeners["keydown"] = js.FuncOf(e.handleKeyDown)
	js.Global().Get("document").Call("addEventListener", "keydown", e.listeners["keydown"])
}

// detachCameraListeners removes camera-specific listeners.
// Português: Remove listeners específicos da câmera.
func (e *stage) detachCameraListeners() {
	if fn, ok := e.listeners["wheel"]; ok {
		e.canvas.Call("removeEventListener", "wheel", fn)
		fn.Release()
		delete(e.listeners, "wheel")
	}
	if fn, ok := e.listeners["keydown"]; ok {
		js.Global().Get("document").Call("removeEventListener", "keydown", fn)
		fn.Release()
		delete(e.listeners, "keydown")
	}
}

// =====================================================================
//  Coordinate Helpers | Helpers de Coordenadas
// =====================================================================

// canvasCoordsToWorld converts screen coordinates to world coordinates,
// or returns them unchanged if no camera is active.
//
// Português: Converte coordenadas de tela para mundo, ou retorna inalteradas
// se não há câmera ativa.
func (e *stage) canvasCoordsToWorld(screenX, screenY float64) (worldX, worldY float64) {
	if e.camera == nil {
		return screenX, screenY
	}
	return e.camera.ScreenToWorld(screenX, screenY)
}

// screenDeltaToWorldDelta converts a screen-space delta (pixels) to a world-space
// delta by dividing by the camera zoom. Without camera, returns unchanged.
//
// Português: Converte um delta no espaço de tela (pixels) para delta no espaço mundo
// dividindo pelo zoom da câmera. Sem câmera, retorna inalterado.
func (e *stage) screenDeltaToWorldDelta(dxScreen, dyScreen float64) (dxWorld, dyWorld float64) {
	if e.camera != nil {
		dxWorld = dxScreen / e.camera.Zoom
		dyWorld = dyScreen / e.camera.Zoom
	} else {
		dxWorld = dxScreen
		dyWorld = dyScreen
	}
	return
}

// =====================================================================
//  Bounds | Limites
// =====================================================================

// GetAllElementsBounds returns the axis-aligned bounding box of all visible elements.
// Returns (0,0,0,0) if no visible elements exist.
//
// Português: Retorna o bounding box de todos os elementos visíveis.
// Retorna (0,0,0,0) se não há elementos visíveis.
func (e *stage) GetAllElementsBounds() (x, y, w, h float64) {
	first := true
	var minX, minY, maxX, maxY float64

	for _, elem := range e.elements {
		if !elem.visible || elem.screenSpace {
			continue
		}
		ex := elem.x
		ey := elem.y
		er := ex + elem.width
		eb := ey + elem.height

		if first {
			minX, minY, maxX, maxY = ex, ey, er, eb
			first = false
		} else {
			if ex < minX {
				minX = ex
			}
			if ey < minY {
				minY = ey
			}
			if er > maxX {
				maxX = er
			}
			if eb > maxY {
				maxY = eb
			}
		}
	}

	if first {
		return 0, 0, 0, 0
	}
	return minX, minY, maxX - minX, maxY - minY
}

// =====================================================================
//  Middle-Mouse Pan Detection | Detecção de Pan via Botão do Meio
// =====================================================================

// shouldStartCameraPan returns true if the event should start a camera pan
// (camera exists, pan enabled, middle mouse button=1).
//
// Português: Retorna true se o evento deve iniciar um pan da câmera.
func (e *stage) shouldStartCameraPan(domEvent js.Value) bool {
	cam := e.camera
	if cam == nil || !cam.PanEnabled {
		return false
	}
	return domEvent.Get("button").Int() == 1
}

// shouldStartEmptyAreaPan returns true when a left-click lands on empty
// canvas (no element under the pointer) from a desktop mouse — in which
// case we treat drag as a camera pan. Touch is excluded because
// single-touch on empty area is reserved for future gestures (e.g.
// rectangular selection). Middle-mouse is already handled by
// shouldStartCameraPan; this is the left-button equivalent for the
// laptop-trackpad user who has no middle button.
//
// Called by handlePointerDown AFTER findElementAt returns nil — do
// NOT invoke speculatively, or a click on an element will also start
// a pan.
//
// Português: Retorna true se um left-click em área vazia da stage
// (sem elemento sob o ponteiro) deve iniciar um pan da câmera. Apenas
// mouse desktop — touch está reservado para gestos futuros. Este é
// o equivalente ao middle-mouse para usuários de trackpad que não
// têm botão do meio.
func (e *stage) shouldStartEmptyAreaPan(domEvent js.Value, elem *elementData) bool {
	cam := e.camera
	if cam == nil || !cam.PanEnabled {
		return false
	}
	// Respect the per-user preference. CameraPanEmptyArea is loaded
	// from the server at workspace startup and can be toggled by
	// the user in Editor Settings → Stage. When false, users get
	// the classic behaviour (only middle-mouse pans).
	//
	// Português: Respeita a preferência do usuário. Quando false,
	// apenas o botão do meio faz pan.
	if !rulesSprite.CameraPanEmptyArea {
		return false
	}
	if elem != nil {
		return false
	}
	if domEvent.Get("pointerType").String() != "mouse" {
		return false
	}
	return domEvent.Get("button").Int() == 0
}

// =====================================================================
//  Pinch Detection | Detecção de Pinch
// =====================================================================

// handleSecondPointerDown is called when a second touch arrives while one is
// already tracked. Starts pinch and cancels any in-progress element drag.
//
// Português: Chamado quando um segundo touch chega enquanto um já está rastreado.
// Inicia pinch e cancela qualquer drag de elemento em progresso.
func (e *stage) handleSecondPointerDown(domEvent js.Value, cx, cy float64) bool {
	cam := e.camera
	if cam == nil || !cam.ZoomEnabled {
		return false
	}

	if domEvent.Get("pointerType").String() != "touch" {
		return false
	}

	if e.interaction == stateIdle {
		return false
	}

	// Cancel element drag in progress.
	// Português: Cancela drag de elemento em progresso.
	if e.interaction == stateDragging && e.activeElement != nil {
		e.activeElement.x = e.elementStartX
		e.activeElement.y = e.elementStartY
	}

	newID := domEvent.Get("pointerId").Int()
	cam.StartPinch(
		e.activePointerID, newID,
		e.pointerStartX, e.pointerStartY,
		cx, cy,
	)

	e.secondPointerID = newID
	e.interaction = statePinching
	return true
}
