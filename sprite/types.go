// sprite/types.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

// ResizeHandle
//
// English:
//
//	Identifies which handle (corner or edge) of an element is being used for resizing.
//
// Português:
//
//	Identifica qual alça (canto ou borda) do elemento está sendo usada para redimensionar.
type ResizeHandle int

const (
	// ResizeHandleNone indicates no resize handle is active.
	ResizeHandleNone ResizeHandle = iota

	// ResizeHandleTopLeft indicates the top-left corner handle.
	ResizeHandleTopLeft

	// ResizeHandleTop indicates the top edge handle.
	ResizeHandleTop

	// ResizeHandleTopRight indicates the top-right corner handle.
	ResizeHandleTopRight

	// ResizeHandleRight indicates the right edge handle.
	ResizeHandleRight

	// ResizeHandleBottomRight indicates the bottom-right corner handle.
	ResizeHandleBottomRight

	// ResizeHandleBottom indicates the bottom edge handle.
	ResizeHandleBottom

	// ResizeHandleBottomLeft indicates the bottom-left corner handle.
	ResizeHandleBottomLeft

	// ResizeHandleLeft indicates the left edge handle.
	ResizeHandleLeft
)

// CursorStyle
//
// English:
//
//	Represents the CSS cursor style to apply to the canvas.
//
// Português:
//
//	Representa o estilo de cursor CSS a ser aplicado ao canvas.
type CursorStyle string

const (
	CursorDefault    CursorStyle = "default"
	CursorPointer    CursorStyle = "pointer"
	CursorGrab       CursorStyle = "grab"
	CursorGrabbing   CursorStyle = "grabbing"
	CursorMove       CursorStyle = "move"
	CursorNResize    CursorStyle = "n-resize"
	CursorSResize    CursorStyle = "s-resize"
	CursorEResize    CursorStyle = "e-resize"
	CursorWResize    CursorStyle = "w-resize"
	CursorNEResize   CursorStyle = "ne-resize"
	CursorNWResize   CursorStyle = "nw-resize"
	CursorSEResize   CursorStyle = "se-resize"
	CursorSWResize   CursorStyle = "sw-resize"
	CursorNotAllowed CursorStyle = "not-allowed"
)

// PointerEvent
//
// English:
//
//	Contains information about a pointer interaction (mouse or touch) relative to the
//	canvas and to the element that received the event.
//
// Português:
//
//	Contém informações sobre uma interação de ponteiro (mouse ou touch) relativas ao
//	canvas e ao elemento que recebeu o evento.
type PointerEvent struct {
	// CanvasX is the X coordinate relative to the canvas origin.
	//
	// Português: Coordenada X relativa à origem do canvas.
	CanvasX float64

	// CanvasY is the Y coordinate relative to the canvas origin.
	//
	// Português: Coordenada Y relativa à origem do canvas.
	CanvasY float64

	// LocalX is the X coordinate relative to the element's top-left corner.
	//
	// Português: Coordenada X relativa ao canto superior esquerdo do elemento.
	LocalX float64

	// LocalY is the Y coordinate relative to the element's top-left corner.
	//
	// Português: Coordenada Y relativa ao canto superior esquerdo do elemento.
	LocalY float64

	// Button indicates the mouse button pressed (0=left, 1=middle, 2=right).
	// For touch events, this is always 0.
	//
	// Português: Indica o botão do mouse pressionado (0=esquerdo, 1=meio, 2=direito).
	// Para eventos de touch, é sempre 0.
	Button int

	// IsTouch is true when the event was triggered by a touch input.
	//
	// Português: True quando o evento foi disparado por uma entrada touch.
	IsTouch bool
}

// DragEvent
//
// English:
//
//	Contains information about a drag interaction, including starting position and
//	accumulated delta movement.
//
// Português:
//
//	Contém informações sobre uma interação de arraste, incluindo posição inicial e
//	delta de movimento acumulado.
type DragEvent struct {
	// PointerEvent embeds the current pointer state.
	//
	// Português: PointerEvent embute o estado atual do ponteiro.
	PointerEvent

	// StartX is the X coordinate where the drag began, relative to the canvas.
	//
	// Português: Coordenada X onde o arraste começou, relativa ao canvas.
	StartX float64

	// StartY is the Y coordinate where the drag began, relative to the canvas.
	//
	// Português: Coordenada Y onde o arraste começou, relativa ao canvas.
	StartY float64

	// DeltaX is the horizontal distance moved since the drag started.
	//
	// Português: Distância horizontal percorrida desde o início do arraste.
	DeltaX float64

	// DeltaY is the vertical distance moved since the drag started.
	//
	// Português: Distância vertical percorrida desde o início do arraste.
	DeltaY float64
}

// ResizeEvent
//
// English:
//
//	Contains information about a resize interaction, including the handle being
//	dragged and the old/new dimensions.
//
// Português:
//
//	Contém informações sobre uma interação de redimensionamento, incluindo a alça
//	sendo arrastada e as dimensões anterior/nova.
type ResizeEvent struct {
	// PointerEvent embeds the current pointer state.
	//
	// Português: PointerEvent embute o estado atual do ponteiro.
	PointerEvent

	// Handle identifies which corner or edge is being dragged.
	//
	// Português: Identifica qual canto ou borda está sendo arrastado.
	Handle ResizeHandle

	// OldWidth is the element width before this resize step.
	//
	// Português: Largura do elemento antes deste passo de redimensionamento.
	OldWidth float64

	// OldHeight is the element height before this resize step.
	//
	// Português: Altura do elemento antes deste passo de redimensionamento.
	OldHeight float64

	// NewWidth is the element width after this resize step.
	//
	// Português: Largura do elemento após este passo de redimensionamento.
	NewWidth float64

	// NewHeight is the element height after this resize step.
	//
	// Português: Altura do elemento após este passo de redimensionamento.
	NewHeight float64
}

// Rect
//
// English:
//
//	Represents a rectangle defined by its origin (X, Y) and size (Width, Height).
//
// Português:
//
//	Representa um retângulo definido por sua origem (X, Y) e tamanho (Width, Height).
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// Point
//
// English:
//
//	Represents a 2D coordinate.
//
// Português:
//
//	Representa uma coordenada 2D.
type Point struct {
	X float64
	Y float64
}

// ElementConfig
//
// English:
//
//	Initial configuration used when creating a new Element. All fields are optional
//	and have sensible defaults (position 0,0 — index 0 — visible true — drag/resize disabled).
//
// Português:
//
//	Configuração inicial usada ao criar um novo Element. Todos os campos são opcionais
//	e possuem valores padrão sensatos (posição 0,0 — index 0 — visível true — drag/resize desabilitados).
type ElementConfig struct {
	// ID is a unique identifier for the element. If empty, a UUID will be generated.
	//
	// Português: Identificador único do elemento. Se vazio, um UUID será gerado.
	ID string

	// X is the initial horizontal position on the canvas.
	//
	// Português: Posição horizontal inicial no canvas.
	X float64

	// Y is the initial vertical position on the canvas.
	//
	// Português: Posição vertical inicial no canvas.
	Y float64

	// Width is the initial display width. If zero, uses the SVG intrinsic width.
	//
	// Português: Largura de exibição inicial. Se zero, usa a largura intrínseca do SVG.
	Width float64

	// Height is the initial display height. If zero, uses the altura intrínseca do SVG.
	//
	// Português: Altura de exibição inicial. Se zero, usa a altura intrínseca do SVG.
	Height float64

	// Index is the initial z-index for layering order.
	//
	// Português: Z-index inicial para a ordem de camadas.
	Index int

	// Visible determines whether the element is drawn.
	//
	// Português: Determina se o elemento é desenhado.
	Visible bool

	// DragEnable allows the element to be dragged.
	//
	// Português: Permite que o elemento seja arrastado.
	DragEnable bool

	// ResizeEnable allows the element to be resized via edge/corner handles.
	//
	// Português: Permite que o elemento seja redimensionado pelas alças de borda/canto.
	ResizeEnable bool

	// SvgXml is the raw SVG XML string to be rendered and cached.
	//
	// Português: String XML do SVG bruto a ser renderizado e cacheado.
	SvgXml string

	// PointerEventsIgnored makes the element transparent to hit-testing:
	// the stage's event dispatcher will skip this element when looking
	// up the element under the pointer, so clicks, drags, and hovers
	// pass straight through to whatever is below.
	//
	// The element is still drawn normally — only input is ignored. This
	// is for purely visual overlays (warning marks, highlights, child-
	// bounds indicators during resize) that must not steal interaction
	// from the underlying device.
	//
	// Zero value is false: by default elements receive pointer events.
	//
	// Português:
	//
	//	Torna o elemento transparente ao hit-testing: o dispatcher do
	//	stage pula este elemento ao procurar quem está sob o ponteiro.
	//	Clicks, drags e hovers passam direto para quem estiver embaixo.
	//	O elemento continua sendo desenhado normalmente — apenas o input
	//	é ignorado. Para overlays puramente visuais (warning marks,
	//	destaques, indicadores) que não devem roubar a interação do
	//	device subjacente.
	PointerEventsIgnored bool

	// ResizeHandleSize defines the pixel size of the interactive resize handles area.
	// Default: 8.
	//
	// Português: Define o tamanho em pixels da área interativa das alças de redimensionamento.
	// Padrão: 8.
	ResizeHandleSize float64

	// MinWidth is the minimum allowed width when resizing. Default: 10.
	//
	// Português: Largura mínima permitida ao redimensionar. Padrão: 10.
	MinWidth float64

	// MinHeight is the minimum allowed height when resizing. Default: 10.
	//
	// Português: Altura mínima permitida ao redimensionar. Padrão: 10.
	MinHeight float64
}

// StageConfig
//
// English:
//
//	Configuration for creating a new Stage. The CanvasID is the HTML element ID
//	of the canvas to use.
//
// Português:
//
//	Configuração para criar um novo Stage. O CanvasID é o ID do elemento HTML
//	do canvas a ser utilizado.
type StageConfig struct {
	// CanvasID is the HTML id attribute of the target canvas element.
	//
	// Português: Atributo id HTML do elemento canvas alvo.
	CanvasID string

	// Width is the canvas width in pixels. If zero, uses the current canvas width.
	//
	// Português: Largura do canvas em pixels. Se zero, usa a largura atual do canvas.
	Width int

	// Height is the canvas height in pixels. If zero, uses the altura atual do canvas.
	//
	// Português: Altura do canvas em pixels. Se zero, usa a altura atual do canvas.
	Height int

	// DoubleClickInterval is the maximum time in milliseconds between two taps/clicks
	// to be considered a double-click. Default: 300.
	//
	// Português: Tempo máximo em milissegundos entre dois toques/cliques para ser
	// considerado double-click. Padrão: 300.
	DoubleClickInterval int64

	// DragThreshold is the minimum distance in pixels the pointer must move before
	// a drag operation begins. Prevents accidental drags on click. Default: 4.
	//
	// Português: Distância mínima em pixels que o ponteiro deve percorrer antes de
	// iniciar uma operação de arraste. Previne arrastes acidentais ao clicar. Padrão: 4.
	DragThreshold float64

	// BackgroundColor is the CSS color used to clear the canvas on each frame.
	// Default: "transparent".
	//
	// Português: Cor CSS usada para limpar o canvas a cada frame.
	// Padrão: "transparent".
	BackgroundColor string
}
