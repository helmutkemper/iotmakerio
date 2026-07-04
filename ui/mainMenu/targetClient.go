// mainMenu/targetClient.go — Fetches the hardware-target list.
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	LoadTargets fetches the selectable hardware targets from GET /api/v1/targets
//	— the boards the maker's dropdown offers. The endpoint is public (no token
//	required), and returns only display fields; the codegen-internal profile and
//	string-buffer size are resolved server-side from the chosen id at generation
//	time, so they never travel to the client.
//
//	This mirrors LoadMenuTree exactly: fetch → check HTTP status → parse JSON,
//	blocking on a channel in the WASM goroutine. On any error (network, HTTP,
//	malformed JSON) it returns nil; the caller should treat nil as "no targets
//	available" and degrade gracefully rather than crash.
//
// Português:
//
//	Busca as placas-alvo selecionáveis de GET /api/v1/targets — as placas que o
//	dropdown do maker oferece. O endpoint é público (sem token) e retorna só
//	campos de exibição; o profile e o tamanho de buffer internos do codegen são
//	resolvidos no servidor a partir do id escolhido, nunca viajam ao cliente.
//	Espelha o LoadMenuTree: fetch → status HTTP → parse JSON, bloqueando num
//	channel. Retorna nil em qualquer erro; o caller deve degradar sem quebrar.
package mainMenu

import (
	"encoding/json"
	"fmt"
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// TargetView is one selectable hardware target as the dropdown needs it — the
// display fields only, matching the server's targetView DTO. The codegen fields
// (which type profile, what buffer size) are deliberately absent: they never
// leave the server.
type TargetView struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	RAMBytes    int    `json:"ramBytes"` // 0 = ample / not applicable
	Icon        string `json:"icon"`

	// StringBufferSize is the board's default string buffer, in bytes. Used by
	// the picker's advanced panel to prefill its field with the current value.
	//
	// Português: Buffer padrão da placa, em bytes. Usado pelo painel avançado do
	// picker para pré-preencher o campo com o valor atual.
	StringBufferSize int `json:"stringBufferSize"`
}

// targetsAPIResponse is the { metadata, data } envelope returned by
// GET /api/v1/targets — the same shape the menu-tree and translation endpoints
// use, so this parses identically.
type targetsAPIResponse struct {
	Metadata struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	} `json:"metadata"`
	Data struct {
		Targets []TargetView `json:"targets"`
	} `json:"data"`
}

// LoadTargets fetches the hardware-target list from the server, already ordered
// for display. Returns nil on any error; the caller should treat nil as "no
// targets available".
func LoadTargets() []TargetView {
	url := rulesServer.ServerURL + rulesServer.EndpointTargets
	token := rulesServer.GetAuthToken()

	log.Printf("[mainMenu/targets] Fetching targets from %s", url)

	type result struct {
		raw []byte
		err string
	}
	ch := make(chan result, 1)

	// Step 1: fetch → check HTTP status → parse JSON.
	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			ch <- result{err: fmt.Sprintf("HTTP %d from %s", resp.Get("status").Int(), url)}
			return js.Null()
		}
		return resp.Call("json")
	})

	thenParse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			ch <- result{err: "server returned null body"}
			return nil
		}
		raw := js.Global().Get("JSON").Call("stringify", args[0]).String()
		ch <- result{raw: []byte(raw)}
		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := "network error"
		if args[0].Get("message").Truthy() {
			msg = args[0].Get("message").String()
		}
		ch <- result{err: msg}
		return nil
	})

	// The endpoint is public; the token is sent only when present, harmlessly
	// ignored by the public route — this keeps the fetch identical to the other
	// clients rather than special-casing.
	opts := js.Global().Get("Object").New()
	if token != "" {
		headers := js.Global().Get("Object").New()
		headers.Set("Authorization", token)
		opts.Set("headers", headers)
	}

	js.Global().Call("fetch", url, opts).
		Call("then", thenResponse).
		Call("then", thenParse).
		Call("catch", catchFn)

	// Block until the response arrives.
	res := <-ch
	thenResponse.Release()
	thenParse.Release()
	catchFn.Release()

	if res.err != "" {
		log.Printf("[mainMenu/targets] LoadTargets error: %s", res.err)
		return nil
	}

	var envelope targetsAPIResponse
	if err := json.Unmarshal(res.raw, &envelope); err != nil {
		log.Printf("[mainMenu/targets] JSON unmarshal error: %v", err)
		return nil
	}
	if envelope.Metadata.Status != 200 {
		log.Printf("[mainMenu/targets] server error: %s", envelope.Metadata.Error)
		return nil
	}

	return envelope.Data.Targets
}
