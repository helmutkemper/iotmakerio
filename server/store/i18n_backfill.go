// server/store/i18n_backfill.go
//
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package store

import "log"

// MigrateI18nBackfillKeys seeds the translation keys that history left
// behind: the LoopDuration/ConstDuration menu labels (their migration
// predates the i18n pattern — 2026-07-13 field diagnosis from the boot
// log's "Reported missing key" lines) and the whole device-property
// family (propComment, propFile, propContent, …) that no seed ever
// covered — device overlays always leaned on the English fallback.
//
// INSERT OR IGNORE everywhere: idempotent, runs every boot, and NEVER
// overwrites an admin edit — the same contract as SeedTranslations.
// Note the i18n_bundles PK is `locale` alone: bundle_id is metadata, so
// no bundle row is touched here — messages flow by locale.
//
// Called from migrate() in db.go after MigrateMenuTreeData().
//
// Português: Semeia as chaves de tradução que a história deixou para
// trás: os rótulos de menu LoopDuration/ConstDuration (a migração deles
// antecede o padrão de i18n — diagnóstico de campo de 2026-07-13 pelas
// linhas "Reported missing key" do boot) e a família inteira de
// propriedades de device que nenhum seed cobriu. INSERT OR IGNORE em
// tudo: idempotente, roda a cada boot e NUNCA sobrescreve edição de
// admin. O PK de i18n_bundles é só `locale`: bundle_id é metadado, então
// nenhuma linha de bundle é tocada — mensagens fluem por locale.
func MigrateI18nBackfillKeys() error {
	type entry struct{ key, en, pt string }
	entries := []entry{
		// Menu labels the LoopDuration-era migration never seeded.
		{"menuMainLoopDuration", "Timed", "Cronometrado"},
		{"menuMainConstDuration", "Duration", "Duração"},

		// Device-property family — overlays across compConsts/compData.
		{"propComment", "Comment", "Comentário"},
		{"propCommentPlaceholder",
			"Comment shown in generated code...",
			"Comentário exibido no código gerado..."},
		{"propLabel", "Label", "Rótulo"},

		// Data · File.
		{"propFile", "File", "Arquivo"},
		{"propFileHint",
			"Embedded into the generated app as bytes",
			"Embutido no app gerado como bytes"},
		{"propFileName", "File name", "Nome do arquivo"},
		{"propFileNameKeep", "leave empty to keep", "deixe vazio para manter"},
		{"propFileNameHint",
			"rename after upload (e.g. img.png)",
			"renomeie após o upload (ex.: img.png)"},

		// Data · Text (incl. the Phase B wire lock).
		{"propContent", "Content", "Conteúdo"},
		{"propLanguage", "Language", "Linguagem"},
		{"propLanguageWired", "Language (from wire)", "Linguagem (do fio)"},
		{"propNullTerminated",
			"Null-terminated (C string)",
			"Terminado em nulo (string C)"},
	}

	for _, e := range entries {
		for _, loc := range []struct{ locale, msg string }{
			{"en-US", e.en}, {"pt-BR", e.pt},
		} {
			if _, err := DB.Exec(`
				INSERT OR IGNORE INTO i18n_messages
					(locale, message_id, other, one, description)
				VALUES (?, ?, ?, '', '')`,
				loc.locale, e.key, loc.msg,
			); err != nil {
				return err
			}
		}
	}

	log.Printf("[i18n_backfill] ensured %d key(s) in en-US + pt-BR", len(entries))
	return nil
}
