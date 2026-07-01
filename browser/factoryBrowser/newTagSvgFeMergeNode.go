package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgFeMergeNode
//
// English:
//
// The feMergeNode takes the result of another filter to be processed by its parent <feMerge>.
//
// Português:
//
// O feMergeNode recebe o resultado de outro filtro para ser processado por seu pai <feMerge>.
func NewTagSvgFeMergeNode() (ref *html.TagSvgFeMergeNode) {
	ref = &html.TagSvgFeMergeNode{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
