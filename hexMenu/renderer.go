// hexMenu/renderer.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package hexMenu

import (
	"math"
	"strings"
)

// gearFallbackPath is the FontAwesome "gear" icon SVG path (viewBox "0 0 512 512").
// Used as the fallback when a unicode codepoint is requested but cannot be
// rendered via webfont in SVG-as-image context (CacheFromSvg loads SVG as an
// HTML <img>, which has no access to CSS webfonts from the page).
//
// This is a copy of rulesIcon.KFAGear — kept here so the hexMenu package does
// not need to import rulesIcon (avoids coupling between the two packages).
// If the gear icon path changes in falcons.go, update this constant too.
const gearFallbackPath = "M495.9 166.6c3.2 8.7 .5 18.4-6.4 24.6l-43.3 39.4c1.1 8.3 1.7 16.8 1.7 25.4s-.6 17.1-1.7 25.4l43.3 39.4c6.9 6.2 9.6 15.9 6.4 24.6c-4.4 11.9-9.7 23.3-15.8 34.3l-4.7 8.1c-6.6 11-14 21.4-22.1 31.2c-5.9 7.2-15.7 9.6-24.5 6.8l-55.7-17.7c-13.4 10.4-28.2 19-44.1 25.4l-12.5 57.1c-2 9.1-9 16.3-18.2 17.8c-13.8 2.3-28 3.5-42.5 3.5s-28.7-1.2-42.5-3.5c-9.2-1.5-16.2-8.7-18.2-17.8l-12.5-57.1c-15.8-6.4-30.6-15-44.1-25.4L83.1 425.9c-8.8 2.8-18.6 .3-24.5-6.8c-8.1-9.8-15.5-20.2-22.1-31.2l-4.7-8.1c-6.1-11-11.4-22.4-15.8-34.3c-3.2-8.7-.5-18.4 6.4-24.6l43.3-39.4C64.6 273.1 64 264.6 64 256s.6-17.1 1.7-25.4L22.4 191.2c-6.9-6.2-9.6-15.9-6.4-24.6c4.4-11.9 9.7-23.3 15.8-34.3l4.7-8.1c6.6-11 14-21.4 22.1-31.2c5.9-7.2 15.7-9.6 24.5-6.8l55.7 17.7c13.4-10.4 28.2-19 44.1-25.4l12.5-57.1c2-9.1 9-16.3 18.2-17.8C227.3 1.2 241.5 0 256 0s28.7 1.2 42.5 3.5c9.2 1.5 16.2 8.7 18.2 17.8l12.5 57.1c15.8 6.4 30.6 15 44.1 25.4l55.7-17.7c8.8-2.8 18.6-.3 24.5 6.8c8.1 9.8 15.5 20.2 22.1 31.2l4.7 8.1c6.1 11 11.4 22.4 15.8 34.3zM256 336a80 80 0 1 0 0-160 80 80 0 1 0 0 160z"

// RenderHexagonSVG generates an SVG XML string for a single hexagon icon.
// Returns pure XML — no DOM APIs needed. Use with sprite.CacheFromSvg().
//
// Config fields are rulesDensity.Density: .GetFloat() returns the density-scaled
// pixel value automatically. No manual "value * density" needed.
//
// [VIEWPORT-FIX] SVG dimensions use math.Ceil to ensure the viewport fully
// contains the hexagon polygon. Without this, int truncation clips vertices
// that fall on fractional pixel boundaries (e.g. √3×52 = 90.066 → was 90,
// now 91), causing visible gaps between adjacent hexagons.
//
// Português: Os campos de Config são rulesDensity.Density: .GetFloat() retorna
// o valor em pixels escalado por densidade automaticamente. Sem necessidade de
// "valor * densidade" manual.
//
// [VIEWPORT-FIX] Dimensões do SVG usam math.Ceil para garantir que o viewport
// contenha totalmente o polígono hexagonal. Sem isso, truncamento por int corta
// vértices em fronteiras fracionárias de pixel, causando gaps visíveis entre
// hexágonos adjacentes.
func RenderHexagonSVG(item MenuItem, state PipelineState, config *Config) string {
	radius := config.HexRadius.GetFloat()
	bw := config.BorderWidth.GetFloat()

	w := HexWidth(radius)
	h := HexHeight(radius)
	style := item.Styles[state]

	// [VIEWPORT-FIX] Use Ceil so the SVG viewport is always large enough
	// to contain the full hexagon without clipping fractional pixels.
	svgW := int(math.Ceil(w))
	svgH := int(math.Ceil(h))

	cx := float64(svgW) / 2.0
	cy := float64(svgH) / 2.0

	hexPath := HexPolygonPath(cx, cy, radius-bw/2)

	iconSize := radius * 0.7
	iconX := cx - iconSize/2
	iconY := cy - iconSize/2 - radius*0.08

	iconX = iconX - float64(item.AdjustIconX)
	iconY = iconY - float64(item.AdjustIconY)

	labelX := cx
	labelY := cy + radius*0.55

	labelX = labelX - float64(item.AdjustLabelX)
	labelY = labelY - float64(item.AdjustLabelY)

	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="` + intToStr(svgW) + `" height="` + intToStr(svgH) + `">`

	// Hexagon body
	svg += `<path d="` + hexPath + `"`
	svg += ` fill="` + style.ColorBackground + `"`
	svg += ` stroke="` + style.ColorBorder + `"`
	svg += ` stroke-width="` + floatToStr(bw, 1) + `"/>`

	// ── Icon ─────────────────────────────────────────────────────────────
	//
	// FontAwesomePath is filled by applyIconToMenuItem (menuBuilder.go) using
	// rulesIcon.IconDefForValue(), which resolves both name and unicode icons
	// to SVG paths via the faIconByCodepoint table (populated by the generator).
	//
	// After running cmd/gen-fa-icons:
	//   icon:f287.  → FontAwesomePath = (USB path),  FontAwesomeUnicode = 0
	//   icon:gear.  → FontAwesomePath = (gear path), FontAwesomeUnicode = 0
	//
	// Before running the generator (or for unknown codepoints):
	//   icon:f287.  → FontAwesomePath = (gear path), FontAwesomeUnicode = 0xf287
	//   icon:gear.  → FontAwesomePath = (gear path), FontAwesomeUnicode = 0
	//
	// The FontAwesomeUnicode branch is therefore the "generator not yet run"
	// fallback. Both branches render a visible icon — no tofu squares.
	if item.FontAwesomePath != "" {
		vb := item.ViewBox
		if vb == "" {
			vb = "0 0 512 512"
		}
		svg += `<svg viewBox="` + vb + `"`
		svg += ` x="` + floatToStr(iconX, 1) + `"`
		svg += ` y="` + floatToStr(iconY, 1) + `"`
		svg += ` width="` + floatToStr(iconSize, 1) + `"`
		svg += ` height="` + floatToStr(iconSize, 1) + `">`
		svg += `<path fill="` + style.ColorIcon + `" d="` + item.FontAwesomePath + `"/>`
		svg += `</svg>`
	} else if item.FontAwesomeUnicode != 0 {
		// Generator not yet run — codepoint not in faIconByCodepoint.
		// applyIconToMenuItem already resolved this to the gear fallback path,
		// so FontAwesomePath should always be non-empty. This branch is a
		// last-resort safety net.
		svg += `<svg viewBox="0 0 512 512"`
		svg += ` x="` + floatToStr(iconX, 1) + `"`
		svg += ` y="` + floatToStr(iconY, 1) + `"`
		svg += ` width="` + floatToStr(iconSize, 1) + `"`
		svg += ` height="` + floatToStr(iconSize, 1) + `">`
		svg += `<path fill="` + style.ColorIcon + `" d="` + gearFallbackPath + `"/>`
		svg += `</svg>`
	}

	// Label
	// Label (supports \n for multi-line, centered)
	if item.Label != "" {
		lines := strings.Split(item.Label, "\\n")
		if len(lines) == 1 {
			svg += `<text`
			svg += ` x="` + floatToStr(labelX, 1) + `"`
			svg += ` y="` + floatToStr(labelY, 1) + `"`
			svg += ` font-family="` + config.FontFamily + `"`
			svg += ` font-size="` + intToStr(config.FontSize.GetInt()) + `"`
			svg += ` fill="` + style.ColorLabel + `"`
			svg += ` text-anchor="middle">`
			svg += escapeXML(lines[0])
			svg += `</text>`
		} else {
			// Multi-line: first tspan at labelY, subsequent shift down by 1.1em
			fontSize := config.FontSize.GetInt()
			startY := labelY - float64(len(lines)-1)*float64(fontSize)*0.55
			svg += `<text`
			svg += ` x="` + floatToStr(labelX, 1) + `"`
			svg += ` y="` + floatToStr(startY, 1) + `"`
			svg += ` font-family="` + config.FontFamily + `"`
			svg += ` font-size="` + intToStr(fontSize) + `"`
			svg += ` fill="` + style.ColorLabel + `"`
			svg += ` text-anchor="middle">`
			for i, line := range lines {
				svg += `<tspan x="` + floatToStr(labelX, 1) + `"`
				if i > 0 {
					svg += ` dy="1.1em"`
				}
				svg += `>` + escapeXML(line) + `</tspan>`
			}
			svg += `</text>`
		}
	}

	svg += `</svg>`
	return svg
}

func escapeXML(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			out = append(out, '&', 'a', 'm', 'p', ';')
		case '<':
			out = append(out, '&', 'l', 't', ';')
		case '>':
			out = append(out, '&', 'g', 't', ';')
		case '"':
			out = append(out, '&', 'q', 'u', 'o', 't', ';')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// formatHex returns the lowercase hex string for a Unicode codepoint,
// with a minimum of 4 digits (padded with leading zeros).
// Used to build XML character references like "&#xf287;".
func formatHex(cp uint32) string {
	const hexDigits = "0123456789abcdef"
	if cp == 0 {
		return "0000"
	}
	// Build digits in reverse, then reverse the result.
	var buf [8]byte
	n := 0
	v := cp
	for v > 0 {
		buf[n] = hexDigits[v&0xf]
		n++
		v >>= 4
	}
	// Pad to minimum 4 digits.
	for n < 4 {
		buf[n] = '0'
		n++
	}
	// Reverse.
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = buf[n-1-i]
	}
	return string(out)
}
