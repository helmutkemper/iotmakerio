// server/public/control/js/pages/visibilityRules.js — Visibility rules admin.
//
// Manages menu_visibility_rules (layer 2 of menu visibility).
// When a menu item has rules, only users who match at least one rule can
// see it. Items with NO rules are visible to everyone.
//
// UI features:
//   - Multi-select checkboxes for menu items (with filter + select all/none)
//   - Multi-select checkboxes for countries
//   - Group dropdown
//   - OTP confirmation for create/delete
//   - Rules table with filter and delete

import { cpApi }              from '../api.js';
import { cpToast, cpConfirm } from '../toast.js';

// ── State ────────────────────────────────────────────────────────────────────

let _rules      = [];
let _catalog     = [];
let _groups      = [];
let _filterSlot  = '';
let _otpCallback = null;

const COUNTRIES = [
    { code: 'BR', label: 'Brasil' },
    { code: 'US', label: 'EUA' },
    { code: 'GB', label: 'Reino Unido' },
    { code: 'DE', label: 'Alemanha' },
    { code: 'FR', label: 'França' },
    { code: 'ES', label: 'Espanha' },
    { code: 'PT', label: 'Portugal' },
    { code: 'IT', label: 'Itália' },
    { code: 'JP', label: 'Japão' },
    { code: 'CN', label: 'China' },
    { code: 'IN', label: 'Índia' },
    { code: 'AR', label: 'Argentina' },
    { code: 'MX', label: 'México' },
    { code: 'CL', label: 'Chile' },
    { code: 'CO', label: 'Colômbia' },
    { code: 'KR', label: 'Coreia do Sul' },
    { code: 'AU', label: 'Austrália' },
    { code: 'CA', label: 'Canadá' },
];

// ── Main render ──────────────────────────────────────────────────────────────

export async function renderVisibilityRules(root) {
    root.innerHTML = `
<div style="max-width:1000px;margin:0 auto">
  <h3 style="margin:0 0 20px;font-size:16px;font-weight:700">
    <i class="fa-solid fa-shield-halved" style="margin-right:6px;color:var(--primary)"></i>
    Regras de Visibilidade
  </h3>
  <div style="font-size:13px;color:var(--text-muted);margin-bottom:20px;line-height:1.5">
    Itens com regras são visíveis <b>apenas</b> para usuários que correspondem a pelo menos uma regra.
    Itens sem regras ficam visíveis para todos.
  </div>
  <div id="vr-loading" style="color:var(--text-muted);font-size:13px">Carregando...</div>
  <div id="vr-content" style="display:none"></div>
</div>`;

    await _loadData();
}

async function _loadData() {
    const r = await cpApi('GET', '/api/control/v1/menu/visibility-rules');
    if (r?.metadata?.status !== 200) {
        document.getElementById('vr-loading').innerHTML =
            '<span style="color:var(--danger)">Erro ao carregar regras.</span>';
        return;
    }

    _rules   = r.data.rules   || [];
    _catalog = r.data.catalog || [];
    _groups  = r.data.groups  || [];

    document.getElementById('vr-loading').style.display = 'none';
    const content = document.getElementById('vr-content');
    content.style.display = 'block';
    _renderContent(content);
}

function _renderContent(container) {
    const groupOptions = _groups.map(g =>
        `<option value="${esc(g.id)}">${esc(g.name)}</option>`
    ).join('');

    const slotsWithRules = [...new Set(_rules.map(r => r.slot_id))];
    const filterOptions = slotsWithRules.map(s => {
        const cat = _catalog.find(c => c.slot_id === s);
        return `<option value="${esc(s)}"${_filterSlot === s ? ' selected' : ''}>${esc(cat?.label_fallback || s)}</option>`;
    }).join('');

    container.innerHTML = `
<!-- ══ Create form ══ -->
<div style="padding:16px;background:var(--bg-surface);border:1px solid var(--border);border-radius:8px;margin-bottom:24px">
  <div style="font-size:14px;font-weight:600;margin-bottom:14px">
    <i class="fa-solid fa-plus" style="margin-right:4px;color:var(--primary)"></i> Nova Regra
  </div>

  <!-- Mode -->
  <div style="margin-bottom:14px">
    <label style="display:block;font-size:12px;font-weight:600;color:var(--text-muted);margin-bottom:6px">Modo</label>
    <div style="display:flex;gap:12px">
      <label style="display:flex;align-items:center;gap:5px;font-size:13px;cursor:pointer">
        <input type="radio" name="vr-mode" value="allow" checked
               style="accent-color:var(--primary)">
        <i class="fa-solid fa-check-circle" style="color:#43a047"></i>
        Permitir apenas
      </label>
      <label style="display:flex;align-items:center;gap:5px;font-size:13px;cursor:pointer">
        <input type="radio" name="vr-mode" value="deny"
               style="accent-color:var(--danger,#e53935)">
        <i class="fa-solid fa-ban" style="color:#e53935"></i>
        Bloquear
      </label>
    </div>
    <div style="font-size:11px;color:var(--text-muted);margin-top:4px">
      <b>Permitir apenas</b>: o item só aparece para quem corresponde à regra.
      <b>Bloquear</b>: o item é oculto para quem corresponde à regra.
    </div>
  </div>

  <!-- Items -->
  <div style="margin-bottom:16px">
    <label style="display:block;font-size:12px;font-weight:600;color:var(--text-muted);margin-bottom:6px">Itens do Menu *</label>
    <div style="display:flex;gap:6px;margin-bottom:6px">
      <input type="text" class="fc" id="vr-item-search" placeholder="Filtrar itens..." oninput="vrFilterItems()" style="font-size:12px;max-width:240px">
      <button class="cp-btn cp-btn-ghost" onclick="vrSelectAllItems(true)" style="font-size:11px;padding:2px 8px">Todos</button>
      <button class="cp-btn cp-btn-ghost" onclick="vrSelectAllItems(false)" style="font-size:11px;padding:2px 8px">Nenhum</button>
    </div>
    <div id="vr-items-grid" style="max-height:200px;overflow-y:auto;border:1px solid var(--border);border-radius:6px;padding:6px 8px;
         display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:2px">
      ${_catalog.filter(c => c.slot_id !== 'SysExit').map(c => {
        const label = c.label_fallback || c.slot_id;
        const typeIcon = c.slot_type === 'section' ? 'fa-layer-group' : c.slot_type === 'device' ? 'fa-microchip' : c.slot_type === 'template' ? 'fa-box-open' : 'fa-cube';
        return `<label class="vr-item-opt" style="display:flex;align-items:center;gap:5px;font-size:12px;padding:3px 4px;border-radius:4px;cursor:pointer;transition:background .1s"
               onmouseenter="this.style.background='rgba(255,255,255,0.04)'" onmouseleave="this.style.background='none'"
               data-label="${esc(label.toLowerCase())}">
          <input type="checkbox" class="vr-item-cb" value="${esc(c.slot_id)}" style="width:14px;height:14px;accent-color:var(--primary);flex-shrink:0">
          <i class="fa-solid ${typeIcon}" style="width:12px;text-align:center;font-size:10px;color:var(--text-muted)"></i>
          <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(label)}</span>
        </label>`;
    }).join('')}
    </div>
  </div>

  <!-- Countries -->
  <div style="margin-bottom:16px">
    <label style="display:block;font-size:12px;font-weight:600;color:var(--text-muted);margin-bottom:6px">Países</label>
    <div style="display:flex;gap:4px;margin-bottom:6px">
      <button class="cp-btn cp-btn-ghost" onclick="vrSelectAllCountries(true)" style="font-size:11px;padding:2px 8px">Todos</button>
      <button class="cp-btn cp-btn-ghost" onclick="vrSelectAllCountries(false)" style="font-size:11px;padding:2px 8px">Nenhum</button>
    </div>
    <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:2px;border:1px solid var(--border);border-radius:6px;padding:6px 8px">
      ${COUNTRIES.map(c => `
      <label style="display:flex;align-items:center;gap:5px;font-size:12px;padding:2px 4px;cursor:pointer">
        <input type="checkbox" class="vr-country-cb" value="${c.code}" style="width:14px;height:14px;accent-color:var(--primary);flex-shrink:0">
        <span>${c.code}</span>
        <span style="color:var(--text-muted);font-size:11px">${esc(c.label)}</span>
      </label>`).join('')}
      <div style="display:flex;align-items:center;gap:4px;padding:2px 4px">
        <span style="font-size:11px;color:var(--text-muted)">Outro:</span>
        <input type="text" class="fc" id="vr-country-other" placeholder="XX" maxlength="2"
               style="width:40px;font-size:12px;padding:2px 4px;text-transform:uppercase">
      </div>
    </div>
  </div>

  <!-- Group + Period -->
  <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px;max-width:600px;margin-bottom:16px">
    <div>
      <label style="display:block;font-size:12px;font-weight:600;color:var(--text-muted);margin-bottom:4px">Grupo</label>
      <select class="fc" id="vr-group" style="font-size:13px">
        <option value="">Nenhum</option>
        ${groupOptions}
      </select>
    </div>
    <div>
      <label style="display:block;font-size:12px;font-weight:600;color:var(--text-muted);margin-bottom:4px">Período</label>
      <div style="display:flex;gap:6px;align-items:center">
        <input type="date" class="fc" id="vr-from" style="font-size:12px;flex:1">
        <span style="color:var(--text-muted);font-size:11px">até</span>
        <input type="date" class="fc" id="vr-until" style="font-size:12px;flex:1">
      </div>
    </div>
  </div>

  <button class="cp-btn cp-btn-primary" onclick="vrCreate()" style="padding:8px 20px;font-size:13px">
    <i class="fa-solid fa-plus"></i> Criar Regra(s)
  </button>
</div>

<!-- ══ Filter + Rules table ══ -->
<div style="display:flex;align-items:center;gap:8px;margin-bottom:12px">
  <label style="font-size:12px;color:var(--text-muted);font-weight:600">Filtrar por item:</label>
  <select class="fc" id="vr-filter" onchange="vrFilter(this.value)" style="font-size:13px;max-width:300px">
    <option value="">Todos</option>
    ${filterOptions}
  </select>
  <span style="margin-left:auto;font-size:12px;color:var(--text-muted)">
    ${_rules.length} regra(s) · ${slotsWithRules.length} item(ns) restrito(s)
  </span>
</div>
<div id="vr-table">${_renderTable()}</div>`;
}

function _renderTable() {
    const filtered = _filterSlot ? _rules.filter(r => r.slot_id === _filterSlot) : _rules;

    if (filtered.length === 0) {
        return `<div style="padding:24px;text-align:center;color:var(--text-muted);font-size:13px;
                     border:1px solid var(--border);border-radius:8px;background:var(--bg-surface)">
            ${_filterSlot ? 'Nenhuma regra para este item.' : 'Nenhuma regra criada. Todos os itens estão visíveis para todos.'}
        </div>`;
    }

    return `<div style="border:1px solid var(--border);border-radius:8px;background:var(--bg-surface);overflow:hidden">
    ${filtered.map(r => {
        const cat = _catalog.find(c => c.slot_id === r.slot_id);
        const slotLabel = cat ? (cat.label_fallback || r.slot_id) : r.slot_id;
        const badges = [];
        const modeBadge = r.mode === 'deny'
            ? '<span style="background:#e5393522;color:#e53935;padding:1px 6px;border-radius:3px;font-size:11px"><i class="fa-solid fa-ban" style="margin-right:2px"></i>Bloquear</span>'
            : '<span style="background:#43a04722;color:#43a047;padding:1px 6px;border-radius:3px;font-size:11px"><i class="fa-solid fa-check-circle" style="margin-right:2px"></i>Permitir</span>';
        badges.push(modeBadge);
        if (r.group_name) badges.push(`<span style="background:#1e88e522;color:#1e88e5;padding:1px 6px;border-radius:3px;font-size:11px"><i class="fa-solid fa-users" style="margin-right:2px"></i>${esc(r.group_name)}</span>`);
        if (r.country_code) badges.push(`<span style="background:#43a04722;color:#43a047;padding:1px 6px;border-radius:3px;font-size:11px"><i class="fa-solid fa-globe" style="margin-right:2px"></i>${esc(r.country_code)}</span>`);
        if (r.valid_from || r.valid_until) badges.push(`<span style="background:#ff980022;color:#ff9800;padding:1px 6px;border-radius:3px;font-size:11px"><i class="fa-solid fa-calendar" style="margin-right:2px"></i>${esc(r.valid_from||'∞')} → ${esc(r.valid_until||'∞')}</span>`);
        return `
<div style="display:flex;align-items:center;gap:8px;padding:10px 14px;border-bottom:1px solid var(--border)">
  <div style="flex:1;min-width:0">
    <div style="font-size:13px;font-weight:600">${esc(slotLabel)}</div>
    <div style="display:flex;flex-wrap:wrap;gap:4px;margin-top:4px">${badges.join('')}</div>
  </div>
  <button class="cp-btn cp-btn-ghost" onclick="vrDelete('${esc(r.id)}')" style="padding:4px 8px;font-size:12px;color:var(--danger,#e53935)" title="Remover">
    <i class="fa-solid fa-trash"></i>
  </button>
</div>`;
    }).join('')}
  </div>`;
}

// ── Create (batch + OTP) ─────────────────────────────────────────────────────

window.vrCreate = function() {
    const mode = document.querySelector('input[name="vr-mode"]:checked')?.value || 'allow';

    const selectedItems = [];
    document.querySelectorAll('.vr-item-cb:checked').forEach(cb => selectedItems.push(cb.value));
    if (selectedItems.length === 0) { cpToast('error', 'Selecione pelo menos um item do menu.'); return; }

    const selectedCountries = [];
    document.querySelectorAll('.vr-country-cb:checked').forEach(cb => selectedCountries.push(cb.value));
    const otherCountry = (document.getElementById('vr-country-other')?.value || '').trim().toUpperCase();
    if (otherCountry && otherCountry.length === 2 && !selectedCountries.includes(otherCountry)) selectedCountries.push(otherCountry);

    const groupId = document.getElementById('vr-group')?.value || '';
    const from = document.getElementById('vr-from')?.value || '';
    const until = document.getElementById('vr-until')?.value || '';

    if (selectedCountries.length === 0 && !groupId && !from && !until) {
        cpToast('error', 'Selecione pelo menos um filtro (país, grupo ou período).'); return;
    }

    const rulesToCreate = [];
    for (const slotId of selectedItems) {
        if (selectedCountries.length > 0) {
            for (const country of selectedCountries) {
                rulesToCreate.push({ slot_id: slotId, mode, group_id: groupId, country_code: country, valid_from: from, valid_until: until });
            }
        } else {
            rulesToCreate.push({ slot_id: slotId, mode, group_id: groupId, country_code: '', valid_from: from, valid_until: until });
        }
    }

    const total = rulesToCreate.length;
    _showOTP(async () => {
        let created = 0;
        for (const rule of rulesToCreate) {
            const r = await cpApi('POST', '/api/control/v1/menu/visibility-rules', rule);
            if (r?.metadata?.status === 201 || r?.metadata?.status === 200) created++;
        }
        return created;
    }, (created) => {
        cpToast('success', `${created} regra(s) criada(s) de ${total}.`);
        _loadData();
    });
};

// ── Delete ───────────────────────────────────────────────────────────────────

window.vrDelete = async function(id) {
    if (!await cpConfirm('<i class="fa-solid fa-trash"></i> Remover regra',
        'Tem certeza que deseja remover esta regra?', 'Remover', 'Cancelar')) return;

    const r = await cpApi('DELETE', `/api/control/v1/menu/visibility-rules/${id}`);
    if (r?.metadata?.status === 200) { cpToast('success', 'Regra removida.'); await _loadData(); }
    else cpToast('error', r?.metadata?.error || 'Erro ao remover.');
};

// ── Filter helpers ───────────────────────────────────────────────────────────

window.vrFilter = function(slotId) { _filterSlot = slotId; document.getElementById('vr-table').innerHTML = _renderTable(); };
window.vrFilterItems = function() {
    const q = (document.getElementById('vr-item-search')?.value || '').toLowerCase();
    document.querySelectorAll('.vr-item-opt').forEach(el => { el.style.display = (!q || el.dataset.label.includes(q)) ? '' : 'none'; });
};
window.vrSelectAllItems = function(checked) { document.querySelectorAll('.vr-item-opt').forEach(el => { if (el.style.display !== 'none') { const cb = el.querySelector('.vr-item-cb'); if (cb) cb.checked = checked; } }); };
window.vrSelectAllCountries = function(checked) { document.querySelectorAll('.vr-country-cb').forEach(cb => { cb.checked = checked; }); };

// ── OTP modal ────────────────────────────────────────────────────────────────

function _showOTP(actionFn, onSuccess) {
    _otpCallback = { actionFn, onSuccess };

    document.getElementById('vr-otp-modal')?.remove();
    const backdrop = document.createElement('div');
    backdrop.id = 'vr-otp-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:360px">
  <h3><i class="fa-solid fa-key" style="margin-right:6px;color:var(--primary)"></i> Código OTP</h3>
  <p style="margin-top:8px;font-size:13px;color:var(--text-dim);line-height:1.5">
    Digite o código de verificação enviado ao seu e-mail.
  </p>
  <input type="text" class="fc" id="vr-otp-code" placeholder="000000" maxlength="6"
         style="width:100%;font-size:18px;text-align:center;letter-spacing:6px;margin-top:12px"
         oninput="vrOtpInput()" onkeydown="if(event.key==='Enter')vrOtpConfirm()">
  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="vrOtpCancel()">Cancelar</button>
    <button class="cp-btn cp-btn-primary" id="vr-otp-ok" onclick="vrOtpConfirm()" disabled>Confirmar</button>
  </div>
  <div id="vr-otp-status" style="margin-top:8px;font-size:12px;min-height:18px"></div>
</div>`;

    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', (e) => { if (e.target === backdrop) vrOtpCancel(); });
    setTimeout(() => document.getElementById('vr-otp-code')?.focus(), 50);

    cpApi('POST', '/api/control/v1/menu/request-otp').then(r => {
        const s = document.getElementById('vr-otp-status');
        if (!s) return;
        if (r?.metadata?.status === 200) { s.textContent = 'Código enviado para o seu e-mail.'; s.style.color = 'var(--success,#16a34a)'; }
        else { s.textContent = r?.metadata?.error || 'Erro ao enviar código.'; s.style.color = 'var(--danger)'; }
    });
}

window.vrOtpInput = function() {
    const val = (document.getElementById('vr-otp-code')?.value || '').trim();
    const btn = document.getElementById('vr-otp-ok');
    if (btn) btn.disabled = val.length < 6;
};
window.vrOtpCancel = function() { document.getElementById('vr-otp-modal')?.remove(); _otpCallback = null; };
window.vrOtpConfirm = async function() {
    if (!_otpCallback) return;
    const code = (document.getElementById('vr-otp-code')?.value || '').trim();
    if (code.length < 6) return;

    const s = document.getElementById('vr-otp-status');
    if (s) { s.textContent = 'Processando...'; s.style.color = 'var(--text-muted)'; }

    try {
        const result = await _otpCallback.actionFn(code);
        document.getElementById('vr-otp-modal')?.remove();
        _otpCallback.onSuccess(result);
    } catch (e) {
        if (s) { s.textContent = 'Erro: ' + (e.message || 'falha'); s.style.color = 'var(--danger)'; }
    }
    _otpCallback = null;
};

function esc(s) { return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/"/g,'&quot;'); }
