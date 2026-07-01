# /ide/docs/MIGRATION_PIN_TO_CONNECTION.md

# Migration Plan: `pin` → `connection` Tag System

> This document lists every file that needs to change and exactly what changes.
> Use it as a checklist during implementation. Each section is one atomic step
> that can be implemented and tested independently.

---

## Summary of Changes

| Category        | What changes                          | Impact                                         |
|-----------------|---------------------------------------|------------------------------------------------|
| Go struct tag   | `pin:"ROLE"` → `connection:"ROLE"`    | Server parser, client types                    |
| Go struct tag   | `color:"#hex"` → **removed**          | Server parser, client types, overlay           |
| CSS class       | `pin-group` → `conn-group`            | SVG files, overlay renderer                    |
| CSS class       | `t-role-bg` → `conn-role-bg`          | SVG files, overlay CSS                         |
| CSS class       | `t-role` → `conn-role`                | SVG files, overlay renderer                    |
| CSS class       | `t-num` → `conn-num`                  | SVG files, overlay CSS                         |
| data attribute  | `data-pin` → `data-id`                | SVG files, overlay renderer                    |
| SVG attribute   | (new) `data-palette` on `<svg>` root  | SVG files, overlay renderer                    |
| Tab type        | `TabPinout` → **removed**             | Overlay types, overlay renderer, blackbox init |
| Overlay feature | (new) Fullscreen lightbox for images  | Overlay renderer                               |
| Overlay feature | Palette parsing from SVG              | Overlay renderer                               |
| Colour source   | `PinColor` struct field → SVG palette | Client types, overlay, blackbox init           |

---

## Step 1 — Server: Parser and Types

### File: `server/codegen/blackbox/types.go`

**PropDef struct** — rename fields, remove PinColor:

```go
// BEFORE
PinRole  string `json:"pinRole,omitempty"`
PinColor string `json:"pinColor,omitempty"`

// AFTER
Connection string `json:"connection,omitempty"`
// PinColor removed entirely
```

Update all comments referencing `pin:` to say `connection:`.

### File: `server/codegen/blackbox/parser.go`

**extractProps function** — parse `connection:` instead of `pin:`, drop `color:`:

```go
// BEFORE
PinRole:  tag.Get("pin"),
PinColor: tag.Get("color"),

// AFTER
Connection: tag.Get("connection"),
// color line removed
```

Update warning messages that reference `pin:` to say `connection:`.

### File: `server/handler/blackboxapi/handler.go`

**PropDefResp struct** — rename fields:

```go
// BEFORE
PinRole  string `json:"pinRole,omitempty"`
PinColor string `json:"pinColor,omitempty"`

// AFTER
Connection string `json:"connection,omitempty"`
```

**Handler mapping** — update field assignment:

```go
// BEFORE
PinRole:  p.PinRole,
PinColor: p.PinColor,

// AFTER
Connection: p.Connection,
```

---

## Step 2 — WASM Client: Type Definitions

### File: `blackbox/clientTypes.go`

**PropDefClient struct** — rename fields:

```go
// BEFORE
PinRole  string `json:"pinRole,omitempty"`
PinColor string `json:"pinColor,omitempty"`

// AFTER
Connection string `json:"connection,omitempty"`
```

Update all doc comments.

### File: `ui/overlay/types.go`

**Remove** `TabPinout` constant.

**Rename** `PinoutProp` → `DiagramProp`:

```go
// BEFORE
type PinoutProp struct {
    Pin   string `json:"pin"`
    Role  string `json:"role"`
    Label string `json:"label"`
    Color string `json:"color"`
}

// AFTER
type DiagramProp struct {
    ID    string `json:"id"`    // matches data-id in SVG
    Role  string `json:"role"`  // connection:"ROLE" value
    Label string `json:"label"` // human-readable: "I2C SDA"
    Color string `json:"color"` // resolved from SVG palette at runtime
}
```

**Rename** Tab fields:

```go
// BEFORE
PinoutBoard string       `json:"pinoutBoard,omitempty"`
PinoutProps []PinoutProp  `json:"pinoutProps,omitempty"`

// AFTER
DiagramURL   string        `json:"diagramURL,omitempty"`
DiagramProps []DiagramProp  `json:"diagramProps,omitempty"`
```

**Rename** Field.PinColor:

```go
// BEFORE
PinColor string `json:"pinColor,omitempty"`

// AFTER
ConnectionColor string `json:"connectionColor,omitempty"`
```

Note: ConnectionColor is no longer set from the Go struct. It is populated
at runtime by reading the SVG palette. This field remains on the Field struct
so that the form renderer can apply the border accent. The value is set in
`statementBlackBoxInit.go` after the palette is parsed.

**Alternative approach (simpler):** Remove `ConnectionColor` from Field entirely.
Instead, apply the border colour via JS after the SVG is loaded and the palette
is parsed. This avoids having to parse the SVG before building the form. The
downside: input borders won't have colour until the SVG loads (minor flash).

**Recommended:** Keep `ConnectionColor` on Field but populate it with a fallback
colour initially (neutral slate `#6b7280`). After the SVG palette loads, update
the input borders reactively. This gives instant visual feedback and the correct
colour after load.

---

## Step 3 — WASM Client: Overlay Renderer

### File: `ui/overlay/overlay.go`

This is the largest change. Split into sub-tasks:

#### 3a. Remove `renderPinout()` and `TabPinout` case

Delete the entire `renderPinout()` function (~110 lines).

Remove the `case TabPinout:` branch in the tab render switch.

#### 3b. Rename CSS constants

```go
// BEFORE
const pinoutInspectorCSS = `
.active .pad        { fill: var(--rc) !important; stroke: var(--rc) !important; }
.active .t-role-bg  { fill: var(--rc) !important; visibility: visible !important; }
.active .t-role     { visibility: visible !important; }
...
.dimmed .t-num      { opacity: 0.25 !important; }
`

// AFTER
const diagramInspectorCSS = `
.active .pad            { fill: var(--rc) !important; stroke: var(--rc) !important; }
.active .conn-role-bg   { fill: var(--rc) !important; visibility: visible !important; }
.active .conn-role      { visibility: visible !important; }
.active .readme-badges  { display: none !important; }
.dimmed .pad            { opacity: 0.18 !important; }
.dimmed .readme-badges  { opacity: 0.18 !important; }
.dimmed .conn-num       { opacity: 0.25 !important; }
`
```

#### 3c. Add palette parsing

New function:

```go
// parsePalette extracts the role→colour mapping from the data-palette
// attribute on an SVG element.
//
// Format: "ROLE1:#hex1, ROLE2:#hex2, ..."
// Returns an empty map when the attribute is missing or malformed.
func parsePalette(svgEl js.Value) map[string]string {
    raw := svgEl.Call("getAttribute", "data-palette")
    if raw.IsNull() || raw.IsUndefined() || raw.String() == "" {
        return nil
    }
    // ... parse comma-separated pairs, uppercase keys, trim whitespace
}
```

#### 3d. Update `activateInlineSVGs()` and `fetchAndInjectSVG()`

Change all selectors:
- `".pin-group"` → `".conn-group"`
- `[data-pin="..."]` → `[data-id="..."]`
- `".t-role"` → `".conn-role"`

Change CSS injection:
- `pinoutInspectorCSS` → `diagramInspectorCSS`

Add palette-based colour resolution:
- After injecting SVG inline, call `parsePalette(svgEl)`
- For each DiagramProp, look up colour from palette instead of using pre-computed colour
- If prop colour was a fallback, update it from palette

Change variable names:
- `pinoutBoard` → `diagramURL`
- `pinoutProps` → `diagramProps`
- `PinoutProp` → `DiagramProp`

#### 3e. Refactor colour functions

```go
// BEFORE
func PinRoleColor(role string) string { ... }
func PinRoleLabel(role string) string { ... }
func EffectivePinColor(pinColor, pinRole string) string { ... }

// AFTER
// ConnectionRoleColor is a built-in fallback that returns a neutral colour.
// The SVG palette is the primary source; this function is used only when
// no SVG is available or the role is not in the palette.
func ConnectionRoleFallbackColor() string {
    return "#6b7280" // neutral slate
}

// ConnectionRoleLabel converts a role identifier to a human-readable label.
// Replaces underscores with spaces: "I2C_SDA" → "I2C SDA"
func ConnectionRoleLabel(role string) string {
    return strings.ReplaceAll(role, "_", " ")
}

// Note: EffectivePinColor is removed. Colour resolution happens at runtime
// from the SVG palette via parsePalette().
```

#### 3f. Add fullscreen lightbox for images

New function:

```go
// addImageLightbox wraps clickable images in the rendered markdown content.
// Clicking any image opens a fullscreen overlay showing the image at
// maximum viewport size. For inline SVGs, the lightbox preserves the
// current inspector-mode state (active/dimmed classes).
//
// Close: click backdrop, click × button, or press Escape.
func addImageLightbox(doc js.Value, container js.Value) { ... }
```

Call this after `renderMD()` completes, after `activateInlineSVGs()`.

---

## Step 4 — WASM Client: BlackBox Init Statement

### File: `devices/compBlackBox/statementBlackBoxInit.go`

#### 4a. Form field colour

```go
// BEFORE
if p.PinRole != "" || p.PinColor != "" {
    field.PinColor = overlay.EffectivePinColor(p.PinColor, p.PinRole)
}

// AFTER
if p.Connection != "" {
    // Use a fallback colour initially. The actual colour from the SVG
    // palette will be applied after the SVG loads in the Help tab.
    field.ConnectionColor = overlay.ConnectionRoleFallbackColor()
}
```

#### 4b. Diagram prop construction

```go
// BEFORE
var pinoutProps []overlay.PinoutProp
if e.def.Interactive != "" {
    for _, p := range e.def.Props {
        if p.PinRole == "" { continue }
        val := e.propValues[p.FieldName]
        if val == "" { val = p.Default }
        if val == "" { continue }
        pinoutProps = append(pinoutProps, overlay.PinoutProp{
            Pin:   val,
            Role:  p.PinRole,
            Label: overlay.PinRoleLabel(p.PinRole),
            Color: overlay.EffectivePinColor(p.PinColor, p.PinRole),
        })
    }
}

// AFTER
var diagramProps []overlay.DiagramProp
if e.def.Interactive != "" {
    for _, p := range e.def.Props {
        if p.Connection == "" { continue }
        val := e.propValues[p.FieldName]
        if val == "" { val = p.Default }
        if val == "" { continue }
        diagramProps = append(diagramProps, overlay.DiagramProp{
            ID:    val,
            Role:  p.Connection,
            Label: overlay.ConnectionRoleLabel(p.Connection),
            Color: "", // resolved from SVG palette at render time
        })
    }
}
```

#### 4c. Tab construction

```go
// BEFORE
PinoutBoard: e.def.Interactive,
PinoutProps: pinoutProps,

// AFTER
DiagramURL:   e.def.Interactive,
DiagramProps: diagramProps,
```

---

## Step 5 — SVG Assets

Create new SVG files following the new spec (using `conn-group`, `data-id`,
`data-palette`, etc.).

The existing HTML demo (`pico-v3-demo.html`) serves as a visual reference but
is **not** a production SVG — it uses the old class names and must not be used
as-is.

---

## Implementation Order

Recommended order to minimise broken states:

1. **Server types + parser** (Step 1) — changes JSON output
2. **Client types** (Step 2) — matches new JSON
3. **Overlay renderer** (Step 3) — uses new types
4. **BlackBox Init statement** (Step 4) — wires everything together
5. **New SVG assets** (Step 5) — provides test content

Steps 1–2 can be deployed together (JSON schema change).
Steps 3–4 can be deployed together (renderer + wiring change).
Step 5 is independent (content, not code).

---

## Files Changed Summary

```
server/codegen/blackbox/types.go        ← PropDef: PinRole→Connection, remove PinColor
server/codegen/blackbox/parser.go       ← parse connection: instead of pin:, drop color:
server/handler/blackboxapi/handler.go   ← response struct: pinRole→connection, drop pinColor

blackbox/clientTypes.go                 ← PropDefClient: same renames
ui/overlay/types.go                     ← remove TabPinout, rename PinoutProp→DiagramProp
ui/overlay/overlay.go                   ← major: remove renderPinout, update CSS/selectors,
                                           add palette parsing, add lightbox, refactor colours
devices/compBlackBox/statementBlackBoxInit.go ← use new field names, new prop construction

docs/INTERACTIVE_DIAGRAM_SPEC.md        ← NEW: SVG format specification
docs/DIAGRAM_CREATION_GUIDE.md          ← NEW: practical creation guide
docs/MIGRATION_PIN_TO_CONNECTION.md     ← NEW: this document
```
