// ide/stagePrefsClient/client.go — HTTP client for the stage preferences API.
//
// The WASM IDE reads the user's stage preferences at workspace startup
// and applies them to the sprite camera. Writes happen in the portal
// UI (Editor Settings → Stage tab), never here — the IDE is read-only
// for stage prefs.
//
// English:
//
//	Blocking fetch following the same pattern as stagefileclient.
//	LoadStagePrefs must be called from a goroutine because it blocks
//	on a channel until the JavaScript fetch Promise resolves.
//
//	The server responds with both the user's resolved prefs AND the
//	server-authoritative defaults. Callers can inspect the defaults
//	without duplicating the values in the WASM binary — if a future
//	server tunes the default, the IDE picks it up on next load.
//
// Português:
//
//	Cliente de leitura das preferências da stage. A escrita vive na
//	página Editor Settings do portal. LoadStagePrefs bloqueia até a
//	fetch resolver e deve ser chamado de dentro de uma goroutine.
//
//	O servidor responde com prefs do usuário + defaults autoritativos.
//	Se um default mudar no servidor no futuro, o IDE pega na próxima
//	abertura sem precisar rebuildar o binário.
package stagePrefsClient

import (
	"encoding/json"
	"fmt"
	"log"
	"syscall/js"

	"github.com/helmutkemper/iotmakerio/rulesServer"
)

// StagePrefs mirrors the server's store.StagePrefs shape. Every field
// carries a concrete value — the server merges NULL columns with
// defaults before sending, so the IDE never has to handle "unset".
//
// Português: Preferências resolvidas — cada campo já tem valor
// concreto. O servidor faz o merge de colunas NULL com defaults
// antes de responder.
type StagePrefs struct {
	// ZoomStep: increment per wheel notch. Tuned for trackpad
	// comfort around 0.03; goes higher for traditional wheel mice.
	ZoomStep float64 `json:"zoomStep"`

	// PanEmptyArea: left-click+drag on an empty spot of the stage
	// triggers a camera pan. Desktop mouse only — touch devices
	// ignore this.
	PanEmptyArea bool `json:"panEmptyArea"`

	// ShowGrabCursor: when true, hovering empty area with the
	// mouse shows a grab cursor hint. Requires PanEmptyArea to be
	// meaningful; otherwise the hint would be misleading.
	ShowGrabCursor bool `json:"showGrabCursor"`
}

// LoadResult is what LoadStagePrefs returns. Bundles the resolved
// user prefs with the server-reported defaults so callers can show
// "reset" UI without hardcoded fallbacks.
//
// Português: Resultado do LoadStagePrefs. Empacota prefs do usuário
// com os defaults do servidor para que o IDE possa mostrar UI de
// "voltar ao padrão" sem hardcode.
type LoadResult struct {
	Prefs    StagePrefs `json:"prefs"`
	Defaults StagePrefs `json:"defaults"`
}

// LoadStagePrefs performs a blocking GET of the user's stage
// preferences. Call from a goroutine.
//
// On any failure (network error, 401, server down) the function
// returns (nil, error). Callers should treat that as "use the
// compile-time defaults" — the IDE must never fail to open just
// because the prefs endpoint is unreachable.
//
// Português: GET bloqueante das preferências da stage. Chamar de
// goroutine. Qualquer falha retorna (nil, err); caller deve usar
// defaults compile-time — o IDE não pode ficar sem abrir só porque
// o servidor de prefs está fora.
func LoadStagePrefs() (*LoadResult, error) {
	url := rulesServer.ServerURL + rulesServer.EndpointStagePrefs

	raw, err := doFetch("GET", url, "")
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Data LoadResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse stage prefs response: %w", err)
	}
	return &envelope.Data, nil
}

// ─── Internal fetch helper ────────────────────────────────────────────────────

// doFetch is a near-copy of stagefileclient.doFetch, kept local to
// avoid a cross-package dependency for a package this small. If a
// third WASM client appears, consider extracting to a shared helper
// (e.g. ide/httpClient/); for two packages the duplication is
// cheaper than the abstraction.
//
// Português: Cópia local do doFetch de stagefileclient para evitar
// dependência entre pacotes pequenos. Se um terceiro cliente
// aparecer, vale extrair para ide/httpClient/.
func doFetch(method, url, jsonBody string) ([]byte, error) {
	token := rulesServer.GetAuthToken()

	type result struct {
		raw []byte
		err string
	}
	ch := make(chan result, 1)

	opts := js.Global().Get("Object").New()
	opts.Set("method", method)

	headers := js.Global().Get("Object").New()
	headers.Set("Content-Type", "application/json")
	if token != "" {
		headers.Set("Authorization", token)
	}
	opts.Set("headers", headers)

	if jsonBody != "" && method != "GET" {
		opts.Set("body", jsonBody)
	}

	thenResponse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		// Both success and error paths call json() — the envelope
		// parser below distinguishes them via metadata.status.
		return resp.Call("json")
	})

	thenParse := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].IsNull() || args[0].IsUndefined() {
			ch <- result{err: "server returned null body"}
			return nil
		}
		raw := js.Global().Get("JSON").Call("stringify", args[0]).String()

		var envelope struct {
			Metadata struct {
				Status int    `json:"status"`
				Error  string `json:"error"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err == nil {
			if envelope.Metadata.Status >= 400 {
				errMsg := envelope.Metadata.Error
				if errMsg == "" {
					errMsg = fmt.Sprintf("HTTP %d", envelope.Metadata.Status)
				}
				ch <- result{err: errMsg}
				return nil
			}
		}

		ch <- result{raw: []byte(raw)}
		return nil
	})

	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		msg := "network error"
		if len(args) > 0 && args[0].Get("message").Truthy() {
			msg = args[0].Get("message").String()
		}
		ch <- result{err: msg}
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

	if res.err != "" {
		log.Printf("[stagePrefsClient] %s %s → error: %s", method, url, res.err)
		return nil, fmt.Errorf("%s", res.err)
	}

	log.Printf("[stagePrefsClient] %s %s → %d bytes", method, url, len(res.raw))
	return res.raw, nil
}
