// /welcomeModal/doc.go

// Package welcomeModal renders a modal overlay that the IDE shows once
// at boot, before the workspace exists. The maker uses it to pick the
// project's compile target — Go or C99 — and that choice is then fixed
// for the lifetime of the project (the language is irreversible by
// design; see /server/store/stage_files.go for the rationale).
//
// Lifecycle:
//
//  1. main.go calls welcomeModal.Show after splash preloads finish and
//     BEFORE ViewManager.Init runs. The modal needs the language up
//     front so the workspace, the menu builder, and the language badge
//     can all be configured with a fixed value from their first call.
//
//  2. Show blocks the main goroutine on a channel until the user
//     either picks a language (clicks one of the cards) or dismisses
//     the modal (X button or ESC key). A dismissed modal returns the
//     C99 default — same default the server's stage_files.language
//     column applies when the column is empty, and same default that
//     Workspace.Init falls back to internally.
//
//  3. After Show returns, the overlay DOM is removed and the rest of
//     main.go proceeds with the chosen language in sharedCfg.Language.
//
// Phase 1 (Parcela 2a — this file):
//
// The modal shows just two cards — "+ Go project" and "+ C99 project".
// No project list, no backup detection, no "open existing" flow. Those
// arrive in Parcelas 2b/2c/2d which extend this same package.
//
// Why a Go package rather than plain JavaScript:
//
// The rest of the IDE's overlay surfaces (splash screen, stage file
// manager, blackbox loader, live config dialog) are Go/WASM packages
// that build DOM through syscall/js. Keeping welcomeModal in the same
// idiom means the same code conventions, the same test path, and one
// less bridge between Go state and JS state. The boot sequence in
// main.go reads top-to-bottom in pure Go.
//
// Português:
//
//	Modal de boas-vindas. Aparece uma vez no boot, antes do workspace
//	existir, para o maker escolher a linguagem do projeto (Go ou C99).
//	Escolha é irreversível.
//
//	Lifecycle: main.go chama Show depois dos preloads do splash e
//	ANTES de vm.Init. Show bloqueia a main goroutine até o usuário
//	escolher ou fechar (X / ESC). Fechar retorna "c" (C99) — mesmo
//	default do server e do Workspace.Init.
//
//	Fase 1 (Parcela 2a): apenas 2 cards "+ Go" / "+ C99". Listagem de
//	projetos, detecção de backup e fluxo de "abrir existente" vêm em
//	Parcelas 2b/2c/2d.
//
//	Package Go (não JS puro) para manter consistência com splash,
//	filemanager, blackbox loader e live config — todos Go/WASM que
//	constroem DOM via syscall/js.
package welcomeModal
