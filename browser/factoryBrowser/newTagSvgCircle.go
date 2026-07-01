// browser/factoryBrowser/newTagSvgCircle.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgCircle
//
// English:
//
// The <circle> SVG element is an SVG basic shape, used to draw circles based on a center point and a radius.
//
// Português:
//
// O elemento SVG <circle> é uma forma básica SVG, usada para desenhar círculos com base em um ponto central e um raio.
func NewTagSvgCircle() (ref *html.TagSvgCircle) {
	ref = &html.TagSvgCircle{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
