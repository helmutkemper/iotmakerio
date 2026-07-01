# hexMenu Package

## What this package does

Renders a hexagonal context menu on top of the IDE canvas using the `sprite`
package. Each hexagon is a standalone `sprite.Element`, pre-rendered as an SVG
bitmap. The menu supports multi-level navigation (submenu stack), a tutorial
mode that flashes a target item, and a transparent backdrop that closes the
menu on any outside click.

---

## Design decisions

| Decision  | Choice                                                                           |
|-----------|----------------------------------------------------------------------------------|
| Canvas    | Same sprite canvas (high z-index elements)                                       |
| Submenu   | Replace current menu (hierarchical navigation with stack)                        |
| Layout    | Hexagonal grid with alternating columns                                          |
| Scope     | Contextual — each device defines its own menu items                              |
| Position  | Configurable: at click, centered on element, or fixed                            |
| Closing   | Click outside closes all; GoBack goes back one level; action executes and closes |
| Tutorial  | Action items execute; submenu items navigate (both advance tutorial step)        |
| Rendering | Each hexagon is a separate sprite.Element                                        |

---

## File structure

| File          | Responsibility                                                       |
|---------------|----------------------------------------------------------------------|
| `types.go`    | All public types, constants, interfaces, default style variables     |
| `grid.go`     | Hex grid coordinate calculations and SVG geometry helpers            |
| `renderer.go` | SVG XML string generation for hexagon icons (one per pipeline state) |
| `_menu.go`    | Menu controller: New, Open, Close, navigate, click handling          |

---

## Hex grid math

Flat-top hexagon grid with offset coordinates:

```
x = (col - 1) * 1.5 * radius
y = (row - 1) * sqrt(3)/2 * radius
```

Adjacent hexagons sharing edges differ by:
- Same column: row ± 2
- Adjacent column: row ± 1

Use **odd columns** for items on the same visual horizontal level.
Use **even columns** for items that sit between two odd-column rows.

---

## MenuItem — layout fields

Every `MenuItem` has two groups of position fields:

### 1. Absolute grid position — `Col` and `Row`

These are the **final** coordinates used by the renderer. They are set by one
of two sources:

- **Static menu items** (math sub-menu, logic, export, …): the developer
	sets `Col` and `Row` directly in the `MenuItem` literal.
- **Black-box function submenus**: `rulesMainMenu.ApplyRadialLayout` computes
	the absolute position from `MenuCol`/`MenuRow` (see below) and writes it
	back into `Col`/`Row` before the slice is returned.

### 2. Relative layout hints — `MenuCol`, `MenuRow`, `MenuPosSet`

These carry the specialist's explicit placement request, loaded from the
`menu:col,row.` IDS directive in the method doc comment.

| Field        | Type   | Meaning                                                                       |
|--------------|--------|-------------------------------------------------------------------------------|
| `MenuCol`    | `int`  | Column offset from the Back button center. Negative = left, positive = right. |
| `MenuRow`    | `int`  | Row offset from the Back button center. Negative = up, positive = down.       |
| `MenuPosSet` | `bool` | `true` when the specialist declared `menu:col,row.`. `false` = auto-place.    |

`rulesMainMenu.ApplyRadialLayout` reads these three fields and writes the
computed absolute position into `Col`/`Row`. Regular menu code never reads
`MenuCol`/`MenuRow` directly — they are consumed once by the layout engine.

**`(0,0)` is reserved** for the Back button and must never appear as a
`(MenuCol, MenuRow)` pair. The layout engine does not validate this at runtime.

---

## Pipeline states per hexagon

Each item pre-renders 5 SVG bitmaps (one per `PipelineState`):

| State                | When used                                      |
|----------------------|------------------------------------------------|
| `PipelineNormal`     | Default — item is clickable                    |
| `PipelineDisabled`   | Tutorial mode — item is not the current target |
| `PipelineSelected`   | (reserved for future hover/active feedback)    |
| `PipelineAttention1` | Tutorial flash phase 1 (brighter)              |
| `PipelineAttention2` | Tutorial flash phase 2 (darker)                |

---

## Integration example — static item

```go
func (e *StatementAdd) getHexMenuItems() []hexMenu.MenuItem {
    return []hexMenu.MenuItem{
        {
            ID:              "resize",
            Col:             1,      // absolute grid position
            Row:             1,
            Label:           "Resize",
            FontAwesomePath: rulesIcon.KFAArrowsUpDownLeftRight,
            ViewBox:         "0 0 512 512",
            Type:            hexMenu.ItemAction,
            OnClick:         func() { e.SetResizeEnable(true) },
            Styles:          hexMenu.DefaultStyles(),
            // MenuCol/MenuRow/MenuPosSet are zero/false — not used for static items
        },
    }
}
```

## Integration example — black-box item (layout engine)

The black-box builder does **not** set `Col`/`Row` directly. It sets the
layout hints and delegates to `rulesMainMenu.ApplyRadialLayout`:

```go
item := hexMenu.MenuItem{
    ID:         "bb_APDS9960_init",
    Label:      "Init",
    Type:       hexMenu.ItemAction,
    MenuCol:    -1,   // from menu:-1,-1. directive
    MenuRow:    -1,
    MenuPosSet: true,
    OnClick:    ...,
    Styles:     styles,
}
// Col and Row are zero here — ApplyRadialLayout will fill them in.
```

---

## GoBackItem

```go
back := hexMenu.GoBackItem(col, row)
back.Styles = styles  // always apply styles after GoBackItem()
```

For black-box submenus, use the constants from `rulesMainMenu`:

```go
back := hexMenu.GoBackItem(rulesMainMenu.BackCenterCol, rulesMainMenu.BackCenterRow)
```

This ensures Back always lands at the absolute center that `ApplyRadialLayout`
expects, regardless of any future changes to the default center coordinates.

---

## Opening a menu

```go
// Device double-click → open at click position
menu := hexMenu.New(spriteStage, hexMenu.Config{
    HexRadius: 28,
    ZIndex:    1000,
})
menu.Open(items, hexMenu.PositionAtClick, clickX, clickY)
```

The `ui/mainMenu` package wraps this in `SpriteHexMenu.OpenFromDevice()` which
also handles camera-to-screen coordinate conversion and canvas edge clamping.

---

## Tutorial mode

```go
menu.StartTutorial(
    mainMenuItems,
    []hexMenu.TutorialStep{
        {PagePath: nil,              ItemID: "SysMath"},  // flash Math on root page
        {PagePath: []string{"SysMath"}, ItemID: "Add"},   // flash Add inside Math
    },
    hexMenu.PositionCentered,
    400, 300,
)
```
