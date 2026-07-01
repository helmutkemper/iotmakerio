// debug/debug_test.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package debug

import (
	"bytes"
	"strings"
	"testing"
)

// captureOutput redireciona a saída, executa fn e devolve o que foi impresso.
func captureOutput(fn func()) string {
	var buf bytes.Buffer
	SetOutput(&buf)
	fn()
	SetOutput(nil) // restaura stderr (nil → os.Stderr internamente — veja nota abaixo)
	return buf.String()
}

func TestLevelNone(t *testing.T) {
	SetLevel(LevelNone)
	out := captureOutput(func() {
		Noticef("notice %d", 1)
		Warningf("warning %d", 2)
		Errorf("error %d", 3)
	})
	if out != "" {
		t.Errorf("LevelNone deveria suprimir tudo, mas obteve: %q", out)
	}
}

func TestLevelNotice(t *testing.T) {
	SetLevel(LevelNotice)
	out := captureOutput(func() {
		Noticef("msg-notice")
		Warningf("msg-warning")
		Errorf("msg-error")
	})
	for _, want := range []string{"msg-notice", "msg-warning", "msg-error"} {
		if !strings.Contains(out, want) {
			t.Errorf("LevelNotice: esperava %q na saída:\n%s", want, out)
		}
	}
}

func TestLevelWarning(t *testing.T) {
	SetLevel(LevelWarning)
	out := captureOutput(func() {
		Noticef("msg-notice")
		Warningf("msg-warning")
		Errorf("msg-error")
	})
	if strings.Contains(out, "msg-notice") {
		t.Error("LevelWarning não deve imprimir notice")
	}
	for _, want := range []string{"msg-warning", "msg-error"} {
		if !strings.Contains(out, want) {
			t.Errorf("LevelWarning: esperava %q na saída:\n%s", want, out)
		}
	}
}

func TestLevelError(t *testing.T) {
	SetLevel(LevelError)
	out := captureOutput(func() {
		Noticef("msg-notice")
		Warningf("msg-warning")
		Errorf("msg-error")
	})
	for _, noWant := range []string{"msg-notice", "msg-warning"} {
		if strings.Contains(out, noWant) {
			t.Errorf("LevelError não deve imprimir %q", noWant)
		}
	}
	if !strings.Contains(out, "msg-error") {
		t.Errorf("LevelError: esperava msg-error na saída:\n%s", out)
	}
}

func TestGetLevel(t *testing.T) {
	SetLevel(LevelWarning)
	if GetLevel() != LevelWarning {
		t.Errorf("GetLevel() = %v, queria LevelWarning", GetLevel())
	}
}

func TestLevelString(t *testing.T) {
	cases := map[Level]string{
		LevelNone:    "NONE",
		LevelNotice:  "NOTICE",
		LevelWarning: "WARNING",
		LevelError:   "ERROR",
	}
	for level, want := range cases {
		if level.String() != want {
			t.Errorf("Level(%d).String() = %q, queria %q", level, level.String(), want)
		}
	}
}
