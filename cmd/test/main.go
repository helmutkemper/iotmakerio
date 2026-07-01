// cmd/test/main.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"log"
	"slices"
)

// ── Black-box: Test ──

type Test struct{}

// Sort int
//
// label:SortInt.
// icon:4.
// executionOrder:4.
func (e Test) SortInt(
	// doc:list.
	// connection:mandatory.
	// label:a.
	a []int64,
) {
	slices.Sort(a)
	log.Printf("%v", a)
}

// Sort float 32
//
// label:SortFloat32.
// icon:3.
// executionOrder:3.
func (e Test) SortFloat32(
	// doc:list.
	// connection:mandatory.
	// label:a.
	a []float32,
) {
	slices.Sort(a)
	log.Printf("%v", a)
}

// Sort float32
//
// label:SortFloat64.
// icon:2.
// executionOrder:2.
func (e Test) SortFloat64(
	// doc:list.
	// connection:mandatory.
	// label:a.
	a []float64,
) {
	slices.Sort(a)
	log.Printf("%v", a)
}

// sort string
//
// label:SortString.
// icon:1.
// executionOrder:1.
func (e Test) SortString(
	// doc:list.
	// connection:mandatory.
	// label:a.
	a []string,
) {
	slices.Sort(a)
	log.Printf("%v", a)
}

func main() {
	var test0 Test
	constInt0 := int64(10)
	constInt1 := int64(10)
	stmEqualTo1 := constInt0 == constInt1
	if stmEqualTo1 {
		constArrayInt0 := []int64{2, 3, 1}
		test0.SortInt(constArrayInt0)
	} else {
		constArrayInt1 := []int64{6, 4, 5}
		test0.SortInt(constArrayInt1)
	}
}
