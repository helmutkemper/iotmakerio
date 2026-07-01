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
