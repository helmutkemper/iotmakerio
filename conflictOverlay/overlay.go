// /ide/conflictOverlay/overlay.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package conflictOverlay

// overlay.go — Stage-level conflict visualization.
//
// English:
//
//	The ConflictOverlay Manager subscribes to the scene.Serializer's
//	global "conflicts changed" event and, for every device currently in
//	conflict, renders a red dashed-bordered box pinned to that device's
//	outer bounding box. The border is a DOM element (a <div>) positioned
//	with getBoundingClientRect math, not a sprite — this keeps it
//	completely out of the sprite hit-test / z-index machinery and
//	guarantees it never steals pointer events from the underlying
//	device (CSS `pointer-events: none`).
//
//	Each overlay uses easterEgg.MorseCode to spell out the conflict
//	kind in Morse via visibility toggling: solid red for a few seconds,
//	then blinks the word ("overlap", "straddle", "pierced"), then solid
//	again. The cycle repeats until the conflict is resolved.
//
//	The overlay repositions on every frame from a single ticker so it
//	tracks camera pans and device drags smoothly. When a device's
//	conflict set becomes empty, its overlay is destroyed.
//
// Português:
//
//	Gerenciador de visualização de conflitos no nível da stage. Para
//	cada device em conflito, desenha uma caixa com borda vermelha
//	pontilhada fixada sobre o outer bbox. Usa DOM (<div>) em vez de
//	sprite — fica fora do z-index e nunca rouba eventos do mouse. A
//	cada ciclo pisca em código Morse o tipo do conflito.

import (
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/easterEgg"
	"github.com/helmutkemper/iotmakerio/scene"
	"github.com/helmutkemper/iotmakerio/sprite"
)

// Manager owns all active conflict overlays. One per workspace.
//
// Lifecycle:
//   - Create with New, passing the stage, the canvas DOM element, and
//     the serializer. The Manager subscribes to conflict events.
//   - The Manager auto-starts a 16ms ticker goroutine that repositions
//     every active overlay on each frame. Stop via Destroy.
//   - When a device is removed from the scene (Unregister), its
//     overlay is cleaned up automatically on the next conflict-clear
//     notification. If the device is destroyed without clearing its
//     conflict list first, the ticker detects the missing device and
//     removes the stale overlay.
type Manager struct {
	stage      sprite.Stage
	canvasEl   js.Value
	serializer *scene.Serializer

	mu       sync.Mutex
	overlays map[string]*overlay

	stopCh chan struct{}
}

// New creates the Manager and wires it to the serializer's global
// conflict callback. Starts the reposition ticker immediately.
//
// Português: Cria o Manager e assina o callback global de conflitos do
// serializer. Inicia o ticker de reposicionamento imediatamente.
func New(stage sprite.Stage, canvasEl js.Value, ser *scene.Serializer) *Manager {
	m := &Manager{
		stage:      stage,
		canvasEl:   canvasEl,
		serializer: ser,
		overlays:   make(map[string]*overlay),
		stopCh:     make(chan struct{}),
	}
	ser.SetOnConflictsChanged(m.onConflictsChanged)
	go m.tickLoop()
	return m
}

// Destroy stops the ticker and tears down every active overlay. Call
// from workspace teardown.
//
// Português: Para o ticker e destrói cada overlay ativo.
func (m *Manager) Destroy() {
	close(m.stopCh)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ov := range m.overlays {
		ov.destroy()
		delete(m.overlays, id)
	}
}

// onConflictsChanged runs whenever the scenegraph detects that a
// device's conflict set has changed (added, removed, or kind
// reclassified).
//
// Reset policy: when the *kind* of conflict changes (e.g. overlap →
// straddle), we destroy the overlay and create a fresh one. This
// restarts the solid-red pause and the Morse blinker from zero so
// the user always gets a predictable rhythm. Changes that leave the
// message identical are no-ops.
func (m *Manager) onConflictsChanged(deviceID string, conflicts []scene.Conflict) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(conflicts) == 0 {
		if ov, ok := m.overlays[deviceID]; ok {
			ov.destroy()
			delete(m.overlays, deviceID)
		}
		return
	}

	newMsg := formatMessage(conflicts)

	if ov, ok := m.overlays[deviceID]; ok {
		if ov.message == newMsg {
			// Same conflict kind, overlay already running — leave its
			// blinker cycle alone so the rhythm stays steady.
			return
		}
		// Kind changed: tear down the old overlay so the new one
		// starts its cycle from zero (solid red → pause → blink).
		ov.destroy()
		delete(m.overlays, deviceID)
	}

	ov := newOverlay(deviceID)
	m.overlays[deviceID] = ov
	ov.setMessage(newMsg)
}

// tickLoop runs on a goroutine and repositions every overlay at ~60fps
// so camera pans and device drags track smoothly.
func (m *Manager) tickLoop() {
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.repositionAll()
		}
	}
}

// repositionAll updates every overlay's screen-space rectangle from
// the latest device geometry and camera state. Also cleans up
// overlays whose devices have vanished.
func (m *Manager) repositionAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.overlays) == 0 {
		return
	}

	// Snapshot the camera/canvas transform once per frame.
	cam := m.stage.GetCamera()
	zoom := 1.0
	offsetX, offsetY := 0.0, 0.0
	if cam != nil {
		zoom = cam.Zoom
		if zoom <= 0 {
			zoom = 1
		}
		offsetX, offsetY = cam.OffsetX, cam.OffsetY
	}

	rect := m.canvasEl.Call("getBoundingClientRect")
	canvasW := m.canvasEl.Get("width").Float()
	canvasH := m.canvasEl.Get("height").Float()
	if canvasW == 0 || canvasH == 0 {
		return
	}
	cssScaleX := rect.Get("width").Float() / canvasW
	cssScaleY := rect.Get("height").Float() / canvasH
	canvasLeft := rect.Get("left").Float()
	canvasTop := rect.Get("top").Float()

	for id, ov := range m.overlays {
		dev := m.serializer.FindDevice(id)
		if dev == nil {
			ov.destroy()
			delete(m.overlays, id)
			continue
		}
		outer := dev.GetOuterBBox()

		// World → canvas pixels → screen pixels.
		cpxX := (outer.X - offsetX) * zoom
		cpxY := (outer.Y - offsetY) * zoom
		cpxW := outer.Width * zoom
		cpxH := outer.Height * zoom

		screenX := canvasLeft + cpxX*cssScaleX
		screenY := canvasTop + cpxY*cssScaleY
		screenW := cpxW * cssScaleX
		screenH := cpxH * cssScaleY

		ov.setScreenRect(screenX, screenY, screenW, screenH)
	}
}

// =====================================================================
//  Per-device overlay
// =====================================================================

// overlay is the DOM div pinned over one conflicting device. It owns a
// Morse blinker goroutine that toggles its own visibility.
type overlay struct {
	deviceID string
	div      js.Value

	stopCh   chan struct{}
	message  string
	writeMsg chan string // queues message updates to the blinker goroutine
}

func newOverlay(deviceID string) *overlay {
	doc := js.Global().Get("document")
	div := doc.Call("createElement", "div")
	s := div.Get("style")
	s.Set("position", "fixed")
	s.Set("pointerEvents", "none") // never steal clicks
	s.Set("boxSizing", "border-box")
	s.Set("border", "3px solid rgba(220, 40, 40, 0.95)")
	s.Set("borderRadius", "6px")
	s.Set("boxShadow", "0 0 10px rgba(220, 40, 40, 0.55)")
	s.Set("zIndex", "9998")
	s.Set("visibility", "visible")
	doc.Get("body").Call("appendChild", div)

	ov := &overlay{
		deviceID: deviceID,
		div:      div,
		stopCh:   make(chan struct{}),
		writeMsg: make(chan string, 1),
	}
	go ov.blinker()
	return ov
}

// setMessage queues a new message for the blinker to render. Safe to
// call from any goroutine; the blinker picks up the latest at the end
// of each cycle.
//
// Português: Enfileira nova mensagem pra ser "piscada" no próximo
// ciclo. Seguro de chamar de qualquer goroutine.
func (o *overlay) setMessage(msg string) {
	if msg == o.message {
		return
	}
	o.message = msg
	// Non-blocking send — if the blinker hasn't consumed the previous
	// message yet, drop the old and queue the new.
	select {
	case o.writeMsg <- msg:
	default:
		// Drain and replace.
		select {
		case <-o.writeMsg:
		default:
		}
		select {
		case o.writeMsg <- msg:
		default:
		}
	}
}

// setScreenRect updates the DOM element's position and size in screen
// pixels. Called by the Manager's reposition loop.
//
// Português: Atualiza posição e tamanho do elemento DOM em pixels de
// tela. Chamado pelo loop do Manager.
func (o *overlay) setScreenRect(x, y, w, h float64) {
	s := o.div.Get("style")
	s.Set("left", fmt.Sprintf("%.0fpx", x))
	s.Set("top", fmt.Sprintf("%.0fpx", y))
	s.Set("width", fmt.Sprintf("%.0fpx", w))
	s.Set("height", fmt.Sprintf("%.0fpx", h))
}

// destroy tears down the DOM element and stops the blinker goroutine.
func (o *overlay) destroy() {
	close(o.stopCh)
	doc := js.Global().Get("document")
	body := doc.Get("body")
	if !body.IsUndefined() && !body.IsNull() {
		body.Call("removeChild", o.div)
	}
}

// =====================================================================
//  Morse blinker
// =====================================================================

// blinker runs the visibility cycle: solid for a few seconds, blink
// the current message in Morse, solid again, repeat. Consumes new
// messages from writeMsg at cycle boundaries.
//
// The first cycle uses whatever message is in writeMsg. If no message
// has been set yet, it stays solid red until one arrives.
func (o *overlay) blinker() {
	const (
		tickInterval = 180 * time.Millisecond
		solidPause   = 5 * time.Second
	)

	morse := &easterEgg.MorseCode{}
	morse.Init()

	var pattern []bool
	var haveMsg bool

	// Helper for flipping visibility with stop-check.
	setVis := func(v bool) bool {
		select {
		case <-o.stopCh:
			return false
		default:
		}
		if v {
			o.div.Get("style").Set("visibility", "visible")
		} else {
			o.div.Get("style").Set("visibility", "hidden")
		}
		return true
	}

	// Wait for stop OR a duration OR a new message.
	// Returns true if we should continue, false if we should exit.
	sleepOrMsg := func(d time.Duration) bool {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-o.stopCh:
			return false
		case <-timer.C:
			return true
		case msg := <-o.writeMsg:
			if p, err := morse.TextToMorse(msg); err == nil {
				pattern = p
				haveMsg = true
			}
			return true
		}
	}

	for {
		// Drain any pending message first (non-blocking).
		select {
		case msg := <-o.writeMsg:
			if p, err := morse.TextToMorse(msg); err == nil {
				pattern = p
				haveMsg = true
			}
		default:
		}

		// Solid-red pause. A message arriving during the pause
		// interrupts and is absorbed immediately.
		if !setVis(true) {
			return
		}
		if !sleepOrMsg(solidPause) {
			return
		}

		// If still no message, skip the blink phase and loop.
		if !haveMsg || len(pattern) == 0 {
			continue
		}

		// Blink the current pattern.
		for _, visible := range pattern {
			if !setVis(visible) {
				return
			}
			select {
			case <-o.stopCh:
				return
			case <-time.After(tickInterval):
			}
		}

		// End of blink: force visible again before the next cycle.
		if !setVis(true) {
			return
		}
	}
}

// =====================================================================
//  Message formatting
// =====================================================================

// formatMessage turns a list of scene.Conflict into a short Morse-able
// string describing the situation. The message is intentionally terse
// — Morse blinking is slow, so "overlap" transmits faster than a full
// sentence.
//
// Português: Transforma uma lista de conflitos numa string curta pra
// piscar em Morse. Curto porque Morse é lento.
func formatMessage(conflicts []scene.Conflict) string {
	if len(conflicts) == 0 {
		return ""
	}
	// Dominant conflict kind wins — usually there is only one anyway.
	// Priority: straddle > pierced > overlap (matches classifyPair).
	best := conflicts[0].Kind
	for _, c := range conflicts[1:] {
		if rank(c.Kind) > rank(best) {
			best = c.Kind
		}
	}
	switch best {
	case "straddle":
		return "straddle"
	case "pierced_outer":
		return "pierced"
	case "overlap":
		return "overlap"
	default:
		return "error"
	}
}

func rank(kind string) int {
	switch kind {
	case "straddle":
		return 3
	case "pierced_outer":
		return 2
	case "overlap":
		return 1
	}
	return 0
}
