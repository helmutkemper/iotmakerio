package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

func NewTagVideo() (ref *html.TagVideo) {
	ref = &html.TagVideo{}
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
