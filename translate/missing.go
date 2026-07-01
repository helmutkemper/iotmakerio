package translate

// missing.go — Reports missing translation keys to the server.
//
// English:
//
//	When T() cannot find a translation and returns the fallback, it calls
//	ReportMissing() asynchronously. The report is fire-and-forget: it does
//	not block the caller, does not wait for a server response, and silently
//	logs errors to the browser console for debugging.
//
//	The server saves the key only if it doesn't already exist, so repeated
//	calls for the same key are harmless (idempotent).
//
//	A dedup map prevents sending the same key more than once per session.
//
// Português:
//
//	Quando T() não encontra uma tradução e retorna o fallback, chama
//	ReportMissing() de forma assíncrona. O reporte é fire-and-forget:
//	não bloqueia, não espera resposta, e loga erros no console do
//	navegador apenas para facilitar debug.
//
//	O servidor salva a chave apenas se ela ainda não existir.
//	Um mapa de dedup evita enviar a mesma chave mais de uma vez por sessão.

import (
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// Locales holds all locale codes the system should maintain translations for.
// Set before calling Load(). ReportMissing sends to ALL of these.
//
// Português: Contém todos os códigos de locale que o sistema deve manter.
// Defina antes de chamar Load(). ReportMissing envia para TODOS.
var Locales = []string{"pt-BR", "en-US"} //todo: isto vem do banco de dados

// reported tracks keys already sent this session to avoid duplicates.
var reported = make(map[string]bool)

// ReportMissing sends a missing translation key to ALL locales asynchronously.
// Fire-and-forget: does not block, does not wait for response.
// Errors are logged to the browser console but otherwise ignored.
// The server only saves the key if it doesn't already exist (idempotent).
//
// Português: Envia uma chave de tradução faltante para TODOS os locales
// de forma assíncrona. Fire-and-forget: não bloqueia, não espera resposta.
// O servidor só salva se a chave ainda não existir (idempotente).
func ReportMissing(id, fallback string) {
	// Dedup: don't send the same key twice in the same session
	if reported[id] {
		return
	}
	reported[id] = true

	body := `{"id":"` + escapeJSON(id) + `","other":"` + escapeJSON(fallback) + `"}`

	for _, locale := range Locales {
		loc := locale // capture for goroutine
		go func() {
			url := rulesServer.ServerURL + "/api/v1/translations/" + loc + "/missing"

			opts := js.Global().Get("Object").New()
			opts.Set("method", "POST")
			opts.Set("body", body)

			headers := js.Global().Get("Headers").New()
			headers.Call("set", "Content-Type", "application/json")
			opts.Set("headers", headers)

			onError := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				log.Printf("[TRANSLATE] Failed to report missing key %q for %s: %v", id, loc, args[0])
				return nil
			})
			defer onError.Release()

			js.Global().Call("fetch", url, opts).Call("catch", onError)
			log.Printf("[TRANSLATE] Reported missing key: %s → %q (locale: %s)", id, fallback, loc)
		}()
	}
}

// escapeJSON escapes special characters for safe JSON string embedding.
func escapeJSON(s string) string {
	result := ""
	for _, c := range s {
		switch c {
		case '"':
			result += `\"`
		case '\\':
			result += `\\`
		case '\n':
			result += `\n`
		case '\r':
			result += `\r`
		case '\t':
			result += `\t`
		default:
			result += string(c)
		}
	}
	return result
}
