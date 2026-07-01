// live/dialog.go — Live Config dialog for the WASM IDE.
//
// Layout:
//
//	┌─ Live Communication ────────────────────────── ✕ ─┐
//	│ ◉ Connected to a8f3c2e1...                        │
//	│                                                    │
//	│ PROJECT NAME                                       │
//	│ ┌────────────────────────────────────────────────┐ │
//	│ │ My Temperature Sensor                          │ │
//	│ └────────────────────────────────────────────────┘ │
//	│                                [Save]              │
//	│                                                    │
//	│ PROJECT ID                                         │
//	│ ┌────────────────────────────────────────────────┐ │
//	│ │ a8f3c2e1b904d7... (read-only)            [📋] │ │
//	│ └────────────────────────────────────────────────┘ │
//	│ ──────────────────────────────────────────────── │
//	│ API KEYS                              [+ New Key]  │
//	│ ┌ sensor-temp ──────────────────────── [Revoke] ┐  │
//	│                                                    │
//	│                  [Connect]                         │
//	│ ──────────────────────────────────────────────── │
//	│ Help markdown                                      │
//	└────────────────────────────────────────────────────┘
//
// The Project Name is a human-friendly label. The Project ID is a unique
// server-generated identifier used in webhook URLs. Save creates the project
// server-side (POST /api/v1/live/projects) or returns the existing one.
//
// Server-side storage means the project follows the user across machines.
//
// Português:
//
//	O nome do projeto é um label amigável. O ID é gerado pelo servidor e
//	usado nas URLs de webhook. Save cria o projeto no servidor ou retorna
//	o existente. Armazenamento server-side acompanha o usuário entre máquinas.
package live

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"syscall/js"
	"time"

	"github.com/helmutkemper/iotmakerio/rulesServer"
	"github.com/helmutkemper/iotmakerio/translate"
)

const (
	liveDialogID    = "live-config-overlay"
	liveDialogCSSID = "live-config-css"
)

// ─── API response types ───────────────────────────────────────────────────────

type apiKeyItem struct {
	ID        string  `json:"id"`
	DeviceID  string  `json:"device_id"`
	Label     string  `json:"label"`
	RevokedAt *string `json:"revoked_at"`
	CreatedAt string  `json:"created_at"`
}

type createKeyResponse struct {
	ID        string `json:"id"`
	APIKey    string `json:"api_key"`
	ProjectID string `json:"project_id"`
	DeviceID  string `json:"device_id"`
	Label     string `json:"label"`
}

type liveProject struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// selectedProject holds the currently selected project in the dialog.
// Set when the user saves a project name or the dialog loads an existing one.
var selectedProject *liveProject

// ─── Dialog ───────────────────────────────────────────────────────────────────

// ShowConfigDialog opens the Live Config dialog.
// Must be called from a goroutine.
func (c *Client) ShowConfigDialog() {
	doc := js.Global().Get("document")

	if existing := doc.Call("getElementById", liveDialogID); existing.Truthy() {
		existing.Get("parentNode").Call("removeChild", existing)
	}

	c.injectDialogCSS()

	// Status.
	statusIcon := "🔴"
	statusText := translate.T("liveStatusDisconnected", "Disconnected")
	if c.connected {
		statusIcon = "🟢"
		statusText = fmt.Sprintf("%s %s",
			translate.T("liveStatusConnected", "Connected to"),
			c.projectID,
		)
	}

	// Pre-fill project name and ID from selectedProject or projectID.
	projName := ""
	projID := ""
	if selectedProject != nil {
		projName = selectedProject.Name
		projID = selectedProject.ID
	}

	projIDDisplay := translate.T("liveProjectIdHint", "Save a project name to generate the ID")
	if projID != "" {
		projIDDisplay = projID
	}

	html := fmt.Sprintf(`
		<div class="lc-panel" id="lc-panel">
			<div class="lc-titlebar" id="lc-titlebar">
				<span class="lc-titlebar-text">%s</span>
				<div class="lc-titlebar-btns">
					<button class="lc-dot lc-dot-green" id="lc-maximize-btn" title="Maximize"></button>
					<button class="lc-dot lc-dot-red" id="lc-close-btn" title="Close"></button>
				</div>
			</div>

			<div class="lc-status">
				<span class="lc-status-icon">%s</span>
				<span class="lc-status-text">%s</span>
			</div>

			<div class="lc-field">
				<label class="lc-label">%s</label>
				<input type="text" class="lc-input" id="lc-proj-name"
					value="%s" placeholder="%s"
					autocomplete="off" spellcheck="false"/>
			</div>

			<button class="lc-btn-save" id="lc-save-btn">%s</button>

			<div class="lc-field">
				<label class="lc-label">%s</label>
				<div class="lc-id-row">
					<input type="text" class="lc-input lc-id-input" id="lc-proj-id"
						value="%s" readonly onclick="this.select()"/>
					<button class="lc-copy-btn" id="lc-copy-id-btn" title="Copy">📋</button>
				</div>
			</div>

			<div class="lc-divider"></div>

			<div class="lc-keys-section">
				<div class="lc-keys-header">
					<span class="lc-section-title">%s</span>
					<button class="lc-new-key-btn" id="lc-new-key-btn">+ %s</button>
				</div>
				<div id="lc-keys-list" class="lc-keys-list">
					<p class="lc-hint">%s</p>
				</div>
			</div>

			<button class="lc-btn-connect lc-btn-disabled" id="lc-connect-btn" disabled>%s</button>

			<div class="lc-divider"></div>

			<div id="lc-help-section" class="lc-help-section">
				<p class="lc-hint">%s</p>
			</div>
		</div>`,
		translate.T("liveTitle", "Live Communication"),
		statusIcon, statusText,
		translate.T("liveProjectName", "Project Name"),
		escHTML(projName),
		translate.T("liveProjectNamePlaceholder", "My Temperature Sensor"),
		translate.T("liveSave", "Save"),
		translate.T("liveProjectId", "Project ID"),
		escHTML(projIDDisplay),
		translate.T("liveApiKeys", "API Keys"),
		translate.T("liveNewKey", "New Key"),
		translate.T("liveLoadingKeys", "Loading keys..."),
		translate.T("liveConnect", "Connect"),
		translate.T("liveLoadingHelp", "Loading help..."),
	)

	overlay := doc.Call("createElement", "div")
	overlay.Set("id", liveDialogID)
	overlay.Set("className", "lc-overlay")
	overlay.Set("innerHTML", html)
	doc.Get("body").Call("appendChild", overlay)

	nameInput := doc.Call("getElementById", "lc-proj-name")
	idInput := doc.Call("getElementById", "lc-proj-id")

	// ── Save button — creates project server-side ──
	saveBtn := doc.Call("getElementById", "lc-save-btn")
	saveFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		name := strings.TrimSpace(nameInput.Get("value").String())
		if name == "" {
			return nil
		}
		go func() {
			proj, err := c.createOrGetProject(name)
			if err != nil {
				log.Printf("[live] save project error: %v", err)
				return
			}
			selectedProject = proj
			// Update the ID field.
			idEl := doc.Call("getElementById", "lc-proj-id")
			if idEl.Truthy() {
				idEl.Set("value", proj.ID)
			}
			// Save to localStorage as cache for quick reconnect.
			storage := js.Global().Get("localStorage")
			if storage.Truthy() {
				storage.Call("setItem", "liveProjectID", proj.ID)
			}
			// Visual feedback.
			saveBtn.Set("textContent", "✓ "+translate.T("liveSaved", "Saved"))
			js.Global().Call("setTimeout", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				saveBtn.Set("textContent", translate.T("liveSave", "Save"))
				return nil
			}), 1500)
			// Reload keys for this project.
			c.loadKeysList(proj.ID)
		}()
		return nil
	})
	saveBtn.Call("addEventListener", "click", saveFn)

	// Enter in name input → save.
	keyFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("key").String() == "Enter" {
			saveBtn.Call("click")
		}
		return nil
	})
	nameInput.Call("addEventListener", "keydown", keyFn)

	// Copy ID button.
	copyIDBtn := doc.Call("getElementById", "lc-copy-id-btn")
	copyIDFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		idInput.Call("select")
		nav := js.Global().Get("navigator")
		if nav.Truthy() && nav.Get("clipboard").Truthy() {
			nav.Get("clipboard").Call("writeText", idInput.Get("value").String())
			copyIDBtn.Set("textContent", "✓")
			js.Global().Call("setTimeout", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				copyIDBtn.Set("textContent", "📋")
				return nil
			}), 1500)
		}
		return nil
	})
	copyIDBtn.Call("addEventListener", "click", copyIDFn)

	// New Key button.
	newKeyBtn := doc.Call("getElementById", "lc-new-key-btn")
	newKeyFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if selectedProject == nil {
			return nil
		}
		go c.showCreateKeyPopup(selectedProject.ID)
		return nil
	})
	newKeyBtn.Call("addEventListener", "click", newKeyFn)

	// Connect button.
	connectBtn := doc.Call("getElementById", "lc-connect-btn")
	connectFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if selectedProject == nil {
			return nil
		}
		go func() {
			c.SetProjectID(selectedProject.ID)
			time.Sleep(500 * time.Millisecond)
			c.ShowConfigDialog()
		}()
		return nil
	})
	connectBtn.Call("addEventListener", "click", connectFn)

	// Close.
	closeBtn := doc.Call("getElementById", "lc-close-btn")
	closeFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if overlay.Get("parentNode").Truthy() {
			overlay.Get("parentNode").Call("removeChild", overlay)
		}
		saveFn.Release()
		keyFn.Release()
		copyIDFn.Release()
		newKeyFn.Release()
		connectFn.Release()
		return nil
	})
	closeBtn.Call("addEventListener", "click", closeFn)

	// ── Maximize button — toggles between normal and fullscreen ──
	panel := doc.Call("getElementById", "lc-panel")
	maximizeBtn := doc.Call("getElementById", "lc-maximize-btn")
	var isMaximized bool
	var savedPanelCSS string

	maxFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		args[0].Call("stopPropagation")
		if !isMaximized {
			savedPanelCSS = panel.Get("style").Get("cssText").String()
			panel.Get("style").Set("cssText",
				"position:fixed;top:0;left:0;width:100vw;height:100vh;"+
					"background:#1e1e2e;border:none;border-radius:0;"+
					"display:flex;flex-direction:column;overflow-y:auto;"+
					"box-shadow:none;font-family:Arial,sans-serif;"+
					"padding:0 28px 28px;max-width:none;max-height:none;"+
					"scrollbar-width:none;-ms-overflow-style:none;")
			isMaximized = true
		} else {
			panel.Get("style").Set("cssText", savedPanelCSS)
			isMaximized = false
		}
		return nil
	})
	maximizeBtn.Call("addEventListener", "click", maxFn)

	// ── Drag title bar ──
	titlebar := doc.Call("getElementById", "lc-titlebar")
	var dragging bool
	var dragStartX, dragStartY float64
	var panelStartX, panelStartY float64

	mouseDownFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ev := args[0]
		if ev.Get("target").Get("tagName").String() == "BUTTON" {
			return nil
		}
		if isMaximized {
			return nil
		}
		dragging = true
		dragStartX = ev.Get("clientX").Float()
		dragStartY = ev.Get("clientY").Float()
		ev.Call("preventDefault")
		return nil
	})
	titlebar.Call("addEventListener", "mousedown", mouseDownFn)

	mouseMoveFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if !dragging {
			return nil
		}
		ev := args[0]
		dx := ev.Get("clientX").Float() - dragStartX
		dy := ev.Get("clientY").Float() - dragStartY
		panel.Get("style").Set("transform",
			fmt.Sprintf("translate(%.0fpx, %.0fpx)", panelStartX+dx, panelStartY+dy))
		return nil
	})
	doc.Call("addEventListener", "mousemove", mouseMoveFn)

	mouseUpFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if dragging {
			ev := args[0]
			panelStartX += ev.Get("clientX").Float() - dragStartX
			panelStartY += ev.Get("clientY").Float() - dragStartY
			dragging = false
		}
		return nil
	})
	doc.Call("addEventListener", "mouseup", mouseUpFn)

	// ── Escape key closes ──
	escFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("key").String() == "Escape" {
			closeBtn.Call("click")
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", escFn)

	bgFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("target").Get("id").String() == liveDialogID {
			closeBtn.Call("click")
		}
		return nil
	})
	overlay.Call("addEventListener", "click", bgFn)

	nameInput.Call("focus")

	// Async loads.
	if projID != "" {
		go c.loadKeysList(projID)
	} else {
		// Try loading user's projects to auto-select the first one.
		go c.autoSelectProject()
	}
	go c.loadHelpMarkdown()
}

// ─── Project creation ─────────────────────────────────────────────────────────

// createOrGetProject calls POST /api/v1/live/projects to create a project
// or return the existing one with the same name.
func (c *Client) createOrGetProject(name string) (*liveProject, error) {
	body := fmt.Sprintf(`{"name":"%s"}`, escJSON(name))
	url := rulesServer.ServerURL + "/api/v1/live/projects"

	raw, err := doFetchJSON("POST", url, body)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data liveProject `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &resp.Data, nil
}

// autoSelectProject fetches the user's projects and selects the first one.
func (c *Client) autoSelectProject() {
	url := rulesServer.ServerURL + "/api/v1/live/projects"
	raw, err := doFetchJSON("GET", url, "")
	if err != nil {
		return
	}

	var resp struct {
		Data struct {
			Projects []liveProject `json:"projects"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Data.Projects) == 0 {
		return
	}

	// Auto-select the first project.
	proj := resp.Data.Projects[0]
	selectedProject = &proj

	// Update UI.
	doc := js.Global().Get("document")
	nameEl := doc.Call("getElementById", "lc-proj-name")
	if nameEl.Truthy() {
		nameEl.Set("value", proj.Name)
	}
	idEl := doc.Call("getElementById", "lc-proj-id")
	if idEl.Truthy() {
		idEl.Set("value", proj.ID)
	}

	// Load keys for this project.
	c.loadKeysList(proj.ID)
}

// ─── API Key list ─────────────────────────────────────────────────────────────

func (c *Client) loadKeysList(projectID string) {
	doc := js.Global().Get("document")
	container := doc.Call("getElementById", "lc-keys-list")
	if !container.Truthy() {
		return
	}

	if projectID == "" {
		container.Set("innerHTML", fmt.Sprintf(
			`<p class="lc-hint">%s</p>`,
			translate.T("liveKeysNoProject", "Save a project name first."),
		))
		c.updateConnectButton(false)
		return
	}

	url := rulesServer.ServerURL + "/api/v1/live/keys?project_id=" + projectID
	raw, err := doFetchJSON("GET", url, "")
	if err != nil {
		container.Set("innerHTML", fmt.Sprintf(
			`<p class="lc-hint lc-error">%s: %s</p>`,
			translate.T("liveKeysError", "Error loading keys"), err.Error(),
		))
		c.updateConnectButton(false)
		return
	}

	var resp struct {
		Data struct {
			Keys []apiKeyItem `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		container.Set("innerHTML", fmt.Sprintf(
			`<p class="lc-hint lc-error">%s</p>`,
			translate.T("liveKeysParseError", "Failed to parse key list."),
		))
		c.updateConnectButton(false)
		return
	}

	var activeKeys []apiKeyItem
	for _, k := range resp.Data.Keys {
		if k.RevokedAt == nil {
			activeKeys = append(activeKeys, k)
		}
	}

	c.updateConnectButton(len(activeKeys) > 0)

	if len(activeKeys) == 0 {
		container.Set("innerHTML", fmt.Sprintf(
			`<p class="lc-hint">%s</p>`,
			translate.T("liveKeysEmpty", "No active keys. Click [+ New Key] to create one."),
		))
		return
	}

	html := ""
	for _, k := range activeKeys {
		label := k.Label
		if label == "" {
			label = k.ID[:8] + "…" // truncated ID as fallback
		}
		html += fmt.Sprintf(
			`<div class="lc-key-row">
				<span class="lc-key-label">%s</span>
				<button class="lc-key-revoke" data-key-id="%s">%s</button>
			</div>`,
			escHTML(label),
			escHTML(k.ID), translate.T("liveKeyRevoke", "Revoke"),
		)
	}
	container.Set("innerHTML", html)

	revokeFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		target := args[0].Get("target")
		keyID := target.Call("getAttribute", "data-key-id")
		if !keyID.Truthy() {
			return nil
		}
		go c.revokeKey(keyID.String(), projectID)
		return nil
	})
	container.Call("addEventListener", "click", revokeFn)
}

func (c *Client) revokeKey(keyID, projectID string) {
	url := rulesServer.ServerURL + "/api/v1/live/keys/" + keyID
	_, err := doFetchJSON("DELETE", url, "")
	if err != nil {
		log.Printf("[live] revoke key error: %v", err)
		return
	}
	log.Printf("[live] key %s revoked", keyID)
	c.loadKeysList(projectID)
}

func (c *Client) updateConnectButton(hasKeys bool) {
	doc := js.Global().Get("document")
	btn := doc.Call("getElementById", "lc-connect-btn")
	if !btn.Truthy() {
		return
	}
	if hasKeys {
		btn.Set("disabled", false)
		btn.Get("classList").Call("remove", "lc-btn-disabled")
	} else {
		btn.Set("disabled", true)
		btn.Get("classList").Call("add", "lc-btn-disabled")
	}
}

// ─── Create Key popup ─────────────────────────────────────────────────────────

func (c *Client) showCreateKeyPopup(projectID string) {
	doc := js.Global().Get("document")

	if existing := doc.Call("getElementById", "lc-create-popup"); existing.Truthy() {
		existing.Get("parentNode").Call("removeChild", existing)
	}

	popupHTML := fmt.Sprintf(`
		<div class="lc-popup-panel">
			<div class="lc-header">
				<span class="lc-title">%s</span>
				<button class="lc-close" id="lc-popup-close">✕</button>
			</div>
			<div class="lc-field">
				<label class="lc-label">%s</label>
				<input type="text" class="lc-input" id="lc-new-label"
					placeholder="%s" autocomplete="off" spellcheck="false"/>
			</div>
			<button class="lc-btn-save" id="lc-create-key-btn">%s</button>
		</div>`,
		translate.T("liveNewKeyTitle", "New API Key"),
		translate.T("liveKeyLabel", "Key Name"),
		translate.T("liveKeyLabelPlaceholder", "My temperature sensor"),
		translate.T("liveCreateKey", "Create Key"),
	)

	popup := doc.Call("createElement", "div")
	popup.Set("id", "lc-create-popup")
	popup.Set("className", "lc-popup-overlay")
	popup.Set("innerHTML", popupHTML)
	doc.Get("body").Call("appendChild", popup)

	closePopup := func() {
		if popup.Get("parentNode").Truthy() {
			popup.Get("parentNode").Call("removeChild", popup)
		}
	}

	closeBtn := doc.Call("getElementById", "lc-popup-close")
	closeFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		closePopup()
		return nil
	})
	closeBtn.Call("addEventListener", "click", closeFn)

	bgFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("target").Get("id").String() == "lc-create-popup" {
			closePopup()
		}
		return nil
	})
	popup.Call("addEventListener", "click", bgFn)

	createBtn := doc.Call("getElementById", "lc-create-key-btn")
	createFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		label := doc.Call("getElementById", "lc-new-label").Get("value").String()
		if label == "" {
			return nil
		}
		go func() {
			closePopup()
			c.createKeyAndShow(projectID, label)
		}()
		return nil
	})
	createBtn.Call("addEventListener", "click", createFn)

	doc.Call("getElementById", "lc-new-label").Call("focus")
}

func (c *Client) createKeyAndShow(projectID, label string) {
	body := fmt.Sprintf(
		`{"project_id":"%s","label":"%s"}`,
		escJSON(projectID), escJSON(label),
	)

	url := rulesServer.ServerURL + "/api/v1/live/keys"
	raw, err := doFetchJSON("POST", url, body)
	if err != nil {
		log.Printf("[live] create key error: %v", err)
		c.showKeyResultPopup("", err.Error())
		return
	}

	var resp struct {
		Data createKeyResponse `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		log.Printf("[live] parse create key response: %v", err)
		return
	}

	c.showKeyResultPopup(resp.Data.APIKey, "")
	c.loadKeysList(projectID)
}

func (c *Client) showKeyResultPopup(rawKey, errMsg string) {
	doc := js.Global().Get("document")

	if existing := doc.Call("getElementById", "lc-key-result"); existing.Truthy() {
		existing.Get("parentNode").Call("removeChild", existing)
	}

	var content string
	if errMsg != "" {
		content = fmt.Sprintf(
			`<p class="lc-error">%s: %s</p>`,
			translate.T("liveKeyCreateError", "Error creating key"), escHTML(errMsg),
		)
	} else {
		content = fmt.Sprintf(`
			<p class="lc-key-warning">⚠ %s</p>
			<div class="lc-field">
				<label class="lc-label">API Key</label>
				<input type="text" class="lc-input lc-key-display" id="lc-raw-key"
					value="%s" readonly onclick="this.select()"/>
			</div>
			<button class="lc-btn-save" id="lc-copy-key-btn">%s</button>`,
			translate.T("liveKeyWarning", "Save this key now. It will not be shown again."),
			escHTML(rawKey),
			translate.T("liveKeyCopy", "Copy to clipboard"),
		)
	}

	html := fmt.Sprintf(`
		<div class="lc-popup-panel">
			<div class="lc-header">
				<span class="lc-title">%s</span>
				<button class="lc-close" id="lc-result-close">✕</button>
			</div>
			%s
		</div>`,
		translate.T("liveKeyCreated", "API Key Created"),
		content,
	)

	popup := doc.Call("createElement", "div")
	popup.Set("id", "lc-key-result")
	popup.Set("className", "lc-popup-overlay")
	popup.Set("innerHTML", html)
	doc.Get("body").Call("appendChild", popup)

	closePopup := func() {
		if popup.Get("parentNode").Truthy() {
			popup.Get("parentNode").Call("removeChild", popup)
		}
	}

	closeBtn := doc.Call("getElementById", "lc-result-close")
	closeFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		closePopup()
		return nil
	})
	closeBtn.Call("addEventListener", "click", closeFn)

	bgFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Get("target").Get("id").String() == "lc-key-result" {
			closePopup()
		}
		return nil
	})
	popup.Call("addEventListener", "click", bgFn)

	if errMsg == "" {
		copyBtn := doc.Call("getElementById", "lc-copy-key-btn")
		copyFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			keyInput := doc.Call("getElementById", "lc-raw-key")
			keyInput.Call("select")
			nav := js.Global().Get("navigator")
			if nav.Truthy() && nav.Get("clipboard").Truthy() {
				nav.Get("clipboard").Call("writeText", keyInput.Get("value").String())
				copyBtn.Set("textContent", translate.T("liveKeyCopied", "Copied!"))
			}
			return nil
		})
		copyBtn.Call("addEventListener", "click", copyFn)
	}
}

// ─── Help markdown ────────────────────────────────────────────────────────────

func (c *Client) loadHelpMarkdown() {
	doc := js.Global().Get("document")
	container := doc.Call("getElementById", "lc-help-section")
	if !container.Truthy() {
		return
	}

	locale := resolveHelpLocale()
	base := rulesServer.ServerURL + "/help/live/settings"

	// Normalise English variants: "en-us", "en-gb" → "en".
	normalised := locale
	if strings.HasPrefix(normalised, "en") {
		normalised = "en"
	}

	md := fetchTextSync(base + "." + normalised + ".md")

	if md == "" && normalised != "en" && strings.Contains(normalised, "-") {
		lang := normalised[:strings.Index(normalised, "-")]
		md = fetchTextSync(base + "." + lang + ".md")
	}

	if md == "" && normalised != "en" {
		md = fetchTextSync(base + ".en.md")
	}

	if md == "" {
		container.Set("innerHTML", "")
		return
	}

	// Replace {project_id} placeholder with the actual server-generated ID.
	// {device_id} and {your_key} remain as placeholders — the user fills them in.
	if selectedProject != nil {
		md = strings.ReplaceAll(md, "{project_id}", selectedProject.ID)
	}

	html := md
	marked := js.Global().Get("marked")
	if marked.Truthy() {
		result := marked.Call("parse", md)
		if result.Truthy() {
			html = result.String()
		}
	}

	container.Set("innerHTML", `<div class="lc-help-body">`+html+`</div>`)

	// Syntax-highlight code blocks using Monaco's colorize API.
	// Monaco is preloaded by the overlay package during splash screen.
	go colorizeCodeBlocks(doc, container)
}

// colorizeCodeBlocks scans the container for <code> elements with a language-*
// class and replaces their content with Monaco-colorized HTML.
// Must run in a goroutine — each colorize call returns a JS Promise.
func colorizeCodeBlocks(doc js.Value, container js.Value) {
	monaco := js.Global().Get("monaco")
	if !monaco.Truthy() {
		return
	}

	// Set dark theme so colorized code blocks match the dialog background.
	monaco.Get("editor").Call("setTheme", "vs-dark")

	codeEls := container.Call("querySelectorAll", "pre code[class*='language-']")
	length := codeEls.Get("length").Int()

	for i := 0; i < length; i++ {
		codeEl := codeEls.Call("item", i)
		classList := codeEl.Get("className").String()

		// Extract language from class name (e.g. "language-bash" → "bash").
		lang := ""
		for _, cls := range strings.Split(classList, " ") {
			if strings.HasPrefix(cls, "language-") {
				lang = strings.TrimPrefix(cls, "language-")
				break
			}
		}
		if lang == "" {
			continue
		}

		// Map common aliases to Monaco language IDs.
		switch lang {
		case "bash", "sh", "shell":
			lang = "shell"
		case "golang":
			lang = "go"
		}

		text := codeEl.Get("textContent").String()

		// colorize returns a Promise<string> with HTML.
		type result struct{ html string }
		ch := make(chan result, 1)

		thenFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args[0].Truthy() {
				ch <- result{html: args[0].String()}
			} else {
				ch <- result{}
			}
			return nil
		})
		catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			ch <- result{}
			return nil
		})

		monaco.Get("editor").Call("colorize", text, lang, js.ValueOf(map[string]interface{}{})).
			Call("then", thenFn).
			Call("catch", catchFn)

		res := <-ch
		thenFn.Release()
		catchFn.Release()

		if res.html != "" {
			codeEl.Set("innerHTML", res.html)
			// Add a class so CSS can style Monaco-colorized blocks differently.
			codeEl.Get("classList").Call("add", "lc-colorized")
		}
	}
}

func resolveHelpLocale() string {
	storage := js.Global().Get("localStorage")
	if storage.Truthy() {
		saved := storage.Call("getItem", "locale")
		if saved.Truthy() {
			if s := saved.String(); s != "" {
				return strings.ToLower(s)
			}
		}
	}
	nav := js.Global().Get("navigator")
	if nav.Truthy() {
		lang := nav.Get("language")
		if lang.Truthy() {
			if s := lang.String(); s != "" {
				return strings.ToLower(s)
			}
		}
	}
	return "en"
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func doFetchJSON(method, url, jsonBody string) ([]byte, error) {
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
		return args[0].Call("json")
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
		return nil, fmt.Errorf("%s", res.err)
	}
	return res.raw, nil
}

func fetchTextSync(url string) string {
	type result struct {
		body string
	}
	ch := make(chan result, 1)

	thenResp := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resp := args[0]
		if !resp.Get("ok").Bool() {
			return js.ValueOf("")
		}
		return resp.Call("text")
	})
	thenText := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		v := args[0]
		if v.IsNull() || v.IsUndefined() {
			ch <- result{}
			return nil
		}
		ch <- result{body: v.String()}
		return nil
	})
	catchFn := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		ch <- result{}
		return nil
	})

	js.Global().Call("fetch", url).
		Call("then", thenResp).
		Call("then", thenText).
		Call("catch", catchFn)

	res := <-ch
	thenResp.Release()
	thenText.Release()
	catchFn.Release()

	return res.body
}

// ─── String helpers ───────────────────────────────────────────────────────────

func escHTML(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			out = append(out, '&', 'a', 'm', 'p', ';')
		case '<':
			out = append(out, '&', 'l', 't', ';')
		case '>':
			out = append(out, '&', 'g', 't', ';')
		case '"':
			out = append(out, '&', 'q', 'u', 'o', 't', ';')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

func escJSON(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}

// ─── CSS ──────────────────────────────────────────────────────────────────────

func (c *Client) injectDialogCSS() {
	doc := js.Global().Get("document")
	if existing := doc.Call("getElementById", liveDialogCSSID); existing.Truthy() {
		return
	}

	css := `
.lc-overlay, .lc-popup-overlay {
	position: fixed; inset: 0; z-index: 9000;
	background: rgba(0,0,0,0.6);
	display: flex; align-items: center; justify-content: center;
	animation: lc-fade 0.15s ease;
}
.lc-popup-overlay { z-index: 9500; }
@keyframes lc-fade { from { opacity: 0; } to { opacity: 1; } }
.lc-panel, .lc-popup-panel {
	background: #1e1e2e; border: 1px solid #2a2a40; border-radius: 12px;
	padding: 28px; width: 480px; max-width: 90vw; max-height: 85vh;
	display: flex; flex-direction: column; gap: 16px;
	box-shadow: 0 8px 32px rgba(0,0,0,0.6);
	font-family: Arial, sans-serif; overflow-y: auto;
	scrollbar-width: none; -ms-overflow-style: none;
}
.lc-panel::-webkit-scrollbar, .lc-popup-panel::-webkit-scrollbar { display: none; }
/* ── Title bar (macOS-style, matches inspect panel overlay) ── */
.lc-titlebar {
	height: 30px; min-height: 30px; flex-shrink: 0;
	background: #313244; border-bottom: 1px solid #45475a;
	border-radius: 12px 12px 0 0;
	display: flex; align-items: center; justify-content: space-between;
	padding: 0 20px; cursor: move; user-select: none; -webkit-user-select: none;
	margin: -28px -28px 0;
	width: calc(100% + 56px); box-sizing: border-box;
}
.lc-titlebar-text {
	color: #cdd6f4; font-size: 11px; font-weight: 600; flex: 1;
	white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.lc-titlebar-btns { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.lc-dot {
	width: 14px; height: 14px; border-radius: 50%; border: none;
	cursor: pointer; padding: 0; transition: opacity 0.15s; opacity: 0.8;
}
.lc-dot:hover { opacity: 1; }
.lc-dot-green { background: #a6e3a1; }
.lc-dot-red { background: #f38ba8; }
/* ── Popup headers (keep simple ✕ style) ── */
.lc-header { display: flex; justify-content: space-between; align-items: center; flex-shrink: 0; }
.lc-title { color: #6c8eff; font-size: 16px; font-weight: 600; }
.lc-close {
	background: none; border: none; color: #555; font-size: 18px;
	cursor: pointer; padding: 2px 6px; border-radius: 4px;
}
.lc-close:hover { color: #fff; background: rgba(255,80,80,0.2); }
.lc-status { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.lc-status-icon { font-size: 14px; }
.lc-status-text { color: #aaa; font-size: 13px; }
.lc-field { display: flex; flex-direction: column; gap: 6px; }
.lc-label { color: #888; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px; }
.lc-input {
	background: #12121e; border: 1px solid #2a2a40; border-radius: 6px;
	color: #ddd; font-size: 15px; padding: 10px 12px; outline: none;
	font-family: monospace; transition: border-color 0.15s; box-sizing: border-box;
}
.lc-input:focus { border-color: #6c8eff; }
.lc-id-row { display: flex; gap: 6px; align-items: stretch; }
.lc-id-input { flex: 1; color: #6c8eff; cursor: text; font-size: 13px; }
.lc-copy-btn {
	background: #2a2a40; border: 1px solid #3a3a5a; border-radius: 6px;
	color: #aaa; font-size: 16px; padding: 8px 12px; cursor: pointer;
	transition: background 0.15s;
}
.lc-copy-btn:hover { background: #3a3a5a; }
.lc-hint { color: #555; font-size: 11px; margin: 0; }
.lc-error { color: #cc4444; font-size: 12px; margin: 0; }
.lc-btn-save {
	background: #6c8eff; color: #fff; border: none; border-radius: 8px;
	padding: 12px; font-size: 15px; font-weight: 500; cursor: pointer;
	transition: background 0.15s; flex-shrink: 0;
}
.lc-btn-save:hover { background: #5575ee; }
.lc-btn-connect {
	background: #44aa44; color: #fff; border: none; border-radius: 8px;
	padding: 12px; font-size: 15px; font-weight: 500; cursor: pointer;
	transition: background 0.15s, opacity 0.15s; flex-shrink: 0;
}
.lc-btn-connect:hover { background: #339933; }
.lc-btn-disabled { opacity: 0.35; cursor: not-allowed; }
.lc-btn-disabled:hover { background: #44aa44; }
.lc-divider { border-top: 1px solid #2a2a40; flex-shrink: 0; }
.lc-keys-section { display: flex; flex-direction: column; gap: 10px; }
.lc-keys-header { display: flex; justify-content: space-between; align-items: center; }
.lc-section-title { color: #888; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px; }
.lc-new-key-btn {
	background: #2a2a40; color: #6c8eff; border: 1px solid #3a3a5a; border-radius: 6px;
	padding: 5px 12px; font-size: 12px; cursor: pointer; font-family: Arial, sans-serif;
	transition: background 0.15s;
}
.lc-new-key-btn:hover { background: #3a3a5a; }
.lc-keys-list { display: flex; flex-direction: column; gap: 6px; }
.lc-key-row {
	display: flex; justify-content: space-between; align-items: center;
	background: #12121e; border: 1px solid #2a2a40; border-radius: 6px;
	padding: 8px 12px;
}
.lc-key-label { color: #bbb; font-size: 13px; font-family: monospace; flex: 1; overflow: hidden; text-overflow: ellipsis; }
.lc-key-revoke {
	background: none; border: 1px solid #553333; border-radius: 4px;
	color: #cc6666; font-size: 11px; padding: 3px 8px; cursor: pointer;
	transition: background 0.15s, color 0.15s; flex-shrink: 0; margin-left: 8px;
}
.lc-key-revoke:hover { background: rgba(204,68,68,0.15); color: #ff6666; }
.lc-key-warning { color: #CCAA44; font-size: 13px; margin: 0; font-weight: 600; }
.lc-key-display { font-size: 13px; cursor: text; color: #6c8eff; letter-spacing: 0.5px; }
.lc-help-section { min-height: 0; }
.lc-help-body { color: #aaa; font-size: 13px; line-height: 1.7; }
.lc-help-body h1, .lc-help-body h2 { color: #ddd; font-size: 14px; margin: 10px 0 6px; }
.lc-help-body h3 { color: #bbb; font-size: 13px; margin: 8px 0 4px; }
.lc-help-body p { margin: 0 0 8px; }
.lc-help-body code { background: #2a2a40; padding: 1px 5px; border-radius: 3px; font-size: 12px; color: #e0e0ff; }
.lc-help-body pre { background: #12121e; padding: 10px; border-radius: 6px; overflow-x: auto; margin: 6px 0; font-size: 11px; }
.lc-help-body pre code { background: none; padding: 0; }
/* Monaco-colorized code blocks get a slightly different style */
.lc-help-body code.lc-colorized { background: none; padding: 0; font-size: 12px; line-height: 1.6; }
.lc-help-body table { border-collapse: collapse; width: 100%; margin: 6px 0; }
.lc-help-body th { background: #2a2a40; color: #ddd; padding: 5px 8px; text-align: left; font-size: 12px; }
.lc-help-body td { padding: 4px 8px; border-bottom: 1px solid #2a2a40; font-size: 12px; }
.lc-help-body strong { color: #ddd; }
`
	style := doc.Call("createElement", "style")
	style.Set("id", liveDialogCSSID)
	style.Set("textContent", css)
	doc.Get("head").Call("appendChild", style)
}
