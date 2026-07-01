// browser/factoryBrowser/newTagHr.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func NewTagHr() (ref *html.TagHr) {
	ref = &html.TagHr{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
