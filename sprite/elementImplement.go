// sprite/elementImplement.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

import (
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesSprite"
)

// element is the concrete implementation of the Element interface.
type elementData struct {
	// Identity
	// Português: Identidade
	id        string
	destroyed bool

	// Geometry
	// Português: Geometria
	x      float64
	y      float64
	width  float64
	height float64
	index  int

	// Display
	// Português: Exibição
	visible bool
	opacity float64

	// Drag state
	// Português: Estado de arraste
	dragEnable bool
	dragBounds *Rect

	// Resize state
	// Português: Estado de redimensionamento
	resizeEnable     bool
	keepAspectRatio  bool
	resizeHandleSize float64
	minWidth         float64
	minHeight        float64
	maxWidth         float64
	maxHeight        float64

	// Resize buttons — visual handles drawn around the element.
	// Indexed using allResizeHandles order (TopLeft=0, Top=1, ..., Left=7).
	//
	// Português: Botões de resize — alças visuais desenhadas ao redor do elemento.
	// Indexados usando a ordem allResizeHandles (TopLeft=0, Top=1, ..., Left=7).
	resizeRenderer      ResizeHandleRenderer
	resizeHandles       [resizeHandleCount]resizeHandleButton
	resizeButtonVisible bool
	resizeHandleOffset  float64

	// SVG cache
	// Português: Cache do SVG
	svgXml      string
	cachedImage js.Value
	isCachedVal bool

	// Event callbacks
	// Português: Callbacks de evento
	onClick        func(PointerEvent)
	onDoubleClick  func(PointerEvent)
	onPointerEnter func(PointerEvent)
	onPointerLeave func(PointerEvent)
	onDragStart    func(DragEvent)
	onDragMove     func(DragEvent)
	onDragEnd      func(DragEvent)
	onResizeStart  func(ResizeEvent)
	onResizeMove   func(ResizeEvent)
	onResizeEnd    func(ResizeEvent)

	// Periodic resize redraw — allows the user to re-cache the element's visual
	// content at regular intervals during an active resize, preventing the
	// stretched/distorted appearance that occurs when a bitmap is scaled without
	// re-rendering.
	//
	// Português: Redesenho periódico durante resize — permite ao usuário re-cachear
	// o conteúdo visual do elemento em intervalos regulares durante um resize ativo,
	// prevenindo a aparência esticada/distorcida que ocorre quando um bitmap é escalado
	// sem re-renderizar.
	resizeRedrawInterval int64             // milliseconds, 0 = disabled
	onResizeRedraw       func(ResizeEvent) // called periodically during resize

	// Cursor hit-test — allows per-pixel cursor control for complex elements
	// that have multiple interactive zones (e.g., a stop button in one corner).
	// When set, updateCursor() calls this function with local coordinates (relative
	// to element top-left) instead of using the automatic cursor logic.
	// Return "" to use the default cursor.
	//
	// Português: Hit-test de cursor — permite controle de cursor por pixel para
	// elementos complexos que têm múltiplas zonas interativas (ex: um botão stop
	// em um canto). Quando definido, updateCursor() chama esta função com coordenadas
	// locais (relativas ao canto superior esquerdo do elemento) em vez de usar a
	// lógica automática de cursor. Retorne "" para usar o cursor padrão.
	cursorHitTest func(localX float64, localY float64) CursorStyle

	// Custom data
	// Português: Dados customizados
	data map[string]interface{}

	// Back-reference to the owning stage, set when added to a stage.
	// Using a concrete type pointer instead of the Stage interface to avoid
	// circular dependency and to allow direct access to internal methods like
	// markSortDirty().
	//
	// Português: Referência reversa ao stage proprietário, definida ao ser adicionado a um stage.
	// Usando ponteiro de tipo concreto ao invés da interface Stage para evitar
	// dependência circular e permitir acesso direto a métodos internos como markSortDirty().
	stage *stage

	// Screen space: element ignores camera transform, stays fixed on screen.
	// Português: Espaço de tela: elemento ignora transformação da câmera, fica fixo na tela.
	screenSpace bool

	// pointerEventsIgnored, when true, makes the stage's event dispatcher
	// skip this element during hit-testing. The element is still drawn
	// but doesn't intercept clicks, drags, or hovers. Used for purely
	// visual overlays (warning marks, highlights, child-bounds
	// indicators during resize).
	//
	// Português: Quando true, faz o dispatcher do stage pular este
	// elemento no hit-testing. O elemento ainda é desenhado, mas não
	// intercepta cliques, drags ou hovers. Para overlays visuais puros.
	pointerEventsIgnored bool
}

// newElement
//
// English:
//
//	Creates a new element from the given configuration with sensible defaults applied.
//
// Português:
//
//	Cria um novo element a partir da configuração fornecida com valores padrão aplicados.
func newElement(config ElementConfig) (elem *elementData) {
	id := config.ID
	if id == "" {
		id = generateID()
	}

	resizeHandleSize := config.ResizeHandleSize
	if resizeHandleSize == 0 {
		resizeHandleSize = rulesSprite.ElementResizeHandleSize
	}

	minW := config.MinWidth
	if minW == 0 {
		minW = rulesSprite.ElementMinWidth
	}

	minH := config.MinHeight
	if minH == 0 {
		minH = rulesSprite.ElementMinHeight
	}

	// Note: Go's bool zero value is false, but we want elements visible by default.
	// Since we can't distinguish "user explicitly set false" from "unset", we always
	// default to true. Users who need an initially hidden element should call
	// SetVisible(false) after creation. This is documented in ElementConfig.
	//
	// Português: O valor zero de bool no Go é false, mas queremos elementos visíveis
	// por padrão. Como não podemos distinguir "usuário definiu false explicitamente"
	// de "não definido", sempre definimos como true. Usuários que precisam de um
	// elemento inicialmente oculto devem chamar SetVisible(false) após a criação.
	visible := true

	elem = &elementData{
		id:      id,
		x:       config.X,
		y:       config.Y,
		width:   config.Width,
		height:  config.Height,
		index:   config.Index,
		visible: visible,
		opacity: rulesSprite.ElementDefaultOpacity,

		dragEnable: config.DragEnable,

		resizeEnable:     config.ResizeEnable,
		resizeHandleSize: resizeHandleSize,
		minWidth:         minW,
		minHeight:        minH,

		svgXml:      config.SvgXml,
		cachedImage: js.Undefined(),

		pointerEventsIgnored: config.PointerEventsIgnored,

		data: make(map[string]interface{}),
	}
	return
}

// =====================================
//  Identification | Identificação
// =====================================

// GetID
//
// English:
//
//	Returns the unique identifier of this element.
//
// Português:
//
//	Retorna o identificador único deste elemento.
func (e *elementData) GetID() (id string) {
	id = e.id
	return
}

// =====================================
//  Position | Posição
// =====================================

// SetPosition
//
// English:
//
//	Sets the element's top-left corner position on the canvas.
//	Marks the stage as dirty so it will re-render on the next animation frame.
//
// Português:
//
//	Define a posição do canto superior esquerdo do elemento no canvas.
//	Marca o stage como dirty para re-renderizar no próximo frame de animação.
func (e *elementData) SetPosition(x float64, y float64) { // todo: ajuste para density
	e.x = x
	e.y = y
	e.markDirty()
}

// GetPosition
//
// English:
//
//	Returns the current position of the element's top-left corner.
//
// Português:
//
//	Retorna a posição atual do canto superior esquerdo do elemento.
func (e *elementData) GetPosition() (x float64, y float64) {
	x = e.x
	y = e.y
	return
}

// =====================================
//  Size | Tamanho
// =====================================

// SetSize
//
// English:
//
//	Sets the display size of the element. The cached image will be scaled to
//	this size when drawn. Marks the stage as dirty.
//
// Português:
//
//	Define o tamanho de exibição do elemento. A imagem cacheada será escalada
//	para este tamanho ao ser desenhada. Marca o stage como dirty.
func (e *elementData) SetSize(width float64, height float64) {
	e.width = width
	e.height = height
	e.markDirty()
}

// GetSize
//
// English:
//
//	Returns the current display size of the element.
//
// Português:
//
//	Retorna o tamanho de exibição atual do elemento.
func (e *elementData) GetSize() (width float64, height float64) {
	width = e.width
	height = e.height
	return
}

// GetBounds
//
// English:
//
//	Returns the bounding rectangle of the element (position + size).
//
// Português:
//
//	Retorna o retângulo delimitador do elemento (posição + tamanho).
func (e *elementData) GetBounds() (bounds Rect) {
	bounds = Rect{
		X:      e.x,
		Y:      e.y,
		Width:  e.width,
		Height: e.height,
	}
	return
}

// =====================================
//  Z-Index | Índice de Camada
// =====================================

// SetIndex
//
// English:
//
//	Sets the z-index of the element. Higher values are drawn on top.
//	Marks the stage as dirty and triggers re-sorting of the draw order.
//
// Português:
//
//	Define o z-index do elemento. Valores maiores são desenhados por cima.
//	Marca o stage como dirty e dispara reordenação da ordem de desenho.
func (e *elementData) SetIndex(index int) {
	e.index = index
	if e.stage != nil {
		e.stage.markSortDirty()
	}
	e.markDirty()
}

// GetIndex
//
// English:
//
//	Returns the current z-index of the element.
//
// Português:
//
//	Retorna o z-index atual do elemento.
func (e *elementData) GetIndex() (index int) {
	index = e.index
	return
}

// MoveToFront
//
// English:
//
//	Sets this element's index to be one above the current highest index on the
//	stage, bringing it to the front of all other elements.
//
// Português:
//
//	Define o index deste elemento para um acima do maior index atual no stage,
//	trazendo-o para a frente de todos os outros elementos.
func (e *elementData) MoveToFront() {
	if e.stage == nil {
		return
	}
	highest := e.stage.GetHighestIndex()
	if e.index <= highest {
		e.SetIndex(highest + 1)
	}
}

// MoveToBack
//
// English:
//
//	Sets this element's index to be one below the current lowest index on the
//	stage, sending it behind all other elements.
//
// Português:
//
//	Define o index deste elemento para um abaixo do menor index atual no stage,
//	enviando-o para trás de todos os outros elementos.
func (e *elementData) MoveToBack() {
	if e.stage == nil {
		return
	}
	lowest := e.stage.GetLowestIndex()
	if e.index >= lowest {
		e.SetIndex(lowest - 1)
	}
}

// =====================================
//  Visibility | Visibilidade
// =====================================

// SetVisible
//
// English:
//
//	Shows or hides the element. Hidden elements do not receive events and are
//	not drawn. Marks the stage as dirty.
//
// Português:
//
//	Mostra ou oculta o elemento. Elementos ocultos não recebem eventos e não
//	são desenhados. Marca o stage como dirty.
func (e *elementData) SetVisible(visible bool) {
	e.visible = visible
	e.markDirty()
}

// IsVisible
//
// English:
//
//	Returns whether the element is currently visible.
//
// Português:
//
//	Retorna se o elemento está atualmente visível.
func (e *elementData) IsVisible() (visible bool) {
	visible = e.visible
	return
}

// =====================================
//  Opacity | Opacidade
// =====================================

// SetOpacity
//
// English:
//
//	Sets the opacity of the element (0.0 = fully transparent, 1.0 = fully opaque).
//	Marks the stage as dirty.
//
// Português:
//
//	Define a opacidade do elemento (0.0 = totalmente transparente, 1.0 = totalmente opaco).
//	Marca o stage como dirty.
func (e *elementData) SetOpacity(opacity float64) {
	e.opacity = clamp(opacity, 0.0, 1.0)
	e.markDirty()
}

// GetOpacity
//
// English:
//
//	Returns the current opacity of the element.
//
// Português:
//
//	Retorna a opacidade atual do elemento.
func (e *elementData) GetOpacity() (opacity float64) {
	opacity = e.opacity
	return
}

// =====================================
//  Drag | Arrastar
// =====================================

// SetDragEnable
//
// English:
//
//	Enables or disables drag interaction on this element.
//
// Português:
//
//	Habilita ou desabilita a interação de arraste neste elemento.
func (e *elementData) SetDragEnable(enable bool) {
	e.dragEnable = enable
}

// IsDragEnabled
//
// English:
//
//	Returns whether drag interaction is currently enabled.
//
// Português:
//
//	Retorna se a interação de arraste está atualmente habilitada.
func (e *elementData) IsDragEnabled() (enabled bool) {
	enabled = e.dragEnable
	return
}

// SetDragBounds
//
// English:
//
//	Sets a bounding rectangle that constrains where the element can be dragged.
//	Pass nil to remove the constraint and allow free dragging.
//
// Português:
//
//	Define um retângulo delimitador que restringe para onde o elemento pode ser
//	arrastado. Passe nil para remover a restrição e permitir arraste livre.
func (e *elementData) SetDragBounds(bounds *Rect) {
	if bounds == nil {
		e.dragBounds = nil
		return
	}
	// Copy to prevent external mutation.
	// Português: Copia para evitar mutação externa.
	copied := *bounds
	e.dragBounds = &copied
}

// GetDragBounds
//
// English:
//
//	Returns the current drag constraint bounds, or nil if unconstrained.
//
// Português:
//
//	Retorna os limites de restrição de arraste atuais, ou nil se sem restrição.
func (e *elementData) GetDragBounds() (bounds *Rect) {
	if e.dragBounds == nil {
		return nil
	}
	copied := *e.dragBounds
	bounds = &copied
	return
}

// =====================================
//  Resize | Redimensionar
// =====================================

// SetResizeEnable
//
// English:
//
//	Enables or disables resize interaction on this element.
//
// Português:
//
//	Habilita ou desabilita a interação de redimensionamento neste elemento.
func (e *elementData) SetResizeEnable(enable bool) {
	e.resizeEnable = enable
}

// IsResizeEnabled
//
// English:
//
//	Returns whether resize interaction is currently enabled.
//
// Português:
//
//	Retorna se a interação de redimensionamento está atualmente habilitada.
func (e *elementData) IsResizeEnabled() (enabled bool) {
	enabled = e.resizeEnable
	return
}

// SetKeepAspectRatio
//
// English:
//
//	When true, resizing from corner handles will preserve the element's aspect ratio.
//
// Português:
//
//	Quando true, redimensionar pelas alças de canto preservará a proporção do elemento.
func (e *elementData) SetKeepAspectRatio(keep bool) {
	e.keepAspectRatio = keep
}

// IsKeepAspectRatio
//
// English:
//
//	Returns whether aspect ratio is preserved during resize.
//
// Português:
//
//	Retorna se a proporção é preservada durante o redimensionamento.
func (e *elementData) IsKeepAspectRatio() (keep bool) {
	keep = e.keepAspectRatio
	return
}

// SetMinSize
//
// English:
//
//	Sets the minimum allowed size when resizing.
//
// Português:
//
//	Define o tamanho mínimo permitido ao redimensionar.
func (e *elementData) SetMinSize(width float64, height float64) {
	e.minWidth = width
	e.minHeight = height
}

// GetMinSize
//
// English:
//
//	Returns the minimum allowed size when resizing.
//
// Português:
//
//	Retorna o tamanho mínimo permitido ao redimensionar.
func (e *elementData) GetMinSize() (width float64, height float64) {
	width = e.minWidth
	height = e.minHeight
	return
}

// SetMaxSize
//
// English:
//
//	Sets the maximum allowed size when resizing.
//
// Português:
//
//	Define o tamanho máximo permitido ao redimensionar.
func (e *elementData) SetMaxSize(width float64, height float64) {
	e.maxWidth = width
	e.maxHeight = height
}

// GetMaxSize
//
// English:
//
//	Returns the maximum allowed size when resizing. Zero values mean no constraint.
//
// Português:
//
//	Retorna o tamanho máximo permitido ao redimensionar. Valores zero significam sem restrição.
func (e *elementData) GetMaxSize() (width float64, height float64) {
	width = e.maxWidth
	height = e.maxHeight
	return
}

// =====================================
//  Resize Buttons | Botões de Resize
// =====================================

// SetResizeButtons
//
// English:
//
//	Sets a ResizeHandleRenderer and caches 8 button images (one per handle).
//	Must be called from a goroutine because it blocks while loading each image.
//
//	The renderer's RenderHandle method returns a js.Value SVG DOM element for
//	each handle. The sprite package serializes it to XML via XMLSerializer,
//	caches it as a raster image via Blob URL, and discards the SVG DOM element.
//	The buttons are then drawn on the canvas around the element's edges.
//
//	Automatically enables resize and makes the buttons visible.
//	Pass nil to remove visual resize buttons.
//
// Português:
//
//	Define um ResizeHandleRenderer e cacheia 8 imagens de botão (uma por alça).
//	Deve ser chamado de uma goroutine porque bloqueia enquanto carrega cada imagem.
//
//	O método RenderHandle do renderer retorna um js.Value do elemento SVG DOM para
//	cada alça. O sprite package o serializa para XML via XMLSerializer, cacheia como
//	imagem raster via Blob URL, e descarta o elemento SVG DOM. Os botões são então
//	desenhados no canvas ao redor das bordas do elemento.
//
//	Automaticamente habilita resize e torna os botões visíveis.
//	Passe nil para remover os botões visuais de resize.
func (e *elementData) SetResizeButtons(renderer ResizeHandleRenderer) (err error) {
	if renderer == nil {
		// Remove buttons and clear cached images.
		// Português: Remove botões e limpa imagens cacheadas.
		e.resizeRenderer = nil
		e.resizeButtonVisible = false
		for i := range e.resizeHandles {
			e.resizeHandles[i] = resizeHandleButton{
				cachedImage: js.Undefined(),
			}
		}
		e.markDirty()
		return
	}

	e.resizeRenderer = renderer
	e.resizeHandleOffset = renderer.GetHandleOffset()

	// Cache each of the 8 handle buttons sequentially.
	// Sequential is simpler and the total load time is negligible since each SVG
	// is tiny and decoded locally (no network). Parallel loading would add complexity
	// with multiple channels for marginal gain.
	//
	// Português: Cacheia cada um dos 8 botões de alça sequencialmente.
	// Sequencial é mais simples e o tempo total de carga é desprezível já que cada SVG
	// é minúsculo e decodificado localmente (sem rede).
	for i, handle := range allResizeHandles {
		// Get the SVG DOM element from the renderer.
		// Português: Obtém o elemento SVG DOM do renderer.
		svgElement, handleWidth, handleHeight := renderer.RenderHandle(handle)

		// Serialize the SVG DOM element to XML string.
		// The serializer works on detached DOM nodes, so the SVG never needs to be
		// appended to the visible document.
		//
		// Português: Serializa o elemento SVG DOM para string XML.
		// O serializer funciona em nós DOM desanexados, então o SVG nunca precisa
		// ser adicionado ao documento visível.
		svgXml := svgElementToXml(svgElement)

		var img js.Value
		img, err = svgToImage(svgXml)
		if err != nil {
			// Clean up already-loaded images on failure.
			// Português: Limpa imagens já carregadas em caso de falha.
			for j := 0; j < i; j++ {
				e.resizeHandles[j] = resizeHandleButton{
					cachedImage: js.Undefined(),
				}
			}
			e.resizeRenderer = nil
			return
		}

		e.resizeHandles[i] = resizeHandleButton{
			handle:      handle,
			cachedImage: img,
			width:       handleWidth,
			height:      handleHeight,
			cached:      true,
		}
	}

	e.resizeButtonVisible = true

	// Automatically enable resize when buttons are set.
	// The user explicitly wants resize if they're setting visual buttons.
	//
	// Português: Automaticamente habilita resize quando botões são definidos.
	// O usuário explicitamente quer resize se está definindo botões visuais.
	e.resizeEnable = true

	e.markDirty()
	return
}

// ShowResizeButtons
//
// English:
//
//	Shows or hides the visual resize buttons.
//
// Português:
//
//	Mostra ou oculta os botões visuais de resize.
func (e *elementData) ShowResizeButtons(visible bool) {
	e.resizeButtonVisible = visible
	e.markDirty()
}

// AreResizeButtonsVisible
//
// English:
//
//	Returns whether the visual resize buttons are currently visible.
//
// Português:
//
//	Retorna se os botões visuais de resize estão atualmente visíveis.
func (e *elementData) AreResizeButtonsVisible() (visible bool) {
	visible = e.resizeButtonVisible && e.resizeRenderer != nil
	return
}

// =====================================
//  SVG Cache | Cache SVG
// =====================================

// CacheFromSvg
//
// English:
//
//	Renders the given SVG XML string into an HTML Image element via Blob URL and
//	caches the result for fast drawImage calls. This blocks the calling goroutine
//	until the image loads.
//
//	The Blob URL approach is used instead of a data URI because:
//	1. Blob URLs avoid the base64 encoding overhead (33% larger payload).
//	2. The browser can decode the SVG more efficiently from a Blob.
//	3. The URL is revoked immediately after load to free memory.
//
// Português:
//
//	Renderiza a string XML SVG fornecida em um elemento HTML Image via Blob URL e
//	cacheia o resultado para chamadas drawImage rápidas. Bloqueia a goroutine
//	chamadora até a imagem carregar.
//
//	A abordagem Blob URL é usada ao invés de data URI porque:
//	1. Blob URLs evitam o overhead de codificação base64 (payload 33% maior).
//	2. O navegador pode decodificar o SVG mais eficientemente de um Blob.
//	3. A URL é revogada imediatamente após o carregamento para liberar memória.
func (e *elementData) CacheFromSvg(svgXml string) (err error) {
	if svgXml == "" {
		err = ErrSvgEmpty
		return
	}

	e.svgXml = svgXml
	e.isCachedVal = false

	global := js.Global()

	// Create Blob from SVG XML.
	// Português: Cria Blob a partir do XML SVG.
	blobParts := global.Get("Array").New()
	blobParts.Call("push", svgXml)

	blobOpts := global.Get("Object").New()
	blobOpts.Set("type", "image/svg+xml;charset=utf-8")
	blob := global.Get("Blob").New(blobParts, blobOpts)

	// Create object URL from Blob.
	// Português: Cria URL de objeto a partir do Blob.
	blobURL := global.Get("URL").Call("createObjectURL", blob)

	err = e.loadImage(blobURL.String(), true)
	return
}

// CacheFromImageSrc
//
// English:
//
//	Loads an image from the given source URL and caches it as the element's visual content.
//
// Português:
//
//	Carrega uma imagem a partir da URL de origem fornecida e a cacheia como conteúdo visual.
func (e *elementData) CacheFromImageSrc(src string) (err error) {
	if src == "" {
		err = ErrImageLoadFailed
		return
	}

	e.isCachedVal = false

	// Do not revoke URL since it was provided externally.
	// Português: Não revoga a URL pois foi fornecida externamente.
	err = e.loadImage(src, false)
	return
}

// loadImage
//
// English:
//
//	Internal helper that loads an image from a URL and waits for it to complete.
//	Uses a channel to synchronize the async Image.onload callback with the Go goroutine.
//
//	In Go WASM, when a goroutine blocks on a channel receive, the Go runtime yields
//	control back to the JavaScript event loop. This allows the browser to process the
//	image load and fire the onload callback, which sends on the channel and wakes the
//	goroutine. This pattern is safe as long as the caller is in a goroutine (not inside
//	a js.FuncOf callback on the main thread).
//
// Português:
//
//	Helper interno que carrega uma imagem de uma URL e espera a conclusão.
//	Usa um channel para sincronizar o callback assíncrono Image.onload com a goroutine Go.
//
//	Em Go WASM, quando uma goroutine bloqueia em um channel receive, o runtime Go
//	devolve o controle ao event loop do JavaScript. Isso permite que o navegador
//	processe o carregamento da imagem e dispare o callback onload, que envia no channel
//	e acorda a goroutine.
func (e *elementData) loadImage(src string, revokeURL bool) (err error) {
	done := make(chan error, 1)

	global := js.Global()
	img := global.Get("document").Call("createElement", "img")

	var onLoad, onError js.Func

	onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		e.cachedImage = img
		e.isCachedVal = true

		// Use intrinsic dimensions if element size was not explicitly set.
		// Português: Usa dimensões intrínsecas se o tamanho do elemento não foi definido explicitamente.
		if e.width == 0 {
			e.width = img.Get("naturalWidth").Float()
		}
		if e.height == 0 {
			e.height = img.Get("naturalHeight").Float()
		}

		if revokeURL {
			global.Get("URL").Call("revokeObjectURL", src)
		}

		e.markDirty()

		// Release JS functions to prevent memory leak.
		// Português: Libera funções JS para prevenir vazamento de memória.
		onLoad.Release()
		onError.Release()

		done <- nil
		return nil
	})

	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if revokeURL {
			global.Get("URL").Call("revokeObjectURL", src)
		}

		onLoad.Release()
		onError.Release()

		done <- ErrSvgRenderFailed
		return nil
	})

	img.Set("onload", onLoad)
	img.Set("onerror", onError)
	img.Set("src", src)

	err = <-done
	return
}

// InvalidateCache
//
// English:
//
//	Forces the cached image to be re-rendered from the current SVG on the next
//	draw cycle.
//
// Português:
//
//	Força a imagem cacheada a ser re-renderizada a partir do SVG atual no próximo
//	ciclo de desenho.
func (e *elementData) InvalidateCache() {
	e.isCachedVal = false
	e.cachedImage = js.Undefined()
	e.markDirty()
}

// IsCached
//
// English:
//
//	Returns whether this element has a valid cached image ready for drawing.
//
// Português:
//
//	Retorna se este elemento possui uma imagem cacheada válida pronta para desenho.
func (e *elementData) IsCached() (cached bool) {
	cached = e.isCachedVal
	return
}

// =====================================
//  Event Callbacks | Callbacks de Evento
// =====================================

// SetOnClick
//
// English:
//
//	Registers a callback to be invoked when the element is clicked.
//
// Português:
//
//	Registra um callback a ser invocado quando o elemento é clicado.
func (e *elementData) SetOnClick(fn func(event PointerEvent)) {
	e.onClick = fn
}

// SetOnDoubleClick
//
// English:
//
//	Registers a callback to be invoked when the element is double-clicked.
//
// Português:
//
//	Registra um callback a ser invocado quando o elemento recebe double-click.
func (e *elementData) SetOnDoubleClick(fn func(event PointerEvent)) {
	e.onDoubleClick = fn
}

// SetOnPointerEnter
//
// English:
//
//	Registers a callback invoked when the pointer enters the element's bounds.
//
// Português:
//
//	Registra um callback invocado quando o ponteiro entra nos limites do elemento.
func (e *elementData) SetOnPointerEnter(fn func(event PointerEvent)) {
	e.onPointerEnter = fn
}

// SetOnPointerLeave
//
// English:
//
//	Registers a callback invoked when the pointer leaves the element's bounds.
//
// Português:
//
//	Registra um callback invocado quando o ponteiro sai dos limites do elemento.
func (e *elementData) SetOnPointerLeave(fn func(event PointerEvent)) {
	e.onPointerLeave = fn
}

// SetOnDragStart
//
// English:
//
//	Registers a callback invoked when a drag operation begins.
//
// Português:
//
//	Registra um callback invocado quando uma operação de arraste começa.
func (e *elementData) SetOnDragStart(fn func(event DragEvent)) {
	e.onDragStart = fn
}

// SetOnDragMove
//
// English:
//
//	Registers a callback invoked on each pointer movement during a drag.
//
// Português:
//
//	Registra um callback invocado a cada movimento do ponteiro durante um arraste.
func (e *elementData) SetOnDragMove(fn func(event DragEvent)) {
	e.onDragMove = fn
}

// SetOnDragEnd
//
// English:
//
//	Registers a callback invoked when a drag operation ends.
//
// Português:
//
//	Registra um callback invocado quando uma operação de arraste termina.
func (e *elementData) SetOnDragEnd(fn func(event DragEvent)) {
	e.onDragEnd = fn
}

// SetOnResizeStart
//
// English:
//
//	Registers a callback invoked when a resize operation begins.
//
// Português:
//
//	Registra um callback invocado quando uma operação de redimensionamento começa.
func (e *elementData) SetOnResizeStart(fn func(event ResizeEvent)) {
	e.onResizeStart = fn
}

// SetOnResizeMove
//
// English:
//
//	Registers a callback invoked on each pointer movement during a resize.
//
// Português:
//
//	Registra um callback invocado a cada movimento do ponteiro durante um redimensionamento.
func (e *elementData) SetOnResizeMove(fn func(event ResizeEvent)) {
	e.onResizeMove = fn
}

// SetOnResizeEnd
//
// English:
//
//	Registers a callback invoked when a resize operation ends.
//
// Português:
//
//	Registra um callback invocado quando uma operação de redimensionamento termina.
func (e *elementData) SetOnResizeEnd(fn func(event ResizeEvent)) {
	e.onResizeEnd = fn
}

// SetResizeRedrawInterval
//
// English:
//
//	Sets the interval in milliseconds at which the onResizeRedraw callback will
//	be invoked during an active resize operation. This allows the element's visual
//	content to be re-rendered periodically, preventing the stretched appearance
//	that occurs when a bitmap is simply scaled.
//
//	Set to 0 to disable periodic redraw (default). A typical value is 500–1000ms.
//
// Português:
//
//	Define o intervalo em milissegundos no qual o callback onResizeRedraw será
//	invocado durante uma operação de resize ativa. Isso permite que o conteúdo
//	visual do elemento seja re-renderizado periodicamente, prevenindo a aparência
//	esticada que ocorre quando um bitmap é simplesmente escalado.
//
//	Defina como 0 para desabilitar o redesenho periódico (padrão). Valor típico: 500–1000ms.
func (e *elementData) SetResizeRedrawInterval(ms int64) {
	e.resizeRedrawInterval = ms
}

// SetOnResizeRedraw
//
// English:
//
//	Registers a callback invoked at regular intervals during a resize operation.
//	The interval is set via SetResizeRedrawInterval. Use this to re-cache the
//	element's SVG at the current size, producing a sharp image instead of a
//	stretched bitmap.
//
//	The callback receives a ResizeEvent with the element's current dimensions.
//	Pass nil to remove the callback.
//
//	IMPORTANT: The callback MUST NOT block. If the re-cache operation blocks
//	(e.g., CacheFromSvg), launch it in a goroutine from inside the callback.
//
// Português:
//
//	Registra um callback invocado em intervalos regulares durante uma operação
//	de resize. O intervalo é definido via SetResizeRedrawInterval. Use para
//	re-cachear o SVG do elemento no tamanho atual, produzindo uma imagem nítida
//	em vez de um bitmap esticado.
//
//	O callback recebe um ResizeEvent com as dimensões atuais do elemento.
//	Passe nil para remover o callback.
//
//	IMPORTANTE: O callback NÃO DEVE bloquear. Se a operação de re-cache bloqueia
//	(ex: CacheFromSvg), lance-a em uma goroutine de dentro do callback.
func (e *elementData) SetOnResizeRedraw(fn func(event ResizeEvent)) {
	e.onResizeRedraw = fn
}

// SetCursorHitTest
//
// English:
//
//	Registers a callback that controls the cursor style based on the pointer's
//	position within the element. When set, this overrides the automatic cursor
//	logic (which shows CursorPointer for any clickable element).
//
//	The callback receives local coordinates (relative to the element's top-left
//	corner) and should return the desired CursorStyle. Return "" (empty string)
//	to use the default cursor.
//
//	Example — show pointer only near a stop button:
//
//	  elem.SetCursorHitTest(func(lx, ly float64) sprite.CursorStyle {
//	      dx := lx - stopX
//	      dy := ly - stopY
//	      if dx*dx + dy*dy <= radius*radius {
//	          return sprite.CursorPointer
//	      }
//	      return "" // default cursor
//	  })
//
// Português:
//
//	Registra um callback que controla o estilo do cursor baseado na posição do
//	ponteiro dentro do elemento. Quando definido, isso sobrescreve a lógica
//	automática de cursor (que mostra CursorPointer para qualquer elemento clicável).
//
//	O callback recebe coordenadas locais (relativas ao canto superior esquerdo
//	do elemento) e deve retornar o CursorStyle desejado. Retorne "" (string vazia)
//	para usar o cursor padrão.
func (e *elementData) SetCursorHitTest(fn func(localX float64, localY float64) CursorStyle) {
	e.cursorHitTest = fn
}

// =====================================
//  Custom Data | Dados Customizados
// =====================================

// SetData
//
// English:
//
//	Stores an arbitrary value associated with this element.
//
// Português:
//
//	Armazena um valor arbitrário associado a este elemento.
func (e *elementData) SetData(key string, value interface{}) {
	e.data[key] = value
}

// GetData
//
// English:
//
//	Retrieves a previously stored custom value by key.
//
// Português:
//
//	Recupera um valor customizado previamente armazenado pela chave.
func (e *elementData) GetData(key string) (value interface{}, found bool) {
	value, found = e.data[key]
	return
}

// DeleteData
//
// English:
//
//	Removes a previously stored custom value by key.
//
// Português:
//
//	Remove um valor customizado previamente armazenado pela chave.
func (e *elementData) DeleteData(key string) {
	delete(e.data, key)
}

// =====================================
//  Internal Drawing | Desenho Interno
// =====================================

// Draw
//
// English:
//
//	Draws the cached image onto the given 2D rendering context at the element's
//	current position and size. Uses globalAlpha for opacity.
//
//	Saving and restoring globalAlpha instead of using save()/restore() because
//	save/restore pushes the entire canvas state onto a stack, which is much heavier
//	than just setting one property. Since we only modify globalAlpha, direct
//	set/reset is significantly more efficient.
//
// Português:
//
//	Desenha a imagem cacheada no contexto de renderização 2D fornecido na posição
//	e tamanho atuais do elemento. Usa globalAlpha para opacidade.
//
//	Salvando e restaurando globalAlpha ao invés de usar save()/restore() porque
//	save/restore empilha todo o estado do canvas, o que é muito mais pesado do que
//	apenas definir uma propriedade. Como só modificamos globalAlpha, set/reset
//	direto é significativamente mais eficiente.
func (e *elementData) Draw(ctx js.Value) (err error) {
	if !e.isCachedVal || e.cachedImage.IsUndefined() {
		return
	}

	needsAlphaChange := e.opacity < 1.0

	if needsAlphaChange {
		ctx.Set("globalAlpha", e.opacity)
	}

	ctx.Call("drawImage", e.cachedImage, e.x, e.y, e.width, e.height)

	// Restore alpha before drawing buttons.
	// Buttons are drawn at full opacity so they remain clearly visible even when
	// the parent element is semi-transparent. This is intentional — the user needs
	// to see and grab the buttons regardless of the parent's opacity.
	//
	// Português: Restaura alpha antes de desenhar botões.
	// Botões são desenhados com opacidade total para que permaneçam claramente visíveis
	// mesmo quando o elemento pai está semi-transparente. Isso é intencional — o usuário
	// precisa ver e pegar os botões independente da opacidade do pai.
	if needsAlphaChange {
		ctx.Set("globalAlpha", 1.0)
	}

	if e.resizeButtonVisible && e.resizeRenderer != nil {
		for i, handle := range allResizeHandles {
			btn := &e.resizeHandles[i]
			if !btn.cached {
				continue
			}

			cx, cy := resizeHandleCenter(handle, e.x, e.y, e.width, e.height, e.resizeHandleOffset)
			drawX, drawY := resizeHandleDrawRect(cx, cy, btn.width, btn.height)
			ctx.Call("drawImage", btn.cachedImage, drawX, drawY, btn.width, btn.height)
		}
	}

	return
}

// HitTest
//
// English:
//
//	Tests whether the given canvas coordinates fall within this element's bounding box
//	or on any of its visible resize buttons. The button check is necessary because
//	buttons with positive offset extend beyond the element's main bounding box.
//
// Português:
//
//	Testa se as coordenadas do canvas fornecidas estão dentro do bounding box deste
//	elemento ou sobre qualquer botão de resize visível. A verificação dos botões é
//	necessária porque botões com offset positivo se estendem além do bounding box
//	principal do elemento.
func (e *elementData) HitTest(canvasX float64, canvasY float64) (hit bool) {
	// Check main element bounds first.
	// Português: Verifica os limites do elemento principal primeiro.
	hit = canvasX >= e.x &&
		canvasX <= e.x+e.width &&
		canvasY >= e.y &&
		canvasY <= e.y+e.height

	if hit {
		return
	}

	// If not in main bounds, check resize button areas.
	// This handles buttons that extend outside the element (positive offset).
	//
	// Português: Se não está nos limites principais, verifica áreas dos botões de resize.
	// Isso trata botões que se estendem para fora do elemento (offset positivo).
	if e.resizeButtonVisible && e.resizeRenderer != nil {
		for i, handle := range allResizeHandles {
			btn := &e.resizeHandles[i]
			if !btn.cached {
				continue
			}

			halfW := btn.width / 2
			halfH := btn.height / 2

			cx, cy := resizeHandleCenter(handle, e.x, e.y, e.width, e.height, e.resizeHandleOffset)
			if canvasX >= cx-halfW && canvasX <= cx+halfW &&
				canvasY >= cy-halfH && canvasY <= cy+halfH {
				hit = true
				return
			}
		}
	}

	return
}

// HitTestResizeHandle
//
// English:
//
//	Tests whether the given canvas coordinates fall on one of the element's resize
//	handles. Checks corners first (they overlap edges), then edges.
//
//	The handle regions extend inward from the element's border by resizeHandleSize
//	pixels. Corners are square regions at each corner. Edges are the remaining
//	border strips between corners.
//
// Português:
//
//	Testa se as coordenadas do canvas fornecidas estão sobre uma das alças de
//	redimensionamento do elemento. Verifica cantos primeiro (eles sobrepõem bordas),
//	depois bordas.
//
//	As regiões das alças se estendem para dentro da borda do elemento por
//	resizeHandleSize pixels. Cantos são regiões quadradas em cada canto. Bordas são
//	as faixas restantes entre os cantos.
func (e *elementData) HitTestResizeHandle(canvasX float64, canvasY float64) (handle ResizeHandle) {
	handle = ResizeHandleNone

	if !e.resizeEnable {
		return
	}

	// Check visual resize buttons first — they have a larger clickable area than
	// the thin edge strips, making resize easier to initiate on touch devices.
	// The button hit-test uses the button's bounding box, which extends beyond
	// the element boundary (by handleOffset), so it catches clicks that would
	// otherwise miss the element entirely.
	//
	// Português: Verifica botões visuais de resize primeiro — eles têm uma área clicável
	// maior que as finas faixas de borda, facilitando iniciar o resize em dispositivos touch.
	// O hit-test do botão usa o bounding box do botão, que se estende além do limite
	// do elemento (por handleOffset), então captura clicks que de outra forma perderiam
	// o elemento completamente.
	if e.resizeButtonVisible && e.resizeRenderer != nil {
		for i, h := range allResizeHandles {
			btn := &e.resizeHandles[i]
			if !btn.cached {
				continue
			}

			halfW := btn.width / 2
			halfH := btn.height / 2

			cx, cy := resizeHandleCenter(h, e.x, e.y, e.width, e.height, e.resizeHandleOffset)
			if canvasX >= cx-halfW && canvasX <= cx+halfW &&
				canvasY >= cy-halfH && canvasY <= cy+halfH {
				handle = h
				return
			}
		}
	}

	// Fall back to invisible edge/corner hit-testing.
	// This provides a secondary hit region even when buttons are visible, and is the
	// only method when no visual buttons are set.
	//
	// Português: Recorre ao hit-testing invisível de borda/canto.
	// Isso fornece uma região de hit secundária mesmo quando botões estão visíveis,
	// e é o único método quando nenhum botão visual está definido.
	hs := e.resizeHandleSize
	left := e.x
	right := e.x + e.width
	top := e.y
	bottom := e.y + e.height

	// Check if the point is within the outer bounds at all.
	// Português: Verifica se o ponto está dentro dos limites externos.
	if canvasX < left || canvasX > right || canvasY < top || canvasY > bottom {
		return
	}

	inLeft := canvasX <= left+hs
	inRight := canvasX >= right-hs
	inTop := canvasY <= top+hs
	inBottom := canvasY >= bottom-hs

	// Corners first — they take priority over edges.
	// Português: Cantos primeiro — eles têm prioridade sobre bordas.
	switch {
	case inTop && inLeft:
		handle = ResizeHandleTopLeft
	case inTop && inRight:
		handle = ResizeHandleTopRight
	case inBottom && inLeft:
		handle = ResizeHandleBottomLeft
	case inBottom && inRight:
		handle = ResizeHandleBottomRight
	case inTop:
		handle = ResizeHandleTop
	case inBottom:
		handle = ResizeHandleBottom
	case inLeft:
		handle = ResizeHandleLeft
	case inRight:
		handle = ResizeHandleRight
	}

	return
}

// =====================================
//  Lifecycle | Ciclo de Vida
// =====================================

// Destroy
//
// English:
//
//	Releases all resources held by this element.
//
// Português:
//
//	Libera todos os recursos mantidos por este elemento.
func (e *elementData) Destroy() {
	e.destroyed = true
	e.cachedImage = js.Undefined()
	e.isCachedVal = false
	e.svgXml = ""

	// Clear resize button resources.
	// Português: Limpa recursos dos botões de resize.
	e.resizeRenderer = nil
	e.resizeButtonVisible = false
	for i := range e.resizeHandles {
		e.resizeHandles[i] = resizeHandleButton{
			cachedImage: js.Undefined(),
		}
	}

	// Clear all callbacks to allow GC.
	// Português: Limpa todos os callbacks para permitir GC.
	e.onClick = nil
	e.onDoubleClick = nil
	e.onPointerEnter = nil
	e.onPointerLeave = nil
	e.onDragStart = nil
	e.onDragMove = nil
	e.onDragEnd = nil
	e.onResizeStart = nil
	e.onResizeMove = nil
	e.onResizeEnd = nil
	e.onResizeRedraw = nil
	e.cursorHitTest = nil

	e.data = nil
	e.stage = nil
}

// =====================================
//  Private Helpers | Helpers Privados
// =====================================

// markDirty
//
// English:
//
//	Notifies the owning stage that the canvas needs to be re-rendered.
//
// Português:
//
//	Notifica o stage proprietário que o canvas precisa ser re-renderizado.
func (e *elementData) markDirty() {
	if e.stage != nil {
		e.stage.MarkDirty()
	}
}

// clampToDragBounds
//
// English:
//
//	Adjusts the given position to stay within the element's drag bounds, if set.
//	Takes element size into account so the element never leaves the bounds entirely.
//
// Português:
//
//	Ajusta a posição fornecida para ficar dentro dos limites de arraste do elemento,
//	se definidos. Leva em conta o tamanho do elemento para que ele nunca saia
//	totalmente dos limites.
func (e *elementData) clampToDragBounds(x float64, y float64) (clampedX float64, clampedY float64) {
	clampedX = x
	clampedY = y

	if e.dragBounds == nil {
		return
	}

	b := e.dragBounds
	clampedX = clamp(x, b.X, b.X+b.Width-e.width)
	clampedY = clamp(y, b.Y, b.Y+b.Height-e.height)
	return
}
