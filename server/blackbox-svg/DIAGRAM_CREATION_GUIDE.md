# /ide/docs/DIAGRAM_CREATION_GUIDE.md

# IoTMaker Interactive Diagram — Creation Guide

> This guide explains how to create interactive SVG diagrams for IoTMaker
> devices and templates. Use it yourself, or paste it into a prompt when asking
> an AI assistant to draw a diagram for you.

---

## Quick Start — Prompt Template

Copy this template and fill in the blanks when asking an AI to draw a diagram:

```
I need an IoTMaker interactive diagram SVG for [WHAT IT IS].

The diagram should show:
- [DESCRIBE THE VISUAL: board shape, components, layout]

Interactive elements (conn-groups):
- [ID]: [DESCRIPTION] — roles: [ROLE1, ROLE2, ...]
- [ID]: [DESCRIPTION] — roles: [ROLE1, ROLE2, ...]
- ...

Non-interactive elements:
- [DESCRIBE: labels, decorations, fixed components]

Palette:
- [ROLE1]: [COLOUR or "your choice"]
- [ROLE2]: [COLOUR or "your choice"]

The SVG must follow the IoTMaker Interactive Diagram Specification.
See: INTERACTIVE_DIAGRAM_SPEC.md
```

**Example — Raspberry Pi Pico:**

```
I need an IoTMaker interactive diagram SVG for a Raspberry Pi Pico board.

The diagram should show:
- Green PCB rectangle (21mm × 51mm, scaled to ~620×411 canvas)
- USB-C connector at the top
- RP2040 chip in the centre
- BOOTSEL button
- LED indicator
- 40 through-hole pins (20 per side)

Interactive elements (conn-groups):
- GP0 through GP28: GPIO pins — roles vary per pin (I2C, SPI, UART, PWM, ADC)
- GND: Ground pins (multiple, 8 total)
- 3V3: 3.3V power pins
- VBUS/VSYS: Power input pins
- RUN: Reset pin

Non-interactive elements:
- DEBUG pads at the bottom (3 pads, labelled)
- Board silkscreen text ("Raspberry Pi Pico", "© 2020")
- Mounting holes (4 corners)

Palette:
- I2C_SDA: #7c3aed (purple)
- I2C_SCL: #0d9488 (teal)
- SPI_MOSI: #2563eb (blue)
- SPI_MISO: #3b82f6 (lighter blue)
- SPI_SCK: #1d4ed8 (darker blue)
- SPI_CS: #60a5fa (light blue)
- UART_TX: #0891b2 (cyan)
- UART_RX: #06b6d4 (lighter cyan)
- GPIO_INT: #dc2626 (red)
- PWM: #ea580c (orange)
- ADC: #d97706 (amber)

The SVG must follow the IoTMaker Interactive Diagram Specification.
```

---

## Anatomy of a Connectable Element

Every element that the user can select in the IDE must follow this structure:

```svg
<g class="conn-group" data-id="GP4" data-number="6"
   data-alt="UART0_CTS,SPI0_SCK,I2C1_SDA,PWM1_A" data-side="left">

  <!-- 1. The visual shape (required) -->
  <rect class="pad" x="247" y="91" width="16" height="10" rx="1.5"
        fill="#2d7a2d" stroke="#1a5a1a" stroke-width="0.5"/>

  <!-- 2. Centre mark or hole (optional, decorative) -->
  <circle class="hole" cx="260" cy="96" r="2.2" fill="#082008"/>

  <!-- 3. Number label (optional) -->
  <text class="conn-num" font-size="8" fill="#888"
        text-anchor="end" x="243" y="99">6</text>

  <!-- 4. Readme-mode badges (required) -->
  <g class="readme-badges">
    <rect x="8" y="90" width="25" height="12" rx="2" fill="#1a5c22"/>
    <text font-size="7.5" fill="#7ac943" text-anchor="middle"
          font-family="monospace" x="20.5" y="99.2">GP4</text>

    <rect x="36" y="90" width="55" height="12" rx="2" fill="#3b1a6e"/>
    <text font-size="7.5" fill="#c4b5fd" text-anchor="middle"
          font-family="monospace" x="63.5" y="99.2">I2C1_SDA</text>

    <!-- ...more badges for each alternative function... -->
  </g>

  <!-- 5. Inspector-mode role label (required, hidden by default) -->
  <rect class="conn-role-bg" x="172" y="90" width="68" height="12" rx="2"
        visibility="hidden"/>
  <text class="conn-role" font-size="7.5" font-weight="bold" fill="#fff"
        font-family="monospace" text-anchor="middle" x="206" y="99.2"
        visibility="hidden"></text>
</g>
```

### Element-by-element explanation

| # | Element | Class | What it does |
|---|---------|-------|-------------|
| 1 | Shape | `.pad` | The visible clickable area. In inspector mode, its `fill` and `stroke` are overridden by the `--rc` CSS variable. Use `cursor: pointer` and a `transition` for polish. |
| 2 | Centre mark | `.hole` | Optional decorative element (e.g. through-hole on a PCB, dot on a node). Not affected by inspector mode. |
| 3 | Number label | `.conn-num` | Optional text showing the element's index/number. Dimmed (not hidden) for inactive elements in inspector mode. |
| 4 | Badges | `.readme-badges` | A group of coloured rectangles + text labels showing all available functions. **Only visible in readme mode.** Hidden for active elements, dimmed for inactive elements. |
| 5 | Role label | `.conn-role-bg` + `.conn-role` | The inspector-mode label. Starts hidden. When activated, the background rect takes the `--rc` colour and the text shows the human-readable role name (e.g. "I2C SDA"). |

---

## Badge Colour Conventions

In readme mode, badges use background colours to encode function families.
These are conventions, not strict rules — choose colours that are readable
against the SVG background.

| Family | Badge background | Badge text | Example |
|--------|-----------------|------------|---------|
| GPIO (primary name) | `#1a5c22` | `#7ac943` | GP4 |
| I2C | `#3b1a6e` | `#c4b5fd` | I2C1_SDA |
| SPI | `#0c2e5c` | `#60a5fa` | SPI0_SCK |
| UART | `#0c4a44` | `#2dd4bf` | UART0_TX |
| PWM | `#5c2d00` | `#fb923c` | PWM1_A |
| ADC | `#5c4a00` | `#fbbf24` | ADC0 |
| Power (3V3, VBUS) | `#5c0000` | `#f87171` | 3V3(OUT) |
| Ground | `#111111` | `#888888` | GND |
| Special (RUN, etc.) | `#2a2a2a` | `#9ca3af` | RUN |

---

## Required CSS in the SVG

Put this in `<defs><style>` inside the SVG:

```css
/* Base interaction */
.pad { cursor: pointer; transition: fill .12s, stroke .12s; }
.pad:hover { filter: brightness(1.6); }

/* Inspector mode — activated elements */
.active .pad          { fill: var(--rc) !important; stroke: var(--rc) !important; }
.active .conn-role-bg { fill: var(--rc) !important; visibility: visible !important; }
.active .conn-role    { visibility: visible !important; }
.active .readme-badges { display: none !important; }

/* Inspector mode — dimmed (non-activated) elements */
.dimmed .pad           { opacity: 0.18 !important; }
.dimmed .readme-badges { opacity: 0.18 !important; }
.dimmed .conn-num      { opacity: 0.25 !important; }
```

---

## Layout Guidelines

### Pin/connector diagrams (hardware boards)

- Place pins along the edges of the board shape.
- Left-side pins: badges extend to the LEFT of the pin pad.
- Right-side pins: badges extend to the RIGHT of the pin pad.
- Pin numbers: on the opposite side of badges (between pad and board).
- Use `data-side="left"` or `data-side="right"` to help badge positioning.
- Space pins evenly (16px vertical spacing works well at 6.2 px/mm scale).

### Node diagrams (network, software architecture)

- Place nodes freely on the canvas.
- Badges can extend in any direction — below the node works well.
- Use shapes appropriate to the domain (circles for servers, cylinders for DBs).
- Connection lines between nodes can be drawn as static paths (not interactive).

### State machine diagrams

- Use circles for states, arrows for transitions.
- Each state is a `conn-group` with `data-id="state_name"`.
- Badges can show the state's description or available transitions.

---

## The `data-palette` Attribute

Declare it on the `<svg>` root element:

```svg
<svg viewBox="0 0 620 411"
     data-palette="I2C_SDA:#7c3aed, I2C_SCL:#0d9488, GPIO_INT:#dc2626"
     xmlns="http://www.w3.org/2000/svg">
```

Rules:
- **Every role** used in a Go struct `connection:"ROLE"` tag must appear in the
  palette.
- The role name is **case-insensitive** but convention is UPPER_SNAKE_CASE.
- If you forget a role, the system falls back to a neutral slate colour (`#6b7280`).
- Keep the palette concise — only include roles that are actually used.

---

## Minimal Complete Example

A simple 4-pin device with 2 connectable pins:

```svg
<svg viewBox="0 0 200 120"
     data-palette="DATA_IN:#2563eb, DATA_OUT:#dc2626"
     xmlns="http://www.w3.org/2000/svg">
<defs><style>
  .pad { cursor: pointer; transition: fill .12s, stroke .12s; }
  .pad:hover { filter: brightness(1.6); }
  .active .pad          { fill: var(--rc) !important; stroke: var(--rc) !important; }
  .active .conn-role-bg { fill: var(--rc) !important; visibility: visible !important; }
  .active .conn-role    { visibility: visible !important; }
  .active .readme-badges { display: none !important; }
  .dimmed .pad           { opacity: 0.18 !important; }
  .dimmed .readme-badges { opacity: 0.18 !important; }
  .dimmed .conn-num      { opacity: 0.25 !important; }
</style></defs>

<!-- Board body -->
<rect fill="#1a5c1a" x="60" y="10" width="80" height="100" rx="4"/>
<text fill="rgba(255,255,255,0.3)" font-size="8" text-anchor="middle"
      x="100" y="65" font-family="monospace">My Chip</text>

<!-- Pin 1: Data Input -->
<g class="conn-group" data-id="PIN1" data-number="1" data-side="left">
  <rect class="pad" x="48" y="25" width="14" height="10" rx="1.5"
        fill="#2d7a2d" stroke="#1a5a1a" stroke-width="0.5"/>
  <text class="conn-num" font-size="7" fill="#888" text-anchor="end"
        x="45" y="33">1</text>
  <g class="readme-badges">
    <rect x="2" y="24" width="38" height="12" rx="2" fill="#0c2e5c"/>
    <text font-size="7" fill="#60a5fa" text-anchor="middle"
          font-family="monospace" x="21" y="33">DATA_IN</text>
  </g>
  <rect class="conn-role-bg" x="2" y="24" width="42" height="12" rx="2"
        visibility="hidden"/>
  <text class="conn-role" font-size="7" font-weight="bold" fill="#fff"
        font-family="monospace" text-anchor="middle" x="23" y="33"
        visibility="hidden"></text>
</g>

<!-- Pin 2: Data Output -->
<g class="conn-group" data-id="PIN2" data-number="2" data-side="right">
  <rect class="pad" x="138" y="25" width="14" height="10" rx="1.5"
        fill="#2d7a2d" stroke="#1a5a1a" stroke-width="0.5"/>
  <text class="conn-num" font-size="7" fill="#888" text-anchor="start"
        x="155" y="33">2</text>
  <g class="readme-badges">
    <rect x="158" y="24" width="42" height="12" rx="2" fill="#5c0000"/>
    <text font-size="7" fill="#f87171" text-anchor="middle"
          font-family="monospace" x="179" y="33">DATA_OUT</text>
  </g>
  <rect class="conn-role-bg" x="158" y="24" width="42" height="12" rx="2"
        visibility="hidden"/>
  <text class="conn-role" font-size="7" font-weight="bold" fill="#fff"
        font-family="monospace" text-anchor="middle" x="179" y="33"
        visibility="hidden"></text>
</g>

<!-- Pin 3: GND (not connectable — no conn-group) -->
<rect fill="#1e1e1e" stroke="#0a0a0a" x="48" y="55" width="14" height="10"
      rx="1.5" stroke-width="0.5"/>
<text font-size="7" fill="#888" text-anchor="end" x="45" y="63">3</text>
<text font-size="7" fill="#888888" font-family="monospace" x="8" y="63">GND</text>

<!-- Pin 4: VCC (not connectable — no conn-group) -->
<rect fill="#8b0000" stroke="#5c0000" x="138" y="55" width="14" height="10"
      rx="1.5" stroke-width="0.5"/>
<text font-size="7" fill="#888" text-anchor="start" x="155" y="63">4</text>
<text font-size="7" fill="#f87171" font-family="monospace" x="158" y="63">VCC</text>

</svg>
```

**Corresponding Go struct:**

```go
// interactive:my-chip.
type MyChip struct {
    dataIn  string `prop:"Data Input Pin"  default:"PIN1" connection:"DATA_IN"`
    dataOut string `prop:"Data Output Pin" default:"PIN2" connection:"DATA_OUT"`
}
```

---

## Validation Checklist

Before uploading your SVG, verify:

| # | Check | How to verify |
|---|-------|--------------|
| 1 | `<svg>` has `data-palette` | Open the SVG source, search for `data-palette` |
| 2 | All roles from your Go struct `connection:` tags appear in the palette | Compare struct tags with palette entries |
| 3 | Every `conn-group` has `data-id` | Search for `class="conn-group"` — each must have `data-id` |
| 4 | `data-id` values match the `options:` or `default:` in your struct | Compare prop options with `data-id` values |
| 5 | Each `conn-group` has `.pad`, `.readme-badges`, `.conn-role-bg`, `.conn-role` | Inspect each group's children |
| 6 | `.conn-role-bg` and `.conn-role` start with `visibility="hidden"` | Search for these classes |
| 7 | CSS rules are in `<defs><style>` inside the SVG | Open source, verify `<defs><style>` contains the required rules |
| 8 | SVG file is in the repository root | Check file location |
| 9 | Filename matches `interactive:` directive | `interactive:rp2040.` → `rp2040.svg` |
| 10 | SVG renders correctly standalone | Open the `.svg` file in a browser |

---

## Tips for Good Diagrams

1. **Use a consistent scale.** If drawing a real-world object, pick a px/mm
   ratio and stick to it. The RP2040 example uses 6.2 px/mm.

2. **Keep badge text readable.** Font size 7–8px works well. Use monospace
   fonts for technical labels.

3. **Contrast matters.** Badge backgrounds should have enough contrast with
   their text colour. Test in both light and dark contexts.

4. **Don't overload badges.** 4–5 function badges per pin is a good maximum.
   If a pin has more functions, show only the most common ones.

5. **ViewBox dimensions.** Keep the viewBox large enough that text is crisp
   (600–800px wide is a good range). The IDE will scale the SVG to fit.

6. **Non-interactive elements.** Ground, power, and fixed-function pins don't
   need to be `conn-group` elements. Just draw them as static shapes with
   labels.

7. **Test both modes.** Open the SVG standalone (readme mode) — all badges
   should be visible. Then mentally walk through the inspector mode: if you
   activate pin X with role Y, does the visual make sense?
