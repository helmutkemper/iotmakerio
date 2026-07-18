// wire/renderer.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package wire

import (
	"math"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesConnection"
)

// =====================================================================
//  Wire Rendering | Renderização de Fios
// =====================================================================

// drawWire renders a single wire onto the canvas 2D context.
// It supports solid and dashed lines, rounded corners at bends, and a
// highlight effect when the wire is selected.
//
// [DENSITY-FIX] The density parameter scales all visual properties:
// strokeWidth, cornerRadius, dashPattern, selectedWidth.
// WireStyle values are density-independent base values.
//
// Português:
//
//	drawWire renderiza um único fio no contexto 2D do canvas.
//
//	[DENSITY-FIX] O parâmetro density escala todas as propriedades visuais:
//	strokeWidth, cornerRadius, dashPattern, selectedWidth.
func drawWire(ctx js.Value, w *Wire, density float64) {
	drawWirePolyline(ctx, w.Waypoints, w.Style, w.Selected, density)
}

// drawWirePolyline strokes an arbitrary point polyline using a wire's style.
// drawWire renders a wire's full path with it; the tunnel renderer reuses it to
// draw only the feed segment (source→tunnel) when the consumer's case is hidden
// — so the const→tunnel connection stays visible across case switches while the
// tunnel→device tap follows the active case.
//
// Português: Desenha uma polilinha arbitrária com o estilo de um fio. O drawWire
// usa para o caminho completo; o renderer do túnel reusa para desenhar só o
// trecho do feed (fonte→túnel) quando a condição do consumidor está oculta — o
// const→túnel fica visível ao trocar de condição, e o tap túnel→device segue a
// condição ativa.
func drawWirePolyline(ctx js.Value, pts []Point, style WireStyle, selected bool, density float64) {
	if len(pts) < 2 {
		return
	}

	strokeColor := style.StrokeColor
	strokeWidth := style.StrokeWidth * density

	if selected {
		if style.SelectedColor != "" {
			strokeColor = style.SelectedColor
		}
		if style.SelectedWidth > 0 {
			strokeWidth = style.SelectedWidth * density
		}
	}

	ctx.Call("beginPath")
	ctx.Set("strokeStyle", strokeColor)
	ctx.Set("lineWidth", strokeWidth)
	ctx.Set("lineCap", "round")
	ctx.Set("lineJoin", "round")

	// Set dash pattern (density-scaled).
	// Português: Define padrão de traço (escalado por densidade).
	emptyArray := js.Global().Get("Array").New()
	if len(style.DashPattern) > 0 {
		dashArray := js.Global().Get("Array").New()
		for _, d := range style.DashPattern {
			dashArray.Call("push", d*density)
		}
		ctx.Call("setLineDash", dashArray)
	} else {
		ctx.Call("setLineDash", emptyArray)
	}

	radius := style.CornerRadius * density

	if radius <= 0 || len(pts) <= 2 {
		// No rounded corners: simple polyline.
		// Português: Sem cantos arredondados: polilinha simples.
		ctx.Call("moveTo", pts[0].X, pts[0].Y)
		for i := 1; i < len(pts); i++ {
			ctx.Call("lineTo", pts[i].X, pts[i].Y)
		}
	} else {
		// Rounded corners using arcTo at each bend point.
		// Português: Cantos arredondados usando arcTo em cada ponto de dobra.
		ctx.Call("moveTo", pts[0].X, pts[0].Y)

		for i := 1; i < len(pts)-1; i++ {
			// Clamp radius to half the length of the shorter adjacent segment
			// to prevent the arc from overshooting.
			//
			// Português: Limita o raio à metade do segmento adjacente mais curto.
			segBefore := segmentLength(pts[i-1], pts[i])
			segAfter := segmentLength(pts[i], pts[i+1])
			maxR := math.Min(segBefore, segAfter) / 2.0
			r := math.Min(radius, maxR)

			ctx.Call("arcTo", pts[i].X, pts[i].Y, pts[i+1].X, pts[i+1].Y, r)
		}

		// Final segment to the last point.
		// Português: Segmento final até o último ponto.
		last := pts[len(pts)-1]
		ctx.Call("lineTo", last.X, last.Y)
	}

	ctx.Call("stroke")

	// Reset dash pattern to solid.
	// Português: Reseta padrão de traço para sólido.
	ctx.Call("setLineDash", emptyArray)
}

// drawConnectorDot draws a small filled circle at a connector position to indicate
// that it is connected. The color matches the wire's data type.
//
// [DENSITY-FIX] The radius parameter should already be density-scaled by the caller.
//
// Português:
//
//	drawConnectorDot desenha um pequeno círculo preenchido na posição do conector.
//	O parâmetro radius já deve estar escalado por densidade pelo chamador.
func drawConnectorDot(ctx js.Value, x float64, y float64, color string, radius float64) {
	ctx.Call("beginPath")
	ctx.Call("arc", x, y, radius, 0, 2*math.Pi)
	ctx.Set("fillStyle", color)
	ctx.Call("fill")
}

// =====================================================================
//  Tunnel markers (LabVIEW-style) | Marcadores de túnel
// =====================================================================

// drawTunnelMarker draws a LabVIEW-style tunnel: a small filled square in the
// wire's data-type colour, sitting on a container's border where a wire crosses
// it. A thin dark outline keeps it legible against both the container fill and
// the wire. Coordinates are world-space (Draw applies the camera transform).
//
// Português: Desenha o túnel estilo LabVIEW — um quadradinho preenchido na cor
// do tipo do fio, na borda do container onde o fio a cruza. Contorno escuro fino
// para legibilidade. Coordenadas em mundo (a câmera é aplicada no Draw).
func drawTunnelMarker(ctx js.Value, x float64, y float64, color string, d float64) {
	side := 9.0 * d
	half := side / 2
	ctx.Set("fillStyle", color)
	ctx.Call("fillRect", x-half, y-half, side, side)
	ctx.Set("strokeStyle", "#1e1e1e")
	ctx.Set("lineWidth", 1.0*d)
	ctx.Call("strokeRect", x-half, y-half, side, side)
}

// Manual phase-tunnel palette — the INTERNAL family (violet), red while
// fresh (spec #4, 2026-07-17). Português: Paleta do túnel manual —
// família interna (violeta), vermelho enquanto fresco.
const (
	manualTunnelViolet = "#8b5cf6"
	manualTunnelRed    = "#ef4444"
)

// drawManualTunnelMarker — same square grammar as drawTunnelMarker, with
// an outline (unwired) vs filled (wired) state and the internal palette.
// The phase role (Kemper spec 2026-07-18) adds the STANDARD PIN — the
// MathAdd reference: PinLength×PinThickness, no stroke — always
// pointing INTO the sequence: role "in" = square on the right border,
// pin protruding LEFT; role "out" = square on the left border, pin
// protruding RIGHT. pinColor is the tunnel's STAMPED type colour (the
// chameleon v2, all types including []T pointer+len slices): the caller
// derives it from the feed wire so pin and wire read as one continuous
// piece; while unwired (no stamp yet) the pin stays the internal violet
// — "awaiting a type". An empty role draws the bare square.
// Português: Mesma gramática do quadrado; o papel adiciona o PINO
// PADRÃO apontando PARA DENTRO. pinColor é a cor do tipo CARIMBADO
// (camaleão v2, todos os tipos, incluindo fatias []T ponteiro+len) —
// derivada do fio de alimentação para pino e fio lerem como peça
// contínua; sem fio, violeta interno — "aguardando tipo".
func drawManualTunnelMarker(ctx js.Value, x, y float64, filled, fresh bool, role, pinColor string, d float64) {
	side := 9.0 * d
	half := side / 2
	color := manualTunnelViolet
	if fresh {
		color = manualTunnelRed
	}

	// The pin goes UNDER the square (drawn first) so the junction face
	// stays crisp. Português: Pino por baixo do quadrado.
	if role == "in" || role == "out" {
		pinLen := rulesConnection.PinLength()
		pinThick := rulesConnection.PinThickness()
		pinX := x + half // "out": protrudes right of the square
		if role == "in" {
			pinX = x - half - pinLen // "in": protrudes left of the square
		}
		ctx.Set("fillStyle", pinColor)
		ctx.Call("fillRect", pinX, y-pinThick/2, pinLen, pinThick)
	}

	if filled {
		ctx.Set("fillStyle", color)
		ctx.Call("fillRect", x-half, y-half, side, side)
	} else {
		ctx.Set("fillStyle", "#ffffff")
		ctx.Call("fillRect", x-half, y-half, side, side)
	}
	ctx.Set("strokeStyle", color)
	ctx.Set("lineWidth", 2.0*d)
	ctx.Call("strokeRect", x-half, y-half, side, side)
}

// pointInRect reports whether (x,y) lies inside (or on) the rectangle.
//
// Português: Diz se (x,y) está dentro (ou na borda) do retângulo.
func pointInRect(rx, ry, rw, rh, x, y float64) bool {
	return x >= rx && x <= rx+rw && y >= ry && y <= ry+rh
}

// segIntersect returns the intersection point of the two segments p1→p2 and
// p3→p4 when they cross within both segments. ok is false for parallel segments
// or an intersection outside either segment.
//
// Português: Ponto de interseção dos segmentos p1→p2 e p3→p4, quando se cruzam
// dentro de ambos. ok=false para paralelos ou interseção fora de algum segmento.
func segIntersect(p1x, p1y, p2x, p2y, p3x, p3y, p4x, p4y float64) (x, y float64, ok bool) {
	den := (p2x-p1x)*(p4y-p3y) - (p2y-p1y)*(p4x-p3x)
	if den == 0 {
		return 0, 0, false
	}
	t := ((p3x-p1x)*(p4y-p3y) - (p3y-p1y)*(p4x-p3x)) / den
	u := ((p3x-p1x)*(p2y-p1y) - (p3y-p1y)*(p2x-p1x)) / den
	if t < 0 || t > 1 || u < 0 || u > 1 {
		return 0, 0, false
	}
	return p1x + t*(p2x-p1x), p1y + t*(p2y-p1y), true
}

// borderCrossing returns the point where the segment (x1,y1)→(x2,y2) crosses the
// rectangle's border, when exactly one endpoint is inside the rectangle. ok is
// false when both endpoints are inside or both outside (no single crossing). The
// rectangle is convex, so an in→out segment crosses exactly one edge.
//
// Português: Ponto onde o segmento cruza a borda do retângulo, quando exatamente
// uma ponta está dentro. ok=false se ambas dentro ou ambas fora. O retângulo é
// convexo, então um segmento dentro→fora cruza exatamente uma aresta.
func borderCrossing(rx, ry, rw, rh, x1, y1, x2, y2 float64) (cx, cy float64, ok bool) {
	if pointInRect(rx, ry, rw, rh, x1, y1) == pointInRect(rx, ry, rw, rh, x2, y2) {
		return 0, 0, false
	}
	edges := [4][4]float64{
		{rx, ry, rx + rw, ry},           // top
		{rx + rw, ry, rx + rw, ry + rh}, // right
		{rx + rw, ry + rh, rx, ry + rh}, // bottom
		{rx, ry + rh, rx, ry},           // left
	}
	for _, e := range edges {
		if ix, iy, hit := segIntersect(x1, y1, x2, y2, e[0], e[1], e[2], e[3]); hit {
			return ix, iy, true
		}
	}
	return 0, 0, false
}

// drawDraftWire renders a wire being created interactively.
// Shows a semi-transparent preview from source connector to current pointer.
//
// [DENSITY-FIX] density parameter scales all visual properties.
//
// Português:
//
//	drawDraftWire renderiza um fio sendo criado interativamente.
//	[DENSITY-FIX] density escala todas as propriedades visuais.
func drawDraftWire(ctx js.Value, from Point, to Point, style WireStyle, density float64) {
	waypoints := ComputeManhattanRoute(from, to)
	if len(waypoints) < 2 {
		return
	}

	ctx.Call("save")
	ctx.Set("globalAlpha", 0.5)

	tempWire := &Wire{
		Style:     style,
		Waypoints: waypoints,
	}
	drawWire(ctx, tempWire, density)

	ctx.Call("restore")
}

// =====================================================================
//  Hit Testing | Teste de Colisão
// =====================================================================

// hitTestWire checks if a point (worldX, worldY) is close enough to any segment
// of the wire to be considered a "hit". The tolerance is the maximum distance in
// pixels from the wire line that counts as a hit.
//
// [CAMERA-FIX] The caller (Manager.HitTest) converts screen→world before calling.
// [DENSITY-FIX] The tolerance should already be density-scaled by the caller.
//
// Português:
//
//	hitTestWire verifica se um ponto (worldX, worldY) está próximo o suficiente
//	de qualquer segmento do fio. O chamador já converteu screen→world e escalou tolerance.
func hitTestWire(w *Wire, worldX float64, worldY float64, tolerance float64) bool {
	if len(w.Waypoints) < 2 {
		return false
	}

	for i := 0; i < len(w.Waypoints)-1; i++ {
		a := w.Waypoints[i]
		b := w.Waypoints[i+1]

		dist := distanceToSegment(worldX, worldY, a.X, a.Y, b.X, b.Y)
		if dist <= tolerance {
			return true
		}
	}
	return false
}

// distanceToSegment returns the shortest distance from point (px, py) to the
// line segment from (ax, ay) to (bx, by).
//
// Português:
//
//	distanceToSegment retorna a menor distância do ponto (px, py) ao segmento
//	de linha de (ax, ay) a (bx, by).
func distanceToSegment(px, py, ax, ay, bx, by float64) float64 {
	dx := bx - ax
	dy := by - ay
	lengthSq := dx*dx + dy*dy

	if lengthSq == 0 {
		// Degenerate segment (a == b): distance to point.
		return math.Sqrt((px-ax)*(px-ax) + (py-ay)*(py-ay))
	}

	// Project point onto the line, clamped to [0,1].
	t := ((px-ax)*dx + (py-ay)*dy) / lengthSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}

	closestX := ax + t*dx
	closestY := ay + t*dy

	return math.Sqrt((px-closestX)*(px-closestX) + (py-closestY)*(py-closestY))
}

// segmentLength returns the Euclidean distance between two points.
//
// Português: Retorna a distância euclidiana entre dois pontos.
func segmentLength(a, b Point) float64 {
	dx := b.X - a.X
	dy := b.Y - a.Y
	return math.Sqrt(dx*dx + dy*dy)
}
