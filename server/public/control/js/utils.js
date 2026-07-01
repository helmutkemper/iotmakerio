// utils.js — pure helpers, UI micro-helpers
import { S } from './state.js';

// ─── i18n auto-report of missing keys ────────────────────────────────────────
//
// Mirror of server/translate/missing.go (the WASM IDE's behaviour).
//
// When t() falls back because a translation key is absent from S.tr, this
// module fires a POST /api/v1/translations/{locale}/missing for EVERY
// configured locale. The server inserts each pair (locale, id) with a
// leading "*" on the value so the admin can spot the entry as "auto-
// captured, needs translation" in the i18n page.
//
// Contract — must match the WASM side exactly:
//
//   - Endpoint:      POST /api/v1/translations/{locale}/missing
//   - Body:          {"id":"<key>","other":"<fallback>"}
//   - Locales:       both pt-BR and en-US (hardcoded — see Locales in
//                    server/translate/missing.go for the same TODO about
//                    sourcing this list from the database).
//   - Dedup:         the same key is reported at most once per session
//                    via the in-memory _reportedTrKeys Set.
//   - Server-side:   INSERT OR IGNORE — already-translated keys are
//                    never overwritten; the "*" prefix only appears on
//                    first capture.
//   - Side-effects:  fire-and-forget. Errors are console.warn'd only;
//                    the caller never blocks on the network round-trip.
//
// This invariant is documented in INVARIANTS.md §3 — do not silently
// remove the auto-report. If the missing-key telemetry breaks, new UI
// strings stop reaching the admin and the i18n table rots.
//
// Português:
//
//	Espelho do server/translate/missing.go (comportamento do WASM IDE).
//	Quando t() não acha a chave em S.tr, dispara POST para os dois locales
//	configurados. O servidor grava com prefixo "*" para o admin notar.
//	Dedup por sessão, fire-and-forget, sem bloquear UI.
const _trLocales = ['pt-BR', 'en-US'];
const _reportedTrKeys = new Set();

function _reportMissingTr(id, fallback) {
    if (_reportedTrKeys.has(id)) return;
    _reportedTrKeys.add(id);

    const body = JSON.stringify({ id, other: fallback });
    for (const locale of _trLocales) {
        fetch(`/api/v1/translations/${locale}/missing`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body,
        }).catch(err => console.warn(`[i18n] report ${id}/${locale}:`, err));
    }
}

/** i18n lookup with auto-report on miss — see _reportMissingTr above.
 *
 *  Three branches:
 *    - key present and non-empty   → return the translation
 *    - key present but empty       → return fallback, do NOT report
 *                                    (admin deliberately blanked it)
 *    - key missing entirely        → return fallback AND report
 *
 *  Matches translate.T() in the WASM IDE: ReportMissing fires only on
 *  Localizer.Localize errors, which means "key not in bundle", not
 *  "value is empty string". Without this distinction every deliberately
 *  blanked translation would re-appear with a "*" on next page load.
 */
export const t = (k, fb) => {
    if (k in S.tr) {
        const hit = S.tr[k];
        return hit !== '' ? hit : (fb || k);
    }
    _reportMissingTr(k, fb || k);
    return fb || k;
};

/** HTML-escape text content */
export const esc = s =>
    (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** Escape for attribute values */
export const ea = s =>
    (s || '').replace(/"/g, '&quot;').replace(/</g, '&lt;');

/** Truncate plain-text excerpt */
export const exc = (s, n) => {
    const p = s.replace(/[#*`\[\]!()>_\-]/g, '').replace(/\s+/g, ' ').trim();
    return p.length > n ? p.slice(0, n) + '…' : p;
};

/** Format ISO date to locale string */
export const fmtD = iso =>
    new Date(iso).toLocaleDateString(S.locale, { day: 'numeric', month: 'long', year: 'numeric' });

/**
 * Show an inline alert inside the element with given id.
 * type: 'success' | 'danger' | 'info' | 'warning'
 */
export function alert2(id, type, msg) {
    const el = document.getElementById(id);
    if (el) el.innerHTML = `<div class="alert alert-${type}">${esc(msg)}</div>`;
}

/**
 * Toggle loading state on a button.
 * Saves original innerHTML and shows a spinner while loading.
 */
export function ld(id, on) {
    const el = document.getElementById(id);
    if (!el) return;
    el.disabled = on;
    if (on) {
        el._html = el.innerHTML;
        el.innerHTML = '<div class="spinner" style="width:16px;height:16px;border-width:2px"></div>';
    } else if (el._html) {
        el.innerHTML = el._html;
    }
}

// ── Modal dialogs — replace all native confirm() / alert() / prompt() ─────────
//
// These functions inject a modal into the DOM and return a Promise, so they
// work naturally with async/await without blocking the JS event loop.
//
// Usage:
//   if (await showConfirm('Delete this item?')) { ... }
//   await showAlert('danger', 'Something went wrong.');
//   const name = await showPrompt('File name (.go):', 'main.go');

let _modalEl = null;

function _injectModal(html) {
    _removeModal();
    const wrap = document.createElement('div');
    wrap.id = '_modal_wrap';
    wrap.style.cssText = [
        'position:fixed', 'inset:0', 'background:rgba(0,0,0,.45)',
        'z-index:9000', 'display:flex', 'align-items:center',
        'justify-content:center', 'padding:16px',
    ].join(';');
    wrap.innerHTML = html;
    document.body.appendChild(wrap);
    _modalEl = wrap;
    // Focus the first focusable element inside the modal.
    setTimeout(() => {
        const f = wrap.querySelector('button,input,[tabindex]');
        if (f) f.focus();
    }, 40);
    return wrap;
}

function _removeModal() {
    const el = document.getElementById('_modal_wrap');
    if (el) el.remove();
    _modalEl = null;
}

/**
 * Async confirm dialog — replaces native confirm().
 * Resolves true (OK) or false (Cancel).
 */
export function showConfirm(message, { okLabel = 'Confirmar', cancelLabel = 'Cancelar', danger = false } = {}) {
    return new Promise(resolve => {
        const okClass = danger ? 'btn btn-danger' : 'btn btn-primary';
        _injectModal(`
<div style="background:var(--bg-card,#fff);border:1px solid var(--border,#ddd);
            border-radius:8px;padding:24px;max-width:400px;width:100%">
  <p style="margin-bottom:20px;font-size:15px;line-height:1.5">${esc(message)}</p>
  <div style="display:flex;justify-content:flex-end;gap:8px">
    <button class="btn btn-ghost" id="_modal_cancel">${esc(cancelLabel)}</button>
    <button class="${okClass}" id="_modal_ok">${esc(okLabel)}</button>
  </div>
</div>`);
        document.getElementById('_modal_ok').onclick     = () => { _removeModal(); resolve(true);  };
        document.getElementById('_modal_cancel').onclick = () => { _removeModal(); resolve(false); };
    });
}

/**
 * Async alert dialog — replaces native alert().
 * type: 'danger' | 'warning' | 'success' | 'info'
 */
export function showAlert(type, message, { label = 'OK' } = {}) {
    return new Promise(resolve => {
        const colors = {
            danger:  { bg: 'var(--danger-bg,#fdecea)',   border: 'var(--danger,#c0392b)',   text: 'var(--danger,#c0392b)' },
            warning: { bg: 'var(--warning-bg,#fef3cd)',  border: 'var(--warning,#b7770d)',  text: 'var(--warning,#b7770d)' },
            success: { bg: 'var(--success-bg,#e6f4ed)',  border: 'var(--success,#2e7d52)',  text: 'var(--success,#2e7d52)' },
            info:    { bg: 'var(--info-bg,#e3f0ff)',     border: 'var(--info,#1565c0)',     text: 'var(--info,#1565c0)' },
        }[type] || {};
        _injectModal(`
<div style="background:var(--bg-card,#fff);border:1px solid var(--border,#ddd);
            border-radius:8px;padding:24px;max-width:400px;width:100%">
  <div style="background:${colors.bg};border:1px solid ${colors.border};color:${colors.text};
              border-radius:6px;padding:12px 16px;margin-bottom:20px;font-size:14px">
    ${esc(message)}
  </div>
  <div style="display:flex;justify-content:flex-end">
    <button class="btn btn-primary" id="_modal_ok">${esc(label)}</button>
  </div>
</div>`);
        document.getElementById('_modal_ok').onclick = () => { _removeModal(); resolve(); };
    });
}

/**
 * Async prompt dialog — replaces native prompt().
 * Resolves with the entered string, or null if cancelled.
 */
export function showPrompt(message, defaultValue = '', { label = 'OK', cancelLabel = 'Cancelar', placeholder = '' } = {}) {
    return new Promise(resolve => {
        _injectModal(`
<div style="background:var(--bg-card,#fff);border:1px solid var(--border,#ddd);
            border-radius:8px;padding:24px;max-width:400px;width:100%">
  <label style="display:block;margin-bottom:8px;font-size:14px;font-weight:600">
    ${esc(message)}
  </label>
  <input id="_modal_input" class="input"
         style="width:100%;margin-bottom:20px"
         value="${ea(defaultValue)}"
         placeholder="${ea(placeholder)}">
  <div style="display:flex;justify-content:flex-end;gap:8px">
    <button class="btn btn-ghost"    id="_modal_cancel">${esc(cancelLabel)}</button>
    <button class="btn btn-primary"  id="_modal_ok">${esc(label)}</button>
  </div>
</div>`);
        const inp = document.getElementById('_modal_input');
        inp.select();
        const ok = () => {
            const v = inp.value;
            _removeModal();
            resolve(v);
        };
        inp.addEventListener('keydown', e => {
            if (e.key === 'Enter')  { e.preventDefault(); ok(); }
            if (e.key === 'Escape') { _removeModal(); resolve(null); }
        });
        document.getElementById('_modal_ok').onclick     = ok;
        document.getElementById('_modal_cancel').onclick = () => { _removeModal(); resolve(null); };
    });
}
