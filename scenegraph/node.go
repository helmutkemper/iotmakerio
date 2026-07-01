// /ide/scenegraph/node.go

package scenegraph

// InteractionState is the per-device transient state during a user gesture.
//
// English:
//
//	A device is Idle most of the time. It becomes Dragging between BeginDrag
//	and EndDrag, and Resizing between BeginResize and EndResize. The state
//	is used by the graph to decide what caches to populate and when.
//
// Português:
//
//	Um device fica Idle a maior parte do tempo. Passa a Dragging entre
//	BeginDrag e EndDrag, e a Resizing entre BeginResize e EndResize.
type InteractionState int

const (
	// Idle — no gesture in progress.
	Idle InteractionState = iota

	// Dragging — BeginDrag has been called; EndDrag has not.
	Dragging

	// Resizing — BeginResize has been called; EndResize has not.
	Resizing
)

// ConflictKind describes why two devices are in spatial conflict. Each
// value corresponds to one of the error configurations enumerated in
// ARCHITECTURE_SCENEGRAPH.md section 4.
//
// Português: Descreve por que dois devices estão em conflito espacial.
type ConflictKind int

const (
	// ConflictOverlap — border 1 of A intersects border 1 of B, and
	// neither is a Complex that fully contains the other. Covers
	// Simple+Simple, Simple+Complex lateral, and Complex+Complex lateral.
	//
	// Português: Sobreposição lateral; border 1 de A intersecta border 1
	// de B, nenhum é Complex que contém o outro.
	ConflictOverlap ConflictKind = iota

	// ConflictStraddle — B is Complex and border 1 of A intersects border
	// 3 of B without being fully inside it. A has one foot in, one foot
	// out of the container's usable area.
	//
	// Português: B é Complex e border 1 de A cruza border 3 de B sem
	// estar totalmente dentro. A está parcialmente na área útil de B.
	ConflictStraddle

	// ConflictPiercedOuter — B is Complex and border 1 of A sits partly
	// inside border 1 of B but outside border 3 of B. A is inside the
	// visual frame of B (between its border 1 and border 3) without
	// qualifying as a child.
	//
	// Português: B é Complex e border 1 de A está parcialmente dentro de
	// border 1 de B mas fora de border 3 de B. A está no "vão" visual.
	ConflictPiercedOuter
)

// String returns a short lowercase label for the conflict kind, used in
// logs and in the serialised JSON.
func (c ConflictKind) String() string {
	switch c {
	case ConflictOverlap:
		return "overlap"
	case ConflictStraddle:
		return "straddle"
	case ConflictPiercedOuter:
		return "pierced_outer"
	default:
		return "unknown"
	}
}

// Conflict identifies a single spatial error between two devices. The
// subject of the conflict is implicit (the device the conflict list
// belongs to); With holds the other party's ID.
//
// Português: Identifica um único conflito espacial entre dois devices.
type Conflict struct {
	With string
	Kind ConflictKind
}

// Node is the graph's live view of a device. Geometry (Outer, Inner) is
// refreshed from the underlying DeviceRef during UpdateDrag and
// UpdateResize. Parent/children links are owned by the Graph and must
// never be written by code outside the Graph's methods.
//
// Português: Visão viva do grafo sobre um device. Geometria espelhada do
// DeviceRef; parent/children são gerenciados pelo Graph.
type Node struct {
	// Identity and classification — immutable after Register.
	ID   string
	Kind Kind

	// Mirrored geometry — refreshed by the graph during interaction.
	Outer Rect  // border 1
	Inner *Rect // border 3; nil when Kind != KindComplex

	// Graph relationships — owned by the Graph.
	ParentID    string   // "" if the device is at root
	ChildrenIDs []string // populated only when Kind == KindComplex

	// Transient gesture state — valid only between Begin* and End*.
	Interaction InteractionState

	// dragSnapshot holds the flat list of every descendant present when
	// BeginDrag was called (children, grandchildren, etc., transitively).
	// On EndDrag, every ID in this list receives the cumulative (dx, dy)
	// of the gesture. Cleared on EndDrag.
	dragSnapshot []string

	// resizeFloor is the bounding box of all children's border 1 at
	// BeginResize; the container cannot shrink past this. nil when the
	// container has no children. Cleared on EndResize.
	resizeFloor *Rect

	// resizeCeiling is the parent's border 3 at BeginResize; the
	// container cannot grow past this. nil when the container has no
	// parent. Cleared on EndResize.
	resizeCeiling *Rect

	// hidden marks a node that is currently not shown (e.g. a device in the
	// inactive branch of an IfElse container). A hidden node is EXCLUDED from
	// candidates(), so it takes part in no conflict, parenting, or snap query —
	// its visible-branch peers must not collide with it, and nothing nests into
	// it. Its parent/child links and geometry are preserved, so it still moves
	// rigidly with its container during a drag; only spatial QUERIES skip it.
	// Toggled by SetHidden.
	//
	// Português: marca um nó atualmente não exibido (ex: device na branch
	// inativa de um IfElse). É EXCLUÍDO de candidates() — não entra em colisão,
	// parenting nem snap. Vínculos pai/filho e geometria são preservados (ainda
	// se move junto do container no drag); só as CONSULTAS espaciais o ignoram.
	hidden bool

	// lastNotifiedConflicts is a deduplication cache: the observer is
	// only fired when the conflict set differs from the value here.
	// Internal; never read by code outside graph.go.
	lastNotifiedConflicts []Conflict
}

// NodeView is a read-only snapshot of a Node, used by the serializer at
// export time. Immutable after construction.
//
// Português: Snapshot somente-leitura de um Node, usado pelo serializer
// no momento da exportação.
type NodeView struct {
	ID          string
	Kind        Kind
	Outer       Rect
	Inner       *Rect
	ParentID    string
	ChildrenIDs []string
	Conflicts   []Conflict
}

// ConflictHighlight is a single draw instruction produced by the graph
// for any renderer that wants to surface an active conflict visually.
// It is a pure-data type — no DOM, no canvas context, no coupling to
// any specific rendering technology.
//
// The graph emits one ConflictHighlight per conflicting device. Each
// entry carries:
//
//   - DeviceID / DeviceRect: the offender. Always populated so the
//     renderer can highlight the device the maker needs to move.
//   - ContainerID / ContainerRect: when the conflict involves a
//     container (Straddle, PiercedOuter), these name the container
//     and its inner rect — the usable area the offender is supposed
//     to fit into. For pure Simple-Simple Overlap there is no
//     container and both fields are zero / nil.
//   - Kind: the most severe conflict affecting the device, so the
//     renderer can pick a colour.
//
// Painting both rectangles gives the maker two pieces of information
// in a single glance: "this device is wrong" and "this is where it
// should fit".
//
// Português: Instrução de desenho emitida pelo grafo. Apenas dados,
// sem DOM/canvas. DeviceID/DeviceRect sempre preenchidos (o culpado).
// ContainerID/ContainerRect quando envolve container (Straddle/
// PiercedOuter). Pintar os dois mostra o erro e onde deveria estar.
type ConflictHighlight struct {
	DeviceID      string
	DeviceRect    Rect
	ContainerID   string // "" when Kind is Overlap (no container)
	ContainerRect *Rect  // nil when Kind is Overlap
	Kind          ConflictKind
}
