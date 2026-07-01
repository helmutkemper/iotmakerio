// server/public/control/js/toast.js — Floating notifications and confirm dialog.
//
// cpToast(type, message)  — self-dismissing popup, top-right corner
// cpConfirm(title, msg)   — async confirm modal, returns Promise<boolean>

const ICONS = {
    success: 'fa-circle-check',
    warning: 'fa-triangle-exclamation',
    danger:  'fa-circle-xmark',
};

// ── Toast ─────────────────────────────────────────────────────────────────────

function getToastContainer() {
    let el = document.getElementById('cp-toast-container');
    if (!el) {
        el = document.createElement('div');
        el.id = 'cp-toast-container';
        el.style.cssText = [
            'position:fixed', 'top:16px', 'right:16px', 'z-index:9999',
            'display:flex', 'flex-direction:column', 'gap:8px', 'pointer-events:none',
        ].join(';');
        document.body.appendChild(el);
    }
    return el;
}

/**
 * Shows a floating toast notification.
 * @param {'success'|'warning'|'danger'} type
 * @param {string} message
 * @param {number} [duration=4000]
 */
export function cpToast(type, message, duration = 4000) {
    const container = getToastContainer();

    const colors = {
        success: { bg: 'var(--success-bg)', border: 'var(--success)', text: 'var(--success)' },
        warning: { bg: 'var(--warning-bg)', border: 'var(--warning)', text: 'var(--warning)' },
        danger:  { bg: 'var(--danger-bg)',  border: 'var(--danger)',  text: 'var(--danger)'  },
    }[type] || { bg: 'var(--bg-card)', border: 'var(--border)', text: 'var(--text)' };

    const toast = document.createElement('div');
    toast.style.cssText = [
        `background:${colors.bg}`, `border:1px solid ${colors.border}`, `color:${colors.text}`,
        'border-radius:6px', 'padding:10px 14px', 'font-size:13px', 'font-family:var(--font)',
        'display:flex', 'align-items:center', 'gap:8px', 'pointer-events:all', 'cursor:pointer',
        'max-width:340px', 'box-shadow:0 4px 16px rgba(0,0,0,.3)',
        'opacity:0', 'transform:translateX(16px)', 'transition:opacity .2s ease, transform .2s ease',
    ].join(';');

    toast.innerHTML = `<i class="fa-solid ${ICONS[type] || 'fa-circle-info'}" style="flex-shrink:0"></i><span>${message}</span>`;
    toast.addEventListener('click', () => dismiss(toast));
    container.appendChild(toast);

    requestAnimationFrame(() => requestAnimationFrame(() => {
        toast.style.opacity = '1';
        toast.style.transform = 'translateX(0)';
    }));

    const timer = setTimeout(() => dismiss(toast), duration);

    function dismiss(el) {
        clearTimeout(timer);
        el.style.opacity = '0';
        el.style.transform = 'translateX(16px)';
        setTimeout(() => el.remove(), 220);
    }
}

// ── Confirm dialog ────────────────────────────────────────────────────────────

/**
 * Shows a modal confirmation dialog.
 * Replaces the browser's native confirm() — stays within the panel theme.
 *
 * @param {string} title          - modal heading (HTML allowed)
 * @param {string} message        - body text (HTML allowed)
 * @param {string} [confirmLabel] - confirm button label
 * @param {string} [cancelLabel]  - cancel button label
 * @returns {Promise<boolean>}
 */
export function cpConfirm(title, message, confirmLabel = 'Confirmar', cancelLabel = 'Cancelar') {
    return new Promise((resolve) => {
        document.getElementById('cp-confirm-modal')?.remove();

        const backdrop = document.createElement('div');
        backdrop.id = 'cp-confirm-modal';
        backdrop.className = 'cp-modal-backdrop';
        backdrop.innerHTML = `
<div class="cp-modal">
  <h3>${title}</h3>
  <p style="margin-top:8px;font-size:13px;color:var(--text-dim);line-height:1.5">${message}</p>
  <div style="display:flex;gap:8px;margin-top:20px;justify-content:flex-end">
    <button class="cp-btn cp-btn-ghost"    id="cp-confirm-cancel">${cancelLabel}</button>
    <button class="cp-btn cp-btn-warning"  id="cp-confirm-ok">${confirmLabel}</button>
  </div>
</div>`;

        document.body.appendChild(backdrop);

        function finish(result) {
            backdrop.remove();
            resolve(result);
        }

        document.getElementById('cp-confirm-cancel').addEventListener('click', () => finish(false));
        document.getElementById('cp-confirm-ok').addEventListener('click',     () => finish(true));
        backdrop.addEventListener('click', (e) => { if (e.target === backdrop) finish(false); });
    });
}

// Expose both functions globally for inline onclick handlers.
window.cpToast   = cpToast;
window.cpConfirm = cpConfirm;
