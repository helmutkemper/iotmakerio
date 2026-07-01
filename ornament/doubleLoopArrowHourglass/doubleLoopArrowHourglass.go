// /ide/ornament/doubleLoopArrowHourglass/doubleLoopArrowHourglass.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package doubleLoopArrowHourglass

// doubleLoopArrowHourglass.go — Ornament for StatementLoopDuration.
//
// Draws the same double-loop arrow border as doubleLoopArrow, but replaces the
// red panic-stop circle with a cyan hourglass icon representing a timed sleep
// interval.  The connection area accepts time.Duration instead of bool.
//
// Visual layout (bottom-right corner detail):
//
//	         connection dot
//	               ↓
//	           ─── ┌──────┐
//	               │  ⏳  │  ← hourglass icon centered in frame
//	               └──────┘
//
// The positions are taken directly from doubleLoopArrow.Update():
//   Frame:      (width-50, height-50) → 20×20 density area
//   Icon:       centered at (width-40, height-40) — same as the red circle
//   Connection: at (width-57, height-42) — LEFT of the frame, where the wire attaches
//
// Português:
//
//	Desenha a mesma borda de seta dupla do doubleLoopArrow, mas substitui o
//	círculo vermelho por um ícone de ampulheta ciano. O connection dot fica
//	à ESQUERDA do frame (não em cima do ícone).

import (
	"fmt"
	"image/color"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/connection"
	"github.com/helmutkemper/iotmakerio/connection/factoryConnection"
	"github.com/helmutkemper/iotmakerio/rulesConnection"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// DoubleLoopArrowHourglass draws the ornament used by StatementLoopDuration:
// a rounded box with two looping arrows and an hourglass icon in the
// bottom-right corner with a time.Duration connection point.
type DoubleLoopArrowHourglass struct {
	// ── Normal state colors ─────────────────────────────────────────────
	arrowNormalColor      color.RGBA
	backgroundNormalColor color.RGBA
	hourglassNormalColor  color.RGBA
	borderNormalColor     color.RGBA

	// ── Selected state colors ───────────────────────────────────────────
	arrowSelectedColor      color.RGBA
	backgroundSelectedColor color.RGBA
	hourglassSelectedColor  color.RGBA
	borderSelectedColor     color.RGBA

	// ── SVG elements ────────────────────────────────────────────────────
	svg               *html.TagSvg
	backgroundContent *html.TagSvgPath
	borderArrow       *html.TagSvgPath
	hourglassIcon     *html.TagSvgPath
	hourglassBorder   *html.TagSvgPath

	// ── Connection ──────────────────────────────────────────────────────
	intervalConnection     *html.TagSvgPath
	intervalConnectionArea connection.Connection
}

// GetConnectionError returns any error accumulated during connection setup.
func (e *DoubleLoopArrowHourglass) GetConnectionError() (err error) {
	return rulesConnection.GetError()
}

// IntervalButtonSetup configures the connection area for the interval port.
// Must be called before Init().
//
// Português: Configura a área de conexão para a porta interval.
func (e *DoubleLoopArrowHourglass) IntervalButtonSetup(setup connection.Setup) {
	e.intervalConnectionArea.Setup(setup)
}

// SetSelected toggles between normal and selected visual states.
func (e *DoubleLoopArrowHourglass) SetSelected(selected bool) {
	if selected {
		e.backgroundContent.Fill(e.backgroundSelectedColor)
		e.borderArrow.Stroke(e.arrowSelectedColor)
		e.hourglassIcon.Fill(e.hourglassSelectedColor)
		e.hourglassBorder.Stroke(e.borderSelectedColor)
		return
	}

	e.backgroundContent.Fill(e.backgroundNormalColor)
	e.borderArrow.Stroke(e.arrowNormalColor)
	e.hourglassIcon.Fill(e.hourglassNormalColor)
	e.hourglassBorder.Stroke(e.borderNormalColor)
}

// GetSvg returns the SVG tag with the element design.
func (e *DoubleLoopArrowHourglass) GetSvg() (svg *html.TagSvg) {
	return e.svg
}

// Init initializes the SVG elements with default colors.
// Must be called before Update().
//
// Color scheme:
//   - Arrow border: orange (same as StatementLoop for visual consistency)
//   - Background: very light cyan tint
//   - Hourglass icon: cyan (#00CCCC) matching KColorTypeDuration
//   - Hourglass border: darker cyan
//
// Português: Inicializa os elementos SVG com cores padrão.
func (e *DoubleLoopArrowHourglass) Init() (err error) {

	// ── Selected state ──────────────────────────────────────────────────
	e.arrowSelectedColor = color.RGBA{R: 255, G: 80, B: 0, A: 255}
	e.backgroundSelectedColor = color.RGBA{R: 230, G: 245, B: 245, A: 255}
	e.hourglassSelectedColor = color.RGBA{R: 0, G: 180, B: 180, A: 255}
	e.borderSelectedColor = color.RGBA{R: 0, G: 160, B: 160, A: 255}

	// ── Normal state ────────────────────────────────────────────────────
	e.arrowNormalColor = color.RGBA{R: 255, G: 100, B: 0, A: 255}
	e.backgroundNormalColor = color.RGBA{R: 240, G: 250, B: 250, A: 255}
	e.hourglassNormalColor = color.RGBA{R: 0, G: 204, B: 204, A: 255} // #00CCCC
	e.borderNormalColor = color.RGBA{R: 0, G: 180, B: 180, A: 255}

	e.svg = factoryBrowser.NewTagSvg()

	// Background — rounded rectangle filled with a subtle cyan tint.
	e.backgroundContent = factoryBrowser.NewTagSvgPath().
		Fill(e.backgroundNormalColor).
		Stroke("none").
		MarkerEnd("url(#backgroundContent)")
	e.svg.Append(e.backgroundContent)

	// Border — double-loop arrow path (purely decorative).
	e.borderArrow = factoryBrowser.NewTagSvgPath().
		Fill("none").
		Stroke(e.arrowNormalColor).
		StrokeWidth(rulesDensity.NewInt(5).GetInt()).
		StrokeLineCap(html.KSvgStrokeLinecapRound).
		StrokeLineJoin(html.KSvgStrokeLinejoinRound).
		MarkerEnd("url(#borderArrow)")
	e.svg.Append(e.borderArrow)

	// Hourglass icon — the FA path is positioned by Update() via transform.
	e.hourglassIcon = factoryBrowser.NewTagSvgPath().
		Fill(e.hourglassNormalColor).
		Stroke("none").
		MarkerEnd("url(#hourglassIcon)")
	e.svg.Append(e.hourglassIcon)

	// Hourglass border — rounded square frame around the icon.
	e.hourglassBorder = factoryBrowser.NewTagSvgPath().
		Fill("none").
		Stroke(e.borderNormalColor).
		StrokeWidth(rulesDensity.NewInt(2).GetInt()).
		MarkerEnd("url(#hourglassBorder)")
	e.svg.Append(e.hourglassBorder)

	// Connection dot — colored by type (time.Duration → cyan).
	e.intervalConnection = factoryConnection.NewConnection("time.Duration", "url(#intervalConnection)")
	e.svg.Append(e.intervalConnection)

	// Connection area — invisible click target.
	e.intervalConnectionArea.Init("url(#intervalConnectionArea)")
	e.svg.Append(e.intervalConnectionArea.GetSvgPath())

	return
}

// Update recalculates all SVG paths for the given bounding box.
// Called on init and after every resize.
//
// The bottom-right icon area uses the EXACT same math as doubleLoopArrow.Update()
// for the stop button. This keeps the hit-test, wire positions, and visual layout
// consistent between the two loop types.
//
// Original stop button positions (from doubleLoopArrow.go):
//
//	margin = 10,  cr = 5,  cx = 20,  cy = 20
//	xp = width - margin - 2*cr - 1.5*cx = width - 50   ← frame top-left X
//	yp = height - margin - 2*cr - 1.5*cy = height - 50  ← frame top-left Y
//	L  = 2*cr + 10 = 20                                  ← frame side length
//	circle center = (width-40, height-40)                 ← icon center
//	connection    = (width-57, height-42)                 ← wire attachment (LEFT of frame)
//
// Português: Recalcula todos os caminhos SVG para a caixa delimitadora dada.
func (e *DoubleLoopArrowHourglass) Update(x, y, width, height rulesDensity.Density) (err error) {

	margin := rulesDensity.Density(10)
	r := rulesDensity.Density(20)
	s := rulesDensity.Density(40)

	// ── Double-loop arrow path (identical to doubleLoopArrow) ────────────
	arrow := []string{
		// Top-right arrow
		fmt.Sprintf("M %v %v", margin+s, margin),
		fmt.Sprintf("l %v %v", rulesDensity.Density(15), rulesDensity.Density(7)),
		fmt.Sprintf("M %v %v", margin+s, margin),
		fmt.Sprintf("l %v %v", rulesDensity.Density(15), rulesDensity.Density(-7)),
		fmt.Sprintf("M %v %v", margin+s, margin),
		fmt.Sprintf("H %v", width-margin-r),
		fmt.Sprintf("Q %v %v, %v %v", width-margin, margin, width-margin, margin+r),
		fmt.Sprintf("V %v", height-margin-s),
		// Bottom-left arrow
		fmt.Sprintf("M %v %v", width-margin-s, height-margin),
		fmt.Sprintf("l %v %v", rulesDensity.Density(-15), rulesDensity.Density(7)),
		fmt.Sprintf("M %v %v", width-margin-s, height-margin),
		fmt.Sprintf("l %v %v", rulesDensity.Density(-15), rulesDensity.Density(-7)),
		fmt.Sprintf("M %v %v", width-margin-s, height-margin),
		fmt.Sprintf("H %v", margin+r),
		fmt.Sprintf("Q %v %v, %v %v", margin, height-margin, margin, height-margin-r),
		fmt.Sprintf("V %v", margin+s),
	}
	e.borderArrow.D(arrow)

	// ── Rounded background (identical to doubleLoopArrow) ───────────────
	background := []string{
		fmt.Sprintf("M %v %v", margin+r, margin),
		fmt.Sprintf("H %v", width-margin-r),
		fmt.Sprintf("Q %v %v, %v %v", width-margin, margin, width-margin, margin+r),
		fmt.Sprintf("V %v", height-margin-r),
		fmt.Sprintf("Q %v %v, %v %v", width-margin, height-margin, width-margin-r, height-margin),
		fmt.Sprintf("H %v", margin+r),
		fmt.Sprintf("Q %v %v, %v %v", margin, height-margin, margin, height-margin-r),
		fmt.Sprintf("V %v", margin+r),
		fmt.Sprintf("Q %v %v, %v %v", margin, margin, margin+r, margin),
	}
	e.backgroundContent.D(background)

	// ── Frame and icon positioning ──────────────────────────────────────
	// Uses the EXACT same variables as doubleLoopArrow.Update() so the
	// frame occupies the identical 20×20 area in the bottom-right corner.
	cr := rulesDensity.Density(5.0)
	cx := rulesDensity.Density(20.0)
	cy := rulesDensity.Density(20.0)
	xp := width - margin - 2.0*cr - 1.5*cx  // frame top-left X = width - 50
	yp := height - margin - 2.0*cr - 1.5*cy // frame top-left Y = height - 50
	L := 2*cr + rulesDensity.Density(10)    // frame side length = 20

	// ── Hourglass icon — centered inside the frame ──────────────────────
	// The FA hourglass-half viewBox is 384×512. We scale it to fit inside
	// the frame with padding. Use the tighter dimension (width) for uniform
	// scaling so the icon isn't distorted.
	//
	// Scale: fit 384px into ~14 density units → 14/384 ≈ 0.0365
	// At this scale: width = 14.0, height = 18.7
	iconScale := (L - rulesDensity.Density(6)) / rulesDensity.Density(384) // ≈ 0.0365
	iconW := rulesDensity.Density(384) * iconScale                         // ≈ 14.0
	iconH := rulesDensity.Density(512) * iconScale                         // ≈ 18.7

	// Icon center = frame center = (width-40, height-40)
	// Same position as the red circle in the original stop button.
	frameCenterX := xp + L/2 // = width - 40
	frameCenterY := yp + L/2 // = height - 40

	// Top-left corner of the scaled icon (for SVG transform translate)
	iconX := frameCenterX - iconW/2
	iconY := frameCenterY - iconH/2

	hourglassPath := "M32 0C14.3 0 0 14.3 0 32S14.3 64 32 64l0 11c0 42.4 16.9 83.1 46.9 113.1l67.9 67.9-67.9 67.9C48.9 353.9 32 394.6 32 437l0 11c-17.7 0-32 14.3-32 32s14.3 32 32 32l320 0c17.7 0 32-14.3 32-32s-14.3-32-32-32l0-11c0-42.4-16.9-83.1-46.9-113.1l-67.9-67.9 67.9-67.9c30-30 46.9-70.7 46.9-113.1l0-11c17.7 0 32-14.3 32-32S369.7 0 352 0L32 0zM96 75l0-11 192 0 0 11c0 19-5.6 37.4-16 53L112 128c-10.3-15.6-16-34-16-53zm16 309c3.5-5.3 7.6-10.3 12.1-14.9l67.9-67.9 67.9 67.9c4.6 4.6 8.6 9.6 12.2 14.9L112 384z"

	e.hourglassIcon.D([]string{hourglassPath})
	e.hourglassIcon.Get().Call("setAttribute",
		"transform",
		fmt.Sprintf("translate(%v, %v) scale(%v)", iconX, iconY, iconScale),
	)

	// ── Frame border — rounded square around the icon ───────────────────
	// Same shape and position as stopButtonBorder in doubleLoopArrow.
	borderPath := []string{
		fmt.Sprintf("M %v %v", xp+rulesDensity.Density(5), yp),
		fmt.Sprintf("h %v", L-rulesDensity.Density(10)),
		fmt.Sprintf("a %v,%v 0 0 1 %v,%v", rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5)),
		fmt.Sprintf("v %v", L-rulesDensity.Density(10)),
		fmt.Sprintf("a %v,%v 0 0 1 -%v,%v", rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5)),
		fmt.Sprintf("h -%v", L-rulesDensity.Density(10)),
		fmt.Sprintf("a %v,%v 0 0 1 -%v,-%v", rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5)),
		fmt.Sprintf("v -%v", L-rulesDensity.Density(10)),
		fmt.Sprintf("a %v,%v 0 0 1 %v,-%v", rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5), rulesDensity.Density(5)),
		"z",
	}
	e.hourglassBorder.D(borderPath)

	// ── Connection dot and click area ───────────────────────────────────
	// Position: (width-57, height-42) — to the LEFT of the frame.
	// This is the exact same position as the stop button connection in
	// doubleLoopArrow. The small horizontal dash between the wire and the
	// frame is the connection indicator drawn by factoryConnection.
	e.intervalConnection.D(rulesConnection.GetPathDraw(width-rulesDensity.Density(57), height-rulesDensity.Density(42)))
	e.intervalConnectionArea.GetSvgPath().D(rulesConnection.GetPathAreaDraw(width-rulesDensity.Density(57), height-rulesDensity.Density(42)))
	e.intervalConnectionArea.SetXY(x+width-rulesDensity.Density(57), y+height-rulesDensity.Density(42))

	return
}
