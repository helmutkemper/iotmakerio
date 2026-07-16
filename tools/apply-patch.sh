#!/usr/bin/env bash
# tools/apply-patch.sh — safe applier for Claude's delivery tarballs.
#
# SPDX-FileCopyrightText: 2026 Helmut Kemper
# SPDX-License-Identifier: AGPL-3.0-only
#
# Born 2026-07-15 after a season of hand-applied patches: a file landed in
# the wrong app, a nest (server/server/) once ate an evening, and one
# tarball out of four was skipped in a copy-paste. This script encodes the
# ritual: run from the repo ROOT, inspect BEFORE touching disk, show what
# changes, extract only after a yes, and let git be the undo.
#
# Português: Nasceu depois de uma temporada de patches aplicados à mão:
# arquivo no app errado, um ninho (server/server/) que comeu uma noite, e
# um tarball de quatro pulado no copiar-e-colar. O script codifica o
# ritual: rodar da RAIZ, inspecionar ANTES de tocar o disco, mostrar o que
# muda, extrair só após um sim — e o git é o desfazer.
#
# Uso | Usage:
#   ./tools/apply-patch.sh caminho/do/pacote.tar.gz
#   ./tools/apply-patch.sh              # pega o iotmaker-*.tar.gz mais novo de ~/Downloads
#   AUTO=1 ./tools/apply-patch.sh ...   # sem prompt (CI/apressados)

set -euo pipefail

say()  { printf '%b\n' "$*"; }
die()  { say "✖ $*" >&2; exit 1; }

# ── 1. raiz do repo, sempre ───────────────────────────────────────────────
[ -f go.mod ] && [ -d server ] && [ -d wire ] \
  || die "rode da RAIZ do repo (onde vivem go.mod, server/, wire/) — você está em: $(pwd)"

# ── 2. escolher o pacote ──────────────────────────────────────────────────
TARBALL="${1:-}"
if [ -z "$TARBALL" ]; then
  TARBALL=$(ls -t "$HOME"/Downloads/iotmaker-*.tar.gz 2>/dev/null | head -1 || true)
  [ -n "$TARBALL" ] || die "nenhum iotmaker-*.tar.gz em ~/Downloads — passe o caminho como argumento"
  say "→ mais novo em Downloads: $TARBALL"
fi
[ -f "$TARBALL" ] || die "não achei: $TARBALL"

# ── 3. inspecionar ANTES de tocar o disco ────────────────────────────────
# Whitelist dos diretórios de topo do repo — um caminho fora daqui é
# tarball errado ou ninho em formação. Português: Fora da lista = pacote
# errado ou ninho.
ALLOW='^(server|wire|devices|ui|blackbox|factoryDevice|docs|tools|00_howto)/'
BAD=0
while IFS= read -r p; do
  case "$p" in
    /*)      say "  ✖ caminho ABSOLUTO no pacote: $p"; BAD=1 ;;
    *../*)   say "  ✖ caminho com '..': $p";           BAD=1 ;;
  esac
  if ! printf '%s' "$p" | grep -Eq "$ALLOW"; then
    say "  ✖ topo fora da whitelist: $p"; BAD=1
  fi
  case "$p" in
    server/server/*|*/wire/wire/*|server/wire/*|server/devices/*|server/ui/overlay/*)
      say "  ✖ cheiro de NINHO: $p"; BAD=1 ;;
  esac
done < <(tar tzf "$TARBALL" | grep -v '/$')
[ "$BAD" -eq 0 ] || die "pacote reprovado na inspeção — nada foi tocado"

# ── 4. resumo novo/muda/igual ─────────────────────────────────────────────
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
tar xzf "$TARBALL" -C "$TMP"
say ""
say "── o que este pacote faz ─────────────────────────────"
CHANGES=0
while IFS= read -r p; do
  if [ ! -f "$p" ]; then
    say "  [novo ] $p"; CHANGES=1
  elif ! cmp -s "$TMP/$p" "$p"; then
    say "  [muda ] $p"; CHANGES=1
  else
    say "  [igual] $p"
  fi
done < <(tar tzf "$TARBALL" | grep -v '/$')
[ "$CHANGES" -eq 1 ] || { say "── tudo já aplicado — nada a fazer."; exit 0; }

# ── 5. confirmar e aplicar ────────────────────────────────────────────────
if [ "${AUTO:-0}" != "1" ]; then
  printf 'aplicar? [s/N] '
  read -r ans
  case "$ans" in s|S|y|Y) ;; *) die "abortado — nada foi tocado";; esac
fi
tar xzf "$TARBALL"
say "✔ aplicado."

# ── 6. o git conta o que mudou (e é o desfazer) ──────────────────────────
if command -v git >/dev/null && git rev-parse --git-dir >/dev/null 2>&1; then
  say ""
  say "── git status ────────────────────────────────────────"
  git status --short
  say ""
  say "desfazer tudo:  git checkout -- ."
fi
say "próximo passo típico:  make docker-up-full   (ou só refresh, se for asset estático)"
