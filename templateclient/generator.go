// ide/templateclient/generator.go

package templateclient

// generator.go — Sends a generate request to the server and triggers a
// browser download of the resulting ZIP.
//
// English:
//
//	GenerateAndDownload performs two operations:
//	  1. POST /api/v1/templates/:id/generate with the config map.
//	  2. On success, create an object URL from the ZIP blob and trigger
//	     a browser download via a programmatic <a> click.
//
//	The config map maps template variable names to the maker's chosen values.
//	If config is nil, all variables are sent as empty strings (the server
//	will use them or warn about missing values).
//
//	The function is synchronous from the Go perspective — it blocks its
//	calling goroutine until the download is triggered or an error occurs.
//	Always call it from a goroutine.
//
// Português:
//
//	GenerateAndDownload envia o config e faz o download do ZIP resultante.
//	Sempre chame de uma goroutine — bloqueia até o download ser acionado.

import (
	"encoding/json"
	"fmt"
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// GenerateAndDownload sends the template config to the server, receives the
// configured project ZIP, and triggers a browser download.
//
// Parameters:
//   - templateID: the ID of the template to generate.
//   - templateName: used as the suggested download filename.
//   - config: map of template variable names to values. Nil means all defaults.
//
// Must be called from a goroutine.
func GenerateAndDownload(templateID, templateName string, config map[string]string) {
	token := rulesServer.GetAuthToken()
	if token == "" {
		log.Println("[templateclient/generate] No auth token — cannot generate")
		return
	}

	if config == nil {
		config = make(map[string]string)
	}

	// Build request body.
	body := struct {
		Config map[string]string `json:"config"`
	}{Config: config}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		log.Printf("[templateclient/generate] marshal body: %v", err)
		return
	}

	generateURL := fmt.Sprintf("%s%s/%s/generate",
		rulesServer.ServerURL,
		rulesServer.EndpointTemplateGenerate,
		templateID,
	)
	log.Printf("[templateclient/generate] Generating %s from %s", templateName, generateURL)

	// ── POST request → binary ZIP response ───────────────────────────────────
	//
	// Unlike the JSON fetches in loader.go, this call:
	//   1. Uses POST with a JSON body.
	//   2. Expects an application/zip binary response, not JSON.
	//   3. Calls response.blob() instead of response.json().
	//   4. Creates an object URL from the blob and triggers a download.

	type generateResult struct {
		blob js.Value // JS Blob object on success
		err  string
	}
	ch := make(chan generateResult, 1)

	headers := js.Global().Get("Object").New()
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/zip")
	if token != "" {
		headers.Set("Authorization", token)
	}

	opts := js.Global().Get("Object").New()
	opts.Set("method", "POST")
	opts.Set("headers", headers)
	opts.Set("body", string(bodyBytes))

	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		status := resp.Get("status").Int()
		if !resp.Get("ok").Bool() {
			ch <- generateResult{err: fmt.Sprintf("HTTP %d from generate endpoint", status)}
			return js.Null()
		}
		// Return the blob() Promise — chains into thenBlob.
		return resp.Call("blob")
	})

	thenBlob := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		blob := args[0]
		if blob.IsNull() || blob.IsUndefined() {
			ch <- generateResult{err: "server returned empty blob"}
			return nil
		}
		ch <- generateResult{blob: blob}
		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := "network error"
		if args[0].Get("message").Truthy() {
			msg = args[0].Get("message").String()
		}
		ch <- generateResult{err: msg}
		return nil
	})

	js.Global().Call("fetch", generateURL, opts).
		Call("then", thenResponse).
		Call("then", thenBlob).
		Call("catch", catchFn)

	res := <-ch
	thenResponse.Release()
	thenBlob.Release()
	catchFn.Release()

	if res.err != "" {
		log.Printf("[templateclient/generate] Error: %s", res.err)
		return
	}

	// ── Trigger browser download ──────────────────────────────────────────────
	//
	// Standard browser download pattern:
	//   1. URL.createObjectURL(blob)  → creates a temporary URL for the blob
	//   2. Create an <a> with href=url and download=filename
	//   3. Append to body, click(), remove
	//   4. URL.revokeObjectURL(url)   → release the object URL memory
	triggerDownload(res.blob, sanitizeFilename(templateName)+".zip")
}

// triggerDownload initiates a browser file download from a JS Blob object.
// Cleans up the object URL after the click is dispatched.
func triggerDownload(blob js.Value, filename string) {
	urlObj := js.Global().Get("URL").Call("createObjectURL", blob)
	if urlObj.IsNull() || urlObj.IsUndefined() {
		log.Println("[templateclient/generate] Failed to create object URL")
		return
	}
	urlStr := urlObj.String()

	doc := js.Global().Get("document")

	// Create a temporary <a> element with the download attribute.
	a := doc.Call("createElement", "a")
	a.Set("href", urlStr)
	a.Set("download", filename)
	a.Get("style").Set("display", "none")
	doc.Get("body").Call("appendChild", a)
	a.Call("click")
	doc.Get("body").Call("removeChild", a)

	// Schedule URL revocation on the next tick to ensure the download
	// starts before the blob is released.
	releaseFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		js.Global().Get("URL").Call("revokeObjectURL", urlStr)
		return nil
	})
	js.Global().Call("setTimeout", releaseFunc, 1000)
	// Note: releaseFunc is intentionally not released here — it lives briefly
	// until the setTimeout callback fires. The GC handles it after that.

	log.Printf("[templateclient/generate] Download triggered: %s", filename)
}

// sanitizeFilename returns a safe filename (no spaces or special chars).
func sanitizeFilename(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		return "template"
	}
	return string(result)
}
