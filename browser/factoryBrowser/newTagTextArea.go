// browser/factoryBrowser/newTagTextArea.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func NewTagTextArea() (ref *html.TagTextArea) {
	ref = &html.TagTextArea{}
	ref.CreateElement(html.KTagTextarea)
	ref.Id(utilsMath.GetUID())

	return ref
}
