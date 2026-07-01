// /ide/ui/overlay/types.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package overlay

import "syscall/js"

// types.go — Configuration types for the overlay panel system.
//
// The overlay panel is a draggable floating window that can display:
//   - Form fields (text, number, select, checkbox, file)
//   - Markdown content (rendered with marked.js)
//   - Monaco editor (code viewing/editing)
//
// Panels are configured via a Config struct that can be marshaled to/from JSON.
// Each device in the IDE provides its own Config for the Inspect action.
//
// Português: Tipos de configuração para o sistema de painel overlay.
// O painel é uma janela flutuante arrastável que pode exibir formulários,
// conteúdo markdown e o editor Monaco. Configurado via JSON.

// PlaceholderMarker is the distinctive text that a specialist places inside
// an HTML comment in a help markdown file (e.g. init.en.md) to embed the
// Properties control panel inline within the documentation.
//
// The full tag in the markdown is an HTML comment wrapping this marker:
//
//	<!-- place_the_control_panel_here -->
//
// Detection uses only the inner marker text (not the full comment) so it is
// robust against whitespace variations — e.g. two spaces before "-->", tabs,
// trailing whitespace. Any HTML comment containing this marker text will
// trigger embedded properties mode.
//
// When this marker is detected in any HelpCard content of a TabHelpDeck tab,
// the overlay switches to "embedded properties" mode:
//   - The separate Properties tab is NOT created.
//   - The form fields are rendered inline inside the markdown, at the exact
//     position where the HTML comment appeared.
//   - The Apply button is rendered below the embedded fields.
//
// The tag is invisible in any standard markdown viewer (GitHub, VS Code,
// etc.) because it is an HTML comment — the specialist sees no visual
// clutter while editing.
//
// Usage in init.en.md:
//
//	# Wiring Guide
//	Connect SDA to pin GP4 and SCL to pin GP5.
//	## Configuration
//	<!-- place_the_control_panel_here -->
//	## Testing
//	After configuring, press Run to test the connection.
//
// Português: Texto marcador que o especialista coloca dentro de um
// comentário HTML no markdown para embutir o painel de propriedades.
// A detecção usa apenas o texto interno, não o comentário completo,
// para ser robusta contra variações de espaçamento.
const PlaceholderMarker = "place_the_control_panel_here"

// TabType defines the content type of a tab.
type TabType string

const (
	TabForm     TabType = "form"
	TabMarkdown TabType = "markdown"
	TabMonaco   TabType = "monaco"
	// TabHTML renders the tab's Content as raw HTML, bypassing the
	// Marked markdown renderer. Used by purpose-built overlays like
	// ShowDiagnostics that assemble their own DOM from a structured
	// input rather than authoring markdown by hand.
	//
	// The content is trusted — it must come from Go code, never from
	// the server or user input, because the overlay does NOT sanitize
	// it. If you need to display user-provided text inside a TabHTML
	// content, escape it yourself before passing in.
	//
	// Português: Renderiza Content como HTML puro. Conteúdo deve vir
	// do Go (confiável) — não é sanitizado.
	TabHTML TabType = "html"
	// TabHelpDeck renders a touch-friendly two-level help area.
	//
	// The first level shows a row of large card buttons — one per HelpCard.
	// Cards have a minimum touch target of 44×44px and display the card name
	// and language badge. Tapping a card flips to its Markdown content.
	//
	// This tab type is used for black-box manual pages and can be reused
	// for any component that ships rich documentation in multiple languages.
	//
	// When DiagramURL is set on the tab, any <img> in rendered markdown whose
	// src matches the diagram SVG is replaced with an interactive inline SVG.
	// Active elements are highlighted based on DiagramProps; all others are
	// dimmed. See docs/INTERACTIVE_DIAGRAM_SPEC.md.
	TabHelpDeck TabType = "helpDeck"
)

// Diagnostic is the WASM-side mirror of the server's codegen Diagnostic.
// It carries enough detail to group issues in the diagnostics overlay
// and to drive click-to-focus behavior: each Devices entry is a device
// ID the overlay can ask the camera to enframe via FitAll.
//
// Fields match the server JSON exactly — see server/codegen/diagnostics.
//
// Português: Espelho WASM do Diagnostic do servidor. Contém o
// suficiente pra agrupar os itens no overlay e dar pan+zoom ao
// device clicado.
type Diagnostic struct {
	Kind     string   `json:"kind"`
	Severity string   `json:"severity"` // "error" | "warning"
	Devices  []string `json:"devices,omitempty"`
	Scope    string   `json:"scope,omitempty"`
	Message  string   `json:"message"`
}

// FieldType defines the type of a form field.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldNumber   FieldType = "number"
	FieldSelect   FieldType = "select"
	FieldCheckbox FieldType = "checkbox"
	FieldTextarea FieldType = "textarea"
	FieldColor    FieldType = "color"

	// FieldFile renders a file upload input with an image preview area.
	// The file is read as a data URL (base64) using the FileReader API and
	// stored as a string value in the form's key→value map.
	//
	// When the form is saved, the base64 data URL is available via the
	// field's key, just like any other string field. This allows components
	// to store uploaded images directly in the scene JSON.
	//
	// The Field.Accept attribute controls which file types are selectable
	// (e.g. "image/png,image/svg+xml"). The Field.Value may contain an
	// existing data URL — if so, a preview is shown immediately.
	//
	// Português: Renderiza um input de upload de arquivo com preview.
	// O arquivo é lido como data URL (base64) via FileReader API e
	// armazenado como string no mapa key→value do formulário.
	FieldFile FieldType = "file"

	// FieldMap renders an editable list of key/value rows for the
	// `map[K]V` props introduced in Slice 2.2. Each row has a key
	// input, a value input, and a ✕ button. A "+ Add row" button
	// at the bottom appends a fresh empty row. The empty state
	// shows only the "+ Add row" affordance — no placeholder rows.
	//
	// Storage: Field.Value carries a JSON object on input and is
	// updated to a JSON object on output. Empty / freshly-created
	// fields use "{}". The renderer never produces a malformed
	// JSON: keys with empty strings are kept (the user's choice);
	// duplicate keys are de-duped at save time with a "last wins"
	// rule so the JSON matches Go's map semantics.
	//
	// Field.KeyType and Field.ValueType tell the renderer what
	// kind of input to draw for each column. v1 supports
	// KeyType="string" with ValueType in
	// {string, int, int*, uint*, byte, bool, float*}. Other
	// combinations render as inert (read-only JSON preview)
	// because the form has no useful UI for them.
	FieldMap FieldType = "map"

	// FieldSlice renders an editable list of value rows for the
	// `[]T` props introduced in Slice 2.4. Each row has a value
	// input, ↑/↓ buttons (to reorder), and a ✕ button. A
	// "+ Add row" button at the bottom appends a fresh empty
	// row. The empty state shows only the "+ Add row" affordance.
	//
	// Storage: Field.Value carries a JSON array on input and is
	// updated to a JSON array on output. Empty / freshly-created
	// fields use "[]". Order matters in slices (unlike maps), so
	// the row order in the DOM IS the order of the JSON array;
	// the ↑/↓ buttons let the user move a row without deleting
	// and recreating it.
	//
	// Field.ValueType tells the renderer what input to draw for
	// each row. KeyType is unused. v1 supports ValueType in
	// {string, int, int*, uint*, byte, bool, float*}. Other
	// types render as inert read-only JSON preview.
	FieldSlice FieldType = "slice"

	// FieldCaseEditor is the bespoke editor for a StatementCase device's
	// ordered list of cases — see overlay_case_field.go. It cannot be a
	// FieldSlice (one primitive per row) or FieldMap (key→value) because each
	// case row carries a label, a match-kind select, a values input, a default
	// toggle and reorder controls.
	//
	// Conventions for a Field of this type:
	//   - Value: JSON array of {id,label,matchKind,values,isDefault}; "[]" when
	//     empty. The device's GetInspectConfig serialises its cases here; the
	//     hidden input round-trips back through doSave to ApplyProperties.
	//   - ValueType: the selector type ("int" or "bool"), used to pick the
	//     editable list (int) versus a read-only true/false note (bool).
	//   - Key: the doSave map key the device reads in ApplyProperties.
	//
	// Português: Editor sob medida para a lista de cases de um StatementCase
	// (ver overlay_case_field.go). Value = JSON dos cases; ValueType = tipo do
	// selector ("int"/"bool"); Key = chave lida no ApplyProperties.
	FieldCaseEditor FieldType = "caseEditor"
)

// Config is the top-level configuration for an overlay panel.
// Can be marshaled to/from JSON for declarative configuration.
//
//	{
//	  "title": "ConstInt — Properties",
//	  "width": "480px",
//	  "tabs": [...]
//	}
type Config struct {
	Title  string `json:"title"`            // window title
	Width  string `json:"width,omitempty"`  // CSS width (default: "520px")
	Height string `json:"height,omitempty"` // CSS max-height (default: "80vh")
	Tabs   []Tab  `json:"tabs"`             // content tabs

	// Actions are custom buttons rendered in the title bar between
	// the title text and the maximize/close buttons. Each action
	// shows as a labelled button (with optional FA icon) and runs
	// its OnClick handler when clicked.
	//
	// Designed for "action on the whole overlay content" — e.g.
	// Download all files as ZIP on a multi-file code view, Copy
	// to clipboard on a code view, Export to PDF on a report
	// view. Tab-specific actions belong inside the tab content,
	// not here.
	//
	// Empty slice (the default) renders no extra buttons, keeping
	// the title bar identical to before this field existed —
	// callers that don't need actions are unaffected.
	//
	// Português: Botões customizados na barra de título do
	// overlay, entre o título e os botões de maximizar/fechar.
	// Pensados pra ações que afetam todo o conteúdo do overlay
	// (Download ZIP, copiar tudo, etc.). Vazio = comportamento
	// idêntico ao anterior.
	Actions []Action `json:"actions,omitempty"`

	// OnSave is called when the user clicks Save on a form tab.
	// The map contains field key → string value for all form fields.
	// Not serialized to JSON — set programmatically.
	OnSave func(values map[string]string) `json:"-"`

	// ValidateBeforeSave, when set, runs on Apply BEFORE OnSave, with the
	// collected field values. Returning false BLOCKS the save: OnSave (and the
	// OnSaveReopen close/reopen below) do not run, and the overlay stays open
	// so the user can fix the inputs. Returning true proceeds with the normal
	// save.
	//
	// The hook MAY block — the overlay calls it from a goroutine — so it can
	// run an async round-trip (e.g. a codegen validation pass) without
	// freezing the UI. It owns surfacing WHY it blocked (showing its own error
	// or diagnostics overlay); on a false return the overlay itself shows
	// nothing. nil (the default) keeps the pre-existing behaviour: Apply saves
	// immediately.
	//
	// Not serialized to JSON — set programmatically.
	ValidateBeforeSave func(values map[string]string) bool `json:"-"`

	// OnSaveReopen, when set, is called after Save completes. The overlay
	// automatically closes itself before calling this function. The caller
	// should use this to reopen the inspect panel with updated values.
	//
	// This is the mechanism for the interactive diagram to reflect prop
	// changes: the user changes a dropdown, clicks Save, the overlay
	// closes and reopens with the new values, and the SVG in the Help
	// tab shows the updated connections.
	//
	// Not serialized to JSON — set programmatically.
	OnSaveReopen func() `json:"-"`

	// OnClose is called when the overlay is closed.
	// Not serialized to JSON — set programmatically.
	OnClose func() `json:"-"`
}

// Action is a custom button rendered in the overlay's title bar.
// See Config.Actions for placement rules and intended use.
//
// Label is the text shown inside the button. Icon (optional) is a
// FontAwesome Free icon name (e.g. "download", "copy"); when set,
// it renders to the left of the label using the same fa-solid /
// fa-regular / fa-brands resolution rule the rest of the IDE
// uses (see stagefileui.faIconClass for the canonical priority).
// OnClick fires when the user clicks the button. The overlay
// stays open after the click — handlers that need to close it
// must call the returned Handle's Close themselves.
type Action struct {
	Label   string `json:"label"`
	Icon    string `json:"icon,omitempty"`
	OnClick func() `json:"-"`
}

// Tab represents a single tab in the overlay panel.
type Tab struct {
	Label      string  `json:"label"`                // tab button text
	Type       TabType `json:"type"`                 // "form", "markdown", "monaco", "helpDeck"
	Fields     []Field `json:"fields,omitempty"`     // fields for TabForm
	Content    string  `json:"content,omitempty"`    // text for TabMarkdown or TabMonaco
	ContentURL string  `json:"contentURL,omitempty"` // URL to fetch markdown content from server
	Language   string  `json:"language,omitempty"`   // language for TabMonaco (e.g. "go", "c", "python")
	ReadOnly   bool    `json:"readOnly,omitempty"`   // for TabMonaco: read-only mode
	// HelpCards holds the manual pages for TabHelpDeck.
	// Each entry becomes one large touch-target card. Tapping it reveals the Markdown.
	HelpCards []HelpCard `json:"helpCards,omitempty"`

	// DiagramURL holds the full public URL of the interactive SVG, resolved
	// by the worker when it processes the device's GitHub release ZIP.
	// Example: "/files/devices/owner/repo/rp2040.svg"
	// Set from BlackBoxDefClient.Interactive in statementBlackBoxInit.go.
	//
	// When non-empty, the markdown renderer replaces <img> tags matching
	// this URL with inline SVGs that highlight active diagram elements.
	DiagramURL string `json:"diagramURL,omitempty"`

	// DiagramProps lists the elements to activate on the interactive SVG.
	// Each entry binds a prop's selected value (matched against data-id in
	// the SVG) to a role whose colour comes from the SVG's data-palette.
	// Populated from the device's prop values at inspection time.
	DiagramProps []DiagramProp `json:"diagramProps,omitempty"`

	// EmbeddedFields carries the Properties form fields when the specialist
	// used PlaceholderMarker in the help markdown. When non-nil, the markdown
	// renderer replaces the placeholder with an inline form containing
	// these fields and an Apply button.
	//
	// Only used with TabHelpDeck. The separate Properties tab is omitted
	// when this is set — the fields appear inline in the documentation.
	//
	// Not serialized to JSON — set programmatically by GetInspectConfig().
	EmbeddedFields []Field `json:"-"`

	// EmbeddedOnSave is the save callback for the embedded form. Called
	// when the user clicks Apply on the inline control panel.
	//
	// In practice, this is set by buildTabs() to cfg.OnSave (which may
	// already be wrapped with the OnSaveReopen close-and-reopen logic).
	// This ensures the embedded Apply button triggers the same save cycle
	// as the regular Properties tab's Apply button.
	//
	// Not serialized to JSON — set programmatically.
	EmbeddedOnSave func(map[string]string) `json:"-"`

	// EmbeddedHeader is the translated section header shown above the
	// inline form fields (e.g. "Properties" in English, "Propriedades"
	// in Portuguese). Set by the caller via translate.T() so the overlay
	// package remains decoupled from i18n.
	//
	// Not serialized to JSON — set programmatically.
	EmbeddedHeader string `json:"-"`
}

// HelpCard is one manual page in a TabHelpDeck.
// It maps directly to a ManualPageClient from the black-box definition.
type HelpCard struct {
	// Name is the page identifier shown as the card title (e.g. "wiring-guide").
	Name string `json:"name"`
	// Language is the BCP-47 code shown as a badge (e.g. "en", "pt-br").
	Language string `json:"language"`
	// Content is the raw Markdown rendered when the card is tapped/clicked.
	Content string `json:"content"`
}

// Field represents a single form field.
type Field struct {
	Key         string    `json:"key"`                   // field identifier (used in OnSave map)
	Label       string    `json:"label"`                 // display label
	Type        FieldType `json:"type"`                  // "text", "number", "select", "checkbox", "textarea", "color", "file"
	Value       string    `json:"value,omitempty"`       // current value
	Placeholder string    `json:"placeholder,omitempty"` // input placeholder
	Options     []Option  `json:"options,omitempty"`     // options for FieldSelect
	Min         string    `json:"min,omitempty"`         // min for FieldNumber
	Max         string    `json:"max,omitempty"`         // max for FieldNumber
	Rows        int       `json:"rows,omitempty"`        // rows for FieldTextarea (default: 4)
	ReadOnly    bool      `json:"readOnly,omitempty"`    // makes the field non-editable

	// InputFilter restricts what the user can type into a FieldText input,
	// enforced live (on every input event, including paste). Supported values:
	//
	//   - "identifier" — keeps only [A-Za-z0-9_] and strips leading digits, so
	//     the field is always a valid C/Go identifier or empty. Used by the
	//     variable-name field: a name like "input a" would otherwise produce
	//     invalid source ("float input a = 0.0f;").
	//
	// Empty means no filtering. Ignored for non-text field types.
	//
	// Português: InputFilter restringe o que o usuário pode digitar num
	// FieldText, aplicado ao vivo (todo evento input, inclusive colar).
	// "identifier" mantém só [A-Za-z0-9_] e remove dígitos iniciais, garantindo
	// um identificador C/Go válido (ou vazio). Vazio = sem filtro.
	InputFilter string `json:"inputFilter,omitempty"`

	// Accept restricts selectable file types for FieldFile inputs.
	// Uses the standard HTML accept attribute syntax (e.g. "image/png,image/svg+xml").
	// Ignored for non-file field types.
	//
	// Português: Restringe os tipos de arquivo selecionáveis para inputs FieldFile.
	// Usa a sintaxe padrão do atributo HTML accept.
	Accept string `json:"accept,omitempty"`

	// ConnectionColor, when non-empty, overrides the input border and adds a
	// subtle glow in that colour. Used by prop fields that carry a
	// connection:"ROLE" tag — the colour creates a visual link between the
	// Properties form and the highlighted element in the diagram SVG.
	//
	// Populated with a neutral fallback initially. After the SVG loads in the
	// Help tab, fetchAndInjectSVG() parses the palette and reactively updates
	// every input whose data-connection-role attribute matches a palette key.
	//
	// Value must be a CSS colour string (e.g. "#6b7280"). The text colour is
	// never changed — only the border and a faint box-shadow are affected.
	ConnectionColor string `json:"connectionColor,omitempty"`

	// ConnectionRole is the raw role identifier from the connection:"ROLE" tag
	// (e.g. "I2C_SDA"). Stored here so the form renderer can set a
	// data-connection-role attribute on the input element. After the SVG loads
	// and the palette is parsed, the overlay reactively updates the border
	// colour of every input whose data-connection-role matches a palette key.
	ConnectionRole string `json:"connectionRole,omitempty"`

	// KeyType / ValueType describe the columns of a FieldMap. The
	// renderer uses them to:
	//
	//   - Choose the input element for each column (text vs number vs
	//     checkbox), mapping Go primitive type names ("string", "int",
	//     "bool", "float64", …) to HTML input types.
	//   - Validate live: numeric inputs reject non-digit keystrokes,
	//     bool maps render a real <input type="checkbox">.
	//
	// Currently only KeyType="string" is exercised by the wizard
	// because Go map literals with non-string keys cannot be edited
	// from a text input meaningfully (a key of `[]byte` has no
	// addressable representation). The renderer falls back to inert
	// JSON preview when the (KeyType, ValueType) pair is unsupported.
	//
	// Both fields are ignored when Type != FieldMap.
	KeyType   string `json:"keyType,omitempty"`
	ValueType string `json:"valueType,omitempty"`
}

// DiagramProp describes one active element for the interactive SVG renderer.
// Each entry corresponds to a prop field whose current value selects an
// element on the diagram SVG (matched by data-id attribute).
//
// Colour resolution: the Color field may be empty when the DiagramProp is
// created. The renderer resolves it at runtime from the SVG's data-palette
// attribute using the Role as the lookup key. When the palette is missing
// or the role is not found, a neutral fallback (#6b7280) is used.
type DiagramProp struct {
	// ID is the data-id attribute value to activate in the SVG (e.g. "GP4").
	// This is the prop's current value — what the user selected.
	ID string `json:"id"`

	// Role is the connection role identifier (e.g. "I2C_SDA"). Used as the
	// lookup key in the SVG's data-palette to determine the highlight colour,
	// and converted to a human-readable label via ConnectionRoleLabel().
	Role string `json:"role"`

	// Label is the human-readable badge text shown on the active element
	// (e.g. "I2C SDA"). Derived from Role by replacing underscores with spaces.
	Label string `json:"label"`

	// Color is the CSS hex colour for this role (e.g. "#7c3aed").
	// May be empty at construction time — resolved from the SVG palette at
	// render time by the overlay renderer.
	Color string `json:"color"`
}

// Option represents a select dropdown option.
type Option struct {
	Value string `json:"value"` // option value
	Label string `json:"label"` // display text
}

// Handle allows the caller to interact with an open overlay.
type Handle struct {
	Close func() // closes the overlay

	// Panel is the root DOM element of the overlay window. Callers
	// that need to attach listeners (e.g. the diagnostics overlay's
	// delegated click handler) use this as the listener target.
	//
	// The field is zero-valued when the overlay failed to open —
	// guard with .Truthy() before using.
	//
	// Português: Elemento DOM raiz do painel. Callers que precisam
	// instalar listeners externos usam esse campo como target.
	Panel js.Value
}
