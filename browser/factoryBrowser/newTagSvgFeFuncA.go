// browser/factoryBrowser/newTagSvgFeFuncA.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgFeFuncA
//
// English:
//
// The <feFuncA> SVG filter primitive defines the transfer function for the alpha component of the input graphic of its
// parent <feComponentTransfer> element.
//
// Português:
//
// A primitiva de filtro SVG <feFuncA> define a função de transferência para o componente alfa do gráfico de entrada
// de seu elemento pai <feComponentTransfer>.
func NewTagSvgFeFuncA() (ref *html.TagSvgFeFuncA) {
	ref = &html.TagSvgFeFuncA{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
