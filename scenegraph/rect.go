// /ide/scenegraph/rect.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package scenegraph

// Rect is an axis-aligned bounding box in world coordinates.
//
// English:
//
//	Rect is the scenegraph's internal geometry primitive. It is kept local
//	to the package so that scenegraph has no outward dependencies on scene
//	or rulesContainer. The Serializer converts between its own scene.Rect
//	and this type at the boundary.
//
// Português:
//
//	Rect é o tipo geométrico primitivo do scenegraph, mantido local ao
//	package para que o scenegraph não tenha dependências de saída.
type Rect struct {
	X, Y, W, H float64
}

// ContainsRect reports whether inner is fully enclosed by r.
//
// English:
//
//	Boundary contact counts as inside. A child whose edge exactly meets its
//	parent's inner edge is a valid child, not a straddle. This asymmetry
//	with IntersectsRect (exclusive) is deliberate.
//
// Português:
//
//	Contato de borda conta como dentro. Um filho cuja borda encosta
//	exatamente na borda do pai é um filho válido, não um straddle.
func (r Rect) ContainsRect(inner Rect) bool {
	return inner.X >= r.X &&
		inner.Y >= r.Y &&
		inner.X+inner.W <= r.X+r.W &&
		inner.Y+inner.H <= r.Y+r.H
}

// IntersectsRect reports whether r and other share positive area.
//
// English:
//
//	Touching edges do NOT count as intersecting. Two devices whose edges
//	just kiss are not in conflict.
//
// Português:
//
//	Bordas que apenas se tocam NÃO contam como interseção. Dois devices
//	cujas bordas encostam não estão em conflito.
func (r Rect) IntersectsRect(other Rect) bool {
	return r.X < other.X+other.W &&
		r.X+r.W > other.X &&
		r.Y < other.Y+other.H &&
		r.Y+r.H > other.Y
}

// Area returns W * H. Returns 0 for degenerate rectangles (W <= 0 or
// H <= 0).
//
// Português: Retorna W * H. Retorna 0 para retângulos degenerados.
func (r Rect) Area() float64 {
	if r.W <= 0 || r.H <= 0 {
		return 0
	}
	return r.W * r.H
}

// Union returns the smallest Rect enclosing both r and other.
//
// Português: Retorna o menor Rect que contém r e other.
func (r Rect) Union(other Rect) Rect {
	minX := r.X
	if other.X < minX {
		minX = other.X
	}
	minY := r.Y
	if other.Y < minY {
		minY = other.Y
	}
	maxX := r.X + r.W
	if other.X+other.W > maxX {
		maxX = other.X + other.W
	}
	maxY := r.Y + r.H
	if other.Y+other.H > maxY {
		maxY = other.Y + other.H
	}
	return Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// IsZero reports whether the rectangle has zero area and zero position.
// Used as a sentinel when a device has not been fully initialised.
//
// Português: Informa se o retângulo é todo zero (sentinela para device
// não-inicializado).
func (r Rect) IsZero() bool {
	return r.X == 0 && r.Y == 0 && r.W == 0 && r.H == 0
}
