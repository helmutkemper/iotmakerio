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
  # Auto-pick é conveniência, não confiança: Downloads acumula pacotes de
  # eras diferentes e um re-download dá mtime novo a conteúdo velho — foi
  # assim que um tarball antigo rebaixou três arquivos de uma vez em
  # 2026-07-15. Mostre a idade e os irmãos; na dúvida, use T=.
  # English: auto-pick is convenience, not trust — a re-download gives old
  # content a fresh mtime; that's how an old tarball time-traveled three
  # files at once. Show age and siblings; when in doubt, pass T=.
  say "→ auto-pick: $TARBALL"
  say "   modificado: $(date -r "$TARBALL" '+%Y-%m-%d %H:%M' 2>/dev/null || stat -c %y "$TARBALL" 2>/dev/null | cut -d. -f1)"
  OTHERS=$(ls -t "$HOME"/Downloads/iotmaker-*.tar.gz 2>/dev/null | tail -n +2 | head -5 || true)
  if [ -n "$OTHERS" ]; then
    say "   outros na pasta (mais antigos por mtime):"
    while IFS= read -r o; do
      say "     · $(basename "$o")  ($(date -r "$o" '+%Y-%m-%d %H:%M' 2>/dev/null))"
    done <<< "$OTHERS"
  fi
fi
[ -f "$TARBALL" ] || die "não achei: $TARBALL"

# ── 3. inspecionar ANTES de tocar o disco ────────────────────────────────
# Top-level check is DYNAMIC (2026-07-16, after stageWorkspace/ was
# refused): a valid top is any directory that EXISTS at the repo root
# right now — self-maintaining, no list to drift. A package that
# legitimately introduces a brand-new top runs once with
# ALLOW_TOP=<name>. Português: Topo válido = diretório que EXISTE na
# raiz agora — sem lista para drifar. Topo genuinamente novo passa uma
# vez com ALLOW_TOP=<nome>.
BAD=0
while IFS= read -r p; do
  case "$p" in
    /*)      say "  ✖ caminho ABSOLUTO no pacote: $p"; BAD=1 ;;
    *../*)   say "  ✖ caminho com '..': $p";           BAD=1 ;;
  esac
  top=${p%%/*}
  if [ ! -d "$top" ] && [ "$top" != "${ALLOW_TOP:-}" ]; then
    say "  ✖ topo desconhecido (não existe na raiz): $p — novo de verdade? re-rode com ALLOW_TOP=$top"
    BAD=1
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

# Quarentena: pacote aplicado sai da roleta do "mais novo" para sempre.
# English: quarantine — an applied package leaves the newest-roulette.
case "$TARBALL" in
  "$HOME"/Downloads/*)
    mkdir -p "$HOME/Downloads/iotmaker-applied"
    mv "$TARBALL" "$HOME/Downloads/iotmaker-applied/"       && say "→ movido para ~/Downloads/iotmaker-applied/ (fora da roleta)"
    ;;
esac

# ── 6. o git conta o que mudou (e é o desfazer) ──────────────────────────
if command -v git >/dev/null && git rev-parse --git-dir >/dev/null 2>&1; then
  say ""
  say "── git status ────────────────────────────────────────"
  git status --short
  say ""
  say "desfazer tudo:  git checkout -- ."
fi
say "próximo passo típico:  make docker-up-full   (ou só refresh, se for asset estático)"
