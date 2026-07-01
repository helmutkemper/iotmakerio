// browser/html/rgbaToJs_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

import (
	"fmt"
	"image/color"
)

func ExampleRGBAToJs() {
	colorRGBA := color.RGBA{
		R: 10,
		G: 20,
		B: 30,
		A: 100,
	}
	fmt.Printf("Color: %v\n", RGBAToJs(colorRGBA))

	// Output:
	// Color: rgba( 10, 20, 30, 100 )
}
