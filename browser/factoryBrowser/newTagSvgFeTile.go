// browser/factoryBrowser/newTagSvgFeTile.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgFeTile
//
// English:
//
// The <feTile> SVG filter primitive allows to fill a target rectangle with a repeated, tiled pattern of an input image.
// The effect is similar to the one of a <pattern>.
//
// Português:
//
// A primitiva de filtro SVG <feTile> permite preencher um retângulo de destino com um padrão repetido e lado a lado de
// uma imagem de entrada.
// O efeito é semelhante ao de um <pattern>.
func NewTagSvgFeTile() (ref *html.TagSvgFeTile) {
	ref = &html.TagSvgFeTile{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
