# devices/compBlackBox — Generic visual devices for black-box components

## What this package does

Provides two generic device types that render **any** black-box component on the
IDE canvas without knowing the component at compile time. The component definition
arrives at runtime from the server (`GET /api/v1/blackbox`) and the devices
configure themselves dynamically from it.

```
Hardware menu → user picks "Test → Init"
    → factory creates StatementBlackBoxInit
    → device reads BlackBoxDefClient.Init ports
    → renders SVG with those ports
    → registers connectors in the wire manager
    → opens Inspect panel on click
```

## Files

| File                       | Lines | Responsibility                                                                                    |
|----------------------------|-------|---------------------------------------------------------------------------------------------------|
| `statementBlackBoxInit.go` | 721   | Visual device for the Init() method — SVG rendering, wire connectors, Inspect panel, scene export |
| `statementBlackBoxRun.go`  | 572   | Same as Init but for Run() — no editable properties (those live on Init)                          |
| `godocMarkdown.go`         | 74    | `buildGodocMarkdown()` — generates a Markdown help card from Go doc comments                      |

## Visual anatomy

```
┌─────────────────────────────┐  ← bbHeaderH = 22px
│         Test Init           │  ← component name + method
├─────────────────────────────┤  ← divider line
●  i2c                    err ●  ← input ports (left) / output ports (right)
●  gain                       │  ← circle: bbConnR = 5px
└─────────────────────────────┘
testInit_1                        ← instanceId label below device
```

Port circle colours: blue = numeric, teal = pointer/bus, purple = bool,
yellow = string, red = error. Defined in `portColor()`.

## Port vertical positioning

All ports use `portCY(i int) float64`:

```go
func portCY(i int) float64 {
    return bbHeaderH + float64(i)*bbPortRowH + bbPortRowH/2
}
```

The `+bbPortPad` margin is added to `bodyH` during `Init()` so the last
connector circle never clips the border regardless of port count.

## instanceId — the glue between Init and Run

Both devices for the **same component** share one `instanceId`. This is the
variable name used in the generated code:

```go
var test1 Test         // instanceId = "test_1"
_ = test1.Init()       // Init device
c := test1.Run(a, b)   // Run device — same test1
```

The factory (`factoryDevice/factoryBlackBox.go`) caches the instanceId per
struct name so Init and Run placed in separate menu clicks receive the same id.

## Inspect panel

**Init device tabs:**

| Tab        | Content                                                                    |
|------------|----------------------------------------------------------------------------|
| Properties | Label (editable) + configurable `prop` fields                              |
| Help       | Manual pages (`showIn:"init"` or `"both"`) + `[go doc]` card (always last) |

**Run device tabs:**

| Tab        | Content                                                                   |
|------------|---------------------------------------------------------------------------|
| Properties | Label only (props are on Init)                                            |
| Help       | Manual pages (`showIn:"run"` or `"both"`) + `[go doc]` card (always last) |

Doc and method description fields were intentionally moved out of Properties —
they belong in Help where there is enough space to read them comfortably.

## godocMarkdown — automatic Help content

`buildGodocMarkdown(componentName, componentDoc, methodName, methodDoc)` is
called after building explicit manual page cards. It always appends a `[go doc]`
card at the end of the Help tab using the doc comments already in the source file.

The method doc is rendered inside a `` ``` `` code fence so IDS tags
(`connection:mandatory`, `range:`, `unit:`) are visible as preformatted text —
matching what `go doc` would show.

## SVG layout constants

All constants are in `statementBlackBoxInit.go` (shared by both devices via the
same package):

```go
const (
    bbWidth    = 160.0  // device width
    bbHeaderH  = 22.0   // header height
    bbPortRowH = 22.0   // height per port row
    bbMinBodyH = 44.0   // minimum body height
    bbConnR    = 5.0    // connector circle radius
    bbConnLeft = 8.0    // input connector X position
    bbPortPad  = bbConnR + 2.0  // bottom padding so last circle never clips
    bbFontSize = 10     // port label font size
    bbHeaderFS = 11     // header text font size
)
```

Change these values to adjust the visual layout for all black-box devices at once.

## Scene export

Both devices implement `GetDeviceType()` which the scene serializer uses to
identify them in the exported JSON:

- Init: `"BlackBoxInit:StructName"` (e.g. `"BlackBoxInit:Test"`)
- Run: `"BlackBoxRun:StructName"`

The codegen server reads these prefixes in `server/codegen/ir/emit.go` to route
each device to the correct IR instruction (`BB_INIT` or `BB_RUN`).

`GetProperties()` persists `instanceId` and `executionOrder` in the scene JSON
so the codegen server can reconstruct the full picture from the exported scene.

## Adding a new port-level feature

Example: adding a `unit:` tooltip on hover.

1. Add `Unit string` to `PortDefClient` in `blackbox/clientTypes.go`.
2. Populate it in `server/handler/blackboxapi/handler.go` `toClientFuncDef()`.
3. Use it in `RegisterConnectors()` and/or `renderSVG()` in this package.
