// steganography/stego.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package steganography

// stego.go
//
// [STEGANOGRAPHY] Core LSB (Least Significant Bit) steganography routines
// for embedding and extracting binary payloads in RGBA pixel arrays.
//
// The format embeds compressed scene JSON inside PNG screenshots of the
// IDE stage. Visually the image is indistinguishable from the original —
// only the least significant bit of each R, G, B channel is modified
// (alpha is left untouched to avoid transparency artifacts).
//
// Header format (10 bytes):
//
//	Bytes  0-3:  "IOTM"          magic marker (identifies as IoTMaker)
//	Byte   4:    version (1)      format version
//	Byte   5:    flags            bit 0: gzip compressed
//	Bytes  6-9:  dataLength       uint32 big-endian (payload size in bytes)
//	Bytes 10-N:  payload          gzip-compressed JSON
//
// Capacity: each pixel stores 3 bits (R, G, B LSBs). A 1920×1080 canvas
// provides ~777 KB of storage. A typical gzip-compressed stage JSON is
// ~2 KB, using only ~5400 pixels — imperceptible.
//
// Português:
//
//	Rotinas LSB para embutir e extrair payloads binários em arrays RGBA.
//	Modifica apenas o bit menos significativo de R, G, B — imperceptível.

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

// ─── Constants ──────────────────────────────────────────────────────────────

var magic = [4]byte{'I', 'O', 'T', 'M'}

const (
	headerVersion = 1
	headerSize    = 10 // magic(4) + version(1) + flags(1) + length(4)
	flagGzip      = 0x01
)

// ─── Embed ──────────────────────────────────────────────────────────────────

// Embed writes a gzip-compressed payload into the LSBs of an RGBA pixel array.
// The pixels slice must be in RGBA order (4 bytes per pixel) as returned by
// canvas.getImageData().data.
//
// Returns an error if the pixel array is too small to hold the payload.
//
// Português: Escreve um payload comprimido nos LSBs de um array RGBA.
// Retorna erro se o array de pixels for pequeno demais.
// RequiredBytes returns the EXACT number of payload bytes the given data
// occupies once embedded: header + gzip-compressed body. Callers can divide
// the image's pixel budget (3 bits per pixel) by 8 and compare — the
// pre-flight check then agrees byte-for-byte with Embed, instead of relying
// on a compression-ratio guess that incompressible content can defeat.
// Português: Retorna o número EXATO de bytes que o dado ocupa embutido:
// cabeçalho + corpo gzip. Quem chama divide o orçamento de pixels da imagem
// (3 bits por pixel) por 8 e compara — a pré-checagem então concorda byte a
// byte com o Embed, em vez de depender de um chute de taxa de compressão
// que conteúdo incompressível derrota.
func RequiredBytes(data []byte) (int, error) {
	compressed, err := gzipCompress(data)
	if err != nil {
		return 0, fmt.Errorf("stego: gzip compress: %w", err)
	}
	return headerSize + len(compressed), nil
}

func Embed(pixels []byte, data []byte) error {
	// Compress the payload.
	compressed, err := gzipCompress(data)
	if err != nil {
		return fmt.Errorf("stego: gzip compress: %w", err)
	}

	// Build the full payload: header + compressed data.
	payload := buildPayload(compressed)

	// Calculate capacity: 3 usable bits per pixel (R, G, B — skip A).
	pixelCount := len(pixels) / 4
	capacityBits := pixelCount * 3
	requiredBits := len(payload) * 8

	if requiredBits > capacityBits {
		return fmt.Errorf("stego: payload too large (%d bytes) for image (%d pixels, %d bytes capacity)",
			len(payload), pixelCount, capacityBits/8)
	}

	// Write bits into LSBs of R, G, B channels. Alpha (index 3) is untouched.
	bitIndex := 0
	for _, b := range payload {
		for bit := 7; bit >= 0; bit-- {
			// Determine which pixel and channel this bit goes into.
			pixelIdx := bitIndex / 3
			channel := bitIndex % 3 // 0=R, 1=G, 2=B

			byteOffset := pixelIdx*4 + channel

			// Clear LSB and set it to the payload bit.
			val := pixels[byteOffset]
			val = (val & 0xFE) | ((b >> bit) & 1)
			pixels[byteOffset] = val

			bitIndex++
		}
	}

	return nil
}

// ─── Extract ────────────────────────────────────────────────────────────────

// Extract reads a payload from the LSBs of an RGBA pixel array.
// Returns the decompressed original data, or an error if no valid
// IoTMaker header is found.
//
// Português: Lê um payload dos LSBs de um array RGBA.
// Retorna os dados descomprimidos ou erro se não encontrar header válido.
func Extract(pixels []byte) ([]byte, error) {
	pixelCount := len(pixels) / 4

	// We need at least enough pixels to read the 10-byte header.
	headerBits := headerSize * 8
	if pixelCount*3 < headerBits {
		return nil, fmt.Errorf("stego: image too small to contain header")
	}

	// Read the header bytes from LSBs.
	headerBytes := readBits(pixels, 0, headerSize)

	// Validate magic marker.
	if headerBytes[0] != magic[0] || headerBytes[1] != magic[1] ||
		headerBytes[2] != magic[2] || headerBytes[3] != magic[3] {
		return nil, fmt.Errorf("stego: no IoTMaker marker found")
	}

	// Parse header.
	version := headerBytes[4]
	if version != headerVersion {
		return nil, fmt.Errorf("stego: unsupported version %d", version)
	}

	flags := headerBytes[5]
	dataLen := binary.BigEndian.Uint32(headerBytes[6:10])

	// Validate data length against image capacity.
	totalBits := (headerSize + int(dataLen)) * 8
	if pixelCount*3 < totalBits {
		return nil, fmt.Errorf("stego: declared payload (%d bytes) exceeds image capacity", dataLen)
	}

	// Read the payload.
	data := readBits(pixels, headerSize, int(dataLen))

	// Decompress if gzip flag is set.
	if flags&flagGzip != 0 {
		decompressed, err := gzipDecompress(data)
		if err != nil {
			return nil, fmt.Errorf("stego: gzip decompress: %w", err)
		}
		return decompressed, nil
	}

	return data, nil
}

// ─── Internal helpers ───────────────────────────────────────────────────────

// buildPayload creates the full byte sequence: header + compressed data.
func buildPayload(compressed []byte) []byte {
	payload := make([]byte, headerSize+len(compressed))

	// Magic.
	copy(payload[0:4], magic[:])

	// Version.
	payload[4] = headerVersion

	// Flags: gzip.
	payload[5] = flagGzip

	// Data length (uint32 big-endian).
	binary.BigEndian.PutUint32(payload[6:10], uint32(len(compressed)))

	// Compressed data.
	copy(payload[headerSize:], compressed)

	return payload
}

// readBits extracts `count` bytes from the LSBs of the pixel array starting
// at byte offset `startByte` (in terms of the embedded bit stream).
func readBits(pixels []byte, startByte, count int) []byte {
	result := make([]byte, count)
	bitIndex := startByte * 8

	for i := 0; i < count; i++ {
		var b byte
		for bit := 7; bit >= 0; bit-- {
			pixelIdx := bitIndex / 3
			channel := bitIndex % 3

			byteOffset := pixelIdx*4 + channel
			b |= (pixels[byteOffset] & 1) << bit

			bitIndex++
		}
		result[i] = b
	}

	return result
}

// gzipCompress compresses data using gzip with best compression.
func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gzipDecompress decompresses gzip data.
func gzipDecompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// HasMarker checks whether the pixel array contains the "IOTM" magic marker
// in the first 32 LSBs (4 bytes × 8 bits = ~11 pixels). This is a fast
// check that avoids decompressing the full payload — useful for scanning
// multiple images to find which ones contain embedded data.
//
// Português: Verifica se o array de pixels contém o marcador "IOTM" nos
// primeiros LSBs. Check rápido sem descomprimir o payload.
func HasMarker(pixels []byte) bool {
	// Need at least 4 bytes × 8 bits / 3 bits per pixel ≈ 11 pixels.
	if len(pixels) < 11*4 {
		return false
	}
	header := readBits(pixels, 0, 4)
	return header[0] == magic[0] &&
		header[1] == magic[1] &&
		header[2] == magic[2] &&
		header[3] == magic[3]
}
