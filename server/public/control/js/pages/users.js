// server/public/control/js/pages/users.js — User management screen.
//
// Role change flow:
//  1. Admin changes role dropdowns — bottom bar appears with pending count.
//  2. Admin clicks "Aplicar" — POST /users/role-otp sends OTP to admin email.
//  3. Modal appears — admin types the 6-digit code.
//  4. PUT /users/roles sends { changes: [...], otp_code } as a single batch.

import { cpApi }             from '../api.js';
import { cpToast, cpConfirm } from '../toast.js';
import { CS }                from '../state.js';
import { registerLeaveGuard } from '../main.js';

// ── Constants ─────────────────────────────────────────────────────────────────

const PAGE_SIZE = 20;

const ROLES = {
    user:                { label: 'User',                badge: 'cp-badge-user'     },
    official_specialist: { label: 'Official Specialist', badge: 'cp-badge-official' },
    admin:               { label: 'Admin',               badge: 'cp-badge-admin'    },
};

// ── State ─────────────────────────────────────────────────────────────────────

// _pending maps userID → newRole for unsaved changes.
const _pending = new Map();

let _currentPage  = 1;
let _currentQuery = '';
let _totalPages   = 1;

// ── Entry point ───────────────────────────────────────────────────────────────

export async function renderUsers(root) {
    setBreadcrumb('Usuários');
    _pending.clear();
    _currentPage  = 1;
    _currentQuery = '';

    // Guard navigation away when there are unsaved changes.
    // Uses cpConfirm (async custom modal) — returns a Promise so the guard
    // must handle navigation itself after the user responds.
    registerLeaveGuard((dest) => {
        if (_pending.size === 0) return true; // nothing pending — allow immediately

        // Show async modal, then navigate if confirmed.
        const n = _pending.size;
        cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Alterações pendentes',
            `Você tem ${n} alteração${n !== 1 ? 'ões' : ''} pendente${n !== 1 ? 's' : ''} não salva${n !== 1 ? 's' : ''}.<br>Deseja sair sem aplicar?`,
            'Sair sem salvar',
            'Voltar'
        ).then((confirmed) => {
            if (confirmed) {
                _pending.clear();
                registerLeaveGuard(null);
                window.cpNav(dest); // navigate now that the user confirmed
            }
        });

        return false; // block navigation until the modal resolves
    });

    root.innerHTML = `
${otpModalHTML()}

<div class="cp-page-title">
  <i class="fa-solid fa-users"></i> Usuários
</div>

<div class="cp-card">
  <div class="cp-card-header">
    <span>Lista de usuários</span>
    <input class="cp-input" id="cp-user-search"
           placeholder="Buscar por nome ou e-mail…"
           style="width:220px;padding:6px 10px;font-size:13px"
           oninput="cpUserSearchInput(this.value)">
  </div>
  <div class="cp-card-body" style="padding:0">
    <div id="cp-users-body"><div class="cp-spinner"></div></div>
  </div>
  <div id="cp-pagination" class="cp-pagination"></div>
</div>

<!-- Fixed bottom bar — visible only when there are pending changes -->
<div id="cp-bottom-bar" class="cp-bottom-bar" style="display:none">
  <span id="cp-pending-label"></span>
  <div style="display:flex;gap:8px">
    <button class="cp-btn cp-btn-ghost" onclick="cpCancelPending()">
      <i class="fa-solid fa-xmark"></i> Cancelar
    </button>
    <button class="cp-btn cp-btn-warning" onclick="cpApplyChanges()">
      <i class="fa-solid fa-check"></i> Aplicar
    </button>
  </div>
</div>`;

    await fetchAndRender();
}

// ── Data fetching ─────────────────────────────────────────────────────────────

async function fetchAndRender() {
    const el = document.getElementById('cp-users-body');
    if (!el) return;
    el.innerHTML = '<div class="cp-spinner"></div>';

    const params = new URLSearchParams({
        page:  _currentPage,
        limit: PAGE_SIZE,
    });
    if (_currentQuery) params.set('q', _currentQuery);

    const r = await cpApi('GET', '/api/control/v1/users?' + params);
    if (r?.metadata?.status !== 200) {
        el.innerHTML = `<div class="cp-empty">
          <i class="fa-solid fa-triangle-exclamation"></i>
          ${r?.metadata?.error || 'Erro ao carregar usuários.'}
        </div>`;
        return;
    }

    _totalPages = r.data?.totalPages ?? 1;
    renderTable(r.data?.users ?? []);
    renderPagination();
}

// ── Table ─────────────────────────────────────────────────────────────────────

function renderTable(users) {
    const el = document.getElementById('cp-users-body');
    if (!el) return;

    if (!users.length) {
        el.innerHTML = `<div class="cp-empty">
          <i class="fa-solid fa-users-slash"></i>Nenhum usuário encontrado.
        </div>`;
        return;
    }

    const myID = CS.user?.uid || '';

    el.innerHTML = `
<div class="cp-table-wrap">
<table class="cp-table">
  <thead>
    <tr>
      <th>Usuário</th>
      <th>E-mail</th>
      <th>Role atual</th>
      <th>Novo role</th>
      <th>Criado em</th>
    </tr>
  </thead>
  <tbody>
    ${users.map(u => {
        const isMe    = u.id === myID;
        const pending = _pending.get(u.id);
        const rowCls  = isMe ? 'cp-row-me' : pending ? 'cp-row-pending' : '';
        const dis     = isMe ? 'disabled title="Você não pode alterar seu próprio role."' : '';
        const selectVal = pending ?? u.role;

        return `
<tr id="cp-row-${u.id}" class="${rowCls}">
  <td>
    <strong>${esc(u.username)}</strong>
    ${isMe ? '<span class="cp-badge cp-badge-admin" style="margin-left:6px;font-size:9px">você</span>' : ''}
  </td>
  <td class="cp-text-dim">${esc(u.email)}</td>
  <td>
    <span class="${badgeCls(u.role)}" id="cp-current-badge-${u.id}">
      ${ROLES[u.role]?.label ?? u.role}
    </span>
  </td>
  <td>
    <select class="cp-input" id="cp-select-${u.id}"
            style="padding:4px 8px;width:auto;font-size:12px"
            onchange="cpMarkPending('${u.id}', '${u.role}', this.value)"
            ${dis}>
      ${Object.entries(ROLES).map(([val, {label}]) =>
            `<option value="${val}"${selectVal === val ? ' selected' : ''}>${label}</option>`
        ).join('')}
    </select>
  </td>
  <td class="cp-text-muted" style="font-size:12px">${fmtDate(u.created_at)}</td>
</tr>`;
    }).join('')}
  </tbody>
</table>
</div>`;
}

// ── Pagination ────────────────────────────────────────────────────────────────

function renderPagination() {
    const el = document.getElementById('cp-pagination');
    if (!el) return;
    if (_totalPages <= 1) { el.innerHTML = ''; return; }

    el.innerHTML = `
<div class="cp-pager">
  <button class="cp-btn cp-btn-ghost" style="padding:5px 12px"
          onclick="cpGoPage(${_currentPage - 1})"
          ${_currentPage <= 1 ? 'disabled' : ''}>
    <i class="fa-solid fa-chevron-left"></i>
  </button>
  <span class="cp-pager-label">Página ${_currentPage} de ${_totalPages}</span>
  <button class="cp-btn cp-btn-ghost" style="padding:5px 12px"
          onclick="cpGoPage(${_currentPage + 1})"
          ${_currentPage >= _totalPages ? 'disabled' : ''}>
    <i class="fa-solid fa-chevron-right"></i>
  </button>
</div>`;
}

window.cpGoPage = function(page) {
    if (page < 1 || page > _totalPages) return;
    _currentPage = page;
    fetchAndRender();
};

// ── Search ────────────────────────────────────────────────────────────────────

let _searchTimer = null;
window.cpUserSearchInput = function(val) {
    clearTimeout(_searchTimer);
    _searchTimer = setTimeout(() => {
        _currentQuery = val.trim();
        _currentPage  = 1;
        fetchAndRender();
    }, 300);
};

// ── Pending tracking ──────────────────────────────────────────────────────────

window.cpMarkPending = function(userID, originalRole, newRole) {
    if (originalRole === newRole) {
        _pending.delete(userID);
    } else {
        _pending.set(userID, newRole);
    }

    const row = document.getElementById(`cp-row-${userID}`);
    if (row) row.className = _pending.has(userID) ? 'cp-row-pending' : '';

    syncBottomBar();
};

window.cpCancelPending = function() {
    // Revert all selects to their original value.
    _pending.forEach((newRole, userID) => {
        const sel = document.getElementById(`cp-select-${userID}`);
        // Find original role from the badge text — easier to re-fetch.
        // Just reload the page cleanly.
    });
    _pending.clear();
    syncBottomBar();
    fetchAndRender(); // Reload to reset all dropdowns cleanly.
};

function syncBottomBar() {
    const bar   = document.getElementById('cp-bottom-bar');
    const label = document.getElementById('cp-pending-label');
    if (!bar || !label) return;
    const n = _pending.size;
    bar.style.display = n > 0 ? 'flex' : 'none';
    label.textContent = `${n} alteração${n !== 1 ? 'ões' : ''} pendente${n !== 1 ? 's' : ''}`;
}

// ── OTP modal ─────────────────────────────────────────────────────────────────

function otpModalHTML() {
    return `
<div id="cp-otp-modal" class="cp-modal-backdrop" style="display:none">
  <div class="cp-modal">
    <h3><i class="fa-solid fa-envelope-open-text"></i> Confirmar alteração de role</h3>
    <p>Um código de confirmação foi enviado para o seu e-mail cadastrado.<br>
       Ele é válido por 15 minutos.</p>
    <div class="cp-field" style="margin-top:16px">
      <label class="cp-label">Código de confirmação</label>
      <input class="cp-input" id="cp-otp-input" type="text"
             inputmode="numeric" maxlength="6" placeholder="000000"
             style="font-size:22px;letter-spacing:8px;text-align:center"
             oninput="cpOTPInput(this.value)">
    </div>
    <div id="cp-otp-alert" class="cp-alert cp-alert-danger" style="margin-top:12px"></div>
    <div style="display:flex;gap:8px;margin-top:16px">
      <button class="cp-btn cp-btn-ghost" onclick="cpOTPCancel()">Cancelar</button>
      <button class="cp-btn cp-btn-primary" id="cp-otp-confirm-btn"
              onclick="cpOTPConfirm()" disabled>
        <i class="fa-solid fa-check"></i> Confirmar
      </button>
    </div>
  </div>
</div>`;
}

// ── Apply flow ────────────────────────────────────────────────────────────────

window.cpApplyChanges = async function() {
    if (_pending.size === 0) return;

    // Request OTP — server emails the code to the admin.
    const r = await cpApi('POST', '/api/control/v1/users/role-otp');
    if (r?.metadata?.status !== 200) {
        cpToast('danger', r?.metadata?.error || 'Erro ao solicitar código de confirmação.');
        return;
    }

    showOTPModal();
};

function showOTPModal() {
    const modal = document.getElementById('cp-otp-modal');
    const input = document.getElementById('cp-otp-input');
    const alert = document.getElementById('cp-otp-alert');
    const btn   = document.getElementById('cp-otp-confirm-btn');
    if (!modal) return;
    if (input)  input.value = '';
    if (alert)  alert.classList.remove('show');
    if (btn)    btn.disabled = true;
    modal.style.display = 'flex';
    setTimeout(() => input?.focus(), 80);
}

function hideOTPModal() {
    const modal = document.getElementById('cp-otp-modal');
    if (modal) modal.style.display = 'none';
}

window.cpOTPInput = function(val) {
    const btn = document.getElementById('cp-otp-confirm-btn');
    if (btn) btn.disabled = val.replace(/\D/g, '').length !== 6;
};

window.cpOTPCancel = hideOTPModal;

window.cpOTPConfirm = async function() {
    const input = document.getElementById('cp-otp-input');
    const alert = document.getElementById('cp-otp-alert');
    const btn   = document.getElementById('cp-otp-confirm-btn');
    const code  = input?.value?.trim() ?? '';

    if (code.length !== 6) return;

    btn.disabled = true;
    btn.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Confirmando…';

    // Build the batch payload.
    const changes = [];
    _pending.forEach((role, user_id) => changes.push({ user_id, role }));

    const r = await cpApi('PUT', '/api/control/v1/users/roles', {
        changes,
        otp_code: code,
    });

    if (r?.metadata?.status !== 200) {
        // Show error inside the modal — keep it open so admin can retry
        // with a fresh OTP if needed.
        if (alert) {
            alert.textContent = r?.metadata?.error || 'Código inválido ou expirado.';
            alert.classList.add('show');
        }
        btn.disabled = false;
        btn.innerHTML = '<i class="fa-solid fa-check"></i> Confirmar';
        return;
    }

    hideOTPModal();
    const n = r.data?.applied?.length ?? 0;
    cpToast('success', `${n} role${n !== 1 ? 's alterados' : ' alterado'} com sucesso.`);
    _pending.clear();
    registerLeaveGuard(null); // changes applied — no guard needed
    syncBottomBar();
    fetchAndRender();
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function badgeCls(role) {
    return ROLES[role]?.badge ?? 'cp-badge-user';
}

function fmtDate(iso) {
    if (!iso) return '—';
    return new Date(iso).toLocaleDateString('pt-BR');
}

function esc(s) {
    return String(s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;')
        .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function setBreadcrumb(label) {
    const el = document.getElementById('cp-breadcrumb');
    if (el) el.innerHTML = `<strong>${label}</strong>`;
}
