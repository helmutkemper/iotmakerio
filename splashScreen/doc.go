// Package splashScreen provides a loading splash screen rendered entirely through
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
// the sprite package's canvas-based Stage/Element system.
//
// Unlike the previous SVG DOM-based implementation, this package creates sprite
// Elements for the overlay, background image, and dynamic text. All rendering
// goes through the shared Stage's requestAnimationFrame loop, ensuring correct
// z-ordering and no DOM pollution.
//
// Architecture:
//
//   - Three sprite.Elements are created on the shared Stage: overlay (semi-transparent
//     background), image (centered splash graphic), and text (dynamically updated
//     SVG text with automatic line wrapping).
//   - The overlay and image use very high z-index values (configurable) to appear
//     above all other Stage elements.
//   - Text is rendered as SVG XML with <tspan> elements for each line, re-cached
//     via Element.CacheFromSvg on every AddText call.
//   - Hide performs a smooth fade-out animation using requestAnimationFrame to
//     interpolate Element opacity, then removes all elements from the Stage.
//   - The Stage must be started (Stage.Start) before calling Show, so the
//     splash screen is visible during the loading process.
//
// Usage:
//
//	splash := splashScreen.New(splashScreen.Config{
//	    ImagePath: "./splashScreen/splashScreen.png",
//	})
//	err := splash.Show(spriteStage)
//	splash.AddText("Initializing systems...")
//	splash.AddText("Loading resources...")
//	splash.Hide()
//
// Português:
//
//	Package splashScreen fornece uma tela de carregamento renderizada inteiramente
//	através do sistema canvas-based Stage/Element do package sprite.
//
//	Diferente da implementação anterior baseada em SVG DOM, este package cria
//	sprite Elements para o overlay, imagem de fundo e texto dinâmico. Toda
//	renderização passa pelo loop requestAnimationFrame do Stage compartilhado,
//	garantindo z-ordering correto e nenhuma poluição do DOM.
package splashScreen
