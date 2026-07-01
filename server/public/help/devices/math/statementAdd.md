# Add — Math Addition

## Description

The **Add** device performs arithmetic addition on two inputs and produces one output.
It is one of the four basic math operations available in the IDE (Add, Sub, Mul, Div).

## Ports

| Port       | Direction | Type | Description                       |
|------------|-----------|------|-----------------------------------|
| **inputX** | Input     | int  | First operand (left side)         |
| **inputY** | Input     | int  | Second operand (left side, below) |
| **output** | Output    | int  | Result of inputX + inputY         |

## Behavior

Each execution cycle, the device reads the current values from `inputX` and `inputY`,
computes the sum, and sends the result to `output`.

- If either input is **not connected**, the device produces a codegen warning.
- Inside a **Loop**, the addition is recalculated every iteration.
- The output can be connected to **multiple** devices simultaneously.

## Code Generation

When the codegen pipeline processes this device, it produces:

```go
add1 := inputX + inputY
```

If the device is inside a Loop and its output crosses the loop scope (e.g., connected
to a Gauge outside the loop), the variable is promoted to a `var` declaration:

```go
var add1 int64   // promoted: read outside loop
for {
    add1 = constInt1 + constInt2
    // ...
}
fmt.Println("result", add1)
```

## Connection Examples

### Simple: two constants added

```
ConstInt(10) → Add.inputX
ConstInt(20) → Add.inputY
Add.output   → Gauge
```

Result: Gauge displays **30**.

### Chain: accumulator inside a loop

```
Add.output  → Add.inputX   (feedback)
ConstInt(1) → Add.inputY
```

Each iteration adds 1 to the running total. Pair with a **Compare** device
connected to the Loop's stop port to create a counter with a limit.

### Multi-output: feeding two consumers

```
ConstInt(5) → Add.inputX
ConstInt(3) → Add.inputY
Add.output  → Gauge ("sum")
Add.output  → Compare.inputA
```

The same output value (8) is sent to both the Gauge and the Compare device.

## Data Types

Currently, the Add device operates on `int` (int64) values. Future versions will
support `float` and `string` (concatenation) through the Data Type selector
in the Properties panel.

## Tips

- **Resize** the device via the hex menu → Resize for better visual layout.
- Use **Inspect** (hex menu) to see the property panel with this help text.
- The output connector supports **unlimited** connections.
- Input connectors accept **one** connection each.
