// browser/factoryBrowser/newTagSvgLine.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgLine
//
// English:
//
// The <line> element is an SVG basic shape used to create a line connecting two points.
//
// Português:
//
// O elemento <line> é uma forma básica SVG usada para criar uma linha conectando dois pontos.
func NewTagSvgLine() (ref *html.TagSvgLine) {
	ref = &html.TagSvgLine{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
