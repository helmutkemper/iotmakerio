// Package debug fornece um logger com níveis configuráveis.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Níveis disponíveis:
//
//	LevelNone    = 0  → nenhuma mensagem é exibida
//	LevelNotice  = 1  → notice, warning e error
//	LevelWarning = 2  → warning e error
//	LevelError   = 3  → apenas error
//
// Uso rápido:
//
//	debug.SetLevel(debug.LevelNotice)
//	debug.Noticef("servidor iniciado na porta %d", 8080)
//	debug.Warningf("timeout ao conectar: %v", err)
//	debug.Errorf("falha crítica: %v", err)
package debug

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// Level representa o nível de verbosidade do logger.
type Level int

const (
	LevelNone    Level = 0 // Nenhuma saída
	LevelNotice  Level = 1 // Informações gerais (mais verboso)
	LevelWarning Level = 2 // Avisos
	LevelError   Level = 3 // Apenas erros críticos
)

// String retorna o nome textual do nível.
func (l Level) String() string {
	switch l {
	case LevelNone:
		return "NONE"
	case LevelNotice:
		return "NOTICE"
	case LevelWarning:
		return "WARNING"
	case LevelError:
		return "ERROR"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// logger é o estado global do pacote.
var (
	mu      sync.RWMutex
	current Level = LevelNone

	logNotice  = log.New(os.Stderr, "[NOTICE]  ", log.Ldate|log.Ltime|log.Lshortfile)
	logWarning = log.New(os.Stderr, "[WARNING] ", log.Ldate|log.Ltime|log.Lshortfile)
	logError   = log.New(os.Stderr, "[ERROR]   ", log.Ldate|log.Ltime|log.Lshortfile)
)

// ─── Configuração ─────────────────────────────────────────────────────────────

// SetLevel define o nível de debug global.
// Apenas mensagens com nível >= ao nível configurado serão impressas.
//
//	none (0)    → silencioso
//	notice (1)  → notice, warning, error
//	warning (2) → warning, error
//	error (3)   → error
func SetLevel(l Level) {
	mu.Lock()
	defer mu.Unlock()
	current = l
}

// GetLevel retorna o nível de debug atual.
func GetLevel() Level {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// SetOutput redireciona todos os loggers para o writer fornecido.
// Útil em testes ou para gravar em arquivo.
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	logNotice.SetOutput(w)
	logWarning.SetOutput(w)
	logError.SetOutput(w)
}

// SetFlags substitui as flags de formatação de todos os loggers.
// Aceita as mesmas flags do pacote padrão "log" (log.Ldate, log.Ltime, etc.).
func SetFlags(flags int) {
	mu.Lock()
	defer mu.Unlock()
	logNotice.SetFlags(flags)
	logWarning.SetFlags(flags)
	logError.SetFlags(flags)
}

// ─── Notice ───────────────────────────────────────────────────────────────────

// Notice imprime uma mensagem de nível Notice.
// Só tem efeito se o nível configurado for LevelNotice (1) ou superior.
func Notice(v ...any) {
	if enabled(LevelNotice) {
		logNotice.Output(2, fmt.Sprint(v...)) //nolint:errcheck
	}
}

// Noticef imprime uma mensagem formatada de nível Notice.
func Noticef(format string, v ...any) {
	if enabled(LevelNotice) {
		logNotice.Output(2, fmt.Sprintf(format, v...)) //nolint:errcheck
	}
}

// Noticeln imprime uma mensagem de nível Notice com newline.
func Noticeln(v ...any) {
	if enabled(LevelNotice) {
		logNotice.Output(2, fmt.Sprintln(v...)) //nolint:errcheck
	}
}

// ─── Warning ──────────────────────────────────────────────────────────────────

// Warning imprime uma mensagem de nível Warning.
// Só tem efeito se o nível configurado for LevelWarning (2) ou superior.
func Warning(v ...any) {
	if enabled(LevelWarning) {
		logWarning.Output(2, fmt.Sprint(v...)) //nolint:errcheck
	}
}

// Warningf imprime uma mensagem formatada de nível Warning.
func Warningf(format string, v ...any) {
	if enabled(LevelWarning) {
		logWarning.Output(2, fmt.Sprintf(format, v...)) //nolint:errcheck
	}
}

// Warningln imprime uma mensagem de nível Warning com newline.
func Warningln(v ...any) {
	if enabled(LevelWarning) {
		logWarning.Output(2, fmt.Sprintln(v...)) //nolint:errcheck
	}
}

// ─── Error ────────────────────────────────────────────────────────────────────

// Error imprime uma mensagem de nível Error.
// Só tem efeito se o nível configurado for LevelError (3) ou superior.
func Error(v ...any) {
	if enabled(LevelError) {
		logError.Output(2, fmt.Sprint(v...)) //nolint:errcheck
	}
}

// Errorf imprime uma mensagem formatada de nível Error.
func Errorf(format string, v ...any) {
	if enabled(LevelError) {
		logError.Output(2, fmt.Sprintf(format, v...)) //nolint:errcheck
	}
}

// Errorln imprime uma mensagem de nível Error com newline.
func Errorln(v ...any) {
	if enabled(LevelError) {
		logError.Output(2, fmt.Sprintln(v...)) //nolint:errcheck
	}
}

// ─── Helpers internos ─────────────────────────────────────────────────────────

// enabled verifica, de forma thread-safe, se o nível l deve ser impresso.
// A lógica é: um nível é ativo quando current > 0 E current <= l.
//
//	SetLevel(LevelNotice)  → notice✓  warning✓  error✓
//	SetLevel(LevelWarning) → notice✗  warning✓  error✓
//	SetLevel(LevelError)   → notice✗  warning✗  error✓
//	SetLevel(LevelNone)    → notice✗  warning✗  error✗
func enabled(l Level) bool {
	mu.RLock()
	c := current
	mu.RUnlock()
	return c != LevelNone && c <= l
}
