// hexMenu/grid.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package hexMenu

import "math"

// HexCenter converts grid (col, row) to pixel (x, y).
// Flat-top hex layout. Col and Row are 1-based.
// Spacing adds extra pixels between hexagons (applied per grid step).
func HexCenter(col, row int, radius, spacing float64) (x, y float64) {
	x = float64(col-1) * (1.5*radius + spacing)
	y = float64(row-1) * (math.Sqrt(3)/2.0*radius + spacing)
	return
}

// HexWidth returns width of a flat-top hexagon: 2 * radius.
func HexWidth(radius float64) float64 {
	return 2.0 * radius
}

// HexHeight returns height of a flat-top hexagon: sqrt(3) * radius.
func HexHeight(radius float64) float64 {
	return math.Sqrt(3) * radius
}

// HexSvgWidth returns the SVG viewport width for a hexagon.
// Uses math.Ceil to ensure the viewport fully contains the polygon,
// preventing clipping on fractional pixel boundaries.
//
// Português: Retorna a largura do viewport SVG para um hexágono.
// Usa math.Ceil para garantir que o viewport contenha totalmente o
// polígono, evitando corte em fronteiras fracionárias de pixel.
func HexSvgWidth(radius float64) float64 {
	return math.Ceil(2.0 * radius)
}

// HexSvgHeight returns the SVG viewport height for a hexagon.
// Uses math.Ceil to ensure the viewport fully contains the polygon,
// preventing clipping on fractional pixel boundaries.
// Example: √3×52 = 90.066 → Ceil → 91 (prevents vertex clipping).
//
// Português: Retorna a altura do viewport SVG para um hexágono.
// Usa math.Ceil para garantir que o viewport contenha totalmente o
// polígono, evitando corte em fronteiras fracionárias de pixel.
// Exemplo: √3×52 = 90.066 → Ceil → 91 (evita corte de vértices).
func HexSvgHeight(radius float64) float64 {
	return math.Ceil(math.Sqrt(3) * radius)
}

// HexPolygonPath generates SVG path "d" for a flat-top hexagon at (cx, cy).
func HexPolygonPath(cx, cy, radius float64) string {
	path := "M"
	for i := 0; i < 6; i++ {
		angle := math.Pi / 180.0 * float64(60*i)
		px := cx + radius*math.Cos(angle)
		py := cy + radius*math.Sin(angle)
		if i == 0 {
			path += fmtFloat(px) + " " + fmtFloat(py)
		} else {
			path += " L" + fmtFloat(px) + " " + fmtFloat(py)
		}
	}
	path += " Z"
	return path
}

// HexContains tests if point (px, py) is inside a flat-top hexagon at (cx, cy).
func HexContains(cx, cy, radius, px, py float64) bool {
	dx := math.Abs(px - cx)
	dy := math.Abs(py - cy)
	h := math.Sqrt(3) / 2.0 * radius
	if dx > radius || dy > h {
		return false
	}
	return 2.0*h*dx+radius*dy <= 2.0*h*radius
}

// GridOffset returns offset so the grid bounding box top-left is at (0,0).
// [VIEWPORT-FIX] Uses HexSvgWidth/Height (ceiled) to match element dimensions.
func GridOffset(items []MenuItem, radius, spacing float64) (offsetX, offsetY float64) {
	if len(items) == 0 {
		return 0, 0
	}
	svgW := HexSvgWidth(radius)
	svgH := HexSvgHeight(radius)
	minX, minY := math.MaxFloat64, math.MaxFloat64
	for _, item := range items {
		cx, cy := HexCenter(item.Col, item.Row, radius, spacing)
		if cx-svgW/2 < minX {
			minX = cx - svgW/2
		}
		if cy-svgH/2 < minY {
			minY = cy - svgH/2
		}
	}
	return -minX, -minY
}

// GridBounds returns total (width, height) of all items.
// [VIEWPORT-FIX] Uses HexSvgWidth/Height (ceiled) to match element dimensions.
func GridBounds(items []MenuItem, radius, spacing float64) (width, height float64) {
	if len(items) == 0 {
		return 0, 0
	}
	svgW := HexSvgWidth(radius)
	svgH := HexSvgHeight(radius)
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
	for _, item := range items {
		cx, cy := HexCenter(item.Col, item.Row, radius, spacing)
		if cx-svgW/2 < minX {
			minX = cx - svgW/2
		}
		if cy-svgH/2 < minY {
			minY = cy - svgH/2
		}
		if cx+svgW/2 > maxX {
			maxX = cx + svgW/2
		}
		if cy+svgH/2 > maxY {
			maxY = cy + svgH/2
		}
	}
	return maxX - minX, maxY - minY
}

// --- internal formatting helpers ---

func fmtFloat(f float64) string {
	if f == math.Trunc(f) {
		return intToStr(int(f))
	}
	return floatToStr(f, 2)
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	d := make([]byte, 0, 12)
	for n > 0 {
		d = append(d, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(d)-1; i < j; i, j = i+1, j-1 {
		d[i], d[j] = d[j], d[i]
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}

func floatToStr(f float64, dec int) string {
	neg := f < 0
	if neg {
		f = -f
	}
	mult := math.Pow(10, float64(dec))
	rounded := int(math.Round(f * mult))
	ip := rounded / int(mult)
	fp := rounded % int(mult)
	fs := intToStr(fp)
	for len(fs) < dec {
		fs = "0" + fs
	}
	for len(fs) > 1 && fs[len(fs)-1] == '0' {
		fs = fs[:len(fs)-1]
	}
	r := intToStr(ip) + "." + fs
	if neg {
		return "-" + r
	}
	return r
}
