package rulesContainer

// rulesContainer — Pure rule functions for container behavior.
//
// English:
//
//	Each function has a single responsibility and operates on plain
//	geometric values (position, size, padding). No dependencies on
//	sprite, device, or scene packages — only math.
//
//	Rules:
//	  - ClampResize:     prevent container from shrinking past children
//	  - DragChildDelta:  compute delta to move children along with container
//	  - ChildrenBounds:  compute tight bounding box of a set of rectangles
//	  - MinSizeForChildren: minimum container size to enclose children
//
// Português:
//
//	Cada função tem responsabilidade única e opera sobre valores
//	geométricos simples (posição, tamanho, padding). Sem dependências
//	de sprite, device ou scene — apenas matemática.
//
//	Regras:
//	  - ClampResize:     impede que o container encolha além dos filhos
//	  - DragChildDelta:  calcula delta para mover filhos junto com o container
//	  - ChildrenBounds:  calcula bounding box justa de um conjunto de retângulos
//	  - MinSizeForChildren: tamanho mínimo do container para conter os filhos

// Rect is a simple rectangle (position + size).
type Rect struct {
	X, Y, W, H float64
}

// Padding defines the distance from container outer edge to inner usable area.
//
// English:
//
//	The inner bbox is computed as:
//	  inner.X = outer.X + Left
//	  inner.Y = outer.Y + Top
//	  inner.W = outer.W - Left - Right
//	  inner.H = outer.H - Top  - Bottom
//
// Português:
//
//	A inner bbox é calculada como:
//	  inner.X = outer.X + Left
//	  inner.Y = outer.Y + Top
//	  inner.W = outer.W - Left - Right
//	  inner.H = outer.H - Top  - Bottom
type Padding struct {
	Left, Right, Top, Bottom float64
}

// ChildrenBounds returns the tight bounding box around a set of rectangles.
// Returns a zero Rect if the slice is empty.
//
// Português: Retorna a bounding box justa de um conjunto de retângulos.
func ChildrenBounds(children []Rect) Rect {
	if len(children) == 0 {
		return Rect{}
	}

	minX := children[0].X
	minY := children[0].Y
	maxX := children[0].X + children[0].W
	maxY := children[0].Y + children[0].H

	for _, c := range children[1:] {
		if c.X < minX {
			minX = c.X
		}
		if c.Y < minY {
			minY = c.Y
		}
		if c.X+c.W > maxX {
			maxX = c.X + c.W
		}
		if c.Y+c.H > maxY {
			maxY = c.Y + c.H
		}
	}

	return Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// MinSizeForChildren returns the minimum container outer size (width, height)
// required to fully enclose the given children bounding box, considering
// padding and a safety margin.
//
// This is the absolute minimum — the container cannot be smaller than this
// regardless of which handle is being dragged.
//
// Português: Retorna o tamanho mínimo externo (largura, altura) do container
// necessário para conter todos os filhos, considerando padding e margem de
// segurança.
func MinSizeForChildren(childBounds Rect, pad Padding, margin float64) (minW, minH float64) {
	minW = childBounds.W + 2*margin + pad.Left + pad.Right
	minH = childBounds.H + 2*margin + pad.Top + pad.Bottom
	return
}

// ClampResize adjusts the container's position and size so that all children
// remain fully inside the container's inner area.
//
// Parameters:
//
//	container  — proposed new container outer rectangle (after user resize)
//	childBounds — tight bounding box of all children (world coordinates)
//	pad        — padding from outer edge to inner usable area
//	margin     — extra safety margin around children
//
// Returns the adjusted container rectangle. Children do NOT move — only the
// container edges are clamped.
//
// The function clamps each edge independently:
//
//	left edge:   cannot move right of (childBounds.X - margin - pad.Left)
//	top edge:    cannot move below    (childBounds.Y - margin - pad.Top)
//	right edge:  cannot move left of  (childBounds.X + childBounds.W + margin + pad.Right)
//	bottom edge: cannot move above    (childBounds.Y + childBounds.H + margin + pad.Bottom)
//
// Português: Ajusta posição e tamanho do container para que todos os filhos
// permaneçam dentro da área interna. Os filhos NÃO se movem — apenas as
// bordas do container são limitadas.
func ClampResize(container Rect, childBounds Rect, pad Padding, margin float64) Rect {
	// Required inner-area limits (world coordinates).
	needLeft := childBounds.X - margin
	needTop := childBounds.Y - margin
	needRight := childBounds.X + childBounds.W + margin
	needBottom := childBounds.Y + childBounds.H + margin

	// Current container edges in world coordinates.
	cLeft := container.X
	cTop := container.Y
	cRight := container.X + container.W
	cBottom := container.Y + container.H

	// Inner edges given current container.
	innerLeft := cLeft + pad.Left
	innerTop := cTop + pad.Top
	innerRight := cRight - pad.Right
	innerBottom := cBottom - pad.Bottom

	// Clamp each edge if it would exclude children.
	if innerLeft > needLeft {
		cLeft = needLeft - pad.Left
	}
	if innerTop > needTop {
		cTop = needTop - pad.Top
	}
	if innerRight < needRight {
		cRight = needRight + pad.Right
	}
	if innerBottom < needBottom {
		cBottom = needBottom + pad.Bottom
	}

	return Rect{
		X: cLeft,
		Y: cTop,
		W: cRight - cLeft,
		H: cBottom - cTop,
	}
}

// DragChildDelta computes the (dx, dy) delta that children should move
// when their container is dragged from oldPos to newPos.
//
// Português: Calcula o delta (dx, dy) que os filhos devem se mover
// quando o container é arrastado de oldPos para newPos.
func DragChildDelta(oldX, oldY, newX, newY float64) (dx, dy float64) {
	return newX - oldX, newY - oldY
}

// =====================================================================
//  Device-specific paddings
//
//  Português: Paddings específicos de cada device
// =====================================================================

// LoopPadding returns the Padding for StatementLoop, derived from
// doubleLoopArrow.go geometry:
//
//	margin  = 10   (gap between element edge and background path)
//	stroke  =  5   (arrow border width, drawn centered on the path)
//	r       = 20   (corner radius)
//	s       = 40   (arrow stub length)
//	stopBtn at (w-57, h-42)
//
// Inner bbox padding:
//
//	left/right:  margin(10) + stroke/2(2.5) + safety(7.5) = 20
//	top:         margin(10) + stroke/2(2.5) + safety(7.5) = 20
//	bottom:      20 + stopButton clearance(35) = 55
//
// Português: Retorna o Padding para StatementLoop, derivado da
// geometria de doubleLoopArrow.go.
func LoopPadding() Padding {
	return Padding{Left: 20, Right: 20, Top: 20, Bottom: 20}
}

// LoopChildMargin is the safety margin around children inside a Loop.
//
// Português: Margem de segurança ao redor dos filhos dentro de um Loop.
const LoopChildMargin = 5.0

// IfElsePadding returns the Padding for StatementIfElse.
// Top padding is larger to account for the toggle pill area.
//
// Português: Retorna o Padding para StatementIfElse.
// O padding superior é maior para acomodar a área do toggle.
func IfElsePadding() Padding {
	return Padding{Left: 20, Right: 20, Top: 45, Bottom: 20}
}

// ClampToParent adjusts a child device's outer rectangle so it stays fully
// inside the parent container's inner rectangle. Each edge is independently
// clamped — the child cannot extend beyond the parent's usable area.
//
// Parameters:
//
//	child       — the child device's outer rectangle (position + size)
//	parentInner — the parent container's inner bbox (usable area)
//
// Returns the adjusted child rectangle. If the child is already fully inside,
// it is returned unchanged. If the child is larger than the parent inner area,
// it is clamped to exactly fit (shrunk to parent inner size).
//
// This function is called during resize to prevent nested containers from
// growing beyond their parent. Without it, resizing a Loop inside an IfElse
// (or vice versa) causes the inner container to jump outside the outer one.
//
// Português: Ajusta o retângulo externo de um device filho para que ele
// permaneça totalmente dentro do retângulo interno do container pai.
// Chamada durante resize para impedir que containers aninhados cresçam
// além do pai.
func ClampToParent(child Rect, parentInner Rect) Rect {
	// Clamp position: child cannot start before parent inner area
	if child.X < parentInner.X {
		child.X = parentInner.X
	}
	if child.Y < parentInner.Y {
		child.Y = parentInner.Y
	}

	// Clamp size: child cannot extend beyond parent inner area
	maxW := parentInner.X + parentInner.W - child.X
	if maxW < 0 {
		maxW = 0
	}
	if child.W > maxW {
		child.W = maxW
	}

	maxH := parentInner.Y + parentInner.H - child.Y
	if maxH < 0 {
		maxH = 0
	}
	if child.H > maxH {
		child.H = maxH
	}

	return child
}
