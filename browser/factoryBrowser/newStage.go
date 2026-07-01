package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/stage"
	"github.com/helmutkemper/iotmakerio/platform/globalEngine"
)

func NewStage() (ref *stage.Stage) {
	ref = &stage.Stage{}
	ref.Engine(globalEngine.Engine)
	ref.Init()

	return ref
}
