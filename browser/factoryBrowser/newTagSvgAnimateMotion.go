// browser/factoryBrowser/newTagSvgAnimateMotion.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
	"github.com/helmutkemper/iotmakerio/platform/globalEngine"
	"github.com/helmutkemper/iotmakerio/utilsMath"
)

// NewTagSvgAnimateMotion
//
// English:
//
// The SVG <animateMotion> element provides a way to define how an element moves along a motion path.
//
//	Notes:
//	  * To reuse an existing path, it will be necessary to use an <mpath> element inside the <animateMotion> element
//	    instead of the path attribute.
//
// Português:
//
// O elemento SVG <animateMotion> fornece uma maneira de definir como um elemento se move ao longo de um caminho
// de movimento.
//
//	Notas:
//	  * Para reutilizar um caminho existente, será necessário usar um elemento <mpath> dentro do elemento
//	    <animateMotion> ao invés do atributo path.
func NewTagSvgAnimateMotion() (ref *html.TagSvgAnimateMotion) {
	ref = &html.TagSvgAnimateMotion{}
	ref.Engine(globalEngine.Engine) //todo: fazer em todos
	ref.Init()
	ref.Id(utilsMath.GetUID())

	return ref
}
