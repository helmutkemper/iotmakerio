// ide/templateclient/loader.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package templateclient

// loader.go — Fetches available template definitions from the server at startup.
//
// English:
//
//	LoadAllTemplates performs two network round-trips:
//	  1. GET /api/v1/templates    — list of visible templates (meta only)
//	  2. GET /api/v1/templates/:id — full def for each "ready" template
//
//	Both calls use the Bearer token from window._ideAuthToken. If the token
//	is absent (unauthenticated), the server returns 401 and LoadAllTemplates
//	returns nil (graceful degradation).
//
//	The function blocks its calling goroutine. It is called from
//	stageWorkspace.Init(), which already runs in a goroutine — blocking is safe.
//
//	All JS function handles are released after each channel receive to prevent
//	memory leaks in the WASM heap.
//
//	Graceful degradation: any error returns nil. The Templates menu will show
//	"No templates". The IDE remains fully functional — templates can be used
//	after a page reload if the server becomes available.
//
// Português:
//
//	LoadAllTemplates faz duas requisições de rede:
//	  1. GET /api/v1/templates    — lista de templates visíveis
//	  2. GET /api/v1/templates/:id — def completa para cada template "ready"
//
//	Ambas usam o token Bearer de window._ideAuthToken. Qualquer erro retorna
//	nil — degradação graciosa. A IDE permanece totalmente funcional.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// fetchResult is the common return type for all JS fetch calls in this file.
type fetchResult struct {
	raw []byte // JSON response body on success
	err string // error message on failure
}

// LoadAllTemplates fetches all ready templates from the server along with
// their full definitions. Returns nil (graceful degradation) on any error.
//
// Called from stageWorkspace.Init() in a goroutine. Blocks until done.
func LoadAllTemplates() []*TemplateFullClient {
	token := rulesServer.GetAuthToken()
	if token == "" {
		log.Println("[templateclient] No auth token — skipping template load")
		return nil
	}

	// ── Step 1: fetch the template list ──────────────────────────────────────
	listURL := rulesServer.ServerURL + rulesServer.EndpointTemplates
	log.Printf("[templateclient] Fetching template list from %s", listURL)

	listRaw, err := fetchJSON(listURL, token)
	if err != "" {
		log.Printf("[templateclient] List fetch error: %s", err)
		return nil
	}

	// The list endpoint uses the SPA envelope: {"metadata":{...},"data":[...]}
	var listEnvelope struct {
		Data []TemplateMetaClient `json:"data"`
	}
	if jsonErr := json.Unmarshal(listRaw, &listEnvelope); jsonErr != nil {
		log.Printf("[templateclient] List JSON unmarshal error: %v", jsonErr)
		return nil
	}

	metas := listEnvelope.Data
	log.Printf("[templateclient] Found %d template(s) in list", len(metas))

	if len(metas) == 0 {
		return nil
	}

	// ── Step 2: fetch full def for each ready template ────────────────────────
	var result []*TemplateFullClient
	for _, meta := range metas {
		if meta.Status != TemplateStatusReady {
			log.Printf("[templateclient] Skipping %s (status=%s)", meta.Name, meta.Status)
			continue
		}

		full := fetchTemplateDetail(meta, token)
		if full != nil {
			result = append(result, full)
			log.Printf("[templateclient] Loaded template %q (%d devices)", meta.Name, len(full.Def.Devices))
		}
	}

	log.Printf("[templateclient] Loaded %d ready template(s) with full defs", len(result))
	return result
}

// fetchTemplateDetail fetches a single template's full definition.
// Returns nil on any error (the template is skipped silently).
func fetchTemplateDetail(meta TemplateMetaClient, token string) *TemplateFullClient {
	detailURL := rulesServer.ServerURL + rulesServer.EndpointTemplate + "/" + meta.ID
	log.Printf("[templateclient] Fetching detail for %s from %s", meta.Name, detailURL)

	raw, fetchErr := fetchJSON(detailURL, token)
	if fetchErr != "" {
		log.Printf("[templateclient] Detail fetch error for %s: %s", meta.Name, fetchErr)
		return nil
	}

	// Detail endpoint envelope: {"metadata":{...},"data":{"template":{...},"def":{...}}}
	var detailEnvelope struct {
		Data struct {
			Template TemplateMetaClient `json:"template"`
			Def      *TemplateDefClient `json:"def"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &detailEnvelope); err != nil {
		log.Printf("[templateclient] Detail JSON unmarshal error for %s: %v", meta.Name, err)
		return nil
	}

	def := detailEnvelope.Data.Def
	if def == nil {
		log.Printf("[templateclient] Template %s returned nil def (not ready?)", meta.Name)
		return nil
	}

	full := &TemplateFullClient{
		Meta:        meta,
		Def:         def,
		VarDefaults: computeVarDefaults(def),
	}
	return full
}

// computeVarDefaults builds a map of variable name → default prop value by
// walking the Manifest.Vars paths and looking up prop defaults in the devices.
//
// Example: vars["StoreName"] = "StoreConfig.Name"
//
//	→ finds StoreConfig device → finds prop with FieldName="Name" → returns Default
//
// Returns an empty map if no defaults can be resolved (never nil).
func computeVarDefaults(def *TemplateDefClient) map[string]string {
	defaults := make(map[string]string, len(def.Manifest.Vars))
	if def.Manifest.Vars == nil {
		return defaults
	}

	// Build device lookup map.
	deviceByName := make(map[string]*devicePropLookup, len(def.Devices))
	for _, d := range def.Devices {
		lu := &devicePropLookup{propByField: make(map[string]string, len(d.Props))}
		for _, p := range d.Props {
			lu.propByField[p.FieldName] = p.Default
		}
		deviceByName[d.Name] = lu
	}

	for varName, path := range def.Manifest.Vars {
		dotIdx := strings.Index(path, ".")
		if dotIdx < 1 || dotIdx == len(path)-1 {
			defaults[varName] = ""
			continue
		}
		deviceName := path[:dotIdx]
		fieldName := path[dotIdx+1:]

		lu, ok := deviceByName[deviceName]
		if !ok {
			defaults[varName] = ""
			continue
		}
		defaults[varName] = lu.propByField[fieldName]
	}
	return defaults
}

// devicePropLookup is a small helper for computeVarDefaults.
type devicePropLookup struct {
	propByField map[string]string // fieldName → default value
}

// ── Fetch helper ──────────────────────────────────────────────────────────────

// fetchJSON performs a synchronous-looking GET request from inside the WASM
// runtime using the JS Promise → Go channel bridge pattern.
//
// token is the full Authorization header value (e.g. "Bearer xyz").
// Returns (rawBytes, "") on success or ("", errorMessage) on failure.
func fetchJSON(url, token string) ([]byte, string) {
	ch := make(chan fetchResult, 1)

	// Build fetch options with Authorization header.
	headers := js.Global().Get("Object").New()
	headers.Set("Accept", "application/json")
	if token != "" {
		headers.Set("Authorization", token)
	}
	opts := js.Global().Get("Object").New()
	opts.Set("headers", headers)

	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		status := resp.Get("status").Int()
		if status == 401 {
			ch <- fetchResult{err: "unauthorized — check that window._ideAuthToken is set"}
			return js.Null()
		}
		if !resp.Get("ok").Bool() {
			ch <- fetchResult{err: fmt.Sprintf("HTTP %d from %s", status, url)}
			return js.Null()
		}
		return resp.Call("json")
	})

	thenParse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			ch <- fetchResult{err: "server returned null body"}
			return nil
		}
		raw := js.Global().Get("JSON").Call("stringify", args[0]).String()
		ch <- fetchResult{raw: []byte(raw)}
		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := "network error"
		if args[0].Get("message").Truthy() {
			msg = args[0].Get("message").String()
		}
		ch <- fetchResult{err: msg}
		return nil
	})

	js.Global().Call("fetch", url, opts).
		Call("then", thenResponse).
		Call("then", thenParse).
		Call("catch", catchFn)

	res := <-ch
	thenResponse.Release()
	thenParse.Release()
	catchFn.Release()

	return res.raw, res.err
}
