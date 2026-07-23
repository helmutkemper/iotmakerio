// sprite/stageEvents.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

import (
	"math"
	"syscall/js"
	"time"
)

// =====================================================================
//  Event Listener Management | Gerenciamento de Event Listeners
// =====================================================================

// attachListeners
//
// English:
//
//	Attaches all pointer event listeners to the canvas. Uses the Pointer Events API
//	which unifies mouse and touch into a single set of events (pointerdown, pointermove,
//	pointerup, pointercancel).
//
//	A single set of listeners on the canvas is much more efficient than attaching
//	listeners to individual elements, because:
//	1. Only 4 event listeners total, regardless of how many elements exist.
//	2. No need to manage listener lifecycle when elements are added/removed.
//	3. The stage performs hit-testing internally, which is O(n) per event but
//	   avoids the DOM overhead of N separate listeners.
//
//	When a Camera is set, additional listeners are attached for wheel (zoom)
//	and keydown (Home/F shortcuts).
//
// Português:
//
//	Anexa todos os event listeners de ponteiro ao canvas. Usa a API Pointer Events
//	que unifica mouse e touch em um único conjunto de eventos.
//
//	Quando uma Camera está definida, listeners adicionais são anexados para wheel
//	(zoom) e keydown (atalhos Home/F).
func (e *stage) attachListeners() {
	e.listeners["pointerdown"] = js.FuncOf(e.handlePointerDown)
	e.listeners["pointermove"] = js.FuncOf(e.handlePointerMove)
	e.listeners["pointerup"] = js.FuncOf(e.handlePointerUp)
	e.listeners["pointercancel"] = js.FuncOf(e.handlePointerCancel)

	for eventName, fn := range e.listeners {
		e.canvas.Call("addEventListener", eventName, fn)
	}

	// Camera-specific listeners (wheel, keydown).
	// Português: Listeners específicos da câmera (wheel, keydown).
	if e.camera != nil {
		e.attachCameraListeners()
	}
}

// detachListeners
//
// English:
//
//	Removes all event listeners from the canvas and releases the associated JS functions.
//	Must be called before discarding the stage to prevent memory leaks.
//
// Português:
//
//	Remove todos os event listeners do canvas e libera as funções JS associadas.
//	Deve ser chamado antes de descartar o stage para prevenir vazamentos de memória.
func (e *stage) detachListeners() {
	// Remove camera-specific listeners first (they are in the same map but
	// attached to different targets — wheel on canvas, keydown on document).
	// Português: Remove listeners da câmera primeiro (estão no mesmo map mas
	// anexados a alvos diferentes — wheel no canvas, keydown no document).
	e.detachCameraListeners()

	for eventName, fn := range e.listeners {
		e.canvas.Call("removeEventListener", eventName, fn)
		fn.Release()
	}
	e.listeners = make(map[string]js.Func)
}

// =====================================================================
//  Pointer Event Helpers | Helpers de Eventos de Ponteiro
// =====================================================================

// buildPointerEvent
//
// English:
//
//	Constructs a PointerEvent struct from a DOM pointer event and an optional
//	target element for local coordinates.
//
//	When a Camera is active, LocalX/LocalY are computed in world coordinates
//	relative to the element, so they remain consistent regardless of pan/zoom.
//
// Português:
//
//	Constrói uma struct PointerEvent a partir de um evento de ponteiro DOM e um
//	elemento alvo opcional para coordenadas locais.
//
//	Quando uma Camera está ativa, LocalX/LocalY são computados em coordenadas mundo
//	relativas ao elemento, permanecendo consistentes independente de pan/zoom.
func (e *stage) buildPointerEvent(domEvent js.Value, canvasX float64, canvasY float64, target *elementData) (pe PointerEvent) {
	pe.CanvasX = canvasX
	pe.CanvasY = canvasY
	pe.Button = domEvent.Get("button").Int()
	pe.IsTouch = domEvent.Get("pointerType").String() == "touch"

	if target != nil {
		if target.screenSpace {
			// Screen-space elements: LocalX/Y are relative to screen position.
			// Português: Elementos screen-space: LocalX/Y são relativos à posição de tela.
			pe.LocalX = canvasX - target.x
			pe.LocalY = canvasY - target.y
		} else {
			// World-space elements: convert screen coords to world for accurate local coordinates.
			// Português: Elementos world-space: converte coordenadas de tela para mundo.
			worldX, worldY := e.canvasCoordsToWorld(canvasX, canvasY)
			pe.LocalX = worldX - target.x
			pe.LocalY = worldY - target.y
		}
	}
	return
}

// buildDragEvent
//
// English:
//
//	Constructs a DragEvent struct with accumulated deltas from the drag start point.
//
// Português:
//
//	Constrói uma struct DragEvent com deltas acumulados a partir do ponto de início
//	do arraste.
func (e *stage) buildDragEvent(domEvent js.Value, canvasX float64, canvasY float64) (de DragEvent) {
	de.PointerEvent = e.buildPointerEvent(domEvent, canvasX, canvasY, e.activeElement)
	de.StartX = e.pointerStartX
	de.StartY = e.pointerStartY
	de.DeltaX = canvasX - e.pointerStartX
	de.DeltaY = canvasY - e.pointerStartY
	return
}

// buildResizeEvent
//
// English:
//
//	Constructs a ResizeEvent struct with old and new dimensions.
//
// Português:
//
//	Constrói uma struct ResizeEvent com dimensões anterior e nova.
func (e *stage) buildResizeEvent(domEvent js.Value, canvasX float64, canvasY float64, oldW float64, oldH float64, newW float64, newH float64) (re ResizeEvent) {
	re.PointerEvent = e.buildPointerEvent(domEvent, canvasX, canvasY, e.activeElement)
	re.Handle = e.activeHandle
	re.OldWidth = oldW
	re.OldHeight = oldH
	re.NewWidth = newW
	re.NewHeight = newH
	return
}

// =====================================================================
//  Pointer Down | Ponteiro Pressionado
// =====================================================================

// handlePointerDown
//
// English:
//
//	Handles the pointerdown event. This is the entry point for all interactions.
//
//	Decision flow:
//	1. Check for middle-mouse pan (camera feature).
//	2. Check for second touch → pinch-zoom (camera feature).
//	3. Find topmost element at pointer position (world coordinates).
//	4. If element has resize enabled and pointer is on a resize handle → start resize.
//	5. Otherwise → enter "pressed" state (will become drag or click on pointerup).
//
//	setPointerCapture is used to ensure we receive pointermove and pointerup events
//	even if the pointer leaves the canvas. This is critical for reliable drag/resize
//	operations on touch devices where the finger can easily slip outside the canvas.
//
// Português:
//
//	Trata o evento pointerdown. Este é o ponto de entrada para todas as interações.
//
//	Fluxo de decisão:
//	1. Verifica pan via botão do meio do mouse (feature da câmera).
//	2. Verifica segundo touch → pinch-zoom (feature da câmera).
//	3. Encontra o elemento mais acima na posição do ponteiro (coordenadas mundo).
//	4. Se o elemento tem resize habilitado e o ponteiro está em uma alça → inicia resize.
//	5. Caso contrário → entra no estado "pressed" (se tornará drag ou click no pointerup).
func (e *stage) handlePointerDown(this js.Value, args []js.Value) interface{} {
	domEvent := args[0]
	domEvent.Call("preventDefault")

	cx, cy := canvasCoords(e.canvas, e.canvasWidth, e.canvasHeight, domEvent)
	pointerID := domEvent.Get("pointerId").Int()

	// Capture the pointer so we get move/up events even outside the canvas.
	// Português: Captura o ponteiro para receber eventos move/up mesmo fora do canvas.
	e.canvas.Call("setPointerCapture", pointerID)

	// [CAMERA] Middle-mouse pan — intercept before element interaction.
	// Português: Pan via botão do meio — intercepta antes da interação com elementos.
	if e.shouldStartCameraPan(domEvent) {
		e.camera.StartPan(cx, cy, pointerID)
		e.dirty = true
		return nil
	}

	// [CAMERA] Minimap click-to-navigate — intercept before element interaction.
	// Português: Click no minimapa para navegar — intercepta antes da interação com elementos.
	if e.camera != nil && domEvent.Get("button").Int() == 0 {
		if e.camera.HandleMinimapClick(cx, cy, e.canvasWidth, e.canvasHeight) {
			e.dirty = true
			return nil
		}
	}

	// [CAMERA] Second touch → pinch-zoom (cancels any in-progress element drag).
	// Português: Segundo touch → pinch-zoom — intercepta antes da interação com elementos.
	if e.interaction != stateIdle && domEvent.Get("pointerType").String() == "touch" {
		if e.handleSecondPointerDown(domEvent, cx, cy) {
			return nil
		}
	}

	// Convert screen coordinates to world for hit-testing.
	// Português: Converte coordenadas de tela para mundo para hit-testing.
	worldX, worldY := e.canvasCoordsToWorld(cx, cy)
	elem := e.findElementAt(cx, cy, worldX, worldY)

	// [CAMERA] Left-drag on empty stage → camera pan (desktop mouse).
	// Intercept BEFORE we set stagePressed — we don't want to go
	// through the click-vs-drag arbitration for this case. The
	// middle-mouse pan above handles the click case; here we handle
	// the left-button case that trackpad users (no middle button)
	// need.
	//
	// Português: Left-drag em área vazia da stage → pan da câmera
	// (apenas desktop mouse). Necessário para quem não tem botão
	// do meio no trackpad.
	if e.shouldStartEmptyAreaPan(domEvent, elem) {
		e.camera.StartPan(cx, cy, pointerID)
		// Cursor hint while panning: `grabbing` is the web-standard
		// for a held drag gesture. Reverts at pan end (handlePointerUp).
		e.canvas.Get("style").Set("cursor", "grabbing")
		e.dirty = true
		return nil
	}

	// Store common start state (screen coordinates for delta calculation).
	// Português: Armazena estado inicial comum (coordenadas de tela para cálculo de delta).
	e.pointerStartX = cx
	e.pointerStartY = cy
	e.pointerStartIsTouch = domEvent.Get("pointerType").String() == "touch"
	e.pointerStartButton = domEvent.Get("button").Int()
	e.activePointerID = pointerID
	e.activeElement = elem

	if elem != nil {
		// Check resize handle first — resize takes priority over drag.
		// For screen-space elements, use screen coords; for world-space, use world coords.
		// Português: Verifica alça de resize primeiro — resize tem prioridade sobre drag.
		// Para elementos screen-space, usa coords de tela; para world-space, usa mundo.
		if elem.resizeEnable {
			var hitX, hitY float64
			if elem.screenSpace {
				hitX, hitY = cx, cy
			} else {
				hitX, hitY = worldX, worldY
			}
			handle := elem.HitTestResizeHandle(hitX, hitY)
			if handle != ResizeHandleNone {
				e.interaction = stateResizing
				e.activeHandle = handle
				e.elementStartX = elem.x
				e.elementStartY = elem.y
				e.elementStartW = elem.width
				e.elementStartH = elem.height
				e.lastResizeRedrawTime = nowMillis()

				if elem.onResizeStart != nil {
					re := e.buildResizeEvent(domEvent, cx, cy, elem.width, elem.height, elem.width, elem.height)
					elem.onResizeStart(re)
				}
				return nil
			}
		}

		// Store element start position for potential drag.
		// Português: Armazena posição inicial do elemento para possível arraste.
		e.elementStartX = elem.x
		e.elementStartY = elem.y
	}

	// Enter pressed state — we'll determine intent (click vs drag) on move/up.
	// Português: Entra no estado pressed — determinaremos a intenção (click vs drag) no move/up.
	e.interaction = statePressed

	return nil
}

// =====================================================================
//  Pointer Move | Ponteiro em Movimento
// =====================================================================

// handlePointerMove
//
// English:
//
//	Handles the pointermove event. Updates drag/resize operations and hover state.
//	When a Camera is active, also handles pan and pinch-zoom updates.
//
// Português:
//
//	Trata o evento pointermove. Atualiza operações de drag/resize e estado de hover.
//	Quando uma Camera está ativa, também trata atualizações de pan e pinch-zoom.
func (e *stage) handlePointerMove(this js.Value, args []js.Value) interface{} {
	domEvent := args[0]
	domEvent.Call("preventDefault")

	cx, cy := canvasCoords(e.canvas, e.canvasWidth, e.canvasHeight, domEvent)

	// [CAMERA] Pan via middle-mouse — handle before everything else.
	// Português: Pan via botão do meio — trata antes de tudo.
	if e.camera != nil && e.camera.IsPanning() {
		pointerID := domEvent.Get("pointerId").Int()
		if pointerID == e.camera.panPointerID {
			e.camera.UpdatePan(cx, cy)
			e.dirty = true
			return nil
		}
	}

	// [CAMERA] Pinch-zoom update.
	// Português: Atualização de pinch-zoom.
	if e.interaction == statePinching && e.camera != nil {
		pointerID := domEvent.Get("pointerId").Int()
		e.camera.UpdatePinch(pointerID, cx, cy)
		e.dirty = true
		return nil
	}

	// Fire stage-level move callback regardless of interaction state.
	// Português: Dispara callback de movimento do stage independente do estado de interação.
	if e.onPointerMoveStage != nil {
		pe := e.buildPointerEvent(domEvent, cx, cy, nil)
		e.onPointerMoveStage(pe)
	}

	switch e.interaction {
	case statePressed:
		e.handleMoveWhilePressed(domEvent, cx, cy)

	case stateDragging:
		e.handleMoveWhileDragging(domEvent, cx, cy)

	case stateResizing:
		e.handleMoveWhileResizing(domEvent, cx, cy)

	case stateIdle:
		// Update hover state and cursor (mouse only — touch has no hover).
		// Português: Atualiza estado de hover e cursor (somente mouse — touch não tem hover).
		if domEvent.Get("pointerType").String() != "touch" {
			e.updateHoverState(domEvent, cx, cy)
			e.updateCursor(cx, cy)
		}
	}

	return nil
}

// handleMoveWhilePressed
//
// English:
//
//	Handles pointermove when in the "pressed" state. If the active element has drag
//	enabled and the pointer has moved beyond the drag threshold, transitions to
//	the "dragging" state.
//
//	The drag threshold prevents accidental drags when the user just wants to click.
//	A typical threshold is 4px — small enough to feel responsive but large enough
//	to filter out unintentional micro-movements.
//
// Português:
//
//	Trata pointermove quando no estado "pressed". Se o elemento ativo tem drag
//	habilitado e o ponteiro se moveu além do threshold de arraste, transiciona
//	para o estado "dragging".
func (e *stage) handleMoveWhilePressed(domEvent js.Value, cx float64, cy float64) {
	if e.activeElement == nil || !e.activeElement.dragEnable {
		return
	}

	dist := distance(e.pointerStartX, e.pointerStartY, cx, cy)
	if dist < e.config.DragThreshold {
		return
	}

	// Threshold exceeded — transition to dragging.
	// Português: Threshold excedido — transiciona para arrastando.
	e.interaction = stateDragging

	if e.activeElement.onDragStart != nil {
		de := e.buildDragEvent(domEvent, cx, cy)
		e.activeElement.onDragStart(de)
	}

	// Apply the first drag movement immediately.
	// Português: Aplica o primeiro movimento de arraste imediatamente.
	e.applyDragMovement(domEvent, cx, cy)
}

// handleMoveWhileDragging
//
// English:
//
//	Handles pointermove during an active drag operation.
//
// Português:
//
//	Trata pointermove durante uma operação de arraste ativa.
func (e *stage) handleMoveWhileDragging(domEvent js.Value, cx float64, cy float64) {
	e.applyDragMovement(domEvent, cx, cy)
}

// applyDragMovement
//
// English:
//
//	Calculates and applies the new position for the dragged element.
//	The new position is based on the element's original position plus the pointer
//	delta from the start. This approach ensures pixel-perfect tracking without
//	accumulated floating-point errors that would occur with incremental deltas.
//
//	When a Camera is active, the screen-space pointer delta is converted to
//	world-space by dividing by the camera zoom factor, so dragging feels
//	consistent regardless of zoom level.
//
// Português:
//
//	Calcula e aplica a nova posição para o elemento sendo arrastado.
//	A nova posição é baseada na posição original do elemento mais o delta do ponteiro
//	desde o início. Esta abordagem garante rastreamento pixel-perfeito sem erros de
//	ponto flutuante acumulados que ocorreriam com deltas incrementais.
//
//	Quando uma Camera está ativa, o delta do ponteiro em espaço de tela é convertido
//	para espaço mundo dividindo pelo fator de zoom da câmera, para que o arraste
//	pareça consistente independente do nível de zoom.
func (e *stage) applyDragMovement(domEvent js.Value, cx float64, cy float64) {
	elem := e.activeElement
	if elem == nil {
		return
	}

	// Convert screen delta to world delta (accounts for camera zoom).
	// Screen-space elements move in screen pixels — no zoom conversion.
	// Português: Converte delta de tela para delta mundo (considera zoom da câmera).
	// Elementos screen-space movem em pixels de tela — sem conversão de zoom.
	var dx, dy float64
	if elem.screenSpace {
		dx = cx - e.pointerStartX
		dy = cy - e.pointerStartY
	} else {
		dx, dy = e.screenDeltaToWorldDelta(cx-e.pointerStartX, cy-e.pointerStartY)
	}

	newX := e.elementStartX + dx
	newY := e.elementStartY + dy

	// Apply drag bounds constraint.
	// Português: Aplica restrição dos limites de arraste.
	newX, newY = elem.clampToDragBounds(newX, newY)

	elem.x = newX
	elem.y = newY
	e.dirty = true

	if elem.onDragMove != nil {
		de := e.buildDragEvent(domEvent, cx, cy)
		elem.onDragMove(de)
	}

	e.updateCursor(cx, cy)
}

// handleMoveWhileResizing
//
// English:
//
//	Handles pointermove during an active resize operation. Computes new position and
//	size based on which handle is being dragged.
//
//	The resize logic uses the element's start state (captured at pointerdown) plus
//	pointer deltas, same as drag. Each handle affects different combinations of
//	x, y, width, height:
//	  - Corner handles: change both position and size
//	  - Top/Bottom handles: change y/height only
//	  - Left/Right handles: change x/width only
//
//	When a Camera is active, pointer deltas are converted from screen-space to
//	world-space to maintain consistent resize behavior across zoom levels.
//
// Português:
//
//	Trata pointermove durante uma operação de resize ativa. Computa nova posição e
//	tamanho baseado em qual alça está sendo arrastada.
//
//	Quando uma Camera está ativa, deltas do ponteiro são convertidos de espaço de
//	tela para espaço mundo para manter comportamento de resize consistente entre
//	níveis de zoom.
func (e *stage) handleMoveWhileResizing(domEvent js.Value, cx float64, cy float64) {
	elem := e.activeElement
	if elem == nil {
		return
	}

	// Convert screen delta to world delta (accounts for camera zoom).
	// Screen-space elements resize in screen pixels — no zoom conversion.
	// Português: Converte delta de tela para delta mundo (considera zoom da câmera).
	// Elementos screen-space redimensionam em pixels de tela — sem conversão de zoom.
	var dx, dy float64
	if elem.screenSpace {
		dx = cx - e.pointerStartX
		dy = cy - e.pointerStartY
	} else {
		dx, dy = e.screenDeltaToWorldDelta(cx-e.pointerStartX, cy-e.pointerStartY)
	}

	oldW := elem.width
	oldH := elem.height

	newX := e.elementStartX
	newY := e.elementStartY
	newW := e.elementStartW
	newH := e.elementStartH

	// Apply deltas based on which handle is being dragged.
	// Português: Aplica deltas baseado em qual alça está sendo arrastada.
	switch e.activeHandle {
	case ResizeHandleTopLeft:
		newX += dx
		newY += dy
		newW -= dx
		newH -= dy

	case ResizeHandleTop:
		newY += dy
		newH -= dy

	case ResizeHandleTopRight:
		newY += dy
		newW += dx
		newH -= dy

	case ResizeHandleRight:
		newW += dx

	case ResizeHandleBottomRight:
		newW += dx
		newH += dy

	case ResizeHandleBottom:
		newH += dy

	case ResizeHandleBottomLeft:
		newX += dx
		newW -= dx
		newH += dy

	case ResizeHandleLeft:
		newX += dx
		newW -= dx
	}

	// Apply aspect ratio constraint for corner handles.
	// Português: Aplica restrição de proporção para alças de canto.
	if elem.keepAspectRatio && isCornerHandle(e.activeHandle) {
		ratio := e.elementStartW / e.elementStartH
		// Use the axis with the larger absolute delta to drive the resize.
		// Português: Usa o eixo com o maior delta absoluto para conduzir o resize.
		if math.Abs(dx) > math.Abs(dy) {
			newH = newW / ratio
		} else {
			newW = newH * ratio
		}
	}

	// Enforce minimum size.
	// Português: Aplica tamanho mínimo.
	if newW < elem.minWidth {
		// When shrinking would go below min, pin the size and adjust position.
		// Português: Quando encolher iria abaixo do mínimo, fixa o tamanho e ajusta posição.
		if affectsLeft(e.activeHandle) {
			newX = e.elementStartX + e.elementStartW - elem.minWidth
		}
		newW = elem.minWidth
	}
	if newH < elem.minHeight {
		if affectsTop(e.activeHandle) {
			newY = e.elementStartY + e.elementStartH - elem.minHeight
		}
		newH = elem.minHeight
	}

	// Enforce maximum size if set.
	// Português: Aplica tamanho máximo se definido.
	if elem.maxWidth > 0 && newW > elem.maxWidth {
		if affectsLeft(e.activeHandle) {
			newX = e.elementStartX + e.elementStartW - elem.maxWidth
		}
		newW = elem.maxWidth
	}
	if elem.maxHeight > 0 && newH > elem.maxHeight {
		if affectsTop(e.activeHandle) {
			newY = e.elementStartY + e.elementStartH - elem.maxHeight
		}
		newH = elem.maxHeight
	}

	elem.x = newX
	elem.y = newY
	elem.width = newW
	elem.height = newH
	e.dirty = true

	if elem.onResizeMove != nil {
		re := e.buildResizeEvent(domEvent, cx, cy, oldW, oldH, newW, newH)
		elem.onResizeMove(re)
	}

	// Periodic resize redraw — if configured, call the redraw callback at regular
	// intervals so the user can re-cache the element's visual content (e.g.,
	// re-render SVG at the current size) to prevent stretched/distorted appearance.
	//
	// Português: Redesenho periódico durante resize — se configurado, chama o callback
	// de redesenho em intervalos regulares para que o usuário possa re-cachear o
	// conteúdo visual do elemento.
	if elem.resizeRedrawInterval > 0 && elem.onResizeRedraw != nil {
		now := nowMillis()
		if now-e.lastResizeRedrawTime >= float64(elem.resizeRedrawInterval) {
			e.lastResizeRedrawTime = now
			re := e.buildResizeEvent(domEvent, cx, cy, oldW, oldH, newW, newH)
			elem.onResizeRedraw(re)
		}
	}

	e.updateCursor(cx, cy)
}

// =====================================================================
//  Pointer Up | Ponteiro Liberado
// =====================================================================

// handlePointerUp
//
// English:
//
//	Handles the pointerup event. Completes the current interaction:
//	- If camera pan → end pan.
//	- If pinching → end pinch.
//	- If pressed (no drag occurred) → treat as click, check for double-click.
//	- If dragging → fire onDragEnd.
//	- If resizing → fire onResizeEnd.
//
// Português:
//
//	Trata o evento pointerup. Completa a interação atual:
//	- Se pan da câmera → finaliza pan.
//	- Se pinch → finaliza pinch.
//	- Se pressed (sem drag) → trata como click, verifica double-click.
//	- Se arrastando → dispara onDragEnd.
//	- Se redimensionando → dispara onResizeEnd.
func (e *stage) handlePointerUp(this js.Value, args []js.Value) interface{} {
	domEvent := args[0]
	domEvent.Call("preventDefault")

	cx, cy := canvasCoords(e.canvas, e.canvasWidth, e.canvasHeight, domEvent)

	// Release pointer capture.
	// Português: Libera a captura do ponteiro.
	pointerID := domEvent.Get("pointerId").Int()
	e.canvas.Call("releasePointerCapture", pointerID)

	// [CAMERA] End pan via middle-mouse or empty-area left-drag.
	// Reset the cursor that we may have set to "grabbing" during
	// shouldStartEmptyAreaPan. Setting it to empty string restores
	// whatever CSS rule the page has for the canvas.
	//
	// Português: Finaliza pan (botão do meio ou left-drag em área
	// vazia). Reseta o cursor que pode ter sido posto como "grabbing".
	if e.camera != nil && e.camera.IsPanning() && pointerID == e.camera.panPointerID {
		e.camera.EndPan()
		e.canvas.Get("style").Set("cursor", "")
		return nil
	}

	// [CAMERA] End pinch-zoom.
	// Português: Finaliza pinch-zoom.
	if e.interaction == statePinching {
		if e.camera != nil {
			e.camera.EndPinch()
		}
		e.interaction = stateIdle
		e.activeElement = nil
		return nil
	}

	switch e.interaction {
	case statePressed:
		e.handleClickDetection(domEvent, cx, cy)

	case stateDragging:
		if e.activeElement != nil && e.activeElement.onDragEnd != nil {
			de := e.buildDragEvent(domEvent, cx, cy)
			e.activeElement.onDragEnd(de)
		}

	case stateResizing:
		if e.activeElement != nil && e.activeElement.onResizeEnd != nil {
			re := e.buildResizeEvent(domEvent, cx, cy,
				e.activeElement.width, e.activeElement.height,
				e.activeElement.width, e.activeElement.height)
			e.activeElement.onResizeEnd(re)
		}
	}

	// Reset interaction state.
	// Português: Reseta o estado de interação.
	e.interaction = stateIdle
	e.activeElement = nil
	e.activeHandle = ResizeHandleNone

	// Update cursor after interaction ends.
	// Português: Atualiza cursor após o fim da interação.
	if domEvent.Get("pointerType").String() != "touch" {
		e.updateCursor(cx, cy)
	}

	return nil
}

// handleClickDetection
//
// English:
//
//	Processes a click (pointer went down and up without becoming a drag).
//	Implements unified double-click detection for both mouse and touch by tracking
//	the time, position, and target element of the last click.
//
//	Native dblclick is unreliable on touch and adds complexity by requiring a separate
//	code path. This timing-based approach gives consistent behavior across input types.
//
// Português:
//
//	Processa um click (ponteiro pressionou e soltou sem se tornar um arraste).
//	Implementa detecção de double-click unificada para mouse e touch rastreando
//	o tempo, posição e elemento alvo do último click.
func (e *stage) handleClickDetection(domEvent js.Value, cx float64, cy float64) {
	now := nowMillis()
	elem := e.activeElement

	pe := e.buildPointerEvent(domEvent, cx, cy, elem)

	// Check if this click qualifies as the second click of a double-click.
	// Conditions: same element, within time interval, within position tolerance.
	//
	// Português: Verifica se este click qualifica como o segundo click de um double-click.
	// Condições: mesmo elemento, dentro do intervalo de tempo, dentro da tolerância de posição.
	isDoubleClick := false
	if e.lastClickElement == elem &&
		(now-e.lastClickTime) < float64(e.config.DoubleClickInterval) &&
		distance(e.lastClickX, e.lastClickY, cx, cy) < e.config.DragThreshold*2 {

		isDoubleClick = true

		// Reset last click to prevent triple-click counting as another double.
		// Português: Reseta último click para prevenir triple-click de contar como outro double.
		e.lastClickElement = nil
		e.lastClickTime = 0
	} else {
		// Record this click for potential double-click detection on next click.
		// Português: Registra este click para possível detecção de double-click no próximo click.
		e.lastClickElement = elem
		e.lastClickTime = now
		e.lastClickX = cx
		e.lastClickY = cy
	}

	if isDoubleClick {
		// The pending single is superseded — cancel it so only the
		// double fires. Português: O simples pendente é superado —
		// cancela para só o duplo disparar.
		if e.pendingClick != nil {
			e.pendingClick.Stop()
			e.pendingClick = nil
		}
		if elem != nil && elem.onDoubleClick != nil {
			elem.onDoubleClick(pe)
		} else if elem == nil && e.onDoubleClickStage != nil {
			e.onDoubleClickStage(pe)
		}
	} else {
		// Fire single click.
		// Português: Dispara click simples.
		// [WIRE-PRIORITY] The click interceptor runs BEFORE element
		// routing: consumers (the workspace's wire hit) can claim the
		// click even when an element — typically a container body —
		// sits under the pointer. Single click only; drags and double
		// clicks are untouched.
		// Português: O interceptador roda ANTES do roteamento por
		// element: consumidores (o hit de wire do workspace) podem
		// reivindicar o clique mesmo com um element — tipicamente o
		// corpo de um container — sob o ponteiro. Só clique simples;
		// drags e duplo-clique intactos.
		// Delayed dispatch: fire after the double-click window unless a
		// second click cancels it. Rapid clicks on different targets
		// flush the previous pending immediately. Português: Disparo
		// atrasado — dispara após a janela do duplo, salvo cancelamento;
		// cliques rápidos em alvos distintos descarregam o pendente.
		fire := func() {
			if e.clickInterceptor != nil && e.clickInterceptor(pe) {
				// consumed / consumido
			} else if elem != nil && elem.onClick != nil {
				elem.onClick(pe)
			} else if elem == nil && e.onClickStage != nil {
				e.onClickStage(pe)
			}
		}
		if e.pendingClick != nil {
			if e.pendingClick.Stop() {
				// A different-target click was waiting — deliver it now
				// so no gesture is swallowed. Português: Um clique de
				// outro alvo esperava — entrega já, nada é engolido.
			}
			e.pendingClick = nil
		}
		e.pendingClick = time.AfterFunc(
			time.Duration(e.config.DoubleClickInterval)*time.Millisecond,
			func() { e.pendingClick = nil; fire() })
	}
}

// =====================================================================
//  Pointer Cancel | Ponteiro Cancelado
// =====================================================================

// handlePointerCancel
//
// English:
//
//	Handles the pointercancel event, which fires when the browser cancels a pointer
//	interaction (e.g., palm rejection on touch, browser taking over for scroll/zoom).
//	Resets the interaction state without firing end callbacks, since the interaction
//	was not completed normally.
//
//	For drag operations, the element is returned to its start position because
//	the cancel implies the interaction was unintentional.
//
// Português:
//
//	Trata o evento pointercancel, que dispara quando o navegador cancela uma interação
//	de ponteiro. Reseta o estado de interação sem disparar callbacks de fim, pois a
//	interação não foi completada normalmente.
func (e *stage) handlePointerCancel(this js.Value, args []js.Value) interface{} {

	// [CAMERA] Cancel pinch if active.
	// Português: Cancela pinch se ativo.
	if e.interaction == statePinching && e.camera != nil {
		e.camera.EndPinch()
	}

	// [CAMERA] Cancel pan if active (pointercancel or unexpected end).
	// Reset the cursor symmetrically to the normal EndPan path.
	//
	// Português: Cancela pan se ativo. Reseta o cursor de forma
	// simétrica ao fim normal.
	if e.camera != nil && e.camera.IsPanning() {
		e.camera.EndPan()
		e.canvas.Get("style").Set("cursor", "")
	}

	// Revert drag/resize to start position if applicable.
	// Português: Reverte drag/resize para posição inicial se aplicável.
	if e.activeElement != nil {
		switch e.interaction {
		case stateDragging:
			e.activeElement.x = e.elementStartX
			e.activeElement.y = e.elementStartY
			e.dirty = true

		case stateResizing:
			e.activeElement.x = e.elementStartX
			e.activeElement.y = e.elementStartY
			e.activeElement.width = e.elementStartW
			e.activeElement.height = e.elementStartH
			e.dirty = true
		}
	}

	e.interaction = stateIdle
	e.activeElement = nil
	e.activeHandle = ResizeHandleNone

	return nil
}

// =====================================================================
//  Hover State | Estado de Hover
// =====================================================================

// updateHoverState
//
// English:
//
//	Updates which element the pointer is hovering over and fires pointerEnter/
//	pointerLeave callbacks when the hovered element changes. Only called for
//	mouse events (touch has no hover).
//
//	When a Camera is active, screen coordinates are converted to world coordinates
//	for hit-testing and local coordinate calculation.
//
// Português:
//
//	Atualiza qual elemento o ponteiro está sobre e dispara callbacks pointerEnter/
//	pointerLeave quando o elemento sob hover muda. Só chamado para eventos de mouse.
//
//	Quando uma Camera está ativa, coordenadas de tela são convertidas para coordenadas
//	mundo para hit-testing e cálculo de coordenadas locais.
func (e *stage) updateHoverState(domEvent js.Value, cx float64, cy float64) {
	// Find element using both screen and world coordinates.
	// Português: Encontra elemento usando coordenadas de tela e mundo.
	worldX, worldY := e.canvasCoordsToWorld(cx, cy)
	elem := e.findElementAt(cx, cy, worldX, worldY)

	if elem == e.hoveredElement {
		return
	}

	pe := e.buildPointerEvent(domEvent, cx, cy, nil)

	// Fire leave on old hovered element.
	// Português: Dispara leave no elemento anterior em hover.
	if e.hoveredElement != nil && e.hoveredElement.onPointerLeave != nil {
		leavePe := pe
		if e.hoveredElement.screenSpace {
			leavePe.LocalX = cx - e.hoveredElement.x
			leavePe.LocalY = cy - e.hoveredElement.y
		} else {
			leavePe.LocalX = worldX - e.hoveredElement.x
			leavePe.LocalY = worldY - e.hoveredElement.y
		}
		e.hoveredElement.onPointerLeave(leavePe)
	}

	// Fire enter on new hovered element.
	// Português: Dispara enter no novo elemento em hover.
	if elem != nil && elem.onPointerEnter != nil {
		enterPe := pe
		if elem.screenSpace {
			enterPe.LocalX = cx - elem.x
			enterPe.LocalY = cy - elem.y
		} else {
			enterPe.LocalX = worldX - elem.x
			enterPe.LocalY = worldY - elem.y
		}
		elem.onPointerEnter(enterPe)
	}

	e.hoveredElement = elem
}

// =====================================================================
//  Handle Helpers | Helpers de Alça
// =====================================================================

// isCornerHandle
//
// English:
//
//	Returns whether the given resize handle is a corner handle.
//
// Português:
//
//	Retorna se a alça de resize fornecida é uma alça de canto.
func isCornerHandle(h ResizeHandle) (corner bool) {
	corner = h == ResizeHandleTopLeft ||
		h == ResizeHandleTopRight ||
		h == ResizeHandleBottomLeft ||
		h == ResizeHandleBottomRight
	return
}

// affectsLeft
//
// English:
//
//	Returns whether the given resize handle affects the left edge (and thus the X position).
//
// Português:
//
//	Retorna se a alça de resize fornecida afeta a borda esquerda (e portanto a posição X).
func affectsLeft(h ResizeHandle) (left bool) {
	left = h == ResizeHandleTopLeft ||
		h == ResizeHandleLeft ||
		h == ResizeHandleBottomLeft
	return
}

// affectsTop
//
// English:
//
//	Returns whether the given resize handle affects the top edge (and thus the Y position).
//
// Português:
//
//	Retorna se a alça de resize fornecida afeta a borda superior (e portanto a posição Y).
func affectsTop(h ResizeHandle) (top bool) {
	top = h == ResizeHandleTopLeft ||
		h == ResizeHandleTop ||
		h == ResizeHandleTopRight
	return
}
