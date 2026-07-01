// /ide/translate/translate.go
package translate

// translate — Loads i18n translations from the server at startup.
//
// English:
//
//	Uses JavaScript fetch() via syscall/js to call the translation API.
//	Parses the response into a go-i18n v2 Bundle and creates a Localizer
//	for the user's preferred locale (falls back to en-US).
//
//	Locale detection order:
//	  1. localStorage "locale" key — set by the SPA when the user picks a
//	     language in the sidebar or profile page. This is the user's explicit
//	     choice and takes priority over everything else.
//	  2. navigator.language — browser default (OS-level setting).
//	  3. rulesServer.DefaultLocale — hardcoded fallback ("en-US").
//
//	Usage in WASM main.go:
//	  translate.Load()  // blocks until translations are fetched
//	  // then use translate.Localizer anywhere
//
// Português:
//
//	Usa JavaScript fetch() via syscall/js para chamar a API de tradução.
//	Faz parse da resposta em um Bundle go-i18n v2 e cria um Localizer
//	para o locale preferido do usuário (fallback para en-US).
//
//	Ordem de detecção de locale:
//	  1. localStorage "locale" — definido pela SPA quando o usuário escolhe
//	     um idioma no sidebar ou perfil. Preferência explícita do usuário.
//	  2. navigator.language — idioma padrão do navegador.
//	  3. rulesServer.DefaultLocale — fallback hardcoded ("en-US").

import (
	"encoding/json"
	"log"
	"sync"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Localizer is the global localizer. Use after calling Load().
// Safe to use from multiple goroutines after Load() returns.
var Localizer *i18n.Localizer

// bundle is the go-i18n bundle (shared, created once).
var bundle *i18n.Bundle

// apiResponse mirrors the server envelope: {metadata, data}.
type apiResponse struct {
	Metadata struct {
		Status int     `json:"status"`
		Error  *string `json:"error"`
	} `json:"metadata"`
	Data *bundleData `json:"data"`
}

type bundleData struct {
	BundleID string       `json:"bundleId"`
	Locale   string       `json:"locale"`
	Messages []apiMessage `json:"messages"`
}

type apiMessage struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Other       string `json:"other"`
	One         string `json:"one,omitempty"`
	Zero        string `json:"zero,omitempty"`
	Few         string `json:"few,omitempty"`
	Many        string `json:"many,omitempty"`
}

// Load fetches translations from the server and initializes the Localizer.
// Blocks until complete. Safe to call from WASM main goroutine.
//
// Detection order:
//  1. localStorage "locale" key (user's explicit SPA preference)
//  2. Browser navigator.language (e.g. "pt-BR")
//  3. Falls back to rulesServer.DefaultLocale ("en-US")
//
// If the detected locale is not found on the server, tries rulesServer.DefaultLocale.
// If both fail, creates an empty Localizer (messages return their ID).
//
// Português:
//
//	Busca traduções do servidor e inicializa o Localizer.
//	Bloqueia até completar. Seguro para chamar da goroutine principal WASM.
func Load() {
	bundle = i18n.NewBundle(language.English)

	// Detect user locale (localStorage → navigator.language → default).
	userLocale := detectUserLocale()

	// Try user locale first.
	loaded := loadLocale(userLocale)

	// If user locale failed and it's not the default, try default.
	if !loaded && userLocale != rulesServer.DefaultLocale {
		log.Printf("[TRANSLATE] Trying fallback locale: %s", rulesServer.DefaultLocale)
		loaded = loadLocale(rulesServer.DefaultLocale)
	}

	if !loaded {
		log.Printf("[TRANSLATE] No translations loaded, using message IDs as fallback")
	}

	// Create localizer with user locale + fallback.
	Localizer = i18n.NewLocalizer(bundle, userLocale, rulesServer.DefaultLocale)
}

// loadLocale fetches a single locale from the server and registers messages.
// Returns true if successful.
func loadLocale(locale string) bool {
	url := rulesServer.ServerURL + rulesServer.EndpointTranslations + locale
	log.Printf("[TRANSLATE] Fetching: %s", url)

	body, ok := jsFetch(url)
	if !ok {
		log.Printf("[TRANSLATE] Fetch failed for %s", locale)
		return false
	}

	var resp apiResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		log.Printf("[TRANSLATE] JSON parse error for %s: %v", locale, err)
		return false
	}

	if resp.Metadata.Status != 200 || resp.Data == nil {
		errMsg := ""
		if resp.Metadata.Error != nil {
			errMsg = *resp.Metadata.Error
		}
		log.Printf("[TRANSLATE] Server returned %d for %s: %s", resp.Metadata.Status, locale, errMsg)
		return false
	}

	// Register each message in the bundle.
	tag := language.Make(locale)
	count := 0
	for _, m := range resp.Data.Messages {
		msg := &i18n.Message{
			ID:          m.ID,
			Description: m.Description,
			Other:       m.Other,
			One:         m.One,
			Zero:        m.Zero,
		}
		if err := bundle.AddMessages(tag, msg); err != nil {
			log.Printf("[TRANSLATE] Failed to add message %q: %v", m.ID, err)
			continue
		}
		count++
	}

	log.Printf("[TRANSLATE] Loaded %d messages for %s (bundle: %s)", count, locale, resp.Data.BundleID)
	return count > 0
}

// detectUserLocale resolves the user's preferred locale.
//
// Priority order:
//  1. localStorage "locale" key — the user's explicit choice made in the SPA
//     (sidebar dropdown or profile settings page). This key is set by
//     api.js switchLocale() and persisted across sessions. Because the IDE
//     iframe is same-origin, it shares localStorage with the SPA.
//  2. navigator.language — the browser's default language (OS-level setting).
//  3. rulesServer.DefaultLocale — hardcoded fallback ("en-US").
//
// Português:
//
//	Resolve o locale preferido do usuário.
//	Prioridade: localStorage "locale" > navigator.language > DefaultLocale.
func detectUserLocale() string {
	// 1. Check localStorage — user's explicit SPA preference.
	//    The SPA sets localStorage.setItem("locale", "pt-BR") whenever the
	//    user changes language. Same-origin iframe shares this storage.
	storage := js.Global().Get("localStorage")
	if !storage.IsUndefined() && !storage.IsNull() {
		saved := storage.Call("getItem", "locale")
		if !saved.IsUndefined() && !saved.IsNull() {
			locale := saved.String()
			if locale != "" {
				return locale
			}
		}
	}

	// 2. Fall back to browser language.
	nav := js.Global().Get("navigator")
	if !nav.IsUndefined() && !nav.IsNull() {
		lang := nav.Get("language")
		if !lang.IsUndefined() && !lang.IsNull() {
			locale := lang.String()
			if locale != "" {
				return locale
			}
		}
	}

	// 3. Last resort: hardcoded default.
	log.Printf("[TRANSLATE] Using default locale: %s", rulesServer.DefaultLocale)
	return rulesServer.DefaultLocale
}

// jsFetch performs a synchronous HTTP GET using JavaScript fetch() + Promise.
// Blocks the calling goroutine until the response is available.
//
// Português:
//
//	Executa um HTTP GET síncrono usando JavaScript fetch() + Promise.
//	Bloqueia a goroutine chamadora até a resposta estar disponível.
func jsFetch(url string) (body string, ok bool) {
	var wg sync.WaitGroup
	wg.Add(1)

	var result string
	var success bool

	// fetch(url).then(r => r.text()).then(text => onSuccess(text)).catch(err => onError(err))
	onSuccess := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		result = args[0].String()
		success = true
		wg.Done()
		return nil
	})
	defer onSuccess.Release()

	onError := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Printf("[TRANSLATE] fetch error: %v", args[0])
		success = false
		wg.Done()
		return nil
	})
	defer onError.Release()

	toText := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return args[0].Call("text")
	})
	defer toText.Release()

	promise := js.Global().Call("fetch", url)
	promise.Call("then", toText).Call("then", onSuccess).Call("catch", onError)

	wg.Wait()
	return result, success
}
