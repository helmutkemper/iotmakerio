// sprite/errors.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package sprite

import "errors"

var (
	// ErrCanvasNotFound is returned when the HTML canvas element with the given ID
	// is not found in the DOM.
	//
	// Português: Retornado quando o elemento HTML canvas com o ID fornecido não é
	// encontrado no DOM.
	ErrCanvasNotFound = errors.New("sprite: canvas element not found in DOM")

	// ErrElementNotFound is returned when an operation references an element ID
	// that does not exist on the Stage.
	//
	// Português: Retornado quando uma operação referencia um ID de elemento que
	// não existe no Stage.
	ErrElementNotFound = errors.New("sprite: element not found")

	// ErrElementAlreadyExists is returned when attempting to add an element with
	// an ID that is already in use on the Stage.
	//
	// Português: Retornado quando se tenta adicionar um elemento com um ID que já
	// está em uso no Stage.
	ErrElementAlreadyExists = errors.New("sprite: element with this ID already exists")

	// ErrSvgEmpty is returned when an empty SVG XML string is passed to CacheFromSvg.
	//
	// Português: Retornado quando uma string XML SVG vazia é passada para CacheFromSvg.
	ErrSvgEmpty = errors.New("sprite: SVG XML string is empty")

	// ErrSvgRenderFailed is returned when the SVG could not be rendered to the
	// offscreen canvas (invalid SVG, encoding error, etc.).
	//
	// Português: Retornado quando o SVG não pôde ser renderizado no canvas offscreen
	// (SVG inválido, erro de codificação, etc.).
	ErrSvgRenderFailed = errors.New("sprite: failed to render SVG to offscreen canvas")

	// ErrImageLoadFailed is returned when an image source could not be loaded.
	//
	// Português: Retornado quando uma fonte de imagem não pôde ser carregada.
	ErrImageLoadFailed = errors.New("sprite: failed to load image from source")

	// ErrStageAlreadyRunning is returned when Start() is called on a Stage that
	// is already running.
	//
	// Português: Retornado quando Start() é chamado em um Stage que já está em execução.
	ErrStageAlreadyRunning = errors.New("sprite: stage is already running")

	// ErrStageNotRunning is returned when Stop() is called on a Stage that is
	// not running.
	//
	// Português: Retornado quando Stop() é chamado em um Stage que não está em execução.
	ErrStageNotRunning = errors.New("sprite: stage is not running")

	// ErrElementDestroyed is returned when an operation is attempted on an element
	// that has been destroyed.
	//
	// Português: Retornado quando uma operação é tentada em um elemento que foi destruído.
	ErrElementDestroyed = errors.New("sprite: element has been destroyed")

	// ErrStageDestroyed is returned when an operation is attempted on a Stage
	// that has been destroyed.
	//
	// Português: Retornado quando uma operação é tentada em um Stage que foi destruído.
	ErrStageDestroyed = errors.New("sprite: stage has been destroyed")

	// ErrCanvasContextFailed is returned when the 2D rendering context could not
	// be obtained from the canvas element.
	//
	// Português: Retornado quando o contexto de renderização 2D não pôde ser obtido
	// do elemento canvas.
	ErrCanvasContextFailed = errors.New("sprite: failed to get 2D rendering context from canvas")
)
