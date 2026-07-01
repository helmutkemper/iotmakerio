# Button

A push button that sends boolean values to hardware.

## What it does

The Button is an **output device** — the user presses it on the frontend and
the value is sent to external hardware via WebSocket. Other devices on the
backend canvas can also wire to the button's output to read its state.

## Modes

**Toggle** (default): each click alternates between ON and OFF.

**Momentary**: click sends ON, then automatically releases back to OFF after
200ms. Like a doorbell — press and let go. Enable via the **Momentary**
checkbox in the Inspect panel.

## How to use

1. Place the Button from the **Display** menu
2. On the backend, wire the `current` output to other devices that need a bool
3. Switch to the **Frontend** tab and click the button

## Visual feedback

- **Released** (false): raised look with outset shadow and idle color
- **Pressed** (true): inset look with dark top shadow and active color

Both colors are customizable via the Inspect panel.

## Properties

| Property         | Type     | Description                                    |
|------------------|----------|------------------------------------------------|
| ID               | text     | Unique identifier for wiring and code gen      |
| Label            | text     | Display name shown below the backend box       |
| Button Text      | text     | Label shown on the button face (default: PUSH) |
| Active Color     | color    | Fill color when pressed (default: blue)        |
| Idle Color       | color    | Fill color when released (default: dark)       |
| Momentary        | checkbox | Auto-release after 200ms (doorbell mode)       |
| Lock Interaction | checkbox | Prevents toggling from the frontend            |

## Connectors

| Port      | Direction | Type | Description                        |
|-----------|-----------|------|------------------------------------|
| `current` | Output    | bool | Button state (true=pressed)        |
