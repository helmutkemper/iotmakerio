package sprite

// Stage
//
// English:
//
//	Stage is the main manager for a single HTML canvas element. It owns all Element
//	instances, handles the render loop via requestAnimationFrame, attaches DOM event
//	listeners (mouse and touch), performs hit-testing to dispatch events to the correct
//	Element, and manages the draw order based on z-index.
//
//	Only one Stage should be created per canvas element. Creating multiple Stages on
//	the same canvas will result in undefined behavior.
//
// Português:
//
//	Stage é o gerenciador principal de um único elemento HTML canvas. Ele possui todas
//	as instâncias de Element, gerencia o loop de renderização via requestAnimationFrame,
//	anexa event listeners do DOM (mouse e touch), realiza hit-testing para despachar
//	eventos ao Element correto e gerencia a ordem de desenho baseada em z-index.
//
//	Apenas um Stage deve ser criado por elemento canvas. Criar múltiplos Stages no
//	mesmo canvas resultará em comportamento indefinido.
type Stage interface {

	// =====================================
	//  Element Management | Gerenciamento de Elementos
	// =====================================

	// CreateElement
	//
	// English:
	//
	//  Creates a new Element with the given configuration, adds it to the Stage,
	//  and returns it. If config.SvgXml is provided, the SVG will be cached
	//  immediately. The element is automatically sorted into the draw order by index.
	//
	// Português:
	//
	//  Cria um novo Element com a configuração fornecida, adiciona-o ao Stage e o
	//  retorna. Se config.SvgXml for fornecido, o SVG será cacheado imediatamente.
	//  O elemento é automaticamente ordenado na ordem de desenho pelo index.
	CreateElement(config ElementConfig) (element Element, err error)

	// AddElement
	//
	// English:
	//
	//  Adds an externally created Element to the Stage. Returns an error if an
	//  element with the same ID already exists.
	//
	// Português:
	//
	//  Adiciona um Element criado externamente ao Stage. Retorna erro se um elemento
	//  com o mesmo ID já existir.
	AddElement(element Element) (err error)

	// RemoveElement
	//
	// English:
	//
	//  Removes the element with the given ID from the Stage. The element is not
	//  destroyed — it can be re-added later. Marks the stage as dirty.
	//
	// Português:
	//
	//  Remove o elemento com o ID fornecido do Stage. O elemento não é destruído —
	//  pode ser re-adicionado depois. Marca o stage como dirty.
	RemoveElement(id string) (err error)

	// GetElement
	//
	// English:
	//
	//  Returns the element with the given ID, or nil and false if not found.
	//
	// Português:
	//
	//  Retorna o elemento com o ID fornecido, ou nil e false se não encontrado.
	GetElement(id string) (element Element, found bool)

	// GetElements
	//
	// English:
	//
	//  Returns all elements currently on the Stage, sorted by z-index (lowest first).
	//
	// Português:
	//
	//  Retorna todos os elementos atualmente no Stage, ordenados por z-index
	//  (menor primeiro).
	GetElements() (elements []Element)

	// GetElementAt
	//
	// English:
	//
	//  Returns the topmost visible element at the given canvas coordinates.
	//  Elements are tested from highest z-index to lowest. Returns nil and false
	//  if no element is found at the given position.
	//
	// Português:
	//
	//  Retorna o elemento visível mais acima nas coordenadas do canvas fornecidas.
	//  Elementos são testados do maior z-index para o menor. Retorna nil e false
	//  se nenhum elemento for encontrado na posição fornecida.
	GetElementAt(canvasX float64, canvasY float64) (element Element, found bool)

	// GetElementCount
	//
	// English:
	//
	//  Returns the total number of elements on the Stage.
	//
	// Português:
	//
	//  Retorna o número total de elementos no Stage.
	GetElementCount() (count int)

	// GetHighestIndex
	//
	// English:
	//
	//  Returns the highest z-index currently in use on the Stage. Returns -1 if
	//  there are no elements.
	//
	// Português:
	//
	//  Retorna o maior z-index atualmente em uso no Stage. Retorna -1 se não
	//  houver elementos.
	GetHighestIndex() (index int)

	// GetLowestIndex
	//
	// English:
	//
	//  Returns the lowest z-index currently in use on the Stage. Returns -1 if
	//  there are no elements.
	//
	// Português:
	//
	//  Retorna o menor z-index atualmente em uso no Stage. Retorna -1 se não
	//  houver elementos.
	GetLowestIndex() (index int)

	// =====================================
	//  Canvas | Canvas
	// =====================================

	// GetCanvasSize
	//
	// English:
	//
	//  Returns the current width and height of the canvas in pixels.
	//
	// Português:
	//
	//  Retorna a largura e altura atuais do canvas em pixels.
	GetCanvasSize() (width int, height int)

	// SetCanvasSize
	//
	// English:
	//
	//  Resizes the canvas to the given dimensions. All elements retain their
	//  absolute positions. Marks the stage as dirty.
	//
	// Português:
	//
	//  Redimensiona o canvas para as dimensões fornecidas. Todos os elementos mantêm
	//  suas posições absolutas. Marca o stage como dirty.
	SetCanvasSize(width int, height int)

	// SetBackgroundColor
	//
	// English:
	//
	//  Sets the CSS color used to clear the canvas before each render.
	//  Use "transparent" for no background.
	//
	// Português:
	//
	//  Define a cor CSS usada para limpar o canvas antes de cada renderização.
	//  Use "transparent" para sem fundo.
	SetBackgroundColor(color string)

	// =====================================
	//  Rendering | Renderização
	// =====================================

	// Render
	//
	// English:
	//
	//  Forces an immediate full render of the canvas. Normally, the Stage renders
	//  automatically on the next animation frame when dirty. Use this only when
	//  you need synchronous rendering (e.g., for screenshots).
	//
	// Português:
	//
	//  Força uma renderização completa imediata do canvas. Normalmente, o Stage
	//  renderiza automaticamente no próximo frame de animação quando dirty. Use apenas
	//  quando precisar de renderização síncrona (ex: para screenshots).
	Render()

	// MarkDirty
	//
	// English:
	//
	//  Marks the stage as needing a re-render on the next animation frame.
	//  This is called automatically by element mutations, but can be called
	//  manually if needed.
	//
	// Português:
	//
	//  Marca o stage como precisando de re-renderização no próximo frame de animação.
	//  Chamado automaticamente por mutações de elementos, mas pode ser chamado
	//  manualmente se necessário.
	MarkDirty()

	// SetRenderCallback
	//
	// English:
	//
	//  Registers a callback that is invoked after each render cycle completes.
	//  Useful for overlays, debug info, or post-processing. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado após cada ciclo de renderização completar.
	//  Útil para overlays, informações de debug ou pós-processamento. Passe nil
	//  para remover.
	SetRenderCallback(fn func())

	// =====================================
	//  Camera | Câmera
	// =====================================

	// GetCamera
	//
	// English:
	//
	//  Returns the Stage's Camera, or nil if no camera has been set.
	//  The Camera controls pan (scroll) and zoom (scale) of the canvas viewport
	//  without affecting the logical coordinates of any Element.
	//
	// Português:
	//
	//  Retorna a Camera do Stage, ou nil se nenhuma câmera foi definida.
	//  A Camera controla pan (rolagem) e zoom (escala) da viewport do canvas
	//  sem afetar as coordenadas lógicas de nenhum Element.
	GetCamera() *Camera

	// SetCamera
	//
	// English:
	//
	//  Sets the Stage's Camera. Pass nil to disable camera features.
	//  Use sprite.NewCamera() to create a Camera with sensible defaults.
	//  If the Stage is already running, camera event listeners are managed
	//  automatically.
	//
	// Português:
	//
	//  Define a Camera do Stage. Passe nil para desabilitar features da câmera.
	//  Use sprite.NewCamera() para criar uma Camera com valores padrão sensatos.
	//  Se o Stage já estiver rodando, event listeners da câmera são gerenciados
	//  automaticamente.
	SetCamera(camera *Camera)

	// GetAllElementsBounds
	//
	// English:
	//
	//  Returns the axis-aligned bounding box that contains all visible elements
	//  on the Stage, in world coordinates. Useful for Camera.FitAll().
	//  Returns (0,0,0,0) if there are no visible elements.
	//
	// Português:
	//
	//  Retorna o bounding box alinhado aos eixos que contém todos os elementos
	//  visíveis no Stage, em coordenadas mundo. Útil para Camera.FitAll().
	//  Retorna (0,0,0,0) se não há elementos visíveis.
	GetAllElementsBounds() (x, y, w, h float64)

	// =====================================
	//  Stage-Level Events | Eventos do Stage
	// =====================================

	// SetOnClickStage
	//
	// English:
	//
	//  Registers a callback invoked when the canvas is clicked but no element is
	//  hit. Useful for deselecting or context menus on empty areas.
	//  Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando o canvas é clicado mas nenhum elemento
	//  é atingido. Útil para desselecionar ou menus de contexto em áreas vazias.
	//  Passe nil para remover.
	SetOnClickStage(fn func(event PointerEvent))

	// SetOnDoubleClickStage
	//
	// English:
	//
	//  Registers a callback invoked when the canvas is double-clicked but no element
	//  is hit. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado quando o canvas recebe double-click mas nenhum
	//  elemento é atingido. Passe nil para remover.
	SetOnDoubleClickStage(fn func(event PointerEvent))

	// SetOnPointerMoveStage
	//
	// English:
	//
	//  Registers a callback invoked on every pointer movement over the canvas,
	//  regardless of whether an element is under the pointer. Useful for custom
	//  cursor logic or coordinate displays. Pass nil to remove.
	//
	// Português:
	//
	//  Registra um callback invocado a cada movimento do ponteiro sobre o canvas,
	//  independente de haver um elemento sob o ponteiro. Útil para lógica de cursor
	//  customizada ou exibição de coordenadas. Passe nil para remover.
	SetOnPointerMoveStage(fn func(event PointerEvent))

	// =====================================
	//  Cursor | Cursor
	// =====================================

	// SetCursor
	//
	// English:
	//
	//  Manually sets the CSS cursor style on the canvas element. This is
	//  overridden by automatic cursor management during drag/resize unless
	//  automatic cursors are disabled.
	//
	// Português:
	//
	//  Define manualmente o estilo de cursor CSS no elemento canvas. É sobrescrito
	//  pelo gerenciamento automático de cursor durante drag/resize, a menos que
	//  cursores automáticos estejam desabilitados.
	SetCursor(cursor CursorStyle)

	// SetAutoCursorEnable
	//
	// English:
	//
	//  Enables or disables automatic cursor style changes based on element
	//  hover, drag, and resize states. Enabled by default.
	//
	// Português:
	//
	//  Habilita ou desabilita mudanças automáticas de estilo de cursor baseadas nos
	//  estados de hover, drag e resize dos elementos. Habilitado por padrão.
	SetAutoCursorEnable(enable bool)

	// =====================================
	//  Lifecycle | Ciclo de Vida
	// =====================================

	// Start
	//
	// English:
	//
	//  Starts the render loop and event listeners. Must be called after creating
	//  the Stage and adding initial elements. Returns an error if the canvas
	//  element is not found in the DOM.
	//
	// Português:
	//
	//  Inicia o loop de renderização e os event listeners. Deve ser chamado após
	//  criar o Stage e adicionar os elementos iniciais. Retorna erro se o elemento
	//  canvas não for encontrado no DOM.
	Start() (err error)

	// Stop
	//
	// English:
	//
	//  Stops the render loop and removes all DOM event listeners from the canvas.
	//  Elements are preserved and the Stage can be restarted with Start().
	//
	// Português:
	//
	//  Para o loop de renderização e remove todos os event listeners do DOM do canvas.
	//  Os elementos são preservados e o Stage pode ser reiniciado com Start().
	Stop()

	// Destroy
	//
	// English:
	//
	//  Stops the Stage, destroys all elements, and releases all resources.
	//  After calling Destroy, the Stage must not be used.
	//
	// Português:
	//
	//  Para o Stage, destrói todos os elementos e libera todos os recursos.
	//  Após chamar Destroy, o Stage não deve ser usado.
	Destroy()

	// IsRunning
	//
	// English:
	//
	//  Returns whether the Stage render loop is currently active.
	//
	// Português:
	//
	//  Retorna se o loop de renderização do Stage está atualmente ativo.
	IsRunning() (running bool)
}
