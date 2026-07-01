// /ide/ui/contextMenu/anchor.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	Placement decisions for the popover. Two concerns only:
//	  1. Convert world coordinates (where a device lives) to screen
//	     coordinates (where the DOM popover must sit).
//	  2. Choose a side of the anchor and clamp the panel to the
//	     viewport so it never opens with a clipped edge.
//
//	The panel's final position is set via inline `left:` and `top:`
//	by the renderer after this file picks the values.
//
//	Design note: the panel's real pixel size depends on which CSS
//	media query matched (coarse vs fine). We expose a single
//	estimatePanelSize() that matches the @media rules in style.go.
//	If you change the widths in style.go, change them here too —
//	they are intentionally duplicated rather than shared via a
//	constant because CSS cannot read Go constants and Go can't
//	query the un-rendered CSSOM reliably.
//
// Português:
//
//	Decisões de posicionamento do popover. Duas responsabilidades:
//	  1. Converter coordenadas de mundo (onde o device está) para
//	     coordenadas de tela (onde o popover DOM precisa ficar).
//	  2. Escolher um lado do ponto de âncora e prender o painel à
//	     viewport para que nunca abra com uma borda cortada.
package contextMenu

import (
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/sprite"
)

// Panel dimensions kept in sync with style.go.
// If you change the @media (pointer: coarse) rule in style.go,
// update these too. Values are inclusive of border.
const (
	panelWidthFine    = 440
	panelHeightFine   = 260 // typical; CSS uses min-height/max-height
	panelWidthCoarse  = 520
	panelHeightCoarse = 320

	// anchorMargin is the gap between the device (or click point)
	// and the panel edge, in pixels. Big enough that a fingertip
	// on the device doesn't also touch the panel.
	anchorMargin = 12

	// viewportMargin is the minimum space kept between the panel
	// and the viewport edge, so the popover never sits flush
	// against the screen boundary.
	viewportMargin = 8
)

// worldToViewport converts a world-space coordinate (as used by
// devices on the sprite stage) to a viewport-space coordinate (as
// used by `position: fixed` elements). The conversion has two steps:
//
//  1. world → canvas-local: delegated to the Camera's own
//     WorldToScreen method. The canonical formula lives on the
//     Camera type (sprite/camera.go) and is:
//
//     screenX = (worldX - OffsetX) * Zoom
//
//     IMPORTANT: never duplicate the formula here. The legacy hex
//     renderer in menuPlacement.go duplicated it incorrectly as
//     `worldX*Zoom + OffsetX` and that bug hid for a long time
//     because OffsetX is 0 whenever the user has not panned. When
//     the camera is panned, the wrong formula drifts proportionally
//     to the offset, which is exactly the symptom reported against
//     Delivery-A (*"a câmera muda o comportamento do bug"*).
//
//  2. canvas-local → viewport-fixed: add the canvas's
//     getBoundingClientRect() top-left. Without this the popover
//     drifts by whatever space sits between the viewport edge and
//     the canvas (sidebar width, header height).
//
// If stage or canvasEl is unusable, the corresponding step falls
// back to identity. This lets unit tests run with a nil stage and
// a null element without crashing.
//
// Português: Converte coordenadas de mundo para coordenadas da
// viewport (usadas por elementos com `position: fixed`). Dois
// passos: primeiro delega ao método canônico cam.WorldToScreen
// (NUNCA duplicar a fórmula — o hex antigo duplicou errado e o
// bug se escondeu porque OffsetX normalmente é zero), depois
// soma o offset do canvas na viewport.
func worldToViewport(stage sprite.Stage, canvasEl js.Value, worldX, worldY float64) (float64, float64) {
	// Step 1 — world → canvas-local, using the Camera's own method.
	canvasX, canvasY := worldX, worldY
	if stage != nil {
		if cam := stage.GetCamera(); cam != nil {
			canvasX, canvasY = cam.WorldToScreen(worldX, worldY)
		}
	}

	// Step 2 — canvas-local → viewport-fixed, using the canvas
	// bounding rect. getBoundingClientRect is cheap (no layout
	// thrashing as long as we don't touch layout properties right
	// after) and gives viewport-relative coordinates directly.
	if canvasEl.Truthy() {
		rect := canvasEl.Call("getBoundingClientRect")
		if rect.Truthy() {
			canvasX += rect.Get("left").Float()
			canvasY += rect.Get("top").Float()
		}
	}

	return canvasX, canvasY
}

// viewportSize returns the inner dimensions of the browser window,
// in CSS pixels. Falls back to a conservative size if the call
// fails (tests, headless contexts).
func viewportSize() (w, h float64) {
	win := js.Global()
	iw := win.Get("innerWidth")
	ih := win.Get("innerHeight")
	if !iw.Truthy() || !ih.Truthy() {
		return 1024, 768
	}
	return iw.Float(), ih.Float()
}

// estimatePanelSize returns the approximate pixel size of the panel
// for the given pointer kind. Used to pre-clamp the anchor before
// the DOM is inserted — picking the real post-layout size would
// force a reflow round-trip.
func estimatePanelSize(pointerCoarse bool) (w, h float64) {
	if pointerCoarse {
		return panelWidthCoarse, panelHeightCoarse
	}
	return panelWidthFine, panelHeightFine
}

// decidePlacement chooses absolute top-left screen coordinates for
// the panel so that:
//   - The panel prefers to sit to the right of the anchor point.
//   - If it would clip off the right edge, it flips to the left.
//   - If it still doesn't fit on either side (tiny viewport), it
//     is centered horizontally and placed below the anchor.
//   - Vertically it aligns its top with the anchor, then clamps to
//     the viewport's vertical bounds.
//
// Returns screen-space left and top for the panel's outer box.
func decidePlacement(anchorScreenX, anchorScreenY float64, pointerCoarse bool) (left, top float64) {
	vw, vh := viewportSize()
	pw, ph := estimatePanelSize(pointerCoarse)

	left = decideHorizontal(anchorScreenX, pw, vw)
	top = decideVertical(anchorScreenY, ph, vh)
	return left, top
}

// decideHorizontal: right-of-anchor preferred, flip left on overflow,
// centre as last resort.
func decideHorizontal(anchorX, panelW, viewportW float64) float64 {
	rightSide := anchorX + anchorMargin
	if rightSide+panelW+viewportMargin <= viewportW {
		return rightSide
	}
	leftSide := anchorX - anchorMargin - panelW
	if leftSide >= viewportMargin {
		return leftSide
	}
	// Last resort: centre the panel horizontally.
	return (viewportW - panelW) / 2
}

// decideVertical: align top with anchor, then clamp both edges to
// the viewport.
func decideVertical(anchorY, panelH, viewportH float64) float64 {
	top := anchorY
	if top+panelH+viewportMargin > viewportH {
		top = viewportH - panelH - viewportMargin
	}
	if top < viewportMargin {
		top = viewportMargin
	}
	return top
}

// decidePlacementForElement picks the anchor point for a DOM element
// (used by OpenAtElement on the frontend side). Picks the element's
// right-middle edge as the anchor so the panel naturally ends up
// floating next to the clicked device.
//
// If the element has zero size or is off-screen, falls back to the
// viewport centre — still a defined behaviour instead of a crash.
func decidePlacementForElement(anchor js.Value, pointerCoarse bool) (left, top float64) {
	if !anchor.Truthy() {
		vw, vh := viewportSize()
		pw, ph := estimatePanelSize(pointerCoarse)
		return (vw - pw) / 2, (vh - ph) / 2
	}
	rect := anchor.Call("getBoundingClientRect")
	ax := rect.Get("right").Float()
	ay := rect.Get("top").Float()
	return decidePlacement(ax, ay, pointerCoarse)
}
