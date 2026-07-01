# IoTMaker Doc Standard (IDS)

> The documentation standard for **black-box** components in the IoTMaker IDE.
> Compatible with `go doc` — extends Go comments with inline tags without
> breaking any existing tooling.

---

## 1. General comment structure

```go
// FunctionName does X.  (summary line — required, used by go doc)
//
// Optional long description.
// May span multiple lines; follows godoc rules.
//
// Markdown is supported for formatting.
//
// Params
//   param1: description.  tag1:value.  tag2:value.
//   param2: description.  tag1:value.
//
// Returns
//   ret1: description.  tag1:value.
//   ret2: description.  connection:optional.
func (s *Device) FunctionName(param1 type, param2 type) (ret1 type, ret2 type) {}
```

### Syntax rules

| Rule                          | Detail                                                |
|-------------------------------|-------------------------------------------------------|
| `Params` / `Returns` sections | A line containing only the keyword, without a colon   |
| Parameter entry               | `name: description.  tag:value.  tag:value.`          |
| Tags                          | Always `key:value` terminated with `.` or end of line |
| Tag order                     | Free — the parser identifies by prefix, not position  |
| Compatibility                 | `go doc` ignores tags and displays the text normally  |

---

## 2. Complete tag reference

### Port / parameter tags (inside `Params` / `Returns` sections)

| Tag           | Syntax                    | Purpose                                                                                               |
|---------------|---------------------------|-------------------------------------------------------------------------------------------------------|
| `range:`      | `min..max`                | Restricts numeric values to a closed interval                                                         |
| `range_min:`  | `value`                   | Lower bound only (no upper bound)                                                                     |
| `range_max:`  | `value`                   | Upper bound only (no lower bound)                                                                     |
| `unit:`       | text                      | Informational measurement unit                                                                        |
| `options:`    | `a\|b\|c`                 | Enum: explicitly listed accepted values                                                               |
| `default:`    | value                     | Default value suggested when the port is not connected                                                |
| `connection:` | `optional` \| `mandatory` | **Required.** Defines whether the port may remain disconnected. The parser warns when absent.         |
| `encoding:`   | scheme                    | How to interpret the base type (e.g. `tristate`, `bitmask`)                                           |
| `bits:`       | `N` or `N..M`             | How many bits of the value are used / which slice                                                     |
| `inputRegex`  | js regex                  | Browser-compatible regex used to validate data typed in the IDE UI                                    |

### Visual / machine directives (in struct or method doc comments)

These tags appear in the **Go doc comment** of a struct or method declaration,
**not** inside `Params` / `Returns` sections. All follow the `key:value.` format
and are stripped from the human-readable `Doc` field — `go doc` ignores them.

| Directive         | Syntax                       | Applies to     | Purpose                                                                            |
|-------------------|------------------------------|----------------|------------------------------------------------------------------------------------|
| `icon:`           | `icon:name.` or `icon:f287.` | struct, method | Icon in the block header and hex menu. See §6 for formats.                         |
| `label:`          | `label:text.`                | struct, method | Human-readable display name. Combined as `{StructLabel} {MethodLabel}` in headers. |
| `executionOrder:` | `executionOrder:N.`          | method only    | Relative run order hint (positive integer). 0 = unordered.                         |
| `menu:`           | `menu:col,row.`              | method only    | Explicit hex-menu position. See §9.                                                |

---

## 3. Native Go types — examples

### `bool`

```go
// SetEnable activates or deactivates the component.
//
// Params
//   enabled: true = on, false = off.  default:false.  connection:optional.
//
// Returns
//   ok: true if the command was accepted.  connection:optional.
func (s *Device) SetEnable(enabled bool) (ok bool) {}
```

### `int` / `int8` / `int16` / `int32` / `int64`

#### Numeric range

```go
// SetVolume adjusts the output volume.
//
// Params
//   level: volume level.  range:0..100.  unit:percent.  default:50.  connection:mandatory.
func (s *Speaker) SetVolume(level int) {}
```

#### Tristate (`encoding:tristate`)

```go
// SetState sets the logical state of the pin.
//
// Params
//   state: logical state.  encoding:tristate.  options:-1|0|1.  connection:mandatory.
//          -1 = false, 0 = undefined (high-impedance), 1 = true.
//
// Returns
//   err: write error.  connection:optional.
func (s *GPIO) SetState(state int) (err error) {}
```

#### Bitmask (`encoding:bitmask`)

```go
// SetFlags configures combined behaviours via bitmask.
//
// Params
//   flags: configuration bit combination.  encoding:bitmask.  bits:3.  connection:mandatory.
//          bit0 = enable, bit1 = invert, bit2 = latch.
func (s *Device) SetFlags(flags int) {}
```

### `uint` / `uint8` / `uint16` / `uint32` / `uint64`

```go
// SetBrightness adjusts LED brightness.
//
// Params
//   value: brightness intensity.  range:0..255.  unit:pwm.  default:128.  connection:mandatory.
func (s *LED) SetBrightness(value uint8) {}
```

### `float32` / `float64`

```go
// ReadTemperature reads ambient temperature.
//
// Returns
//   celsius: temperature in degrees Celsius.  range:-40.0..125.0.  unit:celsius.  connection:optional.
//   ok: true if the reading is valid.  connection:optional.
func (s *TempSensor) ReadTemperature() (celsius float32, ok bool) {}
```

### `string`

```go
// SetLabel sets the text displayed on screen.
//
// Params
//   text: text to display.  range_max:20.  unit:chars.  default:"IoTMaker".  connection:optional.
func (s *Display) SetLabel(text string) {}
```

### `[]byte`

```go
// Write sends a data buffer over SPI.
//
// Params
//   buf: data to transmit.  range_min:1.  range_max:512.  unit:bytes.  connection:mandatory.
//
// Returns
//   n:   bytes actually sent.   connection:optional.
//   err: transmission error.    connection:optional.
func (s *SPI) Write(buf []byte) (n int, err error) {}
```

### `error`

```go
// Returns
//   err: init error.  connection:mandatory.   ← IDE blocks if disconnected
//   err: read error.  connection:optional.    ← may be ignored
```

### Pointers (`*T`)

```go
// Init configures the sensor on the I2C bus.
//
// Params
//   i2c: I2C bus reference.  connection:mandatory.  unit:i2c_bus.
//        Connect to the output of the I2CBus.Init block.
//
// Returns
//   err: init error.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) {}
```

---

## 4. Complete example — APDS-9960 colour sensor

```go
// Package blackbox
//
// APDS9960 — Colour, proximity and gesture sensor via I2C (AMS).
package blackbox

import "machine"

// APDS9960 reads colour (RGBC) data via I2C.
//
// icon:greater-than-equal. label:APDS9960.
type APDS9960 struct {
	gain  byte `prop:"Gain"             default:"0"   options:"0,1,2,3"`
	atime byte `prop:"Integration Time" default:"255" range:"0,255"`
}

// Init configures the APDS-9960 sensor.
//
// icon:gear. label:Init. menu:-1,-1.
//
// Params
//   i2c: I2C bus.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: init error.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) { return nil }

// Run reads the four RGBC colour channels.
//
// executionOrder:10. icon:greater-than-equal. label:Read RGBC. menu:1,-1.
//
// Returns
//   clear: total light.  range:0..65535.  unit:lux_counts.    connection:optional.
//   red:   red channel.  range:0..65535.  unit:color_counts.  connection:optional.
//   green: green channel. range:0..65535. unit:color_counts.  connection:optional.
//   blue:  blue channel.  range:0..65535. unit:color_counts.  connection:optional.
func (s *APDS9960) Run() (clear, red, green, blue uint16) { return }
```

---

## 5. `connection:` tag — required on every port

| Value                  | Visual symbol | Meaning                                                        |
|------------------------|---------------|----------------------------------------------------------------|
| `connection:optional`  | ◎             | Port may remain disconnected. IDE does not block execution.    |
| `connection:mandatory` | ◉             | Port must be connected. IDE blocks execution if empty.         |

---

## 6. `icon:` — three formats accepted

### Format 1 — Name (kebab-case) ✅ Recommended

```
icon:greater-than-equal.
icon:gear.
icon:play.
icon:usb.
```

Uses a pre-registered SVG path from `rulesIcon/iconRegistry.go`. Works in
**both** the WASM IDE blocks and the SPA preview.

**Fallback**: `gear` at struct level, `play` for named methods.

To use a name not yet in the registry: add the SVG `d` path constant to
`rulesIcon/falcons.go` and register the name→path mapping in
`rulesIcon/iconRegistry.go`.

### Format 2 — Unicode codepoint (hex)

```
icon:f287.       ← FA Solid  (font-weight 900, the default)
icon:\uf287.     ← same, Go unicode escape accepted
icon:0xf287.     ← same, 0x prefix accepted
icon:f287:b.     ← FA Brands (font-weight 400, logo icons: USB f287, GitHub f09b)
icon:f287:r.     ← FA Regular (font-weight 400, outline style)
```

The codepoint is found on the FontAwesome icon page under "Unicode":
`https://fontawesome.com/icons/usb` → `f287`

> **⚠ WASM limitation** — Unicode codepoints work only in the **SPA preview**
> (HTML inline context). In the WASM IDE blocks and hex menu, the SVG is
> rendered as a bitmap via a Blob URL, which **cannot access CSS webfonts**
> loaded by `<link>` in the page. A unicode codepoint in the WASM context
> silently falls back to the `gear` icon.
>
> **If you need a specific icon in the WASM IDE**, add its SVG path to
> `rulesIcon/falcons.go`, register it in `rulesIcon/iconRegistry.go`, and use
> Format 1 (`icon:name.`).

### Format 3 — Omitted

When `icon:` is absent on a method, the IDE uses the struct-level `StructIcon`.
When that is also absent: `gear` for structs, `play` for named methods.

---

## 7. `label:` — human-readable display name

```
label:APDS9960.
label:serial log.
label:Read RGBC.
```

- On a **struct**: first word of the visual block header.
- On a **method**: second word, combined with StructLabel → `"APDS9960 serial log"`.
- **Fallback**: struct name / Go method name.

---

## 8. `executionOrder:` — run order hint

```
executionOrder:10.
executionOrder:20.
```

Positive integer. Lower runs first when methods are not connected by wires.
`0` or absent = unordered (runs after all ordered ones).
Integers do not need to be contiguous.

---

## 9. `menu:` — explicit hex-menu position

### Syntax

```
menu:col,row.
```

- `col` and `row` are **signed integers**.
- They are **offsets relative to the Back button center `(0,0)`**.
- `(0,0)` is reserved for Back — **do not use it**.
- Absolute position: `absCol = BackCenterCol + col`, `absRow = BackCenterRow + row`.
	Default centre: `BackCenterCol = 2`, `BackCenterRow = 2`.
- When absent: the IDE assigns the next available radial slot automatically.

### Radial priority ring (automatic, slots 0–5)

```
        slot 4: (0,-2)
slot 0: (-1,-1)     slot 2: (+1,-1)
           [Back]
slot 1: (-1,+1)     slot 3: (+1,+1)
        slot 5: (0,+2)
```

Beyond slot 5, the ring expands to `(-2,-2)`, `(-2,0)`, `(-2,+2)`, …
When the total number of items exceeds `MaxItemsPerPage` (default: 6), the
last slot on each page becomes a **"More →"** submenu.

### Example

```go
// Init sets up the device.
// menu:-1,-1.
func (s *Sensor) Init(...) {}   // → absolute (1, 1)

// Run reads data.
// menu:1,-1.
func (s *Sensor) Run() {}       // → absolute (3, 1)

// Log writes debug output.
// (no menu: — auto-placed at next ring slot)
func (s *Sensor) Log() {}
```

### Rules

| Rule                | Detail                                                                                        |
|---------------------|-----------------------------------------------------------------------------------------------|
| `(0,0)` is reserved | Back always occupies the center.                                                              |
| Uniqueness          | Each method must use a different `(col,row)` pair. Collisions are not detected at parse time. |
| Mixed placement     | Explicit items claim their slots first; auto-placed items fill the remaining ring slots.      |
| Overflow            | Items beyond `MaxItemsPerPage` are moved to "More →" regardless of position type.             |

---

## 10. Manual pages — embedded Markdown `/* */`

```go
/*
manualName:wiring-guide.
language:en.
showIn:both.
```markdown
# Wiring Guide

| Pin | Connect to |
|-----|------------|
| VCC | 3.3 V      |
| SDA | GPIO 0     |
```*/
```

| Directive     | Values                                  | Default  |
|---------------|-----------------------------------------|----------|
| `manualName:` | identifier (e.g. `wiring-guide`)        | required |
| `language:`   | BCP-47 code (`en`, `pt-br`, …)          | `en`     |
| `showIn:`     | `init`, `both`, or any method name      | `both`   |

The closing sequence ` ```*/` must be the last non-empty line in the block.

---

## 11. `go doc` compatibility

All machine directives are extracted and stripped from `Doc` before storage.
`go doc` displays the text as normal prose — no tags visible.

---

## 12. Architectural note — single source of truth for IDS parsing

> **TODO (future work):** the IDS directive extraction logic exists in two places:
>
> - `server/blackbox/parser.go` (legacy/SPA parser)
> - `server/codegen/blackbox/parser.go` (codegen/WASM parser)
>
> Both must implement identical rules for `icon:`, `label:`, `executionOrder:`,
> and `menu:`. A future refactoring should extract a shared `server/blackbox/ids`
> package imported by both parsers to eliminate duplication.
