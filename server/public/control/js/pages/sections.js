// server/public/control/js/pages/sections.js — Menu section management page.
//
// Every write operation (create, edit, delete on sections, items, visibility)
// requires a 6-digit OTP code sent to the admin's email. The flow:
//   1. Admin triggers a write action (e.g. clicks "Criar")
//   2. Frontend calls POST /sections/request-otp → code emailed
//   3. OTP modal appears
//   4. Admin enters the code → frontend retries the operation with otp_code
//
// The generic withOTP(actionFn) helper encapsulates this flow so each
// write operation is a simple one-liner.

import { cpApi }              from '../api.js';
import { cpToast, cpConfirm } from '../toast.js';
import { FA_FAVORITES, FA_ALL } from '../icons.js';

// ── Constants ─────────────────────────────────────────────────────────────────

const API = '/api/control/v1';


const BRAND_PRESETS = [
    { name: 'Sparkfun',  color: '#E62E2E' },
    { name: 'Adafruit',  color: '#1D1D1D' },
    { name: 'Arduino',   color: '#00979D' },
    { name: 'Seeed',     color: '#87C540' },
    { name: 'DFRobot',   color: '#0068B7' },
    { name: 'Waveshare', color: '#1565C0' },
    { name: 'IoTMaker',  color: '#185FA5' },
    { name: 'Roxo',      color: '#7B1FA2' },
];

// ── State ─────────────────────────────────────────────────────────────────────

let _sections   = [];
let _editingId  = null;
let _editTab    = 'config';
let _pickerData = null;

// ══════════════════════════════════════════════════════════════════════════════
//  OTP FLOW — generic wrapper for all write operations
// ══════════════════════════════════════════════════════════════════════════════

// withOTP requests a one-time code from the server, shows a modal for the
// admin to type it, then calls actionFn(otpCode). If the action returns an
// HTTP 401 (invalid code), the error is shown inside the modal and the admin
// can retry without requesting a new code.
//
// actionFn must return the parsed API response { metadata, data }.
// On success (status 200 or 201) the modal closes and onSuccess() is called.
async function withOTP(actionFn, onSuccess) {
    // Step 1 — request OTP.
    const otpR = await cpApi('POST', `${API}/sections/request-otp`);
    if (otpR?.metadata?.status !== 200) {
        cpToast('danger', otpR?.metadata?.error || 'Erro ao solicitar código.');
        return;
    }

    // Step 2 — show modal.
    showOTPModal(async (code) => {
        // Step 3 — execute the write operation with the code.
        const r = await actionFn(code);
        const s = r?.metadata?.status;

        if (s === 200 || s === 201) {
            hideOTPModal();
            if (onSuccess) await onSuccess(r);
            return true; // signal success to modal
        }

        // 401 = bad code — show error inside modal, let admin retry.
        if (s === 401) return r?.metadata?.error || 'Código inválido ou expirado.';

        // Other errors — close modal, show toast.
        hideOTPModal();
        cpToast('danger', r?.metadata?.error || 'Erro na operação.');
        return true;
    });
}

// ── OTP modal ─────────────────────────────────────────────────────────────────

let _otpCallback = null;

function showOTPModal(callback) {
    _otpCallback = callback;
    removeEl('cp-otp-modal');

    const backdrop = document.createElement('div');
    backdrop.id = 'cp-otp-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:400px">
  <h3><i class="fa-solid fa-envelope-open-text" style="margin-right:8px"></i>Confirmar alteração</h3>
  <p style="margin-top:8px;font-size:13px;color:var(--text-dim);line-height:1.5">
    Um código de confirmação foi enviado para o seu e-mail.<br>Ele é válido por 15 minutos.
  </p>
  <div class="cp-field" style="margin-top:16px">
    <span>Código de confirmação</span>
    <input class="cp-input" id="cp-otp-input" type="text"
           inputmode="numeric" maxlength="6" placeholder="000000"
           style="font-size:22px;letter-spacing:8px;text-align:center"
           oninput="cpOtpInput()">
  </div>
  <div id="cp-otp-error" style="display:none;margin-top:8px;padding:8px 12px;
       border-radius:var(--r);background:var(--danger-bg);color:var(--danger);font-size:13px"></div>
  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="cpOtpCancel()">Cancelar</button>
    <button class="cp-btn cp-btn-primary" id="cp-otp-confirm" onclick="cpOtpConfirm()" disabled>
      <i class="fa-solid fa-check"></i> Confirmar
    </button>
  </div>
</div>`;

    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', e => { if (e.target === backdrop) hideOTPModal(); });
    setTimeout(() => document.getElementById('cp-otp-input')?.focus(), 80);
}

function hideOTPModal() {
    _otpCallback = null;
    removeEl('cp-otp-modal');
}

window.cpOtpInput = function() {
    const v = document.getElementById('cp-otp-input')?.value || '';
    const btn = document.getElementById('cp-otp-confirm');
    if (btn) btn.disabled = v.replace(/\D/g, '').length !== 6;
    // Hide error on new input.
    const err = document.getElementById('cp-otp-error');
    if (err) err.style.display = 'none';
};

window.cpOtpCancel = function() { hideOTPModal(); };

window.cpOtpConfirm = async function() {
    if (!_otpCallback) return;
    const input = document.getElementById('cp-otp-input');
    const btn   = document.getElementById('cp-otp-confirm');
    const errEl = document.getElementById('cp-otp-error');
    const code  = input?.value?.trim() || '';
    if (code.length !== 6) return;

    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Confirmando…'; }

    const result = await _otpCallback(code);

    if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-check"></i> Confirmar'; }

    // If result is a string, it's an error message — show inside modal.
    if (typeof result === 'string') {
        if (errEl) { errEl.textContent = result; errEl.style.display = 'block'; }
        if (input) { input.value = ''; input.focus(); }
        if (btn) btn.disabled = true;
    }
    // If true, the callback already handled success/close.
};

// ══════════════════════════════════════════════════════════════════════════════
//  ENTRY POINT
// ══════════════════════════════════════════════════════════════════════════════

export async function renderSections(root) {
    setBreadcrumb('Seções do menu');
    root.innerHTML = `
<div class="cp-page-title">
  <i class="fa-solid fa-bars-staggered"></i> Seções do menu
</div>
<div class="cp-card">
  <div class="cp-card-header">
    <span>Seções configuradas</span>
    <button class="cp-btn cp-btn-primary" onclick="cpSectionNew()">
      <i class="fa-solid fa-plus"></i> Nova seção
    </button>
  </div>
  <div class="cp-card-body" style="padding:0">
    <div id="cp-sections-body"><div class="cp-spinner"></div></div>
  </div>
</div>`;
    await loadAndRender();
}

async function loadAndRender() {
    const r = await cpApi('GET', `${API}/sections`);
    if (r?.metadata?.status !== 200) {
        cpToast('danger', r?.metadata?.error || 'Erro ao carregar seções');
        return;
    }
    _sections = r.data?.sections || [];
    renderTable();
}

// ══════════════════════════════════════════════════════════════════════════════
//  TABLE
// ══════════════════════════════════════════════════════════════════════════════

function renderTable() {
    const body = document.getElementById('cp-sections-body');
    if (!body) return;

    if (_sections.length === 0) {
        body.innerHTML = `<div class="cp-empty">
            <i class="fa-solid fa-bars-staggered"></i>
            Nenhuma seção criada. Clique em "Nova seção" para começar.
        </div>`;
        return;
    }

    const rows = _sections.map(s => {
        const badge = s.active
            ? '<span class="cp-badge cp-badge-official">Ativa</span>'
            : '<span class="cp-badge cp-badge-blocked">Inativa</span>';
        const ic = (s.items || []).length;
        return `<tr>
  <td style="width:40px;text-align:center;color:var(--text-muted)">${s.position}</td>
  <td>
    <div style="display:flex;align-items:center;gap:12px">
      ${colorDots(s)}
      <div style="width:32px;height:32px;border-radius:6px;background:${esc(s.color_normal)};
                  display:flex;align-items:center;justify-content:center;flex-shrink:0">
        <i class="fa-solid fa-${esc(s.icon_fa)}" style="color:#fff;font-size:14px"></i>
      </div>
      <div>
        <div style="font-weight:600;font-size:14px">${esc(s.name)}</div>
        <div style="font-size:12px;color:var(--text-muted)">${esc(s.slug)}
          <span style="margin-left:6px;opacity:.6">${ic} ite${ic !== 1 ? 'ns' : 'm'}</span>
        </div>
      </div>
    </div>
  </td>
  <td>${badge}</td>
  <td style="text-align:right;white-space:nowrap">
    <button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:12px"
            onclick="cpSectionEdit('${esc(s.id)}')">
      <i class="fa-solid fa-pen"></i> Editar
    </button>
    <button class="cp-btn cp-btn-danger" style="padding:4px 10px;font-size:12px"
            onclick="cpSectionDelete('${esc(s.id)}','${esc(s.name)}')">
      <i class="fa-solid fa-trash-can"></i>
    </button>
  </td>
</tr>`;
    }).join('');

    body.innerHTML = `
<div class="cp-table-wrap"><table class="cp-table">
  <thead><tr>
    <th style="width:40px">#</th><th>Seção</th>
    <th style="width:80px">Status</th><th style="width:180px;text-align:right">Ações</th>
  </tr></thead>
  <tbody>${rows}</tbody>
</table></div>`;
}

function colorDots(s) {
    if (!s.color_normal) return '';
    return `<span title="Cor da marca" style="width:12px;height:12px;border-radius:50%;
        background:${esc(s.color_normal)};display:inline-block;border:1px solid rgba(255,255,255,.15);flex-shrink:0"></span>`;
}

// ══════════════════════════════════════════════════════════════════════════════
//  MODAL — container + tabs
// ══════════════════════════════════════════════════════════════════════════════

function openEditor(section) {
    const isEdit = !!section;
    _editingId = isEdit ? section.id : null;
    _editTab = 'config';

    const s = section || {
        name: '', slug: '', position: _sections.length + 1,
        color_normal: '', color_attention: '',
        color_featured: '', icon_fa: 'gear', active: true, items: [],
    };

    removeEl('cp-section-modal');
    const backdrop = document.createElement('div');
    backdrop.id = 'cp-section-modal';
    backdrop.className = 'cp-modal-backdrop';

    const tabs = isEdit ? `
<div id="sec-tabs" style="display:flex;gap:0;border-bottom:1px solid var(--border);margin-bottom:16px">
  ${tabBtn('config','fa-sliders','Configuração')}
  ${tabBtn('items','fa-list','Itens')}
  ${tabBtn('visibility','fa-eye','Visibilidade')}
</div>` : '';

    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:660px;width:100%;max-height:90vh;overflow-y:auto">
  <h3><i class="fa-solid fa-palette" style="margin-right:8px"></i>${isEdit ? 'Editar seção' : 'Nova seção'}</h3>
  ${tabs}
  <div id="sec-tab-body"></div>
  <div style="display:flex;gap:8px;margin-top:24px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="cpSecModalClose()">Fechar</button>
    <button class="cp-btn cp-btn-primary" id="sec-submit"
            onclick="cpSecSubmit('${isEdit ? s.id : ''}')">${isEdit ? 'Salvar' : 'Criar'}</button>
  </div>
</div>`;

    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', e => { if (e.target === backdrop) removeEl('cp-section-modal'); });
    backdrop._sectionData = s;
    renderTab();
}

function tabBtn(key, icon, label) {
    const a = key === _editTab ? 'color:var(--primary);border-bottom:2px solid var(--primary)' : 'color:var(--text-muted);border-bottom:2px solid transparent';
    return `<button style="padding:8px 16px;font-size:13px;font-weight:600;background:none;border:none;cursor:pointer;${a}"
            onclick="cpSecTab('${key}')"><i class="fa-solid ${icon}" style="margin-right:4px"></i>${label}</button>`;
}

function renderTab() {
    const body = document.getElementById('sec-tab-body');
    const modal = document.getElementById('cp-section-modal');
    if (!body || !modal) return;
    const s = modal._sectionData;

    document.getElementById('sec-tabs')?.querySelectorAll('button').forEach(btn => {
        const k = btn.getAttribute('onclick')?.match(/'(\w+)'/)?.[1];
        btn.style.color = k === _editTab ? 'var(--primary)' : 'var(--text-muted)';
        btn.style.borderBottom = k === _editTab ? '2px solid var(--primary)' : '2px solid transparent';
    });

    const sub = document.getElementById('sec-submit');
    if (sub) sub.style.display = _editTab === 'config' ? '' : 'none';

    switch (_editTab) {
        case 'config':     body.innerHTML = renderConfigTab(s); initConfigTab(); break;
        case 'items':      renderItemsTab(body); break;
        case 'visibility': renderVisibilityTab(body); break;
    }
}

window.cpSecTab = t => { _editTab = t; renderTab(); };

// ══════════════════════════════════════════════════════════════════════════════
//  TAB: CONFIGURAÇÃO
// ══════════════════════════════════════════════════════════════════════════════

function renderConfigTab(s) {
    return `
<div style="display:grid;grid-template-columns:1fr 1fr 80px;gap:12px">
  <label class="cp-field"><span>Nome</span>
    <input class="cp-input" id="sec-name" value="${ea(s.name)}" placeholder="Ex: Sparkfun" oninput="cpSecAutoSlug()"></label>
  <label class="cp-field"><span>Slug</span>
    <input class="cp-input" id="sec-slug" value="${ea(s.slug)}" placeholder="sparkfun"></label>
  <label class="cp-field"><span>Posição</span>
    <input class="cp-input" id="sec-position" type="number" min="0" value="${s.position}" style="text-align:center"></label>
</div>
<label style="display:flex;align-items:center;gap:8px;margin-top:4px;font-size:13px;cursor:pointer">
  <input type="checkbox" id="sec-active" ${s.active?'checked':''}> Seção ativa (visível no menu)
</label>
<div style="margin-top:16px">
  <div style="font-size:13px;font-weight:600;margin-bottom:8px;color:var(--text-dim)"><i class="fa-solid fa-palette"></i> Cor da marca</div>
  <div style="display:flex;align-items:center;gap:12px">
    <input type="color" id="sec-brand-picker" value="${ea(s.color_normal || '#2255AA')}"
           oninput="cpSecBrandSync()" style="width:42px;height:36px;padding:0;border:none;cursor:pointer;background:none">
    <input class="cp-input" id="sec-brand-hex" value="${ea(s.color_normal || '')}"
           oninput="cpSecBrandHexSync()" placeholder="padrão"
           style="width:120px;font-family:var(--mono);font-size:12px;padding:4px 8px" maxlength="7" spellcheck="false">
    <button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:11px" onclick="cpSecBrandClear()">
      <i class="fa-solid fa-xmark"></i> Limpar cor
    </button>
    <span id="sec-brand-status" style="font-size:11px;color:var(--text-muted)">
      ${s.color_normal ? '' : '(usando cor padrão do menu)'}
    </span>
  </div>
  <div style="margin-top:8px"><span style="font-size:11px;color:var(--text-muted)">Presets:</span>
    <div style="display:flex;gap:4px;margin-top:4px;flex-wrap:wrap">${presetBtns()}</div></div>
</div>
<div style="margin-top:16px">
  <div style="font-size:13px;font-weight:600;margin-bottom:8px;color:var(--text-dim)"><i class="fa-solid fa-icons"></i> Ícone</div>
  <div style="display:flex;align-items:center;gap:12px;margin-bottom:8px">
    <div id="sec-icon-preview" style="width:48px;height:48px;border-radius:8px;background:${esc(s.color_normal || '#2A2D3E')};
         display:flex;align-items:center;justify-content:center;flex-shrink:0">
      <i class="fa-solid fa-${esc(s.icon_fa)}" style="color:#fff;font-size:22px" id="sec-icon-preview-i"></i></div>
    <input class="cp-input" id="sec-icon" value="${ea(s.icon_fa)}" placeholder="gear" oninput="cpSecIconChange()" style="flex:1">
  </div>
  <input class="cp-input" id="sec-icon-search" placeholder="Buscar ícone…" oninput="cpSecIconFilter()" style="margin-bottom:8px;font-size:12px">
  <div id="sec-icon-grid" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(40px,1fr));
       gap:4px;max-height:160px;overflow-y:auto;padding:4px;background:var(--bg-input);border-radius:var(--r);border:1px solid var(--border)">
    ${iconGrid(s.icon_fa)}</div>
</div>
<div style="margin-top:16px">
  <div style="font-size:13px;font-weight:600;margin-bottom:8px;color:var(--text-dim)"><i class="fa-solid fa-eye"></i> Preview</div>
  <div id="sec-preview" style="display:flex;gap:12px;align-items:center;padding:16px;border-radius:var(--r);
       border:1px solid var(--border);background:var(--bg-input)">${livePreview(s)}</div>
</div>`;
}

function initConfigTab() {
    requestAnimationFrame(() => {
        const h = document.getElementById('sec-brand-hex');
        const p = document.getElementById('sec-brand-picker');
        if (h && p && h.value) p.value = h.value;
        document.getElementById('sec-name')?.focus();
    });
}

function presetBtns() {
    return BRAND_PRESETS.map(p =>
        `<button class="cp-btn cp-btn-ghost" style="padding:2px 8px;font-size:11px;gap:4px"
            onclick="cpSecBrandSet('${p.color}')">
            <span style="width:10px;height:10px;border-radius:50%;background:${p.color};
                  display:inline-block;border:1px solid rgba(255,255,255,.2)"></span> ${p.name}</button>`
    ).join('');
}

function iconGrid(sel, filter) {
    const lf = (filter||'').toLowerCase();
    let icons, label;
    if (lf) {
        // Search across all 4111 registered icons.
        icons = FA_ALL.filter(i => i.includes(lf));
        const total = icons.length;
        if (icons.length > 120) icons = icons.slice(0, 120);
        label = `<div style="grid-column:1/-1;font-size:11px;color:var(--text-muted);padding:2px 4px">${total} resultado${total!==1?'s':''} ${total>120?'(mostrando 120)':''}</div>`;
    } else {
        icons = FA_FAVORITES;
        label = `<div style="grid-column:1/-1;font-size:11px;color:var(--text-muted);padding:2px 4px">Favoritos — digite para buscar nos ${FA_ALL.length.toLocaleString()} ícones</div>`;
    }
    if (!icons.length) return '<div style="padding:12px;text-align:center;color:var(--text-muted);font-size:12px;grid-column:1/-1">Nenhum ícone encontrado</div>';
    return label + icons.map(ic => {
        const bg = ic === sel ? 'background:var(--primary);color:#fff;' : '';
        return `<button style="width:100%;aspect-ratio:1;display:flex;align-items:center;justify-content:center;
            border:1px solid var(--border);border-radius:4px;cursor:pointer;background:none;color:var(--text);font-size:16px;${bg}"
            title="${ic}" onclick="cpSecIconPick('${ic}')"><i class="fa-solid fa-${ic}"></i></button>`;
    }).join('');
}

function livePreview(s) {
    const brandColor = s.color_normal || '';
    const bgColor = brandColor || '#2A2D3E';
    const label = brandColor ? 'Cor da marca' : 'Padrão do menu';
    return `<div style="display:flex;gap:16px;align-items:center;width:100%">
        <div style="text-align:center">
          <div style="width:56px;height:56px;border-radius:10px;background:${esc(bgColor)};
               display:flex;align-items:center;justify-content:center;margin:0 auto 4px">
            <i class="fa-solid fa-${esc(s.icon_fa)}" style="color:#fff;font-size:24px"></i>
          </div>
          <div style="font-size:11px;color:var(--text-muted)">${label}</div>
        </div>
        <div style="flex:1">
          <div style="font-weight:600;font-size:16px">${esc(s.name||'Nome')}</div>
          <div style="font-size:12px;color:var(--text-muted);margin-top:2px">${esc(s.slug||'slug')}</div>
          ${brandColor ? `<div style="font-size:11px;margin-top:4px"><span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:${esc(brandColor)};vertical-align:middle;margin-right:4px"></span><code style="font-size:11px;color:var(--text-dim)">${esc(brandColor)}</code></div>` : ''}
        </div>
    </div>`;
}

// ── Config tab event handlers ─────────────────────────────────────────────────

// Brand color — single picker + hex input + clear + presets.
window.cpSecBrandSync = () => {
    const p = document.getElementById('sec-brand-picker'), h = document.getElementById('sec-brand-hex');
    if (p && h) h.value = p.value;
    updateBrandStatus(); updatePreview();
};
window.cpSecBrandHexSync = () => {
    const p = document.getElementById('sec-brand-picker'), h = document.getElementById('sec-brand-hex');
    if (p && h && /^#[0-9a-fA-F]{6}$/.test(h.value)) p.value = h.value;
    updateBrandStatus(); updatePreview();
};
window.cpSecBrandSet = (color) => {
    const p = document.getElementById('sec-brand-picker'), h = document.getElementById('sec-brand-hex');
    if (p) p.value = color; if (h) h.value = color;
    updateBrandStatus(); updatePreview();
};
window.cpSecBrandClear = () => {
    const h = document.getElementById('sec-brand-hex'), p = document.getElementById('sec-brand-picker');
    if (h) h.value = ''; if (p) p.value = '#2255AA';
    updateBrandStatus(); updatePreview();
};
function updateBrandStatus() {
    const v = document.getElementById('sec-brand-hex')?.value || '';
    const st = document.getElementById('sec-brand-status');
    if (st) st.textContent = v ? '' : '(usando cor padrão do menu)';
}
window.cpSecAutoSlug = () => { const n=document.getElementById('sec-name')?.value||''; const el=document.getElementById('sec-slug'); if(el) el.value=n.toLowerCase().normalize('NFD').replace(/[\u0300-\u036f]/g,'').replace(/[^a-z0-9]+/g,'-').replace(/^-|-$/g,''); updatePreview(); };
window.cpSecIconChange = () => { const v=document.getElementById('sec-icon')?.value||'gear'; const i=document.getElementById('sec-icon-preview-i'); if(i)i.className=`fa-solid fa-${v}`; const g=document.getElementById('sec-icon-grid'); if(g)g.innerHTML=iconGrid(v,document.getElementById('sec-icon-search')?.value); updatePreview(); };
window.cpSecIconPick = name => { const i=document.getElementById('sec-icon'); if(i)i.value=name; window.cpSecIconChange(); };
window.cpSecIconFilter = () => { const g=document.getElementById('sec-icon-grid'); if(g)g.innerHTML=iconGrid(document.getElementById('sec-icon')?.value,document.getElementById('sec-icon-search')?.value); };

function updatePreview() {
    const s = readConfigForm();
    const el = document.getElementById('sec-preview'); if (el) el.innerHTML = livePreview(s);
    const ip = document.getElementById('sec-icon-preview');
    if (ip) ip.style.background = s.color_normal || '#2A2D3E';
    const modal = document.getElementById('cp-section-modal');
    if (modal?._sectionData) Object.assign(modal._sectionData, s);
}

function readConfigForm() {
    const brandColor = document.getElementById('sec-brand-hex')?.value?.trim() || '';
    return {
        name:            document.getElementById('sec-name')?.value?.trim()    || '',
        slug:            document.getElementById('sec-slug')?.value?.trim()    || '',
        position:        parseInt(document.getElementById('sec-position')?.value) || 0,
        active:          document.getElementById('sec-active')?.checked ?? true,
        color_normal:    brandColor,
        color_attention: brandColor,
        color_featured:  brandColor,
        icon_fa:         document.getElementById('sec-icon')?.value?.trim()    || 'gear',
    };
}

// ══════════════════════════════════════════════════════════════════════════════
//  TAB: ITENS
// ══════════════════════════════════════════════════════════════════════════════

async function renderItemsTab(body) {
    if (!_editingId) { body.innerHTML = '<p style="color:var(--text-muted)">Salve a seção primeiro.</p>'; return; }
    body.innerHTML = '<div class="cp-spinner"></div>';

    const r = await cpApi('GET', `${API}/sections/${_editingId}/items`);
    const items = r?.data?.items || [];

    if (!_pickerData) {
        const pr = await cpApi('GET', `${API}/item-picker`);
        _pickerData = pr?.data?.items || [];
    }

    const rows = items.length === 0
        ? '<div style="padding:16px;text-align:center;color:var(--text-muted);font-size:13px">Nenhum item nesta seção.</div>'
        : items.map(it => {
            const tb = it.item_type === 'project'
                ? '<span class="cp-badge cp-badge-admin" style="font-size:10px">project</span>'
                : '<span class="cp-badge cp-badge-official" style="font-size:10px">template</span>';
            const eye = it.visible
                ? `<span style="color:var(--success)" title="Visível"><i class="fa-solid fa-eye"></i></span>`
                : `<span style="color:var(--text-muted)" title="Oculto"><i class="fa-solid fa-eye-slash"></i></span>`;
            const name = (_pickerData||[]).find(e => e.id === it.item_ref_id)?.name || it.item_ref_id;
            return `<div style="display:flex;align-items:center;gap:8px;padding:8px 0;border-bottom:1px solid var(--border)">
                <span style="width:24px;text-align:center;color:var(--text-muted);font-size:12px">${it.position}</span>
                ${tb}
                <span style="flex:1;font-size:13px">${esc(name)}</span>
                <button style="background:none;border:none;cursor:pointer;padding:4px;font-size:13px"
                        onclick="cpSecItemToggle('${it.id}',${!it.visible})">${eye}</button>
                <button class="cp-btn cp-btn-danger" style="padding:2px 8px;font-size:11px"
                        onclick="cpSecItemDel('${it.id}')"><i class="fa-solid fa-xmark"></i></button>
            </div>`;
        }).join('');

    const opts = (_pickerData||[]).map(e =>
        `<option value="${ea(e.id)}" data-type="${e.type}">${esc(e.name)} (${e.type})</option>`).join('');

    body.innerHTML = `
<div style="margin-bottom:16px">
  <div style="font-size:13px;font-weight:600;margin-bottom:8px;color:var(--text-dim)"><i class="fa-solid fa-list"></i> Itens na seção</div>
  <div id="sec-items-list">${rows}</div>
</div>
<div style="border-top:1px solid var(--border);padding-top:16px">
  <div style="font-size:13px;font-weight:600;margin-bottom:8px;color:var(--text-dim)"><i class="fa-solid fa-plus"></i> Adicionar item</div>
  <div style="display:flex;gap:8px;align-items:flex-end">
    <label class="cp-field" style="flex:1;margin:0"><span>Project ou Template</span>
      <select class="cp-input" id="sec-item-select" style="padding:6px 8px">
        <option value="">— selecione —</option>${opts}
      </select></label>
    <button class="cp-btn cp-btn-primary" style="height:34px" onclick="cpSecItemAdd()">
      <i class="fa-solid fa-plus"></i> Adicionar</button>
  </div>
</div>`;
}

window.cpSecItemAdd = function() {
    const sel = document.getElementById('sec-item-select');
    if (!sel?.value) { cpToast('warning','Selecione um item.'); return; }
    const type = sel.options[sel.selectedIndex]?.dataset?.type || 'project';
    const refId = sel.value;

    withOTP(
        code => cpApi('POST', `${API}/sections/${_editingId}/items`, {
            item_type: type, item_ref_id: refId, position: 0, otp_code: code,
        }),
        () => { cpToast('success','Item adicionado.'); renderTab(); }
    );
};

window.cpSecItemToggle = function(itemId, visible) {
    withOTP(
        code => cpApi('PATCH', `${API}/sections/${_editingId}/items/${itemId}`, {
            visible, otp_code: code,
        }),
        () => { renderTab(); }
    );
};

window.cpSecItemDel = async function(itemId) {
    if (!await cpConfirm('<i class="fa-solid fa-xmark"></i> Remover item','Remover este item da seção?','Remover','Cancelar')) return;
    withOTP(
        code => cpApi('DELETE', `${API}/sections/${_editingId}/items/${itemId}`, { otp_code: code }),
        () => { cpToast('success','Item removido.'); renderTab(); }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  TAB: VISIBILIDADE
// ══════════════════════════════════════════════════════════════════════════════

async function renderVisibilityTab(body) {
    if (!_editingId) { body.innerHTML = '<p style="color:var(--text-muted)">Salve a seção primeiro.</p>'; return; }
    body.innerHTML = '<div class="cp-spinner"></div>';

    const [vr, gr] = await Promise.all([
        cpApi('GET', `${API}/sections/${_editingId}/visibility`),
        cpApi('GET', `${API}/groups`),
    ]);
    const rules  = vr?.data?.rules  || [];
    const groups = gr?.data?.groups  || [];
    const groupOpts = groups.map(g => `<option value="${ea(g.id)}">${esc(g.name)}</option>`).join('');

    const rulesHtml = rules.length === 0
        ? '<div style="padding:12px;text-align:center;color:var(--text-muted);font-size:13px">Sem restrições — visível para todos.</div>'
        : rules.map(rule => {
            const gName = rule.group_id ? (groups.find(g=>g.id===rule.group_id)?.name||rule.group_id) : 'Todos os grupos';
            const country = rule.country_code || 'Todos os países';
            const from  = rule.valid_from  ? new Date(rule.valid_from).toLocaleDateString()  : '—';
            const until = rule.valid_until ? new Date(rule.valid_until).toLocaleDateString() : '—';
            return `<div style="display:flex;align-items:center;gap:8px;padding:8px 0;border-bottom:1px solid var(--border);font-size:13px">
                <span style="flex:1"><strong>${esc(gName)}</strong> · ${esc(country)} · ${from} → ${until}</span>
                <button class="cp-btn cp-btn-danger" style="padding:2px 8px;font-size:11px"
                        onclick="cpSecVisDel('${rule.id}')"><i class="fa-solid fa-xmark"></i></button>
            </div>`;
        }).join('');

    body.innerHTML = `
<div style="margin-bottom:16px">
  <div style="font-size:13px;font-weight:600;margin-bottom:8px;color:var(--text-dim)"><i class="fa-solid fa-shield-halved"></i> Regras de visibilidade</div>
  <div style="font-size:12px;color:var(--text-muted);margin-bottom:8px">Sem regras = visível para todos. Adicione regras para restringir por grupo, país ou período.</div>
  <div id="sec-vis-list">${rulesHtml}</div>
</div>
<div style="border-top:1px solid var(--border);padding-top:16px">
  <div style="font-size:13px;font-weight:600;margin-bottom:8px;color:var(--text-dim)"><i class="fa-solid fa-plus"></i> Adicionar regra</div>
  <div style="display:grid;grid-template-columns:1fr 1fr;gap:12px">
    <label class="cp-field" style="margin:0"><span>Grupo (opcional)</span>
      <select class="cp-input" id="sec-vis-group" style="padding:6px 8px"><option value="">Todos</option>${groupOpts}</select></label>
    <label class="cp-field" style="margin:0"><span>País (opcional)</span>
      <input class="cp-input" id="sec-vis-country" placeholder="BR, US, …" maxlength="2" style="text-transform:uppercase"></label>
  </div>
  <div style="display:grid;grid-template-columns:1fr 1fr auto;gap:12px;margin-top:8px;align-items:flex-end">
    <label class="cp-field" style="margin:0"><span>Válido de</span>
      <input class="cp-input" id="sec-vis-from" type="date"></label>
    <label class="cp-field" style="margin:0"><span>Válido até</span>
      <input class="cp-input" id="sec-vis-until" type="date"></label>
    <button class="cp-btn cp-btn-primary" style="height:34px" onclick="cpSecVisAdd()">
      <i class="fa-solid fa-plus"></i> Adicionar</button>
  </div>
</div>`;
}

window.cpSecVisAdd = function() {
    const payload = {};
    const g = document.getElementById('sec-vis-group')?.value;   if (g) payload.group_id = g;
    const c = document.getElementById('sec-vis-country')?.value?.toUpperCase(); if (c) payload.country_code = c;
    const f = document.getElementById('sec-vis-from')?.value;    if (f) payload.valid_from  = new Date(f).toISOString();
    const u = document.getElementById('sec-vis-until')?.value;   if (u) payload.valid_until = new Date(u).toISOString();

    withOTP(
        code => cpApi('POST', `${API}/sections/${_editingId}/visibility`, { ...payload, otp_code: code }),
        () => { cpToast('success','Regra adicionada.'); renderTab(); }
    );
};

window.cpSecVisDel = async function(ruleId) {
    if (!await cpConfirm('<i class="fa-solid fa-xmark"></i> Remover regra','Remover esta regra?','Remover','Cancelar')) return;
    withOTP(
        code => cpApi('DELETE', `${API}/sections/${_editingId}/visibility/${ruleId}`, { otp_code: code }),
        () => { cpToast('success','Regra removida.'); renderTab(); }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  CRUD ACTIONS (with OTP)
// ══════════════════════════════════════════════════════════════════════════════

window.cpSecSubmit = function(id) {
    const data = readConfigForm();
    if (!data.name || !data.slug) { cpToast('warning','Nome e slug são obrigatórios.'); return; }

    withOTP(
        code => id
            ? cpApi('PUT',  `${API}/sections/${id}`, { ...data, otp_code: code })
            : cpApi('POST', `${API}/sections`,       { ...data, otp_code: code }),
        async (r) => {
            cpToast('success', id ? 'Seção atualizada.' : 'Seção criada.');
            if (!id && r.data?.section?.id) { _editingId = r.data.section.id; }
            await loadAndRender();
            if (!id) removeEl('cp-section-modal');
        }
    );
};

window.cpSectionDelete = async function(id, name) {
    if (!await cpConfirm('<i class="fa-solid fa-trash-can"></i> Excluir seção',
        `Excluir <strong>${esc(name)}</strong>? Itens e regras serão removidos.`,'Excluir','Cancelar')) return;

    withOTP(
        code => cpApi('DELETE', `${API}/sections/${id}`, { otp_code: code }),
        async () => { cpToast('success','Seção excluída.'); await loadAndRender(); }
    );
};

window.cpSectionNew  = () => openEditor(null);
window.cpSectionEdit = async id => {
    const r = await cpApi('GET', `${API}/sections/${id}`);
    if (r?.metadata?.status !== 200) { cpToast('danger','Seção não encontrada.'); return; }
    openEditor(r.data?.section);
};
window.cpSecModalClose = () => removeEl('cp-section-modal');

// ── Helpers ───────────────────────────────────────────────────────────────────

function removeEl(id) { document.getElementById(id)?.remove(); }
function setBreadcrumb(l) { const el = document.getElementById('cp-breadcrumb'); if (el) el.innerHTML = `<strong>${l}</strong>`; }
function esc(s) { return (s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
function ea(s)  { return (s||'').replace(/"/g,'&quot;').replace(/</g,'&lt;'); }
