# IoTMaker Doc Standard (IDS) — Complete Reference

> A documentation standard for **black-box** components in the IoTMaker IDE.
> Fully compatible with `go doc` — IDS tags live inside normal Go comments and
> are invisible to every standard Go tool.

---

## What is a black-box?

A **black-box** is a Go struct that a specialist writes once, publishes to
GitHub, and that any maker can then drag onto the IDE canvas as a ready-to-wire
visual block — without reading a single line of code.

The IDE renders the struct as a dark rounded rectangle with labelled connector
pins on each side. Left-side pins are inputs (method parameters); right-side
pins are outputs (return values). The maker connects them with virtual wires,
and the IDE generates valid Go code automatically.

---

## Quick start — the simplest valid black-box

```go
// Package mydevice
//
// Sum adds two integers.
package mydevice

// Sum is a stateless adder with no hardware setup required.
type Sum struct{}

// Run returns the sum of a and b.
//
// Params
//   a: first operand.   connection:mandatory.
//   b: second operand.  connection:mandatory.
//
// Returns
//   c: a + b.  connection:mandatory.
func (s *Sum) Run(a, b int) (c int) {
	return a + b
}
```

Publish this as a GitHub release, submit the URL in the IDE, and the "Sum" block
appears under **Hardware → Sum → Run** on any IDE canvas.

---

## Repository layout

```
your-repo/
├── my_sensor.go          ← Go code with ONE exported struct + IDS tags
├── readme.md             ← Device overview (English, auto-detected)
├── readme.pt-br.md       ← Device overview (Portuguese)
├── init.en.md            ← Init help tab (English)
├── init.pt-br.md         ← Init help tab (Portuguese)
├── run.en.md             ← Run help tab (English)
├── rp2040.svg            ← Interactive diagram (optional)
└── wiring.png            ← Regular image (optional)
```

Rules:
- Exactly **one exported struct** per `.go` file.
- At least **one** of `Init()` or `Run()` must exist.
- All method parameters and return values must use **native Go types** only.
- Every parameter and return value **must** have a `connection:` tag.
- All files go at the repository **root** (the parser finds them there).

---

## Methods

### Init()

`Init()` represents **one-time setup**: acquiring a bus, configuring registers,
opening a connection. The code generator places it in the global scope so it
runs **before** the main loop.

**Important:** if `Init()` exists in the component source, the maker **must**
place an Init device on the canvas. The code generator returns an error if
a Run device is present but its matching Init is missing.

```go
// Init configures the sensor on the given I2C bus.
//
// executionOrder:10. icon:hourglass-start. label:Init.
//
// Params
//   i2c: I2C bus reference.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: initialisation error.  connection:optional.
func (s *MySensor) Init(i2c *machine.I2C) (err error) { ... }
```

### Run()

`Run()` represents **per-iteration work**: reading a sensor, writing an output,
computing a value. The maker drags it inside a Loop block.

```go
// Run reads the sensor and returns the measured value.
//
// icon:bolt. label:Read.
//
// Returns
//   value: measured value.  range:0..4095.  connection:mandatory.
//   err:   read error.      connection:optional.
func (s *MySensor) Run() (value uint16, err error) { ... }
```

### Pure-Run devices (no Init)

A device with **only** `Run()` and an empty struct is valid. Use it for pure
functions — mathematical operations, conversions, encoders — that need no state.

---

## Variable declaration placement

The code generator applies a simple rule for where `var device X` appears:

| Condition                                                 | var placement                        |
|-----------------------------------------------------------|--------------------------------------|
| **Any** method placed **outside** the loop (global scope) | Top of `main()`, **before** the loop |
| **All** methods placed **inside** the loop                | Top of the loop body                 |

This rule is automatic — the specialist does not need to configure it.

---

## Execution order

By default, devices are ordered by **wire dependency**: if device A's output is
connected to device B's input, A runs before B. When two devices share no wire,
the order is unspecified.

For cases where order matters but no wire connects the devices, use
`executionOrder:`:

```go
// Init configures the I2C bus.
// executionOrder:1.
func (b *I2CBus) Init() (bus *machine.I2C, err error) { ... }

// Init configures the sensor using the I2C bus.
// executionOrder:2.
func (s *MySensor) Init(i2c *machine.I2C) (err error) { ... }
```

| Situation                   | Result                                |
|-----------------------------|---------------------------------------|
| Both have `executionOrder:` | Lower number runs first               |
| Only one has it             | Ordered device runs first             |
| Neither has it              | Wire dependency, then alphabetical ID |
| Numbers equal               | Alphabetical ID tiebreak              |

`executionOrder:` applies per-method. If a component has both `Init()` and
`Run()`, each carries its own order value independently.

---

## IDS tag reference

Tags are written in `//` comments in the `Params` or `Returns` section of a
method, on the same line as the parameter description.

```go
// Params
//   paramName: description.  tag1:value.  tag2:value.
```

### Syntax rules

| Rule                          | Detail                                                        |
|-------------------------------|---------------------------------------------------------------|
| `Params` / `Returns` sections | A line containing only the word (no colon) opens the section  |
| Tag format                    | `camelCase` key + `:` + value, followed by `.` or end-of-line |
| Tag order                     | Free — parser identifies tags by prefix, not position         |
| `go doc` compatibility        | Tags appear as plain text; no tool is broken                  |

### Port tags (on method parameters and return values)

| Tag           | Syntax                 | Required            | Description                                          |
|---------------|------------------------|---------------------|------------------------------------------------------|
| `connection:` | `connection:mandatory` | **Yes, every port** | `mandatory` or `optional`. Missing = parse warning   |
| `range:`      | `range:0..255`         | No                  | Closed numeric interval                              |
| `range_min:`  | `range_min:0`          | No                  | Lower bound only                                     |
| `range_max:`  | `range_max:100`        | No                  | Upper bound only                                     |
| `unit:`       | `unit:ms`              | No                  | Physical unit; IDE warns on incompatible connections |
| `default:`    | `default:128`          | No                  | Value used when the port is disconnected             |
| `options:`    | `options:a,b,c`        | No                  | Enum — shows dropdown in the IDE                     |
| `encoding:`   | `encoding:bitmask`     | No                  | `bitmask` or `tristate`                              |
| `bits:`       | `bits:0..3`            | No                  | Bit slice within a wider integer                     |

### Method-level directives (in the method doc comment, before Params)

| Directive         | Syntax                 | Description                                        |
|-------------------|------------------------|----------------------------------------------------|
| `executionOrder:` | `executionOrder:1`     | Relative execution position within a scope         |
| `icon:`           | `icon:hourglass-start` | FontAwesome icon name (kebab-case)                 |
| `label:`          | `label:Init`           | Human-readable display name for the block          |
| `menu:`           | `menu:-1,-1`           | Explicit hex-menu position offset from Back center |

### Struct-level directives (in the struct doc comment)

| Directive      | Syntax                | Description                                                                 |
|----------------|-----------------------|-----------------------------------------------------------------------------|
| `icon:`        | `icon:gear`           | FontAwesome icon for the component                                          |
| `label:`       | `label:APDS9960`      | Human-readable component name                                               |
| `interactive:` | `interactive:rp2040.` | SVG diagram filename without extension (see Section "Interactive diagrams") |

---

## Configurable properties — `prop` struct tag

Fields in the struct with a `prop` tag appear in the **Inspect panel** of the
Init device. They are not connector pins — the maker types or selects a value.

```go
type MySensor struct {
    addr    uint8  `prop:"I2C Address"       default:"0x39"  options:"0x39,0x29"`
    gain    byte   `prop:"Gain"              default:"0"     options:"0,1,2,3"`
    intTime byte   `prop:"Integration Time"  default:"255"`
}
```

| Struct field tag    | Purpose                                                         |
|---------------------|-----------------------------------------------------------------|
| `prop:"Label"`      | Human-readable name shown in the Inspect panel                  |
| `default:"value"`   | Pre-filled value                                                |
| `options:"a,b,c"`   | Renders a dropdown instead of text input                        |
| `connection:"ROLE"` | Links the prop to an interactive SVG diagram (see next section) |

---

## Interactive diagrams

An interactive SVG diagram can visualise the effect of property changes. When the
maker selects a pin, the diagram highlights that element.

### Enabling

1. Create an SVG following the Interactive Diagram Specification
   (see `docs/INTERACTIVE_DIAGRAM_SPEC.md`).
2. Place the SVG in the repository root (e.g. `rp2040.svg`).
3. Add the `interactive:` directive to the struct doc comment:

```go
// RP2040_I2C configures I2C on a Raspberry Pi Pico.
//
// icon:microchip. label:RP2040 I2C. interactive:rp2040.
type RP2040_I2C struct {
    sda  string `prop:"SDA Pin"   default:"GP4" options:"GP0,GP2,GP4,GP6" connection:"I2C_SDA"`
    scl  string `prop:"SCL Pin"   default:"GP5" options:"GP1,GP3,GP5,GP7" connection:"I2C_SCL"`
    freq int    `prop:"Frequency" default:"100000" options:"100000,400000"`
}
```

### How `connection:"ROLE"` works

- `ROLE` maps to a colour in the SVG's `data-palette` attribute.
- The prop **value** (what the maker selects, e.g. `"GP4"`) maps to a `data-id`
  attribute on an SVG element.
- When the maker changes the value, the diagram highlights the selected element
  with the role's colour and dims all others.
- Props **without** `connection:` (like `freq` above) are not linked to the
  diagram — they appear as normal text/dropdown inputs.

### SVG reference in markdown

Reference the SVG in any help markdown file:

```markdown
![](rp2040.svg)
```

The worker rewrites the path to a public URL automatically.

For full SVG creation details, see `docs/DIAGRAM_CREATION_GUIDE.md`.

---

## Markdown help files

Documentation for the IDE Inspect panel and Hardware menu is written as standard
markdown files in the repository root.

### File naming

| Pattern                  | Purpose                                  | Example                             |
|--------------------------|------------------------------------------|-------------------------------------|
| `readme.md`              | Device overview (English, auto-detected) | Device description in Hardware menu |
| `readme.{lang}.md`       | Device overview (other language)         | `readme.pt-br.md`                   |
| `init.{lang}.md`         | Init help tab                            | `init.en.md`, `init.pt-br.md`       |
| `run.{lang}.md`          | Run help tab                             | `run.en.md`                         |
| `{method}.{lang}.md`     | Any method help tab                      | `read.en.md`                        |
| `{method}.{N}.{lang}.md` | Additional tabs (numbered)               | `init.1.en.md`, `init.2.en.md`      |

Language codes follow BCP-47 lowercase: `en`, `pt-br`, `fr`, `ja`, etc.

### Ordering

When a method has multiple markdown files, they appear as sub-tabs:

- The unnumbered file (e.g. `init.en.md`) is always the first tab.
- Numbered files (e.g. `init.1.en.md`, `init.2.en.md`) follow in ascending order.

### Tab title

The title shown on the sub-tab is extracted from the first `# Heading` in the
markdown file. If no heading exists, the filename is used as fallback.

### Images in markdown

Reference images from the same repository root:

```markdown
![Wiring photo](wiring.png)
![Board diagram](rp2040.svg)
```

The worker rewrites bare filenames to public URLs. Images render inline in the
Help tab. All images are clickable — clicking opens a fullscreen lightbox.

Interactive SVGs (referenced via `interactive:` directive) are automatically
post-processed: elements are highlighted/dimmed based on the current prop values.

### Language resolution

The IDE selects which language to display using this priority:

1. Session preference (set by language selector in the Help tab)
2. User's SPA locale preference (localStorage `"locale"`)
3. Browser language (`navigator.language`)
4. Fallback: `"en"`

---

## Embedded control panel

By default, the Inspect panel has two tabs: **Properties** (form fields) and
**Help** (markdown documentation). The specialist can merge them into a single
guided flow by placing this HTML comment in the help markdown:

```markdown
# Configuration

Configure the I2C pins and frequency below.

<!-- place_the_control_panel_here -->

## Board RP2040

When you change the pin configuration and press Apply, the diagram updates.

![](rp2040.svg)
```

### Behaviour

When the IDE detects `place_the_control_panel_here` inside an HTML comment:

- The **Properties tab disappears**.
- The form fields (Label, prop inputs, Apply button) render **inline** at the
  placeholder position, inside a bordered container.
- The maker sees one flow: read docs → configure → see diagram update.

### Without the placeholder

When no help file contains the placeholder, the Inspect panel keeps the normal
two-tab layout (Properties + Help).

### Hardware menu preview

In the Hardware menu (before placing the component), the same placeholder is
replaced with a **static disabled preview** of the props — showing labels and
default values in disabled inputs. No Apply button.

---

## Legacy manual pages (inline `/* */` blocks)

Components that predate the GitHub markdown system can embed documentation
directly in the Go source using `/* */` block comments. This system still works
as a fallback when no markdown files are present.

### Block format

```go
/*
manualName:wiring-guide.
language:en.
showIn:init.
` ` `markdown
# Wiring Guide

Connect **SDA** to GP4 and **SCL** to GP5.
` ` `*/
```

(Note: the backtick fences above are shown with spaces for rendering — in actual
code they have no spaces.)

### Directives

| Directive     | Required | Default | Description              |
|---------------|----------|---------|--------------------------|
| `manualName:` | **Yes**  | —       | Page identifier          |
| `language:`   | No       | `en`    | BCP-47 language code     |
| `showIn:`     | No       | `both`  | `init`, `run`, or `both` |

### When to use

- **New components:** use markdown files (`init.en.md`, etc.) — simpler, easier
  to edit, supports images and interactive diagrams.
- **Legacy components:** inline `/* */` blocks still work and appear in the Help
  tab alongside markdown tabs. No migration required.

---

## Inspect panel

The **Init device** Inspect panel shows:

| Field         | Editable | Source                        |
|---------------|----------|-------------------------------|
| Label         | Yes      | User-defined canvas label     |
| `prop` fields | Yes      | Struct fields with `prop` tag |

The **Run device** Inspect panel shows only the Label field.

The **Help tab** (or embedded help when using the control panel placeholder)
shows markdown documentation and interactive diagrams.

---

## Workflow

1. Write your Go component following IDS rules.
2. Add markdown help files and optional interactive SVG diagram.
3. Create a GitHub release with a version tag.
4. In the IDE, go to **Projects** and submit the GitHub release URL.
5. The worker downloads, parses, and creates the visual blocks.
6. Find your component under **Hardware → ComponentName** in the IDE menu.
7. Choose **Init** or **Run** from the submenu to place the desired block.
8. Wire the pins to other devices and generate code.

---

## Pin symbols

| Symbol | Meaning                                                     |
|--------|-------------------------------------------------------------|
| ◉      | Mandatory connection — must be wired before code generation |
| ◎      | Optional connection — may be left unconnected               |
| ⚠      | Missing `connection:` tag — parse warning                   |

---

## Complete example

```go
// Package blackbox
//
// APDS9960 is a colour, proximity, and gesture sensor connected via I2C.
package blackbox

import "machine"

// APDS9960 reads colour (RGBC) data from an I2C bus.
//
// icon:lightbulb. label:APDS9960. interactive:rp2040.
type APDS9960 struct {
    sda   string `prop:"SDA Pin"          default:"GP4" options:"GP0,GP2,GP4,GP6" connection:"I2C_SDA"`
    scl   string `prop:"SCL Pin"          default:"GP5" options:"GP1,GP3,GP5,GP7" connection:"I2C_SCL"`
    gain  byte   `prop:"ADC Gain"         default:"0"   options:"0,1,2,3"`
    atime byte   `prop:"Integration Time" default:"255"`
}

// Init configures the APDS-9960 sensor on the given I2C bus.
//
// executionOrder:10. icon:hourglass-start. label:Init.
//
// Params
//   i2c: I2C bus.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//   err: initialisation error.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) {
    return nil
}

// Run reads the four RGBC colour channels from the sensor.
//
// icon:bolt. label:Read.
//
// Returns
//   clear: unfiltered light.  range:0..65535.  connection:optional.
//   red:   red channel.       range:0..65535.  connection:optional.
//   green: green channel.     range:0..65535.  connection:optional.
//   blue:  blue channel.      range:0..65535.  connection:optional.
func (s *APDS9960) Run() (clear, red, green, blue uint16) {
    return
}
```

Repository with help files:

```
APDS9960/
├── apds9960.go
├── readme.md             ← "APDS9960 — Colour & Gesture Sensor"
├── init.en.md            ← wiring guide + <!-- place_the_control_panel_here --> + ![](rp2040.svg)
├── init.pt-br.md         ← same in Portuguese
├── run.en.md             ← how to read colour values
└── rp2040.svg            ← interactive board diagram
```

Generated code:

```go
var apds99601 APDS9960
apds99601.sda = "GP4"
apds99601.scl = "GP5"
apds99601.gain = 0
apds99601.atime = 255
_ = apds99601.Init(i2cBus1_bus)

for {
    apds99601_clear, apds99601_red, _, _ := apds99601.Run()
    ...
}
```

---

## Troubleshooting

| Symptom                             | Likely cause                                   | Fix                                          |
|-------------------------------------|------------------------------------------------|----------------------------------------------|
| Component not in Hardware menu      | Parse error                                    | Check worker logs / warnings                 |
| No pins on the component            | `Params`/`Returns` misspelled                  | Check capitalisation: `Params`, `Returns`    |
| Codegen error: "Init block missing" | Component has `Init()` but only Run was placed | Add an Init device to the canvas             |
| Two Init blocks in wrong order      | No wire and no `executionOrder:`               | Add `executionOrder:N` to each Init method   |
| Diagram not highlighting            | `connection:` role not in SVG `data-palette`   | Match role names between struct tags and SVG |
| Help tab empty                      | No markdown files in repo root                 | Add `init.en.md` and/or `readme.md`          |
| Help shows wrong language           | Browser locale overrides                       | Set language in SPA profile preferences      |
| Embedded panel not appearing        | HTML comment has extra whitespace              | OK — the IDE handles whitespace variations   |
