package sprite

import (
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesSprite"
)

// =====================================================================
//  Camera Key Bindings | Atalhos de Teclado da Câmera
// =====================================================================

// CameraAction
//
// English:
//
//	Identifies a camera keyboard action.
//
// Português:
//
//	Identifica uma ação de teclado da câmera.
type CameraAction int

const (
	// CameraActionGoToOrigin animates the camera back to (0,0) at zoom 1.0.
	// Português: Anima a câmera de volta a (0,0) no zoom 1.0.
	CameraActionGoToOrigin CameraAction = iota

	// CameraActionFitAll fits all visible elements on screen.
	// Português: Enquadra todos os elementos visíveis na tela.
	CameraActionFitAll

	// CameraActionZoomIn increments zoom by ZoomStep, centered on canvas center.
	// Português: Incrementa zoom por ZoomStep, centrado no centro do canvas.
	CameraActionZoomIn

	// CameraActionZoomOut decrements zoom by ZoomStep, centered on canvas center.
	// Português: Decrementa zoom por ZoomStep, centrado no centro do canvas.
	CameraActionZoomOut

	// CameraActionPanLeft pans the camera left by a fixed amount.
	// Português: Move a câmera para a esquerda por uma quantidade fixa.
	CameraActionPanLeft

	// CameraActionPanRight pans the camera right.
	// Português: Move a câmera para a direita.
	CameraActionPanRight

	// CameraActionPanUp pans the camera up.
	// Português: Move a câmera para cima.
	CameraActionPanUp

	// CameraActionPanDown pans the camera down.
	// Português: Move a câmera para baixo.
	CameraActionPanDown

	// CameraActionToggleHelp toggles the help overlay on/off.
	// Português: Alterna o overlay de ajuda ligado/desligado.
	CameraActionToggleHelp

	// cameraActionCount is not an action — used as array size.
	cameraActionCount
)

// KeyBinding
//
// English:
//
//	Defines a keyboard shortcut: which key (matching KeyboardEvent.key) and
//	optional modifier keys must be pressed to trigger the action.
//
//	Key values use the standard Web KeyboardEvent.key names:
//	  "Home", "End", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown",
//	  "f", "F", "h", "+", "-", "0", "?", etc.
//
//	Note on Apple keyboards: compact Apple keyboards (MacBook, Magic Keyboard)
//	do NOT have a Home key. The browser may or may not generate "Home" for
//	Fn+LeftArrow. For best cross-platform support, use letter keys (like "h")
//	as alternatives.
//
// Português:
//
//	Define um atalho de teclado: qual tecla (correspondente a KeyboardEvent.key) e
//	teclas modificadoras opcionais devem ser pressionadas para disparar a ação.
//
//	Valores de tecla usam os nomes padrão de KeyboardEvent.key do Web:
//	  "Home", "End", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown",
//	  "f", "F", "h", "+", "-", "0", "?", etc.
//
//	Nota sobre teclados Apple: teclados Apple compactos (MacBook, Magic Keyboard)
//	NÃO possuem tecla Home. O navegador pode ou não gerar "Home" para
//	Fn+SetaEsquerda. Para melhor compatibilidade, use teclas de letra (como "h")
//	como alternativas.
type KeyBinding struct {
	Key   string // KeyboardEvent.key value (e.g., "Home", "f", "+", "ArrowLeft")
	Ctrl  bool   // requires Ctrl (or Cmd on Mac)
	Shift bool   // requires Shift
	Alt   bool   // requires Alt (or Option on Mac)
	Meta  bool   // requires Meta/Cmd specifically (Ctrl on non-Mac)
}

// keyBindingSet holds all bindings for a single action.
//
// Português: Contém todos os bindings para uma única ação.
type keyBindingSet struct {
	enabled  bool
	bindings []KeyBinding
}

// keysConfig holds the complete keyboard configuration.
//
// Português: Contém a configuração completa de teclado.
type keysConfig struct {
	enabled              bool // master enable/disable for all keyboard shortcuts
	actions              [cameraActionCount]keyBindingSet
	panStep              float64 // pixels to pan per key press (in screen space)
	skipWhenInputFocused bool    // skip key handling when INPUT/TEXTAREA/SELECT is focused
}

// =====================================================================
//  Set Functions — Master Control | Funções Set — Controle Mestre
// =====================================================================

// SetKeysEnabled
//
// English:
//
//	Enables or disables all camera keyboard shortcuts at once.
//	When disabled, no key events are processed by the camera.
//	Default: true.
//
// Português:
//
//	Habilita ou desabilita todos os atalhos de teclado da câmera de uma vez.
//	Quando desabilitado, nenhum evento de teclado é processado pela câmera.
//	Padrão: true.
func (c *Camera) SetKeysEnabled(enabled bool) {
	c.ensureKeys()
	c.keys.enabled = enabled
}

// IsKeysEnabled
//
// English:
//
//	Returns whether keyboard shortcuts are globally enabled.
//
// Português:
//
//	Retorna se os atalhos de teclado estão globalmente habilitados.
func (c *Camera) IsKeysEnabled() bool {
	return c.keys != nil && c.keys.enabled
}

// =====================================================================
//  Set Functions — Per-Action Control | Funções Set — Controle por Ação
// =====================================================================

// SetActionEnabled
//
// English:
//
//	Enables or disables a specific camera action. When disabled, the key binding
//	is preserved but the action won't fire.
//
// Português:
//
//	Habilita ou desabilita uma ação específica da câmera. Quando desabilitada, o
//	binding de tecla é preservado mas a ação não dispara.
func (c *Camera) SetActionEnabled(action CameraAction, enabled bool) {
	c.ensureKeys()
	if action >= 0 && action < cameraActionCount {
		c.keys.actions[action].enabled = enabled
	}
}

// IsActionEnabled
//
// English:
//
//	Returns whether a specific camera action is enabled.
//
// Português:
//
//	Retorna se uma ação específica da câmera está habilitada.
func (c *Camera) IsActionEnabled(action CameraAction) bool {
	if c.keys == nil || action < 0 || action >= cameraActionCount {
		return false
	}
	return c.keys.actions[action].enabled
}

// SetActionKeys
//
// English:
//
//	Sets the key bindings for a specific action. Pass multiple bindings to allow
//	alternative keys (e.g., Home OR "h" for GoToOrigin). Pass nil or empty to
//	remove all bindings for the action (effectively disabling it by key).
//
//	Example:
//	  // GoToOrigin via Home key OR "h" key:
//	  cam.SetActionKeys(sprite.CameraActionGoToOrigin, []sprite.KeyBinding{
//	      {Key: "Home"},
//	      {Key: "h"},
//	  })
//
//	  // FitAll only via Ctrl+F:
//	  cam.SetActionKeys(sprite.CameraActionFitAll, []sprite.KeyBinding{
//	      {Key: "f", Ctrl: true},
//	  })
//
// Português:
//
//	Define os bindings de tecla para uma ação específica. Passe múltiplos bindings
//	para permitir teclas alternativas (ex: Home OU "h" para GoToOrigin). Passe nil
//	ou vazio para remover todos os bindings da ação.
func (c *Camera) SetActionKeys(action CameraAction, bindings []KeyBinding) {
	c.ensureKeys()
	if action >= 0 && action < cameraActionCount {
		c.keys.actions[action].bindings = bindings
	}
}

// GetActionKeys
//
// English:
//
//	Returns the current key bindings for a specific action.
//
// Português:
//
//	Retorna os bindings de tecla atuais para uma ação específica.
func (c *Camera) GetActionKeys(action CameraAction) []KeyBinding {
	if c.keys == nil || action < 0 || action >= cameraActionCount {
		return nil
	}
	return c.keys.actions[action].bindings
}

// =====================================================================
//  Set Functions — Pan Step | Funções Set — Passo de Pan
// =====================================================================

// SetKeyPanStep
//
// English:
//
//	Sets how many screen pixels the camera pans per arrow key press.
//	Default: 50. The actual world distance depends on zoom level.
//
// Português:
//
//	Define quantos pixels de tela a câmera desloca por pressionamento de tecla de seta.
//	Padrão: 50. A distância real no mundo depende do nível de zoom.
func (c *Camera) SetKeyPanStep(pixels float64) {
	c.ensureKeys()
	c.keys.panStep = pixels
}

// SetSkipWhenInputFocused
//
// English:
//
//	When true (default), camera key bindings are ignored when an INPUT, TEXTAREA,
//	or SELECT element has focus. This prevents camera actions from interfering
//	with text editing.
//
// Português:
//
//	Quando true (padrão), bindings de tecla da câmera são ignorados quando um elemento
//	INPUT, TEXTAREA ou SELECT tem foco. Isso previne ações da câmera de interferir
//	com edição de texto.
func (c *Camera) SetSkipWhenInputFocused(skip bool) {
	c.ensureKeys()
	c.keys.skipWhenInputFocused = skip
}

// =====================================================================
//  Defaults | Valores Padrão
// =====================================================================

// ensureKeys creates the keys config with defaults if it doesn't exist.
//
// English:
//
//	Default key bindings (cross-platform friendly):
//	  GoToOrigin:  Home, h
//	  FitAll:      f
//	  ZoomIn:      + (or =)
//	  ZoomOut:     -
//	  PanLeft:     ArrowLeft
//	  PanRight:    ArrowRight
//	  PanUp:       ArrowUp
//	  PanDown:     ArrowDown
//	  ToggleHelp:  ?
//
//	"Home" works on full keyboards. "h" is the Apple-friendly alternative.
//
// Português:
//
//	Cria a config de teclas com valores padrão se não existir.
func (c *Camera) ensureKeys() {
	if c.keys != nil {
		return
	}
	c.keys = &keysConfig{
		enabled:              true,
		panStep:              rulesSprite.KeysPanStep,
		skipWhenInputFocused: rulesSprite.KeysSkipWhenInputFocused,
	}

	c.keys.actions[CameraActionGoToOrigin] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysGoToOrigin),
	}
	c.keys.actions[CameraActionFitAll] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysFitAll),
	}
	c.keys.actions[CameraActionZoomIn] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysZoomIn),
	}
	c.keys.actions[CameraActionZoomOut] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysZoomOut),
	}
	c.keys.actions[CameraActionPanLeft] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysPanLeft),
	}
	c.keys.actions[CameraActionPanRight] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysPanRight),
	}
	c.keys.actions[CameraActionPanUp] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysPanUp),
	}
	c.keys.actions[CameraActionPanDown] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysPanDown),
	}
	c.keys.actions[CameraActionToggleHelp] = keyBindingSet{
		enabled:  true,
		bindings: convertBindings(rulesSprite.KeysToggleHelp),
	}
}

// convertBindings converts rulesSprite.KeyBinding to sprite.KeyBinding.
// The types are structurally identical but live in different packages.
func convertBindings(src []rulesSprite.KeyBinding) []KeyBinding {
	out := make([]KeyBinding, len(src))
	for i, b := range src {
		out[i] = KeyBinding{
			Key: b.Key, Ctrl: b.Ctrl, Shift: b.Shift,
			Alt: b.Alt, Meta: b.Meta,
		}
	}
	return out
}

// =====================================================================
//  Key Matching | Verificação de Tecla
// =====================================================================

// matchKeyEvent
//
// English:
//
//	Tests if a DOM KeyboardEvent matches any binding for the given action.
//	Returns true if the action should fire.
//
// Português:
//
//	Testa se um evento DOM KeyboardEvent corresponde a algum binding da ação fornecida.
//	Retorna true se a ação deve disparar.
func (c *Camera) matchKeyEvent(action CameraAction, domEvent js.Value) bool {
	if c.keys == nil || !c.keys.enabled {
		return false
	}
	if action < 0 || action >= cameraActionCount {
		return false
	}
	as := &c.keys.actions[action]
	if !as.enabled || len(as.bindings) == 0 {
		return false
	}

	key := domEvent.Get("key").String()
	ctrlOrMeta := domEvent.Get("ctrlKey").Bool() || domEvent.Get("metaKey").Bool()
	shift := domEvent.Get("shiftKey").Bool()
	alt := domEvent.Get("altKey").Bool()
	meta := domEvent.Get("metaKey").Bool()

	for _, b := range as.bindings {
		if b.Key != key {
			continue
		}
		// Check modifiers. Ctrl matches either ctrlKey or metaKey (Cmd on Mac).
		// Português: Verifica modificadores. Ctrl corresponde a ctrlKey ou metaKey (Cmd no Mac).
		if b.Ctrl && !ctrlOrMeta {
			continue
		}
		if !b.Ctrl && ctrlOrMeta {
			continue
		}
		// Shift check: only enforce when the binding explicitly requires it.
		// When Shift is false in the binding but true in the event, allow it —
		// the Shift is already consumed by the key value (e.g., "?" = Shift+"/",
		// "+" = Shift+"="). This makes bindings work across keyboard layouts.
		//
		// Português: Verificação de Shift: só exige quando o binding explicitamente requer.
		// Quando Shift é false no binding mas true no evento, permite — o Shift já foi
		// consumido pelo valor da tecla (ex: "?" = Shift+"/", "+" = Shift+"=").
		if b.Shift && !shift {
			continue
		}
		if b.Alt != alt {
			continue
		}
		if b.Meta && !meta {
			continue
		}
		return true
	}
	return false
}

// shouldSkipKeyEvent returns true if the event should be ignored because an
// input element has focus.
//
// Português: Retorna true se o evento deve ser ignorado porque um elemento de
// input tem foco.
func (c *Camera) shouldSkipKeyEvent() bool {
	if c.keys == nil || !c.keys.skipWhenInputFocused {
		return false
	}
	activeTag := js.Global().Get("document").Get("activeElement").Get("tagName").String()
	return activeTag == "INPUT" || activeTag == "TEXTAREA" || activeTag == "SELECT"
}
