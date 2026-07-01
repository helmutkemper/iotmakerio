# rulesDevice — IDE Visual Design System

`rulesDevice` is the **single source of truth** for every visual decision in
the IoTMaker IDE canvas.  If you want to know "what color is a `float32`
connector?" or "how tall is the device header?", this package is where you look.

---

## Quick start

```
cp rulesDevice/palette.go → read it top-to-bottom: constants, then TypeStyleFor()
```

Two files in this package:

| File         | Purpose                                            |
|--------------|----------------------------------------------------|
| `palette.go` | All colors, sizes, typography — full design system |
| `rules.go`   | SVG label format string and label height constant  |

---

## The core principle: color = type

**The border color of a device and the fill of its connector dot always match
the Go data type that connector carries.**

This means:

- If two connectors share the same color, they are compatible.
- A maker can learn the color system once and use it everywhere.
- No tooltip or popup is needed to know if a connection is valid.

```
int / int64  →  Blue   #5599FF
int32        →  Blue   #3377EE  (darker)
float32      →  Green  #44CC88
float64      →  Teal   #55DDAA
bool         →  Orange #FF8833
string       →  Amber  #FFCC33
error        →  Red    #FF3333  (wire stroke is 3px, not 1.5px)
byte/[]byte  →  Purple #AA88FF
```

The full mapping lives in `TypeStyleFor(goType string) TypeStyle`.
**Always call this function** instead of embedding hex values in device code.

---

## Geometry reference

```
Device ornament (all types):
  Border width:    KDeviceBorderWidth   = 2px
  Corner radius:   KDeviceCornerRadius  = 5px
  Header height:   KDeviceHeaderHeight  = 18px

Constant devices (no inputs):
  Default width:   KConstDefaultWidth   = 120px
  Default height:  KConstDefaultHeight  = 56px  (ornament only)
  Min width:       KConstMinWidth       = 80px
  Min height:      KConstMinHeight      = 44px
  Total height:    ornament + KLabelHeight = 56 + 18 = 74px

Connectors:
  Dot radius:      KConnectorRadius     = 5px
  Hit radius:      KConnectorHitRadius  = 10px  (larger than visual dot)
  Right offset:    KConnectorOffsetRight= 8px   (from right edge to center)
  Left offset:     KConnectorOffsetLeft = 8px   (from left edge to center)
  Stroke:          KColorConnectorStroke= #FFFFFF (always white)

Label:
  Height:          KLabelHeight         = 18px
  Font size:       KDeviceFontSizeLabel = 12px
  Color:           KColorDeviceTextMuted= #8899AA
```

---

## Typography

| Constant                   | Value             | Used for                        |
|----------------------------|-------------------|---------------------------------|
| `KDeviceFontFamily`        | Arial,sans-serif  | All SVG text                    |
| `KDeviceFontSizeTypeTag`   | 10px              | Header tag (INT, BOOL, F64…)    |
| `KDeviceFontSizeValue`     | 16px bold         | Main value display              |
| `KDeviceFontSizePort`      | 11px              | Port labels (black-box devices) |
| `KDeviceFontSizeLabel`     | 12px              | Editable label below device     |

---

## Background colors

| Constant                | Value     | Used for                   |
|-------------------------|-----------|----------------------------|
| `KColorDeviceBg`        | `#1a1e2e` | Main device body fill      |
| `KColorDeviceHeader`    | `#252a3e` | Header bar fill            |
| `KColorDeviceDivider`   | `#323854` | Header/body separator line |
| `KColorDeviceText`      | `#DDEEFF` | Primary value text         |
| `KColorDeviceTextMuted` | `#8899AA` | Port names, labels         |

---

## How to add a new type color

1. Open `palette.go`.
2. Add a `const KColorTypeXxx = "#RRGGBB"` in the type color section.
3. Add a case to `TypeStyleFor()`.
4. Document the new type in the table in this readme.

No other file needs to change — all devices call `TypeStyleFor()` at render
time and will pick up the new color automatically.

---

## Error type — special treatment

Connectors and wires that carry the `error` type are rendered in **red** with
a **3px stroke** (versus 1.5px for normal wires).  The heavier line makes error
paths impossible to miss on a busy canvas.

This is the only type that has special non-color treatment.  The wire manager
must apply the 3px rule when creating error-type wire segments.

---

## Device hierarchy and z-index

| Category    | z-index constant                   | Examples           |
|-------------|------------------------------------|--------------------|
| Container   | `rulesZIndex.Container` (10)       | Loop               |
| Math        | `rulesZIndex.Math`    (20)         | Add, Sub, Mul, Div |
| Constant    | `rulesZIndex.Constant`(30)         | ConstInt, Bool, …  |
| Wire        | `rulesZIndex.Wire`    (50)         | wire segments      |
| UI          | `rulesZIndex.UI`      (100)        | overlays           |
| Menu button | `rulesMainMenu.ButtonZIndex` (900) | toolbar button     |
| Hex menu    | `rulesMainMenu.MenuZIndex`  (1000) | menu + backdrop    |

Constant devices sit above math devices so their compact boxes stay visible
when the canvas is dense.
