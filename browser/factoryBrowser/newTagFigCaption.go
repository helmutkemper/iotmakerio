// browser/factoryBrowser/newTagFigCaption.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagFigCaption
//
// English:
//
// The <figcaption> HTML element represents a caption or legend describing the rest of the contents
// of its parent <figure> element.
//
// Português:
//
// O elemento HTML <figcaption> representa uma legenda ou legenda descrevendo o restante do conteúdo
// de seu elemento pai <figure>.
func NewTagFigCaption() (ref *html.TagFigCaption) {
	ref = &html.TagFigCaption{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
