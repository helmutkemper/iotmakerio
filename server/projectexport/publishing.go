// server/projectexport/publishing.go — PUBLISHING.md generator.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Produces the step-by-step Git/GitHub instructions bundled with
// every export. Lives alongside the project files (NOT as
// README.md, which the user authors themselves) so it can be
// deleted after the first push without losing anything custom.
//
// Two locales supported today: English (default) and Brazilian
// Portuguese. The user's preferredLocale is consulted; anything
// other than "pt-br" (case-insensitive) falls back to English. The
// list mirrors what the rest of the SPA supports — adding a third
// locale here should happen at the same time as adding it to the
// SPA's translate.T() bundles, otherwise the export disagrees with
// the surrounding UI language.
//
// The {{PROJECT_NAME}} placeholder is the lowercased,
// hyphen-sanitised project name (the same form used in the ZIP's
// top-level folder and the suggested repo name on GitHub).
//
// Português: Gerador do PUBLISHING.md incluído em cada projeto
// exportado. Suporta inglês (padrão) e português brasileiro,
// roteado pelo preferredLocale do usuário. Mantém-se separado do
// README.md que o usuário escreve, para poder ser apagado após o
// primeiro push sem perder nada autoral.
package projectexport

import "strings"

// RenderPublishing returns the PUBLISHING.md byte slice for the
// given project name and locale. The locale is normalised
// (lowercased, trimmed) before lookup so call sites can pass the
// raw User.PreferredLocale without preprocessing.
//
// Returns the file contents ready to be written into the ZIP. The
// returned slice is freshly allocated — safe for the caller to
// hand to a writer that retains the buffer.
func RenderPublishing(projectName, locale string) []byte {
	tmpl := publishingEN
	if strings.ToLower(strings.TrimSpace(locale)) == "pt-br" {
		tmpl = publishingPTBR
	}
	out := strings.ReplaceAll(tmpl, "{{PROJECT_NAME}}", projectName)
	return []byte(out)
}

// publishingEN is the English template. The instructions are
// deliberately copy-paste-ready — the user changes only the GitHub
// username and (optionally) repo name. We avoid recommending
// `--global` git config to keep the surface tiny: the user might
// already have a global identity for other repos.
const publishingEN = `# Publishing this project to GitHub

This folder contains everything needed to publish your IoTMaker
component as a Git repository on GitHub. Apache License 2.0,
` + "`.gitignore`" + `, and your source files are already in place — you do
not need to add a README from the GitHub side, this folder already
includes one if you authored it.

You can delete this ` + "`PUBLISHING.md`" + ` file after the first push.

## Prerequisites

- **Git** installed: <https://git-scm.com/downloads>
- A **GitHub account**: <https://github.com/join>

## Step 1 — Create an empty repository on GitHub

1. Go to <https://github.com/new>
2. Name it (suggested: ` + "`{{PROJECT_NAME}}`" + `)
3. Choose **public** or **private**
4. **Do NOT** check any of the "Initialize this repository with…"
   boxes (no README, no .gitignore, no LICENSE). This folder
   already provides them; pre-initialising on GitHub causes a
   conflict on the first push.
5. Click **Create repository**

## Step 2 — Push from this folder

Open a terminal in this folder, then run (replace
` + "`<your-username>`" + ` with your actual GitHub username):

` + "```bash" + `
git init
git add .
git commit -m "Initial commit from IoTMaker"
git branch -M main
git remote add origin https://github.com/<your-username>/{{PROJECT_NAME}}.git
git push -u origin main
` + "```" + `

That's it — refresh your GitHub page and the project will be there.

## Updating later

When you change something in IoTMaker and re-export, replace the
files in this folder with the new export, then:

` + "```bash" + `
git add .
git commit -m "Describe what changed"
git push
` + "```" + `

## License

This project is published under the **Apache License 2.0**. See the
` + "`LICENSE`" + ` file for the full text.
`

// publishingPTBR is the Brazilian Portuguese version. Same shape
// as the EN template — translation, not localisation: the steps,
// the order, and the placeholder are identical so a bilingual user
// reading both gets the same procedure.
const publishingPTBR = `# Publicando este projeto no GitHub

Esta pasta contém tudo que você precisa para publicar seu componente
IoTMaker como um repositório Git no GitHub. A licença Apache 2.0, o
` + "`.gitignore`" + ` e os arquivos de código-fonte já estão no lugar — você
não precisa adicionar um README pelo GitHub, esta pasta já inclui um
se você o escreveu.

Você pode apagar este arquivo ` + "`PUBLISHING.md`" + ` depois do primeiro push.

## Pré-requisitos

- **Git** instalado: <https://git-scm.com/downloads>
- Uma **conta no GitHub**: <https://github.com/join>

## Passo 1 — Crie um repositório vazio no GitHub

1. Acesse <https://github.com/new>
2. Dê um nome (sugestão: ` + "`{{PROJECT_NAME}}`" + `)
3. Escolha **público** ou **privado**
4. **NÃO** marque nenhuma das opções "Initialize this repository
   with…" (sem README, sem .gitignore, sem LICENSE). Esta pasta já
   traz todos esses arquivos; inicializar pelo GitHub causa
   conflito no primeiro push.
5. Clique em **Create repository**

## Passo 2 — Faça o push a partir desta pasta

Abra um terminal nesta pasta e execute (substitua
` + "`<seu-usuario>`" + ` pelo seu nome de usuário no GitHub):

` + "```bash" + `
git init
git add .
git commit -m "Commit inicial gerado pelo IoTMaker"
git branch -M main
git remote add origin https://github.com/<seu-usuario>/{{PROJECT_NAME}}.git
git push -u origin main
` + "```" + `

Pronto — recarregue a página do GitHub e o projeto estará lá.

## Atualizando depois

Quando você alterar algo no IoTMaker e re-exportar, substitua os
arquivos desta pasta pelo novo export e então execute:

` + "```bash" + `
git add .
git commit -m "Descreva o que mudou"
git push
` + "```" + `

## Licença

Este projeto é publicado sob a **Apache License 2.0**. Veja o
arquivo ` + "`LICENSE`" + ` para o texto completo.
`
