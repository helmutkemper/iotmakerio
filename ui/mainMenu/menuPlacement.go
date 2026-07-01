package mainMenu

// menuPlacement.go — Hex menu positioning strategy.
//
// Positioning logic for the SpriteHexMenu, which after Delivery B
// serves only two client paths:
//
//   1. The main menu sidebar (opened from a screen-space UI element;
//      uses OpenFromScreen).
//   2. The first-time output-click tutorial on compMath and compLogic
//      devices (uses StartTutorialFromDevice). The tutorial cannot
//      move to the linear context menu until the flash/pulse
//      highlight primitive is re-implemented there — planned for
//      Delivery C.
//
// The linear context menu (ui/contextMenu) does NOT use this file —
// it does its own anchoring in contextMenu/anchor.go.
//
// To change placement for the entire application, set Placement:
//
//	mainMenu.Placement = mainMenu.PlacementAtCursor
//
// Português: Lógica de posicionamento do SpriteHexMenu. Após a
// Delivery B, só dois caminhos chamam este arquivo: o sidebar do
// main menu (OpenFromScreen) e o tutorial de primeira visita no
// output de compMath/compLogic (StartTutorialFromDevice). O menu
// de contexto linear (ui/contextMenu) tem seu próprio posicionamento.

import (
	"github.com/helmutkemper/iotmakerio/hexMenu"
)

// PlacementMode controls where hex menus open on the canvas.
//
// Português: Controla onde os hex menus abrem no canvas.
type PlacementMode int

const (
	// PlacementCentered opens the menu at the center of the screen.
	// Best for touch/tablet where finger occlusion is a problem.
	//
	// Português: Abre o menu no centro da tela.
	// Melhor para touch/tablet onde o dedo atrapalha.
	PlacementCentered PlacementMode = iota

	// PlacementAtCursor opens the menu near the click position.
	// Best for mouse-based interaction with precise pointing.
	//
	// Português: Abre o menu próximo à posição do clique.
	// Melhor para interação com mouse.
	PlacementAtCursor
)

// Placement is the global setting that controls menu positioning for all
// devices. Change this single value to switch behavior application-wide.
//
// Português: Configuração global que controla posicionamento do menu para
// todos os devices. Altere este único valor para mudar o comportamento.
var Placement = PlacementCentered

// BackdropOpacity controls the dimming of the canvas when a menu is open.
// 0.0 = fully transparent (no dimming, just click-catching)
// 0.5 = 50% dark overlay
// 0.8 = 80% dark overlay (strong focus on menu)
//
// Default: 0.0 (legacy behavior — invisible backdrop).
//
// Português: Controla o escurecimento do canvas quando um menu está aberto.
var BackdropOpacity = 0.0

// DevicePlacementMode controls how new devices are placed on the canvas.
//
// Português: Controla como novos devices são colocados no canvas.
type DevicePlacementMode int

const (
	// DevicePlaceImmediate creates the device immediately at the last
	// click position or screen center. Legacy behavior.
	//
	// Português: Cria o device imediatamente na última posição de clique
	// ou no centro da tela. Comportamento legado.
	DevicePlaceImmediate DevicePlacementMode = iota

	// DevicePlaceOnClick enters "placement mode" after choosing a device
	// from the menu. The next click on the canvas places the device at
	// that position.
	//
	// Português: Entra no "modo de posicionamento" após escolher um device
	// no menu. Um cursor fantasma segue o ponteiro até o usuário clicar
	// para confirmar a posição.
	DevicePlaceOnClick
)

// DevicePlacement is the global setting that controls how devices are
// placed on the canvas. Change this single value to switch behavior.
//
// Português: Configuração global que controla como devices são colocados
// no canvas. Altere este único valor para mudar o comportamento.
var DevicePlacement = DevicePlaceImmediate

// OpenFromScreen opens the hex menu for a screen-space UI element (e.g. main
// menu button). Coordinates are already in screen-space — no camera conversion.
//
// screenX, screenY: position in canvas-pixel coordinates.
// These are only used in PlacementAtCursor mode.
//
// Português: Abre o hex menu para um elemento UI screen-space (ex: botão do
// menu principal). Coordenadas já estão em screen-space — sem conversão de câmera.
func (m *SpriteHexMenu) OpenFromScreen(items []hexMenu.MenuItem, screenX, screenY float64) {
	switch Placement {
	case PlacementAtCursor:
		sx, sy := m.clampToCanvas(items, screenX, screenY)
		m.Open(items, sx, sy)
	default: // PlacementCentered
		canvasW, canvasH := m.stage.GetCanvasSize()
		m.OpenCentered(items, float64(canvasW)/2, float64(canvasH)/2)
	}
}

// StartTutorialFromDevice opens the hex menu tutorial for a device click.
// Same principle: device provides items + steps + world coordinates,
// menu handles all positioning.
//
// Português: Abre o tutorial do hex menu para um clique de device.
// Mesmo princípio: device fornece itens + passos + coordenadas mundo,
// menu trata todo o posicionamento.
func (m *SpriteHexMenu) StartTutorialFromDevice(items []hexMenu.MenuItem, steps []hexMenu.TutorialStep, worldX, worldY float64) {
	switch Placement {
	case PlacementAtCursor:
		sx, sy := m.worldToScreen(worldX, worldY)
		sx, sy = m.clampToCanvas(items, sx, sy)
		m.StartTutorial(items, steps, sx, sy)
	default: // PlacementCentered
		canvasW, canvasH := m.stage.GetCanvasSize()
		cx := float64(canvasW) / 2
		cy := float64(canvasH) / 2
		radius := m.config.HexRadius.GetFloat()
		spacing := m.config.Spacing.GetFloat()
		w, h := hexMenu.GridBounds(items, radius, spacing)
		m.StartTutorial(items, steps, cx-w/2, cy-h/2)
	}
}

// worldToScreen converts world coordinates to screen-space (canvas pixel)
// using the camera transform: screenX = worldX * zoom + offsetX.
//
// Português: Converte coordenadas mundo para screen-space (pixel do canvas).
func (m *SpriteHexMenu) worldToScreen(worldX, worldY float64) (float64, float64) {
	cam := m.stage.GetCamera()
	zoom := 1.0
	offsetX := 0.0
	offsetY := 0.0
	if cam != nil {
		zoom = cam.Zoom
		if zoom <= 0 {
			zoom = 1.0
		}
		offsetX = cam.OffsetX
		offsetY = cam.OffsetY
	}
	return worldX*zoom + offsetX, worldY*zoom + offsetY
}

// clampToCanvas ensures the menu fits within the visible canvas.
// Uses the actual menu dimensions from the item grid.
//
// Português: Garante que o menu cabe dentro do canvas visível.
// Usa as dimensões reais do menu a partir do grid de itens.
func (m *SpriteHexMenu) clampToCanvas(items []hexMenu.MenuItem, x, y float64) (float64, float64) {
	radius := m.config.HexRadius.GetFloat()
	spacing := m.config.Spacing.GetFloat()
	menuW, menuH := hexMenu.GridBounds(items, radius, spacing)
	menuW += 20 // padding
	menuH += 20

	canvasW, canvasH := m.stage.GetCanvasSize()
	cw := float64(canvasW)
	ch := float64(canvasH)

	if x+menuW > cw {
		x = cw - menuW
	}
	if y+menuH > ch {
		y = ch - menuH
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}
