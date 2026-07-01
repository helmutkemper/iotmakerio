# Seven-Segment Display

A classic LCD-style numeric display with configurable digit count.

## What it does

The Seven-Segment display shows an integer value using the traditional
seven-segment digit style found in clocks, meters, and instrument panels.
Each segment lights up individually, giving the authentic retro LED look.

## How to use

1. Place the 7-Seg from the **Display** menu
2. Connect an **int** output to the `current` input
3. Adjust **Digits** in the Inspect panel (1-8, default 3)
4. Switch to the **Frontend** tab to see the live display

## Negative values

Negative numbers show a minus sign in the leftmost digit position.
For example, with 3 digits: -99 to 999.

## Properties

| Property           | Type     | Description                                |
|--------------------|----------|--------------------------------------------|
| ID                 | text     | Unique identifier for wiring and code gen  |
| Label              | text     | Display name shown below the backend box   |
| Value              | number   | Current value to display                   |
| Digits             | number   | Number of digit positions (1-8)            |
| On Color           | color    | Active segment color (default: red)        |
| Off Color          | color    | Inactive segment color (dim trace)         |
| Background         | color    | Display background color                   |
| Lock Interaction   | checkbox | Standard compFrontend lock                 |

## Connectors

| Port      | Direction | Type | Description            |
|-----------|-----------|------|------------------------|
| `current` | Input     | int  | Integer value to show  |
