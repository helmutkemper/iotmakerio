// browser/factoryBrowser/newTagLabel.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagLabel
//
// English:
//
//	Create the Label element.
//
// The <label> HTML element represents a caption for an item in a user interface.
//
// Português:
//
//	Cria o elemento Label.
//
// O elemento HTML <label> representa uma legenda para um item em uma interface do usuário.
func NewTagLabel() (ref *html.TagLabel) {
	ref = new(html.TagLabel)
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
