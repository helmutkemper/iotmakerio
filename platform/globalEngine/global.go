package globalEngine

import "github.com/helmutkemper/iotmakerio/platform/engine"

var Engine *engine.Engine

func init() {
	Engine = &engine.Engine{}
	Engine.Init()
}
