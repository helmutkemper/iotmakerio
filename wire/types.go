// wire/types.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import "fmt"

// =====================================================================
//  Connector Identification | Identificação de Conector
// =====================================================================

// ConnectorID uniquely identifies a connection point in the workspace.
// It combines the parent element's ID with the port name.
//
// Português:
//
//	ConnectorID identifica unicamente um ponto de conexão no workspace.
//	Combina o ID do elemento pai com o nome da porta.
type ConnectorID struct {
	// ElementID is the unique identifier of the parent component (e.g., "stmAdd_1").
	//
	// Português: Identificador único do componente pai (ex: "stmAdd_1").
	ElementID string

	// PortName is the name of the connection port (e.g., "inputX", "output").
	//
	// Português: Nome da porta de conexão (ex: "inputX", "output").
	PortName string
}

// String returns a human-readable representation of the connector ID.
//
// Português: Retorna uma representação legível do ID do conector.
func (c ConnectorID) String() string {
	return fmt.Sprintf("%s.%s", c.ElementID, c.PortName)
}

// Key returns a unique string key suitable for use as a map key.
//
// Português: Retorna uma chave string única adequada para uso como chave de map.
func (c ConnectorID) Key() string {
	return c.ElementID + ":" + c.PortName
}

// =====================================================================
//  Connector Info | Informações do Conector
// =====================================================================

// ConnectorInfo describes a registered connection point on a component.
//
// Português:
//
//	ConnectorInfo descreve um ponto de conexão registrado em um componente.
type ConnectorInfo struct {
	// ID uniquely identifies this connector.
	//
	// Português: Identifica unicamente este conector.
	ID ConnectorID

	// IsOutput is true for output connectors, false for input connectors.
	//
	// Português: True para conectores de saída, false para conectores de entrada.
	IsOutput bool

	// AllowedTypes lists the data types this connector accepts or produces.
	// A connector may support multiple types (e.g., ["int", "float"]).
	//
	// Português: Lista os tipos de dados que este conector aceita ou produz.
	// Um conector pode suportar múltiplos tipos (ex: ["int", "float"]).
	AllowedTypes []string

	// CallbackType, when non-empty, marks this connector as a CALLBACK port
	// (the wire-ƒ): a function-reference output (the ƒ device's `callback`
	// pin) or a callback input that accepts one (e.g. setDisplay.writer). The
	// value is the function-pointer typedef (e.g. "display_write_fn"); it
	// equals the single entry in AllowedTypes, so the existing exact-match
	// type rule already restricts these to each other. This flag exists for
	// the RENDERER to distinguish callback ports/wires (dashed stroke, ƒ
	// glyph); it does not change connection compatibility. Empty for ordinary
	// data ports.
	//
	// Português: Quando não-vazio, marca este conector como porta de CALLBACK
	// (o wire-ƒ). Usado pelo renderer para diferenciar (tracejado, glyph ƒ);
	// não altera a compatibilidade de conexão. Vazio para portas comuns.
	CallbackType string

	// AcceptNotConnected when false, generates a compilation error if this
	// connector has no wire attached.
	//
	// Português: Quando false, gera um erro de compilação se este conector
	// não tiver nenhum fio conectado.
	AcceptNotConnected bool

	// Locked when true, prevents new connections to/from this connector.
	//
	// Português: Quando true, impede novas conexões de/para este conector.
	Locked bool

	// MaxConnections is the maximum number of wires allowed on this connector.
	// 0 means unlimited (typical for outputs). 1 means single connection (typical for inputs).
	//
	// Português: Número máximo de fios permitidos neste conector.
	// 0 significa ilimitado (típico para saídas). 1 significa conexão única (típico para entradas).
	MaxConnections int

	// PositionFunc returns the absolute canvas position of this connector.
	// It is a function because the parent component can move or resize.
	//
	// Português: Retorna a posição absoluta no canvas deste conector.
	// É uma função porque o componente pai pode se mover ou redimensionar.
	PositionFunc func() (canvasX float64, canvasY float64)

	// Label is a human-readable name for this connector (e.g., "Input X").
	//
	// Português: Nome legível para humanos deste conector (ex: "Input X").
	Label string
}

// =====================================================================
//  Wire | Fio
// =====================================================================

// Wire represents a connection between an output connector and an input connector.
//
// Português:
//
//	Wire representa uma conexão entre um conector de saída e um conector de entrada.
type Wire struct {
	// ID is a unique identifier for this wire.
	//
	// Português: Identificador único deste fio.
	ID string

	// From is the output connector (source).
	//
	// Português: Conector de saída (origem).
	From ConnectorID

	// To is the input connector (destination).
	//
	// Português: Conector de entrada (destino).
	To ConnectorID

	// DataType is the resolved data type flowing through this wire.
	// Determined at connection time from the compatible type between From and To.
	//
	// Português: Tipo de dado resolvido que flui por este fio.
	// Determinado no momento da conexão pelo tipo compatível entre From e To.
	DataType string

	// CallbackType, when non-empty, marks this wire as a CALLBACK connection
	// (the wire-ƒ): it carries a function reference (the ƒ device's output)
	// into a callback input rather than a data value. Set at creation when
	// either endpoint is a callback connector (see applyCallbackWireStyle).
	// Lets the renderer style it distinctly (dashed stroke, ƒ endpoint glyph).
	// Empty for ordinary data wires.
	//
	// Português: Quando não-vazio, marca este fio como conexão de CALLBACK
	// (o wire-ƒ) — carrega uma referência de função, não um valor. Setado na
	// criação quando uma ponta é conector de callback. Vazio para fios comuns.
	CallbackType string

	// Style defines the visual appearance of this wire.
	//
	// Português: Define a aparência visual deste fio.
	Style WireStyle

	// Waypoints are the ordered points that define the wire's path.
	// The first point is at the output connector, the last at the input connector.
	// Intermediate points form the Manhattan route (horizontal/vertical segments).
	//
	// Português: São os pontos ordenados que definem o caminho do fio.
	// O primeiro ponto está no conector de saída, o último no conector de entrada.
	// Pontos intermediários formam a rota Manhattan (segmentos horizontais/verticais).
	Waypoints []Point

	// Tunnel, when non-nil, marks this wire as crossing a container border:
	// the route passes THROUGH the tunnel point (on the border) as two
	// Manhattan segments (source→tunnel, tunnel→consumer), and a tunnel marker
	// is drawn there (LabVIEW-style). It is recomputed on every recalculate
	// from the source→target border crossing, unless Pinned — once the user
	// drags the tunnel it keeps its dragged position (a later slice). nil for
	// ordinary wires that cross no container.
	//
	// The wire stays ONE logical connection (source→consumer), so this is
	// purely about routing/visuals; codegen (scope-crossing → VAR hoist) is
	// unaffected.
	//
	// Português: Quando não-nil, marca o fio como cruzando a borda de um
	// container: a rota passa PELO ponto do túnel (na borda) como dois
	// segmentos Manhattan (fonte→túnel, túnel→consumidor), e um marcador é
	// desenhado ali. Recomputado a cada recalculate a partir do cruzamento
	// fonte→destino, exceto se Pinned (arrastado pelo usuário). nil para fios
	// que não cruzam container. O fio segue sendo UMA conexão lógica — só muda
	// visual/rota; codegen não muda.
	Tunnel *WireTunnel

	// Selected is true when the user has selected this wire (e.g., for deletion).
	//
	// Português: True quando o usuário selecionou este fio (ex: para exclusão).
	Selected bool
}

// WireTunnel is the LabVIEW-style tunnel a wire passes through where it crosses
// a container's border: a draggable point on that border. The wire routes as
// two Manhattan segments joined at Point.
//
// Português: O túnel estilo LabVIEW pelo qual um fio passa onde cruza a borda de
// um container: um ponto arrastável na borda. O fio roteia como dois segmentos
// Manhattan unidos em Point.
type WireTunnel struct {
	// ContainerID is the element ID of the container whose border this tunnel
	// sits on.
	//
	// Português: ID do elemento container em cuja borda o túnel está.
	ContainerID string

	// Point is the tunnel's world position, on the container's border.
	//
	// Português: Posição de mundo do túnel, na borda do container.
	Point Point

	// SplitIndex is the index of the tunnel point within the wire's Waypoints.
	// Waypoints[:SplitIndex+1] is the feed segment (source→tunnel) and
	// Waypoints[SplitIndex:] is the tap segment (tunnel→consumer). The renderer
	// uses it to draw the feed alone when the consumer's case is hidden.
	//
	// Português: Índice do ponto do túnel dentro de Waypoints. Waypoints[:Split
	// Index+1] é o feed (fonte→túnel) e Waypoints[SplitIndex:] é o tap
	// (túnel→consumidor). O renderer usa para desenhar só o feed quando a
	// condição do consumidor está oculta.
	SplitIndex int

	// Pinned is true once the user has dragged the tunnel: its position is then
	// kept (clamped to the border as the container moves) rather than
	// recomputed from the straight source→target crossing. Reserved for the
	// drag slice; auto-placed tunnels are not pinned.
	//
	// Português: True quando o usuário arrastou o túnel: a posição passa a ser
	// mantida (presa à borda) em vez de recomputada. Reservado para a fatia de
	// arrastar; túneis auto-posicionados não são pinned.
	Pinned bool
}

// =====================================================================
//  Wire Style | Estilo do Fio
// =====================================================================

// WireStyle defines the visual appearance of a wire.
//
// Português:
//
//	WireStyle define a aparência visual de um fio.
type WireStyle struct {
	// StrokeColor is the CSS color of the wire line (e.g., "#2196F3").
	//
	// Português: Cor CSS da linha do fio (ex: "#2196F3").
	StrokeColor string

	// StrokeWidth is the line width in pixels.
	//
	// Português: Largura da linha em pixels.
	StrokeWidth float64

	// DashPattern defines the dash pattern for the line.
	// nil or empty means a solid line. Example: []float64{5, 3} for dashed.
	//
	// Português: Define o padrão de traço da linha.
	// nil ou vazio significa linha sólida. Exemplo: []float64{5, 3} para tracejado.
	DashPattern []float64

	// SelectedColor is the highlight color when the wire is selected.
	//
	// Português: Cor de destaque quando o fio está selecionado.
	SelectedColor string

	// SelectedWidth is the line width when selected (typically wider).
	//
	// Português: Largura da linha quando selecionado (tipicamente mais larga).
	SelectedWidth float64

	// CornerRadius is the radius for rounded corners at Manhattan route bends.
	// 0 means sharp corners.
	//
	// Português: Raio para cantos arredondados nas dobras da rota Manhattan.
	// 0 significa cantos retos.
	CornerRadius float64
}

// =====================================================================
//  Wire Layer | Camada do Fio
// =====================================================================

// WireLayer defines whether wires are drawn above or below components.
//
// Português:
//
//	WireLayer define se os fios são desenhados acima ou abaixo dos componentes.
type WireLayer int

const (
	// WireLayerBelow draws wires before (below) components, like a PCB.
	//
	// Português: Desenha fios antes (abaixo) dos componentes, como um PCB.
	WireLayerBelow WireLayer = iota

	// WireLayerAbove draws wires after (above) components, like loose wires.
	//
	// Português: Desenha fios depois (acima) dos componentes, como fios soltos.
	WireLayerAbove
)

// =====================================================================
//  Point | Ponto
// =====================================================================

// Point represents a 2D coordinate on the canvas.
//
// Português:
//
//	Point representa uma coordenada 2D no canvas.
type Point struct {
	X float64
	Y float64
}

// =====================================================================
//  Connect Mode State | Estado do Modo de Conexão
// =====================================================================

// ConnectMode represents the state of the interactive connection flow.
//
// Português:
//
//	ConnectMode representa o estado do fluxo interativo de conexão.
type ConnectMode int

const (
	// ConnectModeIdle means no connection is being made.
	//
	// Português: Nenhuma conexão está sendo feita.
	ConnectModeIdle ConnectMode = iota

	// ConnectModeSelectingTarget means the user has initiated a connection from
	// a source connector and is choosing the target.
	//
	// Português: O usuário iniciou uma conexão de um conector de origem e está
	// escolhendo o destino.
	ConnectModeSelectingTarget
)

// =====================================================================
//  Candidate | Candidato
// =====================================================================

// Candidate represents a compatible target connector that can receive a wire
// from the current source connector.
//
// Português:
//
//	Candidate representa um conector de destino compatível que pode receber um fio
//	do conector de origem atual.
type Candidate struct {
	// Connector is the target connector info.
	//
	// Português: Informações do conector de destino.
	Connector ConnectorInfo

	// ResolvedType is the data type that would flow through the wire if connected.
	//
	// Português: Tipo de dado que fluiria pelo fio se conectado.
	ResolvedType string

	// Label is a display string for the selection menu (e.g., "stmSub_1 · inputX (int)").
	//
	// Português: String de exibição para o menu de seleção (ex: "stmSub_1 · inputX (int)").
	Label string
}
