// server/codegen/ir/emit_comment_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package ir

import (
	"testing"

	"server/codegen/graph"
)

// TestEmitNode_CommentStamp pins the device-comment contract: the maker's
// Inspect comment (Properties["comment"]) rides the node's FIRST emitted
// instruction as Meta["comment"], trimmed; nodes without a comment leave
// Meta untouched.
//
// Português: Pina o contrato do comentário de device: o comentário do
// Inspect (Properties["comment"]) viaja na PRIMEIRA instrução emitida do
// node como Meta["comment"], aparado; nodes sem comentário não tocam Meta.
func TestEmitNode_CommentStamp(t *testing.T) {
	withComment := idxNode("c1", "StatementConstInt")
	withComment.Properties["value"] = "5"
	withComment.Properties["comment"] = "  hello\nworld  "

	plain := idxNode("c2", "StatementConstInt")
	plain.Properties["value"] = "7"

	e := newIndexEmitter([]*graph.Node{withComment, plain}, nil)

	e.emitNode("c1")
	e.emitNode("c2")

	if n := len(e.program.Instructions); n != 2 {
		t.Fatalf("expected 2 instructions, got %d", n)
	}
	got := e.program.Instructions[0].Meta["comment"]
	if got != "hello\nworld" {
		t.Fatalf("comment not stamped/trimmed: %q", got)
	}
	if _, ok := e.program.Instructions[1].Meta["comment"]; ok {
		t.Fatalf("plain node must not carry a comment")
	}
}

// TestEmitNode_CommentOnFirstOnly pins that a multi-instruction node carries
// the comment on the FIRST instruction only — backends print it once, above
// the whole emission.
//
// Português: Pina que um node multi-instrução carrega o comentário só na
// PRIMEIRA instrução — os backends o imprimem uma vez, acima da emissão
// inteira.
func TestEmitNode_CommentOnFirstOnly(t *testing.T) {
	arr := idxNode("a1", "StatementConstArrayInt")
	arr.Properties["values"] = "1, 2, 3"
	arr.Properties["comment"] = "the readings"

	e := newIndexEmitter([]*graph.Node{arr}, nil)
	e.emitNode("a1")

	if len(e.program.Instructions) == 0 {
		t.Fatalf("array node emitted nothing")
	}
	if got := e.program.Instructions[0].Meta["comment"]; got != "the readings" {
		t.Fatalf("first instruction: comment = %q", got)
	}
	for i, inst := range e.program.Instructions[1:] {
		if _, ok := inst.Meta["comment"]; ok {
			t.Fatalf("instruction %d must not repeat the comment", i+1)
		}
	}
}

// TestStampScopeComment pins the container path: LOOP_BEGIN / COND_BEGIN are
// appended by the scope walkers (not emitNode), so stampScopeComment marks
// the just-appended begin frame with the container's comment.
//
// Português: Pina o caminho de container: LOOP_BEGIN / COND_BEGIN são
// anexados pelos walkers de escopo (não pelo emitNode), então o
// stampScopeComment marca o frame begin recém anexado com o comentário do
// container.
func TestStampScopeComment(t *testing.T) {
	loop := idxNode("loop1", "StatementLoop")
	loop.Properties["comment"] = "main cycle"

	e := newIndexEmitter([]*graph.Node{loop}, nil)
	e.program.Append(Instruction{Op: OpLoopBegin, Dest: "loop1"})
	e.stampScopeComment("loop1")

	if got := e.program.Instructions[0].Meta["comment"]; got != "main cycle" {
		t.Fatalf("loop begin: comment = %q", got)
	}

	// Unknown scope and empty program are silent no-ops.
	// Português: Escopo desconhecido e programa vazio são no-ops.
	e.stampScopeComment("ghost")
	e2 := newIndexEmitter(nil, nil)
	e2.stampScopeComment("loop1")
}
