// browser/factoryBrowser/newTagSvgTextPath.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgTextPath
//
// English:
//
// To render text along the shape of a <path>, enclose the text in a <textPath> element that has an href attribute with
// a reference to the <path> element.
//
// Português:
//
// Para renderizar o texto ao longo da forma de um <path>, coloque o texto em um elemento <textPath> que tenha um
// atributo href com uma referência ao elemento <path>.
func NewTagSvgTextPath() (ref *html.TagSvgTextPath) {
	ref = &html.TagSvgTextPath{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
