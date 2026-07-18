// /ide/ornament/caseBorder/caseBorder.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package seqBorder

// caseBorder.go — Ornament for the StatementCase container device.
//
// Draws a rounded rectangle border (no arrow tips) with a "case pill" at
// top-left showing the active case label plus a dropdown caret, and a typed
// selector connection point at the left-center edge.
//
// Visual layout:
//
//     ┌─────────────────────────────────────┐
//     │  ┌──────────────┐                    │  ← case pill: "case 0  ▾"
//     │  └──────────────┘                    │
//     │                                      │
//	◉──│                                      │  ← selector connection (typed)
//     │                                      │
//     └─────────────────────────────────────┘
//
// This is the N-way analogue of ifElseBorder: instead of a green/red true/false
// dot the pill shows the currently-selected case's label, and instead of a
// fixed bool connection the selector connection is coloured by the selector's
// data type (int for the first version; bool selectors lower to if/else and use
// the ifElseBorder ornament instead).
//
// The pill is purely decorative inside the SVG — interactivity (cycling /
// opening the case dropdown) is handled by the StatementCase device's click
// handler, which hit-tests the pill area. Keep the pill geometry below in sync
// with the device's hit-test constants.
//
// Português:
//
//	Ornamento do container StatementCase. Borda retangular arredondada, "pill"
//	no canto superior esquerdo com o label da case ativa e um caret de dropdown,
//	e conexão do selector (tipada) à esquerda central. É o análogo N-vias do
//	ifElseBorder.

import (
	"fmt"
	"image/color"

	"github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/rulesConnection"
	"github.com/helmutkemper/iotmakerio/rulesDensity"
)

// ── Pill layout constants ───────────────────────────────────────────────────
// Positioned inside the border, top-left corner. These values define the
// pill background, the label text within it and the dropdown caret. The
// StatementCase device hit-tests the same rectangle to detect pill clicks, so
// any change here must be mirrored in the device's wireEvents/cursor hit-test.

const (
	// KPillX is the left edge of the pill, relative to container origin.
	KPillX = 20
	// KPillY is the top edge of the pill, relative to container origin.
	KPillY = 16
	// KPillW is the width of the pill background (wide enough for a case label
	// plus the caret).
	KPillW = 120
	// KPillH is the height of the pill background.
	KPillH = 22
	// KPillR is the corner radius of the pill.
	KPillR = 5
	// kLabelX is the X position of the label text, relative to container origin.
	kLabelX = KPillX + 10
	// kCaretRightPad is the gap between the caret and the pill's right edge.
	kCaretRightPad = 12
)

// ── Connection position constants ───────────────────────────────────────────
// The selector connection sits on the LEFT edge of the container, centered.

const ()

// SeqBorder draws the ornament used by StatementCase.
type SeqBorder struct {
	// ── Normal state colors ─────────────────────────────────────────────
	borderNormalColor     color.RGBA
	backgroundNormalColor color.RGBA
	pillBgNormalColor     color.RGBA

	// ── Selected state colors ───────────────────────────────────────────
	borderSelectedColor     color.RGBA
	backgroundSelectedColor color.RGBA

	// ── Case state ──────────────────────────────────────────────────────
	// caseLabel is the text shown in the pill (the active case's label).
	caseLabel string

	// ── SVG elements ────────────────────────────────────────────────────
	svg               *html.TagSvg
	backgroundContent *html.TagSvgPath
	border            *html.TagSvgPath
	pillBg            *html.TagSvgPath
	pillLabel         *html.TagSvgText
	pillCaret         *html.TagSvgPath

	// ── Connection ──────────────────────────────────────────────────────
}

// GetConnectionError returns any error accumulated during connection setup.
func (e *SeqBorder) GetConnectionError() (err error) {
	return rulesConnection.GetError()
}

// SetCaseLabel sets the text shown in the pill (the active case's label).
// The ornament must be re-cached after calling this (via Update + CacheFromSvg).
//
// Português: Define o texto do pill (label da case ativa).
func (e *SeqBorder) SetCaseLabel(label string) {
	e.caseLabel = label
}

// GetCaseLabel returns the current pill label.
func (e *SeqBorder) GetCaseLabel() string {
	return e.caseLabel
}

// SetSelected toggles between normal and selected visual states.
func (e *SeqBorder) SetSelected(selected bool) {
	if selected {
		e.backgroundContent.Fill(e.backgroundSelectedColor)
		e.border.Stroke(e.borderSelectedColor)
		return
	}
	e.backgroundContent.Fill(e.backgroundNormalColor)
	e.border.Stroke(e.borderNormalColor)
}

// GetSvg returns the SVG tag with the element design.
func (e *SeqBorder) GetSvg() (svg *html.TagSvg) {
	return e.svg
}

// Init initializes the SVG elements with default colors.
// Must be called before Update().
//
// Color scheme:
//   - Border: teal (#1F8A8A) — distinct from if/else purple and loop orange
//   - Background: very light teal tint
//   - Pill: dark bg with light label text and a light caret
//   - Connection dot: coloured by the selector type (via factoryConnection)
//
// Português: Inicializa os elementos SVG com as cores padrão.
func (e *SeqBorder) Init() (err error) {

	if e.caseLabel == "" {
		e.caseLabel = "case"
	}

	// ── Normal state colors ─────────────────────────────────────────────
	e.borderNormalColor = color.RGBA{R: 31, G: 138, B: 138, A: 255}      // #1F8A8A teal
	e.backgroundNormalColor = color.RGBA{R: 240, G: 250, B: 250, A: 255} // very light teal
	e.pillBgNormalColor = color.RGBA{R: 40, G: 60, B: 62, A: 255}        // dark pill bg

	// ── Selected state colors ───────────────────────────────────────────
	e.borderSelectedColor = color.RGBA{R: 50, G: 180, B: 180, A: 255} // brighter teal
	e.backgroundSelectedColor = color.RGBA{R: 228, G: 246, B: 246, A: 255}

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

	// Pill background
	e.pillBg = factoryBrowser.NewTagSvgPath().
		Fill(e.pillBgNormalColor).
		Stroke("none").
		MarkerEnd("url(#pillBg)")
	e.svg.Append(e.pillBg)

	// Pill label (active case)
	e.pillLabel = factoryBrowser.NewTagSvgText().
		FontFamily("Arial,sans-serif").
		FontWeight("bold").
		FontSize(12).
		Fill(color.RGBA{R: 220, G: 235, B: 235, A: 255}) // light text on dark pill
	e.svg.Append(e.pillLabel)

	// Pill caret (dropdown hint — a small downward chevron)
	e.pillCaret = factoryBrowser.NewTagSvgPath().
		Fill("none").
		Stroke(color.RGBA{R: 200, G: 220, B: 220, A: 255}).
		StrokeWidth(rulesDensity.NewInt(2).GetInt()).
		StrokeLineCap(html.KSvgStrokeLinecapRound).
		StrokeLineJoin(html.KSvgStrokeLinejoinRound).
		MarkerEnd("url(#pillCaret)")
	e.svg.Append(e.pillCaret)

	return
}

// Update recalculates all SVG paths for the given bounding box.
// Called on init and after every resize or case change.
//
// Português: Recalcula todos os caminhos SVG para a caixa dada.
func (e *SeqBorder) Update(x, y, width, height rulesDensity.Density) (err error) {

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
	e.border.D(background)

	// ── Pill ────────────────────────────────────────────────────────────
	px := rulesDensity.Density(KPillX)
	py := rulesDensity.Density(KPillY)
	pw := rulesDensity.Density(KPillW)
	ph := rulesDensity.Density(KPillH)
	pr := rulesDensity.Density(KPillR)

	pillBgPath := []string{
		fmt.Sprintf("M %v %v", px+pr, py),
		fmt.Sprintf("h %v", pw-2*pr),
		fmt.Sprintf("a %v,%v 0 0 1 %v,%v", pr, pr, pr, pr),
		fmt.Sprintf("v %v", ph-2*pr),
		fmt.Sprintf("a %v,%v 0 0 1 -%v,%v", pr, pr, pr, pr),
		fmt.Sprintf("h -%v", pw-2*pr),
		fmt.Sprintf("a %v,%v 0 0 1 -%v,-%v", pr, pr, pr, pr),
		fmt.Sprintf("v -%v", ph-2*pr),
		fmt.Sprintf("a %v,%v 0 0 1 %v,-%v", pr, pr, pr, pr),
		"z",
	}
	e.pillBg.D(pillBgPath)

	// ── Pill label ──────────────────────────────────────────────────────
	e.pillLabel.X(rulesDensity.Density(kLabelX).GetInt())
	e.pillLabel.Y((py + ph/2 + rulesDensity.Density(4)).GetInt())
	e.pillLabel.Text(e.caseLabel)

	// ── Pill caret (downward chevron near the pill's right edge) ─────────
	caretCx := px + pw - rulesDensity.Density(kCaretRightPad)
	caretCy := py + ph/2
	cw := rulesDensity.Density(4) // half-width of the chevron
	cd := rulesDensity.Density(3) // chevron depth
	caretPath := []string{
		fmt.Sprintf("M %v %v", caretCx-cw, caretCy-cd/2),
		fmt.Sprintf("L %v %v", caretCx, caretCy+cd),
		fmt.Sprintf("L %v %v", caretCx+cw, caretCy-cd/2),
	}
	e.pillCaret.D(caretPath)

	// ── Connection pin and click area (LEFT edge, vertically centered) ──
	// [PIN] the selector input uses the standard pin. The helpers take the
	// pin's BODY-SIDE point (edgeX = KWidth here), draw the body toward the
	// border and derive the OUTER TIP — which lands exactly at x=0, the
	// container's left edge, where the wire anchors.
	// SeqBorder (2026-07-16): the Sequence has NO selector pin — the order
	// device is semantically transparent and takes no data input. The pill
	// (active-phase picker) is the whole left-edge UI. Português: O
	// Sequence NÃO tem pino de seletor — a pill é toda a UI da borda.

	return
}
