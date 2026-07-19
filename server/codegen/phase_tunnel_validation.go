// server/codegen/phase_tunnel_validation.go
//
// The C99 §7 rule, server-side (code-truth): a Sequence phase-tunnel's
// FEED must land in the tunnel's BIRTH phase, and every CONSUMER must
// read in a LATER phase. The frontend enforces this by connector gating
// (in offered only on the natal view, out only on later views), but the
// server is the last line: imported scenes, hand-edited JSON and legacy
// saves bypass the UI, and a violated order would generate silently
// misordered code — a value read before the phase that computes it.
// Diagnostics only, no fixes: the maker owns the scene.
//
// Português: A regra §7 no servidor (verdade-do-código): o FEED de um
// túnel de fase pousa na fase NATAL e todo CONSUMIDOR lê em fase
// POSTERIOR. O frontend garante por gating, mas o servidor é a última
// linha: cenas importadas, JSON editado à mão e saves antigos passam por
// fora da UI, e uma ordem violada geraria código silenciosamente errado
// — valor lido antes da fase que o calcula. Só diagnósticos, sem
// conserto: a cena é do maker.
package codegen

import (
	"fmt"

	"server/codegen/diagnostics"
	"server/codegen/graph"
)

// validatePhaseTunnels walks every StatementTunnel node parented to a
// Sequence scope and checks the §7 temporal order. Non-Sequence parents
// (the Case twin hosts tunnels too, but its branches are alternatives,
// not ordered phases) are skipped. A missing natal mirrors the
// frontend's silent phase-0 fallback; a natal that names an UNKNOWN
// phase warns and skips the order checks — there is no order to check
// against. Devices outside the sequence (or chained tunnels — shells
// belong to no phase) warn instead of erroring: their ordering is not
// governed by the phase machine. Both wire orientations are read — the
// inverted-wire lesson of 2026-07-17 applies to the graph too.
//
// Português: Percorre todo StatementTunnel com pai Sequence e checa a
// ordem §7. Pais não-Sequence (o gêmeo Case também hospeda túneis, mas
// seus ramos são alternativas, não fases ordenadas) são pulados. Natal
// ausente espelha o fallback silencioso de fase 0 do frontend; natal
// apontando fase DESCONHECIDA avisa e pula as checagens — não há ordem
// contra a qual checar. Devices fora do sequence (ou túneis encadeados)
// avisam em vez de errar. As duas orientações de fio contam — a lição
// do fio invertido vale para o grafo também.
func validatePhaseTunnels(g *graph.Graph) []Diagnostic {
	var diags []Diagnostic
	if g == nil {
		return diags
	}

	for id, node := range g.Nodes {
		if node == nil || node.Type != "StatementTunnel" || node.Properties == nil {
			continue
		}
		parent, _ := node.Properties["tunnelParent"].(string)
		natal, _ := node.Properties["tunnelNatal"].(string)
		scope := g.Scopes[parent]
		if scope == nil || !scope.Sequence || len(scope.Cases) == 0 {
			continue
		}

		// Resolve the birth phase. Empty natal = legacy scene → the
		// frontend's phase-0 fallback, silently mirrored. A named but
		// unknown natal is a real inconsistency → warn and skip.
		// Português: Resolve a fase natal. Vazio = cena antiga →
		// fallback fase 0, espelhado em silêncio. Nomeado mas
		// desconhecido = inconsistência real → avisa e pula.
		natalIdx := 0
		if natal != "" {
			natalIdx = -1
			for i, c := range scope.Cases {
				if c.ID == natal {
					natalIdx = i
					break
				}
			}
			if natalIdx < 0 {
				diags = append(diags, Diagnostic{
					Kind:     diagnostics.KindPhaseOrder,
					Severity: diagnostics.SeverityWarning,
					Message: fmt.Sprintf(
						"tunnel %s: birth phase %q not found in sequence %s — phase-order checks skipped",
						id, natal, parent),
				})
				continue
			}
		}

		// Phase index per member device, from the saved membership —
		// the same truth the sequence emitter orders by.
		// Português: Índice de fase por device, da filiação salva — a
		// mesma verdade que o emissor do sequence usa para ordenar.
		memberPhase := make(map[string]int)
		for i, c := range scope.Cases {
			for _, m := range c.IDs {
				memberPhase[m] = i
			}
		}

		phaseName := func(i int) string {
			if i >= 0 && i < len(scope.Cases) && scope.Cases[i].Label != "" {
				return scope.Cases[i].Label
			}
			return fmt.Sprintf("phase %d", i)
		}

		for _, e := range g.Edges {
			if e == nil {
				continue
			}
			// Feed: any wire touching this tunnel's "in", either
			// orientation. Consumer: any wire touching "out".
			// Português: Feed = fio no "in" em qualquer orientação;
			// consumidor = fio no "out".
			var other string
			var port string
			switch {
			case e.To.DeviceID == id:
				other, port = e.From.DeviceID, e.To.PortName
			case e.From.DeviceID == id:
				other, port = e.To.DeviceID, e.From.PortName
			default:
				continue
			}

			idx, member := memberPhase[other]
			switch port {
			case "in":
				if !member {
					diags = append(diags, Diagnostic{
						Kind:     diagnostics.KindPhaseOrder,
						Severity: diagnostics.SeverityWarning,
						Message: fmt.Sprintf(
							"tunnel %s: feed %s is outside sequence %s — phase ordering is not guaranteed",
							id, other, parent),
					})
					continue
				}
				if idx != natalIdx {
					diags = append(diags, Diagnostic{
						Kind:     diagnostics.KindPhaseOrder,
						Severity: diagnostics.SeverityError,
						Message: fmt.Sprintf(
							"tunnel %s: feed %s lands in %q; it must land in the tunnel's birth phase %q",
							id, other, phaseName(idx), phaseName(natalIdx)),
					})
				}
			case "out":
				if !member {
					diags = append(diags, Diagnostic{
						Kind:     diagnostics.KindPhaseOrder,
						Severity: diagnostics.SeverityWarning,
						Message: fmt.Sprintf(
							"tunnel %s: consumer %s is outside sequence %s — phase ordering is not guaranteed",
							id, other, parent),
					})
					continue
				}
				if idx <= natalIdx {
					diags = append(diags, Diagnostic{
						Kind:     diagnostics.KindPhaseOrder,
						Severity: diagnostics.SeverityError,
						Message: fmt.Sprintf(
							"tunnel %s: consumer %s reads in %q, at or before the tunnel's birth phase %q — the value is not computed yet",
							id, other, phaseName(idx), phaseName(natalIdx)),
					})
				}
			}
		}
	}
	return diags
}
