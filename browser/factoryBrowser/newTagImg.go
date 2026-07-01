// browser/factoryBrowser/newTagImg.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func NewTagImg() (ref *html.TagImg) {
	ref = &html.TagImg{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
