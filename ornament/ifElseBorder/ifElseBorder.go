// /ide/ornament/ifElseBorder/ifElseBorder.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ifElseBorder

// ifElseBorder.go — Ornament for the StatementIfElse container device.
//
// Draws a rounded rectangle border (similar to the loop, but WITHOUT arrow
// tips) with a toggle indicator at top-left and a bool connection point at
// the left-center edge.
//
// Visual layout:
//
//     ┌─────────────────────────────────────┐
//     │  ┌──────────┐                       │  ← toggle: ● true / ● false
//     │  └──────────┘                       │
//     │                                     │
//     │                                     │
//	◉──│                                     │  ← bool connection (orange)
//     │                                     │
//     │                                     │
//     │                                     │
//     └─────────────────────────────────────┘
//
// Differences from the loop ornament (doubleLoopArrow):
//   - Border: simple rounded rectangle stroke, no arrowheads
//   - Border color: purple/indigo (#6B4FBB) — distinct from loop orange
//   - Toggle area: top-left with green/red dot and "true"/"false" label
//   - Connection: bool type at left-center (not bottom-right)
//
// The toggle is purely decorative inside the SVG. Interactivity is handled
// by the StatementIfElse device's click handler (hit-test on the toggle area).
//
// Português:
//
//	Ornamento para o container StatementIfElse. Borda retangular arredondada
//	sem pontas de seta, toggle no canto superior esquerdo, conexão bool à
//	esquerda central.

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

// ── Toggle layout constants ─────────────────────────────────────────────────
// Positioned inside the border, top-left corner. These values define the
// toggle pill and the dot/label within it.

const (
	// kToggleX is the left edge of the toggle pill, relative to container origin.
	kToggleX = 20
	// kToggleY is the top edge of the toggle pill, relative to container origin.
	kToggleY = 16
	// kToggleW is the width of the toggle pill background.
	kToggleW = 80
	// kToggleH is the height of the toggle pill background.
	kToggleH = 22
	// kToggleR is the corner radius of the toggle pill.
	kToggleR = 5
	// kDotRadius is the radius of the color indicator dot inside the toggle.
	kDotRadius = 5
	// kDotX is the center X of the dot relative to the toggle pill left edge.
	kDotX = kToggleX + 12
	// kLabelX is the X position of the label text ("true"/"false").
	kLabelX = kToggleX + 24
)

// ── Connection position constants ───────────────────────────────────────────
// The bool connection sits on the LEFT edge of the container, vertically centered.

const (
// The dot is slightly outside the border, creating the visual "tab" effect.
)

// IfElseBorder draws the ornament used by StatementIfElse.
type IfElseBorder struct {
	// ── Normal state colors ─────────────────────────────────────────────
	borderNormalColor     color.RGBA
	backgroundNormalColor color.RGBA
	toggleBgNormalColor   color.RGBA

	// ── Selected state colors ───────────────────────────────────────────
	borderSelectedColor     color.RGBA
	backgroundSelectedColor color.RGBA

	// ── Toggle state ────────────────────────────────────────────────────
	// branch is "true" or "false" — controls the toggle label and dot color.
	branch string

	// ── SVG elements ────────────────────────────────────────────────────
	svg               *html.TagSvg
	backgroundContent *html.TagSvgPath
	border            *html.TagSvgPath
	toggleBg          *html.TagSvgPath
	toggleDot         *html.TagSvgPath
	toggleLabel       *html.TagSvgText

	// ── Connection ──────────────────────────────────────────────────────
	conditionConnection     *html.TagSvgPath
	conditionConnectionArea connection.Connection
}

// GetConnectionError returns any error accumulated during connection setup.
func (e *IfElseBorder) GetConnectionError() (err error) {
	return rulesConnection.GetError()
}

// ConditionButtonSetup configures the connection area for the condition port.
// Must be called before Init().
//
// Português: Configura a área de conexão para a porta de condição (bool).
func (e *IfElseBorder) ConditionButtonSetup(setup connection.Setup) {
	e.conditionConnectionArea.Setup(setup)
}

// SetBranch changes the toggle indicator between "true" and "false".
// The ornament must be re-cached after calling this (via Update + CacheFromSvg).
//
// Português: Muda o indicador do toggle entre "true" e "false".
func (e *IfElseBorder) SetBranch(branch string) {
	e.branch = branch
}

// GetBranch returns the current branch selection ("true" or "false").
func (e *IfElseBorder) GetBranch() string {
	return e.branch
}

// SetSelected toggles between normal and selected visual states.
func (e *IfElseBorder) SetSelected(selected bool) {
	if selected {
		e.backgroundContent.Fill(e.backgroundSelectedColor)
		e.border.Stroke(e.borderSelectedColor)
		return
	}
	e.backgroundContent.Fill(e.backgroundNormalColor)
	e.border.Stroke(e.borderNormalColor)
}

// GetSvg returns the SVG tag with the element design.
func (e *IfElseBorder) GetSvg() (svg *html.TagSvg) {
	return e.svg
}

// Init initializes the SVG elements with default colors.
// Must be called before Update().
//
// Color scheme:
//   - Border: purple/indigo (#6B4FBB) — distinct from loop orange
//   - Background: very light purple tint
//   - Toggle dot: green (true) or red (false)
//   - Toggle label: white text on dark pill
//   - Connection dot: orange (bool type color, via factoryConnection)
//
// Português: Inicializa os elementos SVG com cores padrão.
func (e *IfElseBorder) Init() (err error) {

	// Default branch
	if e.branch == "" {
		e.branch = "true"
	}

	// ── Normal state colors ─────────────────────────────────────────────
	e.borderNormalColor = color.RGBA{R: 107, G: 79, B: 187, A: 255}      // #6B4FBB
	e.backgroundNormalColor = color.RGBA{R: 245, G: 242, B: 255, A: 255} // very light purple
	e.toggleBgNormalColor = color.RGBA{R: 50, G: 50, B: 70, A: 255}      // dark pill bg

	// ── Selected state colors ───────────────────────────────────────────
	e.borderSelectedColor = color.RGBA{R: 140, G: 100, B: 230, A: 255} // brighter purple
	e.backgroundSelectedColor = color.RGBA{R: 235, G: 230, B: 255, A: 255}

	e.svg = factoryBrowser.NewTagSvg()

	// Background — rounded rectangle fill
	e.backgroundContent = factoryBrowser.NewTagSvgPath().
		Fill(e.backgroundNormalColor).
		Stroke("none").
		MarkerEnd("url(#backgroundContent)")
	e.svg.Append(e.backgroundContent)

	// Border — simple rounded rectangle stroke (NO arrow tips)
	e.border = factoryBrowser.NewTagSvgPath().
		Fill("none").
		Stroke(e.borderNormalColor).
		StrokeWidth(rulesDensity.NewInt(4).GetInt()).
		StrokeLineCap(html.KSvgStrokeLinecapRound).
		StrokeLineJoin(html.KSvgStrokeLinejoinRound).
		MarkerEnd("url(#border)")
	e.svg.Append(e.border)

	// Toggle pill background
	e.toggleBg = factoryBrowser.NewTagSvgPath().
		Fill(e.toggleBgNormalColor).
		Stroke("none").
		MarkerEnd("url(#toggleBg)")
	e.svg.Append(e.toggleBg)

	// Toggle dot (green/red indicator)
	e.toggleDot = factoryBrowser.NewTagSvgPath().
		Fill(color.RGBA{R: 0, G: 200, B: 0, A: 255}). // green by default (true)
		Stroke("none").
		MarkerEnd("url(#toggleDot)")
	e.svg.Append(e.toggleDot)

	// Toggle label ("true" / "false")
	e.toggleLabel = factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").
		FontWeight("bold").
		FontSize(12).
		Fill(color.RGBA{R: 220, G: 220, B: 240, A: 255}) // light text on dark pill
	e.svg.Append(e.toggleLabel)

	// Connection dot — colored by bool type (orange)
	e.conditionConnection = factoryConnection.NewConnection("bool", "url(#conditionConnection)")
	e.svg.Append(e.conditionConnection)

	// Connection area — invisible click target
	e.conditionConnectionArea.Init("url(#conditionConnectionArea)")
	e.svg.Append(e.conditionConnectionArea.GetSvgPath())

	return
}

// Update recalculates all SVG paths for the given bounding box.
// Called on init and after every resize or toggle change.
//
// Português: Recalcula todos os caminhos SVG para a caixa delimitadora dada.
func (e *IfElseBorder) Update(x, y, width, height rulesDensity.Density) (err error) {

	margin := rulesDensity.Density(10)
	cornerR := rulesDensity.Density(12)

	// ── Rounded background fill ─────────────────────────────────────────
	background := []string{
		fmt.Sprintf("M %v %v", margin+cornerR, margin),
		fmt.Sprintf("H %v", width-margin-cornerR),
		fmt.Sprintf("Q %v %v, %v %v", width-margin, margin, width-margin, margin+cornerR),
		fmt.Sprintf("V %v", height-margin-cornerR),
		fmt.Sprintf("Q %v %v, %v %v", width-margin, height-margin, width-margin-cornerR, height-margin),
		fmt.Sprintf("H %v", margin+cornerR),
		fmt.Sprintf("Q %v %v, %v %v", margin, height-margin, margin, height-margin-cornerR),
		fmt.Sprintf("V %v", margin+cornerR),
		fmt.Sprintf("Q %v %v, %v %v", margin, margin, margin+cornerR, margin),
	}
	e.backgroundContent.D(background)

	// ── Border stroke — same shape as background, no arrow tips ─────────
	// This is the key visual difference from the loop ornament: just a
	// simple rounded rectangle stroke, no arrowheads at the corners.
	e.border.D(background)

	// ── Toggle pill ─────────────────────────────────────────────────────
	tx := rulesDensity.Density(kToggleX)
	ty := rulesDensity.Density(kToggleY)
	tw := rulesDensity.Density(kToggleW)
	th := rulesDensity.Density(kToggleH)
	tr := rulesDensity.Density(kToggleR)

	toggleBgPath := []string{
		fmt.Sprintf("M %v %v", tx+tr, ty),
		fmt.Sprintf("h %v", tw-2*tr),
		fmt.Sprintf("a %v,%v 0 0 1 %v,%v", tr, tr, tr, tr),
		fmt.Sprintf("v %v", th-2*tr),
		fmt.Sprintf("a %v,%v 0 0 1 -%v,%v", tr, tr, tr, tr),
		fmt.Sprintf("h -%v", tw-2*tr),
		fmt.Sprintf("a %v,%v 0 0 1 -%v,-%v", tr, tr, tr, tr),
		fmt.Sprintf("v -%v", th-2*tr),
		fmt.Sprintf("a %v,%v 0 0 1 %v,-%v", tr, tr, tr, tr),
		"z",
	}
	e.toggleBg.D(toggleBgPath)

	// ── Toggle dot — circle indicator ───────────────────────────────────
	dotCx := rulesDensity.Density(kDotX)
	dotCy := ty + th/2
	dr := rulesDensity.Density(kDotRadius)

	dotPath := []string{
		fmt.Sprintf("M %v %v", dotCx-dr, dotCy),
		fmt.Sprintf("a %v,%v 0 1,1 %v,0", dr, dr, 2*dr),
		fmt.Sprintf("a %v,%v 0 1,1 -%v,0", dr, dr, 2*dr),
		"z",
	}
	e.toggleDot.D(dotPath)

	// Set dot color based on current branch
	if e.branch == "true" {
		e.toggleDot.Fill(color.RGBA{R: 0, G: 200, B: 0, A: 255}) // green
	} else {
		e.toggleDot.Fill(color.RGBA{R: 220, G: 60, B: 60, A: 255}) // red
	}

	// ── Toggle label ────────────────────────────────────────────────────
	e.toggleLabel.X(rulesDensity.Density(kLabelX).GetInt())
	e.toggleLabel.Y((dotCy + rulesDensity.Density(4)).GetInt())
	e.toggleLabel.Text(e.branch)

	// ── Connection dot and click area ───────────────────────────────────
	// Positioned at the LEFT edge, vertically centered.
	// The small tab/dash appears to the left of the border.
	// [PIN] the condition input uses the standard pin. The helpers take the
	// pin's BODY-SIDE point (edgeX = KWidth here) and derive the OUTER TIP —
	// which lands exactly at x=0, the container's left edge, where the wire
	// anchors.
	// Português: A entrada da condição usa o pino padrão. Os helpers recebem
	// o ponto do LADO DO CORPO (edgeX = KWidth aqui) e derivam a PONTA
	// EXTERNA — que cai exatamente em x=0, a borda esquerda do container,
	// onde o fio ancora.
	connX := rulesDensity.Density(rulesConnection.KWidth)
	connY := height / 2

	e.conditionConnection.D(rulesConnection.PinPathDraw(rulesConnection.PinSideLeft, connX, connY))
	e.conditionConnectionArea.GetSvgPath().D(rulesConnection.PinPathAreaDraw(rulesConnection.PinSideLeft, connX, connY))
	ax, ay := rulesConnection.PinAnchorD(rulesConnection.PinSideLeft, connX, connY)
	e.conditionConnectionArea.SetXY(x+ax, y+ay)

	return
}
