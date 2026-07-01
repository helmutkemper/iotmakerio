# LED

A simple on/off indicator light for your dashboard.

## What it does

The LED component displays a boolean value as a colored circle:
- **ON** (true) — bright color (default: green `#44DD88`)
- **OFF** (false) — dark color (default: grey `#333344`)

## How to use

1. Place the LED from the **Display** menu
2. Connect a **bool** output to the LED's `state` input
3. Switch to the **Frontend** tab to see the live indicator

## Interactive mode

By default, clicking the LED on the frontend **toggles** its state and sends
the new value to external hardware via WebSocket. This turns the LED into a
simple on/off button.

To make the LED **read-only** (indicator only), open the Inspect panel and
check **Lock Interaction**. A 🔒 icon will appear on both the backend box
and the frontend circle.

## Properties

| Property           | Type     | Description                                  |
|--------------------|----------|----------------------------------------------|
| ID                 | text     | Unique identifier for wiring and code gen    |
| Label              | text     | Display name shown below the backend box     |
| Current            | checkbox | Current on/off state                         |
| On Color           | color    | Fill color when state is true                |
| Off Color          | color    | Fill color when state is false               |
| Lock Interaction   | checkbox | Prevents toggling from the frontend          |

## Connectors

| Port      | Direction | Type | Description                 |
|-----------|-----------|------|-----------------------------|
| `current` | Input     | bool | The on/off value to display |
