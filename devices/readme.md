# Creating a New Backend Device

## Guide for Developers

This document teaches you how to create a new backend device for the IDE, using
`StatementAdd` as the reference implementation. By the end, you will have a fully
functional device with SVG rendering, wire connections, hex menu, property panel,
label, help page, and code generation support.

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [File Structure](#2-file-structure)
3. [Step 1: The Struct](#3-step-1-the-struct)
4. [Step 2: SVG Drawing (Ornament)](#4-step-2-svg-drawing-ornament)
5. [Step 3: Init — Assembling the Device](#5-step-3-init--assembling-the-device)
6. [Step 4: Wire Connections](#6-step-4-wire-connections)
7. [Step 5: Hex Menu](#7-step-5-hex-menu)
8. [Step 6: The Property Panel (Inspect Overlay)](#8-step-6-the-property-panel-inspect-overlay)
9. [Step 7: The Editable Label](#9-step-7-the-editable-label)
10. [Step 8: Scene Serialization](#10-step-8-scene-serialization)
11. [Step 9: The Icon (Palette)](#11-step-9-the-icon-palette)
12. [Step 10: Factory Registration](#12-step-10-factory-registration)
13. [Step 11: Main Menu Entry](#13-step-11-main-menu-entry)
14. [Step 12: Help Markdown (Server)](#14-step-12-help-markdown-server)
15. [Step 13: Code Generation](#15-step-13-code-generation)
16. [Checklist](#16-checklist)
17. [Common Pitfalls](#17-common-pitfalls)

---

## 1. Architecture Overview

Every backend device follows the same lifecycle:

```
User clicks menu
       │
       ▼
  DeviceFactory.CreateXxx()
       │
       ├── new(StatementXxx)
       ├── SetStage / SetWireManager / SetHexMenu / SetGridAdjust / ...
       ├── Init()                     ← creates sprite.Element from SVG
       ├── RegisterConnectors()       ← registers ports in wire.Manager
       ├── manager.Manager.Register() ← palette icon
       ├── SceneMgr.Register()        ← scene serialization
       ├── SetPosition / Append       ← place on canvas
       │
       ▼
  Device lives on canvas
       │
       ├── Click → hex menu (connect, inspect, delete, resize)
       ├── Double-click → inspect overlay
       ├── Drag → grid snap + wire recalculate
       ├── Resize → re-render SVG + wire recalculate
       │
       ▼
  Export → scene JSON → codegen → Go source
```

Key packages involved:

| Package | Purpose |
|---------|---------|
| `devices/` | Device implementations (your code goes here) |
| `ornament/` | SVG drawing templates for device shapes |
| `sprite` | Canvas element abstraction (position, drag, resize) |
| `wire` | Connection manager (ports, wires, routing) |
| `hexMenu` | Hexagonal context menu system |
| `ui/mainMenu` | Standard menu item builders (Resize, Delete, Inspect) |
| `ui/overlay` | Property panel (form + markdown + Monaco editor) |
| `scene` | Serialization interfaces and JSON export |
| `factoryDevice/` | Factory that creates and wires up devices |
| `ui/mainMenu/menuBuilder.go` | Main menu hierarchy (Math, Logic, Constants...) |

---

## 2. File Structure

For a new device called `StatementModulo`, you will touch these files:

| File | What to do |
|------|-----------|
| `devices/statementModulo.go` | **CREATE** — main device code |
| `ornament/math/ornamentModulo.go` | **CREATE** — SVG drawing (or reuse existing) |
| `devices/sceneExport.go` | **EDIT** — add scene interface methods |
| `factoryDevice/factory.go` | **EDIT** — add `CreateModulo()` |
| `ui/mainMenu/menuBuilder.go` | **EDIT** — add menu item + factory interface |
| `server/help/math/modulo.md` | **CREATE** — help documentation |
| `server/codegen/` | **EDIT** — add codegen support (if new node type) |

---

## 3. Step 1: The Struct

Start by copying `statementAdd.go` and renaming. The struct has these essential
field groups:

```go
type StatementModulo struct {
    // ── Sprite system ──
    stage sprite.Stage        // canvas stage (injected before Init)
    elem  sprite.Element      // the visual element on canvas

    // ── State ──
    name         string
    initialized  bool
    selected     bool
    selectLocked bool
    dragEnabled  bool
    dragLocked   bool
    resizeLocked bool
    width        rulesDensity.Density
    height       rulesDensity.Density

    // ── Pending state (set before Init, applied after) ──
    pendingResizeEnable *bool
    pendingDragEnable   *bool
    pendingSelected     *bool

    // ── Warning mark ──
    warningMark        ornament.WarningMark
    warningElem        sprite.Element
    warningMarkEnabled bool

    // ── Resize ──
    resizerButton block.ResizeButton

    // ── Hex menu (shared — one per workspace) ──
    hexMenu *mainMenu.SpriteHexMenu

    // ── Wire manager (shared — one per workspace) ──
    wireMgr *wire.Manager

    // ── Dimensions ──
    defaultWidth          rulesDensity.Density
    defaultHeight         rulesDensity.Density
    horizontalMinimumSize rulesDensity.Density
    verticalMinimumSize   rulesDensity.Density

    // ── SVG drawing ──
    ornamentDraw     *math.OrnamentModulo     // runtime SVG
    ornamentDrawIcon *math.OrnamentModulo     // palette icon SVG

    // ── Identity ──
    id        string
    gridAdjust grid.Adjust
    iconStatus int

    // ── Label (visible below device) ──
    label    string
    canvasEl js.Value       // canvas DOM element for overlay positioning
    comment  string         // appears as comment in generated code
    hidden   bool           // hides the label (not the device)
    lastClickTime time.Time // double-click detection

    // ── Scene ──
    overlapPolicy scene.OverlapPolicy
    sceneNotify   func()
}
```

### Imports you will need

```go
import (
    "fmt"
    "log"
    "strings"
    "syscall/js"
    "time"

    "github.com/helmutkemper/iotmakerio/browser/factoryBrowser"
    "github.com/helmutkemper/iotmakerio/browser/html"
    "github.com/helmutkemper/iotmakerio/connection"
    "github.com/helmutkemper/iotmakerio/devices/block"
    "github.com/helmutkemper/iotmakerio/grid"
    "github.com/helmutkemper/iotmakerio/hexMenu"
    "github.com/helmutkemper/iotmakerio/manager"
    "github.com/helmutkemper/iotmakerio/ornament"
    "github.com/helmutkemper/iotmakerio/ornament/math"
    "github.com/helmutkemper/iotmakerio/rulesDensity"
    "github.com/helmutkemper/iotmakerio/rulesIcon"
    "github.com/helmutkemper/iotmakerio/rulesSequentialId"
    "github.com/helmutkemper/iotmakerio/rulesZIndex"
    "github.com/helmutkemper/iotmakerio/scene"
    "github.com/helmutkemper/iotmakerio/sprite"
    "github.com/helmutkemper/iotmakerio/translate"
    "github.com/helmutkemper/iotmakerio/ui/mainMenu"
    "github.com/helmutkemper/iotmakerio/ui/overlay"
    "github.com/helmutkemper/iotmakerio/wire"
    "github.com/helmutkemper/iotmakerio/utilsDraw"
    "github.com/helmutkemper/iotmakerio/utilsText"
    "github.com/nicksnyder/go-i18n/v2/i18n"
)
```

---

## 4. Step 2: SVG Drawing (Ornament)

Devices use the **ornament** system for SVG rendering. Each ornament is a Go type
in `ornament/math/` (or `ornament/logic/`, etc.) that generates an SVG element.

### Where SVG drawings live

```
ornament/
├── math/
│   ├── ornamentAdd.go       ← Add (+) shape
│   ├── ornamentSub.go       ← Sub (-) shape
│   ├── ornamentMul.go       ← Mul (×) shape
│   └── ornamentDiv.go       ← Div (÷) shape
├── warningMark.go           ← Warning exclamation overlay
└── ...
```

### Ornament interface

Every ornament must implement:

```go
type Ornament interface {
    Init() error
    Update(x, y, width, height rulesDensity.Density) error
    GetSvg() *html.TagSvg        // returns the SVG element
}
```

For math operations, ornaments also accept connector setups:

```go
ornament.InputXSetup(connection.Setup{...})   // left input (top)
ornament.InputYSetup(connection.Setup{...})   // left input (bottom)
ornament.OutputSetup(connection.Setup{...})   // right output
```

### Creating a new ornament

If your device has a unique shape, create `ornament/math/ornamentModulo.go`.
If it looks like Add but with a different symbol, you can modify the drawing
code that renders the operator symbol (typically a `%` for modulo).

The ornament draws:
- The outer shape (rectangle, rounded rect, hexagon)
- Connection point circles (inputs on the left, outputs on the right)
- The operator symbol in the center
- Fill colors and borders

### Alternative: Inline SVG (no ornament)

For simple devices like `ConstInt`, you can skip the ornament system entirely
and generate SVG as a string:

```go
func (e *StatementModulo) renderSVG() string {
    w := e.width.GetFloat()
    h := e.height.GetFloat()

    svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`, int(w), int(h))

    // Background rectangle
    svg += fmt.Sprintf(`<rect x="1" y="1" width="%.1f" height="%.1f" rx="4" ry="4"
        fill="#334455" stroke="#88AACC" stroke-width="2"/>`, w-2, h-2)

    // Label text
    svg += fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="Arial" font-size="14"
        fill="#FFFFFF" text-anchor="middle" dominant-baseline="central">%%</text>`,
        w/2, h/2)

    // Input connector (left, center)
    svg += fmt.Sprintf(`<circle cx="8" cy="%.1f" r="5" fill="#4488CC" stroke="#FFF" stroke-width="1"/>`, h/2)

    // Output connector (right, center)
    svg += fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="5" fill="#44CC88" stroke="#FFF" stroke-width="1"/>`, w-8, h/2)

    svg += `</svg>`
    return svg
}
```

Then use `CacheFromSvg` to render:

```go
_ = e.elem.CacheFromSvg(e.renderSVG())
```

### Label injection

The label is rendered BELOW the ornament area. To support this:

```go
const moduloLabelHeight = 18  // pixels reserved for label

func (e *StatementModulo) injectLabelIntoSvg(svgXml string, ornH rulesDensity.Density) string {
    if e.hidden {
        return svgXml  // label is hidden
    }
    displayLabel := e.label
    if displayLabel == "" {
        displayLabel = e.id
    }
    // Escape XML
    displayLabel = strings.ReplaceAll(displayLabel, "&", "&amp;")
    displayLabel = strings.ReplaceAll(displayLabel, "<", "&lt;")
    displayLabel = strings.ReplaceAll(displayLabel, ">", "&gt;")

    labelY := ornH.GetFloat() + 3
    labelSvg := fmt.Sprintf(
        `<text x="4" y="%.1f" font-family="Arial,sans-serif" font-size="11"
         fill="#AABBCC" dominant-baseline="hanging">%s</text>`,
        labelY, displayLabel,
    )
    return strings.Replace(svgXml, "</svg>", labelSvg+"</svg>", 1)
}
```

**Important**: the SVG `height` attribute must include the label area:

```go
totalHeight := e.height + moduloLabelHeight
ornamentSvg.Call("setAttribute", "height", totalHeight.GetInt())
```

---

## 5. Step 3: Init — Assembling the Device

The `Init()` method builds everything. Here is the sequence, annotated:

```go
func (e *StatementModulo) Init() (err error) {
    // 1. Guard: stage must exist
    if e.stage == nil {
        log.Println("[SPRITE] Error: SetStage() must be called before Init()")
        return
    }

    // 2. Generate sequential ID: "stmModulo" → "stmModulo_1", "stmModulo_2"...
    e.SetName("stmModulo")
    e.id = rulesSequentialId.GetIdFromBase(e.name)
    e.label = e.id  // default label

    // 3. Warning mark (exclamation icon shown on error)
    warningMark := new(ornament.WarningMarkExclamation)
    warningMark.SetMargin(0)
    _ = warningMark.Init()
    e.warningMark = warningMark

    // 4. Default dimensions
    size := rulesDensity.Density(60)
    e.defaultWidth = size
    e.defaultHeight = size
    e.horizontalMinimumSize = size
    e.verticalMinimumSize = size
    if e.width == 0 { e.width = e.defaultWidth }
    if e.height == 0 { e.height = e.defaultHeight }

    // 5. Lock resize by default (user unlocks via menu)
    e.resizeLocked = true

    // 6. Create ornament instances
    e.ornamentDraw = new(math.OrnamentModulo)
    e.ornamentDrawIcon = new(math.OrnamentModulo)

    // 7. Configure connection points on the ornament
    //    (see "Wire Connections" section for details)
    e.setupConnections()

    // 8. Initialize ornaments
    _ = e.ornamentDraw.Init()
    _ = e.ornamentDrawIcon.Init()

    // 9. Render SVG → sprite.Element
    _ = e.ornamentDraw.Update(0, 0, e.width, e.height)
    ornamentSvg := e.ornamentDraw.GetSvg().Get()
    totalHeight := e.height + moduloLabelHeight
    ornamentSvg.Call("setAttribute", "width", e.width.GetInt())
    ornamentSvg.Call("setAttribute", "height", totalHeight.GetInt())
    ornamentXml := serializeSvgToXml(ornamentSvg)
    ornamentXml = e.injectLabelIntoSvg(ornamentXml, e.height)

    // 10. Create canvas element
    e.elem, err = e.stage.CreateElement(sprite.ElementConfig{
        ID:         e.id,
        X:          0,
        Y:          0,
        Width:      e.width.GetFloat(),
        Height:     totalHeight.GetFloat(),
        Index:      rulesZIndex.Math,      // z-index layer
        DragEnable: false,
        SvgXml:     ornamentXml,
    })
    if err != nil {
        log.Printf("[SPRITE] Failed to create element: %v", err)
        return
    }

    // 11. Minimum size (includes label)
    e.elem.SetMinSizeD(e.horizontalMinimumSize, e.verticalMinimumSize+moduloLabelHeight)

    // 12. Resize buttons
    if e.resizerButton != nil {
        adapter := &hexagonSpriteAdapter{template: e.resizerButton}
        e.elem.SetResizeButtons(adapter)
        e.elem.ShowResizeButtons(false)
        e.elem.SetResizeEnable(false)
    }

    // 13. Wire up events (click, drag, resize)
    e.wireEvents()

    // 14. Create warning overlay element
    e.initWarningElement()

    e.initialized = true

    // 15. Apply any pending states (set before Init)
    if e.pendingSelected != nil { e.SetSelected(*e.pendingSelected); e.pendingSelected = nil }
    if e.pendingDragEnable != nil { e.SetDragEnable(*e.pendingDragEnable); e.pendingDragEnable = nil }
    if e.pendingResizeEnable != nil { e.SetResizeEnable(*e.pendingResizeEnable); e.pendingResizeEnable = nil }

    return nil
}
```

### Z-Index layers

The `rulesZIndex` package defines rendering order:

| Constant | Usage |
|----------|-------|
| `rulesZIndex.Math` | Math devices (Add, Sub, Mul, Div) |
| `rulesZIndex.Logic` | Logic devices (Compare, Bool) |
| `rulesZIndex.Constants` | Constant devices (ConstInt) |
| `rulesZIndex.Containers` | Loop and other containers |

Use the layer that matches your device category.

---

## 6. Step 4: Wire Connections

Connections are the heart of the IDE. Each device registers **connectors** (ports)
with the shared `wire.Manager`.

### Connector positions

Connectors are positioned in **local coordinates** relative to the device element.
The position is a function (not a fixed value) because the device can be moved
and resized:

```go
func (e *StatementModulo) RegisterConnectors() {
    if e.wireMgr == nil || e.elem == nil {
        return
    }

    // Input — left side, vertical center
    e.wireMgr.RegisterConnector(wire.ConnectorInfo{
        ID:             wire.ConnectorID{ElementID: e.id, PortName: "input"},
        IsOutput:       false,
        AllowedTypes:   []string{"int", "float"},   // types this port accepts
        MaxConnections: 1,                            // 1 = single wire, 0 = unlimited
        Label:          "Input",
        PositionFunc: func() (float64, float64) {
            ex, ey := e.elem.GetPosition()
            _, h := e.elem.GetSize()
            ornH := h - float64(moduloLabelHeight)
            return ex + 2, ey + ornH/2
        },
    })

    // Output — right side, vertical center
    e.wireMgr.RegisterConnector(wire.ConnectorInfo{
        ID:             wire.ConnectorID{ElementID: e.id, PortName: "output"},
        IsOutput:       true,
        AllowedTypes:   []string{"int", "float"},
        MaxConnections: 0,  // unlimited outputs
        Label:          "Output",
        PositionFunc: func() (float64, float64) {
            ex, ey := e.elem.GetPosition()
            w, h := e.elem.GetSize()
            ornH := h - float64(moduloLabelHeight)
            return ex + w - 12, ey + ornH/2
        },
    })
}
```

### Important rules for connectors

1. **PositionFunc must subtract label height** — `GetSize()` returns total height
   including the label area. Connector positions should reference the **ornament**
   height: `ornH := h - float64(labelHeight)`

2. **AllowedTypes** determines what can connect to what. A wire from an output
   of type `"int"` can only connect to an input that accepts `"int"`.

3. **MaxConnections=1** for inputs (one source), **MaxConnections=0** (unlimited)
   for outputs (many consumers).

4. **Call RegisterConnectors() AFTER Init()** — the `e.elem` must exist.

5. **Unregister on Remove()** — always call `e.wireMgr.UnregisterElement(e.id)`
   in `Remove()` to clean up all connectors and wires.

6. **Recalculate on move/resize** — after drag or resize, call
   `e.wireMgr.RecalculateForElement(e.id)` so wires redraw correctly.

### Connector position conventions

```
    inputX ●─────────────── ● output
           │               │
           │   (device)    │
           │               │
    inputY ●───────────────┘

    Local coordinates:
    inputX:  (2, 15)
    inputY:  (2, ornH - 18)
    output:  (w - 12, ornH/2 - 2)
```

The exact coordinates come from the ornament's SVG circle positions. If you
create a custom ornament, match the connector registration positions to the
SVG circle coordinates.

---

## 7. Step 5: Hex Menu

The hex menu is a hexagonal context menu that appears on click. It uses a
**grid coordinate system** with columns and rows:

```
        (1,1)       (3,1)       (5,1)
    (2,2)       (4,2)
        (1,3)       (3,3)       (5,3)
    (2,4)       (4,4)
        (1,5)       (3,5)       (5,5)
```

Position `(3,3)` is the center. `GoBackItem(3,3)` always goes there.

### Body menu (click on device body)

```go
func (e *StatementModulo) getBodyMenuItems() []hexMenu.MenuItem {
    return []hexMenu.MenuItem{
        mainMenu.ResizeItem(1, 1, func() {
            e.resizeLocked = false
            e.SetResizeEnable(true)
        }),
        mainMenu.DeleteItem(1, 3, func() {
            e.Remove()
        }),
        mainMenu.InspectItem(1, 5, func() {
            go e.showInspectOverlay()   // MUST run in goroutine
        }),
    }
}
```

The standard builders `ResizeItem`, `DeleteItem`, `InspectItem` provide
consistent icons and labels across all devices. Use them.

### Connector menu (click on a connector circle)

```go
// In the click handler, for each connector hit:
go e.hexMenu.Open(mainMenu.ConnectorMenu(e.wireMgr, e.id, "input"), menuX, menuY)
```

`ConnectorMenu` automatically provides Connect/Disconnect options. You can
append extra items:

```go
mainMenu.ConnectorMenu(e.wireMgr, e.id, "output",
    hexMenu.MenuItem{
        ID: "monitor", Col: 1, Row: 5,
        Label: "Monitor",
        Type:  hexMenu.ItemSubmenu,
        Submenu: e.getMonitorSubmenu(),
    },
)
```

### Submenus

For nested menus, use `Type: hexMenu.ItemSubmenu` and provide `Submenu`:

```go
hexMenu.MenuItem{
    ID: "settings", Col: 1, Row: 5,
    Label:   "Settings",
    Type:    hexMenu.ItemSubmenu,
    Submenu: []hexMenu.MenuItem{
        hexMenu.GoBackItem(3, 3),       // always include back button
        {ID: "opt1", Col: 2, Row: 2, Label: "Option 1", Type: hexMenu.ItemAction,
         OnClick: func() { /* ... */ }},
        {ID: "opt2", Col: 2, Row: 4, Label: "Option 2", Type: hexMenu.ItemAction,
         OnClick: func() { /* ... */ }},
    },
}
```

### Click handler with hit-testing

```go
e.elem.SetOnClick(func(event sprite.PointerEvent) {
    w, h := e.elem.GetSize()
    ornH := h - float64(moduloLabelHeight)
    connRadius := 10.0

    // Double-click detection
    now := time.Now()
    isDoubleClick := now.Sub(e.lastClickTime) < 300*time.Millisecond
    e.lastClickTime = now

    elemX, elemY := e.elem.GetPosition()
    menuX := elemX + event.LocalX
    menuY := elemY + event.LocalY

    // Close any open menu first
    if e.hexMenu.IsVisible() {
        e.hexMenu.Close()
        return
    }

    if isDoubleClick {
        go e.showInspectOverlay()
        return
    }

    // Hit-test each connector (circle collision)
    // Input connector at (2, ornH/2)
    dx := event.LocalX - 2
    dy := event.LocalY - ornH/2
    if dx*dx+dy*dy <= connRadius*connRadius {
        go e.hexMenu.Open(mainMenu.ConnectorMenu(e.wireMgr, e.id, "input"), menuX, menuY)
        return
    }

    // Output connector at (w-12, ornH/2)
    dx = event.LocalX - (w - 12)
    dy = event.LocalY - (ornH/2)
    if dx*dx+dy*dy <= connRadius*connRadius {
        go e.hexMenu.Open(mainMenu.ConnectorMenu(e.wireMgr, e.id, "output"), menuX, menuY)
        return
    }

    // Body click (fallback)
    go e.hexMenu.Open(e.getBodyMenuItems(), menuX, menuY)
})
```

### Cursor hit-test

For visual feedback (pointer cursor on connectors):

```go
e.elem.SetCursorHitTest(func(localX, localY float64) sprite.CursorStyle {
    // Same hit-test logic as above but returns CursorPointer or ""
})
```

---

## 8. Step 6: The Property Panel (Inspect Overlay)

The overlay system (`ui/overlay`) creates a draggable floating window with
tabs. Configuration is JSON-compatible.

### Basic pattern

```go
func (e *StatementModulo) showInspectOverlay() {
    cfg := e.GetInspectConfig().(overlay.Config)
    overlay.Show(cfg)
}
```

### Configuring tabs

```go
func (e *StatementModulo) GetInspectConfig() interface{} {
    hiddenVal := "false"
    if e.hidden { hiddenVal = "true" }

    return overlay.Config{
        Title: fmt.Sprintf("Modulo — %s", e.id),
        Width: "540px",
        Tabs: []overlay.Tab{
            // ── Tab 1: Properties (form) ──
            {
                Label: translate.T("tabProperties", "Properties"),
                Type:  overlay.TabForm,
                Fields: []overlay.Field{
                    {Key: "label", Label: "Label", Type: overlay.FieldText, Value: e.label},
                    {Key: "id", Label: "ID", Type: overlay.FieldText, Value: e.id},
                    {
                        Key: "dataType", Label: "Data Type",
                        Type: overlay.FieldSelect, Value: "int",
                        Options: []overlay.Option{
                            {Value: "int", Label: "Int"},
                            {Value: "float", Label: "Float"},
                        },
                    },
                    {
                        Key: "comment", Label: "Comment",
                        Type: overlay.FieldTextarea, Value: e.comment,
                        Placeholder: "Comment shown in generated code...",
                        Rows: 3,
                    },
                    {Key: "hidden", Label: "Hide Label", Type: overlay.FieldCheckbox, Value: hiddenVal},
                },
            },
            // ── Tab 2: Help (markdown from server) ──
            {
                Label:      translate.T("tabHelp", "Help"),
                Type:       overlay.TabMarkdown,
                ContentURL: "http://localhost:8080/api/help/math/modulo.md",
            },
            // ── Tab 3: Code Preview (Monaco editor, read-only) ──
            {
                Label:    translate.T("tabCodePreview", "Code Preview"),
                Type:     overlay.TabMonaco,
                Content:  e.codePreview(),
                Language: "go",
                ReadOnly: true,
            },
        },
        OnSave: func(values map[string]string) {
            e.ApplyProperties(values)
        },
    }
}
```

### Available field types

| Type | HTML element | Notes |
|------|-------------|-------|
| `overlay.FieldText` | `<input type="text">` | Enter → Save |
| `overlay.FieldNumber` | `<input type="number">` | Enter → Save. Supports `Min`, `Max` |
| `overlay.FieldSelect` | `<select>` | Requires `Options` array |
| `overlay.FieldCheckbox` | `<input type="checkbox">` | Value: `"true"` / `"false"` |
| `overlay.FieldTextarea` | `<textarea>` | Ctrl+Enter → Save. Supports `Rows` |
| `overlay.FieldColor` | `<input type="color">` | Returns hex like `#FF0000` |

### Available tab types

| Type | Description |
|------|-------------|
| `overlay.TabForm` | Form fields with Save button |
| `overlay.TabMarkdown` | Rendered markdown (via marked.js from CDN) |
| `overlay.TabMonaco` | Monaco editor (via CDN). Supports `Language`, `ReadOnly` |

### Markdown from server

Use `ContentURL` to load help from the server:

```go
{Type: overlay.TabMarkdown, ContentURL: "http://localhost:8080/api/help/math/modulo.md"}
```

Or inline markdown with `Content`:

```go
{Type: overlay.TabMarkdown, Content: "# Modulo\n\nComputes remainder of division."}
```

### ApplyProperties — handling Save

**Critical**: `recacheOrnament()` blocks on `Image.onload`. You MUST run it in
a goroutine with a delay to avoid deadlocking the browser:

```go
func (e *StatementModulo) ApplyProperties(values map[string]string) {
    changed := false

    if v, ok := values["label"]; ok && v != "" && v != e.label {
        e.label = v
        changed = true
    }
    if v, ok := values["comment"]; ok && v != e.comment {
        e.comment = v
    }
    if v, ok := values["hidden"]; ok {
        h := v == "true"
        if h != e.hidden {
            e.SetHidden(h)
            changed = true   // label visibility changed, need re-render
        }
    }

    if changed {
        // ⚠ MUST be goroutine + delay — Image.onload deadlock
        go func() {
            time.Sleep(200 * time.Millisecond)
            e.recacheOrnament()
            if e.sceneNotify != nil {
                e.sceneNotify()
            }
        }()
    }
}
```

---

## 9. Step 7: The Editable Label

The label appears below the device, left-aligned. It defaults to the device ID
and can be edited via the Inspect panel.

### What you need

1. **Fields**: `label string`, `hidden bool`, `lastClickTime time.Time`
2. **Constant**: `const moduloLabelHeight = 18`
3. **Inject into SVG**: `injectLabelIntoSvg()` (see Step 2)
4. **Total height**: always add `moduloLabelHeight` to element height
5. **Connector positions**: always subtract `moduloLabelHeight` from `GetSize().h`

### Methods to implement

```go
func (e *StatementModulo) GetLabel() string    { return e.label }
func (e *StatementModulo) SetLabel(label string) {
    e.label = label
    go e.recacheOrnament()
}
func (e *StatementModulo) IsHidden() bool { return e.hidden }
func (e *StatementModulo) SetHidden(h bool) {
    e.hidden = h
    go func() {
        time.Sleep(200 * time.Millisecond)
        e.recacheOrnament()
    }()
}
```

---

## 10. Step 8: Scene Serialization

Add methods to `devices/sceneExport.go`:

```go
// =====================================================================
//  StatementModulo — scene.SceneDevice
// =====================================================================

func (e *StatementModulo) GetDeviceType() string { return "StatementModulo" }

func (e *StatementModulo) GetOuterBBox() scene.Rect {
    if e.elem == nil { return scene.Rect{} }
    x, y := e.elem.GetPosition()
    w, h := e.elem.GetSize()
    return scene.Rect{X: x, Y: y, Width: w, Height: h}
}

func (e *StatementModulo) GetInnerBBox() *scene.Rect {
    if e.elem == nil { return nil }
    x, y := e.elem.GetPosition()
    w, h := e.elem.GetSize()
    p := 5.0
    return &scene.Rect{X: x + p, Y: y + p, Width: w - 2*p, Height: h - 2*p}
}

func (e *StatementModulo) GetOverlapPolicy() scene.OverlapPolicy  { return e.overlapPolicy }
func (e *StatementModulo) SetOverlapPolicy(p scene.OverlapPolicy) { e.overlapPolicy = p }
func (e *StatementModulo) SetSceneNotify(fn func())               { e.sceneNotify = fn }

func (e *StatementModulo) MoveBy(dx, dy float64) {
    if e.elem == nil { return }
    x, y := e.elem.GetPosition()
    e.elem.SetPosition(x+dx, y+dy)
    e.updateWarningPosition()
    if e.wireMgr != nil { e.wireMgr.RecalculateForElement(e.id) }
}
```

### Optional interfaces

Implement these in `statementModulo.go` if applicable:

| Interface | Methods | Purpose |
|-----------|---------|---------|
| `scene.Labeled` | `GetLabel() string` | Exports label to scene JSON |
| `scene.Propertied` | `GetProperties() map[string]interface{}` | Exports comment, hidden, etc. |
| `scene.Inspectable` | `GetInspectConfig() interface{}`, `ApplyProperties(map[string]string)` | Overlay panel |

```go
func (e *StatementModulo) GetProperties() map[string]interface{} {
    props := map[string]interface{}{}
    if e.comment != "" { props["comment"] = e.comment }
    if e.hidden { props["hidden"] = true }
    return props
}
```

---

## 11. Step 9: The Icon (Palette)

Each device provides an icon for the device manager palette. The icon is a
hexagonal shape with a symbol and label.

```go
func (e *StatementModulo) GetIconName() string     { return "Modulo" }
func (e *StatementModulo) GetIconCategory() string { return "Math" }

func (e *StatementModulo) GetIcon() *manager.RegisterIcon {
    translated, err := translate.Localizer.Localize(&i18n.LocalizeConfig{
        DefaultMessage: &i18n.Message{ID: "IconDeviceModulo", Other: "Modulo"},
    })
    if err != nil { translated = "Modulo" }

    name := e.GetIconName()
    category := e.GetIconCategory()
    iconPipeLine := make([]js.Value, 5)
    for i := 0; i < 5; i++ {
        iconPipeLine[i] = e.getIcon(rulesIcon.Data{
            Status: i, Name: name, Category: category, Label: translated,
        })
    }

    register := new(manager.RegisterIcon)
    register.SetName(name)
    register.SetCategory(category)
    register.SetIcon(iconPipeLine)
    return register
}
```

The 5 pipeline states are: Normal, Disabled, Selected, Attention1, Attention2.
The `getIcon` method builds an SVG hexagon with the ornament symbol centered.
Copy it from `statementAdd.go` and change the ornament reference.

---

## 12. Step 10: Factory Registration

Edit `factoryDevice/factory.go`:

```go
func (f *DeviceFactory) CreateModulo() {
    stm := new(devices.StatementModulo)
    stm.SetStage(f.Stage)
    stm.SetWireManager(f.WireMgr)
    stm.SetResizerButton(f.ResizeButton)
    stm.SetDraggerButton(f.DraggerButton)
    stm.SetGridAdjust(f.GridAdjust)
    stm.SetHexMenu(f.HexMenu)          // ← shared hex menu
    stm.SetCanvasEl(f.CanvasEl)         // ← for overlay positioning

    if err := stm.Init(); err != nil {
        log.Printf("[Factory] StatementModulo.Init: %v", err)
        return
    }

    stm.RegisterConnectors()                                              // wire ports
    manager.Manager.Register(stm)                                         // device palette
    manager.Manager.Register(stm.GetIcon())                               // icon
    stm.SetOverlapPolicy(scene.OverlapPolicy{                             // spatial rules
        AllowAbove: false, AllowBelow: true, AllowPartial: false,
    })
    f.SceneMgr.Register(stm)                                              // scene export
    stm.SetSceneNotify(f.SceneNotifyFn)                                   // change callback

    cx, cy := f.screenCenter()
    stm.SetPosition(cx, cy)
    stm.SetDragEnable(true)
    stm.Append()
    log.Printf("[Factory] Created StatementModulo at (%v, %v)", cx, cy)
}
```

**Do not forget** `SetHexMenu` and `SetCanvasEl` — without them, the hex menu
and overlay will not work.

---

## 13. Step 11: Main Menu Entry

Edit `ui/mainMenu/menuBuilder.go`:

### 1. Add to the DeviceCreator interface

```go
type DeviceCreator interface {
    SafeRun(name string, fn func())
    CreateAdd()
    CreateSub()
    CreateMul()
    CreateDiv()
    CreateModulo()     // ← NEW
    CreateLoop()
    CreateConstInt()
    CreateBool()
    CreateCompare()
    CreateGauge()
}
```

### 2. Add menu item in the appropriate submenu

The Math submenu uses a hex grid. Find an available position:

```go
func (b *MenuBuilder) mathSubmenu() []hexMenu.MenuItem {
    // Existing: Add(2,2) Sub(2,4) Mul(4,2) Div(4,4)
    // Available positions in the next "ring": (1,1) (1,3) (1,5) (3,1) (3,5) (5,1) (5,3) (5,5)

    return []hexMenu.MenuItem{
        back,
        // ... existing items ...
        {
            ID:              "Modulo",
            Col:             3, Row: 5,                                // choose a free position
            Label:           translate.T("menuMainModulo", "Mod"),
            FontAwesomePath: rulesIcon.KFAPercent,                     // Font Awesome icon path
            ViewBox:         "0 0 384 512",
            Type:            hexMenu.ItemAction,
            OnClick:         func() { b.factory.SafeRun("CreateModulo", b.factory.CreateModulo) },
            Styles:          styles,
        },
    }
}
```

### Font Awesome icons

Icons are defined in `rulesIcon/falcons.go` as SVG path data constants:

```go
const KFAPercent = "M374.6 118.6c12.5-12.5 12.5-32.8 0-45.3s-32.8-12.5-45.3 0l-320 320c-12.5 12.5-12.5 32.8 0 45.3s32.8 12.5 45.3 0l320-320zM..."
```

If the icon you need is not in `falcons.go`, add it. Find the SVG path data
from [Font Awesome](https://fontawesome.com/) (free icons only) and add a new
`KFA...` constant.

---

## 14. Step 12: Help Markdown (Server)

Create the file `server/help/math/modulo.md`:

```markdown
# Modulo — Remainder Division

## Description

Computes the remainder of integer division: output = inputX % inputY.

## Ports

| Port | Direction | Type | Description |
|------|-----------|------|-------------|
| **inputX** | Input | int | Dividend |
| **inputY** | Input | int | Divisor |
| **output** | Output | int | Remainder (inputX % inputY) |

## Code Generation

    modulo1 := inputX % inputY
```

The server already serves files from `help/` via `GET /api/help/{path}`. No
server-side changes are needed unless you add a new directory outside `help/`.

---

## 15. Step 13: Code Generation

To support code generation for the new device, edit the codegen pipeline on
the server:

### 1. `server/codegen/graph/builder.go`

Add the device type to the node creation switch:

```go
case "StatementModulo":
    node.Type = "mod"
    // ports: inputX, inputY, output
```

### 2. `server/codegen/ir/emit.go`

Add an `OpMod` opcode if it doesn't exist, and handle it in `emitNode()`:

```go
case "mod":
    // emit: dest = srcX % srcY
```

### 3. `server/codegen/backend/golang/emit.go`

Add Go code generation for the mod operation:

```go
case ir.OpMod:
    // result := a % b
```

### Comment support

When the device has a `comment` property (from `GetProperties()`), the codegen
backend should emit it as a Go comment above the operation:

```go
// User's comment here
modulo1 := inputX % inputY
```

---

## 16. Checklist

Before considering your device complete, verify:

- [ ] **Struct**: all field groups present (sprite, state, pending, warning, etc.)
- [ ] **Setters**: `SetStage`, `SetWireManager`, `SetHexMenu`, `SetCanvasEl`,
      `SetGridAdjust`, `SetResizerButton`, `SetDraggerButton`
- [ ] **Init**: creates element, sets min size, wires events, creates warning
- [ ] **SVG**: ornament renders correctly at default and resized dimensions
- [ ] **Label**: visible below device, `injectLabelIntoSvg` works, `hidden` hides it
- [ ] **Connectors**: `RegisterConnectors` with correct positions (subtract label height!)
- [ ] **Hex menu**: body menu (Resize, Delete, Inspect) + connector menus
- [ ] **Double-click**: opens Inspect overlay
- [ ] **Inspect overlay**: Properties tab, Help tab (URL), Code Preview tab (Monaco)
- [ ] **ApplyProperties**: `recacheOrnament` in goroutine with 200ms delay
- [ ] **Scene export**: all methods in `sceneExport.go` (GetDeviceType, BBox, MoveBy)
- [ ] **Scene interfaces**: `Labeled`, `Propertied`, `Inspectable`
- [ ] **Icon**: `GetIcon` returns hexagonal palette icon
- [ ] **Factory**: `CreateModulo()` with all injections
- [ ] **Menu**: `DeviceCreator` interface updated, menu item added
- [ ] **Remove**: calls `wireMgr.UnregisterElement`, hides warning
- [ ] **Drag end**: grid snap + `RecalculateForElement` + `sceneNotify`
- [ ] **Resize end**: recache ornament + grid snap + recalculate + notify
- [ ] **Help file**: `server/help/math/modulo.md` created
- [ ] **Codegen**: new node type handled in graph builder, IR emitter, Go backend
- [ ] **Compiles**: `GOARCH=wasm GOOS=js go build -o main.wasm` with zero errors

---

## 17. Common Pitfalls

### Browser freeze (deadlock)

Any call to `recacheSVG()`, `recacheOrnament()`, or `CacheFromSvg()` that
happens inside a `js.FuncOf` callback will deadlock the browser. These
methods wait for `Image.onload`, which cannot fire while JS is blocked.

**Solution**: always wrap in `go func() { time.Sleep(200*time.Millisecond); ... }()`

### Connector positions wrong after label added

`GetSize()` returns total height including label. If you use `h` directly for
connector positions, they will be offset by 18px.

**Solution**: `ornH := h - float64(labelHeight)` everywhere.

### Hex menu appears but clicking does nothing

The menu's `Open()` and `StartTutorial()` must run in a goroutine.

**Solution**: `go e.hexMenu.Open(...)`, not `e.hexMenu.Open(...)`

### Unused imports

Go WASM compilation is strict. If you import `"time"` or `"fmt"` but don't
use them, build fails. Check all imports before building.

### Missing factory injection

If you see "nil pointer" on click, you probably forgot to call `SetHexMenu()`
or `SetWireManager()` in the factory.

### Menu overlaps with device

Menu position is in **absolute canvas coordinates**, not local coordinates.
Always convert: `menuX := elemX + event.LocalX`
