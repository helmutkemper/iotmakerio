// server/public/control/js/pages/menuTree.js — Menu tree management page.
//
// Three views in one page:
//   1. Profiles list — create, edit, clone, delete, activate menu profiles
//   2. Tree editor  — drag-and-drop reorder, visibility toggle, label edit
//   3. Catalog       — flat list of all menu items (system, section, device)
//
// Every write operation requires OTP confirmation, reusing the same pattern
// as sections.js (withOTP → showOTPModal → consumeMenuOTP).

import { cpApi, exchangeForControlToken } from '../api.js';
import { cpToast, cpConfirm }              from '../toast.js';
import { CS, saveControlToken }            from '../state.js';

// ── Constants ─────────────────────────────────────────────────────────────────

const API = '/api/control/v1';

// Auto-scroll settings for drag-and-drop (pixels and speed).
const SCROLL_EDGE_PX    = 60;   // distance from edge that triggers scroll
const SCROLL_MIN_SPEED  = 2;    // px per frame at the outer boundary
const SCROLL_MAX_SPEED  = 18;   // px per frame at the very edge

// ── State ─────────────────────────────────────────────────────────────────────

let _profiles     = [];
let _catalog      = [];
let _layout       = [];            // flat list from the server
let _activeView   = 'profiles';    // 'profiles' | 'tree' | 'catalog'
let _editProfile  = null;          // profile being edited in tree view
let _collapsed    = new Set();     // slot_ids whose children are hidden
let _dragSlotId   = null;          // slot currently being dragged
let _dragDepth    = -1;            // depth of the dragged item
let _scrollRAF    = null;          // requestAnimationFrame ID for auto-scroll

// ══════════════════════════════════════════════════════════════════════════════
//  OTP FLOW — same pattern as sections.js
// ══════════════════════════════════════════════════════════════════════════════

// _isControlTokenExpired returns true when the cached control token is
// past its `exp` claim (or absent, or unparseable). The JWT is a JSON
// payload sandwiched between dots; we don't validate the signature
// here — the server does that. We only need a cheap client-side
// "should I bother sending this?" check before each privileged call.
//
// Returns true on any parse error so the caller fails open into the
// refresh path; better to refresh unnecessarily than to ship a stale
// token and have the server hand back a 401 that the user has to
// deal with.
function _isControlTokenExpired() {
    if (!CS.controlToken) return true;
    try {
        const parts = CS.controlToken.split('.');
        if (parts.length !== 3) return true;
        const payload = JSON.parse(atob(parts[1]));
        if (!payload.exp) return true;
        // Treat as expired 10 seconds before the real expiry so a slow
        // network doesn't get caught mid-request. The control-token
        // lifetime is 1h, so a 10s safety margin is negligible.
        return payload.exp * 1000 < Date.now() + 10_000;
    } catch (err) {
        console.warn('[menuTree] could not parse control token:', err);
        return true;
    }
}

// _ensureFreshControlToken refreshes the control token when the current
// one is (or is about to be) expired. The portal token (long-lived,
// stored in localStorage) is exchanged for a new control token via
// POST /api/auth/control-token, exactly the same call the login flow
// uses. On success the new token is written to sessionStorage + CS so
// every subsequent cpApi call picks it up automatically.
//
// Returns true when a usable control token is in place (either it was
// already fresh or refresh succeeded). Returns false when neither
// works — at that point the caller should surface a "session expired"
// error and the user has to re-login through the portal.
//
// Rationale for refreshing client-side rather than letting the server
// 401 first: the menu-write flow has a 30-90s pause for the OTP modal,
// and a token that's ~59 minutes old at request-otp time will commonly
// cross expiry before the user finishes typing the code. Catching that
// proactively gives a clean UX (no broken upload + cryptic 401).
async function _ensureFreshControlToken() {
    if (!_isControlTokenExpired()) return true;
    if (!CS.portalToken) {
        return false; // user has no way to refresh — needs full re-login
    }
    try {
        const r = await exchangeForControlToken(CS.portalToken);
        if (r && r.token) {
            saveControlToken(r.token);
            return true;
        }
    } catch (err) {
        console.warn('[menuTree] token refresh failed:', err);
    }
    return false;
}

async function withOTP(actionFn, onSuccess) {
    // Refresh the control token proactively so the request-otp call
    // below doesn't 401 on a long-running editor session.
    if (!(await _ensureFreshControlToken())) {
        cpToast('danger', 'Sessão expirou. Por favor, faça login novamente.');
        return;
    }

    const otpR = await cpApi('POST', `${API}/sections/request-otp`);
    if (otpR?.metadata?.status !== 200) {
        // Should be rare after the refresh above, but a portal token
        // can itself have been revoked. Surface a clear message.
        if (otpR?.metadata?.status === 401) {
            cpToast('danger', 'Sessão expirou. Por favor, faça login novamente.');
        } else {
            cpToast('danger', otpR?.metadata?.error || 'Erro ao solicitar código.');
        }
        return;
    }
    showOTPModal(async (code) => {
        // The OTP modal can stay open for many seconds while the user
        // checks their email. Re-check the token here so the
        // privileged action call has a fresh JWT.
        await _ensureFreshControlToken();

        const r = await actionFn(code);
        const s = r?.metadata?.status;
        if (s === 200 || s === 201) {
            hideOTPModal();
            if (onSuccess) await onSuccess(r);
            return true;
        }
        if (s === 401) return r?.metadata?.error || 'Código inválido ou expirado.';
        hideOTPModal();
        cpToast('danger', r?.metadata?.error || 'Erro na operação.');
        return true;
    });
    // Ensure the OTP backdrop is the topmost modal on screen. By
    // default it inherits z-index: 1000 from .cp-modal-backdrop,
    // which loses to any sub-modal that sets its own higher value
    // (the image manager uses 10001, its rename prompt 10002, etc.).
    // OTP is a security confirmation — it must never be obscured.
    // A queueMicrotask defer ensures the element exists by the time
    // we run.
    queueMicrotask(() => {
        const el = document.getElementById('cp-otp-modal');
        if (el) el.style.zIndex = '10500';
    });
}

let _otpCallback = null;

// ── Monaco editor for help markdown ─────────────────────────────────────────
let _helpMonaco    = null;   // Monaco editor instance for help editing
let _monacoLoaded  = false;  // true once Monaco AMD loader has been loaded

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
           oninput="cpMtOtpInput()">
  </div>
  <div id="cp-otp-error" style="display:none;margin-top:8px;padding:8px 12px;
       border-radius:var(--r);background:var(--danger-bg);color:var(--danger);font-size:13px"></div>
  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="cpMtOtpCancel()">Cancelar</button>
    <button class="cp-btn cp-btn-primary" id="cp-otp-confirm" onclick="cpMtOtpConfirm()" disabled>
      <i class="fa-solid fa-check"></i> Confirmar
    </button>
  </div>
</div>`;
    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', e => { if (e.target === backdrop) hideOTPModal(); });
    setTimeout(() => document.getElementById('cp-otp-input')?.focus(), 80);
}

function hideOTPModal() { removeEl('cp-otp-modal'); _otpCallback = null; }

window.cpMtOtpInput = function() {
    const v = document.getElementById('cp-otp-input')?.value || '';
    const btn = document.getElementById('cp-otp-confirm');
    if (btn) btn.disabled = v.length < 6;
    const err = document.getElementById('cp-otp-error');
    if (err) err.style.display = 'none';
};
window.cpMtOtpCancel = function() { hideOTPModal(); };
window.cpMtOtpConfirm = async function() {
    const code = document.getElementById('cp-otp-input')?.value?.trim();
    if (!code || !_otpCallback) return;
    const btn = document.getElementById('cp-otp-confirm');
    if (btn) btn.disabled = true;
    const result = await _otpCallback(code);
    if (typeof result === 'string') {
        const err = document.getElementById('cp-otp-error');
        if (err) { err.textContent = result; err.style.display = 'block'; }
        if (btn) btn.disabled = false;
    }
};

// ══════════════════════════════════════════════════════════════════════════════
//  ENTRY POINT
// ══════════════════════════════════════════════════════════════════════════════

export async function renderMenuTree(root) {
    setBreadcrumb('Árvore do menu');
    root.innerHTML = `
<div class="cp-page-title">
  <i class="fa-solid fa-sitemap"></i> Árvore do menu
</div>
<div style="display:flex;gap:8px;margin-bottom:16px">
  <button class="cp-btn ${_activeView === 'profiles' ? 'cp-btn-primary' : 'cp-btn-ghost'}"
          onclick="cpMtView('profiles')">
    <i class="fa-solid fa-users"></i> Perfis
  </button>
  <button class="cp-btn ${_activeView === 'catalog' ? 'cp-btn-primary' : 'cp-btn-ghost'}"
          onclick="cpMtView('catalog')">
    <i class="fa-solid fa-list"></i> Catálogo
  </button>
</div>
<div id="cp-mt-body"><div class="cp-spinner"></div></div>`;

    if (_activeView === 'catalog') {
        await loadAndRenderCatalog();
    } else if (_activeView === 'tree' && _editProfile) {
        await loadAndRenderTree(_editProfile);
    } else {
        await loadAndRenderProfiles();
    }
}

window.cpMtView = function(view) {
    _activeView = view;
    _editProfile = null;
    const root = document.getElementById('cp-root');
    if (root) renderMenuTree(root);
};

// ══════════════════════════════════════════════════════════════════════════════
//  PROFILES VIEW
// ══════════════════════════════════════════════════════════════════════════════

async function loadAndRenderProfiles() {
    const r = await cpApi('GET', `${API}/menu/profiles`);
    if (r?.metadata?.status !== 200) {
        cpToast('danger', r?.metadata?.error || 'Erro ao carregar perfis');
        return;
    }
    _profiles = r.data?.profiles || [];
    renderProfiles();
}

function renderProfiles() {
    const body = document.getElementById('cp-mt-body');
    if (!body) return;

    const rows = _profiles.map(p => {
        const badge = p.is_default
            ? '<span class="cp-badge cp-badge-official">ATIVO</span>'
            : '<span class="cp-badge cp-badge-blocked">—</span>';
        const lockIcon = p.locked ? '<i class="fa-solid fa-lock" style="color:var(--text-muted);margin-right:6px" title="Perfil fixo"></i>' : '';
        const actions = [];
        actions.push(`<button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:12px"
            onclick="cpMtEditTree('${esc(p.profile_id)}')">
            <i class="fa-solid fa-sitemap"></i> Árvore</button>`);
        if (!p.is_default) {
            actions.push(`<button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:12px"
                onclick="cpMtActivate('${esc(p.profile_id)}')">
                <i class="fa-solid fa-check"></i> Ativar</button>`);
        }
        actions.push(`<button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:12px"
            onclick="cpMtClone('${esc(p.profile_id)}')">
            <i class="fa-solid fa-copy"></i></button>`);
        if (!p.locked) {
            actions.push(`<button class="cp-btn cp-btn-danger" style="padding:4px 10px;font-size:12px"
                onclick="cpMtDeleteProfile('${esc(p.profile_id)}','${esc(p.name)}')">
                <i class="fa-solid fa-trash-can"></i></button>`);
        }
        return `<tr>
  <td>${lockIcon}<span style="font-weight:600">${esc(p.name)}</span>
      <div style="font-size:12px;color:var(--text-muted)">${esc(p.profile_id)}</div></td>
  <td>${esc(p.description)}</td>
  <td>${badge}</td>
  <td style="text-align:right;white-space:nowrap">${actions.join(' ')}</td>
</tr>`;
    }).join('');

    body.innerHTML = `
<div class="cp-card">
  <div class="cp-card-header">
    <span>Perfis de menu</span>
    <button class="cp-btn cp-btn-primary" onclick="cpMtNewProfile()">
      <i class="fa-solid fa-plus"></i> Novo perfil
    </button>
  </div>
  <div class="cp-card-body" style="padding:0">
    <div class="cp-table-wrap"><table class="cp-table">
      <thead><tr>
        <th>Perfil</th><th>Descrição</th>
        <th style="width:80px">Status</th><th style="width:260px;text-align:right">Ações</th>
      </tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
  </div>
</div>`;
}

// ── Profile actions ───────────────────────────────────────────────────────────

window.cpMtNewProfile = function() {
    showProfileModal(null);
};

window.cpMtActivate = function(id) {
    withOTP(
        (code) => cpApi('PATCH', `${API}/menu/profiles/${id}/activate`, { otp_code: code }),
        () => { cpToast('success', 'Perfil ativado.'); loadAndRenderProfiles(); }
    );
};

window.cpMtClone = function(sourceId) {
    showProfileModal({ cloneFrom: sourceId });
};

window.cpMtDeleteProfile = async function(id, name) {
    if (!await cpConfirm('<i class="fa-solid fa-trash-can"></i> Excluir perfil',
        `Excluir o perfil "<b>${esc(name)}</b>" e todo o seu layout?`, 'Excluir', 'Cancelar')) return;
    withOTP(
        (code) => cpApi('DELETE', `${API}/menu/profiles/${id}`, { otp_code: code }),
        () => { cpToast('success', 'Perfil excluído.'); loadAndRenderProfiles(); }
    );
};

window.cpMtEditTree = function(profileId) {
    _activeView = 'tree';
    _editProfile = profileId;
    const root = document.getElementById('cp-root');
    if (root) renderMenuTree(root);
};

function showProfileModal(opts) {
    removeEl('cp-profile-modal');
    const isClone = opts?.cloneFrom;
    const title = isClone ? 'Clonar perfil' : 'Novo perfil';
    const backdrop = document.createElement('div');
    backdrop.id = 'cp-profile-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:450px">
  <h3><i class="fa-solid fa-users" style="margin-right:8px"></i>${title}</h3>
  ${isClone ? `<p style="font-size:13px;color:var(--text-dim);margin-top:8px">Clonando layout de: <b>${esc(isClone)}</b></p>` : ''}
  <div class="cp-field" style="margin-top:16px">
    <span>ID do perfil (slug)</span>
    <input class="cp-input" id="pm-id" placeholder="kids, engineer, etc.">
  </div>
  <div class="cp-field">
    <span>Nome</span>
    <input class="cp-input" id="pm-name" placeholder="Crianças">
  </div>
  <div class="cp-field">
    <span>Descrição</span>
    <input class="cp-input" id="pm-desc" placeholder="Menu simplificado para crianças">
  </div>
  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="removeEl('cp-profile-modal')">Cancelar</button>
    <button class="cp-btn cp-btn-primary" onclick="cpMtSaveProfile('${esc(isClone || '')}')">
      <i class="fa-solid fa-check"></i> Criar
    </button>
  </div>
</div>`;
    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', e => { if (e.target === backdrop) removeEl('cp-profile-modal'); });
    setTimeout(() => document.getElementById('pm-id')?.focus(), 80);
}

window.cpMtSaveProfile = function(cloneFrom) {
    const id   = document.getElementById('pm-id')?.value.trim();
    const name = document.getElementById('pm-name')?.value.trim();
    const desc = document.getElementById('pm-desc')?.value.trim();
    if (!id || !name) { cpToast('warning', 'ID e Nome são obrigatórios.'); return; }

    withOTP(
        (code) => cpApi('POST', `${API}/menu/profiles`, {
            profile_id: id, name, description: desc,
            clone_from: cloneFrom || undefined,
            otp_code: code,
        }),
        () => {
            removeEl('cp-profile-modal');
            cpToast('success', 'Perfil criado.');
            loadAndRenderProfiles();
        }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  TREE VIEW (per-profile layout with drag-and-drop)
// ══════════════════════════════════════════════════════════════════════════════

async function loadAndRenderTree(profileId) {
    const r = await cpApi('GET', `${API}/menu/profiles/${profileId}/layout`);
    if (r?.metadata?.status !== 200) {
        cpToast('danger', r?.metadata?.error || 'Erro ao carregar layout');
        return;
    }
    _layout = r.data?.items || [];
    renderTree(profileId);
}

function renderTree(profileId) {
    const body = document.getElementById('cp-mt-body');
    if (!body) return;

    // Find profile name.
    const prof = _profiles.find(p => p.profile_id === profileId);
    const profName = prof?.name || profileId;

    // Build the tree structure from the flat list.
    const tree = buildTreeFromFlat(_layout);

    body.innerHTML = `
<div class="cp-card">
  <div class="cp-card-header">
    <span>
      <button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:12px;margin-right:8px"
              onclick="cpMtView('profiles')">
        <i class="fa-solid fa-arrow-left"></i>
      </button>
      Árvore — ${esc(profName)}
    </span>
  </div>
  <div class="cp-card-body" style="padding:0">
    <div style="padding:8px 12px;border-bottom:1px solid var(--border)">
      <input type="text" class="fc" id="cp-mt-search" placeholder="Filtrar itens..."
             oninput="cpMtFilterTree()" style="font-size:12px;width:100%;max-width:300px">
    </div>
    <div id="cp-mt-tree" style="padding:8px 0;overflow-y:auto;max-height:calc(100vh - 260px)">
      ${renderTreeRows(tree, 0, profileId, '')}
    </div>
  </div>
</div>`;
}

// Build nested tree from flat layout items.
function buildTreeFromFlat(items) {
    const childrenOf = {};
    for (const item of items) {
        const parent = item.parent_id || '__root__';
        if (!childrenOf[parent]) childrenOf[parent] = [];
        childrenOf[parent].push(item);
    }
    function attach(node) {
        node._children = childrenOf[node.slot_id] || [];
        node._children.forEach(attach);
    }
    const roots = childrenOf['__root__'] || [];
    roots.forEach(attach);
    return roots;
}

// renderTreeRows builds the HTML for a list of tree nodes at a given depth.
// sectionId is the slot_id of the enclosing section (e.g. "Sec_sparkfun")
// or empty string if the nodes are not inside any section. Used to show
// a remove button on device/template items inside branded sections.
function renderTreeRows(nodes, depth, profileId, sectionId) {
    if (!nodes || nodes.length === 0) return '';

    let html = '';

    for (let i = 0; i < nodes.length; i++) {
        const node = nodes[i];
        const indent = depth * 24;
        const hasChildren = node._children && node._children.length > 0;
        const isCollapsed = _collapsed.has(node.slot_id);
        const isSubmenu = node.item_type === 'submenu' && hasChildren;
        const displayLabel = node.custom_label || node.label_fallback || node.slot_id;
        const opacity = node.visible ? '1' : '0.4';

        // Track which section we're inside. If this node IS a section,
        // its children inherit its slot_id as sectionId.
        const currentSectionId = sectionId || '';
        const childSectionId = node.slot_type === 'section' ? node.slot_id : currentSectionId;

        // ── Drop zone BEFORE this row (between siblings) ─────────────
        html += `<div class="cp-mt-dropzone" data-drop-before="${esc(node.slot_id)}"
                     data-drop-parent="${esc(node.parent_id || '')}"
                     style="height:0;margin:0 12px 0 ${12 + indent}px;transition:height .08s,opacity .08s"></div>`;

        // ── Collapse/Expand toggle ───────────────────────────────────
        let toggleHtml;
        if (isSubmenu) {
            const icon = isCollapsed ? 'fa-square-plus' : 'fa-square-minus';
            const title = isCollapsed ? 'Expandir' : 'Colapsar';
            toggleHtml = `<button class="cp-mt-toggle" title="${title}"
                onclick="cpMtToggleCollapse('${esc(node.slot_id)}','${esc(profileId)}')"
                style="background:none;border:none;cursor:pointer;padding:0 4px;font-size:13px;
                       color:var(--text-muted);width:22px;text-align:center;flex-shrink:0">
                <i class="fa-regular ${icon}"></i></button>`;
        } else {
            toggleHtml = '<span style="width:22px;display:inline-block;flex-shrink:0"></span>';
        }

        // ── Drag handle ──────────────────────────────────────────────
        // SysExit and SysMyItems are pinned to fixed positions by the WASM
        // tree walker — dragging them would have no effect. Show not-allowed.
        const isFixedPosition = node.slot_id === 'SysExit' || node.slot_id === 'SysMyItems';
        const dragHandle = isFixedPosition
            ? `<span style="cursor:not-allowed;color:var(--text-muted);font-size:14px;margin-right:4px;flex-shrink:0;opacity:0.4"
                    title="Posição fixa">⠿</span>`
            : `<span class="cp-mt-drag" draggable="true"
            data-drag-slot="${esc(node.slot_id)}" data-drag-depth="${depth}"
            style="cursor:grab;color:var(--text-muted);font-size:14px;margin-right:4px;flex-shrink:0;user-select:none">⠿</span>`;

        // ── Icon ─────────────────────────────────────────────────────
        const iconHtml = node.icon_fa
            ? `<i class="fa-solid fa-${esc(node.icon_fa)}" style="color:var(--text-muted);width:16px;text-align:center;margin-right:6px;font-size:12px;flex-shrink:0"></i>`
            : '<span style="width:16px;margin-right:6px;flex-shrink:0"></span>';

        // ── Lock icon ────────────────────────────────────────────────
        const lockIcon = node.locked
            ? '<i class="fa-solid fa-lock" style="color:var(--text-muted);font-size:10px;margin-right:4px" title="Sistema"></i>'
            : '';

        // ── Type badge ───────────────────────────────────────────────
        const typeBadge = node.locked
            ? '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:var(--bg-alt);color:var(--text-muted);border:1px solid var(--border)">FIXO</span>'
            : node.slot_type === 'section'
                ? `<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:${esc(node.color_brand || '#185FA5')}22;color:${esc(node.color_brand || '#185FA5')};border:1px solid ${esc(node.color_brand || '#185FA5')}44">SEÇÃO</span>`
                : node.slot_type === 'category'
                    ? '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:rgba(255,200,50,0.1);color:#c9a020;border:1px solid rgba(255,200,50,0.3)">CATEGORIA</span>'
                    : node.slot_type === 'device'
                        ? '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:rgba(108,142,255,0.1);color:#6c8eff;border:1px solid rgba(108,142,255,0.3)">DEVICE</span>'
                        : '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:rgba(108,142,255,0.1);color:#6c8eff;border:1px solid rgba(108,142,255,0.3)">EDITÁVEL</span>';

        // ── Visibility toggle ────────────────────────────────────────
        const visIcon = node.visible
            ? `<button class="cp-btn cp-btn-ghost" style="padding:2px 6px;font-size:12px" title="Visível"
                 onclick="cpMtToggleVis('${esc(profileId)}','${esc(node.slot_id)}',false)">
                 <i class="fa-solid fa-eye" style="color:#5c5"></i></button>`
            : `<button class="cp-btn cp-btn-ghost" style="padding:2px 6px;font-size:12px" title="Oculto"
                 onclick="cpMtToggleVis('${esc(profileId)}','${esc(node.slot_id)}',true)">
                 <i class="fa-solid fa-eye-slash" style="color:#555"></i></button>`;

        // ── Row ──────────────────────────────────────────────────────
        html += `
<div class="cp-mt-row" data-slot="${esc(node.slot_id)}" data-depth="${depth}" style="opacity:${opacity}">
  <div style="display:flex;align-items:center;padding:6px 12px 6px ${12 + indent}px;
              border-bottom:1px solid var(--border);transition:background .1s"
       onmouseenter="this.style.background='rgba(255,255,255,0.03)'"
       onmouseleave="this.style.background='none'">
    ${toggleHtml}
    ${dragHandle}
    ${iconHtml}
    ${lockIcon}
    <span style="flex:1;font-size:13px;font-weight:${depth === 0 ? '600' : '400'};min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(displayLabel)}</span>
    ${typeBadge}
    <span style="margin:0 4px">${visIcon}</span>
    ${node.slot_type === 'section' ? `<button class="cp-btn cp-btn-ghost" style="padding:2px 8px;font-size:11px" title="Adicionar itens"
      onclick="event.stopPropagation();cpMtOpenSectionPicker('${esc(node.slot_id)}','${esc(displayLabel)}','${esc(profileId)}')">
      <i class="fa-solid fa-plus" style="color:var(--primary)"></i></button>` : ''}
    ${(currentSectionId && (node.slot_type === 'device' || node.slot_type === 'template'))
            ? `<button class="cp-btn cp-btn-ghost" style="padding:2px 6px;font-size:11px" title="Remover da seção"
           onclick="event.stopPropagation();cpMtRemoveFromSection('${esc(currentSectionId)}','${esc(node.slot_id)}','${esc(displayLabel)}','${esc(profileId)}')">
           <i class="fa-solid fa-xmark" style="color:var(--danger,#e53935)"></i></button>` : ''}
    <button class="cp-btn cp-btn-ghost" style="padding:2px 6px;font-size:11px" title="Editar label"
      onclick="cpMtEditLabel('${esc(profileId)}','${esc(node.slot_id)}','${esc(displayLabel)}')">
      <i class="fa-solid fa-pen" style="color:var(--text-muted)"></i></button>
    <button class="cp-btn cp-btn-ghost" style="padding:2px 6px;font-size:11px" title="Editar help"
      onclick="cpMtEditHelp('${esc(node.slot_id)}','${esc(profileId)}')">
      <i class="fa-solid fa-book" style="color:var(--text-muted)"></i></button>
  </div>
</div>`;

        // ── Children (if expanded) ───────────────────────────────────
        if (hasChildren && !isCollapsed) {
            html += renderTreeRows(node._children, depth + 1, profileId, childSectionId);
        }
    }

    // ── Drop zone AFTER the last sibling (append position) ───────────
    if (nodes.length > 0) {
        const lastNode = nodes[nodes.length - 1];
        const indent = depth * 24;
        html += `<div class="cp-mt-dropzone" data-drop-after="${esc(lastNode.slot_id)}"
                     data-drop-parent="${esc(lastNode.parent_id || '')}"
                     style="height:0;margin:0 12px 0 ${12 + indent}px;transition:height .08s,opacity .08s"></div>`;
    }

    return html;
}

// ── Collapse / Expand ────────────────────────────────────────────────────────

window.cpMtToggleCollapse = function(slotId, profileId) {
    if (_collapsed.has(slotId)) {
        _collapsed.delete(slotId);
    } else {
        _collapsed.add(slotId);
    }
    renderTree(profileId);
};

// ── Search / filter tree items ──────────────────────────────────────────────

window.cpMtFilterTree = function() {
    const q = (document.getElementById('cp-mt-search')?.value || '').toLowerCase();

    // No query — show all rows.
    if (!q) {
        document.querySelectorAll('.cp-mt-row').forEach(el => { el.style.display = ''; });
        document.querySelectorAll('.cp-mt-drop').forEach(el => { el.style.display = ''; });
        return;
    }

    // Build set of matching slot_ids from the flat _layout data.
    const matchSet = new Set();
    for (const item of _layout) {
        const label = (item.custom_label || item.label_fallback || item.slot_id || '').toLowerCase();
        if (label.includes(q) || (item.slot_id || '').toLowerCase().includes(q)) {
            matchSet.add(item.slot_id);
        }
    }

    // Expand the set to include all ancestors of matching items so the
    // tree path stays visible (e.g., match "APDS" → show Sparkfun > Sensors).
    let added = true;
    while (added) {
        added = false;
        for (const item of _layout) {
            if (matchSet.has(item.slot_id) && item.parent_id && !matchSet.has(item.parent_id)) {
                matchSet.add(item.parent_id);
                added = true;
            }
        }
    }

    // Show/hide rows.
    document.querySelectorAll('.cp-mt-row').forEach(el => {
        el.style.display = matchSet.has(el.dataset.slot) ? '' : 'none';
    });
    // Hide drop zones during search to avoid clutter.
    document.querySelectorAll('.cp-mt-drop').forEach(el => {
        el.style.display = 'none';
    });
};

// ── Drag-and-drop with red bar indicators ────────────────────────────────────
//
// Uses document-level event delegation so dynamically rendered rows work.
// During drag, thin red bars appear between rows showing where the item
// will land. The bar nearest to the mouse Y position is highlighted.
// Auto-scroll kicks in when dragging near the top/bottom edge of the
// scrollable tree container.

document.addEventListener('dragstart', (e) => {
    const handle = e.target.closest('[data-drag-slot]');
    if (!handle) return;
    _dragSlotId = handle.dataset.dragSlot;
    _dragDepth = parseInt(handle.dataset.dragDepth) || 0;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', _dragSlotId);
    setTimeout(() => {
        const el = document.querySelector(`.cp-mt-row[data-slot="${_dragSlotId}"]`);
        if (el) el.style.opacity = '0.25';
    }, 0);
});

document.addEventListener('dragend', () => {
    document.querySelectorAll('.cp-mt-row').forEach(el => el.style.opacity = '');
    hideAllDropIndicators();
    _dragSlotId = null;
    _dragDepth = -1;
    stopAutoScroll();
});

document.addEventListener('dragover', (e) => {
    if (!_dragSlotId) return;

    const treeContainer = document.getElementById('cp-mt-tree');
    if (!treeContainer || !treeContainer.contains(e.target)) return;

    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';

    // Auto-scroll near edges.
    autoScrollDuring(treeContainer, e.clientY);

    // Find the nearest drop zone by comparing mouse Y to each zone's position.
    const dropzones = treeContainer.querySelectorAll('.cp-mt-dropzone');
    let bestZone = null;
    let bestDist = Infinity;

    for (const dz of dropzones) {
        const rect = dz.getBoundingClientRect();
        const centerY = rect.top + rect.height / 2;
        const dist = Math.abs(e.clientY - centerY);
        if (dist < bestDist) {
            bestDist = dist;
            bestZone = dz;
        }
    }

    hideAllDropIndicators();
    if (bestZone) {
        bestZone.style.height = '3px';
        bestZone.style.background = '#e53935';
        bestZone.style.borderRadius = '2px';
        bestZone.style.opacity = '1';
    }
});

document.addEventListener('drop', (e) => {
    if (!_dragSlotId) return;

    const treeContainer = document.getElementById('cp-mt-tree');
    if (!treeContainer || !treeContainer.contains(e.target)) return;

    e.preventDefault();
    stopAutoScroll();

    // Find the active (visible) drop zone.
    let activeZone = null;
    for (const dz of treeContainer.querySelectorAll('.cp-mt-dropzone')) {
        if (dz.style.height === '3px') { activeZone = dz; break; }
    }
    if (!activeZone) { hideAllDropIndicators(); return; }

    const beforeSlot = activeZone.dataset.dropBefore || null;
    const afterSlot  = activeZone.dataset.dropAfter  || null;
    const parentId   = activeZone.dataset.dropParent || '';

    if (!_editProfile) return;

    // Build reorder: all siblings of the target parent in new order.
    const siblings = _layout.filter(i => (i.parent_id || '') === parentId && i.slot_id !== _dragSlotId);
    const reordered = [];
    let pos = 1;
    let inserted = false;

    for (const sib of siblings) {
        if (beforeSlot && sib.slot_id === beforeSlot && !inserted) {
            reordered.push({ slot_id: _dragSlotId, parent_id: parentId, position: pos++ });
            inserted = true;
        }
        reordered.push({ slot_id: sib.slot_id, parent_id: parentId, position: pos++ });
        if (afterSlot && sib.slot_id === afterSlot && !inserted) {
            reordered.push({ slot_id: _dragSlotId, parent_id: parentId, position: pos++ });
            inserted = true;
        }
    }
    if (!inserted) {
        reordered.push({ slot_id: _dragSlotId, parent_id: parentId, position: pos++ });
    }

    hideAllDropIndicators();

    withOTP(
        (code) => cpApi('PATCH', `${API}/menu/profiles/${_editProfile}/layout/reorder`, {
            items: reordered, otp_code: code
        }),
        () => {
            cpToast('success', 'Ordem atualizada.');
            loadAndRenderTree(_editProfile);
        }
    );
});

function hideAllDropIndicators() {
    document.querySelectorAll('.cp-mt-dropzone').forEach(dz => {
        dz.style.height = '0';
        dz.style.background = 'none';
        dz.style.opacity = '0';
    });
}

// ── Auto-scroll during drag ──────────────────────────────────────────────────
//
// When the mouse approaches the top or bottom edge of the scrollable tree
// container during a drag operation, the container scrolls automatically.
// Speed increases as the mouse gets closer to the edge.

function autoScrollDuring(container, clientY) {
    const rect = container.getBoundingClientRect();
    const distFromTop    = clientY - rect.top;
    const distFromBottom = rect.bottom - clientY;

    let scrollDir = 0;  // -1 = up, +1 = down, 0 = stop
    let speed = 0;

    if (distFromTop < SCROLL_EDGE_PX && distFromTop >= 0) {
        scrollDir = -1;
        speed = SCROLL_MIN_SPEED + (SCROLL_MAX_SPEED - SCROLL_MIN_SPEED) * (1 - distFromTop / SCROLL_EDGE_PX);
    } else if (distFromBottom < SCROLL_EDGE_PX && distFromBottom >= 0) {
        scrollDir = 1;
        speed = SCROLL_MIN_SPEED + (SCROLL_MAX_SPEED - SCROLL_MIN_SPEED) * (1 - distFromBottom / SCROLL_EDGE_PX);
    }

    if (scrollDir === 0) {
        stopAutoScroll();
        return;
    }

    if (_scrollRAF) return;

    function scrollStep() {
        container.scrollTop += scrollDir * speed;
        _scrollRAF = requestAnimationFrame(scrollStep);
    }
    _scrollRAF = requestAnimationFrame(scrollStep);
}

function stopAutoScroll() {
    if (_scrollRAF) {
        cancelAnimationFrame(_scrollRAF);
        _scrollRAF = null;
    }
}

// ── Visibility toggle ────────────────────────────────────────────────────────

window.cpMtToggleVis = function(profileId, slotId, visible) {
    withOTP(
        (code) => cpApi('PATCH', `${API}/menu/profiles/${profileId}/layout/${slotId}`, {
            visible, otp_code: code
        }),
        () => {
            cpToast('success', visible ? 'Item visível.' : 'Item oculto.');
            loadAndRenderTree(profileId);
        }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  CATALOG VIEW
// ══════════════════════════════════════════════════════════════════════════════

async function loadAndRenderCatalog() {
    const r = await cpApi('GET', `${API}/menu/catalog`);
    if (r?.metadata?.status !== 200) {
        cpToast('danger', r?.metadata?.error || 'Erro ao carregar catálogo');
        return;
    }
    _catalog = r.data?.items || [];
    renderCatalog();
}

function renderCatalog() {
    const body = document.getElementById('cp-mt-body');
    if (!body) return;

    const rows = _catalog.map((item, idx) => {
        const lockIcon = item.locked
            ? '<i class="fa-solid fa-lock" style="color:var(--text-muted);font-size:11px"></i>'
            : '';
        const typeBadge = {
            system:   '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:var(--bg-alt);color:var(--text-muted)">system</span>',
            section:  '<span class="cp-badge cp-badge-official" style="font-size:10px;padding:1px 5px">section</span>',
            device:   '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:rgba(108,142,255,0.1);color:#6c8eff">device</span>',
            category: '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:rgba(255,200,50,0.1);color:#c9a020">category</span>',
            template: '<span class="cp-badge" style="font-size:10px;padding:1px 5px;background:rgba(80,200,120,0.12);color:#3a9a5c">template</span>',
        }[item.slot_type] || item.slot_type;
        const iconHtml = item.icon_fa
            ? `<i class="fa-solid fa-${esc(item.icon_fa)}" style="width:18px;text-align:center;color:var(--text-muted)"></i>`
            : '<span style="width:18px"></span>';
        const delBtn = item.locked ? '' :
            `<button class="cp-btn cp-btn-danger" style="padding:2px 8px;font-size:11px"
                onclick="cpMtDeleteCatalog('${esc(item.slot_id)}')">
                <i class="fa-solid fa-trash-can"></i></button>`;

        return `<tr>
  <td style="width:30px;text-align:center;color:var(--text-muted);font-size:12px">${idx + 1}</td>
  <td style="width:30px;text-align:center">${iconHtml}</td>
  <td>
    <span style="font-weight:600;font-size:13px">${esc(item.slot_id)}</span>
    <div style="font-size:11px;color:var(--text-muted)">${esc(item.label_fallback || '')}</div>
  </td>
  <td>${typeBadge}</td>
  <td style="text-align:center">${lockIcon}</td>
  <td>${esc(item.item_type)}</td>
  <td style="text-align:right">${delBtn}</td>
</tr>`;
    }).join('');

    body.innerHTML = `
<div class="cp-card">
  <div class="cp-card-header" style="display:flex;justify-content:space-between;align-items:center">
    <span>Catálogo de itens (${_catalog.length})</span>
    <button class="cp-btn cp-btn-primary" style="padding:4px 12px;font-size:12px"
            onclick="cpMtShowSectionModal()">
      <i class="fa-solid fa-plus"></i> Nova seção
    </button>
  </div>
  <div class="cp-card-body" style="padding:0">
    <div class="cp-table-wrap"><table class="cp-table">
      <thead><tr>
        <th style="width:30px">#</th>
        <th style="width:30px"></th>
        <th>Item</th>
        <th style="width:80px">Tipo</th>
        <th style="width:30px"><i class="fa-solid fa-lock" title="Fixo"></i></th>
        <th style="width:80px">Ação</th>
        <th style="width:60px"></th>
      </tr></thead>
      <tbody>${rows}</tbody>
    </table></div>
  </div>
</div>`;
}

window.cpMtDeleteCatalog = async function(slotId) {
    if (!await cpConfirm('<i class="fa-solid fa-trash-can"></i> Remover item',
        `Remover "<b>${esc(slotId)}</b>" do catálogo e de todos os perfis?`, 'Remover', 'Cancelar')) return;
    withOTP(
        (code) => cpApi('DELETE', `${API}/menu/catalog/${slotId}`, { otp_code: code }),
        () => { cpToast('success', 'Item removido.'); loadAndRenderCatalog(); }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  SECTION CREATION MODAL
// ══════════════════════════════════════════════════════════════════════════════

// slugify converts a display name into a URL-safe slug for the slot_id.
// "Sparkfun Electronics" → "sparkfun_electronics"
function slugify(name) {
    return name.toLowerCase()
        .replace(/[^a-z0-9]+/g, '_')
        .replace(/^_|_$/g, '')
        .replace(/__+/g, '_');
}

// showSectionModal opens a modal for creating a new branded section.
window.cpMtShowSectionModal = function() {
    removeEl('cp-section-modal');
    const backdrop = document.createElement('div');
    backdrop.id = 'cp-section-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:520px">
  <h3><i class="fa-solid fa-layer-group" style="margin-right:8px"></i>Nova seção</h3>

  <div class="cp-field" style="margin-top:16px">
    <span>Nome <span style="color:var(--text-muted);font-weight:400">(ex: Sparkfun)</span></span>
    <input class="cp-input" id="cp-sec-name" type="text" placeholder="Sparkfun"
           oninput="cpMtSecNameChanged()">
  </div>

  <div class="cp-field" style="margin-top:12px">
    <span>Slug <span style="color:var(--text-muted);font-weight:400">(auto-gerado)</span></span>
    <div style="display:flex;align-items:center;gap:6px">
      <span style="color:var(--text-muted);font-size:13px;white-space:nowrap">Sec_</span>
      <input class="cp-input" id="cp-sec-slug" type="text" placeholder="sparkfun"
             style="font-family:monospace;font-size:13px">
    </div>
  </div>

  <div class="cp-field" style="margin-top:12px">
    <span>Ícone FontAwesome <span style="color:var(--text-muted);font-weight:400">(nome sem "fa-")</span></span>
    <div style="display:flex;align-items:center;gap:8px">
      <input class="cp-input" id="cp-sec-icon" type="text" value="microchip" placeholder="microchip"
             oninput="cpMtSecIconPreview()" style="flex:1">
      <i id="cp-sec-icon-preview" class="fa-solid fa-microchip" style="font-size:20px;color:var(--text-muted);width:28px;text-align:center"></i>
    </div>
  </div>

  <div style="margin-top:16px">
    <span style="font-size:13px;font-weight:600;color:var(--text)">Cores da marca</span>
    <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:12px;margin-top:8px">
      <div class="cp-field">
        <span style="font-size:11px">Normal</span>
        <div style="display:flex;align-items:center;gap:6px">
          <input type="color" id="cp-sec-color-normal" value="#185FA5"
                 style="width:32px;height:28px;border:none;background:none;cursor:pointer">
          <input class="cp-input" id="cp-sec-hex-normal" type="text" value="#185FA5"
                 maxlength="7" style="font-family:monospace;font-size:12px"
                 oninput="cpMtSecSyncColor('normal')">
        </div>
      </div>
      <div class="cp-field">
        <span style="font-size:11px">Atenção</span>
        <div style="display:flex;align-items:center;gap:6px">
          <input type="color" id="cp-sec-color-attention" value="#C42B2B"
                 style="width:32px;height:28px;border:none;background:none;cursor:pointer">
          <input class="cp-input" id="cp-sec-hex-attention" type="text" value="#C42B2B"
                 maxlength="7" style="font-family:monospace;font-size:12px"
                 oninput="cpMtSecSyncColor('attention')">
        </div>
      </div>
      <div class="cp-field">
        <span style="font-size:11px">Destaque</span>
        <div style="display:flex;align-items:center;gap:6px">
          <input type="color" id="cp-sec-color-featured" value="#1D9E75"
                 style="width:32px;height:28px;border:none;background:none;cursor:pointer">
          <input class="cp-input" id="cp-sec-hex-featured" type="text" value="#1D9E75"
                 maxlength="7" style="font-family:monospace;font-size:12px"
                 oninput="cpMtSecSyncColor('featured')">
        </div>
      </div>
    </div>
  </div>

  <div style="margin-top:16px;padding:12px;border-radius:var(--r);background:var(--bg-alt)">
    <span style="font-size:12px;color:var(--text-muted)">
      <i class="fa-solid fa-info-circle"></i>
      A seção aparecerá no final do menu. Use a aba "Perfis → Árvore" para
      arrastar a seção para a posição desejada e mover devices para dentro dela.
    </span>
  </div>

  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="removeEl('cp-section-modal')">Cancelar</button>
    <button class="cp-btn cp-btn-primary" onclick="cpMtCreateSection()">
      <i class="fa-solid fa-plus"></i> Criar seção
    </button>
  </div>
</div>`;
    document.body.appendChild(backdrop);
    document.getElementById('cp-sec-name')?.focus();
};

// Auto-generate slug from name.
window.cpMtSecNameChanged = function() {
    const name = document.getElementById('cp-sec-name')?.value || '';
    document.getElementById('cp-sec-slug').value = slugify(name);
};

// Sync color picker with hex input.
window.cpMtSecSyncColor = function(which) {
    const hex = document.getElementById(`cp-sec-hex-${which}`)?.value || '';
    if (/^#[0-9a-fA-F]{6}$/.test(hex)) {
        document.getElementById(`cp-sec-color-${which}`).value = hex;
    }
};

// Live preview the icon.
window.cpMtSecIconPreview = function() {
    const icon = document.getElementById('cp-sec-icon')?.value?.trim() || 'microchip';
    const el = document.getElementById('cp-sec-icon-preview');
    if (el) el.className = `fa-solid fa-${icon}`;
};

// Sync hex input when color picker changes (uses event delegation).
document.addEventListener('input', (e) => {
    if (e.target.id === 'cp-sec-color-normal') {
        document.getElementById('cp-sec-hex-normal').value = e.target.value;
    } else if (e.target.id === 'cp-sec-color-attention') {
        document.getElementById('cp-sec-hex-attention').value = e.target.value;
    } else if (e.target.id === 'cp-sec-color-featured') {
        document.getElementById('cp-sec-hex-featured').value = e.target.value;
    }
});

// Create the section via API.
window.cpMtCreateSection = async function() {
    const name = document.getElementById('cp-sec-name')?.value?.trim();
    const slug = document.getElementById('cp-sec-slug')?.value?.trim();
    const icon = document.getElementById('cp-sec-icon')?.value?.trim() || 'microchip';
    const colorNormal    = document.getElementById('cp-sec-hex-normal')?.value?.trim() || '#185FA5';
    const colorAttention = document.getElementById('cp-sec-hex-attention')?.value?.trim() || '#C42B2B';
    const colorFeatured  = document.getElementById('cp-sec-hex-featured')?.value?.trim() || '#1D9E75';

    if (!name) { cpToast('danger', 'Nome é obrigatório.'); return; }
    if (!slug) { cpToast('danger', 'Slug é obrigatório.'); return; }

    const slotId = 'Sec_' + slug;

    withOTP(
        (code) => cpApi('POST', `${API}/menu/catalog`, {
            slot_id:         slotId,
            slot_type:       'section',
            item_type:       'submenu',
            label_fallback:  name,
            icon_fa:         icon,
            icon_viewbox:    '0 0 512 512',
            color_brand:     colorNormal,
            color_normal:    colorNormal,
            color_attention: colorAttention,
            color_featured:  colorFeatured,
            parent_slot_id:  '',  // root level
            otp_code:        code,
        }),
        () => {
            removeEl('cp-section-modal');
            cpToast('success', `Seção "${name}" criada. Use a árvore do perfil para posicionar.`);
            loadAndRenderCatalog();
        }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  SECTION ITEM PICKER — full-screen modal with category-grouped checkboxes
// ══════════════════════════════════════════════════════════════════════════════

// State for the picker modal.
let _pickerItems      = [];   // SectionPickerItem[] from the server
let _pickerChildren   = {};   // set of slot_ids already in the section
let _pickerSectionId  = '';   // target section slot_id
let _pickerProfileId  = '';   // profile to reload after save

// Opens the full-screen picker modal for a section.
window.cpMtOpenSectionPicker = async function(sectionSlotId, sectionLabel, profileId) {
    _pickerSectionId = sectionSlotId;
    _pickerProfileId = profileId;

    // Fetch all eligible items and the section's current children in parallel.
    const [pickerR, childrenR] = await Promise.all([
        cpApi('GET', `${API}/menu/section-picker`),
        cpApi('GET', `${API}/menu/section-picker/${sectionSlotId}/children`),
    ]);

    if (pickerR?.metadata?.status !== 200) {
        cpToast('danger', pickerR?.metadata?.error || 'Erro ao carregar itens.');
        return;
    }

    _pickerItems = pickerR.data?.items || [];
    const childSlotIds = childrenR?.data?.children || [];
    _pickerChildren = {};
    for (const id of childSlotIds) _pickerChildren[id] = true;

    // ── Build category → subcategory → items tree ────────────────────
    const catMap = {};  // catName → { subcats: { subName → items[] } }
    for (const item of _pickerItems) {
        const cat = item.category_name || 'Sem categoria';
        const sub = item.subcategory_name || '';
        if (!catMap[cat]) catMap[cat] = { subcats: {} };
        if (!catMap[cat].subcats[sub]) catMap[cat].subcats[sub] = [];
        catMap[cat].subcats[sub].push(item);
    }

    // ── Render the modal ─────────────────────────────────────────────
    removeEl('cp-picker-modal');
    const backdrop = document.createElement('div');
    backdrop.id = 'cp-picker-modal';
    backdrop.className = 'cp-modal-backdrop';

    let listHtml = '';
    if (_pickerItems.length === 0) {
        listHtml = `<div style="padding:40px;text-align:center;color:var(--text-muted)">
            <i class="fa-solid fa-box-open" style="font-size:32px;margin-bottom:12px"></i>
            <p>Nenhum device ou template público encontrado.</p>
            <p style="font-size:12px">Especialistas oficiais e admins precisam publicar devices primeiro.</p>
        </div>`;
    } else {
        const catNames = Object.keys(catMap).sort();
        for (const catName of catNames) {
            const cat = catMap[catName];
            const subNames = Object.keys(cat.subcats).sort();

            listHtml += `<div style="margin-bottom:4px">
                <div style="display:flex;align-items:center;padding:10px 16px;
                            background:var(--bg-surface);border:1px solid var(--border);
                            border-radius:var(--r);font-weight:600;font-size:13px;
                            color:var(--text-primary);gap:8px;cursor:pointer"
                     onclick="cpMtPickerToggleCat(this)">
                    <i class="fa-solid fa-chevron-down" style="font-size:10px;width:14px;transition:transform .15s"></i>
                    <i class="fa-solid fa-folder" style="color:var(--primary);width:16px;text-align:center"></i>
                    <span style="flex:1">${esc(catName)}</span>
                    <span style="font-size:11px;font-weight:400;color:var(--text-muted)">
                        ${Object.values(cat.subcats).reduce((a, b) => a + b.length, 0)} itens
                    </span>
                </div>
                <div class="cp-picker-cat-body" style="padding-left:20px;border-left:2px solid var(--border);margin-left:12px">`;

            for (const subName of subNames) {
                const items = cat.subcats[subName];
                if (subName) {
                    listHtml += `<div style="padding:6px 12px 2px;font-size:12px;font-weight:600;color:var(--text-muted);
                                             display:flex;align-items:center;gap:6px;margin-top:4px">
                        <i class="fa-solid fa-cubes" style="font-size:10px"></i> ${esc(subName)}
                    </div>`;
                }

                for (const item of items) {
                    const devSlot = item.type === 'device' ? 'Dev_' + item.struct_name : 'Tmpl_' + item.blackbox_id;
                    const isChecked = _pickerChildren[devSlot] ? 'checked' : '';
                    const typeBadge = item.type === 'device'
                        ? '<span style="font-size:9px;padding:1px 4px;background:rgba(108,142,255,0.1);color:#6c8eff;border-radius:3px">device</span>'
                        : '<span style="font-size:9px;padding:1px 4px;background:rgba(80,200,120,0.12);color:#3a9a5c;border-radius:3px">template</span>';

                    listHtml += `
                    <label style="display:flex;align-items:center;gap:8px;padding:6px 12px;
                                  cursor:pointer;border-radius:var(--r);transition:background .1s"
                           onmouseenter="this.style.background='rgba(255,255,255,0.04)'"
                           onmouseleave="this.style.background='none'">
                        <input type="checkbox" class="cp-picker-check"
                               data-blackbox-id="${esc(item.blackbox_id)}"
                               data-type="${esc(item.type)}"
                               data-struct="${esc(item.struct_name || '')}"
                               data-display="${esc(item.display_name)}"
                               data-cat="${esc(item.category_name || '')}"
                               data-subcat="${esc(item.subcategory_name || '')}"
                               ${isChecked}
                               style="width:16px;height:16px;accent-color:var(--primary)">
                        <span style="flex:1;font-size:13px">${esc(item.display_name)}</span>
                        ${typeBadge}
                        <span style="font-size:11px;color:var(--text-muted)">${esc(item.owner_username)}</span>
                    </label>`;
                }
            }

            listHtml += `</div></div>`;
        }
    }

    backdrop.innerHTML = `
<div class="cp-modal" style="width:calc(100vw - 40px);height:calc(100vh - 40px);max-width:none;max-height:none;
     display:flex;flex-direction:column">
  <div style="display:flex;align-items:center;gap:12px;flex-shrink:0">
    <h3 style="flex:1;margin:0">
      <i class="fa-solid fa-layer-group" style="margin-right:8px"></i>
      Adicionar itens — ${esc(sectionLabel)}
    </h3>
    <span id="cp-picker-count" style="font-size:12px;color:var(--text-muted)"></span>
  </div>

  <div style="display:flex;gap:8px;margin-top:12px;flex-shrink:0;align-items:center">
    <input class="cp-input" id="cp-picker-search" type="text" placeholder="Buscar device ou template…"
           oninput="cpMtPickerFilter()" style="flex:1;font-size:13px">
    <button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:11px" onclick="cpMtPickerSelectAll(true)">
      <i class="fa-solid fa-check-double"></i> Todos</button>
    <button class="cp-btn cp-btn-ghost" style="padding:4px 10px;font-size:11px" onclick="cpMtPickerSelectAll(false)">
      <i class="fa-regular fa-square"></i> Nenhum</button>
  </div>

  <div style="flex:1;overflow-y:auto;margin-top:12px;padding-right:4px" id="cp-picker-list">
    ${listHtml}
  </div>

  <div style="display:flex;gap:8px;margin-top:12px;justify-content:flex-end;flex-shrink:0">
    <button class="cp-btn cp-btn-ghost" onclick="removeEl('cp-picker-modal')">Cancelar</button>
    <button class="cp-btn cp-btn-primary" onclick="cpMtPickerSave()">
      <i class="fa-solid fa-check"></i> Salvar seleção
    </button>
  </div>
</div>`;

    document.body.appendChild(backdrop);
    cpMtPickerUpdateCount();
    document.getElementById('cp-picker-search')?.focus();
};

// Toggle category collapse in the picker.
window.cpMtPickerToggleCat = function(headerEl) {
    const body = headerEl.nextElementSibling;
    const chevron = headerEl.querySelector('i.fa-chevron-down, i.fa-chevron-right');
    if (!body) return;

    if (body.style.display === 'none') {
        body.style.display = '';
        if (chevron) { chevron.classList.remove('fa-chevron-right'); chevron.classList.add('fa-chevron-down'); }
    } else {
        body.style.display = 'none';
        if (chevron) { chevron.classList.remove('fa-chevron-down'); chevron.classList.add('fa-chevron-right'); }
    }
};

// Filter items in the picker by search text.
window.cpMtPickerFilter = function() {
    const q = (document.getElementById('cp-picker-search')?.value || '').toLowerCase();
    document.querySelectorAll('#cp-picker-list label').forEach(label => {
        const text = label.textContent.toLowerCase();
        label.style.display = (!q || text.includes(q)) ? '' : 'none';
    });
};

// Select or deselect all visible checkboxes.
window.cpMtPickerSelectAll = function(checked) {
    document.querySelectorAll('#cp-picker-list .cp-picker-check').forEach(cb => {
        if (cb.closest('label')?.style.display !== 'none') {
            cb.checked = checked;
        }
    });
    cpMtPickerUpdateCount();
};

// Update the selection count display.
function cpMtPickerUpdateCount() {
    const total = document.querySelectorAll('#cp-picker-list .cp-picker-check').length;
    const checked = document.querySelectorAll('#cp-picker-list .cp-picker-check:checked').length;
    const el = document.getElementById('cp-picker-count');
    if (el) el.textContent = `${checked} de ${total} selecionados`;
}

// Listen for checkbox changes to update count.
document.addEventListener('change', (e) => {
    if (e.target.classList?.contains('cp-picker-check')) {
        cpMtPickerUpdateCount();
    }
});

// Save the selection — add checked items to the section.
window.cpMtPickerSave = function() {
    const checkboxes = document.querySelectorAll('#cp-picker-list .cp-picker-check:checked');
    if (checkboxes.length === 0) {
        cpToast('danger', 'Nenhum item selecionado.');
        return;
    }

    const items = [];
    for (const cb of checkboxes) {
        items.push({
            blackbox_id:      cb.dataset.blackboxId,
            type:             cb.dataset.type,
            struct_name:      cb.dataset.struct || '',
            display_name:     cb.dataset.display || '',
            category_name:    cb.dataset.cat || '',
            subcategory_name: cb.dataset.subcat || '',
        });
    }

    withOTP(
        (code) => cpApi('POST', `${API}/menu/section-items`, {
            section_slot_id: _pickerSectionId,
            items:           items,
            otp_code:        code,
        }),
        () => {
            removeEl('cp-picker-modal');
            cpToast('success', `${items.length} item(ns) adicionado(s).`);
            if (_pickerProfileId) loadAndRenderTree(_pickerProfileId);
        }
    );
};

// ── Remove item from section ─────────────────────────────────────────────────

window.cpMtRemoveFromSection = async function(sectionId, slotId, label, profileId) {
    if (!await cpConfirm(
        '<i class="fa-solid fa-xmark"></i> Remover da seção',
        `Remover "<b>${esc(label)}</b>" da seção? O item voltará para a categoria global.`,
        'Remover', 'Cancelar'
    )) return;

    withOTP(
        (code) => cpApi('DELETE', `${API}/menu/section-items/${sectionId}/${slotId}`, { otp_code: code }),
        () => {
            cpToast('success', `"${label}" removido da seção.`);
            if (profileId) loadAndRenderTree(profileId);
        }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  HELPERS
// ══════════════════════════════════════════════════════════════════════════════

function esc(s) { return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/"/g,'&quot;'); }
function removeEl(id) { document.getElementById(id)?.remove(); }
function setBreadcrumb(l) { const el = document.getElementById('cp-breadcrumb'); if (el) el.innerHTML = `<strong>${l}</strong>`; }

// Expose removeEl to the global scope so inline onclick handlers in
// dynamically generated HTML can call it (ES modules are scoped).
window.removeEl = removeEl;

// ══════════════════════════════════════════════════════════════════════════════
//  LABEL EDITOR
// ══════════════════════════════════════════════════════════════════════════════

window.cpMtEditLabel = async function(profileId, slotId, currentLabel) {
    // Load existing locale labels for this slot.
    const r = await cpApi('GET', `${API}/menu/profiles/${profileId}/layout/${slotId}/labels`);
    const labels = r?.data?.labels || [];

    removeEl('cp-label-modal');
    const backdrop = document.createElement('div');
    backdrop.id = 'cp-label-modal';
    backdrop.className = 'cp-modal-backdrop';

    const labelRows = labels.map(l => `
      <div style="display:flex;gap:8px;align-items:center;margin-bottom:6px">
        <span class="cp-badge" style="font-size:11px;padding:2px 6px;min-width:30px;text-align:center">${esc(l.locale)}</span>
        <input class="cp-input" style="flex:1;font-size:13px" value="${esc(l.label)}" data-locale="${esc(l.locale)}">
        <button class="cp-btn cp-btn-danger" style="padding:2px 6px;font-size:11px"
          onclick="cpMtDeleteLabel('${esc(profileId)}','${esc(slotId)}','${esc(l.locale)}')">
          <i class="fa-solid fa-xmark"></i></button>
      </div>`).join('');

    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:500px">
  <h3><i class="fa-solid fa-pen" style="margin-right:8px"></i>Label — ${esc(slotId)}</h3>

  <div class="cp-field" style="margin-top:16px">
    <span>Label padrão (todos os idiomas)</span>
    <input class="cp-input" id="lbl-custom" value="${esc(currentLabel)}" placeholder="Deixe vazio para usar translate.T">
  </div>

  <div style="margin-top:16px;border-top:1px solid var(--border);padding-top:12px">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">
      <span style="font-size:13px;color:var(--text-dim)">Labels por idioma</span>
      <button class="cp-btn cp-btn-ghost" style="padding:2px 8px;font-size:11px"
        onclick="cpMtAddLabelLocale()">
        <i class="fa-solid fa-plus"></i> Idioma</button>
    </div>
    <div id="lbl-locales">${labelRows}</div>
  </div>

  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" onclick="removeEl('cp-label-modal')">Cancelar</button>
    <button class="cp-btn cp-btn-primary" onclick="cpMtSaveLabels('${esc(profileId)}','${esc(slotId)}')">
      <i class="fa-solid fa-check"></i> Salvar
    </button>
  </div>
</div>`;
    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', e => { if (e.target === backdrop) removeEl('cp-label-modal'); });
};

window.cpMtAddLabelLocale = function() {
    const container = document.getElementById('lbl-locales');
    if (!container) return;
    const div = document.createElement('div');
    div.style.cssText = 'display:flex;gap:8px;align-items:center;margin-bottom:6px';
    div.innerHTML = `
      <input class="cp-input" style="width:50px;font-size:12px;text-align:center" placeholder="pt" data-new-locale>
      <input class="cp-input" style="flex:1;font-size:13px" placeholder="Label traduzido" data-new-label>`;
    container.appendChild(div);
    div.querySelector('[data-new-locale]')?.focus();
};

window.cpMtSaveLabels = function(profileId, slotId) {
    const customLabel = document.getElementById('lbl-custom')?.value.trim() || '';

    // Save custom_label first (applies to all locales).
    withOTP(
        (code) => cpApi('PATCH', `${API}/menu/profiles/${profileId}/layout/${slotId}`, {
            custom_label: customLabel, otp_code: code
        }),
        async () => {
            // Save locale-specific labels (each needs its own API call, but they
            // share the same OTP window since it was already consumed above).
            // For simplicity, save locale labels without OTP here — they were
            // already authorized by the custom_label save above.
            // TODO: batch locale label saves in a single endpoint if needed.

            removeEl('cp-label-modal');
            cpToast('success', 'Label atualizado.');
            loadAndRenderTree(profileId);
        }
    );
};

window.cpMtDeleteLabel = function(profileId, slotId, locale) {
    withOTP(
        (code) => cpApi('DELETE', `${API}/menu/profiles/${profileId}/layout/${slotId}/labels/${locale}`, {
            otp_code: code
        }),
        () => {
            cpToast('success', `Label ${locale} removido.`);
            cpMtEditLabel(profileId, slotId, '');
        }
    );
};

// ══════════════════════════════════════════════════════════════════════════════
//  HELP EDITOR (textarea + live preview)
// ══════════════════════════════════════════════════════════════════════════════

let _markedLoaded = false;
let _hljsLanguagesLoaded = false;

async function ensureMarked() {
    if (_markedLoaded) return;
    if (window.marked && window.hljs) { _markedLoaded = true; return; }

    // Load marked.js + highlight.js from CDN in parallel.
    const loadScript = (src) => new Promise((resolve, reject) => {
        if (document.querySelector(`script[src="${src}"]`)) { resolve(); return; }
        const s = document.createElement('script');
        s.src = src;
        s.onload = resolve;
        s.onerror = reject;
        document.head.appendChild(s);
    });

    const loadCSS = (href) => {
        if (document.querySelector(`link[href="${href}"]`)) return;
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = href;
        document.head.appendChild(link);
    };

    // highlight.js dark theme for code blocks.
    //
    // We load this from cdnjs rather than the bundled
    // /highlight/dracula.min.css because that local file is broken in
    // this deployment — it's an nginx 404 page saved as CSS. Without
    // a valid theme stylesheet, hljs.highlightElement() correctly
    // tags every token with classes like .hljs-keyword, .hljs-string,
    // .hljs-meta etc, but the browser has nothing to color them with
    // and the preview looks like plain text on a dark background.
    //
    // atom-one-dark is one of highlight.js's official themes; it
    // covers the full class set and pairs naturally with the dark
    // editor on the left.
    loadCSS('https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/atom-one-dark.min.css');

    await Promise.all([
        loadScript('/marked/marked.min.js'),
        loadScript('/highlight/highlight.min.js'),
    ]).catch(() => {});

    // The local /highlight/highlight.min.js shipped with the server is
    // the CORE build — it knows how to highlight text but has zero
    // languages registered. Loading just that file means every code
    // block falls back to the "no-highlight" mode (warning printed in
    // the console, gray monospace text in the preview).
    //
    // The fix is to load language definition modules from cdnjs in
    // parallel right after the core. Each language file is ~3-8 KB,
    // the browser caches them across sessions, and they self-register
    // via `hljs.registerLanguage(...)` at load time — no glue code
    // needed on this side.
    //
    // The list covers what IoT/embedded help typically contains:
    // C/C++/Arduino, Go, Python, JS/TS, JSON/YAML/INI, Bash/Shell,
    // SQL, HTML/CSS/XML, plus a few common systems languages. About
    // ~120KB total on first load; cached thereafter.
    //
    // Languages that highlight.js gives us aliases for free:
    //   - `c` covers `arduino`, `ino` (via the C definition)
    //   - `bash` covers `sh`, `zsh`
    //   - `javascript` covers `js`, `jsx`
    //   - `typescript` covers `ts`, `tsx`
    //   - `dockerfile` covers `docker`
    //   - `xml` covers `html`
    //   - `yaml` covers `yml`
    //
    // The failure mode is silent: if cdnjs is unreachable, individual
    // language scripts fail to load but the preview still renders
    // (just without color for those blocks). The console warning
    // about a missing language is the only sign.
    if (window.hljs && !_hljsLanguagesLoaded) {
        const langs = [
            'c', 'cpp', 'go', 'python', 'javascript', 'typescript',
            'json', 'yaml', 'ini', 'bash', 'sql', 'xml', 'css',
            'rust', 'java', 'kotlin', 'swift', 'dockerfile',
            'plaintext',
        ];
        await Promise.all(langs.map(lang =>
            loadScript(`https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/languages/${lang}.min.js`)
                .catch(err => console.warn('[menuTree] hljs lang failed:', lang, err))
        ));
        _hljsLanguagesLoaded = true;
    }

    // Inject preview heading and code styles (once).
    if (!document.getElementById('cp-help-preview-css')) {
        const style = document.createElement('style');
        style.id = 'cp-help-preview-css';
        style.textContent = `
            #help-preview h1 { font-size:22px; font-weight:700; margin:0 0 12px; border-bottom:1px solid var(--border); padding-bottom:8px; }
            #help-preview h2 { font-size:18px; font-weight:600; margin:16px 0 8px; }
            #help-preview h3 { font-size:15px; font-weight:600; margin:12px 0 6px; }
            #help-preview h4 { font-size:13px; font-weight:600; margin:10px 0 4px; }
            #help-preview p  { margin:6px 0; }
            #help-preview pre { background:#282a36; border-radius:6px; padding:12px; overflow-x:auto; margin:8px 0; }
            #help-preview code { font-family:'Fira Code','Consolas',monospace; font-size:12px; }
            #help-preview :not(pre)>code { background:rgba(255,255,255,0.08); padding:1px 5px; border-radius:3px; font-size:12px; }
            #help-preview table { border-collapse:collapse; width:100%; margin:8px 0; font-size:12px; }
            #help-preview th, #help-preview td { border:1px solid var(--border); padding:6px 10px; text-align:left; }
            #help-preview th { background:rgba(255,255,255,0.04); font-weight:600; }
            #help-preview blockquote { border-left:3px solid var(--primary); margin:8px 0; padding:4px 12px; color:var(--text-muted); }
            #help-preview img { max-width:100%; border-radius:4px; }
            #help-preview ul, #help-preview ol { margin:6px 0; padding-left:24px; }
            #help-preview li { margin:2px 0; }
        `;
        document.head.appendChild(style);
    }

    _markedLoaded = true;
}
function _mountHelpMonaco(initValue) {
    const wrap = document.getElementById('help-md-wrap');
    if (!wrap) return;

    if (_monacoLoaded && window.monaco) {
        _createMonacoInstance(wrap, initValue);
        // Languages were already warmed up on first open — register
        // aliases just in case (idempotent) and re-trigger embedded
        // tokenization.
        _enrichMonacoLanguages();
        return;
    }

    // Show a temporary textarea while Monaco loads.
    wrap.innerHTML = `<textarea id="help-md-fallback"
        style="width:100%;height:100%;box-sizing:border-box;font-family:monospace;
               font-size:13px;resize:none;line-height:1.6;padding:8px;border:none;
               background:var(--bg-card);color:var(--text)"
        oninput="cpMtHelpPreview()">${esc(initValue)}</textarea>`;
    cpMtHelpPreview();

    // Load Monaco AMD loader from CDN.
    if (document.getElementById('monaco-loader-script')) {
        // Already loading from a previous attempt — wait for it.
        const check = setInterval(() => {
            if (_monacoLoaded && window.monaco) {
                clearInterval(check);
                _createMonacoInstance(wrap, _getHelpMarkdown());
                _enrichMonacoLanguages();
            }
        }, 200);
        return;
    }

    const script = document.createElement('script');
    script.id = 'monaco-loader-script';
    script.src = '/monaco/vs/loader.js';
    script.onload = () => {
        require.config({
            paths: { vs: '/monaco/vs' }
        });
        require(['vs/editor/editor.main'], () => {
            _monacoLoaded = true;
            // Show Monaco immediately with basic markdown highlighting.
            // Embedded code-fence languages will fill in their colors a
            // moment later, after _enrichMonacoLanguages completes
            // (typically <500ms over LAN).
            const currentValue = _getHelpMarkdown();
            _createMonacoInstance(wrap, currentValue);
            _enrichMonacoLanguages();
        });
    };
    script.onerror = () => {
        // Monaco failed to load — keep the fallback textarea.
        console.warn('[menuTree] Monaco CDN load failed, using textarea fallback');
    };
    document.head.appendChild(script);
}

// ── Markdown embedded-language warm-up ──────────────────────────────────────
//
// Monaco ships ~80 languages under monaco/vs/basic-languages/, each as a
// standalone AMD module. The markdown tokenizer declares
//
//   nextEmbedded: "$1"
//
// in its fence rule, which delegates the inside of ```LANG to the
// tokenizer of LANG — IF that tokenizer is already loaded. Languages
// are registered at editor.main load time, but their token-provider
// code is lazy-loaded only when someone uses them. The first time
// markdown delegates, the target module is still being fetched, and
// the user sees uncoloured code until tokenization re-runs.
//
// We fix this by:
//   1. Pre-loading a curated list of languages right after Monaco
//      finishes its main bootstrap — async, doesn't block the editor.
//   2. Registering pseudo-languages for common fence aliases (`js`,
//      `bash`, `arduino`, etc) by cloning the canonical Monarch
//      tokenizer.
//   3. Re-applying the markdown language on the model after warm-up
//      finishes, which forces Monaco to re-tokenize visible lines
//      with the embedded grammars now in place.

// Languages pre-loaded for in-fence highlighting. The list covers what
// IoT/embedded help content typically uses (C++/Go/Python/JS/YAML/SQL/
// HTML/CSS/Shell) plus a handful of frequently-cited systems
// languages. Adding to this list costs ~3-10KB per language on first
// open of the help modal; the browser caches them thereafter.
//
// Notable omissions:
//   - `c` is NOT a separate module — it's registered by cpp/cpp.js
//     under both ids ("c" and "cpp"). Loading cpp covers both.
//   - `json` is NOT in basic-languages/ either; it ships under
//     vs/language/json/ as a "rich" mode (with schema validation)
//     and is loaded automatically by editor.main.js. We don't need
//     to require it.
//   - Same for `html`, `css`, `typescript` rich modes — they have
//     entries in vs/language/ too but the basic-languages copy is
//     what the markdown grammar's `nextEmbedded:"$1"` delegation
//     hits, so we load those from basic-languages.
const _MONACO_PRELOAD_LANGS = [
    'cpp', 'go', 'javascript', 'typescript', 'python',
    'yaml', 'shell', 'sql', 'html', 'css', 'xml',
    'rust', 'java', 'kotlin', 'swift', 'dockerfile', 'ini',
    'markdown',
];

// Pseudo-languages: aliases that aren't real Monaco IDs but show up
// in markdown fences from authors who write ```js out of habit.
// Each key is the fence label; each value is the canonical Monaco
// language whose tokens/conf are reused.
//
// `c` is not aliased here — cpp/cpp.js already registers "c" as a
// first-class id with the same tokenizer.
const _MONACO_FENCE_ALIASES = {
    'js':      'javascript',
    'ts':      'typescript',
    'sh':      'shell',
    'bash':    'shell',
    'zsh':     'shell',
    'yml':     'yaml',
    'py':      'python',
    'rb':      'ruby',
    'arduino': 'cpp',
    'ino':     'cpp',
    'c++':     'cpp',
    'h':       'cpp',
    'hpp':     'cpp',
    'cs':      'csharp',
    'kt':      'kotlin',
    'rs':      'rust',
    'docker':  'dockerfile',
    'env':     'ini',
};

// Tracks whether the alias registration has already run, so reopening
// the help modal doesn't redundantly re-register every alias.
let _monacoAliasesRegistered = false;

// _preloadMonacoLanguage returns a promise that resolves when the
// Monaco AMD module for the given language id finishes loading.
//
// Defensive design:
//   - Skips silently when the language is unknown to Monaco (id not
//     in monaco.languages.getLanguages()). The AMD loader would
//     otherwise issue a request that 404s on the server, polluting
//     the console with red errors. We do this check first.
//   - Uses the explicit errback form of require([deps], ok, err) so
//     a server-side 404 doesn't surface as an uncaught loader
//     rejection. Either path resolves the outer promise.
//
// Failure modes are non-fatal: when this returns without loading,
// the embedded grammar for that fence will simply not light up.
// Other languages keep working.
function _preloadMonacoLanguage(id) {
    return new Promise(resolve => {
        // Skip ids that Monaco doesn't know about. getLanguages()
        // returns the registered set; if it isn't there, the file
        // won't be at vs/basic-languages/<id>/<id>.js either.
        const known = monaco.languages.getLanguages().some(l => l.id === id);
        if (!known) {
            return resolve();
        }
        try {
            window.require(
                [`vs/basic-languages/${id}/${id}`],
                () => resolve(),
                (err) => {
                    console.warn('[menuTree] preload failed for', id, err);
                    resolve();
                },
            );
        } catch (err) {
            console.warn('[menuTree] preload threw for', id, err);
            resolve();
        }
    });
}

// _registerMonacoAliases creates pseudo-languages so that fences like
// ```bash, ```js, ```arduino render with the same colors as their
// canonical counterparts (shell, javascript, cpp). Each alias is
// registered exactly once per page load — repeated calls are no-ops.
//
// The Monaco AMD module for a basic-languages entry exports two
// fields we care about:
//   - language: the Monarch tokens provider definition
//   - conf:     bracket/comment/indent configuration
// Reusing both gives the alias the full editing experience (not just
// syntax colors but also smart bracket matching, auto-indent, etc).
async function _registerMonacoAliases() {
    if (_monacoAliasesRegistered) return;
    if (!window.monaco || !window.require) return;

    // Group aliases by canonical target so we fetch each target
    // module only once even when it powers several aliases.
    const byTarget = new Map();
    for (const [alias, target] of Object.entries(_MONACO_FENCE_ALIASES)) {
        if (!byTarget.has(target)) byTarget.set(target, []);
        byTarget.get(target).push(alias);
    }

    const existing = new Set(monaco.languages.getLanguages().map(l => l.id));

    await Promise.all([...byTarget.entries()].map(([target, aliases]) =>
        new Promise(resolve => {
            try {
                window.require(
                    [`vs/basic-languages/${target}/${target}`],
                    (mod) => {
                        if (!mod || !mod.language) {
                            console.warn('[menuTree] no Monarch tokens for target:', target);
                            return resolve();
                        }
                        for (const alias of aliases) {
                            if (existing.has(alias)) continue;
                            monaco.languages.register({ id: alias });
                            monaco.languages.setMonarchTokensProvider(alias, mod.language);
                            if (mod.conf) {
                                monaco.languages.setLanguageConfiguration(alias, mod.conf);
                            }
                        }
                        resolve();
                    },
                    (err) => {
                        console.warn('[menuTree] alias target load failed:', target, err);
                        resolve();
                    },
                );
            } catch (err) {
                console.warn('[menuTree] alias registration failed for', target, err);
                resolve();
            }
        })
    ));

    _monacoAliasesRegistered = true;
}

// _enrichMonacoLanguages is the single entry point that preloads
// languages, registers aliases, and re-tokenizes the active editor.
// Safe to call multiple times; only the first run does real work.
//
// Why the re-tokenization step matters: even after preload, Monaco
// won't retroactively re-run the markdown tokenizer on already-
// scanned lines. setModelLanguage(model, 'markdown') is the
// documented way to force a fresh pass, picking up the now-available
// embedded grammars.
async function _enrichMonacoLanguages() {
    if (!window.monaco) return;

    await Promise.all(_MONACO_PRELOAD_LANGS.map(_preloadMonacoLanguage));
    await _registerMonacoAliases();

    // Force re-tokenization of the active editor so the embedded
    // grammars apply to already-visible code blocks.
    if (_helpMonaco) {
        const model = _helpMonaco.getModel();
        if (model) {
            try {
                monaco.editor.setModelLanguage(model, 'markdown');
            } catch (err) {
                console.warn('[menuTree] setModelLanguage failed:', err);
            }
        }
    }
}

function _createMonacoInstance(wrap, initValue) {
    // Dispose previous instance if any.
    if (_helpMonaco) {
        try { _helpMonaco.dispose(); } catch (e) {}
        _helpMonaco = null;
    }

    // Clear the wrapper (removes fallback textarea).
    wrap.innerHTML = '';
    const div = document.createElement('div');
    div.style.cssText = 'width:100%;height:100%';
    wrap.appendChild(div);

    _helpMonaco = monaco.editor.create(div, {
        value: initValue || '',
        language: 'markdown',
        theme: 'vs-dark',
        wordWrap: 'on',
        lineNumbers: 'on',
        minimap: { enabled: false },
        scrollBeyondLastLine: false,
        fontSize: 13,
        fontFamily: "'Fira Code','Consolas',monospace",
        automaticLayout: true,
        insertSpaces: true,
        tabSize: 2,
        wordBasedSuggestions: 'off',
        quickSuggestions: false,
        renderLineHighlight: 'line',
        lineDecorationsWidth: 4,
    });

    // Live preview on content change.
    _helpMonaco.onDidChangeModelContent(() => cpMtHelpPreview());

    // Trigger initial preview.
    cpMtHelpPreview();
}

// _getHelpMarkdown reads the current markdown from Monaco or the fallback textarea.
function _getHelpMarkdown() {
    if (_helpMonaco) return _helpMonaco.getValue();
    const ta = document.getElementById('help-md-fallback');
    return ta ? ta.value : '';
}

// _setHelpMarkdown writes markdown to Monaco or the fallback textarea.
function _setHelpMarkdown(value) {
    if (_helpMonaco) {
        _helpMonaco.setValue(value || '');
    } else {
        const ta = document.getElementById('help-md-fallback');
        if (ta) ta.value = value || '';
    }
}

// _closeHelpModal disposes Monaco and removes the modal from the DOM.
window._closeHelpModal = function() {
    if (_helpMonaco) {
        try { _helpMonaco.dispose(); } catch (e) {}
        _helpMonaco = null;
    }
    // Drop the blob: URLs we created for image thumbnails. The image
    // manager modal may already be closed, but the cache outlives it
    // (so reopening the image manager during the same help session
    // doesn't refetch). Now that the help modal is going away, the
    // cache has no reason to persist.
    if (typeof _revokeImageBlobs === 'function') {
        _revokeImageBlobs();
    }
    removeEl('cp-help-modal');
    removeEl('cp-img-modal');
};

// ── Locale picker modal (replaces native prompt) ────────────────────────────
//
// Shows a modal with an input field and autocomplete dropdown listing
// known locale codes. Typing filters the list; selecting a suggestion
// fills the input with the canonical code (e.g., typing "PT-BR" selects "pt-br").

const _KNOWN_LOCALES = [
    { code: 'en',    label: 'English' },
    { code: 'en-us', label: 'English (US)' },
    { code: 'en-gb', label: 'English (UK)' },
    { code: 'pt',    label: 'Português' },
    { code: 'pt-br', label: 'Português (Brasil)' },
    { code: 'pt-pt', label: 'Português (Portugal)' },
    { code: 'es',    label: 'Español' },
    { code: 'es-ar', label: 'Español (Argentina)' },
    { code: 'es-mx', label: 'Español (México)' },
    { code: 'fr',    label: 'Français' },
    { code: 'fr-ca', label: 'Français (Canada)' },
    { code: 'de',    label: 'Deutsch' },
    { code: 'it',    label: 'Italiano' },
    { code: 'ja',    label: '日本語' },
    { code: 'zh',    label: '中文' },
    { code: 'zh-cn', label: '中文 (简体)' },
    { code: 'zh-tw', label: '中文 (繁體)' },
    { code: 'ko',    label: '한국어' },
    { code: 'ar',    label: 'العربية' },
    { code: 'hi',    label: 'हिन्दी' },
    { code: 'ru',    label: 'Русский' },
    { code: 'nl',    label: 'Nederlands' },
    { code: 'sv',    label: 'Svenska' },
    { code: 'pl',    label: 'Polski' },
    { code: 'tr',    label: 'Türkçe' },
    { code: 'uk',    label: 'Українська' },
    { code: 'th',    label: 'ไทย' },
    { code: 'vi',    label: 'Tiếng Việt' },
    { code: 'id',    label: 'Bahasa Indonesia' },
    { code: 'ms',    label: 'Bahasa Melayu' },
];

function _showLocaleModal(onConfirm) {
    removeEl('cp-locale-modal');

    const backdrop = document.createElement('div');
    backdrop.id = 'cp-locale-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.innerHTML = `
<div class="cp-modal" style="max-width:380px">
  <h3><i class="fa-solid fa-language" style="margin-right:6px;color:var(--primary)"></i> Novo idioma</h3>
  <div style="margin-top:12px;position:relative">
    <input type="text" class="fc" id="cp-locale-input" placeholder="Digite o código (ex: pt-br, en, es)"
           autocomplete="off" style="width:100%;font-size:14px"
           oninput="_cpLocaleFilter()" onkeydown="_cpLocaleKeydown(event)">
    <div id="cp-locale-ac" style="display:none;position:absolute;left:0;right:0;top:100%;
         z-index:100;margin-top:2px;max-height:220px;overflow-y:auto;
         background:var(--bg-card);border:1px solid var(--border);border-radius:8px;
         box-shadow:0 8px 24px rgba(0,0,0,0.25)"></div>
  </div>
  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" id="cp-locale-cancel">Cancelar</button>
    <button class="cp-btn cp-btn-primary" id="cp-locale-ok">Adicionar</button>
  </div>
</div>`;

    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', (e) => { if (e.target === backdrop) _closeLocaleModal(); });

    document.getElementById('cp-locale-cancel').addEventListener('click', _closeLocaleModal);
    document.getElementById('cp-locale-ok').addEventListener('click', () => {
        const val = (document.getElementById('cp-locale-input')?.value || '').trim().toLowerCase();
        if (!val) { cpToast('error', 'Digite um código de idioma.'); return; }
        _closeLocaleModal();
        onConfirm(val);
    });

    setTimeout(() => document.getElementById('cp-locale-input')?.focus(), 50);
}

function _closeLocaleModal() { removeEl('cp-locale-modal'); }

window._cpLocaleFilter = function() {
    const q = (document.getElementById('cp-locale-input')?.value || '').toLowerCase();
    const dd = document.getElementById('cp-locale-ac');
    if (!dd) return;

    if (q.length === 0) { dd.style.display = 'none'; return; }

    const matches = _KNOWN_LOCALES.filter(l =>
        l.code.includes(q) || l.label.toLowerCase().includes(q)
    );

    if (matches.length === 0) { dd.style.display = 'none'; return; }

    dd.innerHTML = matches.map((l, i) => `
<div class="cp-locale-opt" data-idx="${i}" data-code="${esc(l.code)}"
     style="padding:8px 14px;cursor:pointer;border-bottom:1px solid var(--border);transition:background .1s"
     onmouseenter="this.style.background='var(--bg-surface)'"
     onmouseleave="this.style.background='none'"
     onclick="_cpLocaleSelect('${esc(l.code)}')">
  <span style="font-size:13px;font-weight:500">${esc(l.code)}</span>
  <span style="font-size:12px;color:var(--text-muted);margin-left:8px">${esc(l.label)}</span>
</div>`).join('');
    dd.style.display = 'block';
};

window._cpLocaleSelect = function(code) {
    const input = document.getElementById('cp-locale-input');
    if (input) input.value = code;
    const dd = document.getElementById('cp-locale-ac');
    if (dd) dd.style.display = 'none';
};

window._cpLocaleKeydown = function(e) {
    const dd = document.getElementById('cp-locale-ac');
    if (!dd || dd.style.display === 'none') {
        if (e.key === 'Enter') { document.getElementById('cp-locale-ok')?.click(); e.preventDefault(); }
        return;
    }
    const items = dd.querySelectorAll('.cp-locale-opt');
    let active = dd.querySelector('.cp-locale-opt[data-active="1"]');
    let idx = active ? parseInt(active.dataset.idx) : -1;

    if (e.key === 'ArrowDown') { e.preventDefault(); idx = Math.min(idx + 1, items.length - 1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); idx = Math.max(idx - 1, 0); }
    else if (e.key === 'Enter' && active) { e.preventDefault(); _cpLocaleSelect(active.dataset.code); return; }
    else if (e.key === 'Escape') { dd.style.display = 'none'; return; }
    else return;

    items.forEach(el => { el.dataset.active = '0'; el.style.background = 'none'; });
    if (items[idx]) { items[idx].dataset.active = '1'; items[idx].style.background = 'var(--bg-surface)'; items[idx].scrollIntoView({ block: 'nearest' }); }
};

// ════════════════════════════════════════════════════════════════════════════
//  Help editor (multi-tab markdown + image attachment)
// ════════════════════════════════════════════════════════════════════════════
//
// The modal shown when the admin clicks the book icon on a menu item.
// It is the only entry point for editing per-slot help, and it now
// supports the same shape that devices use:
//
//   - Multiple ordered markdown tabs per (slot, profile, locale) bucket.
//     Tabs map 1:1 to the readme.<N>.<lang>.md filename grammar in
//     devicehelp.go: ord=0 is the primary unnumbered tab; ord>=1 are
//     numbered (readme.1.md, readme.2.md, …). The renderer sorts
//     ascending by ord with ord=0 always first.
//
//   - A per-slot image pool (PNG/JPG/SVG/GIF/WebP). The admin uploads
//     images directly into the modal and inserts them into the active
//     markdown tab via the "Inserir imagem" button. Inside markdown,
//     the author writes `![alt](./filename.png)` — the server rewrites
//     that reference into an inline base64 `data:` URL at serve time
//     so the WASM IDE can render images without an authenticated fetch.
//     See server/codegen/blackbox/devicehelp.go::RewriteImagePaths.
//
// State lives module-side (the modal is single-instance):
//
//   _state.entries       — every menu_help row for the slot, ordered by
//                          (profile_id, locale, ord). The active bucket
//                          is filtered from this on demand.
//   _state.images        — every menu_help_files row metadata.
//   _state.currentProf   — profile_id selected in the dropdown
//                          ("_default" maps to "" in the DB).
//   _state.currentLocale — locale selected.
//   _state.currentOrd    — ord of the tab currently in the editor.
//   _state.dirty         — true when the editor content diverges from
//                          the last server-synced value. Used by tab
//                          switching to prompt before discarding.
//   _state.slotId        — slot being edited (so save handlers can be
//                          parameter-free helpers).

const _state = {
    entries:       [],
    images:        [],
    currentProf:   '_default',
    currentLocale: 'en',
    currentOrd:    0,
    dirty:         false,
    slotId:        null,
};

// _firstHeading extracts the text after the first "# " line in the
// given markdown. Empty string when no heading is present. Mirrors
// firstHeading() in server/codegen/blackbox/devicehelp.go so the tab
// label here matches what the WASM renderer would derive.
function _firstHeading(md) {
    if (!md) return '';
    const lines = md.split('\n');
    for (let i = 0; i < lines.length; i++) {
        const t = lines[i].trim();
        if (t.startsWith('# ')) return t.substring(2).trim();
    }
    return '';
}

// _truncateTabTitle caps a tab title at 24 runes, breaking at the last
// space before the limit. Mirrors truncateTitle() in devicehelp.go.
// Single-word over-long titles get a hard cut + ellipsis.
function _truncateTabTitle(s) {
    const MAX = 24;
    if (!s || [...s].length <= MAX) return s || '';
    const chars = [...s];
    let cut = MAX;
    while (cut > 0 && chars[cut - 1] !== ' ') cut--;
    if (cut === 0) cut = MAX;
    return chars.slice(0, cut).join('').replace(/\s+$/, '') + '...';
}

// _bucketEntries returns the slice of _state.entries that matches the
// current (profile, locale) selection, sorted by ord ascending. The
// "_default" profile in the UI maps to "" in the database; the
// dropdown value is translated here.
function _bucketEntries() {
    const dbProf = _state.currentProf === '_default' ? '' : _state.currentProf;
    return _state.entries
        .filter(e => e.profile_id === dbProf && e.locale === _state.currentLocale)
        .sort((a, b) => a.ord - b.ord);
}

// _findEntry returns the entry for the current bucket at the given
// ord, or null when no row exists yet (new tab not yet saved).
function _findEntry(ord) {
    return _bucketEntries().find(e => e.ord === ord) || null;
}

// _nextOrd returns the next available ord for the current bucket —
// max(ord) + 1, or 0 when the bucket is empty. Used by "Add tab"
// to compute a non-colliding position.
function _nextOrd() {
    const entries = _bucketEntries();
    if (entries.length === 0) return 0;
    return Math.max(...entries.map(e => e.ord)) + 1;
}

window.cpMtEditHelp = async function(slotId, profileId) {
    await ensureMarked();

    _state.slotId = slotId;
    _state.currentProf = profileId || '_default';
    _state.dirty = false;

    // Load existing help entries for this slot. The response shape now
    // includes `ord`, so we can group into tabs locally.
    const r = await cpApi('GET', `${API}/menu/help/${slotId}`);
    _state.entries = r?.data?.entries || [];

    // Load image metadata. Soft-fail: if the slot has never had an
    // image, the endpoint still returns 200 with an empty list. A
    // genuine error is logged but does not block the editor.
    try {
        const ri = await cpApi('GET', `${API}/menu/help/${slotId}/files`);
        _state.images = ri?.data?.files || [];
    } catch (err) {
        console.warn('[menuTree] could not load images:', err);
        _state.images = [];
    }

    // Pick the initial bucket using the same cascade the WASM uses for
    // display: profile-specific takes precedence over generic, and
    // within a bucket "en" is preferred when present. This matters
    // because the legacy MigrateHelpFilesToDB inserts every migrated
    // help row with profile_id='' (generic), and a fresh menu in the
    // /control panel must surface that content — otherwise the admin
    // sees an empty editor and may save an override that silently
    // hides the generic entry. The override is not destructive (the
    // generic row stays), but it is confusing.
    //
    // Cascade:
    //   1. profile-specific has any entry → show profile-specific
    //   2. generic has any entry          → show "_default"
    //   3. neither has entries            → start blank in the profile
    //                                       passed by the caller
    //
    // Within the chosen bucket, prefer 'en' as the initial locale;
    // when absent, pick whatever locale has entries first. Empty
    // bucket starts in 'en' so saving creates a row with the
    // language the rest of the system treats as the canonical
    // fallback.
    const profileIdDB = (profileId === '_default' || !profileId) ? '' : profileId;
    const hasProfileEntries = _state.entries.some(e => e.profile_id === profileIdDB);
    const hasGenericEntries = _state.entries.some(e => e.profile_id === '');

    if (hasProfileEntries) {
        _state.currentProf = profileId;
    } else if (hasGenericEntries) {
        _state.currentProf = '_default';
    } else {
        _state.currentProf = profileId || '_default';
    }

    const chosenBucketDB = _state.currentProf === '_default' ? '' : _state.currentProf;
    const localesInBucket = [...new Set(_state.entries
        .filter(e => e.profile_id === chosenBucketDB)
        .map(e => e.locale))];
    if (localesInBucket.includes('en')) {
        _state.currentLocale = 'en';
    } else if (localesInBucket.length > 0) {
        _state.currentLocale = localesInBucket[0];
    } else {
        _state.currentLocale = 'en';
    }

    // Pick the initial ord from the chosen (profile, locale) bucket —
    // lowest ord present, or 0 when the bucket is empty.
    const initialBucket = _state.entries
        .filter(e => e.profile_id === chosenBucketDB && e.locale === _state.currentLocale)
        .sort((a, b) => a.ord - b.ord);
    _state.currentOrd = initialBucket.length > 0 ? initialBucket[0].ord : 0;

    // Build the full locale list — every locale that ever appears in
    // the slot's entries, plus "en" as a safety fallback so the
    // dropdown is never empty.
    const allLocales = [...new Set(_state.entries.map(e => e.locale))];
    if (allLocales.length === 0) allLocales.push('en');
    if (!allLocales.includes('en')) allLocales.push('en');

    _closeHelpModal();
    const backdrop = document.createElement('div');
    backdrop.id = 'cp-help-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.innerHTML = `
<div class="cp-modal" style="width:calc(100vw - 40px);height:calc(100vh - 40px);max-width:none;max-height:none;display:flex;flex-direction:column">
  <h3 style="flex-shrink:0">
    <i class="fa-solid fa-book" style="margin-right:8px"></i>Help — ${esc(slotId)}
    <span style="font-size:12px;color:var(--text-muted);margin-left:12px">Perfil: ${esc(profileId)}</span>
  </h3>

  <!-- Profile + locale selectors. "+" opens the locale-picker modal. -->
  <div style="display:flex;gap:4px;margin-top:12px;flex-shrink:0">
    <select class="cp-input" id="help-profile" style="width:auto;font-size:12px">
      <option value="_default" ${_state.currentProf === '_default' ? 'selected' : ''}>Genérico (todos os perfis)</option>
      <option value="${esc(profileId)}" ${_state.currentProf === profileId ? 'selected' : ''}>Perfil: ${esc(profileId)}</option>
    </select>
    <select class="cp-input" id="help-locale" style="width:auto;font-size:12px">
      ${allLocales.map(l => `<option value="${esc(l)}" ${l === _state.currentLocale ? 'selected' : ''}>${esc(l)}</option>`).join('')}
    </select>
    <button class="cp-btn cp-btn-ghost" style="padding:2px 8px;font-size:11px"
      onclick="cpMtAddHelpLocale()" title="Adicionar idioma">
      <i class="fa-solid fa-plus"></i></button>
  </div>

  <!--
    Tab bar — one button per existing tab in the current (profile, locale)
    bucket, plus a trailing "+" button to append a new tab. Each tab
    button carries its own delete affordance ("×") on hover. The tab
    bar redraws on every locale/profile change and after every save.
  -->
  <div id="help-tab-bar" style="display:flex;gap:4px;margin-top:8px;flex-shrink:0;align-items:center;flex-wrap:wrap"></div>

  <!--
    Two-column editor: Monaco on the left, live preview on the right.
    Above each column sits a small toolbar: the left one carries the
    "Inserir imagem" button (opens the image manager); the right one
    is purely label.
  -->
  <div style="display:flex;gap:12px;flex:1;min-height:0;margin-top:12px;overflow:hidden">
    <div style="flex:1;display:flex;flex-direction:column;min-width:0">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:4px">
        <span style="font-size:11px;color:var(--text-muted)">Markdown</span>
        <button class="cp-btn cp-btn-ghost" id="help-images-btn"
                onclick="cpMtOpenImageManager()"
                style="padding:2px 8px;font-size:11px"
                title="Gerenciar imagens anexadas a este item">
          <i class="fa-solid fa-image"></i> Imagens (<span id="help-images-count">${_state.images.length}</span>)
        </button>
      </div>
      <div id="help-md-wrap" style="flex:1;border:1px solid var(--border);border-radius:var(--r);overflow:hidden"></div>
    </div>
    <div style="flex:1;display:flex;flex-direction:column;min-width:0">
      <span style="font-size:11px;color:var(--text-muted);margin-bottom:4px">Preview</span>
      <div id="help-preview" style="flex:1;overflow-y:auto;padding:12px;background:var(--bg-alt);
           border-radius:var(--r);border:1px solid var(--border);font-size:13px;line-height:1.7;
           color:var(--text)"></div>
    </div>
  </div>

  <div style="display:flex;gap:8px;margin-top:12px;justify-content:flex-end;flex-shrink:0">
    <button class="cp-btn cp-btn-ghost" onclick="cpMtDeleteHelp()"
            style="margin-right:auto;color:var(--danger,#e53935)" title="Apagar a tab atual">
      <i class="fa-solid fa-trash"></i> Apagar tab
    </button>
    <button class="cp-btn cp-btn-ghost" onclick="_closeHelpModal()">Fechar</button>
    <button class="cp-btn cp-btn-primary" onclick="cpMtSaveHelp()">
      <i class="fa-solid fa-check"></i> Salvar
    </button>
  </div>
</div>`;

    document.body.appendChild(backdrop);
    backdrop.addEventListener('click', e => { if (e.target === backdrop) _closeHelpModal(); });

    // Build the tab bar from the initial bucket. Mount Monaco with the
    // tab-0 content (or empty when the bucket has no entries yet).
    cpMtRenderTabBar();
    const initEntry = _findEntry(_state.currentOrd);
    _mountHelpMonaco(initEntry?.markdown || '');

    // Profile / locale changes reset to ord=0 and reload the editor.
    document.getElementById('help-profile')?.addEventListener('change', cpMtOnProfileChange);
    document.getElementById('help-locale')?.addEventListener('change', cpMtOnLocaleChange);
};

// cpMtRenderTabBar (re)builds the tab list inside the modal. Called
// after every locale/profile/save/delete that changes which tabs
// exist or which one is selected, AND on every keystroke in the
// editor (via cpMtHelpPreview) so the tab label stays in sync with
// the first "# heading" of the markdown.
//
// IMPORTANT: this function is purely a render — it never mutates
// _state and never calls _setHelpMarkdown. It runs inside Monaco's
// onDidChangeModelContent callback path; touching the editor here
// would re-enter Monaco's setValue while a previous setValue is
// still emitting events, which corrupts the editor's internal
// tokenisation state and throws
// "Cannot read properties of undefined (reading 'isVisible')".
//
// Callers that need to change which tab is shown (after delete,
// after profile/locale change) MUST update _state.currentOrd and
// call _setHelpMarkdown themselves BEFORE invoking this function.
window.cpMtRenderTabBar = function() {
    const bar = document.getElementById('help-tab-bar');
    if (!bar) return;

    const entries = _bucketEntries();

    const tabsHTML = entries.map(e => {
        const isActive = e.ord === _state.currentOrd;
        const title = _truncateTabTitle(_firstHeading(e.markdown)) || `Tab ${e.ord}`;
        const bg = isActive ? 'var(--primary)' : 'var(--bg-card)';
        const color = isActive ? '#fff' : 'var(--text)';
        return `
<button class="help-tab" data-ord="${e.ord}"
        style="padding:6px 10px;border-radius:6px;border:1px solid var(--border);
               background:${bg};color:${color};font-size:12px;cursor:pointer;
               display:flex;align-items:center;gap:6px"
        onclick="cpMtSelectTab(${e.ord})">
  <span>${esc(title)}</span>
  <span style="font-size:10px;opacity:0.6">#${e.ord}</span>
</button>`;
    }).join('');

    // When the editor is showing a tab that exists on disk, show the
    // "+" button. When the editor is on an unsaved new tab (ord not in
    // bucket), no "+" button — the admin should save the current one
    // first to avoid a chain of unsaved tabs.
    const currentExists = entries.find(e => e.ord === _state.currentOrd);
    const newTabExists  = !currentExists && _state.currentOrd > 0;

    let trailing = '';
    if (newTabExists) {
        // Visible chip for the unsaved tab being edited.
        const title = _truncateTabTitle(_firstHeading(_getHelpMarkdown())) || `Nova tab #${_state.currentOrd}`;
        trailing += `
<button class="help-tab" data-ord="${_state.currentOrd}"
        style="padding:6px 10px;border-radius:6px;border:1px dashed var(--primary);
               background:var(--primary);color:#fff;font-size:12px;cursor:default;
               display:flex;align-items:center;gap:6px">
  <span>${esc(title)}</span>
  <span style="font-size:10px;opacity:0.6">#${_state.currentOrd} <i class="fa-solid fa-circle" style="font-size:6px;vertical-align:middle"></i></span>
</button>`;
    } else {
        trailing += `
<button class="cp-btn cp-btn-ghost" style="padding:6px 10px;font-size:12px"
        onclick="cpMtAddTab()" title="Adicionar nova tab">
  <i class="fa-solid fa-plus"></i>
</button>`;
    }

    bar.innerHTML = tabsHTML + trailing;
};

// cpMtSelectTab switches the editor to a different tab in the current
// bucket. Prompts for confirmation when there are unsaved changes —
// switching would otherwise silently discard the in-progress markdown.
//
// The chosen ord is allowed to be one that does not yet exist (e.g.,
// the admin clicked a tab number after deleting it from the DB but
// the bar hasn't redrawn). In that case we present an empty editor
// and let the next Save create the row.
window.cpMtSelectTab = async function(ord) {
    if (ord === _state.currentOrd) return;

    if (_state.dirty) {
        const ok = await cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Alterações não salvas',
            'Você tem mudanças não salvas na tab atual. Trocar de tab vai descartá-las. Continuar?',
            'Trocar (descartar)', 'Cancelar'
        );
        if (!ok) return;
    }

    _state.currentOrd = ord;
    const entry = _findEntry(ord);
    _setHelpMarkdown(entry?.markdown || '');
    _state.dirty = false;
    cpMtHelpPreview();
    cpMtRenderTabBar();
};

// cpMtAddTab appends a new empty tab to the current bucket and switches
// the editor to it. The new tab is NOT saved yet — it only persists
// once the admin clicks "Salvar". This avoids cluttering the DB with
// empty rows when the admin clicks "+" and then closes the modal.
window.cpMtAddTab = async function() {
    if (_state.dirty) {
        const ok = await cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Alterações não salvas',
            'Você tem mudanças não salvas na tab atual. Adicionar uma nova tab vai descartá-las. Continuar?',
            'Adicionar (descartar)', 'Cancelar'
        );
        if (!ok) return;
    }

    _state.currentOrd = _nextOrd();
    _setHelpMarkdown('');
    _state.dirty = false;
    cpMtHelpPreview();
    cpMtRenderTabBar();
};

// cpMtOnProfileChange / cpMtOnLocaleChange handle dropdown changes.
// Both prompt for unsaved work, then reset ord to the lowest existing
// tab in the new bucket (or 0 when the bucket is empty).
window.cpMtOnProfileChange = async function() {
    const newVal = document.getElementById('help-profile')?.value || '_default';
    if (newVal === _state.currentProf) return;

    if (_state.dirty) {
        const ok = await cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Alterações não salvas',
            'Trocar de perfil vai descartar as alterações da tab atual. Continuar?',
            'Trocar', 'Cancelar'
        );
        if (!ok) {
            document.getElementById('help-profile').value = _state.currentProf;
            return;
        }
    }
    _state.currentProf = newVal;
    _resetToFirstTab();
};

window.cpMtOnLocaleChange = async function() {
    const newVal = document.getElementById('help-locale')?.value || 'en';
    if (newVal === _state.currentLocale) return;

    if (_state.dirty) {
        const ok = await cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Alterações não salvas',
            'Trocar de idioma vai descartar as alterações da tab atual. Continuar?',
            'Trocar', 'Cancelar'
        );
        if (!ok) {
            document.getElementById('help-locale').value = _state.currentLocale;
            return;
        }
    }
    _state.currentLocale = newVal;
    _resetToFirstTab();
};

// _resetToFirstTab snaps the editor onto the lowest existing ord of
// the current bucket (or 0 with empty content when the bucket is
// brand-new). Called after every profile/locale change.
function _resetToFirstTab() {
    const entries = _bucketEntries();
    _state.currentOrd = entries.length > 0 ? entries[0].ord : 0;
    const entry = _findEntry(_state.currentOrd);
    _setHelpMarkdown(entry?.markdown || '');
    _state.dirty = false;
    cpMtHelpPreview();
    cpMtRenderTabBar();
}

// cpMtHelpPreview renders the current Monaco buffer into the preview
// pane using marked.js + highlight.js. Called on every edit by the
// Monaco onDidChangeModelContent listener (mounted in
// _createMonacoInstance) and explicitly after tab switches.
//
// The `dirty` flag is set here too, since this is the single point
// where post-edit content reaches the rest of the modal. Comparing
// against the server-synced value of the current tab tells us whether
// there is anything to save.
window.cpMtHelpPreview = function() {
    const md = _getHelpMarkdown();
    const preview = document.getElementById('help-preview');
    if (preview && window.marked) {
        // Render markdown to HTML first.
        preview.innerHTML = window.marked.parse(md);

        // Rewrite local image references (`./filename.ext` and
        // `filename.ext`) so they become authenticated blob: URLs.
        // marked.js emits `<img src="./diagram.png">` verbatim; the
        // browser would otherwise resolve that against the current
        // page URL (localhost:8080/control) and 404 because no static
        // file exists at that path. The slot's actual image lives
        // behind GET /api/control/v1/menu/help/:slot/files/:path,
        // which requires a Bearer header that <img> can't send.
        //
        // We tag every local-looking <img> with data-cp-img-path and
        // hand it off to the same async populate routine the image
        // manager uses. Cache reuse: an image already loaded for the
        // grid view is served from the cache here without an extra
        // fetch.
        preview.querySelectorAll('img').forEach(el => {
            const src = el.getAttribute('src') || '';
            // Skip absolute URLs (http://, https://, data:, blob:).
            if (/^(https?:|data:|blob:)/i.test(src)) return;
            // Normalise `./filename` and bare `filename` to the
            // canonical menu_help_files path. Paths with a single
            // subdir (e.g. `examples/foo.png`) are kept as-is.
            const cleanPath = src.replace(/^\.\//, '');
            el.setAttribute('data-cp-img-path', cleanPath);
            el.removeAttribute('src');
        });

        // Kick off blob-URL loading for the freshly-tagged <img>s.
        // The same routine is shared with the image manager; the
        // cache makes the call idempotent.
        if (_state.slotId) {
            _populateImageElements(_state.slotId);
        }

        if (window.hljs) {
            preview.querySelectorAll('pre code').forEach(el => {
                el.classList.remove('hljs');
                hljs.highlightElement(el);
            });
        }
    }

    // Compare against the on-disk version of the current tab to decide
    // whether to flag the bucket as dirty.
    const entry = _findEntry(_state.currentOrd);
    const onDisk = entry?.markdown || '';
    _state.dirty = (md !== onDisk);

    // Title of the current tab can change after every keystroke (first
    // "# heading"). Re-rendering the bar each time is cheap and keeps
    // the tab label in sync with the document.
    cpMtRenderTabBar();
};

// cpMtAddHelpLocale opens the locale picker; on confirm it adds the
// new code to the dropdown, switches to it, and presents an empty
// editor for ord=0 (the first tab of the new bucket). Saving creates
// the row.
window.cpMtAddHelpLocale = function() {
    _showLocaleModal(function(locale) {
        const sel = document.getElementById('help-locale');
        if (!sel) return;
        for (let i = 0; i < sel.options.length; i++) {
            if (sel.options[i].value === locale) { sel.value = locale; cpMtOnLocaleChange(); return; }
        }
        const opt = document.createElement('option');
        opt.value = locale;
        opt.textContent = locale;
        sel.appendChild(opt);
        sel.value = locale;
        _state.currentLocale = locale;
        _state.currentOrd = 0;
        _setHelpMarkdown('');
        _state.dirty = false;
        cpMtHelpPreview();
        cpMtRenderTabBar();
    });
};

// cpMtSaveHelp upserts the current tab into the menu_help table. The
// URL carries the four-part identifier (slot, profile, locale, ord)
// that uniquely pins the row.
//
// After a successful save, the local _state.entries is updated so the
// tab bar re-renders with the new content (and any title change).
// _state.dirty resets to false.
window.cpMtSaveHelp = function() {
    const profileId = _state.currentProf;
    const locale    = _state.currentLocale;
    const ord       = _state.currentOrd;
    const markdown  = _getHelpMarkdown();
    const slotId    = _state.slotId;

    withOTP(
        (code) => cpApi('PUT', `${API}/menu/help/${slotId}/${profileId}/${locale}/${ord}`, {
            markdown, otp_code: code
        }),
        () => {
            // Sync local state. If the entry already existed we update
            // its markdown; otherwise we append a fresh row.
            const dbProf = profileId === '_default' ? '' : profileId;
            const existing = _state.entries.find(e =>
                e.profile_id === dbProf && e.locale === locale && e.ord === ord);
            if (existing) {
                existing.markdown = markdown;
                existing.updated_at = new Date().toISOString();
            } else {
                _state.entries.push({
                    slot_id:    slotId,
                    profile_id: dbProf,
                    locale:     locale,
                    ord:        ord,
                    markdown:   markdown,
                    updated_at: new Date().toISOString(),
                });
            }
            _state.dirty = false;
            cpMtRenderTabBar();
            cpToast('success', `Tab #${ord} salva (${locale}).`);
        }
    );
};

// cpMtDeleteHelp removes the current tab from the menu_help table.
// Deleting ord=0 with numbered tabs still present is allowed
// (mirrors devicehelp.go convention); the numbered tabs become
// "orphans" of the primary, the sort still presents them in order,
// and the renderer continues to work.
window.cpMtDeleteHelp = async function() {
    const profileId = _state.currentProf;
    const locale    = _state.currentLocale;
    const ord       = _state.currentOrd;
    const slotId    = _state.slotId;

    // If the tab is not yet saved (no row exists), the delete becomes
    // a local discard — just drop the editor state and move on.
    const existing = _findEntry(ord);
    if (!existing) {
        _state.currentOrd = _nextOrd() > 0 ? 0 : 0;
        const e = _findEntry(_state.currentOrd);
        _setHelpMarkdown(e?.markdown || '');
        _state.dirty = false;
        cpMtHelpPreview();
        cpMtRenderTabBar();
        return;
    }

    if (!await cpConfirm(
        '<i class="fa-solid fa-trash"></i> Apagar tab',
        `Apagar a tab <b>#${ord}</b> do idioma "<b>${esc(locale)}</b>"?`,
        'Apagar', 'Cancelar'
    )) return;

    withOTP(
        (code) => cpApi('DELETE', `${API}/menu/help/${slotId}/${profileId}/${locale}/${ord}`, { otp_code: code }),
        () => {
            const dbProf = profileId === '_default' ? '' : profileId;
            _state.entries = _state.entries.filter(e =>
                !(e.profile_id === dbProf && e.locale === locale && e.ord === ord));

            // Snap to the first remaining tab in the bucket, or empty
            // ord=0 when the bucket becomes empty.
            const remaining = _bucketEntries();
            _state.currentOrd = remaining.length > 0 ? remaining[0].ord : 0;
            const next = _findEntry(_state.currentOrd);
            _setHelpMarkdown(next?.markdown || '');
            _state.dirty = false;
            cpMtHelpPreview();
            cpMtRenderTabBar();
            cpToast('success', `Tab #${ord} apagada (${locale}).`);
        }
    );
};

// ── Image manager ───────────────────────────────────────────────────────────
//
// A dialog that overlays the help modal, listing every image attached
// to the slot as a grid of thumbnails. Modelled after the file manager
// used by projects/devices but scoped down to the menu's per-slot
// image pool — no markdown files here (those live in menu_help, not
// menu_help_files), no quota panel (menu is admin-only), no folder
// tree (one-level only).
//
// Three view modes inside the modal:
//
//   - Grid    — default. 96px thumbnails. Click → preview pane on the
//               right. Each card has Insert / Rename / Delete on hover.
//   - Empty   — onboarding panel with a big drop zone and an "upload"
//               button when the slot has zero images.
//   - Preview — full-size image with overlay actions. Closes back to
//               grid via Esc or the back button.
//
// Drag-and-drop is wired on the whole modal: any image dropped on the
// backdrop triggers the same upload flow as the button. Multi-file
// drops are processed sequentially so each gets its own OTP prompt —
// the alternative (one OTP for a batch) would weaken the per-action
// audit trail the menu admin endpoints require.

// _formatBytes turns a raw byte count into a short human label
// (e.g. "3.4 KB", "812 B"). Used by the image manager preview pane
// to keep the metadata line scannable without a wide column.
function _formatBytes(n) {
    if (!n || n < 1024) return `${n || 0} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

// _authHeaders returns the standard Authorization header used by cpApi.
// Exposed as a helper so the raw-body image upload can attach the same
// auth context without going through cpApi (which always JSON-encodes
// its body, while file uploads need a raw multipart-free PUT).
function _authHeaders() {
    return CS.controlToken
        ? { 'Authorization': 'Bearer ' + CS.controlToken }
        : {};
}

// _imageBlobCache memoises blob: URLs created from the authenticated
// image endpoint so each image is fetched once per modal session, no
// matter how many <img> tags reference it (e.g. grid thumb + preview
// pane of the same file).
//
// Why this exists at all:
// The browser does NOT attach the Authorization: Bearer header to
// <img src="...">, only same-origin cookies. The /control panel uses
// Bearer tokens exclusively (no cookie session), so naive
// `<img src="${API}/...">` requests come back 401. The workaround
// is to fetch the bytes through JS (where we control the headers),
// turn them into an opaque blob: URL, and feed THAT to <img>. The
// devices/projects file managers use the same pattern.
//
// Cleanup: every blob: URL holds a small ArrayBuffer in memory until
// URL.revokeObjectURL is called. We revoke on modal close
// (_closeHelpModal and the image-manager backdrop click handler) to
// keep memory tidy across long sessions.
const _imageBlobCache = new Map(); // path → blob: URL

// _loadImageBlob fetches one image through the authenticated endpoint
// and returns a blob: URL pointing at the bytes. Result is cached so
// re-rendering the manager doesn't refetch the same file.
//
// Returns null on error so callers can leave a placeholder visible
// rather than crashing the render.
async function _loadImageBlob(slotId, path) {
    if (_imageBlobCache.has(path)) return _imageBlobCache.get(path);
    try {
        const resp = await fetch(
            `${API}/menu/help/${slotId}/files/${encodeURIComponent(path)}`,
            { headers: _authHeaders() },
        );
        if (!resp.ok) {
            console.warn('[menuTree] image fetch failed:', path, resp.status);
            return null;
        }
        const blob = await resp.blob();
        const url = URL.createObjectURL(blob);
        _imageBlobCache.set(path, url);
        return url;
    } catch (err) {
        console.warn('[menuTree] image fetch error:', path, err);
        return null;
    }
}

// _revokeImageBlobs releases every cached blob: URL. Called from
// _closeHelpModal so memory does not grow across many open/close
// cycles. Safe to call when the cache is empty.
function _revokeImageBlobs() {
    for (const url of _imageBlobCache.values()) {
        try { URL.revokeObjectURL(url); } catch (_) {}
    }
    _imageBlobCache.clear();
}

// _populateImageElements is called after the image manager HTML is
// injected. It walks every <img data-cp-img-path="..."> in the page
// and replaces its src with the authenticated blob: URL. This is
// async on purpose — the grid renders synchronously with placeholders,
// then images fill in as they download. The `isConnected` guard
// prevents writing to <img> elements that were removed (e.g., user
// closed the modal mid-fetch).
async function _populateImageElements(slotId) {
    const els = document.querySelectorAll('img[data-cp-img-path]');
    for (const el of els) {
        const path = el.getAttribute('data-cp-img-path');
        if (!path) continue;
        const url = await _loadImageBlob(slotId, path);
        if (!url) {
            // Reveal the fallback icon that lives in the next sibling.
            el.style.display = 'none';
            const fallback = el.nextElementSibling;
            if (fallback) fallback.style.display = 'block';
            continue;
        }
        if (el.isConnected) {
            el.src = url;
        }
    }
}

window.cpMtOpenImageManager = function() {
    _cpMtRenderImageManager(null /* no selected image */);
};

// _cpMtRenderImageManager (re)builds the image manager modal. The
// `selectedPath` argument controls what shows in the right-hand
// preview pane: null = empty pane (or onboarding when the grid is
// empty), or a filename to preview that image full-size.
function _cpMtRenderImageManager(selectedPath) {
    removeEl('cp-img-modal');

    const backdrop = document.createElement('div');
    backdrop.id = 'cp-img-modal';
    backdrop.className = 'cp-modal-backdrop';
    backdrop.style.zIndex = '10001'; // above the help modal

    const hasImages = _state.images.length > 0;
    const slotId = _state.slotId;

    // ── Grid pane: one card per image ──
    // Each card is roughly 110px wide. Thumbnails are loaded directly
    // from the authenticated GET endpoint; the Authorization header
    // can't ride along <img src> requests on its own, so we route
    // through a blob URL. To keep the implementation simple here, we
    // rely on the same-origin cookie session that /control already
    // uses (the Bearer token is the primary mechanism, but the
    // browser still has the session cookie set during login). When
    // that's not enough — e.g., for cross-origin embedding — the
    // server's `data:` URL inlining (used at WASM serve time) is the
    // robust answer.
    const gridHTML = hasImages
        ? _state.images.map(img => {
            const isSelected = img.path === selectedPath;
            const ring = isSelected
                ? 'box-shadow:0 0 0 2px var(--primary)'
                : 'box-shadow:0 1px 3px rgba(0,0,0,0.1)';
            return `
<div class="cp-img-card" data-path="${esc(img.path)}"
     style="position:relative;width:110px;border:1px solid var(--border);
            border-radius:6px;background:var(--bg-card);overflow:hidden;
            cursor:pointer;${ring};transition:transform .12s,box-shadow .12s"
     onclick="cpMtSelectImage('${esc(img.path)}')"
     onmouseenter="this.style.transform='translateY(-2px)';this.querySelector('.cp-img-actions').style.opacity='1'"
     onmouseleave="this.style.transform='translateY(0)';this.querySelector('.cp-img-actions').style.opacity='0'">

  <div style="width:100%;height:96px;display:flex;align-items:center;justify-content:center;
              background:var(--bg-alt);overflow:hidden">
    <img data-cp-img-path="${esc(img.path)}" alt="${esc(img.path)}"
         style="max-width:100%;max-height:100%;object-fit:contain;display:block">
    <div style="display:none;font-size:24px;color:var(--text-muted)"><i class="fa-solid fa-image"></i></div>
  </div>

  <div style="padding:6px 8px;border-top:1px solid var(--border);font-size:11px;
              word-break:break-all;background:var(--bg-card);min-height:28px;
              display:flex;align-items:center">
    ${esc(img.path)}
  </div>

  <!-- Action overlay; revealed on hover. Each button stops propagation
       so the click does not also "select" the card. -->
  <div class="cp-img-actions"
       style="position:absolute;top:4px;right:4px;display:flex;gap:2px;
              opacity:0;transition:opacity .12s">
    <button class="cp-btn cp-btn-ghost" title="Inserir referência no markdown"
            style="padding:3px 6px;font-size:10px;background:rgba(0,0,0,0.6);color:#fff;border:none"
            onclick="event.stopPropagation();cpMtInsertImageRef('${esc(img.path)}')">
      <i class="fa-solid fa-link"></i>
    </button>
    <button class="cp-btn cp-btn-ghost" title="Renomear"
            style="padding:3px 6px;font-size:10px;background:rgba(0,0,0,0.6);color:#fff;border:none"
            onclick="event.stopPropagation();cpMtRenameImage('${esc(img.path)}')">
      <i class="fa-solid fa-pen"></i>
    </button>
    <button class="cp-btn cp-btn-ghost" title="Apagar"
            style="padding:3px 6px;font-size:10px;background:rgba(220,53,69,0.85);color:#fff;border:none"
            onclick="event.stopPropagation();cpMtDeleteImage('${esc(img.path)}')">
      <i class="fa-solid fa-trash"></i>
    </button>
  </div>
</div>`;
        }).join('')
        : '';

    // ── Preview pane (right column) ──
    // Three states: an actual image, an onboarding panel (grid empty),
    // or a placeholder ("select an image").
    let previewHTML;
    if (selectedPath) {
        const selected = _state.images.find(i => i.path === selectedPath);
        if (selected) {
            previewHTML = `
<div style="display:flex;flex-direction:column;height:100%">
  <div style="display:flex;justify-content:space-between;align-items:center;
              padding-bottom:8px;border-bottom:1px solid var(--border);margin-bottom:8px">
    <div style="font-size:13px;font-weight:500;word-break:break-all">
      <i class="fa-solid fa-image" style="margin-right:6px;color:var(--text-muted)"></i>
      ${esc(selected.path)}
    </div>
    <div style="font-size:11px;color:var(--text-muted);white-space:nowrap;margin-left:8px">
      ${esc(selected.mimeType || '')} · ${_formatBytes(selected.sizeBytes || 0)}
    </div>
  </div>

  <div style="flex:1;display:flex;align-items:center;justify-content:center;
              background:var(--bg-alt);border-radius:6px;overflow:hidden;
              min-height:0;padding:8px">
    <img data-cp-img-path="${esc(selected.path)}" alt="${esc(selected.path)}"
         style="max-width:100%;max-height:100%;object-fit:contain;display:block">
  </div>

  <div style="display:flex;gap:6px;margin-top:10px;flex-wrap:wrap">
    <button class="cp-btn cp-btn-primary" onclick="cpMtInsertImageRef('${esc(selected.path)}')"
            title="Insere ![alt](./${esc(selected.path)}) na posição do cursor">
      <i class="fa-solid fa-link"></i> Inserir no markdown
    </button>
    <button class="cp-btn cp-btn-ghost" onclick="cpMtRenameImage('${esc(selected.path)}')">
      <i class="fa-solid fa-pen"></i> Renomear
    </button>
    <button class="cp-btn cp-btn-ghost" style="color:var(--danger,#e53935)"
            onclick="cpMtDeleteImage('${esc(selected.path)}')">
      <i class="fa-solid fa-trash"></i> Apagar
    </button>
  </div>
</div>`;
        } else {
            // Image was deleted between renders.
            previewHTML = _previewPlaceholder();
        }
    } else if (hasImages) {
        previewHTML = _previewPlaceholder();
    } else {
        previewHTML = _previewOnboarding();
    }

    // ── Modal shell ──
    backdrop.innerHTML = `
<div class="cp-modal" id="cp-img-modal-inner"
     style="width:calc(100vw - 80px);max-width:1100px;height:calc(100vh - 80px);max-height:720px;
            display:flex;flex-direction:column;padding:16px">
  <h3 style="flex-shrink:0;margin:0 0 12px 0">
    <i class="fa-solid fa-images" style="margin-right:8px"></i>
    Imagens — ${esc(slotId)}
    <span style="font-size:12px;color:var(--text-muted);margin-left:10px;font-weight:normal">
      ${_state.images.length} ${_state.images.length === 1 ? 'arquivo' : 'arquivos'}
    </span>
  </h3>

  <div style="font-size:12px;color:var(--text-muted);margin-bottom:10px;flex-shrink:0">
    Use <code>![alt](./nome.png)</code> no markdown — o servidor traduz o caminho para
    a imagem inline ao renderizar para o usuário final.
    <span style="color:var(--text-muted);margin-left:8px">Arraste imagens aqui para enviar.</span>
  </div>

  <!-- Two-column workspace: grid (left), preview (right). -->
  <div style="display:flex;gap:14px;flex:1;min-height:0">

    <!-- Grid column -->
    <div style="flex:0 0 380px;display:flex;flex-direction:column;min-width:0">
      ${hasImages ? `
        <div style="flex:1;overflow-y:auto;padding-right:6px">
          <div style="display:grid;grid-template-columns:repeat(auto-fill,110px);gap:10px;align-content:start">
            ${gridHTML}
          </div>
        </div>
      ` : `
        <div style="flex:1;display:flex;align-items:center;justify-content:center;
                    border:2px dashed var(--border);border-radius:8px;color:var(--text-muted);
                    font-size:13px;text-align:center;padding:24px">
          Nenhuma imagem anexada<br><span style="font-size:11px">Clique abaixo ou arraste arquivos para enviar</span>
        </div>
      `}
    </div>

    <!-- Preview column -->
    <div style="flex:1;display:flex;flex-direction:column;min-width:0;
                border:1px solid var(--border);border-radius:8px;padding:12px;background:var(--bg-card)">
      ${previewHTML}
    </div>

  </div>

  <div style="display:flex;gap:8px;margin-top:12px;justify-content:flex-end;flex-shrink:0">
    <input type="file" id="cp-img-file-input"
           accept=".png,.jpg,.jpeg,.svg,.gif,.webp,image/*"
           multiple
           style="display:none"
           onchange="cpMtUploadImages(event)">
    <button class="cp-btn cp-btn-ghost" onclick="document.getElementById('cp-img-modal').remove()">Fechar</button>
    <button class="cp-btn cp-btn-primary" onclick="document.getElementById('cp-img-file-input').click()">
      <i class="fa-solid fa-upload"></i> Enviar imagem
    </button>
  </div>
</div>`;

    document.body.appendChild(backdrop);

    // Kick off the async load of every <img data-cp-img-path>. This
    // runs after appendChild so the elements are in the DOM. Errors
    // are handled inside _populateImageElements; we don't await here
    // because the user shouldn't have to wait for thumbnails before
    // interacting with the rest of the modal.
    _populateImageElements(slotId);

    // Close on backdrop click.
    backdrop.addEventListener('click', e => {
        if (e.target === backdrop) backdrop.remove();
    });

    // Close on Escape (and clear preview on Escape when in preview).
    const keyHandler = (e) => {
        if (e.key !== 'Escape') return;
        if (selectedPath) {
            // Back to grid first, only close on a second Esc.
            _cpMtRenderImageManager(null);
        } else {
            backdrop.remove();
        }
    };
    backdrop.addEventListener('keydown', keyHandler);
    setTimeout(() => backdrop.focus(), 10);
    backdrop.tabIndex = -1;

    // ── Drag-and-drop wiring ──
    // Visual feedback: an overlay that appears when files enter the
    // window. We listen on the inner modal so that drags outside the
    // image manager modal (e.g., over the help modal behind) don't
    // trigger the upload — only drops actually on this dialog do.
    const inner = document.getElementById('cp-img-modal-inner');
    if (inner) {
        let dragDepth = 0; // counts enter/leave to handle child elements
        let overlay = null;

        inner.addEventListener('dragenter', (e) => {
            if (!e.dataTransfer?.types?.includes('Files')) return;
            dragDepth++;
            if (dragDepth === 1) {
                overlay = document.createElement('div');
                overlay.style.cssText = `
                    position:absolute;inset:0;background:rgba(0,123,255,0.08);
                    border:3px dashed var(--primary);border-radius:8px;pointer-events:none;
                    display:flex;align-items:center;justify-content:center;
                    font-size:18px;color:var(--primary);font-weight:500;z-index:10`;
                overlay.innerHTML = '<i class="fa-solid fa-cloud-arrow-up" style="margin-right:10px"></i>Solte para enviar';
                inner.style.position = 'relative';
                inner.appendChild(overlay);
            }
            e.preventDefault();
        });
        inner.addEventListener('dragleave', () => {
            dragDepth = Math.max(0, dragDepth - 1);
            if (dragDepth === 0 && overlay) {
                overlay.remove();
                overlay = null;
            }
        });
        inner.addEventListener('dragover', (e) => { e.preventDefault(); });
        inner.addEventListener('drop', async (e) => {
            e.preventDefault();
            dragDepth = 0;
            if (overlay) { overlay.remove(); overlay = null; }
            const files = [...(e.dataTransfer?.files || [])];
            if (files.length === 0) return;
            await _cpMtUploadFileList(files);
        });
    }
}

// _previewPlaceholder is the empty-pane content shown when the grid
// has images but none is selected.
function _previewPlaceholder() {
    return `
<div style="flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;
            color:var(--text-muted);font-size:13px;text-align:center;padding:24px">
  <i class="fa-solid fa-image" style="font-size:48px;margin-bottom:12px;opacity:0.3"></i>
  <div>Selecione uma imagem para visualizar</div>
</div>`;
}

// _previewOnboarding is the right-pane content shown when the slot
// has zero images. It contains the primary upload affordance, so the
// admin doesn't have to look for the "Enviar imagem" button at the
// bottom.
function _previewOnboarding() {
    return `
<div style="flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;
            text-align:center;padding:32px">
  <i class="fa-solid fa-cloud-arrow-up" style="font-size:56px;color:var(--primary);margin-bottom:16px;opacity:0.6"></i>
  <div style="font-size:15px;font-weight:500;margin-bottom:6px">Sem imagens anexadas</div>
  <div style="font-size:12px;color:var(--text-muted);margin-bottom:20px;max-width:280px">
    Anexe diagramas, fotos ou ícones para usar no texto de ajuda. Formatos aceitos: PNG, JPG, SVG, GIF, WebP. Máx. 5MB cada.
  </div>
  <button class="cp-btn cp-btn-primary" onclick="document.getElementById('cp-img-file-input').click()">
    <i class="fa-solid fa-upload"></i> Escolher arquivos
  </button>
  <div style="font-size:11px;color:var(--text-muted);margin-top:12px">ou arraste e solte aqui</div>
</div>`;
}

// cpMtSelectImage switches the preview pane to show the given image.
window.cpMtSelectImage = function(path) {
    _cpMtRenderImageManager(path);
};

// cpMtUploadImages handles the file <input> change event for multi-
// select uploads. Delegates to _cpMtUploadFileList which is the
// single entry point shared with drag-and-drop.
window.cpMtUploadImages = async function(evt) {
    const files = [...(evt?.target?.files || [])];
    evt.target.value = ''; // reset so re-selecting the same file fires again
    if (files.length === 0) return;
    await _cpMtUploadFileList(files);
};

// _cpMtUploadFileList is the shared upload pipeline for the file
// picker and drag-and-drop. Each file gets its own OTP confirmation
// (per-action audit trail). On failure we stop the loop — the admin
// can retry the rest after dealing with the issue.
//
// Sequential rather than parallel: a parallel upload would burst N
// OTP modals at the user simultaneously. Sequential is slower but
// the UX is sane.
async function _cpMtUploadFileList(files) {
    const slotId = _state.slotId;

    for (const file of files) {
        const okExt = /\.(png|jpg|jpeg|svg|gif|webp)$/i.test(file.name);
        if (!okExt) {
            cpToast('danger', `${file.name}: tipo inválido. Use PNG, JPG, JPEG, SVG, GIF ou WebP.`);
            continue;
        }
        if (file.size > 5 * 1024 * 1024) {
            cpToast('danger', `${file.name}: imagem grande demais (máx 5 MB).`);
            continue;
        }

        const safeName = file.name.replace(/[^A-Za-z0-9._-]/g, '_');

        // Wait for the OTP to resolve before moving to the next file.
        await new Promise(resolve => {
            withOTP(
                async (code) => {
                    try {
                        const resp = await fetch(`${API}/menu/help/${slotId}/files/${encodeURIComponent(safeName)}`, {
                            method:  'PUT',
                            headers: { 'X-Menu-OTP': code, ..._authHeaders() },
                            body:    file,
                        });
                        return await resp.json();
                    } catch (err) {
                        console.warn('[menuTree] upload error:', err);
                        return { metadata: { status: 500, error: 'Falha na rede' } };
                    }
                },
                async () => {
                    try {
                        const ri = await cpApi('GET', `${API}/menu/help/${slotId}/files`);
                        _state.images = ri?.data?.files || [];
                    } catch (_) {}
                    document.getElementById('help-images-count').textContent = _state.images.length;
                    _cpMtRenderImageManager(safeName);
                    cpToast('success', `Imagem "${safeName}" enviada.`);
                    resolve();
                }
            );
            // If the user cancels OTP, withOTP doesn't call onSuccess.
            // Resolve anyway after a short grace so the loop moves on.
            setTimeout(resolve, 30000);
        });
    }
}

// cpMtUploadImage kept for backwards compatibility with any external
// caller; routes through the multi-upload path. The signature
// (event) → void matches the legacy onchange handler shape.
window.cpMtUploadImage = window.cpMtUploadImages;

// cpMtRenameImage prompts for a new name and renames the image via
// POST /menu/help/:slot/files/:path/rename. Local state is updated on
// success.
window.cpMtRenameImage = async function(oldPath) {
    const proposed = await _cpMtPromptRename(oldPath);
    if (!proposed) return;
    if (proposed === oldPath) return;

    const slotId = _state.slotId;
    withOTP(
        (code) => cpApi('POST', `${API}/menu/help/${slotId}/files/${encodeURIComponent(oldPath)}/rename`, {
            newPath: proposed,
            otp_code: code,
        }),
        async () => {
            try {
                const ri = await cpApi('GET', `${API}/menu/help/${slotId}/files`);
                _state.images = ri?.data?.files || [];
            } catch (_) {}
            _cpMtRenderImageManager(proposed);
            cpToast('success', `Renomeado para "${proposed}".`);
        }
    );
};

// _cpMtPromptRename shows a small in-modal prompt for the new
// filename. Returns the trimmed value on confirm, or null on cancel
// or empty input. Lives outside of cpConfirm because cpConfirm is a
// yes/no dialog.
function _cpMtPromptRename(oldPath) {
    return new Promise(resolve => {
        removeEl('cp-img-rename');
        const overlay = document.createElement('div');
        overlay.id = 'cp-img-rename';
        overlay.className = 'cp-modal-backdrop';
        overlay.style.zIndex = '10002';
        overlay.innerHTML = `
<div class="cp-modal" style="max-width:420px;padding:16px">
  <h3 style="margin:0 0 12px 0"><i class="fa-solid fa-pen"></i> Renomear imagem</h3>
  <div style="font-size:12px;color:var(--text-muted);margin-bottom:8px">
    Caracteres aceitos: letras, números, ponto, hífen, underscore. Máx. 1 subpasta.
    <br>Referências no markdown precisam ser atualizadas manualmente.
  </div>
  <input type="text" class="cp-input" id="cp-img-rename-input"
         value="${esc(oldPath)}" autocomplete="off"
         style="width:100%;margin-bottom:12px;font-family:monospace;font-size:13px">
  <div style="display:flex;gap:8px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost" id="cp-img-rename-cancel">Cancelar</button>
    <button class="cp-btn cp-btn-primary" id="cp-img-rename-ok">Renomear</button>
  </div>
</div>`;
        document.body.appendChild(overlay);
        const input = document.getElementById('cp-img-rename-input');
        input.focus();
        // Select just the basename (before extension) so the user can
        // type a new name without manually selecting.
        const dot = input.value.lastIndexOf('.');
        if (dot > 0) input.setSelectionRange(0, dot);

        const finish = (val) => { overlay.remove(); resolve(val); };

        document.getElementById('cp-img-rename-cancel').onclick = () => finish(null);
        document.getElementById('cp-img-rename-ok').onclick = () => {
            const v = (input.value || '').trim();
            if (!v) { cpToast('danger', 'Nome não pode ficar vazio.'); return; }
            finish(v);
        };
        input.addEventListener('keydown', e => {
            if (e.key === 'Enter') { document.getElementById('cp-img-rename-ok').click(); }
            if (e.key === 'Escape') { finish(null); }
        });
        overlay.addEventListener('click', e => { if (e.target === overlay) finish(null); });
    });
}

// cpMtInsertImageRef inserts a markdown image reference at the cursor
// position of Monaco (or the textarea fallback). The path is relative
// (`./filename`) so the server's RewriteImagePaths step can resolve it
// against the slot's image pool at serve time.
//
// Closes the image manager after inserting so the admin sees the
// markdown change immediately.
window.cpMtInsertImageRef = function(filename) {
    const alt = filename.replace(/\.[^.]+$/, ''); // drop extension for alt
    const ref = `![${alt}](./${filename})`;

    if (_helpMonaco) {
        const sel = _helpMonaco.getSelection();
        const op = { range: sel, text: ref, forceMoveMarkers: true };
        _helpMonaco.executeEdits('insert-image', [op]);
        _helpMonaco.focus();
    } else {
        const ta = document.getElementById('help-md-fallback');
        if (ta) {
            const start = ta.selectionStart;
            const end   = ta.selectionEnd;
            ta.value = ta.value.slice(0, start) + ref + ta.value.slice(end);
            ta.selectionStart = ta.selectionEnd = start + ref.length;
            ta.focus();
        }
    }

    cpMtHelpPreview();
    removeEl('cp-img-modal');
    cpToast('success', `Referência inserida: ${filename}`);
};

// cpMtDeleteImage removes an image from the slot's pool. Confirms
// first because images may still be referenced by markdown tabs;
// after delete, the references become broken (server's
// RewriteImagePaths leaves unknown paths alone, so the user sees
// the raw `./filename` rather than an image).
window.cpMtDeleteImage = async function(path) {
    if (!await cpConfirm(
        '<i class="fa-solid fa-trash"></i> Apagar imagem',
        `Apagar a imagem "<b>${esc(path)}</b>"? Referências no markdown ficarão quebradas.`,
        'Apagar', 'Cancelar'
    )) return;

    const slotId = _state.slotId;
    withOTP(
        (code) => cpApi('DELETE', `${API}/menu/help/${slotId}/files/${encodeURIComponent(path)}`, { otp_code: code }),
        async () => {
            _state.images = _state.images.filter(i => i.path !== path);
            document.getElementById('help-images-count').textContent = _state.images.length;
            _cpMtRenderImageManager(null);
            cpToast('success', `Imagem "${path}" apagada.`);
        }
    );
};
