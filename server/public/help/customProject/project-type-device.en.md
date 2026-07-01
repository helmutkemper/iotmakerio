# What is a Device?

A **Device** is a reusable visual block that makers wire together in the IoTMaker IDE.

You write a Go struct following the **IDS standard** (IoTMaker Doc Standard), publish it
as a GitHub release, and submit the URL here. IoTMaker downloads and parses the release
automatically — the struct becomes a drag-and-drop block with typed input and output ports.

---

## How it works

```
Your Go struct (IDS-annotated)
        │
        ▼
GitHub release  ←  you submit this URL
        │
        ▼
IoTMaker worker downloads ZIP → parses .go files → creates visual block
        │
        ▼
Block appears in the IDE hardware menu under your chosen category
```

---

## Minimum IDS example

```go
// APDS9960 reads RGBC colour data via I2C.
//
// icon:lightbulb. label:APDS9960.
type APDS9960 struct {
    gain  byte `prop:"ADC Gain" default:"0" options:"0,1,2,3"`
    atime byte `prop:"Integration Time" default:"255"`
}

// Init configures the sensor.
//
// executionOrder:1. icon:hourglass-start. label:Init.
//
// Params
//
//	i2c: I2C bus.  connection:mandatory.  unit:i2c_bus.
//
// Returns
//
//	err: initialisation error.  connection:optional.
func (s *APDS9960) Init(i2c *machine.I2C) (err error) { ... }
```

---

## Key rules

| Rule | Detail |
|------|--------|
| Language | Go only |
| Repository layout | `.go` files at the repo root (the parser finds them all) |
| URL format | `https://github.com/{you}/{repo}/releases/tag/{version}` |
| Ownership | The URL owner must match your connected GitHub account |
| Updating | Re-submit the same repo with a new tag — existing blocks are updated in place |

---

## Before submitting

- [ ] GitHub account connected — **Profile → Connect GitHub**
- [ ] Every struct has `icon:` and `label:` directives
- [ ] Every method has `icon:`, `label:`, and `executionOrder:` directives
- [ ] Every port has `connection:mandatory.` or `connection:optional.`
- [ ] Tested on real hardware

> **Tip:** Start with **Private**. Test the device in your own IDE sessions first.
> Switch to Public only when it is documented and reliable.
