// ide/ui/mainMenu/menuTreeClient.go — Fetches the menu tree from the server.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// English:
//
//	LoadMenuTree fetches the resolved menu tree from GET /api/v1/menu/tree.
//	The server returns a nested JSON structure with all cascades resolved
//	(labels, icons, help markdown, visibility rules, user prefs) so the WASM
//	does not perform any fallback logic.
//
//	The tree now includes ALL menu items: system items, branded sections,
//	device categories, individual devices, and templates. The WASM uses
//	device_struct_name to match against loaded BlackBoxDefClient entries
//	and build Init/method submenus for devices.
//
//	The function blocks until the response arrives (channel-based sync in
//	WASM goroutine), following the same pattern as LoadSections in sections.go.
//
//	On failure (network error, server unreachable, empty response), returns nil.
//	The caller (MenuBuilder.Build) falls back to legacy hardcoded menu.
//
// Português:
//
//	Busca a árvore de menu do servidor. Retorna nil em caso de erro.
//	O caller cai de volta para o menu hardcoded legado.
package mainMenu

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/blackbox"
	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// RailSlot is one node in the menu tree received from the server.
// The server resolves custom labels (admin overrides) but also sends
// label_key and label_fallback so the WASM can call translate.T for
// i18n translation of standard items without custom overrides.
type RailSlot struct {
	SlotID         string `json:"slot_id"`
	SlotType       string `json:"slot_type"`        // "system" | "section" | "device" | "category" | "template"
	ItemType       string `json:"item_type"`        // "submenu" | "action" | "exit"
	Label          string `json:"label"`            // resolved by server cascade
	LabelKey       string `json:"label_key"`        // translate.T key for i18n
	LabelFallback  string `json:"label_fallback"`   // translate.T english fallback
	HasCustomLabel bool   `json:"has_custom_label"` // true = admin override, use Label directly
	IconFA         string `json:"icon_fa"`          // FA name or custom SVG path
	IconViewBox    string `json:"icon_viewbox"`     // viewbox for the icon
	ColorBrand     string `json:"color_brand"`      // brand color (sections only)

	// HelpTabs is the ordered list of help pages for this slot, resolved
	// by the server's cascade (profile + locale, with English fallback).
	// One entry per `ord` row in menu_help for the chosen bucket;
	// img references inside Content are already rewritten to inline
	// base64 `data:` URLs.
	//
	// Empty slice (not nil) when the slot has no help — the JSON
	// contract enforces an array, never null, so mergeTreeHelp can
	// range over the slice without a nil guard.
	HelpTabs []blackbox.HelpTabClient `json:"help_tabs"`

	Children []RailSlot `json:"children"` // nil for leaf nodes

	// DeviceStructName is the Go struct name (e.g. "APDS9960") for device items.
	// The WASM uses this to find the matching BlackBoxDefClient in the loaded
	// bbDefs slice and build the correct Init/method submenu.
	// Empty for non-device items.
	DeviceStructName string `json:"device_struct_name"`

	// DeviceParsedJSON is the raw BlackBoxDef JSON embedded by the tree endpoint
	// for curated section devices (Behavior B). When present, the WASM can build
	// the Init/method submenu without needing the device in the user's own bbDefs.
	// Empty for non-device items and when the blackbox has no parsed_json.
	DeviceParsedJSON string `json:"device_parsed_json"`

	// Section brand colors — the 3 pipeline state colors for branded sections.
	// Used by the WASM to create hexMenu.IconStyle arrays with section branding.
	// Empty for non-section items.
	ColorNormal    string `json:"color_normal"`
	ColorAttention string `json:"color_attention"`
	ColorFeatured  string `json:"color_featured"`
}

// menuTreeAPIResponse is the JSON envelope returned by GET /api/v1/menu/tree.
type menuTreeAPIResponse struct {
	Metadata struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	} `json:"metadata"`
	Data struct {
		ProfileID            string     `json:"profile_id"`
		HideUserPrefsOverlay bool       `json:"hide_user_prefs_overlay"`
		Tree                 []RailSlot `json:"tree"`
	} `json:"data"`
}

// MenuTreeMeta holds metadata from the tree response that the workspace
// may need (e.g., to decide whether to show the preferences overlay).
type MenuTreeMeta struct {
	ProfileID            string
	HideUserPrefsOverlay bool
}

// ─── Loader ───────────────────────────────────────────────────────────────────

// LoadMenuTree fetches the resolved menu tree from the server.
//
// The locale parameter is sent as a query param so the server resolves
// labels and help in the correct language. If empty, defaults to "en".
//
// Returns the tree of visible items and metadata, or nil on any error.
// The caller should fall back to legacy hardcoded menu when nil is returned.
func LoadMenuTree() ([]RailSlot, *MenuTreeMeta) {
	// Use the locale chosen by the user in their profile settings.
	locale := detectUserLocale()

	url := rulesServer.ServerURL + rulesServer.EndpointMenuTree + "?locale=" + locale
	token := rulesServer.GetAuthToken()

	log.Printf("[mainMenu/tree] Fetching menu tree from %s", url)

	type result struct {
		raw []byte
		err string
	}
	ch := make(chan result, 1)

	// Step 1: fetch → check HTTP status → parse JSON.
	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			status := resp.Get("status").Int()
			ch <- result{err: fmt.Sprintf("HTTP %d from %s", status, url)}
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

	// Build fetch options with auth token.
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

	// Block until response arrives.
	res := <-ch
	thenResponse.Release()
	thenParse.Release()
	catchFn.Release()

	if res.err != "" {
		log.Printf("[mainMenu/tree] LoadMenuTree error: %s", res.err)
		return nil, nil
	}

	// Parse the API envelope.
	var envelope menuTreeAPIResponse
	if err := json.Unmarshal(res.raw, &envelope); err != nil {
		log.Printf("[mainMenu/tree] JSON unmarshal error: %v", err)
		return nil, nil
	}

	if envelope.Metadata.Status != 200 {
		log.Printf("[mainMenu/tree] server error: %s", envelope.Metadata.Error)
		return nil, nil
	}

	tree := envelope.Data.Tree
	meta := &MenuTreeMeta{
		ProfileID:            envelope.Data.ProfileID,
		HideUserPrefsOverlay: envelope.Data.HideUserPrefsOverlay,
	}

	log.Printf("[mainMenu/tree] Loaded menu tree: profile=%s, %d root item(s), hidePrefs=%v",
		meta.ProfileID, len(tree), meta.HideUserPrefsOverlay)

	return tree, meta
}

// detectUserLocale returns the locale chosen by the user in the portal
// profile settings (stored in localStorage as "locale"). Falls back to
// the browser's navigator.language, then to "en" as a last resort.
//
// The user's explicit choice always takes priority over the browser locale.
func detectUserLocale() string {
	// Priority 1: user's chosen locale from portal settings (localStorage).
	ls := js.Global().Get("localStorage")
	if ls.Truthy() {
		stored := ls.Call("getItem", "locale")
		if stored.Truthy() {
			locale := strings.TrimSpace(stored.String())
			if idx := strings.IndexByte(locale, '-'); idx > 0 {
				locale = strings.ToLower(locale[:idx])
			}
			if locale != "" {
				return locale
			}
		}
	}

	// Priority 2: browser locale (navigator.language).
	nav := js.Global().Get("navigator")
	if !nav.IsUndefined() && !nav.IsNull() {
		lang := nav.Get("language")
		if lang.Truthy() {
			locale := lang.String()
			if idx := strings.IndexByte(locale, '-'); idx > 0 {
				return strings.ToLower(locale[:idx])
			}
			return strings.ToLower(locale)
		}
	}

	return "en"
}
