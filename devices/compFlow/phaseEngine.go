// devices/compFlow/phaseEngine.go
//
// phaseEngine is THE single phase machine shared by the phase-hosting
// containers (Function today; Case and Sequence migrate next, one
// slice at a time). It owns: the ordered phase list, the selected
// phase, membership maintenance (adopt-on-sight with pruning and the
// tunnel seat-belt), the visibility pass, the menu section, and the
// JSON-string serialization. Hosts inject their world through bound
// closures and keep only their specifics — extracting this ends the
// twin-duplication era (Kemper 2026-07-20: divergent behaviors mean
// the wheel is being rewritten; it was — this is the one wheel).
//
// Português: phaseEngine é A máquina de fases única compartilhada
// pelos containers hospedeiros (Function hoje; Case e Sequence migram
// nas próximas fatias). Possui: lista ordenada, fase selecionada,
// membresia (adoção-na-vista com poda e o cinto dos túneis), passe de
// visibilidade, seção de menu e serialização em string JSON. Os
// hospedeiros injetam seu mundo por closures e ficam só com as
// especificidades — a extração encerra a era da duplicação de gêmeos.
package compFlow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/helmutkemper/iotmakerio/rulesIcon"
	"github.com/helmutkemper/iotmakerio/translate"
	"github.com/helmutkemper/iotmakerio/ui/contextMenu"
)

// phaseEntry is one ordered phase. Português: Uma fase ordenada.
type phaseEntry struct {
	id    string
	label string
	ids   []string

	// matchKind/values — the Case/Sequence twins' selector metadata
	// (e.g. "is" + ["3"]). The Function leaves them empty; the engine
	// carries them opaquely so the twins can migrate without a second
	// model. Português: Metadados de seleção dos gêmeos Case/Sequence;
	// a Function os deixa vazios — o engine os carrega opacamente.
	matchKind string
	values    []string
	isDefault bool
}

// phaseEngine — see the file header. Hosts bind the closures once via
// bind(); every public method is nil-safe before binding. Português:
// Ver cabeçalho; hospedeiros ligam as closures via bind(); métodos
// são nil-safe antes disso.
type phaseEngine struct {
	entries  []phaseEntry
	selected string

	children         func() []string
	exempt           func(id string) bool
	setElemVisible   func(id string, show bool)
	setWireHidden    func(id string, hidden bool)
	setSpatialHidden func(id string, hidden bool)
	tail             func()
	notify           func()
	onSelect         func(id string)

	// maintainHook — when bound, membership maintenance runs through
	// the DOCTRINE implementation (maintainCaseMembership in util.go:
	// spatial innerBBox adoption, the 2026-07-18 resilient pass) —
	// one adoption behavior for every host. The built-in fallback
	// below stays for hosts that haven't bound it. Português: Com o
	// hook, a manutenção roda pela implementação DOUTRINÁRIA
	// (adoção espacial, passe resiliente) — um comportamento só.
	maintainHook func(entries []phaseEntry, selectedIdx int)

	// labelFn — optional custom entry label (the Case shows its match,
	// e.g. "== 3"); nil falls back to the "phase N" standard.
	// Português: Rótulo customizado opcional (o Case mostra o match);
	// nil cai no padrão "phase N".
	labelFn func(i int, e *phaseEntry) string
}

// ensureDefault guarantees phase 0 exists so legacy scenes keep
// behaving. Português: Garante a phase 0 — cenas antigas seguem.
func (p *phaseEngine) ensureDefault() {
	if len(p.entries) > 0 {
		if p.selected == "" {
			p.selected = p.entries[0].id
		}
		return
	}
	p.entries = []phaseEntry{{id: "phase_0"}}
	p.selected = "phase_0"
}

// freshID mints an unused "phase_N". Português: Cunha id livre.
func (p *phaseEngine) freshID() string {
	for n := len(p.entries); ; n++ {
		id := fmt.Sprintf("phase_%d", n)
		taken := false
		for i := range p.entries {
			if p.entries[i].id == id {
				taken = true
				break
			}
		}
		if !taken {
			return id
		}
	}
}

// indexOf — run-order position (-1 unknown). Português: Posição (-1).
func (p *phaseEngine) indexOf(id string) int {
	for i := range p.entries {
		if p.entries[i].id == id {
			return i
		}
	}
	return -1
}

// menuLabel — the Sequence's literal standard: "phase N", ZERO-based
// (Kemper 2026-07-20). Português: O padrão literal do Sequence:
// "phase N", base ZERO.
func (p *phaseEngine) menuLabel(i int) string {
	if p.labelFn != nil {
		if s := p.labelFn(i, &p.entries[i]); s != "" {
			return s
		}
	}
	if p.entries[i].label != "" {
		return p.entries[i].label
	}
	return fmt.Sprintf("phase %d", i)
}

// menuItems builds the phase section: one entry per phase in order
// (the selected wears the eye) + "New phase". Hosts append their
// specifics after. Português: Seção de fases — entrada por fase (a
// ativa leva o olho) + "New phase"; hospedeiros anexam o resto.
func (p *phaseEngine) menuItems(idPrefix string) []contextMenu.Item {
	p.ensureDefault()
	items := make([]contextMenu.Item, 0, len(p.entries)+1)
	for i := range p.entries {
		id := p.entries[i].id
		item := contextMenu.Item{
			ID:           idPrefix + "_phase_" + id,
			Label:        p.menuLabel(i),
			HelpFallback: "Show this phase on the stage; new devices join the visible phase.",
			OnClick:      func() { p.selectPhase(id) },
		}
		if id == p.selected {
			item.FontAwesomePath = rulesIcon.KFAEye
			item.ViewBox = "0 0 512 512"
		}
		items = append(items, item)
	}
	items = append(items, contextMenu.Item{
		ID:              idPrefix + "_phase_add",
		Label:           translate.T("phaseAdd", "New phase"),
		FontAwesomePath: seqIconPlus.Path,
		ViewBox:         seqIconPlus.ViewBox,
		HelpFallback:    "Add an ordered phase; phases run one after the other in the generated code.",
		OnClick:         func() { p.addPhase() },
	})
	return items
}

// addPhase appends and selects a fresh phase. Português: Anexa e seleciona.
func (p *phaseEngine) addPhase() {
	p.ensureDefault()
	fresh := phaseEntry{id: p.freshID()}
	p.entries = append(p.entries, fresh)
	p.selectPhase(fresh.id)
}

// selectPhase makes a phase the visible/edited one. Português: Torna
// a fase a visível/editada.
func (p *phaseEngine) selectPhase(id string) {
	// Adopt BEFORE switching (the Sequence's doctrine, S2b): strays
	// placed while the OLD phase was visible belong to it — running
	// maintenance after the switch mis-attributed them to the new
	// phase. Português: Adota ANTES de trocar — órfãos colocados com a
	// fase ANTIGA visível pertencem a ela; manter depois os atribuía
	// errado à nova.
	p.maintain()
	p.selected = id
	p.applyVisibility()
	if p.onSelect != nil {
		p.onSelect(id)
	}
	if p.notify != nil {
		p.notify()
	}
}

// maintain adopts unassigned children into the selected phase and
// prunes departures; exempt ids (border furniture) never join.
// Português: Adota filhos sem fase na selecionada e poda saídas;
// isentos (mobília de borda) nunca entram.
func (p *phaseEngine) maintain() {
	if p.maintainHook != nil {
		p.ensureDefault()
		p.maintainHook(p.entries, p.indexOf(p.selected))
		return
	}
	if p.children == nil {
		return
	}
	p.ensureDefault()
	kids := map[string]bool{}
	for _, id := range p.children() {
		if p.exempt != nil && p.exempt(id) {
			continue
		}
		kids[id] = true
	}
	known := map[string]bool{}
	for i := range p.entries {
		kept := p.entries[i].ids[:0]
		for _, id := range p.entries[i].ids {
			if kids[id] {
				kept = append(kept, id)
				known[id] = true
			}
		}
		p.entries[i].ids = kept
	}
	sel := 0
	for i := range p.entries {
		if p.entries[i].id == p.selected {
			sel = i
			break
		}
	}
	for id := range kids {
		if !known[id] {
			p.entries[sel].ids = append(p.entries[sel].ids, id)
		}
	}
}

// applyVisibility — the proven pass: only the selected phase's members
// stay visible, in the wire layer and in collision; exempt ids
// untouched; the host tail (tunnel refresh) rides the funnel.
// Português: O passe provado — só a fase ativa visível, na wire layer
// e na colisão; isentos intocados; a cauda do hospedeiro pega carona.
func (p *phaseEngine) applyVisibility() {
	for i := range p.entries {
		show := p.entries[i].id == p.selected
		for _, id := range p.entries[i].ids {
			if p.exempt != nil && p.exempt(id) {
				continue
			}
			if p.setElemVisible != nil {
				p.setElemVisible(id, show)
			}
			if p.setWireHidden != nil {
				p.setWireHidden(id, !show)
			}
			if p.setSpatialHidden != nil {
				p.setSpatialHidden(id, !show)
			}
		}
	}
	if p.tail != nil {
		p.tail()
	}
}

// phaseWire is the serialization shape. Português: Forma serializada.
type phaseWire struct {
	ID        string   `json:"id"`
	Label     string   `json:"label,omitempty"`
	IDs       []string `json:"ids,omitempty"`
	MatchKind string   `json:"matchKind,omitempty"`
	Values    []string `json:"values,omitempty"`
	IsDefault bool     `json:"isDefault,omitempty"`
}

// marshal → the flattener-safe JSON string (+ selected). Português:
// String JSON à prova do achatador (+ selecionada).
func (p *phaseEngine) marshal() (entriesJSON, selected string, ok bool) {
	if len(p.entries) == 0 {
		return "", "", false
	}
	ls := make([]phaseWire, 0, len(p.entries))
	for k := range p.entries {
		ls = append(ls, phaseWire{
			ID:        p.entries[k].id,
			Label:     p.entries[k].label,
			IDs:       append([]string(nil), p.entries[k].ids...),
			MatchKind: p.entries[k].matchKind,
			Values:    append([]string(nil), p.entries[k].values...),
			IsDefault: p.entries[k].isDefault,
		})
	}
	b, err := json.Marshal(ls)
	if err != nil {
		return "", "", false
	}
	return string(b), p.selected, true
}

// restore reads the JSON string back. Português: Lê a string de volta.
func (p *phaseEngine) restore(entriesJSON, selected string) {
	if entriesJSON != "" {
		var ls []phaseWire
		if err := json.Unmarshal([]byte(entriesJSON), &ls); err == nil {
			p.entries = p.entries[:0]
			for _, l := range ls {
				if l.ID != "" {
					p.entries = append(p.entries, phaseEntry{
						id: l.ID, label: l.Label, ids: l.IDs,
						matchKind: l.MatchKind, values: l.Values,
						isDefault: l.IsDefault,
					})
				}
			}
		}
	}
	if selected != "" {
		p.selected = selected
	}
}

// tunnelExempt is the shared seat-belt predicate hosts reuse.
// Português: O predicado do cinto compartilhado.
func tunnelExempt(id string) bool {
	return strings.HasPrefix(id, "tunnel")
}
