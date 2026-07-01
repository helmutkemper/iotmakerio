// browser/factoryBrowser/newTagSvgFeDistantLight.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgFeDistantLight
//
// English:
//
// The <feDistantLight> filter primitive defines a distant light source that can be used within a lighting filter
// primitive: <feDiffuseLighting> or <feSpecularLighting>.
//
// Português:
//
// A primitiva de filtro <feDistantLight> define uma fonte de luz distante que pode ser usada em uma primitiva de filtro
// de iluminação: <feDiffuseLighting> ou <feSpecularLighting>.
func NewTagSvgFeDistantLight() (ref *html.TagSvgFeDistantLight) {
	ref = &html.TagSvgFeDistantLight{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
