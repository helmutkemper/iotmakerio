// browser/factoryBrowser/newTagLegend.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagLegend
//
// English:
//
//	Create the Legend element.
//
// The <legend> HTML element represents a caption for the content of its parent <fieldset>.
//
// Português:
//
//	Crie o elemento Legenda.
//
// O elemento HTML <legend> representa uma legenda para o conteúdo de seu pai <fieldset>.
func NewTagLegend() (ref *html.TagLegend) {
	ref = &html.TagLegend{}
	ref.CreateElement(html.KTagLegend)
	ref.Id(utilsMath.GetUID())

	return ref
}
