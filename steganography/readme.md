# steganography

Embeds and extracts IoTMaker stage JSON inside PNG screenshots using **LSB
(Least Significant Bit)** steganography.

## How it works

The least significant bit of the Red, Green, and Blue channels of each pixel
is used to store binary data. Alpha is left untouched. The human eye cannot
perceive a ±1 difference in any colour channel, so the image looks identical.

## Payload format

```
Bytes  0-3:  "IOTM"          ← magic marker
Byte   4:    version (1)
Byte   5:    flags (0x01 = gzip)
Bytes  6-9:  dataLength       ← uint32 big-endian
Bytes 10-N:  payload           ← gzip-compressed JSON
```

## Capacity

| Canvas size | Pixels   | Capacity |
|-------------|----------|----------|
| 1920×1080   | 2.07M    | ~777 KB  |
| 1280×720    | 921K     | ~345 KB  |
| 800×600     | 480K     | ~180 KB  |

A typical stage JSON compresses to ~2 KB with gzip.

## Usage

```go
// Embed
pixels := getImageData() // RGBA []byte from canvas
jsonBytes := scene.Export()
err := steganography.Embed(pixels, jsonBytes)

// Extract
jsonBytes, err := steganography.Extract(pixels)
```

## Integration

- **Export**: Menu → Export → Image (PNG+Stage)
- **Import**: Drag-and-drop PNG onto IDE canvas, or File Manager → Import Image
