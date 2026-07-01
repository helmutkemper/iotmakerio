// browser/factoryBrowser/newTagMeter.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagMeter
//
// English:
//
//	Create the Meter element.
//
//	The <meter> HTML element represents either a scalar value within a known range or a fractional
//	value.
//
// Português:
//
//	Crie o elemento Medidor.
//
//	O elemento HTML <meter> representa um valor escalar dentro de um intervalo conhecido ou um
//	valor fracionário.
func NewTagMeter() (ref *html.TagMeter) {
	ref = &html.TagMeter{}
	ref.CreateElement(html.KTagMeter)
	ref.Id(utilsMath.GetUID())

	return ref
}
