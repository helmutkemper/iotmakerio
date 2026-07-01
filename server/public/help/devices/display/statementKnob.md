# Knob

A rotary dial control for setting integer values.

## What it does

The Knob displays and controls an integer value using a circular dial with a
270° sweep — the same layout used in audio equipment and industrial panels.
Clicking anywhere on the knob sets the value based on the angle from center.

## How to use

1. Place the Knob from the **Display** menu
2. Connect **int** outputs to the `max`, `current`, and `min` inputs
3. Switch to the **Frontend** tab and click on the dial to set values

## Interaction

Click on the knob face to set the value. The position you click maps to the
270° sweep range: bottom-left is min, clockwise to bottom-right is max.
There is a 90° dead zone at the bottom.

## Properties

| Property           | Type     | Description                                |
|--------------------|----------|--------------------------------------------|
| ID                 | text     | Unique identifier for wiring and code gen  |
| Label              | text     | Display name shown below the backend box   |
| Min                | number   | Minimum value (left end of sweep)          |
| Max                | number   | Maximum value (right end of sweep)         |
| Value              | number   | Current value                              |
| Knob Color         | color    | Indicator and value arc color              |
| Track Color        | color    | Background arc color                       |
| Lock Interaction   | checkbox | Prevents click-to-set on frontend          |

## Connectors

| Port      | Direction | Type | Description              |
|-----------|-----------|------|--------------------------|
| `max`     | Input     | int  | Maximum scale value      |
| `current` | Input     | int  | Current value            |
| `min`     | Input     | int  | Minimum scale value      |
