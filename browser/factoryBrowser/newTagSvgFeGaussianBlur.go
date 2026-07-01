// browser/factoryBrowser/newTagSvgFeGaussianBlur.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgFeGaussianBlur
//
// English:
//
// The <feGaussianBlur> SVG filter primitive blurs the input image by the amount specified in stdDeviation, which
// defines the bell-curve.
//
// Português:
//
// A primitiva de filtro SVG <feGaussianBlur> desfoca a imagem de entrada pela quantidade especificada em stdDeviation,
// que define a curva de sino.
func NewTagSvgFeGaussianBlur() (ref *html.TagSvgFeGaussianBlur) {
	ref = &html.TagSvgFeGaussianBlur{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
