// devices/hexagonspriteadapter.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package devices

import (
	"strconv"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/devices/block"
	"github.com/helmutkemper/iotmakerio/sprite"
)

// HexagonSpriteAdapter
//
// English:
//
//	Adapts block.ResizeButton to the sprite.ResizeHandleRenderer interface.
//
//	The sprite package calls RenderHandle() once per handle during SetResizeButtons().
//	This adapter clones the template hexagon via GetNew() and returns the detached SVG
//	DOM element with its intrinsic dimensions. The sprite package then serializes it to
//	XML via XMLSerializer, caches it as a raster bitmap, and discards the DOM node.
//
//	Rotation is not applied because regular hexagons have 6-fold rotational symmetry —
//	rotating by any multiple of 60° produces the same visual shape. If a non-symmetric
//	polygon (e.g., triangle) is used, rotation would need to be handled by baking it
//	into the polygon path rather than using SVG transforms.
//
// Português:
//
//	Adapta block.ResizeButton para a interface sprite.ResizeHandleRenderer.
//
//	Rotação não é aplicada porque hexágonos regulares têm simetria rotacional de
//	ordem 6 — rotacionar por qualquer múltiplo de 60° produz a mesma forma visual.
type HexagonSpriteAdapter struct {
	Template block.ResizeButton
}

// RenderHandle
//
// English:
//
//	Creates a detached SVG DOM element for the given resize handle by cloning
//	the template hexagon. Returns the SVG element and its intrinsic dimensions.
//
// Português:
//
//	Cria um elemento SVG DOM desanexado para a alça de resize fornecida clonando
//	o hexágono template. Retorna o elemento SVG e suas dimensões intrínsecas.
func (e *HexagonSpriteAdapter) RenderHandle(handle sprite.ResizeHandle) (svgElement js.Value, width float64, height float64) {

	// Suppress unused variable warning for handle.
	// Hexagons don't need rotation, but the parameter is required by the interface.
	_ = handle

	btn := e.Template.GetNew()
	svgElement = btn.GetSvg().Get()

	// Ensure xmlns is set for valid SVG serialization via Blob URL.
	// Português: Garante xmlns para serialização SVG válida via Blob URL.
	svgElement.Call("setAttribute", "xmlns", "http://www.w3.org/2000/svg")

	// Read the intrinsic dimensions from the SVG element (set by init: 2*size).
	// These MUST match the returned width/height, otherwise the bitmap is rendered
	// at the SVG's viewport size but positioned using the returned dimensions,
	// causing misalignment (buttons too far on top-left, correct on bottom-right).
	//
	// Português: Lê as dimensões intrínsecas do elemento SVG (definidas por init: 2*size).
	// Estas DEVEM corresponder ao width/height retornado, caso contrário o bitmap é
	// renderizado no tamanho do viewport SVG mas posicionado usando as dimensões
	// retornadas, causando desalinhamento.
	width = readSvgAttrFloat(svgElement, "width", 20)
	height = readSvgAttrFloat(svgElement, "height", 20)

	return
}

// GetHandleOffset
//
// English:
//
//	Returns the pixel distance from the element edge to the handle center.
//
// Português:
//
//	Retorna a distância em pixels da borda do elemento ao centro da alça.
func (e *HexagonSpriteAdapter) GetHandleOffset() (offset float64) {
	offset = e.Template.GetSpace().GetFloat()
	return
}

// readSvgAttrFloat reads a numeric attribute from an SVG DOM element.
// Returns the fallback value if the attribute is missing or unparseable.
//
// Português: Lê um atributo numérico de um elemento SVG DOM.
// Retorna o valor fallback se o atributo estiver ausente ou não-parseável.
func readSvgAttrFloat(el js.Value, attr string, fallback float64) float64 {
	val := el.Call("getAttribute", attr)
	if val.IsNull() || val.IsUndefined() {
		return fallback
	}
	f, err := strconv.ParseFloat(val.String(), 64)
	if err != nil || f <= 0 {
		return fallback
	}
	return f
}
