// browser/factoryBrowser/newTagSvgMetadata.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgMetadata
//
// English:
//
// The <metadata> SVG element adds metadata to SVG content. Metadata is structured information about data.
// The contents of <metadata> should be elements from other XML namespaces such as RDF, FOAF, etc.
//
// Português:
//
// O elemento SVG <metadata> adiciona metadados ao conteúdo SVG. Metadados são informações estruturadas sobre dados.
// O conteúdo de <metadata> deve ser elementos de outros namespaces XML, como RDF, FOAF, etc.
func NewTagSvgMetadata() (ref *html.TagSvgMetadata) {
	ref = &html.TagSvgMetadata{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
