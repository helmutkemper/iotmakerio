// /ide/devices/compFlow/util.go
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only

package compFlow

// util.go — Package-level helpers for the flow/container family. Born
// 2026-07-16 when the Tunnel device immigrated from compVars and left
// its package-private escapeXml behind (the house pattern is one util
// per device package; four siblings already carry their own copy).
//
// Português: Helpers de pacote da família de fluxo/containers. Nasceu
// quando o Tunnel emigrou do compVars e deixou o escapeXml privado para
// trás (o padrão da casa é um util por pacote de devices).

import (
	"strings"

	"github.com/helmutkemper/iotmakerio/rulesDensity"
	"github.com/helmutkemper/iotmakerio/scene"
)

// maintainCaseMembership is the SHARED membership pass of the N-way
// phase/case twins (Sequence and Case), replacing their old
// adopt-then-filterExisting bodies after the 2026-07-18 field collapse
// ("o sequence passou a mostrar todos os elementos na mesma fase" +
// the scene.json autopsy: members conflicted, parentless, and wiped
// from every case). The old filter treated ChildrenOf as ground truth
// and DELETED any member missing from it — but a device leaves
// ChildrenOf for transient reasons too (overlap conflicts at
// import-register time, the container's size race, mid-drag gaps), and
// a conflicted orphan can never re-parent (RefreshAll skips conflicted
// nodes), so one hiccup became permanent membership loss and the
// occlusion lost authority over the device forever: visible in every
// phase. The exact wipe trigger of 2026-07-18 is recorded as partially
// open; this pass removes the whole failure CLASS instead of one
// trigger:
//
//	(a) ADOPT spatial children not yet in any case — the standing rule;
//	(b) ADOPT stranded orphans: registered devices with NO parent whose
//	    box still sits INSIDE this container — the conflicted castaways
//	    ChildrenOf cannot see. They land in the SELECTED case so the
//	    occlusion governs them again (their historical phase is
//	    unknowable after a wipe; the maker redistributes by drag);
//	(c) FILTER conservatively: a member is dropped ONLY when it is
//	    REALLY gone — deleted, parented into a DIFFERENT container, or
//	    parentless AND outside our box. Parentless-but-inside keeps its
//	    membership (benefit of the doubt: transient conflict or race).
//	    Tunnel shells are always dropped (border furniture, never
//	    members — legacy saves may still carry them).
//
// Português: O passe de filiação COMPARTILHADO dos gêmeos N-vias,
// substituindo o antigo adota-e-filtra após o colapso de campo de
// 2026-07-18. O filtro antigo tratava ChildrenOf como verdade absoluta
// e APAGAVA membro ausente — mas um device sai de ChildrenOf por
// razões transitórias (conflito de overlap no registro do import,
// corrida de tamanho do container, lacunas de drag), e um órfão em
// conflito nunca se re-parenteia (o RefreshAll pula conflitados):
// um soluço virava perda permanente e a oclusão perdia autoridade —
// visível em toda fase. Este passe remove a CLASSE inteira de falha:
// (a) ADOTA filhos espaciais sem fase; (b) ADOTA órfãos encalhados
// (sem pai, caixa DENTRO do container) na fase selecionada — a
// oclusão volta a governá-los e o maker redistribui arrastando;
// (c) FILTRA conservador: só sai quem REALMENTE se foi — apagado,
// parentado em OUTRO container, ou órfão E fora da caixa. Órfão
// dentro mantém a filiação (benefício da dúvida). Cascas de túnel
// sempre saem (móvel de borda, nunca membro).
func maintainCaseMembership(sceneMgr *scene.Serializer, containerID string,
	inner *scene.Rect, cases []caseEntry, selIdx int) {
	if sceneMgr == nil {
		return
	}

	containedIDs := sceneMgr.ChildrenOf(containerID)
	containedSet := make(map[string]bool, len(containedIDs))
	for _, id := range containedIDs {
		containedSet[id] = true
	}

	known := make(map[string]bool)
	for _, c := range cases {
		for _, id := range c.ids {
			known[id] = true
		}
	}

	// (a) The standing adoption: spatial children not yet in any case.
	for _, id := range containedIDs {
		if !known[id] && selIdx >= 0 {
			cases[selIdx].ids = append(cases[selIdx].ids, id)
			known[id] = true
		}
	}

	// (b) Stranded-orphan adoption — only possible with a known box.
	if inner != nil && selIdx >= 0 {
		for _, id := range sceneMgr.RegisteredIDs() {
			if id == containerID || known[id] || strings.HasPrefix(id, "tunnel") {
				continue
			}
			if sceneMgr.ParentOf(id) != "" {
				continue
			}
			dev := sceneMgr.FindDevice(id)
			if dev == nil || !rectContainsRect(*inner, dev.GetOuterBBox()) {
				continue
			}
			cases[selIdx].ids = append(cases[selIdx].ids, id)
			known[id] = true
		}
	}

	// (c) The conservative filter.
	keep := func(id string) bool {
		if strings.HasPrefix(id, "tunnel") {
			return false
		}
		if containedSet[id] {
			return true
		}
		dev := sceneMgr.FindDevice(id)
		if dev == nil {
			return false // deleted for real
		}
		if p := sceneMgr.ParentOf(id); p != "" && p != containerID {
			return false // moved into another container for real
		}
		// Parentless: keep while still inside us; unknowable box (inner
		// nil) also keeps — never destroy membership on missing data.
		// Português: Órfão — fica enquanto estiver dentro; caixa
		// desconhecida também fica (nunca destruir filiação sem dado).
		if inner == nil {
			return true
		}
		return rectContainsRect(*inner, dev.GetOuterBBox())
	}
	for i := range cases {
		kept := cases[i].ids[:0]
		for _, id := range cases[i].ids {
			if keep(id) {
				kept = append(kept, id)
			}
		}
		cases[i].ids = kept
	}
}

// rectContainsRect reports whether inner fully contains outer, with a
// half-pixel epsilon for float fuzz — the same containment reading the
// scenegraph's findParent applies. Português: Contém com meia-pixel de
// folga — a mesma leitura de contenção do findParent.
func rectContainsRect(inner, outer scene.Rect) bool {
	const eps = 0.5
	return outer.X >= inner.X-eps &&
		outer.Y >= inner.Y-eps &&
		outer.X+outer.Width <= inner.X+inner.Width+eps &&
		outer.Y+outer.Height <= inner.Y+inner.Height+eps
}

// escapeXml replaces the five XML special characters so that user-provided
// names can be safely embedded inside SVG text elements without breaking the
// markup or creating injection vectors.
//
// Português: Escapa os cinco caracteres especiais de XML para embutir nomes
// do usuário em SVG com segurança.
func escapeXml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// Phase-tunnel constants (2026-07-17). kTunnelSide is the square's edge
// in density units — marker-family scale, comfortably clickable.
// Português: Constantes do túnel de fase — quadrado escala-de-marker.
const (
	kTunnelViolet   = "#8b5cf6"
	kTunnelBlinkRed = "#ef4444"
)

var kTunnelSide = rulesDensity.Density(18)
