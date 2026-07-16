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

// monacoFieldProviders holds the DISPOSE handle of the completion
// provider each FieldMonaco registered — providers are GLOBAL to Monaco
// per language, so the previous one must be disposed before registering
// again or every overlay open stacks another. Keyed by field.Key like
// the editors. Português: Guarda o handle de dispose do provider de cada
// FieldMonaco — providers são GLOBAIS por linguagem no Monaco, então o
// anterior precisa ser descartado antes de registrar de novo, ou cada
// abertura empilha mais um.
var monacoFieldProviders = map[string]js.Value{}

// registerCompletionDict parses the dictionary JSON and registers a
// Monaco completion provider for lang, disposing the field's previous
// provider first. Dictionary shape: [{"label","insert","doc"}]; "insert"
// falls back to the label and supports snippet tab-stops ($1). A broken
// dictionary is ignored — the editor must open regardless.
// Português: Registra o provider de autocompletar do dicionário para a
// linguagem, descartando o anterior do campo. "insert" cai no label e
// suporta tab-stops de snippet. Dicionário quebrado é ignorado — o
// editor abre de qualquer jeito.
func registerCompletionDict(key, lang, dictJSON string) {
	if prev, ok := monacoFieldProviders[key]; ok && prev.Truthy() {
		prev.Call("dispose")
		delete(monacoFieldProviders, key)
	}
	if dictJSON == "" {
		return
	}
	monaco := js.Global().Get("monaco")
	if !monaco.Truthy() {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Overlay] completion dict rejected: %v", r)
		}
	}()
	items := js.Global().Get("JSON").Call("parse", dictJSON)
	if items.Type() != js.TypeObject || items.Length() == 0 {
		return
	}

	snippetRule := monaco.Get("languages").
		Get("CompletionItemInsertTextRule").Get("InsertAsSnippet")
	snippetKind := monaco.Get("languages").
		Get("CompletionItemKind").Get("Snippet")

	provider := js.Global().Get("Object").New()
	provider.Set("provideCompletionItems",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			suggestions := js.Global().Get("Array").New()
			for i := 0; i < items.Length(); i++ {
				it := items.Index(i)
				label := it.Get("label")
				if !label.Truthy() {
					continue
				}
				insert := it.Get("insert")
				if !insert.Truthy() {
					insert = label
				}
				s := js.Global().Get("Object").New()
				s.Set("label", label)
				s.Set("kind", snippetKind)
				s.Set("insertText", insert)
				s.Set("insertTextRules", snippetRule)
				if doc := it.Get("doc"); doc.Truthy() {
					// detail renders INLINE on the suggestion row — the
					// documentation pane is collapsed by default in
					// Monaco and nobody discovers the toggle (field
					// report 2026-07-13: "o doc não aparece"). Both are
					// set: the row shows the short read, the expandable
					// pane keeps the full text. Português: detail aparece
					// NA LINHA da sugestão — o painel de documentation
					// nasce recolhido e ninguém acha o botão. Os dois são
					// preenchidos: a linha dá a leitura curta, o painel
					// expansível guarda o texto completo.
					s.Set("detail", doc)
					s.Set("documentation", doc)
				}
				suggestions.Call("push", s)
			}
			result := js.Global().Get("Object").New()
			result.Set("suggestions", suggestions)
			return result
		}))

	disp := monaco.Get("languages").Call(
		"registerCompletionItemProvider", lang, provider)
	monacoFieldProviders[key] = disp
	log.Printf("[Overlay] completion dict registered: key=%s lang=%s items=%d",
		key, lang, items.Length())
}

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
		// Suggestions open AS YOU TYPE — without this, the completion
		// dictionary only answers Ctrl+Space (2026-07-13 field report:
		// "só digitar server não faz nada"). Português: Sugestões abrem
		// ENQUANTO DIGITA — sem isto, o dicionário só responde ao
		// Ctrl+Espaço.
		opts.Set("quickSuggestions", true)
		opts.Set("quickSuggestionsDelay", 10)
		opts.Set("suggestOnTriggerCharacters", true)
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
		registerCompletionDict(field.Key, lang, field.CompletionDictJSON)
		log.Printf("[Overlay] Monaco FIELD created: language=%s", lang)
	})

	return container, hiddenInput
}
