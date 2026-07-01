// /ide/scene/types.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package scene

// types.go
//
// Defines all types for the scene serialization system.
//
// English:
//
//	The scene package produces the canonical JSON representation of the
//	stage. It does NOT compute containment relationships; that is the
//	scenegraph's job. The serializer reads from a *scenegraph.Graph,
//	translates each NodeView into a DeviceJSON, and emits the envelope.
//
//	The SceneDevice interface is the contract every device on the stage
//	must satisfy. It carries the bare minimum for identification,
//	geometry and classification. Optional interfaces (Labeled, Movable,
//	Propertied, LiveUpdatable, Inspectable, Padded) enrich the device
//	with additional capabilities that the serializer or the graph pick
//	up via type assertion.
//
// Português:
//
//	O package scene produz a representação JSON canônica da stage.
//	NÃO computa containment — isso é trabalho do scenegraph. O
//	serializer lê de um *scenegraph.Graph e traduz cada NodeView para
//	um DeviceJSON.

import (
	"github.com/helmutkemper/iotmakerio/rulesContainer"
	"github.com/helmutkemper/iotmakerio/scenegraph"
)

// =====================================================================
//  Core geometry types
// =====================================================================

// Rect represents a rectangular bounding box in world coordinates.
//
// Português: Retângulo com posição e tamanho em coordenadas de mundo.
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// ConflictHighlight is the scene-level mirror of
// scenegraph.ConflictHighlight, kept here so consumers (workspace,
// renderers) import only the scene package and never have to reach
// across into scenegraph for a type. Kind uses the stringified form
// of the ConflictKind for the same reason — string values travel
// easily to JS and JSON.
//
// Fields match the scenegraph version: DeviceID/DeviceRect are the
// offender (always populated), ContainerID/ContainerRect name the
// container area the offender should fit into (populated only for
// Straddle / PiercedOuter).
//
// Português: Espelho do ConflictHighlight do scenegraph, com Rect no
// tipo scene.Rect e Kind em string. Device é sempre o culpado;
// Container é preenchido quando o conflito envolve container.
type ConflictHighlight struct {
	DeviceID      string
	DeviceRect    Rect
	ContainerID   string // "" for Overlap
	ContainerRect *Rect  // nil for Overlap
	Kind          string // "overlap" | "straddle" | "pierced_outer"
}

// Contains returns true if other is fully inside r. Boundary contact
// counts as inside.
//
// Português: Retorna true se other está totalmente dentro de r. Contato
// de borda conta como dentro.
func (r Rect) Contains(other Rect) bool {
	return other.X >= r.X &&
		other.Y >= r.Y &&
		other.X+other.Width <= r.X+r.Width &&
		other.Y+other.Height <= r.Y+r.Height
}

// Overlaps returns true if r and other share positive area. Touching
// edges do NOT count as overlapping.
//
// Português: Retorna true se r e other compartilham área positiva.
// Bordas que apenas se tocam NÃO contam.
func (r Rect) Overlaps(other Rect) bool {
	return r.X < other.X+other.Width &&
		r.X+r.Width > other.X &&
		r.Y < other.Y+other.Height &&
		r.Y+r.Height > other.Y
}

// toGraphRect converts a scene.Rect to a scenegraph.Rect. Used at the
// boundary between the two packages.
//
// Português: Converte scene.Rect para scenegraph.Rect na fronteira
// entre os dois packages.
func (r Rect) toGraphRect() scenegraph.Rect {
	return scenegraph.Rect{X: r.X, Y: r.Y, W: r.Width, H: r.Height}
}

// =====================================================================
//  SceneDevice interface — the contract every stage device implements
// =====================================================================

// SceneDevice is the interface every device on the stage must implement
// so the scenegraph can observe its geometry and the serializer can
// produce its JSON entry.
//
// English:
//
//	The interface carries only the bare minimum: identity and geometry.
//	Classification is opt-in via the Kinded interface below — devices
//	that don't implement Kinded default to KindSimple, which is the
//	correct classification for every leaf device (constants, math
//	operators, comparisons, frontend widgets).
//
//	GetInnerBBox returns nil for non-container devices. Complex
//	devices return their border 3 rectangle.
//
// Português:
//
//	Interface mínima: identidade e geometria. Classificação é opt-in
//	via Kinded (abaixo); devices que não a implementam são tratados
//	como KindSimple, que é o correto para todo device-folha.
type SceneDevice interface {
	GetID() string
	GetDeviceType() string
	GetOuterBBox() Rect
	GetInnerBBox() *Rect
}

// Kinded is the optional interface a device implements to classify
// itself explicitly. When present, its GetKind return value drives
// every spatial decision the scenegraph makes about the device.
// When absent, the scenegraph defaults the device to KindSimple.
//
// Complex devices (Loop, IfElse) MUST implement Kinded and return
// KindComplex. Simple devices may implement it for documentation,
// but nothing breaks if they don't.
//
// Português:
//
//	Interface opcional para classificação explícita do device.
//	Complex (Loop, IfElse) DEVEM implementar; Simples podem
//	implementar por documentação, mas nada quebra se não.
type Kinded interface {
	GetKind() scenegraph.Kind
}

// Labeled is the optional interface for devices with a user-editable
// label. The serializer checks for it and, when present, emits the
// label into the JSON.
//
// Português: Interface opcional para devices com label editável pelo
// usuário. O serializer a consulta e emite o label no JSON.
type Labeled interface {
	GetLabel() string
}

// Movable is the optional interface for devices that can be relocated
// by the scenegraph — for example, when a Complex is dragged and its
// children travel with it. MoveBy must shift the device by (dx, dy)
// and handle all visual side-effects (warning mark, wire routing,
// ornament redraw).
//
// Every device that can become a child of a Complex MUST implement
// Movable.
//
// Português: Interface opcional para devices reposicionáveis pelo
// scenegraph. Todo device que pode ser filho de Complex deve
// implementá-la.
type Movable interface {
	MoveBy(dx, dy float64)
}

// Propertied is the optional interface for devices that carry codegen-
// relevant properties (e.g. a Bool's value, a comparison's operator).
// The serializer queries it and emits the properties map into the JSON.
//
// Português: Interface opcional para devices que carregam propriedades
// relevantes para codegen.
type Propertied interface {
	GetProperties() map[string]interface{}
}

// LiveUpdatable is the optional interface for devices that receive
// real-time data from external hardware via WebSocket. The live client
// performs a type assertion and calls LiveUpdate when a message
// arrives for the device.
//
// Português: Interface opcional para devices que recebem dados em
// tempo real do hardware via WebSocket.
type LiveUpdatable interface {
	LiveUpdate(port string, value []byte) error
}

// Inspectable is the optional interface for devices that support the
// property panel overlay. GetInspectConfig returns the overlay
// configuration (tabs, fields, help text). ApplyProperties is called
// when the user saves the form.
//
// The return type of GetInspectConfig is interface{} to avoid an
// import cycle with ui/overlay. The actual type returned must be
// overlay.Config.
//
// Português: Interface opcional para o painel de propriedades.
// Retorna interface{} para evitar ciclo de import com ui/overlay.
type Inspectable interface {
	GetInspectConfig() interface{}
	ApplyProperties(values map[string]string)
}

// Padded is the optional interface for Complex devices that carry
// container padding — the distance from border 1 to border 3. The
// scenegraph consults it during resize to clamp against the floor
// (children bounding box + padding). Simple and Fitting devices do
// not need to implement it.
//
// Português: Interface opcional para Complex que têm padding de
// container. O scenegraph consulta durante resize para aplicar o
// piso (bounding box dos filhos + padding).
type Padded interface {
	GetContainerPadding() rulesContainer.Padding
}

// VariableDeclarer is the optional interface for devices that declare a user
// variable (GetVar / SetVar). The serializer queries it and emits the
// declaration into the scene's top-level "variables" array (Path A), which the
// codegen pipeline reads to emit one zero-initialised declaration per distinct
// name. Name is the variable identifier; Type is the abstract type ("int",
// "float", "string"). An empty Name contributes nothing.
//
// Português: Interface opcional para devices que declaram uma variável do
// usuário (GetVar / SetVar). O serializer a consulta e emite a declaração no
// array "variables" no topo da cena (Path A), lido pelo codegen para gerar uma
// declaração zero-init por nome distinto. Name é o identificador; Type é o tipo
// abstrato ("int", "float", "string"). Name vazio não contribui.
type VariableDeclarer interface {
	GetVariableDecl() (name string, typ string)
}

// =====================================================================
//  JSON serialization types
// =====================================================================

// VariableJSON is one user-variable declaration embedded in the scene's
// top-level "variables" array (Path A). The codegen pipeline reads these to
// emit a zero-initialised declaration per distinct name. The field names mirror
// server/codegen/ir.VariableDecl exactly ("name", "type") so the codegen can
// re-unmarshal the scene into []ir.VariableDecl directly.
//
// Português: Uma declaração de variável do usuário embutida no array
// "variables" no topo da cena (Path A). Os nomes dos campos espelham
// server/codegen/ir.VariableDecl ("name", "type").
type VariableJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SceneJSON is the top-level JSON structure for the entire scene.
type SceneJSON struct {
	Version   string         `json:"version"`
	Metadata  MetadataJSON   `json:"metadata"`
	Devices   []DeviceJSON   `json:"devices"`
	Wires     []WireJSON     `json:"wires"`
	Variables []VariableJSON `json:"variables,omitempty"`
}

// MetadataJSON holds canvas-level and project-level state at export time.
//
// Language carries the project's compile target — "c" (C99) or
// "go". It mirrors the language column on the parent stage_files
// row; storing it inside the scene JSON too lets the codegen
// pipeline read it without needing to query the database (the
// /api/v1/codegen endpoints only receive the scene JSON, not the
// file metadata). The IDE writes this field on every save so the
// two sources never drift apart.
//
// TargetProfile picks per-architecture type/literal choices for
// the C backend (arduino_uno, cortex_m, pi_linux, portable).
// Unused by the Go backend. Optional — empty string resolves to
// arduino_uno on the C backend side.
//
// Português: Language guarda a linguagem do projeto ("c" ou "go") —
// espelha o stage_files.language e evita que o pipeline de codegen
// precise consultar o DB. TargetProfile escolhe o perfil de tipos
// para o backend C; vazio cai em arduino_uno.
type MetadataJSON struct {
	Density       int                   `json:"density"`
	CanvasWidth   int                   `json:"canvasWidth"`
	CanvasHeight  int                   `json:"canvasHeight"`
	Camera        CameraJSON            `json:"camera"`
	Cameras       map[string]CameraJSON `json:"cameras,omitempty"`
	Language      string                `json:"language,omitempty"`
	TargetProfile string                `json:"targetProfile,omitempty"`
}

// CameraJSON represents camera state in JSON.
type CameraJSON struct {
	OffsetX float64 `json:"offsetX"`
	OffsetY float64 `json:"offsetY"`
	Zoom    float64 `json:"zoom"`
}

// DeviceJSON represents a single device in the JSON export.
//
// The Kind field replaces the old OverlapPolicy. Containment (parent,
// children, status) comes from the scenegraph; the serializer fills
// these fields by reading the graph snapshot.
type DeviceJSON struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	Kind             string                 `json:"kind"`
	Stage            string                 `json:"stage,omitempty"`
	Label            string                 `json:"label,omitempty"`
	Properties       map[string]interface{} `json:"properties,omitempty"`
	Position         PointJSON              `json:"position"`
	FrontendPosition *PointJSON             `json:"frontendPosition,omitempty"`
	Size             SizeJSON               `json:"size"`
	OuterBBox        RectJSON               `json:"outerBBox"`
	InnerBBox        *RectJSON              `json:"innerBBox"`
	Connectors       []ConnectorJSON        `json:"connectors"`
	Containment      ContainmentJSON        `json:"containment"`
}

// PointJSON represents a 2D point.
type PointJSON struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// SizeJSON represents a width/height pair.
type SizeJSON struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// RectJSON represents a rectangular region.
type RectJSON struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ConnectorJSON represents a single connector in the JSON export.
type ConnectorJSON struct {
	Port               string              `json:"port"`
	DataType           string              `json:"dataType"`
	IsOutput           bool                `json:"isOutput"`
	AcceptNotConnected bool                `json:"acceptNotConnected"`
	Position           PointJSON           `json:"position"`
	Connections        []ConnectionRefJSON `json:"connections"`
}

// ConnectionRefJSON represents a connection to another device's port.
type ConnectionRefJSON struct {
	WireID       string `json:"wireId"`
	TargetDevice string `json:"targetDevice"`
	TargetPort   string `json:"targetPort"`
}

// ContainmentJSON describes the spatial relationships of a device.
//
// Status possible values:
//   - "container"     — device is Complex (may or may not have children)
//   - "contained"     — device has a parent and no conflicts
//   - "error"         — device has one or more current conflicts
//   - "free"          — device is at root with no conflicts
//
// Conflicts, when non-empty, list every peer involved in the error.
type ContainmentJSON struct {
	IsContainer bool           `json:"isContainer"`
	Children    []string       `json:"children,omitempty"`
	Parent      string         `json:"parent,omitempty"`
	Status      string         `json:"status"`
	Conflicts   []ConflictJSON `json:"conflicts,omitempty"`
}

// ConflictJSON represents a single spatial conflict in the JSON export.
//
// Português: Representa um único conflito espacial na exportação JSON.
type ConflictJSON struct {
	With string `json:"with"`
	Kind string `json:"kind"`
}

// WireJSON represents a wire connection in the JSON export.
type WireJSON struct {
	ID       string       `json:"id"`
	From     EndpointJSON `json:"from"`
	To       EndpointJSON `json:"to"`
	DataType string       `json:"dataType"`
}

// EndpointJSON represents one end of a wire.
type EndpointJSON struct {
	Device string `json:"device"`
	Port   string `json:"port"`
}
