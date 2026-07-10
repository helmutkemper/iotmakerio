// ui/connectorTip/connectorTip.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

// Package connectorTip renders the small hover tooltip shown when the
// pointer rests over a connector pin: the port's label, its data type and
// its direction ("value · int · in"). One fixed-position DOM <div> shared by
// the whole IDE, created lazily on first use and repositioned per show —
// the same pattern as the Chart hover tooltip, promoted to a package because
// EVERY device benefits from it.
//
// The workspace owns the WHEN (hit-testing, the ~350ms hover delay, the
// hide triggers); this package owns only the WHAT (the element, its style,
// its content). Content is set via textContent, never innerHTML — port
// labels are user-influenced text.
//
// Português: Renderiza o tooltip de hover dos pinos: label da porta, tipo de
// dado e direção ("value · int · in"). Um <div> fixo compartilhado pela IDE
// inteira, criado sob demanda e reposicionado a cada exibição — o mesmo
// padrão do tooltip do Chart, promovido a pacote porque TODO device se
// beneficia. O workspace decide o QUANDO (hit-test, atraso de ~350ms,
// gatilhos de esconder); este pacote decide só O QUÊ (o elemento, o estilo,
// o conteúdo). Conteúdo via textContent, nunca innerHTML — labels de porta
// são texto influenciado pelo usuário.
package connectorTip

import (
	"fmt"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesDevice"
)

// tipEl is the shared tooltip element. Zero value (js.Value{}) means "not
// created yet"; ensure() builds it on first use.
//
// Português: O elemento compartilhado. Valor zero = "ainda não criado";
// ensure() constrói no primeiro uso.
var tipEl js.Value

// visible mirrors the display state so IsVisible needs no DOM read.
//
// Português: Espelha o estado de exibição para IsVisible não ler o DOM.
var visible bool

// ensure lazily creates the tooltip element and appends it to the document
// body. Idempotent.
//
// Português: Cria o elemento sob demanda e anexa ao body. Idempotente.
func ensure() {
	if tipEl.Truthy() {
		return
	}
	doc := js.Global().Get("document")
	tip := doc.Call("createElement", "div")
	tip.Get("style").Set("cssText",
		"position:fixed;display:none;pointer-events:none;z-index:10000;"+
			"background:#1e1e2eee;border:1px solid #444;border-radius:4px;"+
			"padding:3px 9px;font-family:"+rulesDevice.KDeviceFontFamilyMono+";"+
			"font-size:13px;color:#fff;white-space:nowrap;"+
			"box-shadow:0 2px 8px rgba(0,0,0,0.5);")
	doc.Get("body").Call("appendChild", tip)
	tipEl = tip
}

// Show displays the tooltip near the given VIEWPORT coordinates (the caller
// converts canvas coordinates via getBoundingClientRect). The three text
// parts join as "label · typ · dir"; empty parts are skipped so a connector
// without a label still reads cleanly ("int · in").
//
// Português: Exibe o tooltip nas coordenadas de VIEWPORT dadas (o chamador
// converte de canvas via getBoundingClientRect). As três partes viram
// "label · tipo · dir"; partes vazias são puladas, então conector sem label
// ainda lê limpo ("int · in").
func Show(label, typ, dir string, viewportX, viewportY float64) {
	ensure()

	text := ""
	for _, part := range []string{label, typ, dir} {
		if part == "" {
			continue
		}
		if text != "" {
			text += " · "
		}
		text += part
	}
	tipEl.Set("textContent", text)
	tipEl.Get("style").Set("whiteSpace", "nowrap")
	tipEl.Get("style").Set("maxWidth", "none")
	tipEl.Get("style").Set("left", fmt.Sprintf("%.0fpx", viewportX))
	tipEl.Get("style").Set("top", fmt.Sprintf("%.0fpx", viewportY))
	tipEl.Get("style").Set("display", "block")
	visible = true
}

// ShowComment displays a DEVICE-BODY comment near the given viewport
// coordinates. Unlike the single-line pin tip, comments are multi-line: the
// element switches to pre-line wrapping with a width cap, and Show switches
// it back — the two modes share the one element.
// Português: Exibe um comentário de CORPO de device nas coordenadas de
// viewport dadas. Diferente do tip de pino (linha única), comentários são
// multi-linha: o elemento troca para quebra pre-line com teto de largura, e
// o Show troca de volta — os dois modos compartilham o mesmo elemento.
func ShowComment(text string, viewportX, viewportY float64) {
	ensure()
	tipEl.Set("textContent", text)
	tipEl.Get("style").Set("whiteSpace", "pre-line")
	tipEl.Get("style").Set("maxWidth", "340px")
	tipEl.Get("style").Set("left", fmt.Sprintf("%.0fpx", viewportX))
	tipEl.Get("style").Set("top", fmt.Sprintf("%.0fpx", viewportY))
	tipEl.Get("style").Set("display", "block")
	visible = true
}

// Hide conceals the tooltip. Safe to call before the element exists and
// safe to call repeatedly — the hide triggers (pointer left the pin, wire
// draft started, stage clicked, canvas left) overlap on purpose.
//
// Português: Esconde o tooltip. Seguro antes do elemento existir e seguro
// repetido — os gatilhos de esconder (ponteiro saiu do pino, draft de fio
// começou, clique no stage, saiu do canvas) se sobrepõem de propósito.
func Hide() {
	if !tipEl.Truthy() || !visible {
		return
	}
	tipEl.Get("style").Set("display", "none")
	visible = false
}

// IsVisible reports whether the tooltip is currently shown.
//
// Português: Informa se o tooltip está visível.
func IsVisible() bool { return visible }
