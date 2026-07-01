// /ide/devices/compBlackBox/helpLang.go
package compBlackBox

// helpLang.go — Session language preference for the help panel.
//
// Language resolution order for markdown help tabs:
//  1. Session storage key "iotmaker_help_lang" — set when the user picks a
//     language from the selector in the help deck. Per-session override.
//  2. localStorage "locale" key — the user's explicit SPA preference (set in
//     sidebar or profile page). Shared with the SPA via same-origin iframe.
//  3. Browser locale (navigator.language), normalised to lowercase BCP-47.
//  4. Hard fallback: "en".
//
// The session preference is stored in sessionStorage (not localStorage) so it
// resets when the browser tab closes. This keeps the preference local to a
// single working session without permanently overriding the user's SPA choice.
//
// The language selector only appears when AvailableHelpLangs() returns more
// than one language for the method being inspected — avoids clutter when
// documentation is available in one language only.

import "syscall/js"

const helpLangStorageKey = "iotmaker_help_lang"

// helpSessionLang returns the active language code for help panels.
//
// Resolution: sessionStorage → localStorage "locale" → navigator.language → "en".
// Returns a lowercase BCP-47 code such as "en", "pt-br", or "en-us".
func helpSessionLang() string {
	// 1. Session preference set by the user in the language selector.
	//    This is a per-session override that resets when the tab closes.
	if lang := sessionStorageGet(helpLangStorageKey); lang != "" {
		return lang
	}

	// 2. User's explicit SPA preference from localStorage.
	//    The SPA stores the chosen locale (e.g. "en-US") in localStorage
	//    under the key "locale". Same-origin iframe shares this storage.
	storage := js.Global().Get("localStorage")
	if !storage.IsUndefined() && !storage.IsNull() {
		saved := storage.Call("getItem", "locale")
		if !saved.IsUndefined() && !saved.IsNull() {
			if s := saved.String(); s != "" {
				return toLowerASCII(s)
			}
		}
	}

	// 3. Browser locale from navigator.language.
	nav := js.Global().Get("navigator")
	if !nav.IsUndefined() && !nav.IsNull() {
		lang := nav.Get("language")
		if !lang.IsUndefined() && !lang.IsNull() {
			if s := lang.String(); s != "" {
				// Normalise "pt-BR" → "pt-br" to match the file-naming convention.
				return toLowerASCII(s)
			}
		}
	}

	// 4. Hard fallback.
	return "en"
}

// SetHelpSessionLang stores a user-selected language in sessionStorage.
// Called by the language selector in the help deck overlay.
func SetHelpSessionLang(lang string) {
	sessionStorageSet(helpLangStorageKey, toLowerASCII(lang))
}

// sessionStorageGet retrieves a value from sessionStorage.
// Returns "" when sessionStorage is unavailable or the key is not present.
func sessionStorageGet(key string) string {
	ss := js.Global().Get("sessionStorage")
	if ss.IsUndefined() || ss.IsNull() {
		return ""
	}
	v := ss.Call("getItem", key)
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	return v.String()
}

// sessionStorageSet stores a value in sessionStorage.
// Silently ignores errors (e.g. when called in a worker context).
func sessionStorageSet(key, value string) {
	ss := js.Global().Get("sessionStorage")
	if ss.IsUndefined() || ss.IsNull() {
		return
	}
	ss.Call("setItem", key, value)
}

// toLowerASCII normalises a BCP-47 tag to lowercase using only ASCII rules,
// avoiding the unicode package for minimal WASM binary size.
// "pt-BR" → "pt-br", "en-US" → "en-us".
func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
