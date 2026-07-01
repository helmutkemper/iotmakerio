package blackbox

// loader.go — Fetches black-box device definitions from the server at startup.
//
// English:
//
//	LoadDefs performs a synchronous-looking GET request from inside the WASM
//	runtime. It blocks the calling goroutine until the server responds (or
//	until an error occurs). The caller (stageWorkspace.Init) is already
//	running in a goroutine, so blocking is safe.
//
//	The function uses the JS Promise → Go channel bridge pattern:
//	  1. Call fetch() via syscall/js, wiring .then() / .catch() callbacks.
//	  2. Each callback sends to a buffered channel (size 1) and returns.
//	  3. The Go goroutine blocks on <-ch, which is released by whichever
//	     JS callback fires first.
//	  4. All js.Func handles are Released after the channel receive to
//	     prevent memory leaks in the WASM heap.
//
//	Graceful degradation: if the server is unreachable, the component bank is
//	empty, or parsing fails, LoadDefs returns nil. The Hardware submenu will
//	show "No devices". The IDE remains fully usable; hardware components can
//	be added after the server comes back online and the user reloads.
//
// Português:
//
//	LoadDefs realiza uma requisição GET aparentemente síncrona a partir do
//	runtime WASM. Bloqueia a goroutine chamadora até o servidor responder.
//	O chamador (stageWorkspace.Init) já está em uma goroutine, então bloquear
//	é seguro.
//
//	Padrão usado: JS Promise → canal Go. Todos os handles js.Func são
//	liberados após o recebimento do canal para evitar vazamentos de memória.
//
//	Degradação graciosa: se o servidor estiver inacessível ou sem dados,
//	retorna nil. O submenu Hardware mostrará "No devices". A IDE continua
//	totalmente funcional sem dispositivos de hardware.
//
// Usage:
//
//	defs := blackbox.LoadDefs()
//	if len(defs) > 0 {
//	    menuBuilder.SetBlackBoxDefs(defs)
//	}

import (
	"encoding/json"
	"fmt"
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// fetchResult carries the outcome of the JS fetch call back to the Go side.
type fetchResult struct {
	// raw holds the JSON bytes of the response body on success.
	raw []byte
	// err holds the error message on failure (HTTP error, network error, etc.).
	err string
}

// LoadDefs fetches all black-box definitions from the server and returns them
// as a slice ready to be passed to menuBuilder.SetBlackBoxDefs().
//
// Returns nil (not an empty slice) on any error so callers can use
//
//	if len(defs) > 0 { ... }
//
// without a separate nil check.
//
// Must be called from a goroutine — it blocks until the fetch completes.
func LoadDefs() []*BlackBoxDefClient {
	url := rulesServer.ServerURL + rulesServer.EndpointBlackBox

	log.Printf("[blackbox/loader] Fetching definitions from %s", url)

	ch := make(chan fetchResult, 1)

	// thenResponse receives the raw Response object.
	// Checks HTTP status and, if OK, calls .json() to decode the body.
	// Returns the Promise from .json() so the next .then() receives the
	// parsed JS value.
	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			status := resp.Get("status").Int()
			ch <- fetchResult{err: fmt.Sprintf("HTTP %d from %s", status, url)}
			return js.Null()
		}
		// Return the json() Promise — chains into thenParse.
		return resp.Call("json")
	})

	// thenParse receives the parsed JS object (the JSON array).
	// Converts it back to a JSON string via JSON.stringify, then
	// unmarshals into []*BlackBoxDefClient on the Go side.
	//
	// Why JSON.stringify then json.Unmarshal?
	//   WASM has no direct way to inspect JS object trees. Serializing
	//   back to a JSON string and then using encoding/json is the standard
	//   pattern for Go/WASM ↔ JS data exchange.
	thenParse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			ch <- fetchResult{err: "server returned null body"}
			return nil
		}
		raw := js.Global().Get("JSON").Call("stringify", args[0]).String()
		ch <- fetchResult{raw: []byte(raw)}
		return nil
	})

	// catchFn handles network failures (DNS, CORS, connection refused, etc.).
	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := "network error"
		if args[0].Get("message").Truthy() {
			msg = args[0].Get("message").String()
		}
		ch <- fetchResult{err: msg}
		return nil
	})

	// Authentication: use the IDE auth token injected via postMessage by the
	// SPA. This is the SAME mechanism used by all other WASM → server calls
	// (see rulesServer.GetAuthToken). Reading from localStorage directly is
	// insecure — stale tokens from other sessions could leak data across users.
	fetchOpts := js.Global().Get("Object").New()
	headers := js.Global().Get("Object").New()
	if token := rulesServer.GetAuthToken(); token != "" {
		headers.Set("Authorization", token)
	}
	fetchOpts.Set("headers", headers)

	// Fire the fetch and chain the handlers.
	js.Global().Call("fetch", url, fetchOpts).
		Call("then", thenResponse).
		Call("then", thenParse).
		Call("catch", catchFn)

	// Block until one of the callbacks writes to the channel.
	res := <-ch

	// Release all JS function handles to free WASM heap memory.
	// This must happen AFTER the channel receive — releasing before would
	// allow the GC to collect the funcs while JS still holds references.
	thenResponse.Release()
	thenParse.Release()
	catchFn.Release()

	if res.err != "" {
		log.Printf("[blackbox/loader] LoadDefs error: %s", res.err)
		return nil
	}

	// Unmarshal the JSON array into the client type slice.
	var defs []*BlackBoxDefClient
	if err := json.Unmarshal(res.raw, &defs); err != nil {
		log.Printf("[blackbox/loader] JSON unmarshal error: %v", err)
		return nil
	}

	log.Printf("[blackbox/loader] Loaded %d black-box definition(s)", len(defs))
	return defs
}
