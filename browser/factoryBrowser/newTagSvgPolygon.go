// browser/factoryBrowser/newTagSvgPolygon.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgPolygon
//
// English:
//
// The <polygon> element defines a closed shape consisting of a set of connected straight line segments. The last point
// is connected to the first point.
//
// For open shapes, see the <polyline> element.
//
// Português:
//
// O elemento <polygon> define uma forma fechada que consiste em um conjunto de segmentos de linha reta conectados.
// O último ponto está conectado ao primeiro ponto.
//
// Para formas abertas, consulte o elemento <polyline>.
func NewTagSvgPolygon() (ref *html.TagSvgPolygon) {
	ref = &html.TagSvgPolygon{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
