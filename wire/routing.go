// wire/routing.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

// =====================================================================
//  Manhattan Routing | Roteamento Manhattan
// =====================================================================

// defaultStubLength is the minimum horizontal distance a wire extends from a connector
// before making its first turn. This ensures wires visually "leave" the connector
// cleanly rather than turning immediately at the connector edge.
//
// Português:
//
//	defaultStubLength é a distância horizontal mínima que um fio se estende de um conector
//	antes de fazer sua primeira curva. Isso garante que os fios "saiam" do conector de forma
//	limpa visualmente, em vez de virar imediatamente na borda do conector.
const defaultStubLength = 20.0

// ComputeManhattanRoute calculates the waypoints for a Manhattan-style wire route
// between two points. The route consists only of horizontal and vertical segments.
//
// The algorithm assumes:
//   - The source (from) exits horizontally to the RIGHT.
//   - The target (to) enters horizontally from the LEFT.
//
// This produces clean connections typical of circuit/node editors where outputs
// are on the right side and inputs on the left side of components.
//
// Three cases are handled:
//
//  1. Simple case: source is to the left of target with enough room.
//     Route: from → horizontal → vertical → horizontal → to
//
//  2. Close case: source and target are horizontally close but not reversed.
//     Route: from → stub → vertical → horizontal → stub → to
//
//  3. Reverse case: target is behind the source (to the left).
//     Route: from → stub → vertical(bypass) → horizontal → vertical → stub → to
//
// Português:
//
//	ComputeManhattanRoute calcula os waypoints para uma rota estilo Manhattan entre dois
//	pontos. A rota consiste apenas em segmentos horizontais e verticais.
//
//	O algoritmo assume:
//	  - A origem (from) sai horizontalmente para a DIREITA.
//	  - O destino (to) entra horizontalmente pela ESQUERDA.
func ComputeManhattanRoute(from Point, to Point) (waypoints []Point) {
	return ComputeManhattanRouteWithStub(from, to, defaultStubLength)
}

// ComputeManhattanRouteWithStub is the same as ComputeManhattanRoute but allows
// specifying a custom stub length.
//
// Português:
//
//	ComputeManhattanRouteWithStub é igual a ComputeManhattanRoute mas permite
//	especificar um comprimento de stub customizado.
func ComputeManhattanRouteWithStub(from Point, to Point, stubLength float64) (waypoints []Point) {
	waypoints = make([]Point, 0, 8)

	// Starting point: the output connector.
	// Português: Ponto inicial: o conector de saída.
	waypoints = append(waypoints, from)

	stubFromX := from.X + stubLength
	stubToX := to.X - stubLength

	if stubFromX+stubLength < stubToX {
		// Case 1: Simple — enough horizontal space between connectors.
		// Route goes: right → down/up → right.
		//
		//   from ●────┐
		//              │
		//              └────● to
		//
		// Português: Caso 1: Simples — espaço horizontal suficiente entre conectores.
		midX := (stubFromX + stubToX) / 2.0
		waypoints = append(waypoints,
			Point{midX, from.Y}, // horizontal from source
			Point{midX, to.Y},   // vertical to target height
		)
	} else if from.X < to.X {
		// Case 2: Close — source is left of target but not enough room for clean mid-route.
		// Route goes: stub right → down/up → horizontal → stub left.
		//
		//   from ●──┐
		//            │
		//            └──● to
		//
		// Português: Caso 2: Próximo — origem à esquerda do destino mas sem espaço suficiente.
		midX := (from.X + to.X) / 2.0
		waypoints = append(waypoints,
			Point{midX, from.Y},
			Point{midX, to.Y},
		)
	} else {
		// Case 3: Reverse — target is to the left of (or directly above/below) the source.
		// The wire needs to make a "U-turn" to avoid crossing through components.
		//
		//         ┌────────────────┐
		//         │                │
		//   to ●──┘    from ●─────┘
		//
		// When source and target are at the same Y, we offset the bypass vertically
		// to create a visible route instead of overlapping lines.
		//
		// Português: Caso 3: Reverso — destino está à esquerda (ou diretamente acima/abaixo)
		// da origem. O fio precisa fazer uma "volta em U".
		midY := (from.Y + to.Y) / 2.0

		// If they're on the same horizontal line, offset to avoid overlapping.
		// Português: Se estão na mesma linha horizontal, desloca para evitar sobreposição.
		if abs(from.Y-to.Y) < 1.0 {
			midY = from.Y - 40.0 // offset above | desloca para cima
		}

		waypoints = append(waypoints,
			Point{stubFromX, from.Y}, // stub from source
			Point{stubFromX, midY},   // vertical to bypass height
			Point{stubToX, midY},     // horizontal across
			Point{stubToX, to.Y},     // vertical down to target
		)
	}

	// Ending point: the input connector.
	// Português: Ponto final: o conector de entrada.
	waypoints = append(waypoints, to)

	return
}

// RecalculateWireRoute updates the waypoints of a wire based on the current positions
// of its source and target connectors.
//
// Português:
//
//	RecalculateWireRoute atualiza os waypoints de um fio baseado nas posições atuais
//	dos conectores de origem e destino.
func RecalculateWireRoute(w *Wire, fromPos Point, toPos Point) {
	w.Waypoints = ComputeManhattanRoute(fromPos, toPos)
}

// abs returns the absolute value of a float64.
//
// Português: Retorna o valor absoluto de um float64.
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
