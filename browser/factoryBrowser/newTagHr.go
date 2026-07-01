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
