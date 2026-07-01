// browser/factoryBrowser/newTagSvgAnimate.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgAnimate
//
// English:
//
//	The SVG <animate> element provides a way to animate an attribute of an element over time.
//
// Português:
//
//	O elemento SVG <animate> fornece uma maneira de animar um atributo de um elemento ao longo do tempo.
func NewTagSvgAnimate() (ref *html.TagSvgAnimate) {
	ref = &html.TagSvgAnimate{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
