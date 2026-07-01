// server/public/control/js/pages/userMenuPrefs.js — Admin user menu prefs editor.
//
// Allows the admin to look up a user by email and manage their per-item
// menu visibility preferences (the same checkboxes the user sees in their
// own Editor Settings > Menu page, but controlled by the admin).
//
// Flow:
//   1. Admin types user email → clicks "Buscar"
//   2. User info displayed + full menu tree with checkboxes
//   3. Admin checks/unchecks items → auto-saves with debounce
//   4. "Reset" restores all overrides to admin defaults

import { cpApi }              from '../api.js';
import { cpToast }            from '../toast.js';

// ── State ────────────────────────────────────────────────────────────────────

let _userId   = '';
let _userInfo = null;
let _menuTree = [];
let _hidden   = new Set();
let _saveTimer = null;
let _acTimer  = null;  // autocomplete debounce

// ── Main render ──────────────────────────────────────────────────────────────

export function renderUserMenuPrefs(root) {
    _userId   = '';
    _userInfo = null;
    _menuTree = [];
    _hidden   = new Set();

    root.innerHTML = `
<div style="max-width:960px;margin:0 auto">
  <h3 style="margin:0 0 20px;font-size:16px;font-weight:700">
    <i class="fa-solid fa-user-gear" style="margin-right:6px;color:var(--primary)"></i>
    Menu do Usuário
  </h3>

  <!-- Autocomplete search -->
  <div style="position:relative;max-width:500px;margin-bottom:20px">
    <div style="display:flex;gap:8px">
      <div style="flex:1;position:relative">
        <input type="text" class="fc" id="ump-email"
               placeholder="Digite o e-mail ou username do usuário"
               autocomplete="off"
               style="width:100%;font-size:13px"
               oninput="umpAutocomplete()"
               onkeydown="umpAcKeydown(event)">
        <div id="ump-ac-dropdown" style="display:none;position:absolute;left:0;right:0;top:100%;
             z-index:100;margin-top:2px;max-height:320px;overflow-y:auto;
             background:var(--bg-card);border:1px solid var(--border);border-radius:8px;
             box-shadow:0 8px 24px rgba(0,0,0,0.25)"></div>
      </div>
    </div>
  </div>

  <!-- Results area -->
  <div id="ump-result"></div>
</div>`;

    // Close dropdown when clicking outside.
    document.addEventListener('click', function _umpClickOutside(e) {
        const dd = document.getElementById('ump-ac-dropdown');
        const input = document.getElementById('ump-email');
        if (!dd) { document.removeEventListener('click', _umpClickOutside); return; }
        if (!dd.contains(e.target) && e.target !== input) {
            dd.style.display = 'none';
        }
    });
}

// ── Autocomplete ─────────────────────────────────────────────────────────────

window.umpAutocomplete = function() {
    if (_acTimer) clearTimeout(_acTimer);
    const q = (document.getElementById('ump-email')?.value || '').trim();
    const dd = document.getElementById('ump-ac-dropdown');
    if (!dd) return;

    if (q.length < 2) {
        dd.style.display = 'none';
        return;
    }

    _acTimer = setTimeout(async () => {
        const r = await cpApi('GET',
            `/api/control/v1/users?q=${encodeURIComponent(q)}&limit=20&page=1`);
        if (r?.metadata?.status !== 200) {
            dd.style.display = 'none';
            return;
        }

        const users = r.data?.users || [];
        if (users.length === 0) {
            dd.innerHTML = `<div style="padding:10px 14px;font-size:12px;color:var(--text-muted)">
                Nenhum usuário encontrado</div>`;
            dd.style.display = 'block';
            return;
        }

        dd.innerHTML = users.map((u, i) => {
            const roleBadge = u.role === 'admin'
                ? '<span style="background:#e53935;color:#fff;padding:0 4px;border-radius:3px;font-size:10px;margin-left:4px">ADM</span>'
                : u.role === 'official_specialist'
                    ? '<span style="background:#1e88e5;color:#fff;padding:0 4px;border-radius:3px;font-size:10px;margin-left:4px">SPEC</span>'
                    : '';
            return `
<div class="ump-ac-item" data-idx="${i}"
     style="padding:8px 14px;cursor:pointer;border-bottom:1px solid var(--border);
            transition:background .1s"
     onmouseenter="this.style.background='var(--bg-surface)'"
     onmouseleave="this.style.background='none'"
     onclick="umpAcSelect('${esc(u.email)}')">
  <div style="font-size:13px;font-weight:500">${esc(u.username)}${roleBadge}</div>
  <div style="font-size:11px;color:var(--text-muted)">${esc(u.email)}</div>
</div>`;
        }).join('');

        dd.style.display = 'block';
    }, 250);
};

// Keyboard navigation in the dropdown.
window.umpAcKeydown = function(e) {
    const dd = document.getElementById('ump-ac-dropdown');
    if (!dd || dd.style.display === 'none') {
        if (e.key === 'Enter') umpSearch();
        return;
    }

    const items = dd.querySelectorAll('.ump-ac-item');
    if (items.length === 0) return;

    let active = dd.querySelector('.ump-ac-item[data-active="1"]');
    let idx = active ? parseInt(active.dataset.idx) : -1;

    if (e.key === 'ArrowDown') {
        e.preventDefault();
        idx = Math.min(idx + 1, items.length - 1);
    } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        idx = Math.max(idx - 1, 0);
    } else if (e.key === 'Enter' && active) {
        e.preventDefault();
        const email = active.querySelector('div:last-child')?.textContent || '';
        umpAcSelect(email);
        return;
    } else if (e.key === 'Escape') {
        dd.style.display = 'none';
        return;
    } else {
        return;
    }

    items.forEach(el => {
        el.dataset.active = '0';
        el.style.background = 'none';
    });
    if (items[idx]) {
        items[idx].dataset.active = '1';
        items[idx].style.background = 'var(--bg-surface)';
        items[idx].scrollIntoView({ block: 'nearest' });
    }
};

window.umpAcSelect = function(email) {
    const input = document.getElementById('ump-email');
    const dd = document.getElementById('ump-ac-dropdown');
    if (input) input.value = email;
    if (dd) dd.style.display = 'none';
    umpSearch();
};

// ── Search user ──────────────────────────────────────────────────────────────

window.umpSearch = async function() {
    const email = (document.getElementById('ump-email')?.value || '').trim();
    if (!email) return;

    const result = document.getElementById('ump-result');
    if (!result) return;
    result.innerHTML = '<div style="color:var(--text-muted);font-size:13px">Buscando...</div>';

    const r = await cpApi('GET', `/api/control/v1/menu/user-prefs?email=${encodeURIComponent(email)}`);
    if (r?.metadata?.status !== 200) {
        result.innerHTML = `<div style="color:var(--danger);font-size:13px">
            ${esc(r?.metadata?.error || 'Usuário não encontrado.')}
        </div>`;
        return;
    }

    _userId   = r.data.user_id;
    _userInfo = r.data;
    _menuTree = r.data.tree || [];
    _hidden   = new Set(r.data.hidden || []);

    _renderUserResult(result);
};

// ── Render user info + tree ──────────────────────────────────────────────────

function _renderUserResult(container) {
    const d = _userInfo;

    const roleBadge = d.role === 'admin'
        ? '<span style="background:#e53935;color:#fff;padding:1px 6px;border-radius:4px;font-size:11px">ADMIN</span>'
        : d.role === 'official_specialist'
            ? '<span style="background:#1e88e5;color:#fff;padding:1px 6px;border-radius:4px;font-size:11px">SPECIALIST</span>'
            : '<span style="background:#616161;color:#fff;padding:1px 6px;border-radius:4px;font-size:11px">USER</span>';

    container.innerHTML = `
<!-- User info card -->
<div style="display:flex;align-items:center;gap:12px;padding:12px 16px;
     background:var(--bg-surface);border:1px solid var(--border);border-radius:8px;
     margin-bottom:16px;max-width:500px">
  <i class="fa-solid fa-circle-user" style="font-size:28px;color:var(--text-muted)"></i>
  <div style="flex:1;min-width:0">
    <div style="font-size:14px;font-weight:600">${esc(d.username)} ${roleBadge}</div>
    <div style="font-size:12px;color:var(--text-muted)">${esc(d.email)}</div>
    <div style="font-size:11px;color:var(--text-muted)">Profile: ${esc(d.menu_profile || 'default')}</div>
  </div>
</div>

<!-- Search + Reset -->
<div style="display:flex;align-items:center;gap:8px;margin-bottom:12px">
  <input type="text" class="fc" id="ump-filter" placeholder="Filtrar itens..."
         oninput="umpFilterTree()" style="flex:1;max-width:300px;font-size:13px">
  <button class="cp-btn cp-btn-ghost" onclick="umpResetPrefs()" style="font-size:12px;white-space:nowrap"
          title="Restaurar tudo para os padrões do admin">
    <i class="fa-solid fa-rotate-left"></i> Reset menu
  </button>
  <button class="cp-btn cp-btn-ghost" onclick="umpResetPanelPrefs()" style="font-size:12px;white-space:nowrap;color:var(--text-muted)"
          title="Apagar larguras de coluna salvas do painel IDE">
    <i class="fa-solid fa-arrows-left-right"></i> Reset larguras
  </button>
</div>

<div style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
  Desmarque itens para ocultar do menu da IDE deste usuário. Mudanças são salvas automaticamente.
</div>

<!-- Tree -->
<div id="ump-tree" style="border:1px solid var(--border);border-radius:8px;
     background:var(--bg-surface);padding:8px 0;max-height:55vh;overflow-y:auto">
  ${_renderTree(_menuTree, 0)}
</div>

<div id="ump-save-status" style="margin-top:8px;font-size:12px;color:var(--text-muted);min-height:18px"></div>
`;
}

// ── Render checkbox tree ─────────────────────────────────────────────────────

function _renderTree(nodes, depth) {
    if (!nodes || nodes.length === 0) return '';

    return nodes.map(node => {
        const indent = depth * 20;
        const isHidden = _hidden.has(node.slot_id);
        const hasChildren = node.children && node.children.length > 0;
        const label = node.label || node.label_fallback || node.slot_id;

        if (node.slot_id === 'SysExit' || node.slot_id === 'SysMyItems') return '';

        const icon = node.slot_type === 'section'
            ? `<i class="fa-solid fa-layer-group" style="color:${esc(node.color_brand || 'var(--primary)')};width:14px;text-align:center;font-size:11px"></i>`
            : node.slot_type === 'device'
                ? '<i class="fa-solid fa-microchip" style="color:#6c8eff;width:14px;text-align:center;font-size:11px"></i>'
                : node.slot_type === 'template'
                    ? '<i class="fa-solid fa-box-open" style="color:#3a9a5c;width:14px;text-align:center;font-size:11px"></i>'
                    : '<span style="width:14px"></span>';

        const childrenHtml = hasChildren ? _renderTree(node.children, depth + 1) : '';

        return `
<div class="ump-tree-item" data-slot="${esc(node.slot_id)}" style="opacity:${isHidden ? '0.5' : '1'}">
  <label style="display:flex;align-items:center;gap:6px;padding:5px 12px 5px ${12 + indent}px;
         cursor:pointer;transition:background .1s;font-size:13px"
         onmouseenter="this.style.background='rgba(255,255,255,0.04)'"
         onmouseleave="this.style.background='none'">
    <input type="checkbox" class="ump-check"
           data-slot="${esc(node.slot_id)}"
           ${isHidden ? '' : 'checked'}
           onchange="umpToggle('${esc(node.slot_id)}', this.checked)"
           style="width:15px;height:15px;accent-color:var(--primary);flex-shrink:0">
    ${icon}
    <span style="flex:1;font-weight:${depth === 0 ? '600' : '400'}">${esc(label)}</span>
  </label>
  ${childrenHtml}
</div>`;
    }).join('');
}

// ── Toggle + auto-save ───────────────────────────────────────────────────────

window.umpToggle = function(slotId, checked) {
    if (checked) {
        _hidden.delete(slotId);
    } else {
        _hidden.add(slotId);
    }

    const item = document.querySelector(`.ump-tree-item[data-slot="${slotId}"]`);
    if (item) item.style.opacity = checked ? '1' : '0.5';

    _scheduleSave();
};

function _scheduleSave() {
    if (_saveTimer) clearTimeout(_saveTimer);
    const status = document.getElementById('ump-save-status');
    if (status) { status.textContent = 'Salvando...'; status.style.color = 'var(--text-muted)'; }

    _saveTimer = setTimeout(async () => {
        const r = await cpApi('PUT', '/api/control/v1/menu/user-prefs', {
            user_id: _userId,
            hidden: Array.from(_hidden),
        });
        if (status) {
            if (r?.metadata?.status === 200) {
                status.textContent = 'Salvo. O usuário verá as mudanças ao recarregar a IDE.';
                status.style.color = 'var(--success, #16a34a)';
            } else {
                status.textContent = r?.metadata?.error || 'Erro ao salvar';
                status.style.color = 'var(--danger)';
            }
        }
    }, 600);
}

// ── Filter tree ──────────────────────────────────────────────────────────────

window.umpFilterTree = function() {
    const q = (document.getElementById('ump-filter')?.value || '').toLowerCase();
    if (!q) {
        document.querySelectorAll('.ump-tree-item').forEach(el => { el.style.display = ''; });
        return;
    }

    function filterNode(el) {
        const label = el.querySelector(':scope > label span')?.textContent?.toLowerCase() || '';
        const slot  = el.dataset.slot || '';
        const selfMatch = label.includes(q) || slot.toLowerCase().includes(q);

        let childMatch = false;
        el.querySelectorAll(':scope > .ump-tree-item').forEach(child => {
            if (filterNode(child)) childMatch = true;
        });

        const visible = selfMatch || childMatch;
        el.style.display = visible ? '' : 'none';
        return visible;
    }

    document.querySelectorAll('#ump-tree > .ump-tree-item').forEach(el => filterNode(el));
};

// ── Reset ────────────────────────────────────────────────────────────────────

window.umpResetPrefs = async function() {
    if (!_userId) return;

    const r = await cpApi('DELETE', `/api/control/v1/menu/user-prefs?user_id=${encodeURIComponent(_userId)}`);
    if (r?.metadata?.status === 200) {
        _hidden.clear();
        const treeEl = document.getElementById('ump-tree');
        if (treeEl) treeEl.innerHTML = _renderTree(_menuTree, 0);
        cpToast('success', 'Preferências do usuário resetadas.');
    } else {
        cpToast('error', r?.metadata?.error || 'Erro ao resetar');
    }
};

window.umpResetPanelPrefs = async function() {
    if (!_userId) return;

    if (!await cpConfirm(
        '<i class="fa-solid fa-arrows-left-right"></i> Resetar larguras do painel',
        'Apagar todas as larguras de coluna salvas para este usuário? Na próxima abertura do IDE, as larguras voltarão ao padrão.',
        'Apagar', 'Cancelar'
    )) return;

    const r = await cpApi('DELETE', `/api/control/v1/menu/user-panel-prefs?user_id=${encodeURIComponent(_userId)}`);
    if (r?.metadata?.status === 200) {
        const deleted = r.data?.deleted || 0;
        cpToast('success', `${deleted} configuração(ões) de largura apagada(s).`);
    } else {
        cpToast('error', r?.metadata?.error || 'Erro ao apagar');
    }
};

// ── Helpers ──────────────────────────────────────────────────────────────────

function esc(s) { return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/"/g,'&quot;'); }
