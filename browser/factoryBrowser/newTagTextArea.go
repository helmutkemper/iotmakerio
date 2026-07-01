package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func NewTagTextArea() (ref *html.TagTextArea) {
	ref = &html.TagTextArea{}
	ref.CreateElement(html.KTagTextarea)
	ref.Id(utilsMath.GetUID())

	return ref
}
