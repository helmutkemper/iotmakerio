# blackbox — Client-side black-box type definitions (WASM IDE)

## What this package does

This package contains the **WASM-side mirror** of the server's black-box types.
It is not compiled into the server — it runs entirely inside the browser as WebAssembly.

Two responsibilities:

1. **Type definitions** (`clientTypes.go`) — lightweight structs that match the
	 JSON returned by `GET /api/v1/blackbox`. These types drive the visual rendering
	 of black-box devices on the IDE canvas.

2. **Server fetch** (`loader.go`) — a blocking HTTP call, made once at startup,
	 that populates the Hardware submenu with all black-box components saved in the
	 server database.

## Files

| File             | Lines | Responsibility                                                                                                      |
|------------------|-------|---------------------------------------------------------------------------------------------------------------------|
| `clientTypes.go` | 163   | `BlackBoxDefClient`, `FuncDefClient`, `PortDefClient`, `PropDefClient`, `ManualPageClient` and their helper methods |
| `loader.go`      | 153   | `LoadDefs()` — fetches `GET /api/v1/blackbox` and returns `[]*BlackBoxDefClient`                                    |

## How it fits in the project

```
server/handler/blackboxapi   GET /api/v1/blackbox  →  JSON array
                                      ↓
blackbox/loader.go           LoadDefs() fetches during splash screen
                                      ↓
blackbox/clientTypes.go      []*BlackBoxDefClient in memory
                                      ↓
ui/mainMenu/menuBuilder.go   SetBlackBoxDefs() → Hardware submenu
                                      ↓
factoryDevice/               CreateBlackBoxInit() / CreateBlackBoxRun()
                                      ↓
devices/compBlackBox/        Visual blocks on the canvas
```

## Why separate from the server types

The server uses `server/codegen/blackbox.BlackBoxDef` which carries heavy fields
(`StructCode`, `MethodsCode`, `Imports`) needed only for code generation. The
client types intentionally omit these — the WASM binary only needs port names,
types, labels, and manual page content to render the visual blocks and populate
the Inspect panel.

## LoadDefs — the fetch bridge

WASM has no blocking HTTP call. `LoadDefs()` uses the JS Promise → Go channel
bridge pattern:

```
js.FuncOf callbacks → buffered channel (size 1) → <-ch blocks goroutine
```

`Init()` in `stageWorkspace` is always called from a goroutine, so blocking is
safe. On network failure or empty response, `LoadDefs()` returns `nil` and the
Hardware submenu shows "No devices" — the IDE stays fully functional.

## Key types

### BlackBoxDefClient

The top-level type for one component. `Init` and `Run` are both optional
pointers — a device may have only one.

```go
type BlackBoxDefClient struct {
    Name        string
    Doc         string             // package-level comment
    Init        *FuncDefClient     // nil for pure-Run devices
    Run         *FuncDefClient     // nil for Init-only devices
    Props       []PropDefClient    // configurable fields (Inspect panel)
    ManualPages []ManualPageClient // documentation pages from /* */ blocks
}
```

### PagesFor(device string) []ManualPageClient

Filters manual pages by `showIn` tag. Called when building Inspect panel tabs:

```go
initCards := def.PagesFor("init") // showIn:"init" or showIn:"both"
runCards  := def.PagesFor("run")  // showIn:"run"  or showIn:"both"
```

## Adding a new field from the server

1. Add the field to `clientTypes.go` with a `json:` tag matching the server DTO.
2. Add the field to `clientManualPage` (or the appropriate DTO) in
	 `server/handler/blackboxapi/handler.go`.
3. Populate it in `toClientDef()` in the same file.

No other files need to change.
