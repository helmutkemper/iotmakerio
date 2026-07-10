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

// ── Toast ────────────────────────────────────────────────────────────────────

const _TOAST_ICONS = {
    success: 'fa-circle-check',
    danger:  'fa-circle-xmark',
    warning: 'fa-triangle-exclamation',
    info:    'fa-circle-info',
};

function _getToastContainer() {
    let el = document.getElementById('_portal-toasts');
    if (!el) {
        el = document.createElement('div');
        el.id = '_portal-toasts';
        el.style.cssText = [
            'position:fixed', 'bottom:24px', 'right:24px', 'z-index:9999',
            'display:flex', 'flex-direction:column-reverse', 'gap:8px', 'pointer-events:none',
        ].join(';');
        document.body.appendChild(el);
    }
    return el;
}

/**
 * Show a floating toast notification.
 * Uses the portal light-theme CSS variables.
 *
 * @param {'success'|'danger'|'warning'|'info'} type
 * @param {string} message
 * @param {number} [duration=4000]
 */
export function toast(type, message, duration = 4000) {
    const container = _getToastContainer();

    const styles = {
        success: { bg: 'var(--success-bg)', border: 'var(--success)', text: 'var(--success)' },
        danger:  { bg: 'var(--danger-bg)',  border: 'var(--danger)',  text: 'var(--danger)'  },
        warning: { bg: 'var(--warning-bg)', border: 'var(--warning)', text: 'var(--warning)' },
        info:    { bg: 'var(--info-bg)',     border: 'var(--info)',    text: 'var(--info)'    },
    }[type] || { bg: '#fff', border: 'var(--border)', text: 'var(--text-primary)' };

    const el = document.createElement('div');
    el.style.cssText = [
        `background:${styles.bg}`, `border:1px solid ${styles.border}`, `color:${styles.text}`,
        'border-radius:var(--r)', 'padding:10px 14px', 'font-size:14px', 'font-family:var(--font)',
        'display:flex', 'align-items:center', 'gap:8px', 'pointer-events:all', 'cursor:pointer',
        'max-width:360px', 'box-shadow:var(--sh)',
        'opacity:0', 'transform:translateY(8px)',
        'transition:opacity .2s ease, transform .2s ease',
    ].join(';');
    // Persistent toasts (duration === 0) get a small dismiss icon so
    // users know they need to click to close. Non-persistent toasts
    // remain unchanged — the click-to-dismiss behavior is still there
    // but the visual stays clean for the common transient case.
    const dismissHint = duration === 0
        ? '<i class="fa-solid fa-xmark" style="margin-left:auto;opacity:.6;flex-shrink:0"></i>'
        : '';
    el.innerHTML = `<i class="fa-solid ${_TOAST_ICONS[type] || 'fa-circle-info'}" style="flex-shrink:0"></i><span>${message}</span>${dismissHint}`;
    el.addEventListener('click', () => _dismissToast(el));
    container.appendChild(el);

    requestAnimationFrame(() => requestAnimationFrame(() => {
        el.style.opacity = '1';
        el.style.transform = 'translateY(0)';
    }));

    // duration === 0 means "persistent" — the toast stays until the
    // user clicks it. Useful for errors that may scroll out of view
    // before the user has a chance to read them. The click handler
    // installed above dismisses cleanly in that case.
    if (duration > 0) {
        const timer = setTimeout(() => _dismissToast(el), duration);
        el._toastTimer = timer;
    }
}

function _dismissToast(el) {
    clearTimeout(el._toastTimer);
    el.style.opacity = '0';
    el.style.transform = 'translateY(8px)';
    setTimeout(() => el.remove(), 220);
}

// ── Confirm dialog ────────────────────────────────────────────────────────────

/**
 * Async confirmation dialog — replaces native confirm().
 *
 * @param {string} message
 * @param {string} [confirmLabel='Confirmar']
 * @param {string} [cancelLabel='Cancelar']
 * @returns {Promise<boolean>}
 */
export function showConfirm(message, confirmLabel = 'Confirmar', cancelLabel = 'Cancelar') {
    return new Promise((resolve) => {
        document.getElementById('_portal-confirm')?.remove();

        const backdrop = document.createElement('div');
        backdrop.id = '_portal-confirm';
        backdrop.style.cssText = [
            'position:fixed', 'inset:0', 'background:rgba(0,0,0,.45)',
            'z-index:10000', 'display:flex', 'align-items:center', 'justify-content:center',
        ].join(';');
        backdrop.innerHTML = `
<div style="background:var(--bg-card);border:1px solid var(--border);border-radius:var(--rl);
            padding:28px;max-width:400px;width:90%;box-shadow:var(--shh)">
  <p style="font-size:15px;color:var(--text-primary);line-height:1.6;white-space:pre-line">${esc(message)}</p>
  <div style="display:flex;gap:8px;margin-top:20px;justify-content:flex-end">
    <button id="_pc-cancel" style="padding:8px 16px;border:1px solid var(--border);
            border-radius:var(--r);background:none;font-family:var(--font);font-size:14px;
            cursor:pointer;color:var(--text-secondary)">${esc(cancelLabel)}</button>
    <button id="_pc-ok" style="padding:8px 16px;border:none;border-radius:var(--r);
            background:var(--danger);color:#fff;font-family:var(--font);font-size:14px;
            font-weight:600;cursor:pointer">${esc(confirmLabel)}</button>
  </div>
</div>`;

        document.body.appendChild(backdrop);

        function finish(v) { backdrop.remove(); resolve(v); }
        document.getElementById('_pc-cancel').addEventListener('click', () => finish(false));
        document.getElementById('_pc-ok').addEventListener('click',     () => finish(true));
        // Same phantom-cancel guard as showPrompt (see the note there):
        // the gesture must START on the backdrop too.
        let pressOnBackdrop = false;
        backdrop.addEventListener('mousedown', (e) => { pressOnBackdrop = (e.target === backdrop); });
        backdrop.addEventListener('click', (e) => {
            if (e.target === backdrop && pressOnBackdrop) finish(false);
        });
    });
}

// ── Unsaved-changes confirm (three-way) ─────────────────────────────────────

/**
 * Three-way modal for the "unsaved changes" decision: Save / Discard /
 * Cancel. Resolves to one of those exact strings.
 *
 * Use whenever the user is about to leave a buffer that has been
 * modified — closing an editor, switching versions, navigating away
 * from a page that owns a Monaco/textarea/form. The visual is shared
 * with the help_files file manager so the IDE has a single vocabulary
 * for "you have unsaved work".
 *
 * Behaviour:
 *   - Save (default — Enter / blue button): the caller persists the
 *     buffer (await its save routine) before continuing. We do not
 *     persist on the caller's behalf because every page knows its
 *     own save semantics; we only return the choice.
 *   - Discard (red button): the caller throws the buffer away and
 *     continues.
 *   - Cancel (Escape / X / backdrop click): the caller stays put.
 *
 * The dialog is intentionally NOT keyboard-bindable to Discard. A
 * destructive action should always require an explicit click,
 * mirroring the help_files modal.
 *
 * @param {string} message  Body text describing what will be lost.
 * @param {object} [opts]
 * @param {string} [opts.title='Unsaved changes']
 * @param {string} [opts.saveLabel='Save']
 * @param {string} [opts.discardLabel='Discard']
 * @param {string} [opts.cancelLabel='Cancel']
 * @returns {Promise<'save'|'discard'|'cancel'>}
 *
 * Português: modal de três vias para decidir o destino de um buffer
 * não-salvo. Resolve para uma das três strings; o caller faz o save
 * de fato (se a escolha for 'save') porque cada página tem sua
 * rotina de persistência própria.
 */
export function showUnsavedConfirm(message, opts = {}) {
    const title        = opts.title        || 'Unsaved changes';
    const saveLabel    = opts.saveLabel    || 'Save';
    const discardLabel = opts.discardLabel || 'Discard';
    const cancelLabel  = opts.cancelLabel  || 'Cancel';

    return new Promise((resolve) => {
        document.getElementById('_portal-unsaved')?.remove();

        const backdrop = document.createElement('div');
        backdrop.id = '_portal-unsaved';
        backdrop.style.cssText = [
            'position:fixed', 'inset:0', 'background:rgba(0,0,0,.45)',
            'z-index:10000', 'display:flex', 'align-items:center', 'justify-content:center',
        ].join(';');

        backdrop.innerHTML = `
<div style="background:var(--bg-card);border:1px solid var(--border);border-radius:var(--rl);
            padding:0;max-width:440px;width:90%;box-shadow:var(--shh);overflow:hidden">
  <div style="display:flex;align-items:center;gap:12px;padding:14px 20px;
              border-bottom:1px solid var(--border)">
    <i class="fa-solid fa-triangle-exclamation"
       style="color:var(--warning,#f59e0b);font-size:20px;flex-shrink:0"></i>
    <h3 style="margin:0;font-size:15px;font-weight:600;color:var(--text-primary);flex:1">
      ${esc(title)}
    </h3>
    <button type="button" data-cancel aria-label="Cancel"
            style="background:none;border:none;color:var(--text-muted);
                   font-size:18px;cursor:pointer;padding:4px 8px;border-radius:4px;
                   line-height:1">×</button>
  </div>
  <div style="padding:18px 20px;color:var(--text-secondary);font-size:14px;line-height:1.55">
    ${esc(message)}
  </div>
  <div style="padding:12px 20px 16px;display:flex;justify-content:flex-end;gap:8px;
              border-top:1px solid var(--border)">
    <button type="button" data-cancel
            style="padding:8px 16px;border:1px solid var(--border);
                   border-radius:var(--r);background:none;font-family:var(--font);
                   font-size:14px;cursor:pointer;color:var(--text-secondary)">
      ${esc(cancelLabel)}
    </button>
    <button type="button" data-discard
            style="padding:8px 16px;border:none;border-radius:var(--r);
                   background:var(--danger);color:#fff;font-family:var(--font);
                   font-size:14px;font-weight:600;cursor:pointer">
      ${esc(discardLabel)}
    </button>
    <button type="button" data-save
            style="padding:8px 16px;border:none;border-radius:var(--r);
                   background:var(--primary);color:#fff;font-family:var(--font);
                   font-size:14px;font-weight:600;cursor:pointer">
      ${esc(saveLabel)}
    </button>
  </div>
</div>`;

        document.body.appendChild(backdrop);

        const finish = (choice) => {
            backdrop.remove();
            document.removeEventListener('keydown', onKey);
            resolve(choice);
        };

        // Default focus on Save so Enter naturally lands on the safe
        // action. Tab still cycles through the buttons in visual order.
        const saveBtn = backdrop.querySelector('[data-save]');
        saveBtn.focus();

        saveBtn.onclick                                     = () => finish('save');
        backdrop.querySelector('[data-discard]').onclick    = () => finish('discard');
        backdrop.querySelectorAll('[data-cancel]').forEach(b => b.onclick = () => finish('cancel'));

        // Same phantom-cancel guard as showPrompt (see the note there).
        let pressOnBackdrop = false;
        backdrop.addEventListener('mousedown', (e) => { pressOnBackdrop = (e.target === backdrop); });
        backdrop.addEventListener('click', (e) => {
            if (e.target === backdrop && pressOnBackdrop) finish('cancel');
        });

        // Enter → Save (safe action). Escape → Cancel.
        // No shortcut for Discard — destructive actions require a click.
        const onKey = (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                finish('save');
            } else if (e.key === 'Escape') {
                e.preventDefault();
                finish('cancel');
            }
        };
        document.addEventListener('keydown', onKey);
    });
}



// ── Prompt dialog ─────────────────────────────────────────────────────────────

/**
 * Async text input dialog — replaces native prompt().
 *
 * @param {string} message
 * @param {string} [defaultValue='']
 * @returns {Promise<string|null>} — null if cancelled
 */
export function showPrompt(message, defaultValue = '') {
    return new Promise((resolve) => {
        document.getElementById('_portal-prompt')?.remove();

        const backdrop = document.createElement('div');
        backdrop.id = '_portal-prompt';
        backdrop.style.cssText = [
            'position:fixed', 'inset:0', 'background:rgba(0,0,0,.45)',
            'z-index:10000', 'display:flex', 'align-items:center', 'justify-content:center',
        ].join(';');
        backdrop.innerHTML = `
<div style="background:var(--bg-card);border:1px solid var(--border);border-radius:var(--rl);
            padding:28px;max-width:400px;width:90%;box-shadow:var(--shh)">
  <p style="font-size:15px;color:var(--text-primary);margin-bottom:12px;line-height:1.6">${esc(message)}</p>
  <input id="_pp-input" type="text" value="${ea(defaultValue)}"
         style="width:100%;padding:9px 12px;border:1px solid var(--border);
                border-radius:var(--r);font-family:var(--font);font-size:14px;
                background:var(--bg-input);color:var(--text-primary);outline:none;
                box-sizing:border-box">
  <div style="display:flex;gap:8px;margin-top:16px;justify-content:flex-end">
    <button id="_pp-cancel" style="padding:8px 16px;border:1px solid var(--border);
            border-radius:var(--r);background:none;font-family:var(--font);font-size:14px;
            cursor:pointer;color:var(--text-secondary)">Cancelar</button>
    <button id="_pp-ok" style="padding:8px 16px;border:none;border-radius:var(--r);
            background:var(--primary);color:#fff;font-family:var(--font);font-size:14px;
            font-weight:600;cursor:pointer">OK</button>
  </div>
</div>`;

        document.body.appendChild(backdrop);

        const input = document.getElementById('_pp-input');
        setTimeout(() => { input?.focus(); input?.select(); }, 60);

        function finish(v) { backdrop.remove(); resolve(v); }
        document.getElementById('_pp-cancel').addEventListener('click', () => finish(null));
        document.getElementById('_pp-ok').addEventListener('click', () => finish(input.value));
        input.addEventListener('keydown', (e) => {
            if (e.key === 'Enter')  finish(input.value);
            if (e.key === 'Escape') finish(null);
        });
        // Backdrop-cancel only when the WHOLE gesture happened on the
        // backdrop: a `click` fires on the common ancestor of mousedown
        // and mouseup, so starting a text selection inside the input and
        // releasing outside the dialog used to close it as a PHANTOM
        // cancel — the popup vanished with no warning (field report,
        // 2026-07-08, while typing templates/portal.html over the
        // preselected suggestion). Track where the press started.
        //
        // Português: Cancelar pelo backdrop só quando o gesto INTEIRO
        // aconteceu nele: `click` dispara no ancestral comum de
        // mousedown/mouseup — começar seleção no input e soltar fora
        // fechava o modal como cancelamento FANTASMA (relato de campo,
        // digitando um caminho sobre a sugestão pré-selecionada).
        let pressOnBackdrop = false;
        backdrop.addEventListener('mousedown', (e) => { pressOnBackdrop = (e.target === backdrop); });
        backdrop.addEventListener('click', (e) => {
            if (e.target === backdrop && pressOnBackdrop) finish(null);
        });
    });
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

// ── Modal dialog ─────────────────────────────────────────────────────────────

const _DIALOG_ICONS = {
    success: { icon: 'fa-circle-check',         color: 'var(--success, #22c55e)' },
    danger:  { icon: 'fa-circle-xmark',         color: 'var(--danger,  #ef4444)' },
    warning: { icon: 'fa-triangle-exclamation', color: 'var(--warning, #f59e0b)' },
    info:    { icon: 'fa-circle-info',          color: 'var(--primary, #3b82f6)' },
};

const _DIALOG_TITLES = {
    success: 'Sucesso',
    danger:  'Erro',
    warning: 'Atenção',
    info:    'Informação',
};

/**
 * Show a modal dialog with a message. Returns a Promise that resolves when
 * the user dismisses the dialog (clicking OK, the X, the backdrop, or Esc).
 *
 * @param {string} message - The message to display (required).
 * @param {object} [opts]
 * @param {string} [opts.type='danger'] - Visual style: success|danger|warning|info
 * @param {string} [opts.title] - Optional title override (default depends on type)
 * @param {string} [opts.okLabel='OK'] - Text for the confirm button
 */
export function showDialog(message, opts = {}) {
    const type = opts.type || 'danger';
    const meta = _DIALOG_ICONS[type] || _DIALOG_ICONS.danger;
    const title = opts.title || _DIALOG_TITLES[type] || 'Aviso';
    const okLabel = opts.okLabel || 'OK';

    // Remove any existing dialog first.
    document.getElementById('_app-dialog')?.remove();

    return new Promise(resolve => {
        const overlay = document.createElement('div');
        overlay.id = '_app-dialog';
        overlay.style.cssText =
            'position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:10000;' +
            'display:flex;align-items:center;justify-content:center;padding:16px;' +
            'animation:_dlg-fade-in .15s ease';

        overlay.innerHTML = `
<style>
@keyframes _dlg-fade-in { from { opacity: 0 } to { opacity: 1 } }
@keyframes _dlg-slide-up { from { transform: translateY(8px); opacity: 0 } to { transform: translateY(0); opacity: 1 } }
</style>
<div role="alertdialog" aria-modal="true" aria-labelledby="_app-dialog-title"
     style="background:var(--bg-card,#1e1e2e);border:1px solid var(--border,#2a2a40);
            border-radius:var(--r,8px);box-shadow:0 20px 60px rgba(0,0,0,.4);
            min-width:320px;max-width:480px;width:100%;
            animation:_dlg-slide-up .2s cubic-bezier(.22,1,.36,1)">
  <div style="display:flex;align-items:center;gap:12px;padding:16px 20px;
              border-bottom:1px solid var(--border,#2a2a40)">
    <i class="fa-solid ${meta.icon}" style="color:${meta.color};font-size:22px;flex-shrink:0"></i>
    <h3 id="_app-dialog-title"
        style="margin:0;font-size:15px;font-weight:600;color:var(--text-primary,#eee);flex:1">
      ${esc(title)}
    </h3>
    <button type="button" aria-label="Close" data-dlg-close
            style="background:none;border:none;color:var(--text-muted,#888);
                   font-size:18px;cursor:pointer;padding:4px 8px;border-radius:4px;
                   line-height:1">×</button>
  </div>
  <div style="padding:20px;color:var(--text-secondary,#ccc);font-size:14px;line-height:1.55;
              max-height:60vh;overflow-y:auto;white-space:pre-wrap;word-break:break-word">
    ${esc(message)}
  </div>
  <div style="padding:12px 20px 16px;display:flex;justify-content:flex-end;gap:8px;
              border-top:1px solid var(--border,#2a2a40)">
    <button type="button" data-dlg-ok
            style="background:var(--primary,#3b82f6);color:#fff;border:none;
                   padding:8px 18px;border-radius:6px;font-size:13px;font-weight:600;
                   cursor:pointer;min-width:80px">${esc(okLabel)}</button>
  </div>
</div>`;

        const close = () => {
            if (overlay.parentNode) overlay.parentNode.removeChild(overlay);
            document.removeEventListener('keydown', onKey);
            resolve();
        };
        const onKey = (e) => {
            if (e.key === 'Escape' || e.key === 'Enter') { e.preventDefault(); close(); }
        };

        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) close();
            else if (e.target.closest('[data-dlg-close]')) close();
            else if (e.target.closest('[data-dlg-ok]')) close();
        });
        document.addEventListener('keydown', onKey);

        document.body.appendChild(overlay);
        overlay.querySelector('[data-dlg-ok]')?.focus();
    });
}
