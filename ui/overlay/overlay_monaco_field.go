// /ide/ui/overlay/overlay_monaco_field.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package overlay

// overlay_monaco_field.go — FieldMonaco: an EDITABLE Monaco editor inside a
// TabForm, collected by the ordinary doSave loop.
//
// The house pattern for rich fields (FieldMap, FieldSlice, FieldFile) is
// "container + hidden input kept in sync"; this field applies it to code:
// every content change mirrors the editor's value into the hidden input,
// so the form's save collects the text like any other field — zero new
// save plumbing. Born for the Data · Text device (the maker authors
// yaml/xml/json/... that ships as an embedded byte array).
//
// Português: FieldMonaco: um editor Monaco EDITÁVEL dentro de um TabForm,
// coletado pelo doSave comum. O padrão da casa para campos ricos é
// "container + hidden input sincronizado"; este campo o aplica a código:
// cada mudança espelha o valor do editor no input oculto, e o save do
// form coleta o texto como qualquer campo — zero encanamento novo.
// Nasceu para o device Data · Text.

import (
	"fmt"
	"log"
	"syscall/js"
)

// monacoFieldEditors maps a FieldMonaco's Key to its live editor instance
// for the CURRENTLY OPEN overlay, so a sibling FieldSelect carrying
// MonacoLanguageTarget can retarget the highlighting the instant the
// maker picks a new language — no close/reopen. Entries are overwritten
// on every open (the overlay is modal: one form at a time), so staleness
// is impossible by construction.
// Português: Mapeia a Key de um FieldMonaco para a instância viva do
// editor do overlay ABERTO, para um FieldSelect irmão (com
// MonacoLanguageTarget) trocar o highlight no instante em que o maker
// escolhe outra linguagem — sem fechar/reabrir. Entradas são
// sobrescritas a cada abertura (o overlay é modal), então não envelhecem.
var monacoFieldEditors = map[string]js.Value{}

// RetargetMonacoField switches the language of the live FieldMonaco
// registered under key. Silently ignores unknown keys (editor still
// loading or overlay without one). Português: Troca a linguagem do
// FieldMonaco vivo registrado sob key; ignora keys desconhecidas.
func RetargetMonacoField(key, language string) {
	editor, ok := monacoFieldEditors[key]
	if !ok || !editor.Truthy() {
		return
	}
	monaco := js.Global().Get("monaco")
	if !monaco.Truthy() {
		return
	}
	model := editor.Call("getModel")
	if !model.Truthy() {
		return
	}
	monaco.Get("editor").Call("setModelLanguage", model, language)
	log.Printf("[Overlay] Monaco FIELD retargeted: key=%s language=%s", key, language)
}

// buildMonacoField renders the editable editor and returns (container,
// hiddenInput). field.Value is the initial content; field.Language the
// Monaco language id ("yaml", "xml", "json", …, ""=plaintext).
// Português: Renderiza o editor editável e retorna (container, input
// oculto). field.Value é o conteúdo inicial; field.Language o id de
// linguagem do Monaco.
func buildMonacoField(doc js.Value, field Field) (container js.Value, hiddenInput js.Value) {
	hiddenInput = doc.Call("createElement", "input")
	hiddenInput.Set("type", "hidden")
	hiddenInput.Set("value", field.Value)

	container = doc.Call("createElement", "div")
	container.Get("style").Set("cssText",
		"flex:1;position:relative;height:260px;min-height:260px;"+
			"border:1px solid "+colSurface1+";border-radius:6px;overflow:hidden;")

	loading := doc.Call("createElement", "div")
	loading.Get("style").Set("cssText", fmt.Sprintf(
		"position:absolute;top:50%%;left:50%%;transform:translate(-50%%,-50%%);"+
			"color:%s;font-size:12px;font-family:sans-serif;", colSubtext))
	loading.Set("textContent", "Loading editor...")
	container.Call("appendChild", loading)

	lang := field.Language
	if lang == "" {
		lang = "plaintext"
	}

	loadMonaco(doc, func() {
		if loading.Get("parentNode").Truthy() {
			container.Call("removeChild", loading)
		}
		monaco := js.Global().Get("monaco")
		if !monaco.Truthy() {
			loading.Set("textContent", "Failed to load Monaco editor")
			return
		}

		editorDiv := doc.Call("createElement", "div")
		editorDiv.Get("style").Set("cssText", "width:100%;height:100%;")
		container.Call("appendChild", editorDiv)

		opts := js.Global().Get("Object").New()
		opts.Set("value", field.Value)
		opts.Set("language", lang)
		opts.Set("theme", "vs-dark")
		opts.Set("readOnly", false)
		opts.Set("fontSize", 13)
		opts.Set("lineNumbers", "on")
		opts.Set("scrollBeyondLastLine", false)
		opts.Set("automaticLayout", true)
		opts.Set("wordWrap", "on")
		minimapOpts := js.Global().Get("Object").New()
		minimapOpts.Set("enabled", false)
		opts.Set("minimap", minimapOpts)
		paddingOpts := js.Global().Get("Object").New()
		paddingOpts.Set("top", 8)
		paddingOpts.Set("bottom", 8)
		opts.Set("padding", paddingOpts)

		editor := monaco.Get("editor").Call("create", editorDiv, opts)

		// The sync that makes the field collectable: every keystroke
		// mirrors into the hidden input the doSave loop reads.
		// Português: A sincronia que torna o campo coletável: cada tecla
		// espelha no input oculto que o doSave lê.
		editor.Call("onDidChangeModelContent",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				hiddenInput.Set("value", editor.Call("getValue").String())
				return nil
			}))

		monacoFieldEditors[field.Key] = editor
		log.Printf("[Overlay] Monaco FIELD created: language=%s", lang)
	})

	return container, hiddenInput
}
