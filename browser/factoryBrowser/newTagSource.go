// browser/factoryBrowser/newTagSource.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func NewTagSource() (ref *html.TagSource) {
	ref = &html.TagSource{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
