# debug

A lightweight Go package for leveled logging with a clean, familiar API.

## Levels

| Constant        | Value | Prints                    |
|-----------------|:-----:|---------------------------|
| `LevelNone`     | `0`   | nothing (silent)          |
| `LevelNotice`   | `1`   | notice · warning · error  |
| `LevelWarning`  | `2`   | warning · error           |
| `LevelError`    | `3`   | error only                |

A message is printed when **`current level != None`** and **`current level <= message level`**.

---

## Installation

```bash
go get github.com/your-username/debug
```

> Replace `github.com/your-username/debug` with your actual module path in `go.mod`.

---

## Usage

### Setting the level

```go
import "github.com/your-username/debug"

func main() {
    debug.SetLevel(debug.LevelNotice) // enable all messages
}
```

### Logging

Each level exposes three functions that mirror the standard `fmt` signatures:

```go
// Notice — informational messages (only printed when level = Notice)
debug.Notice("server started")
debug.Noticef("listening on port %d", 8080)
debug.Noticeln("ready")

// Warning — non-fatal issues (printed when level = Notice or Warning)
debug.Warning("disk usage high")
debug.Warningf("timeout connecting to %s: %v", host, err)
debug.Warningln("retrying…")

// Error — critical failures (always printed unless level = None)
debug.Error("unexpected shutdown")
debug.Errorf("fatal: %v", err)
debug.Errorln("aborting")
```

### Reading the current level

```go
lvl := debug.GetLevel()
fmt.Printf("current level: %v (%d)\n", lvl, lvl) // e.g. "NOTICE (1)"
```

### Redirecting output

By default all messages are written to `os.Stderr`. You can redirect them to
any `io.Writer` — useful for writing to a file or capturing output in tests:

```go
f, _ := os.Create("app.log")
debug.SetOutput(f)
```

To restore `os.Stderr`:

```go
debug.SetOutput(os.Stderr)
```

### Customising log flags

The package uses the standard `log` flags. Override them at any time:

```go
// timestamp only, no file/line info
debug.SetFlags(log.Ldate | log.Ltime)

// full path + microseconds
debug.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Llongfile)
```

---

## Complete example

```go
package main

import (
    "errors"
    "fmt"

    "github.com/your-username/debug"
)

func main() {
    err := errors.New("connection refused")

    // --- LevelNone: silent ---
    debug.SetLevel(debug.LevelNone)
    debug.Noticef("not printed")
    debug.Warningf("not printed")
    debug.Errorf("not printed: %v", err)

    // --- LevelNotice: everything ---
    fmt.Println("=== LevelNotice ===")
    debug.SetLevel(debug.LevelNotice)
    debug.Noticef("server started on port %d", 8080)
    debug.Warningf("timeout: %v", err)
    debug.Errorf("critical failure: %v", err)

    // --- LevelWarning: warning + error ---
    fmt.Println("=== LevelWarning ===")
    debug.SetLevel(debug.LevelWarning)
    debug.Noticef("not printed")
    debug.Warningf("disk at %.0f%% capacity", 90.0)
    debug.Errorf("critical failure: %v", err)

    // --- LevelError: errors only ---
    fmt.Println("=== LevelError ===")
    debug.SetLevel(debug.LevelError)
    debug.Noticef("not printed")
    debug.Warningf("not printed")
    debug.Errorf("critical failure: %v", err)
}
```

Expected output (timestamps omitted for brevity):

```
=== LevelNotice ===
[NOTICE]  …  server started on port 8080
[WARNING] …  timeout: connection refused
[ERROR]   …  critical failure: connection refused

=== LevelWarning ===
[WARNING] …  disk at 90% capacity
[ERROR]   …  critical failure: connection refused

=== LevelError ===
[ERROR]   …  critical failure: connection refused
```

---

## Running the tests

```bash
# run all tests
go test ./...

# with verbose output
go test -v ./...

# with race detector (recommended)
go test -race ./...
```

### What the tests cover

| Test                | Description                                             |
|---------------------|---------------------------------------------------------|
| `TestLevelNone`     | Asserts that **no** output is produced when level = None |
| `TestLevelNotice`   | Asserts that notice, warning, and error are all printed  |
| `TestLevelWarning`  | Asserts that notice is suppressed; warning and error appear |
| `TestLevelError`    | Asserts that only error messages are printed             |
| `TestGetLevel`      | Verifies that `GetLevel` returns the value set by `SetLevel` |
| `TestLevelString`   | Verifies the `String()` representation of each level    |

---

## API reference

```
SetLevel(l Level)        — set the global logging level
GetLevel() Level         — return the current level
SetOutput(w io.Writer)   — redirect all loggers to w
SetFlags(flags int)      — set log formatting flags (see package "log")

Notice(v ...any)
Noticef(format string, v ...any)
Noticeln(v ...any)

Warning(v ...any)
Warningf(format string, v ...any)
Warningln(v ...any)

Error(v ...any)
Errorf(format string, v ...any)
Errorln(v ...any)
```

---

## Thread safety

All level reads and writes are protected by a `sync.RWMutex`, making the
package safe for concurrent use across multiple goroutines.

---

## Example

```go
package main

import (
	"errors"
	"fmt"

	"github.com/seu-usuario/debug" // ajuste para o seu module path
)

func main() {
	err := errors.New("connection refused")

	fmt.Println("=== LevelNone (silencioso) ===")
	debug.SetLevel(debug.LevelNone)
	debug.Noticef("isso não aparece")
	debug.Warningf("isso não aparece")
	debug.Errorf("isso não aparece: %v", err)

	fmt.Println("\n=== LevelNotice (tudo aparece) ===")
	debug.SetLevel(debug.LevelNotice)
	debug.Noticef("servidor iniciado na porta %d", 8080)
	debug.Warningf("timeout ao conectar: %v", err)
	debug.Errorf("falha crítica: %v", err)

	fmt.Println("\n=== LevelWarning (warning + error) ===")
	debug.SetLevel(debug.LevelWarning)
	debug.Noticef("isso NÃO aparece")
	debug.Warningf("aviso: disco com %.0f%% de uso", 90.0)
	debug.Errorf("falha crítica: %v", err)

	fmt.Println("\n=== LevelError (apenas erros) ===")
	debug.SetLevel(debug.LevelError)
	debug.Noticef("isso NÃO aparece")
	debug.Warningf("isso NÃO aparece")
	debug.Errorf("falha crítica: %v", err)

	fmt.Printf("\nNível atual: %v (%d)\n", debug.GetLevel(), debug.GetLevel())
}
```
