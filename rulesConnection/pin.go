// rulesConnection/pin.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesConnection

// pin.go — The standard connector pin.
//
// English:
//
//	User testing showed two connector aesthetics living side by side: the
//	"math add" pin (a small rectangle protruding from the device body, wire
//	attached at the OUTER TIP) and a colored dot drawn INSIDE the body of the
//	rectangular devices. The pin is the product standard; this file makes it
//	the ONLY way to draw, hit-test and anchor a connector, so a device can
//	never drift from the standard again.
//
//	Vocabulary used by every function here:
//
//	  EDGE POINT (edgeX, edgeY)
//	      The point where the pin meets the device body border, vertically
//	      centered on the pin. Each device defines its edge points in exactly
//	      ONE method (see StatementIndexInt.connectorLayout for the pattern)
//	      and feeds them to all four consumers: SVG rendering, click
//	      hit-test, cursor hit-test and wire.ConnectorInfo.PositionFunc.
//
//	  SIDE
//	      Which way the pin protrudes from the edge point. Inputs live on the
//	      left border and protrude LEFT; outputs live on the right border and
//	      protrude RIGHT.
//
//	  ANCHOR
//	      The point where the wire attaches: the CENTER OF THE OUTER TIP of
//	      the pin. This is what PositionFunc must return.
//
//	Geometry (logical units, scaled by rulesDensity on output):
//
//	           body border
//	               │
//	    ┌──────────┤
//	    │   body   ├────■ ← anchor = edge + KWidth on the pin axis
//	    │          ├────┘
//	    └──────────┤  ↑ pin: KWidth long × KHeight thick
//	               │
//	          edge point (edgeX, edgeY)
//
//	Devices with pins must inset their body rectangle by PinBodyInset() on
//	every side that carries pins, so the pin protrudes WITHIN the sprite
//	element bounds (the element size itself does not change — grid layout
//	and existing scenes are unaffected).
//
//	Density: the float helpers return SCALED canvas pixels (they multiply by
//	the global density internally), matching the coordinate space of
//	sprite.Element sizes, positions and PointerEvent.Local*. The Density
//	path helpers return logical values that scale when printed — the space
//	the SVG-DOM ornaments work in.
//
// Português:
//
//	Os testes com usuários mostraram duas estéticas de conector convivendo: o
//	pino do "math add" (retângulo pequeno saindo do corpo, fio preso na PONTA
//	EXTERNA) e uma bolinha desenhada DENTRO do corpo dos devices retangulares.
//	O pino é o padrão do produto; este arquivo o torna a ÚNICA forma de
//	desenhar, testar clique e ancorar um conector.
//
//	Vocabulário: EDGE POINT é onde o pino encosta na borda do corpo,
//	centrado verticalmente no pino — cada device define seus edge points em
//	exatamente UM método e alimenta os quatro consumidores (SVG, clique,
//	cursor, PositionFunc). SIDE é para onde o pino sai (entrada = esquerda,
//	saída = direita). ANCHOR é onde o fio prende: o CENTRO DA PONTA EXTERNA.
//
//	Devices com pinos recuam o retângulo do corpo em PinBodyInset() nos lados
//	com pinos, para o pino caber dentro dos limites do sprite element (o
//	tamanho do element não muda — grid e cenas existentes não são afetados).

import (
	"fmt"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// PinSide selects the direction a pin protrudes from its edge point.
//
// Português: Seleciona a direção em que o pino sai do seu edge point.
type PinSide int

const (
	// PinSideLeft — the pin protrudes to the LEFT of the edge point.
	// Used by INPUT connectors (they live on the left border of a device).
	//
	// Português: O pino sai para a ESQUERDA do edge point. Usado por
	// conectores de ENTRADA (vivem na borda esquerda do device).
	PinSideLeft PinSide = iota

	// PinSideRight — the pin protrudes to the RIGHT of the edge point.
	// Used by OUTPUT connectors (they live on the right border of a device).
	//
	// Português: O pino sai para a DIREITA do edge point. Usado por
	// conectores de SAÍDA (vivem na borda direita do device).
	PinSideRight
)

// Hit-area padding around the pin body. The pin itself is small (KWidth ×
// KHeight = 6×4 logical px); a click target that small would be hostile,
// especially on touch. The padded hit box is centered on the pin body and
// ends up 18×20 logical px — comparable to the 20px-diameter circular hit
// areas the dot-style devices used before the standardization, so the click
// comfort users already learned is preserved.
//
// Português: Preenchimento da área de clique ao redor do corpo do pino. O
// pino em si é pequeno (6×4 px lógicos); um alvo desse tamanho seria hostil,
// principalmente no touch. A caixa de clique fica 18×20 px lógicos —
// comparável aos círculos de 20px de diâmetro que os devices de bolinha
// usavam antes, preservando o conforto de clique já aprendido.
const (
	// KPinHitWidth is the total width of the pin click/hover box.
	KPinHitWidth = KWidth + 12

	// KPinHitHeight is the total height of the pin click/hover box.
	KPinHitHeight = KHeight + 16
)

// PinLength returns the pin length in SCALED canvas pixels — how far the pin
// protrudes from the device body border.
//
// Português: Retorna o comprimento do pino em pixels ESCALADOS do canvas —
// quanto o pino sai da borda do corpo do device.
func PinLength() float64 {
	return rulesDensity.Density(KWidth).GetFloat()
}

// PinThickness returns the pin thickness in SCALED canvas pixels.
//
// Português: Retorna a espessura do pino em pixels ESCALADOS do canvas.
func PinThickness() float64 {
	return rulesDensity.Density(KHeight).GetFloat()
}

// PinBodyInset returns how much a device must inset its body rectangle on
// every side that carries pins, in SCALED canvas pixels. It equals the pin
// length: the freed margin is exactly the space the pins occupy.
//
// Português: Retorna quanto o device deve recuar o retângulo do corpo em
// cada lado que carrega pinos, em pixels ESCALADOS. É igual ao comprimento
// do pino: a margem liberada é exatamente o espaço que os pinos ocupam.
func PinBodyInset() float64 {
	return PinLength()
}

// PinAnchor returns the wire attachment point for a pin: the CENTER OF THE
// OUTER TIP. This is the value a device's wire.ConnectorInfo.PositionFunc
// must return (in world coordinates: element position + local anchor).
//
// Coordinates are in the same space as the inputs — pass scaled locals, get
// scaled locals; pass world, get world.
//
// Português: Retorna o ponto de fixação do fio: o CENTRO DA PONTA EXTERNA do
// pino. É o valor que o PositionFunc do device deve retornar (em coordenadas
// de mundo: posição do element + anchor local). As coordenadas ficam no
// mesmo espaço da entrada.
func PinAnchor(side PinSide, edgeX, edgeY float64) (x, y float64) {
	if side == PinSideLeft {
		return edgeX - PinLength(), edgeY
	}
	return edgeX + PinLength(), edgeY
}

// PinAnchorD is the logical-Density twin of PinAnchor, for SVG-DOM ornaments
// that keep their geometry in Density space.
//
// Português: Gêmeo em Density lógico do PinAnchor, para ornaments SVG-DOM
// que mantêm a geometria em Density.
func PinAnchorD(side PinSide, edgeX, edgeY rulesDensity.Density) (x, y rulesDensity.Density) {
	if side == PinSideLeft {
		return edgeX - rulesDensity.Density(KWidth), edgeY
	}
	return edgeX + rulesDensity.Density(KWidth), edgeY
}

// PinSVGFragment returns the SVG fragment for one pin, for devices that
// build their ornament as an SVG string (renderSVG pattern). The fill is the
// pin's DATA TYPE color — use rulesDevice.TypeStyleFor(goType).Color — so
// the pin and the wire it meets read as one continuous piece.
//
// No stroke, matching the math add reference pin.
//
// Português: Retorna o fragmento SVG de um pino, para devices que montam o
// ornamento como string SVG (padrão renderSVG). O fill é a cor do TIPO DE
// DADO do pino — use rulesDevice.TypeStyleFor(goType).Color — para pino e
// fio lerem como uma peça contínua. Sem stroke, igual ao pino de referência
// do math add.
func PinSVGFragment(side PinSide, edgeX, edgeY float64, fillHex string) string {
	length := PinLength()
	thick := PinThickness()

	x := edgeX
	if side == PinSideLeft {
		x = edgeX - length
	}
	return fmt.Sprintf(
		`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
		x, edgeY-thick/2, length, thick, fillHex,
	)
}

// PinHit reports whether the local point (px, py) is inside the pin's padded
// hit box. Devices use it for BOTH the click hit-test and the cursor
// hit-test, guaranteeing the two can never disagree.
//
// The box is a rectangle (not a circle): it matches the pin's shape, covers
// the tip generously so users can click "where the wire attaches", and is
// cheaper than a distance check.
//
// Português: Informa se o ponto local (px, py) está dentro da caixa de
// clique do pino. Devices usam para o hit-test de clique E o de cursor,
// garantindo que os dois nunca divirjam. A caixa é retangular: casa com a
// forma do pino, cobre a ponta com folga e é mais barata que distância.
func PinHit(side PinSide, edgeX, edgeY, px, py float64) bool {
	// Center of the pin body along its axis.
	// Português: Centro do corpo do pino ao longo do seu eixo.
	cx := edgeX + PinLength()/2
	if side == PinSideLeft {
		cx = edgeX - PinLength()/2
	}

	halfW := rulesDensity.Density(KPinHitWidth).GetFloat() / 2
	halfH := rulesDensity.Density(KPinHitHeight).GetFloat() / 2

	dx := px - cx
	dy := py - edgeY
	return dx >= -halfW && dx <= halfW && dy >= -halfH && dy <= halfH
}

// PinPathDraw returns the SVG path commands for one pin, for ornaments that
// draw through the SVG DOM (html.TagSvgPath.D). Values are logical Density —
// they scale when the path string is built.
//
// This replaces the removed top-left GetPathDraw: same 6×4 pin, but the
// caller passes the EDGE POINT instead of the rectangle's top-left corner,
// so input and output pins share one convention and the wire anchor
// (PinAnchor) is derivable from the same point.
//
// Português: Retorna os comandos de path SVG de um pino, para ornaments que
// desenham pelo DOM SVG. Valores em Density lógico — escalam ao montar a
// string. Substitui o GetPathDraw removido: mesmo pino 6×4, mas o
// chamador passa o EDGE POINT em vez do canto superior esquerdo, unificando
// a convenção de entradas e saídas.
func PinPathDraw(side PinSide, edgeX, edgeY rulesDensity.Density) (path []string) {
	x := edgeX
	if side == PinSideLeft {
		x = edgeX - rulesDensity.Density(KWidth)
	}
	y := edgeY - rulesDensity.Density(KHeight)/2

	return []string{
		fmt.Sprintf("M %v %v", x, y),
		fmt.Sprintf("l %v 0", rulesDensity.Density(KWidth)),
		fmt.Sprintf("l 0 %v", rulesDensity.Density(KHeight)),
		fmt.Sprintf("l -%v 0", rulesDensity.Density(KWidth)),
		fmt.Sprintf("l 0 -%v", rulesDensity.Density(KHeight)),
	}
}

// PinPathAreaDraw returns the SVG path commands for the enlarged (invisible)
// interaction area of one pin, mirroring PinPathDraw's convention. Kept for
// the ornament connection.Connection areas.
//
// Português: Retorna os comandos de path da área de interação ampliada
// (invisível) de um pino, espelhando a convenção do PinPathDraw. Mantido
// para as áreas connection.Connection dos ornaments.
func PinPathAreaDraw(side PinSide, edgeX, edgeY rulesDensity.Density) (path []string) {
	padX := rulesDensity.Density(KWidthArea-KWidth) / 2
	padY := rulesDensity.Density(KHeightArea-KHeight) / 2

	x := edgeX - padX
	if side == PinSideLeft {
		x = edgeX - rulesDensity.Density(KWidth) - padX
	}
	y := edgeY - rulesDensity.Density(KHeight)/2 - padY

	return []string{
		fmt.Sprintf("M %v %v", x, y),
		fmt.Sprintf("l %v 0", rulesDensity.Density(KWidthArea)),
		fmt.Sprintf("l 0 %v", rulesDensity.Density(KHeightArea)),
		fmt.Sprintf("l -%v 0", rulesDensity.Density(KWidthArea)),
		fmt.Sprintf("l 0 -%v", rulesDensity.Density(KHeightArea)),
	}
}
