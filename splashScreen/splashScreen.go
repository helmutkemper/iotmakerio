// splashScreen/splashScreen.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package splashScreen

import (
	"errors"
	"syscall/js"

	// Import path must match the project layout. The user should adjust this to
	// their actual module path.
	//
	// Português: O caminho de import deve corresponder ao layout do projeto. O
	// usuário deve ajustar para o caminho real do módulo.
	"github.com/helmutkemper/iotmakerio/sprite"
)

// =====================================================================
//  Errors | Erros
// =====================================================================

var (
	// ErrNoImage is returned when neither ImagePath nor SvgXml is set in Config.
	//
	// Português: Retornado quando nem ImagePath nem SvgXml estão definidos no Config.
	ErrNoImage = errors.New("splashScreen: either ImagePath or SvgXml must be provided")

	// ErrAlreadyShowing is returned when Show is called while the splash is visible.
	//
	// Português: Retornado quando Show é chamado enquanto o splash está visível.
	ErrAlreadyShowing = errors.New("splashScreen: splash screen is already showing")

	// ErrNotShowing is returned when AddText, Clear, or Hide is called while not showing.
	//
	// Português: Retornado quando AddText, Clear ou Hide é chamado sem estar visível.
	ErrNotShowing = errors.New("splashScreen: splash screen is not showing")
)

// =====================================================================
//  SplashScreen | Tela de Carregamento
// =====================================================================

// SplashScreen
//
// English:
//
//	Manages a loading splash screen composed of three sprite.Elements on a shared
//	Stage: a full-screen semi-transparent overlay, a centered splash image, and
//	a dynamically updated text area.
//
//	All rendering is handled by the sprite Stage's render loop. The Stage must be
//	started (Start) before calling Show so that the splash is visible during loading.
//
//	SplashScreen is not safe for concurrent use from multiple goroutines. All methods
//	should be called sequentially from the same goroutine (typically main).
//
// Português:
//
//	Gerencia uma tela de carregamento composta por três sprite.Elements em um Stage
//	compartilhado: um overlay semi-transparente de tela cheia, uma imagem de splash
//	centralizada e uma área de texto atualizada dinamicamente.
//
//	Toda renderização é tratada pelo loop de render do sprite Stage. O Stage deve
//	estar iniciado (Start) antes de chamar Show para que o splash seja visível
//	durante o carregamento.
type SplashScreen struct {
	config Config
	stage  sprite.Stage

	// Sprite elements created on the Stage.
	// Português: Elementos sprite criados no Stage.
	overlayElem sprite.Element
	imageElem   sprite.Element
	textElem    sprite.Element

	// State
	// Português: Estado
	showing bool

	// Text state: accumulated lines (newest first, like the original).
	// Português: Estado do texto: linhas acumuladas (mais recentes primeiro, como o original).
	textLines []string

	// Computed layout values (calculated in Show after image loads).
	// Português: Valores de layout computados (calculados em Show após imagem carregar).
	lineHeight    float64
	textBoxX      float64
	textBoxY      float64
	textBoxWidth  float64
	textBoxHeight float64
}

// New
//
// English:
//
//	Creates a new SplashScreen with the given configuration. Does not create any
//	sprite Elements — call Show to make it visible on a Stage.
//
// Português:
//
//	Cria um novo SplashScreen com a configuração fornecida. Não cria nenhum
//	sprite Element — chame Show para torná-lo visível em um Stage.
func New(config Config) (s *SplashScreen) {
	s = &SplashScreen{
		config:    applyDefaults(config),
		textLines: make([]string, 0),
	}
	return
}

// Show
//
// English:
//
//	Creates the splash screen elements on the given Stage and makes them visible.
//	This method blocks while the overlay SVG and splash image are loaded/cached.
//	The Stage must already be started (Start) for the splash to be visible on screen.
//
//	Internally creates three sprite.Elements:
//	  - overlay: full-canvas semi-transparent rectangle (ZIndex)
//	  - image:   centered splash graphic (ZIndex + 1)
//	  - text:    dynamic text area on top of the image (ZIndex + 2)
//
//	The image is loaded from Config.SvgXml (priority) or Config.ImagePath.
//	After loading, the image is scaled to fit within the canvas (minus border)
//	while preserving its aspect ratio, and centered.
//
// Português:
//
//	Cria os elementos do splash screen no Stage fornecido e os torna visíveis.
//	Este método bloqueia enquanto o SVG do overlay e a imagem do splash são
//	carregados/cacheados. O Stage deve estar iniciado (Start) para o splash
//	ser visível na tela.
func (s *SplashScreen) Show(stage sprite.Stage) (err error) {
	if s.showing {
		err = ErrAlreadyShowing
		return
	}

	if s.config.ImagePath == "" && s.config.SvgXml == "" {
		err = ErrNoImage
		return
	}

	s.stage = stage
	cfg := s.config

	canvasW, canvasH := stage.GetCanvasSize()
	border := cfg.Border

	// Usable area after border.
	// Português: Área utilizável após a borda.
	areaW := canvasW - border
	areaH := canvasH - border

	// -----------------------------------------------------------------
	//  1. Create overlay element (full-canvas semi-transparent rect).
	// -----------------------------------------------------------------
	overlaySvg := buildOverlaySvg(canvasW, canvasH, cfg.OverlayColor)

	s.overlayElem, err = stage.CreateElement(sprite.ElementConfig{
		ID:     cfg.ElementPrefix + "_overlay",
		X:      0,
		Y:      0,
		Width:  float64(canvasW),
		Height: float64(canvasH),
		Index:  cfg.ZIndex,
	})
	if err != nil {
		return
	}

	err = s.overlayElem.CacheFromSvg(overlaySvg)
	if err != nil {
		return
	}

	// -----------------------------------------------------------------
	//  2. Create image element — load and center.
	// -----------------------------------------------------------------
	s.imageElem, err = stage.CreateElement(sprite.ElementConfig{
		ID:    cfg.ElementPrefix + "_image",
		Index: cfg.ZIndex + 1,
		// Width/Height left at 0 so CacheFromSvg/CacheFromImageSrc will use
		// the image's natural dimensions.
	})
	if err != nil {
		return
	}

	// Load image from SVG XML or from a path.
	// Português: Carrega imagem de SVG XML ou de um caminho.
	if cfg.SvgXml != "" {
		err = s.imageElem.CacheFromSvg(cfg.SvgXml)
	} else {
		err = s.imageElem.CacheFromImageSrc(cfg.ImagePath)
	}
	if err != nil {
		return
	}

	// After caching, the element has the image's natural dimensions.
	// Scale to fit within the usable area while preserving aspect ratio.
	//
	// Português: Após o cache, o elemento tem as dimensões naturais da imagem.
	// Escala para caber na área utilizável preservando a proporção.
	imgW, imgH := s.imageElem.GetSize()

	widthRatio := float64(areaW) / imgW
	heightRatio := float64(areaH) / imgH

	var scale float64
	if widthRatio < heightRatio {
		scale = widthRatio
	} else {
		scale = heightRatio
	}

	scaledW := imgW * scale
	scaledH := imgH * scale

	// Center within the canvas (accounting for border).
	// Português: Centraliza no canvas (considerando a borda).
	imgX := float64(areaW)/2 - scaledW/2 + float64(border)/2
	imgY := float64(areaH)/2 - scaledH/2 + float64(border)/2

	s.imageElem.SetPosition(imgX, imgY)
	s.imageElem.SetSize(scaledW, scaledH)

	// -----------------------------------------------------------------
	//  3. Compute text box layout relative to the positioned image.
	// -----------------------------------------------------------------
	s.textBoxX = imgX + scaledW*cfg.TextBox.X
	s.textBoxY = imgY + scaledH*cfg.TextBox.Y
	s.textBoxWidth = scaledW * cfg.TextBox.Width
	s.textBoxHeight = scaledH * cfg.TextBox.Height

	s.lineHeight = measureLineHeight(
		cfg.FontFamily, cfg.FontSize, cfg.FontWeight, cfg.FontStyle,
	)

	// -----------------------------------------------------------------
	//  4. Create text element (initially empty — populated by AddText).
	// -----------------------------------------------------------------
	s.textElem, err = stage.CreateElement(sprite.ElementConfig{
		ID:     cfg.ElementPrefix + "_text",
		X:      s.textBoxX,
		Y:      s.textBoxY,
		Width:  s.textBoxWidth,
		Height: s.textBoxHeight,
		Index:  cfg.ZIndex + 2,
	})
	if err != nil {
		return
	}

	s.textLines = make([]string, 0)
	s.showing = true

	stage.MarkDirty()
	return
}

// AddText
//
// English:
//
//	Adds a text message to the splash screen. Long messages are automatically
//	wrapped to fit the text box width. New messages appear at the top, pushing
//	older messages down. Messages that overflow the text box height are discarded
//	(oldest first), matching the original splashScreen behavior.
//
//	Regenerates the text SVG and re-caches the text element. This blocks briefly
//	while the SVG image is loaded.
//
// Português:
//
//	Adiciona uma mensagem de texto à tela de splash. Mensagens longas são
//	automaticamente quebradas para caber na largura da caixa de texto. Novas
//	mensagens aparecem no topo, empurrando mensagens mais antigas para baixo.
//	Mensagens que ultrapassam a altura da caixa de texto são descartadas
//	(mais antigas primeiro), correspondendo ao comportamento do splashScreen original.
func (s *SplashScreen) AddText(text string) (err error) {
	if !s.showing {
		err = ErrNotShowing
		return
	}

	cfg := s.config

	// Wrap the new text into lines that fit the text box.
	// Português: Quebra o novo texto em linhas que cabem na caixa de texto.
	newLines := wrapText(
		text, s.textBoxWidth,
		cfg.FontFamily, cfg.FontSize, cfg.FontWeight, cfg.FontStyle,
	)

	// Prepend new lines (newest at top, like the original).
	// Português: Insere novas linhas no início (mais recentes no topo, como o original).
	s.textLines = append(newLines, s.textLines...)

	// Remove excess lines that overflow the text box height.
	// Português: Remove linhas excedentes que ultrapassam a altura da caixa de texto.
	s.trimExcessLines()

	// Rebuild and re-cache the text SVG.
	// Português: Reconstrói e re-cacheia o SVG de texto.
	err = s.updateTextCache()
	return
}

// Clear
//
// English:
//
//	Removes all text from the splash screen. The text element remains but is blank.
//
// Português:
//
//	Remove todo o texto da tela de splash. O elemento de texto permanece mas fica vazio.
func (s *SplashScreen) Clear() (err error) {
	if !s.showing {
		err = ErrNotShowing
		return
	}

	s.textLines = make([]string, 0)
	s.textElem.InvalidateCache()
	s.stage.MarkDirty()
	return
}

// Hide
//
// English:
//
//	Fades out the splash screen using a requestAnimationFrame-driven opacity animation,
//	then removes all splash elements from the Stage. This method blocks the calling
//	goroutine until the animation completes.
//
//	The animation interpolates the opacity of all three elements from 1.0 to 0.0
//	over Config.FadeDurationMs milliseconds. After completion, elements are removed
//	from the Stage and destroyed.
//
// Português:
//
//	Faz fade-out da tela de splash usando uma animação de opacidade via
//	requestAnimationFrame, então remove todos os elementos do splash do Stage.
//	Este método bloqueia a goroutine chamadora até a animação completar.
func (s *SplashScreen) Hide() (err error) {
	if !s.showing {
		err = ErrNotShowing
		return
	}

	duration := s.config.FadeDurationMs

	done := make(chan struct{}, 1)
	startTime := 0.0

	var animFn js.Func
	animFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		timestamp := args[0].Float()

		if startTime == 0 {
			startTime = timestamp
		}

		elapsed := timestamp - startTime
		progress := elapsed / duration
		if progress > 1.0 {
			progress = 1.0
		}

		opacity := 1.0 - progress

		// Update opacity on all splash elements.
		// Português: Atualiza opacidade em todos os elementos do splash.
		s.overlayElem.SetOpacity(opacity)
		s.imageElem.SetOpacity(opacity)
		if s.textElem.IsCached() {
			s.textElem.SetOpacity(opacity)
		}

		if progress < 1.0 {
			// Schedule next frame.
			// Português: Agenda próximo frame.
			js.Global().Call("requestAnimationFrame", animFn)
		} else {
			// Animation complete — clean up.
			// Português: Animação completa — limpa.
			animFn.Release()
			s.cleanup()
			close(done)
		}

		return nil
	})

	js.Global().Call("requestAnimationFrame", animFn)

	// Block until animation completes. In Go WASM this yields to the JS event loop,
	// allowing requestAnimationFrame callbacks to fire.
	//
	// Português: Bloqueia até a animação completar. Em Go WASM isso libera para o
	// event loop JS, permitindo que callbacks de requestAnimationFrame disparem.
	<-done

	return
}

// IsShowing
//
// English:
//
//	Returns whether the splash screen is currently visible.
//
// Português:
//
//	Retorna se a tela de splash está atualmente visível.
func (s *SplashScreen) IsShowing() (showing bool) {
	showing = s.showing
	return
}

// Destroy
//
// English:
//
//	Immediately removes and destroys all splash elements without animation.
//	Use when you need to remove the splash screen instantly (e.g., on error).
//	Safe to call whether the splash is showing or not.
//
// Português:
//
//	Remove e destrói imediatamente todos os elementos do splash sem animação.
//	Use quando precisar remover a tela de splash instantaneamente (ex: em erro).
//	Seguro chamar esteja o splash visível ou não.
func (s *SplashScreen) Destroy() {
	if s.showing {
		s.cleanup()
	}
}

// =====================================================================
//  Private | Privado
// =====================================================================

// trimExcessLines
//
// English:
//
//	Removes lines from the end of the textLines slice (oldest messages) until
//	the total height fits within the text box. This replicates the original
//	splashScreen's removesExcessLines behavior.
//
// Português:
//
//	Remove linhas do final do slice textLines (mensagens mais antigas) até que
//	a altura total caiba na caixa de texto. Replica o comportamento
//	removesExcessLines do splashScreen original.
func (s *SplashScreen) trimExcessLines() {
	lineStep := s.lineHeight + float64(s.config.TextPadding)

	for len(s.textLines) > 0 {
		totalHeight := float64(len(s.textLines)) * lineStep
		if totalHeight <= s.textBoxHeight {
			break
		}
		// Remove the last line (oldest).
		// Português: Remove a última linha (mais antiga).
		s.textLines = s.textLines[:len(s.textLines)-1]
	}
}

// updateTextCache
//
// English:
//
//	Rebuilds the text SVG from the current textLines and re-caches it on the
//	text sprite Element. Blocks briefly while the SVG image loads.
//
// Português:
//
//	Reconstrói o SVG de texto a partir das textLines atuais e re-cacheia no
//	sprite Element de texto. Bloqueia brevemente enquanto a imagem SVG carrega.
func (s *SplashScreen) updateTextCache() (err error) {
	if len(s.textLines) == 0 {
		s.textElem.InvalidateCache()
		s.stage.MarkDirty()
		return
	}

	cfg := s.config

	svgXml := buildTextSvg(
		s.textLines,
		s.textBoxWidth,
		s.textBoxHeight,
		cfg.FontFamily,
		cfg.FontSize,
		cfg.FontWeight,
		cfg.FontStyle,
		cfg.TextColor,
		cfg.TextPadding,
		s.lineHeight,
	)

	err = s.textElem.CacheFromSvg(svgXml)
	return
}

// cleanup
//
// English:
//
//	Removes all splash elements from the Stage and resets internal state.
//	Elements are destroyed to release their cached images and prevent
//	memory leaks.
//
// Português:
//
//	Remove todos os elementos do splash do Stage e reseta o estado interno.
//	Elementos são destruídos para liberar suas imagens cacheadas e prevenir
//	vazamentos de memória.
func (s *SplashScreen) cleanup() {
	if s.stage == nil {
		return
	}

	prefix := s.config.ElementPrefix

	// Remove from stage first (so they stop rendering), then destroy.
	// Português: Remove do stage primeiro (para parar de renderizar), depois destrói.
	if s.overlayElem != nil {
		s.stage.RemoveElement(prefix + "_overlay")
		s.overlayElem.Destroy()
		s.overlayElem = nil
	}

	if s.imageElem != nil {
		s.stage.RemoveElement(prefix + "_image")
		s.imageElem.Destroy()
		s.imageElem = nil
	}

	if s.textElem != nil {
		s.stage.RemoveElement(prefix + "_text")
		s.textElem.Destroy()
		s.textElem = nil
	}

	s.textLines = nil
	s.showing = false

	s.stage.MarkDirty()
}
