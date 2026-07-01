// sprite/element.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

import (
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// Element
//
// English:
//
//	Element represents a single sprite on the Stage. Each Element is an SVG rendered
//	once into a cached offscreen canvas and drawn to the visible canvas at the
//	element's position, size, and z-index.
//
//	Elements support click, double-click, drag, and resize interactions via both
//	mouse and touch input.
//
// Português:
//
//	Element representa um único sprite no Stage. Cada Element é um SVG renderizado
//	uma única vez em um canvas offscreen cacheado e desenhado no canvas visível
//	na posição, tamanho e z-index do elemento.
//
//	Elements suportam interações de click, double-click, arrastar e redimensionar
//	via mouse e touch.
type Element interface {

	// =====================================
	//  Identification | Identificação
	// =====================================

	// GetID
	//
	// English:
	//
	//  Returns the unique identifier of this element.
	//
	// Português:
	//
	//  Retorna o identificador único deste elemento.
	GetID() (id string)

	// =====================================
	//  Position | Posição
	// =====================================

	// SetPosition
	//
	// English:
	//
	//  Sets the element's top-left corner position on the canvas.
	//  Marks the stage as dirty so it will re-render on the next animation frame.
	//
	// Português:
	//
	//  Define a posição do canto superior esquerdo do elemento no canvas.
	//  Marca o stage como dirty para re-renderizar no próximo frame de animação.
	SetPosition(x float64, y float64) //todo: ajustar density

	// GetPosition
	//
	// English:
	//
	//  Returns the current position of the element's top-left corner.
	//
	// Português:
	//
	//  Retorna a posição atual do canto superior esquerdo do elemento.
	GetPosition() (x float64, y float64)

	// SetPositionD
	//
	// English:
	//
	//  Sets the element's position using Density values. The density factor
	//  is applied automatically.
	//
	// Português:
	//
	//  Define a posição do elemento usando valores Density. O fator de
	//  densidade é aplicado automaticamente.
	SetPositionD(x, y rulesDensity.Density)

	// GetPositionD
	//
	// English:
	//
	//  Returns the element's position as Density values.
	//
	// Português:
	//
	//  Retorna a posição do elemento como valores Density.
	GetPositionD() (x, y rulesDensity.Density)

	// GetXD
	//
	// English:
	//
	//  Returns the horizontal position as a Density value.
	//
	// Português:
	//
	//  Retorna a posição horizontal como valor Density.
	GetXD() rulesDensity.Density

	// GetYD
	//
	// English:
	//
	//  Returns the vertical position as a Density value.
	//
	// Português:
	//
	//  Retorna a posição vertical como valor Density.
	GetYD() rulesDensity.Density

	// =====================================
	//  Size | Tamanho
	// =====================================

	// SetSize
	//
	// English:
	//
	//  Sets the display size of the element. The cached image will be scaled to
	//  this size when drawn. Marks the stage as dirty.
	//
	// Português:
	//
	//  Define o tamanho de exibição do elemento. A imagem cacheada será escalada
	//  para este tamanho ao ser desenhada. Marca o stage como dirty.
	SetSize(width float64, height float64)

	// GetSize
	//
	// English:
	//
	//  Returns the current display size of the element.
	//
	// Português:
	//
	//  Retorna o tamanho de exibição atual do elemento.
	GetSize() (width float64, height float64)

	// GetBounds
	//
	// English:
	//
	//  Returns the bounding rectangle of the element (position + size).
	//
	// Português:
	//
	//  Retorna o retângulo delimitador do elemento (posição + tamanho).
	GetBounds() (bounds Rect)

	// SetSizeD
	//
	// English:
	//
	//  Sets the display size using Density values.
	//
	// Português:
	//
	//  Define o tamanho de exibição usando valores Density.
	SetSizeD(width, height rulesDensity.Density)

	// GetSizeD
	//
	// English:
	//
	//  Returns the display size as Density values.
	//
	// Português:
	//
	//  Retorna o tamanho de exibição como valores Density.
	GetSizeD() (width, height rulesDensity.Density)

	// GetWidthD
	//
	// English:
	//
	//  Returns the width as a Density value.
	//
	// Português:
	//
	//  Retorna a largura como valor Density.
	GetWidthD() rulesDensity.Density

	// GetHeightD
	//
	// English:
	//
	//  Returns the height as a Density value.
	//
	// Português:
	//
	//  Retorna a altura como valor Density.
	GetHeightD() rulesDensity.Density

	// =====================================
	//  Z-Index | Índice de Camada
	// =====================================

	// SetIndex
	//
	// English:
	//
	//  Sets the z-index of the element. Higher values are drawn on top.
	//  Marks the stage as dirty and triggers re-sorting of the draw order.
	//
	// Português:
	//
	//  Define o z-index do elemento. Valores maiores são desenhados por cima.
	//  Marca o stage como dirty e dispara reordenação da ordem de desenho.
	SetIndex(index int)

	// GetIndex
	//
	// English:
	//
	//  Returns the current z-index of the element.
	//
	// Português:
	//
	//  Retorna o z-index atual do elemento.
	GetIndex() (index int)

	// MoveToFront
	//
	// English:
	//
	//  Sets this element's index to be one above the current highest index on the
	//  stage, bringing it to the front of all other elements.
	//
	// Português:
	//
	//  Define o index deste elemento para um acima do maior index atual no stage,
	//  trazendo-o para a frente de todos os outros elementos.
	MoveToFront()

	// MoveToBack
	//
	// English:
	//
	//  Sets this element's index to be one below the current lowest index on the
	//  stage, sending it to trás de todos os outros elementos.
	//
	// Português:
	//
	//  Define o index deste elemento para um abaixo do menor index atual no stage,
	//  enviando-o para trás de todos os outros elementos.
	MoveToBack()

	// =====================================
	//  Visibility | Visibilidade
	// =====================================

	// SetVisible
	//
	// English:
	//
	//  Shows or hides the element. Hidden elements do not receive events and are
	//  not drawn. Marks the stage as dirty.
	//
	// Português:
	//
	//  Mostra ou oculta o elemento. Elementos ocultos não recebem eventos e não
	//  são desenhados. Marca o stage como dirty.
	SetVisible(visible bool)

	// IsVisible
	//
	// English:
	//
	//  Returns whether the element is currently visible.
	//
	// Português:
	//
	//  Retorna se o elemento está atualmente visível.
	IsVisible() (visible bool)

	// =====================================
	//  Opacity | Opacidade
	// =====================================

	// SetOpacity
	//
	// English:
	//
	//  Sets the opacity of the element (0.0 = fully transparent, 1.0 = fully opaque).
	//  Marks the stage as dirty.
	//
	// Português:
	//
	//  Define a opacidade do elemento (0.0 = totalmente transparente, 1.0 = totalmente opaco).
	//  Marca o stage como dirty.
	SetOpacity(opacity float64)

	// GetOpacity
	//
	// English:
	//
	//  Returns the current opacity of the element.
	//
	// Português:
	//
	//  Retorna a opacidade atual do elemento.
	GetOpacity() (opacity float64)

	// =====================================
	//  Screen Space | Espaço de Tela
	// =====================================

	// SetScreenSpace
	//
	// English:
	//
	//  When true, the element is rendered in screen (canvas pixel) coordinates,
	//  ignoring the Camera transform. The element stays fixed on screen regardless
	//  of pan/zoom. Useful for UI overlays like menus, HUDs, toolbars.
	//
	//  Screen-space elements:
	//    - Are drawn after the camera transform is restored (on top of world elements)
	//    - Use screen coordinates for position and size
	//    - Are excluded from GetAllElementsBounds (FitAll ignores them)
	//    - Are excluded from the minimap
	//    - Hit-testing uses screen coordinates directly (no world conversion)
	//    - Drag deltas are NOT divided by zoom
	//
	// Português:
	//
	//  Quando true, o elemento é renderizado em coordenadas de tela (pixels do canvas),
	//  ignorando a transformação da Camera. O elemento fica fixo na tela independente
	//  de pan/zoom. Útil para overlays de UI como menus, HUDs, toolbars.
	//
	//  Elementos screen-space:
	//    - São desenhados após a restauração da transformação da câmera
	//    - Usam coordenadas de tela para posição e tamanho
	//    - São excluídos de GetAllElementsBounds (FitAll os ignora)
	//    - São excluídos do minimapa
	//    - Hit-testing usa coordenadas de tela diretamente
	//    - Deltas de drag NÃO são divididos pelo zoom
	SetScreenSpace(screenSpace bool)

	// IsScreenSpace
	//
	// English:
	//
	//  Returns whether the element is rendered in screen space.
	//
	// Português:
	//
	//  Retorna se o elemento é renderizado em espaço de tela.
	IsScreenSpace() (screenSpace bool)

	// =====================================
	//  Drag | Arrastar
	// =====================================

	// SetDragEnable
	//
	// English:
	//
	//  Enables or disables drag interaction on this element. When enabled, the user
	//  can click/touch and drag the element to a new position.
	//
	// Português:
	//
	//  Habilita ou desabilita a interação de arraste neste elemento. Quando habilitado,
	//  o usuário pode clicar/tocar e arrastar o elemento para uma nova posição.
	SetDragEnable(enable bool)

	// IsDragEnabled
	//
	// English:
	//
	//  Returns whether drag interaction is currently enabled.
	//
	// Português:
	//
	//  Retorna se a interação de arraste está atualmente habilitada.
	IsDragEnabled() (enabled bool)

	// SetDragBounds
	//
	// English:
	//
	//  Sets a bounding rectangle that constrains where the element can be dragged.
	//  Pass nil to remove the constraint and allow free dragging.
	//
	// Português:
	//
	//  Define um retângulo delimitador que restringe para onde o elemento pode ser
	//  arrastado. Passe nil para remover a restrição e permitir arraste livre.
	SetDragBounds(bounds *Rect)

	// GetDragBounds
	//
	// English:
	//
	//  Returns the current drag constraint bounds, or nil if unconstrained.
	//
	// Português:
	//
	//  Retorna os limites de restrição de arraste atuais, ou nil se sem restrição.
	GetDragBounds() (bounds *Rect)

	// SetDragBoundsD
	//
	// English:
	//
	//  Sets drag constraint rectangle using Density values.
	//
	// Português:
	//
	//  Define retângulo de restrição de arraste usando valores Density.
	SetDragBoundsD(x, y, width, height rulesDensity.Density)

	// =====================================
	//  Resize | Redimensionar
	// =====================================

	// SetResizeEnable
	//
	// English:
	//
	//  Enables or disables resize interaction on this element. When enabled, the user
	//  can drag the corners or edges of the element to resize it.
	//
	// Português:
	//
	//  Habilita ou desabilita a interação de redimensionamento neste elemento. Quando
	//  habilitado, o usuário pode arrastar os cantos ou bordas do elemento para
	//  redimensioná-lo.
	SetResizeEnable(enable bool)

	// IsResizeEnabled
	//
	// English:
	//
	//  Returns whether resize interaction is currently enabled.
	//
	// Português:
	//
	//  Retorna se a interação de redimensionamento está atualmente habilitada.
	IsResizeEnabled() (enabled bool)

	// SetKeepAspectRatio
	//
	// English:
	//
	//  When true, resizing from corner handles will preserve the element's
	//  aspect ratio.
	//
	// Português:
	//
	//  Quando true, redimensionar pelas alças de canto preservará a proporção
	//  do elemento.
	SetKeepAspectRatio(keep bool)

	// IsKeepAspectRatio
	//
	// English:
	//
	//  Returns whether aspect ratio is preserved during resize.
	//
	// Português:
	//
	//  Retorna se a proporção é preservada durante o redimensionamento.
	IsKeepAspectRatio() (keep bool)

	// SetMinSize
	//
	// English:
	//
	//  Sets the minimum allowed size when resizing.
	//
	// Português:
	//
	//  Define o tamanho mínimo permitido ao redimensionar.
	SetMinSize(width float64, height float64)

	// GetMinSize
	//
	// English:
	//
	//  Returns the minimum allowed size when resizing.
	//
	// Português:
	//
	//  Retorna o tamanho mínimo permitido ao redimensionar.
	GetMinSize() (width float64, height float64)

	// SetMaxSize
	//
	// English:
	//
	//  Sets the maximum allowed size when resizing. Pass zero values to remove
	//  the constraint.
	//
	// Português:
	//
	//  Define o tamanho máximo permitido ao redimensionar. Passe valores zero
	//  para remover a restrição.
	SetMaxSize(width float64, height float64)

	// GetMaxSize
	//
	// English:
	//
	//  Returns the maximum allowed size when resizing. Zero values mean no constraint.
	//
	// Português:
	//
	//  Retorna o tamanho máximo permitido ao redimensionar. Valores zero significam
	//  sem restrição.
	GetMaxSize() (width float64, height float64)

	// SetResizeButtons
	//
	// English:
	//
	//  Sets a ResizeHandleRenderer that defines the visual appearance of 8 resize
	//  handle buttons positioned around the element's edges and corners.
	//
	//  When set, the sprite package calls RenderHandle() for each of the 8 handles,
	//  serializes the returned SVG DOM element to XML via XMLSerializer, caches each
	//  as a raster image, and draws them around the element. The SVG DOM elements are
	//  discarded after caching — they are never added to the visible DOM.
	//
	//  The buttons provide intuitive visual feedback for resize interactions, especially
	//  on tablets. Their hit areas extend beyond the element boundary (by the offset
	//  returned from GetHandleOffset), making resize easier to initiate.
	//
	//  Pass nil to remove visual resize buttons (invisible edge hit-testing still works
	//  if ResizeEnable is true).
	//
	//  IMPORTANT: This method blocks while caching the 8 button images. It must be called
	//  from a goroutine, not from the main thread or from inside a js.FuncOf callback.
	//
	// Português:
	//
	//  Define um ResizeHandleRenderer que determina a aparência visual de 8 botões de
	//  alça de redimensionamento posicionados ao redor das bordas e cantos do elemento.
	//
	//  IMPORTANTE: Este método bloqueia enquanto cacheia as 8 imagens dos botões. Deve
	//  ser chamado de uma goroutine, não da thread principal ou de dentro de um callback js.FuncOf.
	SetResizeButtons(renderer ResizeHandleRenderer) (err error)

	// ShowResizeButtons
	//
	// English:
	//
	//  Shows or hides the visual resize buttons. Buttons must have been previously set
	//  via SetResizeButtons. When hidden, the invisible edge/corner hit-testing still
	//  works if ResizeEnable is true. Marks the stage as dirty.
	//
	// Português:
	//
	//  Mostra ou oculta os botões visuais de resize. Os botões devem ter sido previamente
	//  definidos via SetResizeButtons. Quando ocultos, o hit-testing invisível de
	//  borda/canto ainda funciona se ResizeEnable for true. Marca o stage como dirty.
	ShowResizeButtons(visible bool)

	// AreResizeButtonsVisible
	//
	// English:
	//
	//  Returns whether the visual resize buttons are currently visible.
	//
	// Português:
	//
	//  Retorna se os botões visuais de resize estão atualmente visíveis.
	AreResizeButtonsVisible() (visible bool)

	// SetMinSizeD
	//
	// English:
	//
	//  Sets minimum resize size using Density values.
	//
	// Português:
	//
	//  Define tamanho mínimo de resize usando valores Density.
	SetMinSizeD(width, height rulesDensity.Density)

	// GetMinSizeD
	//
	// English:
	//
	//  Returns minimum resize size as Density values.
	//
	// Português:
	//
	//  Retorna tamanho mínimo de resize como valores Density.
	GetMinSizeD() (width, height rulesDensity.Density)

	// SetMaxSizeD
	//
	// English:
	//
	//  Sets maximum resize size using Density values.
	//
	// Português:
	//
	//  Define tamanho máximo de resize usando valores Density.
	SetMaxSizeD(width, height rulesDensity.Density)

	// GetMaxSizeD
	//
	// English:
	//
	//  Returns maximum resize size as Density values.
	//
	// Português:
	//
	//  Retorna tamanho máximo de resize como valores Density.
	GetMaxSizeD() (width, height rulesDensity.Density)

	// =====================================
	//  SVG Cache | Cache SVG
	// =====================================

	// CacheFromSvg
	//
	// English:
	//
	//  Renders the given SVG XML string into an offscreen canvas and caches the result.
	//  This is the primary method to set the visual content of the element.
	//  If the element's Width/Height are zero, they will be set to the SVG's intrinsic
	//  dimensions. Marks the stage as dirty.
	//
	// Português:
	//
	//  Renderiza a string XML SVG fornecida em um canvas offscreen e cacheia o resultado.
	//  Este é o método principal para definir o conteúdo visual do elemento.
	//  Se Width/Height do elemento forem zero, serão definidos com as dimensões intrínsecas
	//  do SVG. Marca o stage como dirty.
	CacheFromSvg(svgXml string) (err error)

	// CacheFromImageSrc
	//
	// English:
	//
	//  Loads an image from the given source URL (data URI, blob URL, or HTTP URL)
	//  and caches it as the element's visual content. Marks the stage as dirty.
	//
	// Português:
	//
	//  Carrega uma imagem a partir da URL de origem fornecida (data URI, blob URL,
	//  ou URL HTTP) e a cacheia como conteúdo visual do elemento. Marca o stage como dirty.
	CacheFromImageSrc(src string) (err error)

	// InvalidateCache
	//
	// English:
	//
	//  Forces the cached image to be re-rendered from the current SVG on the next
	//  draw cycle. Useful if the SVG was modified externally.
	//
	// Português:
	//
	//  Força a imagem cacheada a ser re-renderizada a partir do SVG atual no próximo
	//  ciclo de desenho. Útil se o SVG foi modificado externamente.
	InvalidateCache()

	// IsCached
	//
	// English:
	//
	//  Returns whether this element has a valid cached image ready for drawing.
	//
	// Português:
	//
	//  Retorna se este elemento possui uma imagem cacheada válida pronta para desenho.
	IsCached() (cached bool)

	// =====================================
	//  Event Callbacks | Callbacks de Evento
	// =====================================

	// SetOnClick
	//
	// English:
	//
	//  Registers a callback to be invoked when the element is clicked (mouse or touch tap).
	//  Pass nil to remove the callback.
	//
	// Português:
	//
	//  Registra um callback a ser invocado quando o elemento é clicado (mouse ou tap touch).
	//  Passe nil para remover o callback.
	SetOnClick(fn func(event PointerEvent))

	// SetOnDoubleClick
	//
	// English:
	//
	//  Registers a callback to be invoked when the element is double-clicked
	//  (mouse dblclick or two touch taps within the configured interval).
	//  Pass nil to remove the callback.
	//
	// Português:
	//
	//  Registra um callback a ser invocado quando o elemento recebe double-click
	//  (dblclick do mouse ou dois taps touch dentro do intervalo configurado).
	//  Passe nil para remover o callback.
	SetOnDoubleClick(fn func(event PointerEvent))

	// SetOnPointerEnter
	//
	// English:
	//
	//  Registers a callback invoked when the pointer enters the element's bounds.
	//  Useful for hover effects. Only relevant for mouse; touch does not generate
	//  hover events. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando o ponteiro entra nos limites do elemento.
	//  Útil para efeitos de hover. Relevante apenas para mouse; touch não gera
	//  eventos de hover. Passe nil para remover.
	SetOnPointerEnter(fn func(event PointerEvent))

	// SetOnPointerLeave
	//
	// English:
	//
	//  Registers a callback invoked when the pointer leaves the element's bounds.
	//  Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando o ponteiro sai dos limites do elemento.
	//  Passe nil para remover.
	SetOnPointerLeave(fn func(event PointerEvent))

	// SetOnDragStart
	//
	// English:
	//
	//  Registers a callback invoked when a drag operation begins on this element.
	//  Requires DragEnable to be true. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando uma operação de arraste começa neste elemento.
	//  Requer DragEnable como true. Passe nil para remover.
	SetOnDragStart(fn func(event DragEvent))

	// SetOnDragMove
	//
	// English:
	//
	//  Registers a callback invoked on each pointer movement during a drag operation.
	//  Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado a cada movimento do ponteiro durante uma operação
	//  de arraste. Passe nil para remover.
	SetOnDragMove(fn func(event DragEvent))

	// SetOnDragEnd
	//
	// English:
	//
	//  Registers a callback invoked when a drag operation ends. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando uma operação de arraste termina.
	//  Passe nil para remover.
	SetOnDragEnd(fn func(event DragEvent))

	// SetOnResizeStart
	//
	// English:
	//
	//  Registers a callback invoked when a resize operation begins on this element.
	//  Requires ResizeEnable to be true. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando uma operação de redimensionamento começa
	//  neste elemento. Requer ResizeEnable como true. Passe nil para remover.
	SetOnResizeStart(fn func(event ResizeEvent))

	// SetOnResizeMove
	//
	// English:
	//
	//  Registers a callback invoked on each pointer movement during a resize operation.
	//  Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado a cada movimento do ponteiro durante uma operação
	//  de redimensionamento. Passe nil para remover.
	SetOnResizeMove(fn func(event ResizeEvent))

	// SetOnResizeEnd
	//
	// English:
	//
	//  Registers a callback invoked when a resize operation ends. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando uma operação de redimensionamento termina.
	//  Passe nil para remover.
	SetOnResizeEnd(fn func(event ResizeEvent))

	// SetResizeRedrawInterval
	//
	// English:
	//
	//  Sets the interval in milliseconds at which onResizeRedraw is invoked during
	//  an active resize. Set to 0 to disable (default). Typical value: 500–1000ms.
	//
	// Português:
	//
	//  Define o intervalo em milissegundos no qual onResizeRedraw é invocado durante
	//  um resize ativo. Defina 0 para desabilitar (padrão). Valor típico: 500–1000ms.
	SetResizeRedrawInterval(ms int64)

	// SetOnResizeRedraw
	//
	// English:
	//
	//  Registers a callback invoked at regular intervals (set by SetResizeRedrawInterval)
	//  during a resize operation. Use to re-cache the element's SVG at current size.
	//  The callback MUST NOT block — launch goroutines for blocking operations.
	//  Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado em intervalos regulares (definidos por
	//  SetResizeRedrawInterval) durante uma operação de resize. Use para re-cachear
	//  o SVG do elemento no tamanho atual. O callback NÃO DEVE bloquear — lance
	//  goroutines para operações bloqueantes. Passe nil para remover.
	SetOnResizeRedraw(fn func(event ResizeEvent))

	// SetCursorHitTest
	//
	// English:
	//
	//  Registers a callback that controls the cursor style based on pointer position
	//  within the element. When set, overrides the automatic cursor logic.
	//  Receives local coordinates (0,0 = element top-left).
	//  Return "" for default cursor, or a specific CursorStyle.
	//  Pass nil to remove and restore automatic cursor behavior.
	//
	// Português:
	//
	//  Registra um callback que controla o estilo do cursor baseado na posição do
	//  ponteiro dentro do elemento. Quando definido, sobrescreve a lógica automática.
	//  Recebe coordenadas locais (0,0 = canto superior esquerdo do elemento).
	//  Retorne "" para cursor padrão, ou um CursorStyle específico.
	//  Passe nil para remover e restaurar o comportamento automático.
	SetCursorHitTest(fn func(localX float64, localY float64) CursorStyle)

	// =====================================
	//  Custom Data | Dados Customizados
	// =====================================

	// SetData
	//
	// English:
	//
	//  Stores an arbitrary value associated with this element. Useful for attaching
	//  application-specific metadata without extending the struct.
	//
	// Português:
	//
	//  Armazena um valor arbitrário associado a este elemento. Útil para anexar
	//  metadados específicos da aplicação sem estender a struct.
	SetData(key string, value interface{})

	// GetData
	//
	// English:
	//
	//  Retrieves a previously stored custom value by key. Returns nil and false
	//  if the key does not exist.
	//
	// Português:
	//
	//  Recupera um valor customizado previamente armazenado pela chave. Retorna nil
	//  e false se a chave não existir.
	GetData(key string) (value interface{}, found bool)

	// DeleteData
	//
	// English:
	//
	//  Removes a previously stored custom value by key.
	//
	// Português:
	//
	//  Remove um valor customizado previamente armazenado pela chave.
	DeleteData(key string)

	// =====================================
	//  Internal Drawing | Desenho Interno
	// =====================================

	// Draw
	//
	// English:
	//
	//  Draws the cached image onto the given 2D rendering context at the element's
	//  current position and size. This is called by the Stage during the render loop
	//  and should not be called directly by application code.
	//
	// Português:
	//
	//  Desenha a imagem cacheada no contexto de renderização 2D fornecido na posição
	//  e tamanho atuais do elemento. Chamado pelo Stage durante o loop de renderização
	//  e não deve ser chamado diretamente pelo código da aplicação.
	Draw(ctx js.Value) (err error)

	// HitTest
	//
	// English:
	//
	//  Tests whether the given canvas coordinates fall within this element's bounds.
	//  Used by the Stage for event dispatching. Does not consider visibility — the
	//  caller (Stage) should check visibility before calling HitTest.
	//
	// Português:
	//
	//  Testa se as coordenadas do canvas fornecidas estão dentro dos limites deste
	//  elemento. Usado pelo Stage para despacho de eventos. Não considera visibilidade
	//  — o chamador (Stage) deve verificar visibilidade antes de chamar HitTest.
	HitTest(canvasX float64, canvasY float64) (hit bool)

	// HitTestResizeHandle
	//
	// English:
	//
	//  Tests whether the given canvas coordinates fall on one of the element's
	//  resize handles. Returns ResizeHandleNone if no handle is hit.
	//
	// Português:
	//
	//  Testa se as coordenadas do canvas fornecidas estão sobre uma das alças de
	//  redimensionamento do elemento. Retorna ResizeHandleNone se nenhuma alça for atingida.
	HitTestResizeHandle(canvasX float64, canvasY float64) (handle ResizeHandle)

	// =====================================
	//  Lifecycle | Ciclo de Vida
	// =====================================

	// Destroy
	//
	// English:
	//
	//  Releases all resources held by this element, including the cached offscreen
	//  canvas, event callbacks, and custom data. After calling Destroy, the element
	//  must not be used. The Stage will automatically remove it from its collection.
	//
	// Português:
	//
	//  Libera todos os recursos mantidos por este elemento, incluindo o canvas offscreen
	//  cacheado, callbacks de evento e dados customizados. Após chamar Destroy, o
	//  elemento não deve ser usado. O Stage o removerá automaticamente de sua coleção.
	Destroy()
}
