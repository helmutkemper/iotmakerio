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
