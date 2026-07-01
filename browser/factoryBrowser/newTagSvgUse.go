package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgUse
//
// English:
//
// The <use> element takes nodes from within the SVG document, and duplicates them somewhere else.
//
// Português:
//
// O elemento <use> pega nós de dentro do documento SVG e os duplica em outro lugar.
func NewTagSvgUse() (ref *html.TagSvgUse) {
	ref = &html.TagSvgUse{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
