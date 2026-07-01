# TextDisplay

A resizable text viewer for the frontend dashboard.

## What it does

The TextDisplay component shows string data as monospace text on the frontend
canvas. Ideal for log output, status messages, configuration snippets, or any
text data from hardware.

## How to use

1. Place the TextDisplay from the **Display** menu
2. Connect a **string** output to the `current` input on the backend
3. Optionally enter default **Text** in the Inspect panel
4. Switch to the **Frontend** tab to see the live text preview

## Resizable preview

Click the frontend element to open a menu. Select **Resize** to show corner
handles, then drag to adjust the preview area. After releasing, the element
returns to drag mode automatically. The new dimensions are saved in the scene
JSON.

## Properties

| Property         | Type     | Description                               |
|------------------|----------|-------------------------------------------|
| ID               | text     | Unique identifier for wiring and code gen |
| Label            | text     | Display name shown below the backend box  |
| Text             | textarea | Default text before live data arrives     |
| Lock Interaction | checkbox | Prevents the context menu from opening    |

## Connectors

| Port      | Direction | Type   | Description                   |
|-----------|-----------|--------|-------------------------------|
| `current` | Input     | string | Text content to display       |
