// /ide/scenegraph/rules.go

package scenegraph

// rules.go — the rule engine.
//
// English:
//
//	This file holds the pure geometric functions that decide containment
//	and conflicts. They do not touch the Graph struct and do not cause
//	side effects; every one of them is deterministic over its inputs and
//	testable in isolation. rules_test.go exercises them against the
//	configurations listed in ARCHITECTURE_SCENEGRAPH.md section 4.
//
//	The single spatial rule — from SCENEGRAPH_SPEC.md section 3 — is:
//
//	  Border 1 of A must be entirely outside border 1 of B, or — if B is
//	  Complex — border 1 of A must be entirely inside border 3 of B. Any
//	  other configuration is an error.
//
//	classifyPair implements exactly this rule, and every other function
//	in this file composes classifyPair for different callers.
//
// Português:
//
//	Contém as funções geométricas puras que decidem containment e
//	conflitos. Sem efeitos colaterais; todas determinísticas e testáveis.

// candidate is the minimal view rules need of a node. Avoids passing
// Node pointers around, which would couple rules.go to the Graph's
// internal state. All four rule functions accept slices of candidate,
// so tests can feed them synthetic inputs without building a Graph.
//
// Português: Visão mínima que as regras precisam de um nó. Evita acoplar
// rules.go ao estado interno do Graph.
type candidate struct {
	ID    string
	Kind  Kind
	Outer Rect
	Inner *Rect // nil unless Kind == KindComplex
}

// classifyPair reports the spatial relationship between A and B.
//
// The function is semantically symmetric: classifyPair(A, B) and
// classifyPair(B, A) return the same verdict (legal or conflict of the
// same kind) for the same pair of rectangles. It returns (_, false)
// when the pair is in a legal configuration:
//
//   - A and B are fully separate (border 1s do not touch), or
//   - B is Complex and A is fully inside B's border 3 (A is a legal
//     child of B), or
//   - A is Complex and B is fully inside A's border 3 (B is a legal
//     child of A).
//
// Otherwise it returns (kind, true) with the most specific
// ConflictKind that applies, in priority order:
//
//	ConflictStraddle > ConflictPiercedOuter > ConflictOverlap.
//
// Português: Classifica a relação espacial entre A e B de forma
// simétrica. Retorna (_, false) para configurações legais; (kind, true)
// para conflitos, com a classificação mais específica que se aplica.
func classifyPair(a, b candidate) (ConflictKind, bool) {
	// Case 1: fully separate — legal.
	if !a.Outer.IntersectsRect(b.Outer) {
		return 0, false
	}

	// Case 2: A is a legal child of B.
	if b.Kind == KindComplex && b.Inner != nil && b.Inner.ContainsRect(a.Outer) {
		return 0, false
	}

	// Case 3: B is a legal child of A.
	if a.Kind == KindComplex && a.Inner != nil && a.Inner.ContainsRect(b.Outer) {
		return 0, false
	}

	// The pair is in conflict. Classify it, most-specific first.

	// Straddle: one's outer pierces the other's inner without being
	// fully contained by it. Checked in both directions because
	// conceptually either device can be "the one straddling".
	if b.Kind == KindComplex && b.Inner != nil && b.Inner.IntersectsRect(a.Outer) {
		return ConflictStraddle, true
	}
	if a.Kind == KindComplex && a.Inner != nil && a.Inner.IntersectsRect(b.Outer) {
		return ConflictStraddle, true
	}

	// PiercedOuter: at least one party is a Complex whose outer is
	// breached without the inner being touched. Since Straddle is
	// already ruled out above, if any Complex is present, its frame is
	// what's being pierced.
	if b.Kind == KindComplex {
		return ConflictPiercedOuter, true
	}
	if a.Kind == KindComplex {
		return ConflictPiercedOuter, true
	}

	// Case 4: neither is Complex — plain lateral overlap.
	return ConflictOverlap, true
}

// findParent returns the ID of the smallest Complex whose border 3
// fully contains outer, ignoring any candidate with ID == excludeID.
// Returns "" if no container qualifies.
//
// "Smallest" is measured by the area of the inner rectangle; this
// guarantees that when containers are nested, the innermost one wins.
//
// Português: Retorna o ID do menor Complex cuja border 3 contém outer
// totalmente. "Menor" é medido pela área da inner.
func findParent(outer Rect, excludeID string, candidates []candidate) string {
	bestID := ""
	bestArea := -1.0

	for i := range candidates {
		c := candidates[i]
		if c.ID == excludeID {
			continue
		}
		if c.Kind != KindComplex || c.Inner == nil {
			continue
		}
		if !c.Inner.ContainsRect(outer) {
			continue
		}
		area := c.Inner.Area()
		if bestArea < 0 || area < bestArea {
			bestArea = area
			bestID = c.ID
		}
	}

	return bestID
}

// findConflicts returns every current spatial conflict involving the
// subject. The list is computed freshly from geometry; nothing is
// cached.
//
// classifyPair is symmetric, so a single pass over the peers suffices:
// classifyPair(subject, peer) yields the same verdict as
// classifyPair(peer, subject).
//
// Português: Retorna todos os conflitos espaciais envolvendo o subject.
// Como classifyPair é simétrico, basta uma varredura.
func findConflicts(subjectID string, candidates []candidate) []Conflict {
	var subject candidate
	found := false
	for i := range candidates {
		if candidates[i].ID == subjectID {
			subject = candidates[i]
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	result := make([]Conflict, 0)
	for i := range candidates {
		peer := candidates[i]
		if peer.ID == subjectID {
			continue
		}
		if k, bad := classifyPair(subject, peer); bad {
			result = append(result, Conflict{With: peer.ID, Kind: k})
		}
	}
	return result
}

// conflictsEqual reports whether two conflict lists are element-wise
// equal, order-independent. Used by the graph to decide whether to fire
// OnConflictsChanged.
//
// Complexity is O(n*m), acceptable because a device rarely has more
// than a handful of conflicts simultaneously.
//
// Português: Informa se duas listas de conflitos são equivalentes
// (independente da ordem). Usado para deduplicar notificações.
func conflictsEqual(a, b []Conflict) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		match := false
		for j := range b {
			if a[i].With == b[j].With && a[i].Kind == b[j].Kind {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return true
}
