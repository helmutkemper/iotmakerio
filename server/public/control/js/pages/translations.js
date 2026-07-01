// server/public/control/js/pages/translations.js — Translation admin screen.
//
// Scope
// -----
//
// UI for the control-panel admin to review and edit every translation bundle.
// Replaces the retired /admin/i18n standalone page. All writes go through
// /api/control/v1/translations/* and require a per-save OTP confirmation.
//
// User flow
// ---------
//
//  1. Page boots → GET /api/v1/translations to enumerate locales.
//  2. For each locale → GET /api/v1/translations/:locale to load its bundle.
//  3. Admin picks a locale tab, edits strings inline, or imports a JSON file.
//  4. "Salvar" → POST /api/control/v1/translations/otp → backend emails OTP.
//  5. Modal asks for the 6-digit code.
//  6. PUT /api/control/v1/translations/:locale { messages, otp_code }.
//     Success → reload the locale bundle; failure → modal shows the reason.
//
// Pending edits are tracked locally per-locale. A navigation guard blocks
// leaving the page while any locale has unsaved changes, exactly like the
// users.js guard pattern.
//
// Conventions reused from the rest of the SPA
// -------------------------------------------
//   - cpApi()          — bearer-token HTTP client.
//   - cpToast()        — ephemeral notifications.
//   - cpConfirm()      — custom confirm modal (replaces window.confirm).
//   - registerLeaveGuard() — blocks sidebar navigation when dirty.
//   - OTP modal shape follows categories.js (small, centred, numeric input).

import { cpApi }              from '../api.js';
import { cpToast, cpConfirm } from '../toast.js';
import { registerLeaveGuard } from '../main.js';

// ─── Endpoints ───────────────────────────────────────────────────────────────

// Read endpoints are public — we call them even from the admin panel because
// the response shape (TrBundle) is identical and we get HTTP caching for free.
const PUBLIC_API  = '/api/v1/translations';
const CONTROL_API = '/api/control/v1/translations';

// ─── State ───────────────────────────────────────────────────────────────────

// _locales is populated from GET /api/v1/translations. Order is alphabetical
// because the server sorts it that way, which matches the tab order the admin
// expects.
let _locales = [];

// _state[locale] = { messages: TrMessage[], dirty: boolean, bundle: TrBundle? }.
// "messages" is the working copy the admin edits in place; "bundle" is the
// last-loaded server snapshot kept for display of bundleId / updatedAt.
const _state = {};

// _activeLocale is which tab is currently rendered. null until the first load.
let _activeLocale = null;

// _otpCallback is the function the OTP modal calls when the admin submits.
// Set by showOTPModal and cleared by hideOTPModal.
let _otpCallback = null;

// ─── Entry point ─────────────────────────────────────────────────────────────

export async function renderTranslations(root) {
    setBreadcrumb('Traduções');

    // Render the page skeleton immediately so the spinner appears before
    // the network round-trips finish.
    root.innerHTML = `
${otpModalHTML()}

<div class="cp-page-title">
  <i class="fa-solid fa-language"></i> Traduções
</div>

<div class="cp-card">
  <div class="cp-card-header" id="cp-tr-tabs">
    <div class="cp-spinner"></div>
  </div>
  <div class="cp-card-body" id="cp-tr-body" style="padding:0">
    <div class="cp-spinner"></div>
  </div>
</div>`;

    // Install the leave guard: a navigation attempt while any locale is
    // dirty triggers a confirmation; discarding clears all pending edits.
    registerLeaveGuard((dest) => {
        if (!anyDirty()) return true;
        cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Alterações pendentes',
            'Você tem traduções editadas que ainda não foram salvas.<br>Deseja sair mesmo assim?',
            'Sair sem salvar',
            'Voltar',
        ).then((ok) => {
            if (ok) {
                Object.keys(_state).forEach(l => { _state[l].dirty = false; });
                registerLeaveGuard(null);
                window.cpNav(dest);
            }
        });
        return false;
    });

    await loadAllLocales();
}

// ─── Data loading ────────────────────────────────────────────────────────────

// loadAllLocales fetches the list of registered locales then downloads each
// bundle in parallel. Parallel fetch is safe because the endpoints are
// cacheable and the bundles are independent.
async function loadAllLocales() {
    const listR = await cpApi('GET', PUBLIC_API);
    if (listR?.metadata?.status !== 200) {
        showError(listR?.metadata?.error || 'Erro ao carregar lista de idiomas.');
        return;
    }

    _locales = listR.data?.locales ?? [];
    if (_locales.length === 0) {
        showError('Nenhum idioma registrado.');
        return;
    }

    // Load every bundle in parallel. Failures for one locale leave that tab
    // with an empty message list and a visible error — other locales still
    // render normally.
    await Promise.all(_locales.map(loadLocale));

    // Pick the first locale as the initial tab if nothing is active yet.
    if (!_activeLocale || !_locales.includes(_activeLocale)) {
        _activeLocale = _locales[0];
    }

    renderTabs();
    renderBundle(_activeLocale);
}

async function loadLocale(locale) {
    const r = await cpApi('GET', `${PUBLIC_API}/${encodeURIComponent(locale)}`);
    if (r?.metadata?.status === 200 && r.data) {
        _state[locale] = {
            bundle:   r.data,
            messages: (r.data.messages || []).map(m => ({ ...m })),
            dirty:    false,
        };
    } else {
        _state[locale] = { bundle: null, messages: [], dirty: false };
    }
}

// ─── Tabs ────────────────────────────────────────────────────────────────────

function renderTabs() {
    const el = document.getElementById('cp-tr-tabs');
    if (!el) return;

    const tabs = _locales.map(loc => {
        const s       = _state[loc] || { messages: [], dirty: false };
        const missing = s.messages.filter(m => isMissing(m)).length;
        const cls     = loc === _activeLocale ? 'cp-tr-tab active' : 'cp-tr-tab';
        const dirtyFlag = s.dirty ? '<span class="cp-tr-dirty" title="Alterações não salvas">•</span>' : '';
        const missingBadge = missing > 0
            ? `<span class="cp-tr-badge-missing">${missing}</span>`
            : '';
        return `
<button class="${cls}" onclick="cpTrSelectTab('${esc(loc)}')">
  ${esc(loc)} ${dirtyFlag} ${missingBadge}
</button>`;
    }).join('');

    el.innerHTML = `
<div class="cp-tr-tab-row">${tabs}</div>
<div class="cp-tr-toolbar">
  <button class="cp-btn cp-btn-ghost" onclick="cpTrExport()"
          title="Baixar todas as traduções como JSON">
    <i class="fa-solid fa-download"></i> Exportar JSON
  </button>
  <button class="cp-btn cp-btn-ghost" onclick="cpTrImportClick()"
          title="Carregar JSON e substituir o locale atual">
    <i class="fa-solid fa-upload"></i> Importar JSON
  </button>
  <input type="file" id="cp-tr-file" accept=".json" style="display:none"
         onchange="cpTrImportFile(this)">
</div>`;
}

// cpTrSelectTab swaps the active locale. Dirty state is preserved per-locale,
// so the admin can edit several locales before saving any of them.
window.cpTrSelectTab = function(locale) {
    _activeLocale = locale;
    renderTabs();
    renderBundle(locale);
};

// ─── Bundle table ────────────────────────────────────────────────────────────

function renderBundle(locale) {
    const body = document.getElementById('cp-tr-body');
    if (!body) return;

    const s = _state[locale];
    if (!s) {
        body.innerHTML = `<div class="cp-empty">
          <i class="fa-solid fa-triangle-exclamation"></i>
          Locale ${esc(locale)} não carregado.
        </div>`;
        return;
    }

    const total   = s.messages.filter(m => m.id?.trim()).length;
    const missing = s.messages.filter(m => m.id?.trim() && isMissing(m)).length;

    const header = `
<div class="cp-tr-header">
  <div>
    <span class="cp-tr-locale-label">${esc(locale)}</span>
    ${s.bundle?.updatedAt ? `<span class="cp-text-muted" style="font-size:12px;margin-left:8px">
      atualizado em ${new Date(s.bundle.updatedAt).toLocaleString('pt-BR')}
    </span>` : ''}
  </div>
  <div class="cp-tr-counters">
    <span class="cp-tr-count">${total} chaves</span>
    ${missing > 0
        ? `<span class="cp-tr-count cp-tr-count-missing">${missing} faltando</span>`
        : `<span class="cp-tr-count cp-tr-count-ok">100%</span>`}
  </div>
</div>

<div class="cp-tr-actions">
  <button class="cp-btn cp-btn-primary" onclick="cpTrSave('${esc(locale)}')"
          ${s.dirty ? '' : 'disabled'}>
    <i class="fa-solid fa-floppy-disk"></i> Salvar (${esc(locale)})
  </button>
  <button class="cp-btn cp-btn-ghost" onclick="cpTrAddRow('${esc(locale)}')">
    <i class="fa-solid fa-plus"></i> Adicionar
  </button>
  <button class="cp-btn cp-btn-ghost" onclick="cpTrReload('${esc(locale)}')">
    <i class="fa-solid fa-rotate"></i> Recarregar
  </button>
</div>`;

    if (!s.messages.length) {
        body.innerHTML = header + `<div class="cp-empty">
          <i class="fa-solid fa-folder-open"></i>
          Sem traduções. Clique em "Adicionar" para começar.
        </div>`;
        return;
    }

    const rows = s.messages.map((m, i) => {
        const missingCls = isMissing(m) ? 'cp-tr-row-missing' : '';
        return `
<tr class="${missingCls}">
  <td class="cp-tr-cell-idx">${i + 1}</td>
  <td>
    <input class="cp-input cp-tr-id" type="text"
           value="${esc(m.id || '')}"
           placeholder="messageId"
           oninput="cpTrUpdate('${esc(locale)}', ${i}, 'id', this.value)">
  </td>
  <td>
    <input class="cp-input" type="text"
           value="${esc(m.other || '')}"
           placeholder="Texto traduzido"
           oninput="cpTrUpdate('${esc(locale)}', ${i}, 'other', this.value)">
  </td>
  <td>
    <input class="cp-input" type="text"
           value="${esc(m.one || '')}"
           placeholder="Singular (opcional)"
           oninput="cpTrUpdate('${esc(locale)}', ${i}, 'one', this.value)">
  </td>
  <td>
    <input class="cp-input" type="text"
           value="${esc(m.description || '')}"
           placeholder="Descrição (opcional)"
           oninput="cpTrUpdate('${esc(locale)}', ${i}, 'description', this.value)">
  </td>
  <td class="cp-tr-cell-actions">
    <button class="cp-tr-remove" title="Remover"
            onclick="cpTrRemoveRow('${esc(locale)}', ${i})">
      <i class="fa-solid fa-xmark"></i>
    </button>
  </td>
</tr>`;
    }).join('');

    body.innerHTML = header + `
<div class="cp-tr-table-wrap">
<table class="cp-tr-table">
  <thead>
    <tr>
      <th style="width:48px">#</th>
      <th style="width:220px">ID</th>
      <th>Other (padrão)</th>
      <th style="width:180px">One (singular)</th>
      <th style="width:200px">Descrição</th>
      <th style="width:40px"></th>
    </tr>
  </thead>
  <tbody>${rows}</tbody>
</table>
</div>`;
}

// ─── Row editing ─────────────────────────────────────────────────────────────

// isMissing returns true when the row represents an untranslated or auto-
// reported key: empty "other", or the telemetry "*"-prefix from the IDE WASM
// InsertMissingMessage path.
function isMissing(m) {
    const v = (m?.other ?? '').trim();
    return v === '' || v.startsWith('*');
}

window.cpTrUpdate = function(locale, idx, field, value) {
    const s = _state[locale];
    if (!s || idx >= s.messages.length) return;
    s.messages[idx][field] = value;
    s.dirty = true;

    // The save button and tab counters depend on the dirty/missing state;
    // a light redraw of the headers is enough — we avoid full table redraw
    // so the input keeps focus and the caret position.
    refreshHeadersOnly(locale);
};

window.cpTrAddRow = function(locale) {
    const s = _state[locale];
    if (!s) return;
    s.messages.push({ id: '', other: '', one: '', description: '' });
    s.dirty = true;
    renderBundle(locale);
    renderTabs();

    // Focus the ID input of the row we just added.
    setTimeout(() => {
        const inputs = document.querySelectorAll('.cp-tr-table .cp-tr-id');
        if (inputs.length) inputs[inputs.length - 1].focus();
    }, 30);
};

window.cpTrRemoveRow = async function(locale, idx) {
    const s = _state[locale];
    if (!s || idx >= s.messages.length) return;
    const m = s.messages[idx];

    // Only confirm when the row has real content — throwaway empty rows
    // added by mistake should vanish silently.
    if ((m.id || '').trim() || (m.other || '').trim()) {
        const ok = await cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Remover linha',
            `Remover <strong>${esc(m.id || '(sem id)')}</strong> do locale ${esc(locale)}?<br>` +
            'A remoção só é persistida quando você clicar em Salvar.',
            'Remover',
            'Cancelar',
        );
        if (!ok) return;
    }

    s.messages.splice(idx, 1);
    s.dirty = true;
    renderBundle(locale);
    renderTabs();
};

window.cpTrReload = async function(locale) {
    const s = _state[locale];
    if (s?.dirty) {
        const ok = await cpConfirm(
            '<i class="fa-solid fa-triangle-exclamation"></i> Descartar edições',
            `Descartar as alterações não salvas em ${esc(locale)}?`,
            'Descartar',
            'Cancelar',
        );
        if (!ok) return;
    }
    await loadLocale(locale);
    renderBundle(locale);
    renderTabs();
};

// ─── Save flow (OTP-gated) ───────────────────────────────────────────────────

window.cpTrSave = async function(locale) {
    const s = _state[locale];
    if (!s || !s.dirty) return;

    // Sanity check on the client before round-tripping.
    const bad = s.messages.find(m => !(m.id || '').trim());
    if (bad) {
        cpToast('danger', 'Existem linhas sem ID. Preencha ou remova antes de salvar.');
        return;
    }
    const dupId = findDuplicateID(s.messages);
    if (dupId) {
        cpToast('danger', `ID duplicado: ${dupId}. Cada chave deve ser única por locale.`);
        return;
    }

    // Request an OTP for this locale. The backend uses the `locale` query
    // param only to label the confirmation email ("confirm pt-BR save") —
    // the eventual PUT carries the locale in the URL path as the real
    // source of truth.
    const otpR = await cpApi('POST',
        `${CONTROL_API}/otp?locale=${encodeURIComponent(locale)}`);
    if (otpR?.metadata?.status !== 200) {
        cpToast('danger', otpR?.metadata?.error || 'Erro ao solicitar código de confirmação.');
        return;
    }

    showOTPModal(async (code) => {
        // Build the payload. Trim messages with no useful content so the
        // server doesn't store empty rows, and strip the "*" prefix so the
        // admin doesn't accidentally persist the telemetry marker.
        const messages = s.messages
            .filter(m => (m.id || '').trim())
            .map(m => ({
                id:          m.id.trim(),
                other:       stripStar(m.other || ''),
                one:         (m.one || '').trim(),
                description: (m.description || '').trim(),
            }));

        const r = await cpApi('PUT', `${CONTROL_API}/${encodeURIComponent(locale)}`, {
            messages,
            otp_code: code,
        });

        // The modal callback contract:
        //   return true  → close the modal and run onSuccess side-effects here
        //   return str   → stay open, display the string as the error message
        const status = r?.metadata?.status;
        if (status === 200) {
            cpToast('success', `Traduções de ${locale} salvas.`);
            s.dirty = false;
            await loadLocale(locale);
            renderBundle(locale);
            renderTabs();
            return true;
        }
        if (status === 401) {
            return r?.metadata?.error || 'Código inválido ou expirado.';
        }
        // Any other status is a server error — close the modal and toast.
        cpToast('danger', r?.metadata?.error || 'Erro ao salvar traduções.');
        return true;
    });
};

// ─── Import / export ─────────────────────────────────────────────────────────

// cpTrExport bundles every locale (in memory) into the same JSON shape used
// by the old /admin/i18n page, so existing backup files keep working.
window.cpTrExport = function() {
    const out = {};
    for (const loc of _locales) {
        const s = _state[loc];
        if (!s) continue;
        out[loc] = {
            messages: s.messages
                .filter(m => (m.id || '').trim())
                .map(m => {
                    const e = { id: m.id.trim(), other: stripStar(m.other || '') };
                    if ((m.one || '').trim())         e.one         = m.one.trim();
                    if ((m.description || '').trim()) e.description = m.description.trim();
                    return e;
                }),
        };
    }
    const blob = new Blob([JSON.stringify(out, null, 2)], { type: 'application/json' });
    const url  = URL.createObjectURL(blob);
    const a    = Object.assign(document.createElement('a'), {
        href: url,
        download: 'translations_' + new Date().toISOString().slice(0, 10) + '.json',
    });
    a.click();
    URL.revokeObjectURL(url);
    cpToast('success', 'Download iniciado.');
};

window.cpTrImportClick = function() {
    document.getElementById('cp-tr-file')?.click();
};

window.cpTrImportFile = function(input) {
    const file = input.files?.[0];
    if (!file) return;
    input.value = ''; // allow re-import of the same file later

    const reader = new FileReader();
    reader.onerror = () => cpToast('danger', 'Falha ao ler o arquivo.');
    reader.onload  = (ev) => {
        try {
            const data = JSON.parse(String(ev.target?.result || ''));
            applyImport(data);
        } catch (err) {
            cpToast('danger', 'JSON inválido: ' + (err?.message || err));
        }
    };
    reader.readAsText(file);
};

// applyImport merges an exported JSON into the local edit state. It does NOT
// call the server — the admin still has to press "Salvar" (with OTP) for each
// affected locale. That keeps the OTP gate meaningful for imports too.
function applyImport(data) {
    if (!data || typeof data !== 'object') {
        cpToast('danger', 'Formato inesperado: objeto raiz ausente.');
        return;
    }
    let touched = 0;
    for (const loc of Object.keys(data)) {
        if (!_locales.includes(loc)) continue;
        const msgs = data[loc]?.messages;
        if (!Array.isArray(msgs)) continue;
        _state[loc].messages = msgs.map(m => ({
            id:          String(m.id || ''),
            other:       String(m.other || ''),
            one:         String(m.one || ''),
            description: String(m.description || ''),
        }));
        _state[loc].dirty = true;
        touched++;
    }
    if (touched === 0) {
        cpToast('warning', 'Nenhum locale reconhecido no arquivo.');
        return;
    }
    cpToast('success', `Importadas alterações em ${touched} locale(s). Salve para persistir.`);
    renderTabs();
    if (_activeLocale) renderBundle(_activeLocale);
}

// ─── OTP modal ───────────────────────────────────────────────────────────────

// Reused verbatim-ish from categories.js — keeping it local so this page is
// self-contained and the categories-specific window bindings (cpCatOtp*) do
// not collide with ours.
function otpModalHTML() {
    return `
<div id="cp-tr-otp-modal" class="cp-modal-backdrop" style="display:none">
  <div class="cp-modal" style="max-width:420px">
    <h3><i class="fa-solid fa-envelope-open-text"></i> Confirmar salvamento</h3>
    <p style="margin-top:8px;font-size:13px;color:var(--text-dim)">
      Enviamos um código de 6 dígitos para o seu e-mail.<br>
      Ele é válido por 15 minutos.
    </p>
    <div class="cp-field" style="margin-top:16px">
      <label class="cp-label">Código de confirmação</label>
      <input class="cp-input" id="cp-tr-otp-input" type="text"
             inputmode="numeric" maxlength="6" placeholder="000000"
             style="font-size:22px;letter-spacing:8px;text-align:center"
             oninput="cpTrOtpInput()">
    </div>
    <div id="cp-tr-otp-error" class="cp-alert cp-alert-danger"
         style="margin-top:12px;display:none"></div>
    <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
      <button class="cp-btn cp-btn-ghost" onclick="cpTrOtpCancel()">Cancelar</button>
      <button class="cp-btn cp-btn-primary" id="cp-tr-otp-confirm"
              onclick="cpTrOtpConfirm()" disabled>
        <i class="fa-solid fa-check"></i> Confirmar
      </button>
    </div>
  </div>
</div>`;
}

function showOTPModal(cb) {
    _otpCallback = cb;
    const m = document.getElementById('cp-tr-otp-modal');
    const i = document.getElementById('cp-tr-otp-input');
    const e = document.getElementById('cp-tr-otp-error');
    const b = document.getElementById('cp-tr-otp-confirm');
    if (!m) return;
    if (i) i.value = '';
    if (e) { e.style.display = 'none'; e.textContent = ''; }
    if (b) {
        b.disabled = true;
        b.innerHTML = '<i class="fa-solid fa-check"></i> Confirmar';
    }
    m.style.display = 'flex';
    setTimeout(() => i?.focus(), 80);
}

function hideOTPModal() {
    _otpCallback = null;
    const m = document.getElementById('cp-tr-otp-modal');
    if (m) m.style.display = 'none';
}

window.cpTrOtpInput = function() {
    const v = document.getElementById('cp-tr-otp-input')?.value || '';
    const b = document.getElementById('cp-tr-otp-confirm');
    if (b) b.disabled = v.replace(/\D/g, '').length !== 6;
    const e = document.getElementById('cp-tr-otp-error');
    if (e) e.style.display = 'none';
};

window.cpTrOtpCancel = hideOTPModal;

window.cpTrOtpConfirm = async function() {
    if (!_otpCallback) return;

    const input = document.getElementById('cp-tr-otp-input');
    const btn   = document.getElementById('cp-tr-otp-confirm');
    const errEl = document.getElementById('cp-tr-otp-error');
    const code  = (input?.value || '').trim();
    if (code.length !== 6) return;

    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Confirmando…'; }

    const result = await _otpCallback(code);

    if (result === true) {
        hideOTPModal();
        return;
    }
    if (typeof result === 'string') {
        if (errEl) { errEl.textContent = result; errEl.style.display = 'block'; }
        if (btn)   { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-check"></i> Confirmar'; }
        return;
    }
    // Safety net — any other return value means the callback already handled
    // the UX; just close the modal.
    hideOTPModal();
};

// ─── Helpers ─────────────────────────────────────────────────────────────────

// anyDirty is used by the leave guard to decide whether to prompt.
function anyDirty() {
    return Object.values(_state).some(s => s.dirty);
}

// findDuplicateID returns the first duplicate ID found, or '' when the list
// is unique. Linear scan is fine — realistic bundle sizes stay under ~200.
function findDuplicateID(messages) {
    const seen = new Set();
    for (const m of messages) {
        const id = (m.id || '').trim();
        if (!id) continue;
        if (seen.has(id)) return id;
        seen.add(id);
    }
    return '';
}

// stripStar removes the "*" prefix used by the missing-key telemetry so that
// when the admin saves, the persisted value is the one they actually wrote.
function stripStar(s) {
    return String(s).startsWith('*') ? String(s).slice(1).trim() : String(s).trim();
}

// refreshHeadersOnly updates only the counters and tab labels without
// redrawing the message rows. Keeps focus on the input the admin is typing in.
function refreshHeadersOnly(locale) {
    renderTabs();
    // Re-render the action toolbar so the Save button flips between enabled
    // and disabled in response to dirty-state flips.
    const body = document.getElementById('cp-tr-body');
    if (!body) return;
    const actions = body.querySelector('.cp-tr-actions');
    const s = _state[locale];
    if (!actions || !s) return;

    const saveBtn = actions.querySelector('.cp-btn-primary');
    if (saveBtn) saveBtn.disabled = !s.dirty;
}

function showError(msg) {
    const body = document.getElementById('cp-tr-body');
    if (body) body.innerHTML = `<div class="cp-empty">
      <i class="fa-solid fa-triangle-exclamation"></i>${esc(msg)}
    </div>`;
}

function setBreadcrumb(label) {
    const el = document.getElementById('cp-breadcrumb');
    if (el) el.innerHTML = `<strong>${esc(label)}</strong>`;
}

function esc(s) {
    return String(s ?? '')
        .replace(/&/g, '&amp;').replace(/</g, '&lt;')
        .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}
