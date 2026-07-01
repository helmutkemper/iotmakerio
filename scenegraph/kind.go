// /ide/scenegraph/kind.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package scenegraph

// Kind classifies a device by its containment behaviour.
//
// English:
//
//	Every device on the stage belongs to exactly one Kind. The Kind drives
//	all containment and collision decisions — which borders the device has,
//	what other devices it may contain, what spatial configurations are
//	legal between it and other devices.
//
//	The rules are:
//	  - Simple  — borders 1 and 2 only. Cannot hold children. Cannot be
//	              placed over another device.
//	  - Fitting — borders 1 and 2 only. Same collision rules as Simple,
//	              but earmarked for a future snap-to-neighbour behaviour
//	              (tabs and sockets drawn on border 2, collision still uses
//	              border 1). Not yet shipping; behaves identically to
//	              Simple in the current engine.
//	  - Complex — borders 1, 2 and 3. Can hold other devices inside its
//	              border 3.
//
// Português:
//
//	Todo device na stage pertence a exatamente um Kind. O Kind governa todas
//	as decisões de containment e colisão.
type Kind int

const (
	// KindSimple — borders 1 and 2; cannot contain, cannot overlap.
	KindSimple Kind = iota

	// KindFitting — borders 1 and 2; snappable (future). Geometrically
	// identical to Simple today.
	KindFitting

	// KindComplex — borders 1, 2 and 3; can contain other devices.
	KindComplex
)

// CanContain reports whether a device of this kind can hold children.
//
// Português: Informa se um device deste Kind pode conter filhos.
func (k Kind) CanContain() bool {
	return k == KindComplex
}

// CanBeChild reports whether a device of this kind may sit inside a
// container. Currently always true; kept as a method to leave room for
// future kinds that might be restricted to the root (e.g. a workspace
// device).
//
// Português: Informa se um device deste Kind pode ser filho de outro.
func (k Kind) CanBeChild() bool {
	return true
}

// String returns the lowercase JSON representation of the Kind.
// Used by the serializer to produce the "kind" field in scene JSON.
//
// Português: Retorna a representação em minúsculas do Kind, usada na
// serialização JSON da cena.
func (k Kind) String() string {
	switch k {
	case KindSimple:
		return "simple"
	case KindFitting:
		return "fitting"
	case KindComplex:
		return "complex"
	default:
		return "unknown"
	}
}
