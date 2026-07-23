// sprite/stageImplement.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

import (
	"sort"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/rulesSprite"
)

// interactionState represents the current pointer interaction mode.
//
// English:
//
//	The stage uses a finite state machine to manage pointer interactions. This avoids
//	complex nested conditionals and makes the drag/resize/click logic deterministic.
//
// Português:
//
//	O stage usa uma máquina de estados finita para gerenciar interações de ponteiro.
//	Isso evita condicionais aninhadas complexas e torna a lógica de drag/resize/click
//	determinística.
type interactionState int

const (
	// stateIdle means no active interaction.
	stateIdle interactionState = iota

	// statePressed means pointer is down but we haven't determined intent yet.
	// Could become a drag, a resize (already decided at pointerdown), or a click.
	statePressed

	// stateDragging means a confirmed drag operation is in progress.
	stateDragging

	// stateResizing means a confirmed resize operation is in progress.
	stateResizing

	// statePinching means a two-finger pinch-zoom gesture is in progress.
	// Português: Significa que um gesto de pinch-zoom com dois dedos está em progresso.
	statePinching
)

// stage is the concrete implementation of the Stage interface.
type stage struct {
	config StageConfig

	// DOM references
	// Português: Referências ao DOM
	canvas       js.Value
	ctx          js.Value
	canvasWidth  int
	canvasHeight int

	// Element storage: map for O(1) lookup, slice for ordered draw.
	// Português: Armazenamento de elementos: map para busca O(1), slice para desenho ordenado.
	elements  map[string]*elementData
	drawOrder []*elementData
	sortDirty bool

	// Render loop state
	// Português: Estado do loop de renderização
	dirty      bool
	running    bool
	destroyed  bool
	renderFunc js.Func

	// Interaction state machine
	// Português: Máquina de estados de interação
	interaction     interactionState
	activeElement   *elementData
	activeHandle    ResizeHandle
	activePointerID int

	// Pointer start state — recorded at pointerdown for drag threshold and click detection.
	// Português: Estado inicial do ponteiro — registrado no pointerdown para threshold de
	// arraste e detecção de click.
	pointerStartX       float64
	pointerStartY       float64
	pointerStartIsTouch bool
	pointerStartButton  int

	// Element start state — recorded at pointerdown for drag/resize delta calculation.
	// Português: Estado inicial do elemento — registrado no pointerdown para cálculo de
	// delta de drag/resize.
	elementStartX float64
	elementStartY float64
	elementStartW float64
	elementStartH float64

	// Double-click detection via timing.
	// Português: Detecção de double-click por temporização.
	lastClickTime    float64
	lastClickX       float64
	lastClickY       float64
	lastClickElement *elementData

	// Hover tracking — which element the pointer is currently over (mouse only).
	// Português: Rastreamento de hover — qual elemento o ponteiro está sobre (somente mouse).
	hoveredElement *elementData

	// Resize redraw timer — tracks when the last periodic redraw was triggered.
	// Português: Timer de redesenho durante resize — rastreia quando o último redesenho periódico foi disparado.
	lastResizeRedrawTime float64

	// Cursor
	autoCursorEnable bool

	// JS function references for event listener cleanup.
	// Stored as a map so we can removeEventListener on Stop().
	//
	// Português: Referências de funções JS para limpeza de event listeners.
	// Armazenadas como map para poder usar removeEventListener no Stop().
	listeners map[string]js.Func

	// Stage-level callbacks
	// Português: Callbacks de nível do stage
	onClickStage       func(PointerEvent)
	clickInterceptor   func(event PointerEvent) bool
	onDoubleClickStage func(PointerEvent)

	// pendingClick delays the single-click dispatch by the double-click
	// window, so a double-click cancels it instead of flashing the
	// single-click UI first (field 2026-07-23: the context menu blinked
	// under every double-click). Português: Atrasa o clique simples
	// pela janela do duplo — o duplo cancela o pendente em vez de
	// piscar a UI do simples antes.
	pendingClick       *time.Timer
	onPointerMoveStage func(PointerEvent)

	// Post-render callback
	// Português: Callback pós-renderização
	renderCallback func()

	// Camera — infinite canvas support (pan + zoom).
	// When nil, the Stage behaves exactly as before (no pan/zoom, backward compatible).
	//
	// Português: Camera — suporte a canvas infinito (pan + zoom).
	// Quando nil, o Stage se comporta exatamente como antes (sem pan/zoom, retrocompatível).
	camera *Camera

	// Second pointer tracking for pinch-zoom (multi-touch).
	// Português: Rastreamento do segundo ponteiro para pinch-zoom (multi-touch).
	secondPointerID int

	resizeObserver   js.Value
	resizeObserverCb js.Func
}

// Compile-time check that *stage satisfies the Stage interface.
// Português: Verificação em tempo de compilação de que *stage satisfaz a interface Stage.
var _ Stage = (*stage)(nil)

// Compile-time check that *element satisfies the Element interface.
// Português: Verificação em tempo de compilação de que *element satisfaz a interface Element.
var _ Element = (*elementData)(nil)

// =====================================================================
//  Factory | Fábrica
// =====================================================================

// newStage
//
// English:
//
//	Internal constructor. Creates a stage struct with config defaults applied.
//
// Português:
//
//	Construtor interno. Cria uma struct stage com valores padrão de config aplicados.
func newStage(config StageConfig) (s *stage) {
	if config.DoubleClickInterval == 0 {
		config.DoubleClickInterval = rulesSprite.StageDoubleClickInterval
	}
	if config.DragThreshold == 0 {
		config.DragThreshold = rulesSprite.StageDragThreshold
	}
	if config.BackgroundColor == "" {
		config.BackgroundColor = rulesSprite.StageBackgroundColor
	}

	s = &stage{
		config:           config,
		canvas:           js.Undefined(),
		ctx:              js.Undefined(),
		elements:         make(map[string]*elementData),
		drawOrder:        make([]*elementData, 0),
		listeners:        make(map[string]js.Func),
		autoCursorEnable: true,
		interaction:      stateIdle,
	}
	return
}

// =====================================================================
//  Element Management | Gerenciamento de Elementos
// =====================================================================

// CreateElement
//
// English:
//
//	Creates a new Element with the given configuration, adds it to the Stage,
//	and returns it. If config.SvgXml is provided, the SVG will be cached immediately.
//
// Português:
//
//	Cria um novo Element com a configuração fornecida, adiciona-o ao Stage e o retorna.
//	Se config.SvgXml for fornecido, o SVG será cacheado imediatamente.
func (e *stage) CreateElement(config ElementConfig) (element Element, err error) {
	elem := newElement(config)
	elem.stage = e

	if _, exists := e.elements[elem.id]; exists {
		err = ErrElementAlreadyExists
		return
	}

	e.elements[elem.id] = elem
	e.drawOrder = append(e.drawOrder, elem)
	e.sortDirty = true
	e.dirty = true

	// Cache SVG if provided.
	// Português: Cacheia o SVG se fornecido.
	if config.SvgXml != "" {
		err = elem.CacheFromSvg(config.SvgXml)
		if err != nil {
			// Roll back: remove the element if caching failed.
			// Português: Desfaz: remove o elemento se o cache falhou.
			delete(e.elements, elem.id)
			e.removeFromDrawOrder(elem)
			return
		}
	}

	element = elem
	return
}

// AddElement
//
// English:
//
//	Adds an externally created Element to the Stage.
//
// Português:
//
//	Adiciona um Element criado externamente ao Stage.
func (e *stage) AddElement(element Element) (err error) {
	elem, ok := element.(*elementData)
	if !ok {
		err = ErrElementNotFound
		return
	}

	if _, exists := e.elements[elem.id]; exists {
		err = ErrElementAlreadyExists
		return
	}

	elem.stage = e
	e.elements[elem.id] = elem
	e.drawOrder = append(e.drawOrder, elem)
	e.sortDirty = true
	e.dirty = true
	return
}

// RemoveElement
//
// English:
//
//	Removes the element with the given ID from the Stage without destroying it.
//
// Português:
//
//	Remove o elemento com o ID fornecido do Stage sem destruí-lo.
func (e *stage) RemoveElement(id string) (err error) {
	elem, exists := e.elements[id]
	if !exists {
		err = ErrElementNotFound
		return
	}

	elem.stage = nil
	delete(e.elements, id)
	e.removeFromDrawOrder(elem)
	e.dirty = true

	// Clear any interaction state referencing this element.
	// Português: Limpa qualquer estado de interação que referencie este elemento.
	if e.activeElement == elem {
		e.interaction = stateIdle
		e.activeElement = nil
	}
	if e.hoveredElement == elem {
		e.hoveredElement = nil
	}
	if e.lastClickElement == elem {
		e.lastClickElement = nil
	}
	return
}

// GetElement
//
// English:
//
//	Returns the element with the given ID, or nil and false if not found.
//
// Português:
//
//	Retorna o elemento com o ID fornecido, ou nil e false se não encontrado.
func (e *stage) GetElement(id string) (element Element, found bool) {
	elem, found := e.elements[id]
	if found {
		element = elem
	}
	return
}

// GetElements
//
// English:
//
//	Returns all elements currently on the Stage, sorted by z-index.
//
// Português:
//
//	Retorna todos os elementos atualmente no Stage, ordenados por z-index.
func (e *stage) GetElements() (elements []Element) {
	e.ensureSorted()
	elements = make([]Element, len(e.drawOrder))
	for i, elem := range e.drawOrder {
		elements[i] = elem
	}
	return
}

// GetElementAt
//
// English:
//
//	Returns the topmost visible element at the given canvas coordinates.
//	Iterates from highest z-index to lowest for correct hit precedence.
//
// Português:
//
//	Retorna o elemento visível mais acima nas coordenadas do canvas fornecidas.
//	Itera do maior z-index para o menor para precedência correta de hit.
func (e *stage) GetElementAt(canvasX float64, canvasY float64) (element Element, found bool) {
	// Screen-space elements use screen coords; world-space elements use world coords.
	// Português: Elementos screen-space usam coordenadas de tela; world-space usam mundo.
	worldX, worldY := e.canvasCoordsToWorld(canvasX, canvasY)
	elem := e.findElementAt(canvasX, canvasY, worldX, worldY)
	if elem != nil {
		element = elem
		found = true
	}
	return
}

// GetElementCount
//
// English:
//
//	Returns the total number of elements on the Stage.
//
// Português:
//
//	Retorna o número total de elementos no Stage.
func (e *stage) GetElementCount() (count int) {
	count = len(e.elements)
	return
}

// GetHighestIndex
//
// English:
//
//	Returns the highest z-index currently in use. Returns -1 if empty.
//
// Português:
//
//	Retorna o maior z-index atualmente em uso. Retorna -1 se vazio.
func (e *stage) GetHighestIndex() (index int) {
	if len(e.drawOrder) == 0 {
		index = -1
		return
	}
	e.ensureSorted()
	index = e.drawOrder[len(e.drawOrder)-1].index
	return
}

// GetLowestIndex
//
// English:
//
//	Returns the lowest z-index currently in use. Returns -1 if empty.
//
// Português:
//
//	Retorna o menor z-index atualmente em uso. Retorna -1 se vazio.
func (e *stage) GetLowestIndex() (index int) {
	if len(e.drawOrder) == 0 {
		index = -1
		return
	}
	e.ensureSorted()
	index = e.drawOrder[0].index
	return
}

// =====================================================================
//  Canvas | Canvas
// =====================================================================

// GetCanvasSize
//
// English:
//
//	Returns the current width and height of the canvas in pixels.
//
// Português:
//
//	Retorna a largura e altura atuais do canvas em pixels.
func (e *stage) GetCanvasSize() (width int, height int) {
	width = e.canvasWidth
	height = e.canvasHeight
	return
}

// SetCanvasSize
//
// English:
//
//	Resizes the canvas to the given dimensions. Marks the stage as dirty.
//
// Português:
//
//	Redimensiona o canvas para as dimensões fornecidas. Marca o stage como dirty.
func (e *stage) SetCanvasSize(width int, height int) {
	e.canvasWidth = width
	e.canvasHeight = height

	if !e.canvas.IsUndefined() {
		e.canvas.Set("width", width)
		e.canvas.Set("height", height)
	}

	e.dirty = true
}

// SetBackgroundColor
//
// English:
//
//	Sets the CSS color used to clear the canvas before each render.
//
// Português:
//
//	Define a cor CSS usada para limpar o canvas antes de cada renderização.
func (e *stage) SetBackgroundColor(color string) {
	e.config.BackgroundColor = color
	e.dirty = true
}

// =====================================================================
//  Rendering | Renderização
// =====================================================================

// Render
//
// English:
//
//	Forces an immediate full render of the canvas.
//
// Português:
//
//	Força uma renderização completa imediata do canvas.
func (e *stage) Render() {
	e.renderInternal()
}

// MarkDirty
//
// English:
//
//	Marks the stage as needing a re-render on the next animation frame.
//
// Português:
//
//	Marca o stage como precisando de re-renderização no próximo frame de animação.
func (e *stage) MarkDirty() {
	e.dirty = true
}

// SetRenderCallback
//
// English:
//
//	Registers a callback invoked after each render cycle.
//
// Português:
//
//	Registra um callback invocado após cada ciclo de renderização.
func (e *stage) SetRenderCallback(fn func()) {
	e.renderCallback = fn
}

// renderInternal
//
// English:
//
//	Performs the actual canvas rendering: clear, sort, draw all visible elements.
//
//	When a Camera is active, delegates to renderWithCamera which applies the
//	camera transform (pan + zoom) and draws grid/origin/info overlays.
//	When no Camera is set, draws elements directly (backward compatible).
//
//	Elements are drawn in ascending z-index order so higher-index elements appear
//	on top. Using clearRect for transparent backgrounds instead of fillRect saves
//	a fill operation. For opaque backgrounds, fillRect is used which also implicitly
//	clears the canvas.
//
// Português:
//
//	Realiza a renderização real do canvas: limpar, ordenar, desenhar todos os
//	elementos visíveis.
//
//	Quando uma Camera está ativa, delega para renderWithCamera que aplica a
//	transformação da câmera (pan + zoom) e desenha overlays de grid/origem/info.
//	Quando nenhuma Camera está definida, desenha elementos diretamente (retrocompatível).
func (e *stage) renderInternal() {
	if e.ctx.IsUndefined() {
		return
	}

	// Clear the canvas.
	// Português: Limpa o canvas.
	if e.config.BackgroundColor == "transparent" {
		e.ctx.Call("clearRect", 0, 0, e.canvasWidth, e.canvasHeight)
	} else {
		e.ctx.Set("fillStyle", e.config.BackgroundColor)
		e.ctx.Call("fillRect", 0, 0, e.canvasWidth, e.canvasHeight)
	}

	// Camera-aware rendering pipeline.
	// Português: Pipeline de renderização com suporte à câmera.
	if e.camera != nil {
		e.renderWithCamera()
	} else {
		// Original pipeline (no camera) — backward compatible.
		// Português: Pipeline original (sem câmera) — retrocompatível.
		e.ensureSorted()

		// Draw elements from lowest index to highest (painter's algorithm).
		// Português: Desenha elementos do menor index para o maior (algoritmo do pintor).
		for _, elem := range e.drawOrder {
			if elem.visible && elem.isCachedVal {
				elem.Draw(e.ctx)
			}
		}

		// Render callback (wires, overlays) — no camera, draw after all elements.
		// Português: Callback de renderização (wires, overlays) — sem câmera, após todos os elementos.
		if e.renderCallback != nil {
			e.renderCallback()
		}
	}

	e.dirty = false
}

// startRenderLoop
//
// English:
//
//	Initiates the requestAnimationFrame loop. The render function only calls
//	renderInternal when the dirty flag is set, avoiding unnecessary redraws.
//	The loop self-schedules via requestAnimationFrame for smooth 60fps pacing.
//
//	When a Camera is present and animating, the dirty flag is kept true so the
//	animation continues smoothly until completion.
//
// Português:
//
//	Inicia o loop requestAnimationFrame. A função de render só chama renderInternal
//	quando o flag dirty está setado, evitando redesenhos desnecessários.
//	O loop se auto-agenda via requestAnimationFrame para pacing suave de 60fps.
//
//	Quando uma Camera está presente e animando, o flag dirty é mantido true para
//	que a animação continue suavemente até completar.
func (e *stage) startRenderLoop() {
	e.renderFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if !e.running {
			return nil
		}

		// Keep dirty during camera animation so frames continue rendering.
		// Português: Mantém dirty durante animação da câmera para que os frames continuem renderizando.
		if e.camera != nil && e.camera.IsAnimating() {
			e.dirty = true
		}

		if e.dirty {
			e.renderInternal()
		}

		// Schedule next frame.
		// Português: Agenda próximo frame.
		js.Global().Call("requestAnimationFrame", e.renderFunc)
		return nil
	})

	js.Global().Call("requestAnimationFrame", e.renderFunc)
}

// =====================================================================
//  Stage-Level Events | Eventos do Stage
// =====================================================================

// SetOnClickStage
//
// English:
//
//	Registers a callback invoked when the canvas is clicked but no element is hit.
//
// Português:
//
//	Registra um callback invocado quando o canvas é clicado mas nenhum elemento é atingido.
//
// SetClickInterceptor installs a hook that runs BEFORE element click
// routing (single click only). Returning true consumes the click: neither
// the element under the pointer nor the stage handler fires. The workspace
// uses it to give wires an "invisible thicker layer" with priority over
// container bodies.
// Português: Instala um gancho que roda ANTES do roteamento de clique por
// element (só clique simples). Retornar true consome o clique: nem o
// element sob o ponteiro nem o handler do stage disparam. O workspace usa
// para dar aos wires uma "camada invisível mais grossa" com prioridade
// sobre corpos de container.
func (e *stage) SetClickInterceptor(fn func(event PointerEvent) bool) {
	e.clickInterceptor = fn
}

func (e *stage) SetOnClickStage(fn func(event PointerEvent)) {
	e.onClickStage = fn
}

// SetOnDoubleClickStage
//
// English:
//
//	Registers a callback invoked when the canvas is double-clicked but no element is hit.
//
// Português:
//
//	Registra um callback invocado quando o canvas recebe double-click mas nenhum
//	elemento é atingido.
func (e *stage) SetOnDoubleClickStage(fn func(event PointerEvent)) {
	e.onDoubleClickStage = fn
}

// SetOnPointerMoveStage
//
// English:
//
//	Registers a callback invoked on every pointer movement over the canvas.
//
// Português:
//
//	Registra um callback invocado a cada movimento do ponteiro sobre o canvas.
func (e *stage) SetOnPointerMoveStage(fn func(event PointerEvent)) {
	e.onPointerMoveStage = fn
}

// =====================================================================
//  Cursor | Cursor
// =====================================================================

// SetCursor
//
// English:
//
//	Manually sets the CSS cursor style on the canvas element.
//
// Português:
//
//	Define manualmente o estilo de cursor CSS no elemento canvas.
func (e *stage) SetCursor(cursor CursorStyle) {
	if !e.canvas.IsUndefined() {
		e.canvas.Get("style").Set("cursor", string(cursor))
	}
}

// SetAutoCursorEnable
//
// English:
//
//	Enables or disables automatic cursor style changes.
//
// Português:
//
//	Habilita ou desabilita mudanças automáticas de estilo de cursor.
func (e *stage) SetAutoCursorEnable(enable bool) {
	e.autoCursorEnable = enable
}

// updateCursor
//
// English:
//
//	Updates the canvas cursor based on the current interaction state and hovered element.
//	Only runs when autoCursorEnable is true.
//
//	When a Camera is active, converts screen coordinates to world coordinates
//	before hit-testing elements.
//
// Português:
//
//	Atualiza o cursor do canvas baseado no estado de interação atual e elemento sob hover.
//	Só executa quando autoCursorEnable é true.
//
//	Quando uma Camera está ativa, converte coordenadas de tela para coordenadas mundo
//	antes do hit-testing de elementos.
func (e *stage) updateCursor(canvasX float64, canvasY float64) {
	if !e.autoCursorEnable {
		return
	}

	// During active interactions, the cursor is fixed.
	// Português: Durante interações ativas, o cursor é fixo.
	switch e.interaction {
	case stateDragging:
		e.SetCursor(CursorGrabbing)
		return
	case stateResizing:
		e.SetCursor(cursorForResizeHandle(e.activeHandle))
		return
	case statePinching:
		return // no cursor change during pinch
	}

	// Convert to world coordinates for hit-testing.
	// Português: Converte para coordenadas mundo para hit-testing.
	worldX, worldY := e.canvasCoordsToWorld(canvasX, canvasY)

	elem := e.findElementAt(canvasX, canvasY, worldX, worldY)
	if elem == nil {
		// Empty stage area. If the user enabled both "pan empty
		// area" and "show grab cursor" in Editor Settings → Stage,
		// hint that left-drag will pan by painting a grab cursor.
		// Otherwise keep the default cursor — announcing an action
		// that is disabled would be misleading.
		//
		// Português: Área vazia da stage. Se o usuário ligou ambas
		// as prefs em Editor Settings → Stage, pinta o cursor grab
		// como dica de que left-drag faz pan. Caso contrário,
		// cursor padrão — não anuncia ação desabilitada.
		if rulesSprite.CameraPanEmptyArea && rulesSprite.CameraShowGrabCursor {
			e.SetCursor(CursorGrab)
		} else {
			e.SetCursor(CursorDefault)
		}
		return
	}

	// Determine the coordinates for hit-testing based on element space.
	// Português: Determina as coordenadas para hit-testing baseado no espaço do elemento.
	var hitX, hitY float64
	if elem.screenSpace {
		hitX, hitY = canvasX, canvasY
	} else {
		hitX, hitY = worldX, worldY
	}

	// Resize handles take priority over everything.
	// Português: Alças de redimensionamento têm prioridade sobre tudo.
	if elem.resizeEnable {
		handle := elem.HitTestResizeHandle(hitX, hitY)
		if handle != ResizeHandleNone {
			e.SetCursor(cursorForResizeHandle(handle))
			return
		}
	}

	if elem.dragEnable {
		e.SetCursor(CursorGrab)
		return
	}

	// Custom cursor hit-test — allows per-pixel cursor control for complex elements
	// that have multiple interactive zones (e.g., a stop button in one corner).
	// When set, this overrides the automatic onClick/onDoubleClick cursor logic.
	//
	// Português: Hit-test customizado de cursor — permite controle por pixel para
	// elementos complexos com múltiplas zonas interativas. Quando definido, sobrescreve
	// a lógica automática de cursor para onClick/onDoubleClick.
	if elem.cursorHitTest != nil {
		localX := hitX - elem.x
		localY := hitY - elem.y
		cursor := elem.cursorHitTest(localX, localY)
		if cursor != "" {
			e.SetCursor(cursor)
			return
		}
		e.SetCursor(CursorDefault)
		return
	}

	// Automatic cursor for clickable elements (only when cursorHitTest is not set).
	// Português: Cursor automático para elementos clicáveis (apenas quando cursorHitTest não está definido).
	if elem.onClick != nil || elem.onDoubleClick != nil {
		e.SetCursor(CursorPointer)
		return
	}

	e.SetCursor(CursorDefault)
}

// =====================================================================
//  Lifecycle | Ciclo de Vida
// =====================================================================

// Start
//
// English:
//
//	Starts the render loop and attaches event listeners to the canvas.
//	Uses Pointer Events API which unifies mouse and touch into a single interface.
//	This is significantly cleaner than maintaining separate mouse/touch handlers
//	and is supported by all modern browsers.
//
//	touch-action: none is set on the canvas to prevent the browser from intercepting
//	touch gestures (scroll, zoom) which would interfere with drag/resize interactions.
//
// Português:
//
//	Inicia o loop de renderização e anexa event listeners ao canvas.
//	Usa a API Pointer Events que unifica mouse e touch em uma única interface.
//	Isso é significativamente mais limpo do que manter handlers separados para
//	mouse/touch e é suportado por todos os navegadores modernos.
//
//	touch-action: none é definido no canvas para evitar que o navegador intercepte
//	gestos de touch (scroll, zoom) que interfeririam com interações de drag/resize.
func (e *stage) Start() (err error) {
	if e.running {
		err = ErrStageAlreadyRunning
		return
	}

	doc := js.Global().Get("document")
	e.canvas = doc.Call("getElementById", e.config.CanvasID)
	if e.canvas.IsNull() || e.canvas.IsUndefined() {
		err = ErrCanvasNotFound
		return
	}

	// Get 2D rendering context.
	// Português: Obtém o contexto de renderização 2D.
	e.ctx = e.canvas.Call("getContext", "2d")
	if e.ctx.IsNull() || e.ctx.IsUndefined() {
		err = ErrCanvasContextFailed
		return
	}

	// Apply canvas dimensions.
	// Português: Aplica as dimensões do canvas.
	if e.config.Width > 0 {
		e.canvasWidth = e.config.Width
		e.canvas.Set("width", e.config.Width)
	} else {
		e.canvasWidth = e.canvas.Get("width").Int()
	}

	if e.config.Height > 0 {
		e.canvasHeight = e.config.Height
		e.canvas.Set("height", e.config.Height)
	} else {
		e.canvasHeight = e.canvas.Get("height").Int()
	}

	// Prevent browser touch gestures from interfering with our interactions.
	// Português: Previne gestos de touch do navegador de interferir com nossas interações.
	e.canvas.Get("style").Set("touchAction", "none")

	// Attach event listeners.
	// Português: Anexa event listeners.
	e.attachListeners()

	e.running = true
	e.dirty = true

	// Observe canvas container for size changes.
	// Português: Observa o container do canvas para mudanças de tamanho.
	e.attachResizeObserver()

	// Start the render loop.
	// Português: Inicia o loop de renderização.
	e.startRenderLoop()

	return
}

// attachResizeObserver attaches a ResizeObserver to the canvas parent element
// so the canvas pixel dimensions stay in sync with CSS layout.
//
// Português: Anexa um ResizeObserver ao elemento pai do canvas para manter
// as dimensões do canvas em sincronia com o layout CSS.
func (e *stage) attachResizeObserver() {
	d := rulesDensity.GetDensity()

	cb := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		entries := args[0]
		if entries.Length() == 0 {
			return nil
		}

		entry := entries.Index(0)
		cr := entry.Get("contentRect")
		cssW := cr.Get("width").Float()
		cssH := cr.Get("height").Float()

		newW := int(cssW * d)
		newH := int(cssH * d)

		if newW != e.canvasWidth || newH != e.canvasHeight {
			e.SetCanvasSize(newW, newH)
		}

		return nil
	})

	observer := js.Global().Get("ResizeObserver").New(cb)
	observer.Call("observe", e.canvas.Get("parentElement"))

	e.resizeObserverCb = cb
	e.resizeObserver = observer
}

// detachResizeObserver cleans up the observer.
// Português: Limpa o observer.
func (e *stage) detachResizeObserver() {
	if !e.resizeObserver.IsUndefined() {
		e.resizeObserver.Call("disconnect")
		e.resizeObserverCb.Release()
	}
}

// Stop
//
// English:
//
//	Stops the render loop and removes all DOM event listeners.
//
// Português:
//
//	Para o loop de renderização e remove todos os event listeners do DOM.
func (e *stage) Stop() {
	if !e.running {
		return
	}

	e.running = false

	// Release the render function.
	// Português: Libera a função de render.
	e.renderFunc.Release()

	// Remove and release all event listeners.
	// Português: Remove e libera todos os event listeners.
	e.detachListeners()
}

// Destroy
//
// English:
//
//	Stops the Stage, destroys all elements, and releases all resources.
//
// Português:
//
//	Para o Stage, destrói todos os elementos e libera todos os recursos.
func (e *stage) Destroy() {
	if e.running {
		e.Stop()
	}

	for _, elem := range e.elements {
		elem.Destroy()
	}

	e.elements = nil
	e.drawOrder = nil
	e.destroyed = true
	e.canvas = js.Undefined()
	e.ctx = js.Undefined()
	e.camera = nil
}

// IsRunning
//
// English:
//
//	Returns whether the Stage render loop is currently active.
//
// Português:
//
//	Retorna se o loop de renderização do Stage está atualmente ativo.
func (e *stage) IsRunning() (running bool) {
	running = e.running
	return
}

// =====================================================================
//  Private Helpers | Helpers Privados
// =====================================================================

// markSortDirty
//
// English:
//
//	Marks the draw order as needing re-sorting.
//
// Português:
//
//	Marca a ordem de desenho como precisando reordenação.
func (e *stage) markSortDirty() {
	e.sortDirty = true
}

// ensureSorted
//
// English:
//
//	Sorts the draw order slice by z-index if it has been marked dirty.
//	Uses Go's sort.Slice which is efficient for mostly-sorted data (common case
//	when only one element's index changed).
//
// Português:
//
//	Ordena o slice de ordem de desenho por z-index se foi marcado como dirty.
//	Usa sort.Slice do Go que é eficiente para dados quase ordenados (caso comum
//	quando apenas um elemento teve o index alterado).
func (e *stage) ensureSorted() {
	if !e.sortDirty {
		return
	}
	sort.Slice(e.drawOrder, func(i int, j int) bool {
		return e.drawOrder[i].index < e.drawOrder[j].index
	})
	e.sortDirty = false
}

// removeFromDrawOrder
//
// English:
//
//	Removes the given element from the draw order slice.
//
// Português:
//
//	Remove o elemento fornecido do slice de ordem de desenho.
func (e *stage) removeFromDrawOrder(elem *elementData) {
	for i, el := range e.drawOrder {
		if el == elem {
			e.drawOrder = append(e.drawOrder[:i], e.drawOrder[i+1:]...)
			return
		}
	}
}

// findElementAt
//
// English:
//
//	Finds the topmost visible element at the given world coordinates.
//	Iterates draw order in reverse (highest z-index first) for correct hit precedence.
//
//	Note: callers must convert screen coordinates to world coordinates before
//	calling this method when a Camera is active.
//
// Português:
//
//	Encontra o elemento visível mais acima nas coordenadas mundo fornecidas.
//	Itera a ordem de desenho em reverso (maior z-index primeiro) para precedência
//	correta de hit.
//
//	Nota: chamadores devem converter coordenadas de tela para coordenadas mundo
//	antes de chamar este método quando uma Camera está ativa.
func (e *stage) findElementAt(screenX, screenY, worldX, worldY float64) (found *elementData) {
	e.ensureSorted()

	// Reverse iteration: last element in drawOrder has highest z-index.
	// Screen-space elements are tested against screen coordinates;
	// world-space elements are tested against world coordinates.
	//
	// Português: Iteração reversa: último elemento no drawOrder tem o maior z-index.
	// Elementos screen-space são testados contra coordenadas de tela;
	// elementos world-space são testados contra coordenadas mundo.
	for i := len(e.drawOrder) - 1; i >= 0; i-- {
		elem := e.drawOrder[i]
		if !elem.visible || !elem.isCachedVal || elem.pointerEventsIgnored {
			continue
		}
		if elem.screenSpace {
			if elem.HitTest(screenX, screenY) {
				found = elem
				return
			}
		} else {
			if elem.HitTest(worldX, worldY) {
				found = elem
				return
			}
		}
	}
	return
}
