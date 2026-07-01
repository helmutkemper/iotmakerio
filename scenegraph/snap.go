// /ide/scenegraph/snap.go

package scenegraph

// snap.go — Fitting snap behaviour (placeholder).
//
// English:
//
//	Fitting devices carry tabs and sockets drawn on border 2 and snap to
//	compatible neighbours on mouseup, like magnets. The snap operation is
//	a pre-step at the very start of Graph.EndDrag: if a Fitting device is
//	close enough to a compatible neighbour, its position is nudged so the
//	rectangles sit edge-to-edge before conflict detection runs.
//
//	Fitting is not yet shipping. This file exists so that the extension
//	point is visible in the code layout; applyFittingSnap is called
//	unconditionally by EndDrag, but returns (0,0) while the feature is
//	dormant. When Fitting devices are introduced, the implementation lives
//	here and does NOT change any other file in scenegraph.
//
// Português:
//
//	Fittings encaixam como ímãs no mouseup. A operação de snap é uma
//	pré-etapa no início de Graph.EndDrag. Funcionalidade ainda não
//	lançada — este arquivo marca o ponto de extensão.

// applyFittingSnap computes the (dx, dy) offset that should be added to
// the node's position before conflict detection runs, based on nearby
// compatible Fitting neighbours.
//
// Currently returns (0, 0) unconditionally. When Fitting ships, the
// implementation will:
//  1. Walk the candidate list for other Fitting nodes.
//  2. Filter by compatibility rules (to be defined).
//  3. Find the closest neighbour within a snap distance threshold.
//  4. Compute the offset so that node.Outer sits edge-to-edge with
//     neighbour.Outer on the appropriate side.
//
// Python-like pseudocode of the future implementation is kept in a
// comment block in this file for reference when the work resumes.
//
// Português: Retorna (0, 0) enquanto Fitting não foi lançado. Ver
// bloco de comentário abaixo para a implementação futura.
func applyFittingSnap(node *Node, candidates []candidate) (dx, dy float64) {
	_ = node
	_ = candidates
	return 0, 0
}

// Future implementation sketch (kept as a comment so the next developer
// has a starting point):
//
//   const snapDistance = 12.0
//   var best candidate
//   var bestDelta (0, 0)
//   var bestDistance = snapDistance + 1
//
//   for each peer in candidates where peer.Kind == KindFitting:
//       if not compatible(node, peer):  // TBD: connector types, pattern
//           continue
//       delta, distance := computeSnapDelta(node.Outer, peer.Outer)
//       if distance < bestDistance:
//           bestDistance = distance
//           bestDelta = delta
//           best = peer
//
//   return bestDelta
