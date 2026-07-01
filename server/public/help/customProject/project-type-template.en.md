# What is a Template?

A **Template** is a complete project skeleton that makers can load in the IDE,
configure visually, and generate ready-to-flash code from — without writing a
single line of Go.

You build the template as a normal Go project using IDS-annotated devices,
publish it as a GitHub release, and submit the URL here. IoTMaker parses the
release and makes the template available in the IDE.

---

## How it works

```
Your Go project  (uses IDS devices + template placeholders)
        │
        ▼
GitHub release  ←  you submit this URL
        │
        ▼
IoTMaker worker downloads ZIP → parses devices → extracts readme.md metadata
        │
        ▼
Template appears in the IDE under your chosen category
        │
        ▼
Maker loads the template, connects and configures the blocks visually,
then clicks Generate — complete Go source is produced
```

---

## Template placeholder syntax

Inside your Go source, use `{{.StructName.FieldName}}` to mark values that the
maker will configure visually:

```go
// LED blink example — the maker chooses the pin and interval.
led  := machine.Pin({{.Config.LedPin}})
time.Sleep({{.Config.Interval}} * time.Millisecond)
```

No `template.json` file is needed — the placeholders are read directly from the
source by the parser.

---

## Repository layout

```
your-repo/
├── readme.md          ← first # heading becomes the template display name
├── main.go            ← entry point with template placeholders
└── *.go               ← any additional IDS-annotated device files
```

---

## Key rules

| Rule         | Detail                                                   |
|--------------|----------------------------------------------------------|
| Language     | Go only                                                  |
| URL format   | `https://github.com/{you}/{repo}/releases/tag/{version}` |
| Ownership    | The URL owner must match your connected GitHub account   |
| Display name | Taken from the first `#` heading in `readme.md`          |
| Updating     | Re-submit the same repo with a new tag                   |

---

## Before submitting

- [ ] GitHub account connected — **Profile → Connect GitHub**
- [ ] `readme.md` present with a clear `#` heading as the display name
- [ ] All devices inside the template are properly IDS-annotated
- [ ] Placeholders follow the `{{.StructName.FieldName}}` format
- [ ] Tested end-to-end: load → configure → generate → compile

> **Tip:** Start with **Private**. Verify the generated code compiles and runs
> on real hardware before publishing to the community.
