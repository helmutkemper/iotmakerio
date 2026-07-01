// browser/html/typeDegrees_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package html

import "fmt"

func ExampleDegrees_String() {
	a := Degrees(-65)
	fmt.Printf("%v", a)

	// output:
	// -65deg
}
