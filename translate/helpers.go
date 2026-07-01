// translate/helpers.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package translate

// helpers.go — Translation helper functions.
//
// English:
//
//	T() is the main translation function used throughout the IDE.
//	When a key is not found, it returns the fallback AND reports the
//	missing key to the server asynchronously (fire-and-forget).
//
//	ROOT CAUSE NOTE:
//	  Do NOT use DefaultMessage inside LocalizeConfig. When DefaultMessage
//	  is set, go-i18n v2 silently returns the fallback with err == nil
//	  whenever the key is missing from the bundle. This prevents the
//	  err != nil check from ever triggering, so ReportMissing is never called.
//	  Solution: use MessageID only, so a missing key always returns a real
//	  *MessageNotFoundErr that this function can detect and handle.
//
// Português:
//
//	T() é a função principal de tradução usada em toda a IDE.
//	Quando uma chave não é encontrada, retorna o fallback E reporta
//	a chave faltante ao servidor de forma assíncrona (fire-and-forget).
//
//	CAUSA DO BUG ANTERIOR:
//	  NÃO usar DefaultMessage dentro do LocalizeConfig. Quando DefaultMessage
//	  está definido, o go-i18n v2 retorna o fallback com err == nil quando
//	  a chave não existe no bundle, impedindo que ReportMissing seja chamado.

import (
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// T returns the translated string for the given message ID.
//
// Lookup order:
//  1. If Localizer is nil (Load() was not called or failed entirely),
//     report missing and return the fallback immediately.
//  2. Look up the ID in the bundle using MessageID only — no DefaultMessage.
//     This ensures go-i18n returns a real *MessageNotFoundErr when the key
//     is absent, rather than silently swallowing the miss.
//  3. On any error (key not found, bundle empty, etc.),
//     report missing asynchronously and return the fallback.
//
// Usage:
//
//	label := translate.T("menuDeviceAdd", "Add Device")
//
// Português:
//
//	Retorna a string traduzida para o ID dado.
//	Usa apenas MessageID na busca para garantir que chaves ausentes
//	gerem um erro real detectável, disparando o reporte assíncrono.
func T(id, fallback string) string {
	// Guard: if Load() was never called or failed completely, the Localizer
	// will be nil. Report the miss and return the fallback right away.
	if Localizer == nil {
		ReportMissing(id, fallback)
		return fallback
	}

	// Use MessageID only — do NOT set DefaultMessage here.
	// Setting DefaultMessage causes go-i18n to return err == nil even when
	// the key is missing, making it impossible to detect the miss below.
	s, err := Localizer.Localize(&i18n.LocalizeConfig{
		MessageID: id,
	})

	if err != nil {
		// Key was not found in the bundle (or any other localize error).
		// Report the miss to the server and return the Go-side fallback.
		ReportMissing(id, fallback)
		return fallback
	}

	return s
}
