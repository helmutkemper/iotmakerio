// browser/factoryBrowser/NewSprite.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package factoryBrowser

import (
	"github.com/helmutkemper/iotmakerio/browser/html"
)

func NewSprite(
	canvas *html.TagCanvas,
	imgPath string,
	width, height int,

) (ref *html.Sprite) {

	ref = new(html.Sprite)
	ref.Canvas(canvas)
	ref.Image(imgPath)
	ref.SpriteWidth(width)
	ref.SpriteHeight(height)
	ref.Init()
	return ref
}
