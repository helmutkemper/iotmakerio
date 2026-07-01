# devices/compConsts

Constant value devices for the IoTMaker IDE canvas.

A **constant device** has no input connectors.  It emits a fixed, user-configured
value through a single output connector.  The value is set at design time via the
Inspect panel or by double-clicking the device.

---

## Devices in this package

| File | Type | Go type | Border color |
|------|------|---------|--------------|
| `statementConstInt.go`    | `StatementConstInt`    | `int64`           | Blue `#5599FF`  |
| `statementBool.go`        | `StatementBool`        | `bool`            | Orange `#FF8833`|
| `statementConstFloat.go`  | `StatementConstFloat`  | `float32`/`float64`| Green/Teal    |
| `statementConstString.go` | `StatementConstString` | `string`          | Amber `#FFCC33` |

---

## Visual design system

All constant devices share the same geometry.  Colors are defined in
`rulesDevice/palette.go` and must not be hardcoded inside device files.

### Layout (all devices)

```
┌─────────────────────┐  ← KDeviceBorderWidth (2px), KDeviceCornerRadius (5px)
│ TAG             ◉   │  ← KDeviceHeaderHeight (18px); ◉ = output connector
├─────────────────────┤  ← divider (KColorDeviceDivider)
│                     │
│       value         │  ← KDeviceFontSizeValue (16px) bold, centered
│                     │
└─────────────────────┘  ← ornament height = KConstDefaultHeight (56px)
deviceLabel              ← KDeviceLabel format, KLabelHeight (18px) below ornament
```

### Default sizes

| Constant              | Value |
|-----------------------|-------|
| `KConstDefaultWidth`  | 120px |
| `KConstDefaultHeight` | 56px  |
| `KConstMinWidth`      | 80px  |
| `KConstMinHeight`     | 44px  |

Total element height = ornament height + `KLabelHeight` (18px) = **74px**.

### Border / connector color = output type

The border and the output connector dot always use the color of the Go type
being emitted.  This is the main visual cue that allows makers to match
compatible connectors at a glance.  See `rulesDevice.TypeStyleFor()` for the
full mapping.

---

## Menu behavior

**Body click** opens a hex menu with two items:

| Item    | Position | Action                              |
|---------|----------|-------------------------------------|
| Inspect | (1, 1)   | Opens the Inspect overlay           |
| Delete  | (1, 3)   | Removes the device from the canvas  |

Resize is **not available** on constant devices — they are intentionally compact
and their visual size does not affect the output value.

**Connector click** (output dot) opens a one-item menu:

| Item    | Position | Action                              |
|---------|----------|-------------------------------------|
| Connect | (1, 1)   | Starts visual wire-connect mode     |

The user disconnects from the **receiving** end, not from the constant's output.

**Double-click** opens the Inspect overlay directly (shortcut for experienced
users).

### Ghost menu fix

All click handlers follow this pattern to prevent the "ghost menu" bug:

```go
if e.hexMenu.IsVisible() || e.hexMenu.WasJustClosed() {
    if e.hexMenu.IsVisible() {
        e.hexMenu.Close()
    }
    return
}
```

**Why this is needed:** when the menu is open and the user clicks the device
body, the backdrop (z-index 1000) fires `Close()` before the device's own click
handler runs.  By the time the device handler fires, `IsVisible()` is already
`false` — causing the device to open a new menu immediately (the "ghost").
`WasJustClosed()` returns `true` for 100 ms after `Close()`, blocking the
re-open.

---

## StatementConstFloat — precision selection

`StatementConstFloat` can emit either `float32` or `float64`.  The precision
is selected in the Inspect → Properties tab via a dropdown.

When precision changes:
1. The border and connector dot color update (F32 = green, F64 = teal-green).
2. The type tag in the header changes (`F32` / `F64`).
3. The connector is **re-registered** with the new type.  Existing wires that
   connected to a `float32` output will be **incompatible** with a `float64`
   input — the wire manager handles this.

**Why two precision options?**  The RP2040 microcontroller has a hardware FPU
for `float32` but emulates `float64` in software (much slower).  Makers
targeting RP2040 should prefer `float32` unless they need the extra range.

---

## Adding a new constant type

1. Copy `statementConstInt.go` as a starting point.
2. Change the struct name and `GetDeviceType()` return value.
3. Call `rulesDevice.TypeStyleFor("yourtype")` to get the correct color.
4. Add the new Go type string to `rulesDevice.TypeStyleFor()` if it is not
   already present.
5. Register the device in `factoryDevice/factory.go`.
6. Update this readme.
