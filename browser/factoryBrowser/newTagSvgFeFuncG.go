// browser/factoryBrowser/newTagSvgFeFuncG.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgFeFuncG
//
// English:
//
// The <feFuncG> SVG filter primitive defines the transfer function for the green component of the input graphic of
// its parent <feComponentTransfer> element.
//
// Português:
//
// A primitiva de filtro SVG <feFuncG> define a função de transferência para o componente verde do gráfico de entrada
// de seu elemento pai <feComponentTransfer>.
func NewTagSvgFeFuncG() (ref *html.TagSvgFeFuncG) {
	ref = &html.TagSvgFeFuncG{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
