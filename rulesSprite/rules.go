// rulesSprite/rules.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package rulesSprite

import "github.com/helmutkemper/iotmakerio/translate"

// rulesSprite — Centralized configuration for the sprite package.
//
// English:
//
//	All hardcoded constants from the sprite package are defined here as exported
//	variables. This allows customization before calling NewCamera(), NewStage(),
//	or CreateElement() — any changes to these variables take effect the next time
//	the corresponding ensure/new function runs.
//
//	Organized by subsystem:
//	  - Camera (zoom, grid, origin, info)
//	  - Keys (keyboard bindings, pan step)
//	  - Minimap (overlay appearance and behavior)
//	  - Help (overlay appearance and content)
//	  - Stage (double-click, drag threshold, background)
//	  - Element (resize handles, min size, opacity)
//	  - Origin (crosshair/arrow rendering)
//
// Português:
//
//	Todas as constantes hardcoded do package sprite são definidas aqui como
//	variáveis exportadas. Isso permite customização antes de chamar NewCamera(),
//	NewStage(), ou CreateElement().
//
//	Organizado por subsistema:
//	  - Camera (zoom, grid, origin, info)
//	  - Keys (atalhos de teclado, passo de pan)
//	  - Minimap (aparência e comportamento do overlay)
//	  - Help (aparência e conteúdo do overlay)
//	  - Stage (double-click, limiar de drag, fundo)
//	  - Element (alças de resize, tamanho mínimo, opacidade)
//	  - Origin (renderização do crosshair/seta)

// =====================================================================
//  Camera — Zoom | Câmera — Zoom
// =====================================================================

// CameraDefaultZoom is the initial zoom level (1.0 = 100%).
// Português: Nível de zoom inicial (1.0 = 100%).
var CameraDefaultZoom = 1.0

// CameraMinZoom is the minimum allowed zoom (0.1 = 10%).
// Português: Zoom mínimo permitido (0.1 = 10%).
var CameraMinZoom = 0.1

// CameraMaxZoom is the maximum allowed zoom (5.0 = 500%).
// Português: Zoom máximo permitido (5.0 = 500%).
var CameraMaxZoom = 5.0

// CameraZoomStep is the zoom change per scroll notch.
// 0.03 = 3% per notch — tuned for MacBook trackpad two-finger
// scroll, which fires many wheel events per gesture. A higher
// value (e.g. the original 0.1 = 10%) felt explosive on trackpad.
//
// If users with a traditional wheel mouse complain that zoom is
// too slow, the right fix is NOT to raise this value — instead,
// expose it in the future Canvas tab of Editor Settings so each
// user can calibrate for their own input device.
//
// Português: Mudança de zoom por notch do scroll. 0.03 = 3% por
// notch — calibrado para trackpad MacBook. Se mouse de roda
// tradicional reclamar que está lento, expor em Editor Settings
// ao invés de subir globalmente.
var CameraZoomStep = 0.03

// =====================================================================
//  Camera — Grid | Câmera — Grid
// =====================================================================

// CameraGridEnabled controls whether the background grid is drawn.
// Português: Controla se o grid de fundo é desenhado.
var CameraGridEnabled = true

// CameraGridSize is the spacing between minor grid lines in world units.
// Português: Espaçamento entre linhas menores do grid em unidades mundo.
var CameraGridSize = 20.0

// CameraGridColor is the CSS color for minor grid lines.
// Português: Cor CSS para linhas menores do grid.
var CameraGridColor = "rgba(200, 200, 200, 0.3)"

// CameraGridMajorEvery draws a major line every N minor lines.
// Português: Desenha uma linha maior a cada N linhas menores.
var CameraGridMajorEvery = 5

// CameraGridMajorColor is the CSS color for major grid lines.
// Português: Cor CSS para linhas maiores do grid.
var CameraGridMajorColor = "rgba(180, 180, 180, 0.5)"

// =====================================================================
//  Camera — Origin Crosshair | Câmera — Crosshair da Origem
// =====================================================================

// CameraOriginEnabled controls whether the origin crosshair is drawn.
// Português: Controla se o crosshair da origem é desenhado.
var CameraOriginEnabled = true

// CameraOriginColor is the CSS color for the origin crosshair and arrow.
// Português: Cor CSS para o crosshair e seta da origem.
var CameraOriginColor = "rgba(220, 50, 50, 0.4)"

// OriginCrossLen is the half-length of the crosshair arms in density-independent pixels.
// Português: Metade do comprimento dos braços do crosshair em pixels independentes de densidade.
var OriginCrossLen = 20.0

// OriginDotRadius is the radius of the center dot in density-independent pixels.
// Português: Raio do ponto central em pixels independentes de densidade.
var OriginDotRadius = 3.0

// OriginLineWidth is the stroke width of the crosshair in density-independent pixels.
// Português: Largura do traço do crosshair em pixels independentes de densidade.
var OriginLineWidth = 1.5

// OriginArrowSize is the length of the off-screen arrow in density-independent pixels.
// Português: Comprimento da seta fora da tela em pixels independentes de densidade.
var OriginArrowSize = 10.0

// OriginArrowWidth is the width of the off-screen arrow in density-independent pixels.
// Português: Largura da seta fora da tela em pixels independentes de densidade.
var OriginArrowWidth = 8.0

// OriginMargin is the margin for the off-screen arrow in density-independent pixels.
// Português: Margem para a seta fora da tela em pixels independentes de densidade.
var OriginMargin = 20.0

// =====================================================================
//  Camera — Info Overlay | Câmera — Overlay de Informação
// =====================================================================

// CameraInfoEnabled controls whether camera position/zoom is displayed.
// Português: Controla se posição/zoom da câmera é exibido.
var CameraInfoEnabled = true

// CameraInfoColor is the CSS color for the info text.
// Português: Cor CSS para o texto de informação.
var CameraInfoColor = "rgba(100, 100, 100, 0.7)"

// CameraInfoFontSize is the font size for the info text in density-independent pixels.
// Português: Tamanho da fonte para o texto de informação em pixels independentes de densidade.
var CameraInfoFontSize = 11

// CameraInfoMargin is the margin from the canvas edge in density-independent pixels.
// Português: Margem da borda do canvas em pixels independentes de densidade.
var CameraInfoMargin = 8.0

// =====================================================================
//  Camera — Pan | Câmera — Pan
// =====================================================================

// CameraPanEnabled controls whether pan via middle-mouse or two-finger drag is active.
// Português: Controla se o pan via botão do meio ou dois dedos está ativo.
var CameraPanEnabled = true

// CameraPanEmptyArea toggles the desktop-mouse shortcut of
// left-click-dragging on an empty spot of the stage to pan the
// camera. Separate from CameraPanEnabled so the user can disable
// the empty-area shortcut (e.g. to reserve left-drag for future
// rectangular selection) while keeping middle-mouse pan working.
// Touch devices ignore this flag entirely.
//
// Loaded from per-user preferences by stageWorkspace at startup
// (see stagePrefsClient.LoadStagePrefs).
//
// Português: Habilita o atalho desktop de left-click+drag em área
// vazia da stage para pan da câmera. Separado de CameraPanEnabled
// para o usuário poder desligar o atalho mantendo o pan do botão
// do meio. Touch ignora esta flag.
var CameraPanEmptyArea = true

// CameraShowGrabCursor enables the `cursor: grab` hint when the
// mouse hovers empty stage area (i.e. no element underneath). Only
// meaningful while CameraPanEmptyArea is also true — otherwise the
// hint would promise an action that doesn't happen.
//
// Off by default because it requires a pointermove listener that
// hit-tests on every event. The cost is modest but not zero.
//
// Português: Habilita o cursor grab no hover de área vazia. Só faz
// sentido com CameraPanEmptyArea=true. Desligado por padrão pois
// requer um listener de pointermove contínuo.
var CameraShowGrabCursor = false

// CameraZoomEnabled controls whether zoom via wheel or pinch is active.
// Português: Controla se o zoom via scroll ou pinch está ativo.
var CameraZoomEnabled = true

// =====================================================================
//  Keys — Keyboard Bindings | Teclas — Atalhos de Teclado
// =====================================================================

// KeysPanStep is the number of screen pixels the camera pans per key press.
// Português: Número de pixels de tela que a câmera desloca por tecla pressionada.
var KeysPanStep = 50.0

// KeysSkipWhenInputFocused controls whether camera keys are ignored
// when INPUT/TEXTAREA/SELECT is focused.
// Português: Controla se teclas da câmera são ignoradas quando INPUT/TEXTAREA/SELECT tem foco.
var KeysSkipWhenInputFocused = true

// KeyBinding defines a keyboard shortcut.
// Exported here so rulesSprite users can build custom bindings.
//
// Português: Define um atalho de teclado.
type KeyBinding struct {
	Key   string
	Ctrl  bool
	Shift bool
	Alt   bool
	Meta  bool
}

// KeysGoToOrigin defines the key bindings for "go to origin".
// Default: Ctrl+H (cross-platform friendly).
// Português: Atalhos para "ir à origem". Padrão: Ctrl+H.
var KeysGoToOrigin = []KeyBinding{
	{Key: "h", Ctrl: true},
}

// KeysFitAll defines the key bindings for "fit all elements".
// Default: f.
// Português: Atalhos para "enquadrar todos". Padrão: f.
var KeysFitAll = []KeyBinding{
	{Key: "f"},
}

// KeysZoomIn defines the key bindings for "zoom in".
// Default: + and = (unshifted on US keyboards).
// Português: Atalhos para "zoom in". Padrão: + e =.
var KeysZoomIn = []KeyBinding{
	{Key: "+"},
	{Key: "="},
}

// KeysZoomOut defines the key bindings for "zoom out".
// Default: -.
// Português: Atalhos para "zoom out". Padrão: -.
var KeysZoomOut = []KeyBinding{
	{Key: "-"},
}

// KeysPanLeft defines the key bindings for "pan left".
// Português: Atalhos para "pan esquerda".
var KeysPanLeft = []KeyBinding{
	{Key: "ArrowLeft"},
}

// KeysPanRight defines the key bindings for "pan right".
// Português: Atalhos para "pan direita".
var KeysPanRight = []KeyBinding{
	{Key: "ArrowRight"},
}

// KeysPanUp defines the key bindings for "pan up".
// Português: Atalhos para "pan cima".
var KeysPanUp = []KeyBinding{
	{Key: "ArrowUp"},
}

// KeysPanDown defines the key bindings for "pan down".
// Português: Atalhos para "pan baixo".
var KeysPanDown = []KeyBinding{
	{Key: "ArrowDown"},
}

// KeysToggleHelp defines the key bindings for "toggle help overlay".
// Português: Atalhos para "alternar overlay de ajuda".
var KeysToggleHelp = []KeyBinding{
	{Key: "?"},
}

// =====================================================================
//  Minimap | Minimapa
// =====================================================================

// MinimapWidth is the minimap width in density-independent pixels.
// Português: Largura do minimapa em pixels independentes de densidade.
var MinimapWidth = 180.0

// MinimapHeight is the minimap height in density-independent pixels.
// Português: Altura do minimapa em pixels independentes de densidade.
var MinimapHeight = 120.0

// MinimapMarginX is the horizontal margin from the canvas edge.
// Português: Margem horizontal da borda do canvas.
var MinimapMarginX = 10.0

// MinimapMarginY is the vertical margin from the canvas edge.
// Português: Margem vertical da borda do canvas.
var MinimapMarginY = 10.0

// MinimapBackgroundColor is the CSS color for the minimap background.
// Português: Cor CSS para o fundo do minimapa.
var MinimapBackgroundColor = "rgba(40, 40, 40, 0.85)"

// MinimapBorderColor is the CSS color for the minimap border.
// Português: Cor CSS para a borda do minimapa.
var MinimapBorderColor = "rgba(100, 100, 100, 0.8)"

// MinimapElementColor is the CSS color for element rectangles in the minimap.
// Português: Cor CSS para retângulos de elementos no minimapa.
var MinimapElementColor = "rgba(100, 160, 220, 0.7)"

// MinimapViewportColor is the CSS stroke color for the viewport rectangle.
// Português: Cor CSS do traço do retângulo da viewport.
var MinimapViewportColor = "rgba(255, 255, 255, 0.9)"

// MinimapViewportFill is the CSS fill color for the viewport rectangle.
// Português: Cor CSS de preenchimento do retângulo da viewport.
var MinimapViewportFill = "rgba(255, 255, 255, 0.1)"

// MinimapClickToNavigate controls whether clicking the minimap pans the camera.
// Português: Controla se clicar no minimapa move a câmera.
var MinimapClickToNavigate = true

// MinimapMinElemSize is the minimum rendered size of an element in the minimap.
// Português: Tamanho mínimo renderizado de um elemento no minimapa.
var MinimapMinElemSize = 2.0

// =====================================================================
//  Help Overlay | Overlay de Ajuda
// =====================================================================

// HelpMarginX is the horizontal margin from the canvas edge.
// Português: Margem horizontal da borda do canvas.
var HelpMarginX = 10.0

// HelpMarginY is the vertical margin from the canvas edge.
// Português: Margem vertical da borda do canvas.
var HelpMarginY = 40.0

// HelpBackgroundColor is the CSS color for the help overlay background.
// Português: Cor CSS para o fundo do overlay de ajuda.
var HelpBackgroundColor = "rgba(40, 40, 40, 0.88)"

// HelpBorderColor is the CSS color for the help overlay border.
// Português: Cor CSS para a borda do overlay de ajuda.
var HelpBorderColor = "rgba(100, 100, 100, 0.8)"

// HelpTextColor is the CSS color for the help text.
// Português: Cor CSS para o texto de ajuda.
var HelpTextColor = "rgba(220, 220, 220, 0.95)"

// HelpTitleColor is the CSS color for the help overlay title.
// Português: Cor CSS para o título do overlay de ajuda.
var HelpTitleColor = "rgba(255, 255, 255, 1.0)"

// HelpFontSize is the font size in density-independent pixels.
// Português: Tamanho da fonte em pixels independentes de densidade.
var HelpFontSize = 11

// HelpLineHeight is the line height multiplier for help text.
// Português: Multiplicador de altura de linha para o texto de ajuda.
var HelpLineHeight = 1.6

// HelpPadding is the internal padding in density-independent pixels.
// Português: Padding interno em pixels independentes de densidade.
var HelpPadding = 12.0

// HelpBorderRadius is the corner radius of the help overlay box.
// Português: Raio dos cantos do box do overlay de ajuda.
var HelpBorderRadius = 6.0

// HelpTitle is the default title text for the help overlay.
// Português: Texto padrão do título do overlay de ajuda.
// After (executa quando chamado, Localizer já existe):
var HelpTitle = func() string {
	return translate.T("helpViewCameraControls", "Camera Controls")
}

// HelpLines returns the default help content lines.
// This is a function (not a var) so it can be overridden dynamically
// and integrates with the translate package if needed.
//
// Português: Retorna as linhas padrão de conteúdo da ajuda.
// É uma função (não var) para poder ser sobrescrita dinamicamente
// e integrar com o package translate se necessário.
var HelpLines = func() []string {
	return []string{
		translate.T("helpViewPanText", "Pan:        Middle mouse + drag / Arrow keys"),
		translate.T("helpViewZoomInText", "Zoom in:    Scroll up / Pinch open / + key"),
		translate.T("helpViewZoomOutText", "Zoom out:   Scroll down / Pinch close / - key"),
		translate.T("helpViewGoHomeText", "Go home:    Home key / ctrl+h key"),
		translate.T("helpViewFitAll", "Fit all:    f key"),
		translate.T("helpViewThisHelp", "This help:  ? key"),
	}
}

// =====================================================================
//  Stage | Palco
// =====================================================================

// StageDoubleClickInterval is the maximum time in milliseconds between two
// taps/clicks to be considered a double-click.
// Português: Tempo máximo em ms entre dois cliques para double-click.
var StageDoubleClickInterval int64 = 300

// StageDragThreshold is the minimum distance in pixels the pointer must move
// before a drag operation begins.
// Português: Distância mínima em pixels antes de iniciar drag.
var StageDragThreshold = 4.0

// StageBackgroundColor is the CSS color used to clear the canvas on each frame.
// Português: Cor CSS usada para limpar o canvas a cada frame.
var StageBackgroundColor = "transparent"

// =====================================================================
//  Element | Elemento
// =====================================================================

// ElementResizeHandleSize is the pixel size of the interactive resize handle area.
// Português: Tamanho em pixels da área interativa da alça de resize.
var ElementResizeHandleSize = 8.0

// ElementMinWidth is the minimum allowed width when resizing.
// Português: Largura mínima permitida ao redimensionar.
var ElementMinWidth = 10.0

// ElementMinHeight is the minimum allowed height when resizing.
// Português: Altura mínima permitida ao redimensionar.
var ElementMinHeight = 10.0

// ElementDefaultOpacity is the default opacity for new elements.
// Português: Opacidade padrão para novos elementos.
var ElementDefaultOpacity = 1.0
