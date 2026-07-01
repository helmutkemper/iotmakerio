// server/public/control/js/pages/groups.js — User Groups management.
//
// CRUD for user_groups + member management with autocomplete email search.
//
// Endpoints used (all under /api/control/v1):
//   GET    /groups                        — list all groups
//   POST   /groups                        — create a group
//   PUT    /groups/:id                    — update name/description
//   DELETE /groups/:id                    — delete group
//   GET    /groups/:id/members            — list members
//   POST   /groups/:id/members            — add member { user_id }
//   DELETE /groups/:id/members/:user_id   — remove member
//   GET    /users?q=...&limit=20          — autocomplete search

import { cpApi }              from '../api.js';
import { cpToast, cpConfirm } from '../toast.js';

// ── State ────────────────────────────────────────────────────────────────────

let _groups    = [];
let _activeGrp = null;   // group being viewed/edited
let _members   = [];
let _acTimer   = null;

// ── Main render ──────────────────────────────────────────────────────────────

export async function renderGroups(root) {
    _groups = [];
    _activeGrp = null;
    _members = [];

    root.innerHTML = `
<div style="max-width:960px;margin:0 auto">
  <div style="display:flex;align-items:center;gap:12px;margin-bottom:20px">
    <h3 style="margin:0;font-size:16px;font-weight:700">
      <i class="fa-solid fa-users" style="margin-right:6px;color:var(--primary)"></i>
      Grupos de Usuários
    </h3>
    <button class="cp-btn cp-btn-primary" onclick="grpShowCreate()" style="margin-left:auto;font-size:12px">
      <i class="fa-solid fa-plus"></i> Novo Grupo
    </button>
  </div>

  <div id="grp-content"><div style="color:var(--text-muted);font-size:13px">Carregando...</div></div>
</div>`;

    await _loadGroups();
}

async function _loadGroups() {
    const r = await cpApi('GET', '/api/control/v1/groups');
    if (r?.metadata?.status !== 200) {
        document.getElementById('grp-content').innerHTML =
            '<span style="color:var(--danger)">Erro ao carregar grupos.</span>';
        return;
    }
    _groups = r.data?.groups || [];
    _renderList();
}

// ── Group list ───────────────────────────────────────────────────────────────

function _renderList() {
    const c = document.getElementById('grp-content');
    if (!c) return;

    if (_groups.length === 0) {
        c.innerHTML = `<div style="text-align:center;padding:40px;color:var(--text-muted);font-size:13px">
            Nenhum grupo criado. Clique em "Novo Grupo" para começar.
        </div>`;
        return;
    }

    c.innerHTML = `
<div style="border:1px solid var(--border);border-radius:8px;background:var(--bg-surface);overflow:hidden">
  ${_groups.map(g => `
  <div style="display:flex;align-items:center;gap:8px;padding:12px 16px;border-bottom:1px solid var(--border);
       cursor:pointer;transition:background .1s"
       onmouseenter="this.style.background='rgba(255,255,255,0.03)'"
       onmouseleave="this.style.background='none'"
       onclick="grpView('${esc(g.id)}')">
    <i class="fa-solid fa-users" style="color:var(--primary);width:20px;text-align:center"></i>
    <div style="flex:1;min-width:0">
      <div style="font-size:14px;font-weight:600">${esc(g.name)}</div>
      ${g.description ? `<div style="font-size:12px;color:var(--text-muted);margin-top:2px">${esc(g.description)}</div>` : ''}
    </div>
    <i class="fa-solid fa-chevron-right" style="color:var(--text-muted);font-size:11px"></i>
  </div>`).join('')}
</div>`;
}

// ── View group (members) ─────────────────────────────────────────────────────

window.grpView = async function(id) {
    _activeGrp = _groups.find(g => g.id === id);
    if (!_activeGrp) return;

    const c = document.getElementById('grp-content');
    c.innerHTML = '<div style="color:var(--text-muted);font-size:13px">Carregando membros...</div>';

    const r = await cpApi('GET', `/api/control/v1/groups/${id}/members`);
    _members = r?.data?.members || [];

    _renderGroupDetail(c);
};

function _renderGroupDetail(container) {
    const g = _activeGrp;

    container.innerHTML = `
<!-- Back + title -->
<div style="display:flex;align-items:center;gap:8px;margin-bottom:16px">
  <button class="cp-btn cp-btn-ghost" onclick="grpBack()" style="padding:4px 10px;font-size:12px">
    <i class="fa-solid fa-arrow-left"></i>
  </button>
  <div>
    <span style="font-size:16px;font-weight:700">${esc(g.name)}</span>
    <button class="cp-btn cp-btn-ghost" onclick="grpEdit()" style="padding:2px 6px;font-size:11px;margin-left:6px" title="Editar">
      <i class="fa-solid fa-pen" style="color:var(--text-muted)"></i>
    </button>
    <button class="cp-btn cp-btn-ghost" onclick="grpDelete()" style="padding:2px 6px;font-size:11px" title="Excluir grupo">
      <i class="fa-solid fa-trash" style="color:var(--danger,#e53935)"></i>
    </button>
  </div>
</div>
${g.description ? `<div style="font-size:13px;color:var(--text-muted);margin-bottom:16px">${esc(g.description)}</div>` : ''}

<!-- Add member -->
<div style="position:relative;max-width:400px;margin-bottom:16px">
  <div style="display:flex;gap:6px">
    <div style="flex:1;position:relative">
      <input type="text" class="fc" id="grp-add-input" placeholder="Buscar usuário por e-mail..."
             autocomplete="off" style="width:100%;font-size:13px"
             oninput="grpAcSearch()" onkeydown="grpAcKeydown(event)">
      <div id="grp-ac-dropdown" style="display:none;position:absolute;left:0;right:0;top:100%;
           z-index:100;margin-top:2px;max-height:260px;overflow-y:auto;
           background:var(--bg-card);border:1px solid var(--border);border-radius:8px;
           box-shadow:0 8px 24px rgba(0,0,0,0.25)"></div>
    </div>
  </div>
</div>

<!-- Members list -->
<div style="font-size:12px;color:var(--text-muted);margin-bottom:8px">${_members.length} membro(s)</div>
<div id="grp-members">
  ${_renderMembers()}
</div>`;

    // Close dropdown on outside click.
    document.addEventListener('click', function _outsideClick(e) {
        const dd = document.getElementById('grp-ac-dropdown');
        const input = document.getElementById('grp-add-input');
        if (!dd) { document.removeEventListener('click', _outsideClick); return; }
        if (!dd.contains(e.target) && e.target !== input) dd.style.display = 'none';
    });
}

function _renderMembers() {
    if (_members.length === 0) {
        return `<div style="text-align:center;padding:20px;color:var(--text-muted);font-size:12px;
                     border:1px solid var(--border);border-radius:8px;background:var(--bg-surface)">
            Nenhum membro neste grupo.</div>`;
    }

    return `<div style="border:1px solid var(--border);border-radius:8px;background:var(--bg-surface);overflow:hidden">
    ${_members.map(m => `
    <div style="display:flex;align-items:center;gap:8px;padding:8px 14px;border-bottom:1px solid var(--border)">
      <i class="fa-solid fa-user" style="color:var(--text-muted);width:14px;text-align:center;font-size:11px"></i>
      <span style="flex:1;font-size:13px">${esc(m.user_id)}</span>
      <span style="font-size:11px;color:var(--text-muted)">${esc(m.source)}</span>
      <button class="cp-btn cp-btn-ghost" onclick="grpRemoveMember('${esc(m.user_id)}')"
              style="padding:2px 6px;font-size:11px;color:var(--danger,#e53935)" title="Remover">
        <i class="fa-solid fa-xmark"></i>
      </button>
    </div>`).join('')}
  </div>`;
}

// ── Autocomplete for adding members ──────────────────────────────────────────

window.grpAcSearch = function() {
    if (_acTimer) clearTimeout(_acTimer);
    const q = (document.getElementById('grp-add-input')?.value || '').trim();
    const dd = document.getElementById('grp-ac-dropdown');
    if (!dd) return;
    if (q.length < 2) { dd.style.display = 'none'; return; }

    _acTimer = setTimeout(async () => {
        const r = await cpApi('GET', `/api/control/v1/users?q=${encodeURIComponent(q)}&limit=20&page=1`);
        const users = r?.data?.users || [];
        if (users.length === 0) {
            dd.innerHTML = '<div style="padding:10px 14px;font-size:12px;color:var(--text-muted)">Nenhum resultado</div>';
            dd.style.display = 'block';
            return;
        }

        // Filter out users already in the group.
        const existing = new Set(_members.map(m => m.user_id));

        dd.innerHTML = users.map((u, i) => `
<div class="grp-ac-item" data-idx="${i}" data-uid="${esc(u.id)}"
     style="padding:8px 14px;cursor:pointer;border-bottom:1px solid var(--border);transition:background .1s"
     onmouseenter="this.style.background='var(--bg-surface)'"
     onmouseleave="this.style.background='none'"
     onclick="grpAcSelect('${esc(u.id)}')">
  <div style="font-size:13px;font-weight:500;display:flex;align-items:center;gap:6px">
    ${esc(u.username)}
    ${existing.has(u.id) ? '<span style="font-size:10px;color:var(--text-muted)">(já membro)</span>' : ''}
  </div>
  <div style="font-size:11px;color:var(--text-muted)">${esc(u.email)}</div>
</div>`).join('');
        dd.style.display = 'block';
    }, 250);
};

window.grpAcKeydown = function(e) {
    const dd = document.getElementById('grp-ac-dropdown');
    if (!dd || dd.style.display === 'none') return;
    const items = dd.querySelectorAll('.grp-ac-item');
    let active = dd.querySelector('.grp-ac-item[data-active="1"]');
    let idx = active ? parseInt(active.dataset.idx) : -1;

    if (e.key === 'ArrowDown') { e.preventDefault(); idx = Math.min(idx + 1, items.length - 1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); idx = Math.max(idx - 1, 0); }
    else if (e.key === 'Enter' && active) { e.preventDefault(); grpAcSelect(active.dataset.uid); return; }
    else if (e.key === 'Escape') { dd.style.display = 'none'; return; }
    else return;

    items.forEach(el => { el.dataset.active = '0'; el.style.background = 'none'; });
    if (items[idx]) { items[idx].dataset.active = '1'; items[idx].style.background = 'var(--bg-surface)'; items[idx].scrollIntoView({ block: 'nearest' }); }
};

window.grpAcSelect = async function(userId) {
    const dd = document.getElementById('grp-ac-dropdown');
    if (dd) dd.style.display = 'none';
    const input = document.getElementById('grp-add-input');
    if (input) input.value = '';

    if (!_activeGrp) return;

    const r = await cpApi('POST', `/api/control/v1/groups/${_activeGrp.id}/members`, { user_id: userId });
    if (r?.metadata?.status === 201 || r?.metadata?.status === 200) {
        cpToast('success', 'Membro adicionado.');
        grpView(_activeGrp.id);
    } else {
        cpToast('error', r?.metadata?.error || 'Erro ao adicionar membro.');
    }
};

// ── Actions ──────────────────────────────────────────────────────────────────

window.grpBack = function() { _activeGrp = null; _renderList(); };

window.grpShowCreate = function() {
    _showGroupModal('Novo Grupo', '', '', async (name, desc) => {
    const r = await cpApi('POST', '/api/control/v1/groups', { name, description: desc });
    if (r?.metadata?.status === 201 || r?.metadata?.status === 200) {
        cpToast('success', `Grupo "${name}" criado.`);
        await _loadGroups();
    } else {
        cpToast('error', r?.metadata?.error || 'Erro ao criar grupo.');
    }
    });
};

window.grpEdit = function() {
    if (!_activeGrp) return;
    _showGroupModal('Editar Grupo', _activeGrp.name, _activeGrp.description || '', async (name, desc) => {
    const r = await cpApi('PUT', `/api/control/v1/groups/${_activeGrp.id}`, { name, description: desc });
    if (r?.metadata?.status === 200) {
        cpToast('success', 'Grupo atualizado.');
        _activeGrp.name = name;
        _activeGrp.description = desc;
        await _loadGroups();
        grpView(_activeGrp.id);
    } else {
        cpToast('error', r?.metadata?.error || 'Erro ao atualizar.');
    }
    });
};

// ── Group modal (replaces native prompt) ─────────────────────────────────────

function _showGroupModal(title, nameVal, descVal, onConfirm) {
    document.getElementById('grp-modal')?.remove();

    const backdrop = document.createElement('div');
    backdrop.id = 'grp-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:420px">
  <h3><i class="fa-solid fa-users" style="margin-right:6px;color:var(--primary)"></i>${esc(title)}</h3>

  <div style="margin-top:16px">
    <label style="display:block;font-size:12px;font-weight:600;color:var(--text-muted);margin-bottom:4px">Nome *</label>
    <input type="text" class="fc" id="grp-modal-name" value="${esc(nameVal)}"
           placeholder="Ex: school_kids, brazil_makers"
           style="width:100%;font-size:13px" autofocus>
  </div>

  <div style="margin-top:12px">
    <label style="display:block;font-size:12px;font-weight:600;color:var(--text-muted);margin-bottom:4px">Descrição</label>
    <input type="text" class="fc" id="grp-modal-desc" value="${esc(descVal)}"
           placeholder="Descrição opcional"
           style="width:100%;font-size:13px">
  </div>

  <div style="display:flex;gap:8px;margin-top:20px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" id="grp-modal-cancel">Cancelar</button>
    <button class="cp-btn cp-btn-primary" id="grp-modal-ok">Salvar</button>
  </div>
</div>`;

    document.body.appendChild(backdrop);

    function close() { backdrop.remove(); }

    document.getElementById('grp-modal-cancel').addEventListener('click', close);
    backdrop.addEventListener('click', (e) => { if (e.target === backdrop) close(); });

    document.getElementById('grp-modal-ok').addEventListener('click', () => {
        const name = (document.getElementById('grp-modal-name')?.value || '').trim();
        if (!name) {
            cpToast('error', 'Nome é obrigatório.');
            return;
        }
        const desc = (document.getElementById('grp-modal-desc')?.value || '').trim();
        close();
        onConfirm(name, desc);
    });

    // Enter to confirm.
    backdrop.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') document.getElementById('grp-modal-ok')?.click();
        if (e.key === 'Escape') close();
    });

    // Focus the name input.
    setTimeout(() => document.getElementById('grp-modal-name')?.focus(), 50);
}

window.grpDelete = async function() {
    if (!_activeGrp) return;
    if (!await cpConfirm(
        '<i class="fa-solid fa-trash"></i> Excluir grupo',
        `Excluir "<b>${esc(_activeGrp.name)}</b>"? Todos os membros serão removidos.`,
        'Excluir', 'Cancelar'
    )) return;

    const r = await cpApi('DELETE', `/api/control/v1/groups/${_activeGrp.id}`);
    if (r?.metadata?.status === 200) {
        cpToast('success', 'Grupo excluído.');
        _activeGrp = null;
        await _loadGroups();
    } else {
        cpToast('error', r?.metadata?.error || 'Erro ao excluir.');
    }
};

window.grpRemoveMember = async function(userId) {
    if (!_activeGrp) return;
    if (!await cpConfirm(
        '<i class="fa-solid fa-xmark"></i> Remover membro',
        'Remover este usuário do grupo?',
        'Remover', 'Cancelar'
    )) return;

    const r = await cpApi('DELETE', `/api/control/v1/groups/${_activeGrp.id}/members/${userId}`);
    if (r?.metadata?.status === 200) {
        cpToast('success', 'Membro removido.');
        grpView(_activeGrp.id);
    } else {
        cpToast('error', r?.metadata?.error || 'Erro ao remover.');
    }
};

// ── Helpers ──────────────────────────────────────────────────────────────────

function esc(s) { return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/"/g,'&quot;'); }
