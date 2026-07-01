# /ide/docs/INTERACTIVE_DIAGRAM_SPEC.md

# IoTMaker Interactive Diagram Specification

> Version 1.0 — Defines the SVG contract that powers the dual-mode interactive
> diagrams in the IoTMaker IDE. This spec is the single source of truth for SVG
> authors, the WASM client renderer, the server-side worker, and any AI assistant
> asked to draw a new diagram.

---

## 1. Overview

An **Interactive Diagram** is an SVG image that operates in two modes inside the
IoTMaker IDE:

| Mode | Where it appears | Behaviour |
|------|-----------------|-----------|
| **Readme mode** | Any markdown file (`readme.md`, `init.en.md`, etc.) rendered outside the Inspect panel, or inside the Inspect panel when no connections are configured | All elements visible with their full badge labels. Static — no JS interaction. |
| **Inspector mode** | Inside the Inspect panel of the Init block, within the Help tab markdown content | Only the elements bound to configured props are highlighted. All other elements are dimmed. Clicking/tapping any SVG image opens it in a fullscreen lightbox. |

The diagram concept is **not limited to hardware**. It works for any visual where
selectable elements need to react to user configuration — circuit boards, network
topologies, state machines, software architectures, mechanical assemblies, etc.

---

## 2. SVG Root Element

The `<svg>` root **must** declare a colour palette that maps role identifiers to
CSS hex colours. The palette is the **single source of truth** for all highlight
colours used in inspector mode and for form field border accents.

```svg
<svg viewBox="0 0 620 411"
     data-palette="I2C_SDA:#7c3aed, I2C_SCL:#0d9488, GPIO_INT:#dc2626"
     xmlns="http://www.w3.org/2000/svg">
  ...
</svg>
```

### 2.1 `data-palette` format

```
data-palette="ROLE1:#hex1, ROLE2:#hex2, ROLE3:#hex3"
```

- Comma-separated list of `ROLE:#colour` pairs.
- Whitespace around commas and colons is ignored by the parser.
- `ROLE` is the same identifier used in the Go struct `connection:"ROLE"` tag.
- `#colour` is a 6-digit or 3-digit CSS hex colour (e.g. `#7c3aed`, `#f00`).
- Roles are matched **case-insensitively** (`I2C_SDA` matches `i2c_sda`).
- If a role used in a Go struct `connection:` tag is not found in the palette,
  the system falls back to a built-in neutral colour (`#6b7280` slate).

### 2.2 Recommended palette colours

These are suggestions — the SVG author is free to use any colours.

| Role family | Suggested colour | Hex |
|-------------|-----------------|-----|
| I2C SDA | Purple | `#7c3aed` |
| I2C SCL | Teal | `#0d9488` |
| SPI MOSI/TX | Blue | `#2563eb` |
| SPI MISO/RX | Blue lighter | `#3b82f6` |
| SPI SCK | Blue darker | `#1d4ed8` |
| SPI CS | Blue light | `#60a5fa` |
| UART TX | Cyan | `#0891b2` |
| UART RX | Cyan lighter | `#06b6d4` |
| Interrupt | Red | `#dc2626` |
| PWM | Orange | `#ea580c` |
| ADC | Amber | `#d97706` |
| Reset | Violet | `#9333ea` |
| Generic / other | Slate | `#6b7280` |

---

## 3. Connectable Element Groups

Each interactive element in the SVG is a `<g>` element with the class
`conn-group` and a `data-id` attribute.

```svg
<g class="conn-group"
   data-id="GP4"
   data-number="6"
   data-alt="UART0_CTS,SPI0_SCK,I2C1_SDA,PWM1_A"
   data-side="left">

  <!-- Visual pad (the clickable shape) -->
  <rect class="pad" ... />

  <!-- Readme-mode badges (visible by default, hidden in inspector mode) -->
  <g class="readme-badges">
    <rect .../><text ...>GP4</text>
    <rect .../><text ...>I2C1_SDA</text>
    ...
  </g>

  <!-- Inspector-mode role badge (hidden by default, shown in inspector mode) -->
  <rect class="conn-role-bg" ... visibility="hidden"/>
  <text class="conn-role" ... visibility="hidden"></text>
</g>
```

### 3.1 Required attributes

| Attribute | Required | Description |
|-----------|:--------:|-------------|
| `class="conn-group"` | ✅ | Identifies the element for the IDE renderer. |
| `data-id` | ✅ | The selectable value that appears in the prop dropdown and is stored in the scene JSON. Must be unique within the SVG. This is what the IDE matches against the prop's current value. |

### 3.2 Optional attributes

| Attribute | Description |
|-----------|-------------|
| `data-number` | A human-readable index (e.g. pin number on a board). Used for display only. |
| `data-alt` | Comma-separated list of alternative function names (e.g. `"SPI0_RX,I2C0_SDA,PWM0_A"`). Used in readme mode for badge display and for documentation. |
| `data-side` | Visual hint: `"left"` or `"right"`. Used by badge layout logic to position badges on the correct side. |

### 3.3 Required child elements

Each `conn-group` **must** contain these children:

| Child | CSS class | Purpose |
|-------|-----------|---------|
| Shape element | `.pad` | The visual shape the user sees/clicks. Can be `<rect>`, `<circle>`, `<path>`, etc. In inspector mode, its fill/stroke are overridden by `--rc`. |
| Badge container | `.readme-badges` | A `<g>` containing coloured badge rectangles + text labels. Visible in readme mode, hidden (via `display:none`) in inspector mode for active elements, dimmed for inactive elements. |
| Role background | `.conn-role-bg` | A `<rect>` used as the background of the inspector-mode role label. Initially `visibility="hidden"`. |
| Role text | `.conn-role` | A `<text>` element that receives the human-readable role label in inspector mode (e.g. "I2C SDA"). Initially `visibility="hidden"` with empty content. |

### 3.4 Optional child elements

| Child | CSS class | Purpose |
|-------|-----------|---------|
| Number text | `.conn-num` | Shows the element number (e.g. pin number). Dimmed in inspector mode for inactive elements. |
| Hole / centre mark | `.hole` | Visual centre of the element (e.g. through-hole on a PCB). Purely decorative. |

---

## 4. CSS Contract

The SVG **must** contain these CSS rules in a `<defs><style>` block. These rules
define the visual transitions for both modes.

```css
/* Pad hover effect (both modes) */
.pad { cursor: pointer; transition: fill .12s, stroke .12s; }
.pad:hover { filter: brightness(1.6); }

/* Inspector mode — active elements */
.active .pad        { fill: var(--rc) !important; stroke: var(--rc) !important; }
.active .conn-role-bg { fill: var(--rc) !important; visibility: visible !important; }
.active .conn-role    { visibility: visible !important; }
.active .readme-badges { display: none !important; }

/* Inspector mode — dimmed (inactive) elements */
.dimmed .pad           { opacity: 0.18 !important; }
.dimmed .readme-badges { opacity: 0.18 !important; }
.dimmed .conn-num      { opacity: 0.25 !important; }
```

The `--rc` CSS custom property is set by the IDE host at runtime via
`element.style.setProperty('--rc', colour)`. The colour comes from the
`data-palette` on the `<svg>` root.

**Important:** These CSS rules must be INSIDE the SVG `<defs>` block, not in an
external stylesheet. This ensures the styles work both when the SVG is rendered
as a standalone image and when it is injected inline into the DOM.

---

## 5. Inspector Mode Activation Protocol

The IDE host (WASM overlay renderer) performs these steps when the SVG is loaded
inside an Inspect panel Help tab:

```
1. Parse data-palette from <svg> root → build map[role]colour

2. For each prop with connection:"ROLE" tag and a non-empty value:
   a. Look up ROLE in the palette map → get colour
   b. Find <g class="conn-group" data-id="VALUE"> in the SVG
   c. Set CSS property: g.style.setProperty('--rc', colour)
   d. Set role label:   g.querySelector('.conn-role').textContent = "ROLE LABEL"
   e. Add class:        g.classList.add('active')

3. If at least one element was activated:
   Dim all others:      svg.querySelectorAll('.conn-group:not(.active)')
                        → each gets classList.add('dimmed')

4. If NO elements were activated (all props empty):
   Leave SVG in readme mode — all elements visible with badges.
```

### 5.1 Palette parsing algorithm (pseudocode)

```
function parsePalette(svgElement):
    raw = svgElement.getAttribute("data-palette")
    if raw is empty: return empty map

    palette = {}
    for each pair in raw.split(","):
        pair = pair.trim()
        colonIdx = pair.lastIndexOf(":")
        if colonIdx < 0: skip

        role  = pair[:colonIdx].trim().toUpperCase()
        color = pair[colonIdx+1:].trim()
        palette[role] = color

    return palette
```

### 5.2 Colour resolution priority

When the IDE needs a colour for a `connection:"ROLE"` prop:

1. **SVG palette** — `data-palette` on the `<svg>` root. This is the primary source.
2. **Built-in fallback** — A hardcoded neutral colour (`#6b7280`) used when the
   role is not found in the palette and no SVG is loaded.

The Go struct no longer carries a `color:` tag. All colours come from the SVG.

---

## 6. Go Struct Integration

### 6.1 Struct tags

```go
// interactive:rp2040.
type APDS9960 struct {
    sda    string `prop:"SDA Pin"        default:"GP4"  connection:"I2C_SDA"`
    scl    string `prop:"SCL Pin"        default:"GP5"  connection:"I2C_SCL"`
    intPin string `prop:"Interrupt Pin"  default:"GP3"  connection:"GPIO_INT"`
    gain   byte   `prop:"Gain"           default:"0"    options:"0,1,2,3"`
}
```

| Tag | Purpose |
|-----|---------|
| `prop:"Label"` | Makes the field appear as an editable property in the Inspect panel. |
| `default:"value"` | Pre-filled value. |
| `options:"a,b,c"` | Renders a dropdown instead of a text input. |
| `connection:"ROLE"` | Links this prop to the SVG diagram. The ROLE maps to a colour in the SVG palette and generates the label shown on active elements. The prop's **value** (what the user selects) is matched against `data-id` in the SVG. |

### 6.2 The `interactive:` directive

Declared in the struct doc comment:

```go
// interactive:rp2040.
type APDS9960 struct { ... }
```

The worker resolves this to a public URL:
`/files/devices/{owner}/{repo}/rp2040.svg`

This URL is stored in `BlackBoxDefClient.Interactive` and used by the markdown
renderer to identify which `<img>` tags to replace with inline interactive SVGs.

### 6.3 Role label generation

The label shown on active elements in inspector mode is derived from the role
identifier by replacing underscores with spaces:

```
"I2C_SDA"   → "I2C SDA"
"GPIO_INT"  → "GPIO INT"
"DATABASE"  → "DATABASE"
"SPI_MOSI"  → "SPI MOSI"
```

---

## 7. Markdown Embedding

The specialist references the SVG in any markdown help file:

```markdown
# Wiring Guide

Connect the sensor to your Raspberry Pi Pico as shown below:

![Pico pinout](rp2040.svg)

The SDA pin connects to GP4 and SCL to GP5.
```

### 7.1 Readme mode (default)

When this markdown is rendered in the Hardware menu readme or in any context
where no connection data is available, the SVG renders as-is: all connectable
elements visible with their full badge labels. The `<img>` tag is replaced with
an inline `<svg>` for quality rendering, but no activation/dimming happens.

### 7.2 Inspector mode

When the same markdown is rendered inside the Inspect panel of an Init block,
the renderer has access to the current prop values and their `connection:` roles.
After injecting the SVG inline, it runs the activation protocol (Section 5):
only the configured elements light up, and all others are dimmed.

### 7.3 Image path rewriting

The worker rewrites bare image references to public URLs:

```markdown
<!-- Before (in the specialist's repo) -->
![Pico pinout](rp2040.svg)

<!-- After (stored in DB, sent to client) -->
![Pico pinout](/files/devices/owner/repo/rp2040.svg)
```

Only files in the **root** of the repository are rewritten. Subdirectory
references (e.g. `images/rp2040.svg`) are **not** rewritten and must use
the full path.

### 7.4 Fullscreen lightbox

All images inside rendered markdown in the Inspect panel are clickable. Clicking
an image opens a fullscreen lightbox overlay with:

- The image centred on a dark semi-transparent backdrop
- A close button (×) in the top-right corner
- Click on backdrop to close
- Escape key to close

For interactive SVGs, the lightbox shows the SVG at full viewport size while
preserving the current inspector-mode state (active/dimmed elements).

---

## 8. File Naming and Upload

### 8.1 For standalone devices

```
my-sensor/
├── my_sensor.go          ← Go code with struct + connection tags
├── readme.md             ← Device overview (English)
├── readme.pt-br.md       ← Device overview (Portuguese)
├── init.en.md            ← Init help tab (English)
├── init.pt-br.md         ← Init help tab (Portuguese)
├── rp2040.svg            ← Interactive diagram (referenced in markdown)
├── wiring.png            ← Regular image (referenced in markdown)
└── run.en.md             ← Run help tab
```

### 8.2 For templates

```
my-template/
├── template.json
├── readme.md
├── rp2040.svg            ← Template-level diagram
├── devices/
│   ├── my_sensor.go
│   ├── readme.md
│   ├── init.en.md        ← References rp2040.svg in markdown
│   └── rp2040.svg        ← Device-level diagram (can be different)
└── output/
    └── main.go
```

The `interactive:` directive in the struct doc comment must match the SVG
filename **without extension**: `interactive:rp2040.` → file `rp2040.svg`.

---

## 9. Non-Hardware Example

A network topology diagram where the user configures which servers to use:

**Go struct:**

```go
// interactive:topology.
type WebApp struct {
    db    string `prop:"Database"     default:"primary-db"   connection:"DATABASE"    options:"primary-db,replica-db"`
    cache string `prop:"Cache Server"  default:"redis-main"  connection:"CACHE"       options:"redis-main,redis-backup"`
    api   string `prop:"API Gateway"   default:"gateway-us"  connection:"API_GATEWAY" options:"gateway-us,gateway-eu"`
}
```

**SVG palette:**

```svg
<svg data-palette="DATABASE:#7c3aed, CACHE:#dc2626, API_GATEWAY:#2563eb" ...>
  <g class="conn-group" data-id="primary-db">...</g>
  <g class="conn-group" data-id="replica-db">...</g>
  <g class="conn-group" data-id="redis-main">...</g>
  <g class="conn-group" data-id="redis-backup">...</g>
  <g class="conn-group" data-id="gateway-us">...</g>
  <g class="conn-group" data-id="gateway-eu">...</g>
</svg>
```

When the maker selects "replica-db" for Database, "redis-main" for Cache, and
"gateway-eu" for API Gateway, the diagram shows only those three elements
highlighted with their respective colours (purple, red, blue) and all other
elements dimmed.

---

## 10. Quick Reference

### SVG checklist for authors

- [ ] `<svg>` has `data-palette="ROLE1:#hex, ROLE2:#hex, ..."` attribute
- [ ] Each interactive element is `<g class="conn-group" data-id="UNIQUE_ID">`
- [ ] Each group has child `.pad` (shape), `.readme-badges`, `.conn-role-bg`, `.conn-role`
- [ ] CSS rules for `.active`, `.dimmed`, `.pad` are in `<defs><style>`
- [ ] `.conn-role-bg` and `.conn-role` start with `visibility="hidden"`
- [ ] SVG file is in the repository root (not in a subfolder)
- [ ] Filename matches the `interactive:` directive (e.g. `rp2040.svg` for `interactive:rp2040.`)

### Go struct checklist for specialists

- [ ] Struct doc comment has `// interactive:filename.` (without `.svg`)
- [ ] Props that bind to diagram elements have `connection:"ROLE"` tag
- [ ] ROLE values match keys in the SVG's `data-palette`
- [ ] Prop values (user selections) match `data-id` values in the SVG
