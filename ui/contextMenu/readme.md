# `ui/contextMenu` — Linear two-column popover menu

## What this package does

Renders a tablet-friendly linear context menu on top of the IDE
canvas (backend stage, frontend stage) or anchored to a DOM element
(frontend device clicks). Replaces the radial `hexMenu`
`OpenFromDevice` path and the inline `td-ctx-*` HTML used by a
handful of frontend devices.

Two visible columns:

```
┌──────────────┬────────────────────────────┐
│ [icon] Label │ Markdown help preview      │
│ [icon] Label │ (translatable, via         │
│ [icon] Label │  window.marked)            │
│ ← Back       │                            │
└──────────────┴────────────────────────────┘
```

Interaction is adaptive:

- **Fine pointer (mouse)** — hover moves the preview, click on a
  leaf row executes, click on a submenu row navigates.
- **Coarse pointer (touch)** — first tap moves the preview *and*
  flags the row as pending; a second tap on the same row confirms
  and executes. Submenu rows always navigate on first tap.

Detection is done once at open time via `matchMedia('(pointer: coarse)')`.
Same menu code, different behaviour, no runtime mode switch.

---

## Why this package exists

The hex menu is deprecated — see
[`/ide/docs/REDESIGN_HEX_MENU_TO_TABLET.md`](../../docs/REDESIGN_HEX_MENU_TO_TABLET.md)
for the full reasoning. Briefly: radial does not scale past ~6
items, hover affordances do not translate to touch, and nested hex
menus are painful on tablets. The implementation plan and the
locked design decisions live in
[`/ide/docs/NEW_CONTEXT_MENU.md`](../../docs/NEW_CONTEXT_MENU.md).

This package is the execution of that plan.

---

## Public surface

One controller per workspace. Inject into devices via the same
pattern used for the old hex menu.

```go
import "github.com/helmutkemper/iotmakerio/ui/contextMenu"

// At workspace init:
ctx := contextMenu.New(stage)

// In the device:
stm.SetContextMenu(ctx)

// In a click handler:
ctx.OpenAtWorld(e.bodyItems(), clickWorldX, clickWorldY)
```

| Method                            | Use when                                      |
|-----------------------------------|-----------------------------------------------|
| `New(stage)`                      | Once per workspace, at startup                |
| `Open(items, sx, sy)`             | You already have screen coordinates           |
| `OpenAtWorld(items, wx, wy)`      | Backend device click (world-space coords)     |
| `OpenAtElement(items, domNode)`   | Frontend device click (DOM element available) |
| `Close()`                         | Programmatically dismiss                      |
| `IsOpen() bool`                   | Check before opening a new menu               |

### `Item` — the only type callers construct

```go
contextMenu.Item{
    ID:              "delete",
    Label:           translate.T("menuDeviceDelete", "Delete"),
    FontAwesomePath: rulesIcon.KFATrashCan,
    ViewBox:         "0 0 448 512",
    HelpKey:         "helpMenuDelete",
    HelpFallback:    "Removes this device and disconnects its wires.",
    Danger:          true,
    OnClick:         func() { e.Remove() },
}
```

Nested submenus: set `Submenu []Item` and leave `OnClick` nil —
tapping the row pushes into the submenu instead of executing.

---

## File layout

| File              | Responsibility                                                |
|-------------------|---------------------------------------------------------------|
| `contextMenu.go`  | `Controller` and public API (Open, Close, IsOpen, mount)      |
| `types.go`        | `Item`, `panelState`, `listenerBinding`, small state helpers  |
| `renderer.go`     | DOM builders: `buildPanelHTML`, `buildList`, `renderItem`     |
| `events.go`       | DOM event wiring: click, mouseover, keydown, pointer detect   |
| `anchor.go`       | Placement math: world→screen, side decision, viewport clamp   |
| `style.go`        | Single CSS constant, idempotent `injectCSS`                   |

**Size cap, by project convention:** no file in this package may
exceed 400 lines. If a file approaches the cap, split by
responsibility — do not grow.

---

## Conventions this package follows

- **Caller pre-translates `Label`.** The package never calls
  `translate.T()` on `Label`. The caller has already done it. The
  one exception is the internal "Back" row label and the preview
  hint placeholder — package-owned strings use `translate.T()`
  internally.
- **`HelpKey` is resolved by the package.** Markdown blocks are too
  large to ask every caller to pre-fetch. If `HelpKey` is set, the
  renderer calls `translate.T(HelpKey, HelpFallback)`. If not, it
  uses `HelpFallback` verbatim.
- **150 ms delay between `Close` and `OnClick`.** Matches the
  legacy `SafeRun` contract. Creating canvas elements while a DOM
  menu is being torn down caused jank in the hex era; the delay
  stays. Callers do NOT wrap their own callbacks with delays.
- **Only one menu visible at a time per controller.** `Open` while
  already open silently closes the previous menu first. No
  animations, no confirmations.
- **No backdrop dimming.** The overlay is a transparent
  click-catcher — tapping outside the panel closes the menu, but
  the canvas behind stays fully visible. This is Variant A of the
  redesign.
- **Every `addEventListener` is matched by a `removeEventListener`
  at `Close` time.** Document-level listeners (Escape) would leak
  or panic without it; see the comment on `listenerBinding`.

---

## What this package does NOT do

- **Tutorial highlighting.** The tutorial is
  [Delivery C](../../docs/NEW_CONTEXT_MENU.md) and will be authored
  from the control panel. No `StartTutorial`-equivalent method
  lives here today. If you need one, extend deliberately — do not
  bolt a prototype on.
- **Main menu sidebar.** That is `ui/mainMenu`. Different
  lifecycle, different z-index, different visual language (three
  columns, not two). Do not merge the two.
- **World↔screen for anything other than anchor placement.** The
  controller reads the camera once per `Open`. Scroll, zoom, and
  pan are frozen from the menu's perspective for its lifetime —
  by the time the user closes the menu and re-opens, the camera
  is consulted again.

---

## How to add a new action to every device's backend menu

1. Add a factory in `ui/mainMenu/menuItens.go` (e.g. `DuplicateItem`)
   returning a `contextMenu.Item` with the translated `Label`,
   `HelpKey`/`HelpFallback`, and the standard icon from `rulesIcon`.
2. Append it to `getBodyMenuItems()` / `bodyMenuItems()` in every
   device that should expose the action.
3. Seed the translation keys in `translate` bundles (en, pt-br).

No changes in this package are needed — the linear layout accepts
any number of items up to the scroll limit imposed by `.cm-list`'s
`overflow-y: auto`.

---

## Integration with the workspace

`stageWorkspace/workspace.go` constructs one `Controller` right
next to where it constructs the `SpriteHexMenu` (both live during
the hybrid period of Delivery A/B). The controller is a field on
`DeviceFactory` and is injected into devices via `SetContextMenu`
in each `Create*` method.

During the hybrid period a device may have **both** `SetHexMenu`
and `SetContextMenu` called — the hex menu is still used for port
menus in some devices and for the output-click tutorial in
`StatementAdd`. Delivery B removes `SetHexMenu` from every device
and eventually from the factory.

---

## Related documents

- `/ide/docs/NEW_CONTEXT_MENU.md` — master plan with locked
  decisions, checklist, gotchas, and the inventory of which device
  has which menu today.
- `/ide/docs/REDESIGN_HEX_MENU_TO_TABLET.md` — original briefing.
- `/ide/CLAUDE.md` — project-wide rules (naming, translation,
  Catppuccin palette, SafeRun contract).
- `/ide/hexMenu/readme.md` — what stays from the hex package (the
  main-menu tutorial rendering).
- `/ide/ui/mainMenu/panel.go` — the sidebar whose visual language
  this package mirrors. Reference only; do not import from.
