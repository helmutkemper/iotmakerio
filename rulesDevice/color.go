// rulesDevice/color.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesDevice

// color.go — Color utilities for the unified type→color palette.
//
// English:
//
//	This file exists because the project historically kept THREE independent
//	type→color tables:
//
//	  1. rulesDevice/palette.go   — device borders, headers, connector pins
//	  2. wire/registry.go         — wire strokes
//	  3. rulesConnection (TypeToColor) — ornament pin fills (color.RGBA)
//
//	They drifted apart (string was green on the wire but amber on the device;
//	bool pins were green while bool wires were orange). With the standard
//	connector pin becoming the physical junction between device and wire, the
//	mismatch would be visible at every connection point.
//
//	rulesDevice/palette.go is now the single source of truth. The helpers in
//	this file let the two other consumers derive their colors from it:
//
//	  - HexToRGBA converts the palette's "#RRGGBB" strings into color.RGBA for
//	    SVG-DOM consumers (the ornament pin paths).
//	  - TypeColorRGBA is the RGBA twin of TypeStyleFor(...).Color.
//	  - LightenHex derives the "selected" highlight variant used by wires, so
//	    the palette does not need a hand-maintained light color per type.
//
// Português:
//
//	Este arquivo existe porque o projeto mantinha TRÊS tabelas tipo→cor
//	independentes (devices, fios e pinos de ornament), que divergiram. Com o
//	pino padrão virando a junção física device↔fio, a divergência ficaria
//	visível em toda conexão. rulesDevice/palette.go agora é a fonte única; os
//	helpers daqui permitem que os outros consumidores derivem suas cores dela.

import (
	"fmt"
	"image/color"
	"strconv"
)

// HexToRGBA parses a CSS hex color string ("#RRGGBB" or "#RGB") into a
// color.RGBA with full opacity. Invalid input falls back to the muted text
// color so a typo renders as "visually off" instead of crashing or going
// black — the same defensive philosophy as wire.DefaultUnknownStyle.
//
// Português: Converte uma cor hex CSS ("#RRGGBB" ou "#RGB") em color.RGBA
// opaca. Entrada inválida cai na cor de texto apagada, para um typo render
// "visualmente estranho" em vez de quebrar ou ficar preto.
func HexToRGBA(hex string) color.RGBA {
	fallback := color.RGBA{R: 0x88, G: 0x99, B: 0xAA, A: 0xFF} // KColorDeviceTextMuted

	if len(hex) == 0 || hex[0] != '#' {
		return fallback
	}
	h := hex[1:]

	// Expand the short "#RGB" form to "RRGGBB".
	// Português: Expande a forma curta "#RGB" para "RRGGBB".
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return fallback
	}

	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return fallback
	}
	return color.RGBA{
		R: uint8(v >> 16),
		G: uint8(v >> 8),
		B: uint8(v),
		A: 0xFF,
	}
}

// RGBAToHex formats a color.RGBA back into the "#RRGGBB" CSS form (alpha is
// discarded — the palette is fully opaque by design).
//
// Português: Formata um color.RGBA de volta para a forma CSS "#RRGGBB"
// (alfa é descartado — a paleta é opaca por design).
func RGBAToHex(c color.RGBA) string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// LightenHex blends a hex color toward white by the given factor (0.0 keeps
// the color, 1.0 yields pure white). Used to derive per-type "selected"
// highlight colors for wires — one formula instead of a hand-maintained
// parallel palette that would drift again.
//
// Português: Mistura uma cor hex em direção ao branco pelo fator dado (0.0
// mantém a cor, 1.0 vira branco puro). Usado para derivar as cores de
// destaque "selected" dos fios — uma fórmula em vez de uma paleta paralela
// mantida à mão, que divergiria de novo.
func LightenHex(hex string, factor float64) string {
	if factor < 0 {
		factor = 0
	}
	if factor > 1 {
		factor = 1
	}
	c := HexToRGBA(hex)
	blend := func(v uint8) uint8 {
		return uint8(float64(v) + (255.0-float64(v))*factor)
	}
	return RGBAToHex(color.RGBA{R: blend(c.R), G: blend(c.G), B: blend(c.B), A: 0xFF})
}

// TypeColorRGBA is the color.RGBA twin of TypeStyleFor(goType).Color, for
// consumers that paint through the SVG DOM (html.TagSvgPath.Fill) instead of
// SVG strings. The ornament pin renderer is the primary caller.
//
// Português: Gêmeo em color.RGBA de TypeStyleFor(goType).Color, para
// consumidores que pintam pelo DOM SVG em vez de strings SVG. O renderer de
// pinos dos ornaments é o chamador principal.
func TypeColorRGBA(goType string) color.RGBA {
	return HexToRGBA(TypeStyleFor(goType).Color)
}
