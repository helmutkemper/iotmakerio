// pages/project_export.js — "Github package" export flow.
//
// Orchestrates the toolbar's Github-package button. The boundary
// with projects.js mirrors the help_files.js boundary: projects.js
// owns _currentProjectId() and _isProjectDirty(), and calls into
// this module via projExportRun(projectId, { isDirty }). This
// module owns the modals, the API calls, and the actual download.
//
// Flow:
//
//   1. If isDirty → "Save first" modal, exit (user clicks Save and
//      retries).
//   2. POST /api/v1/projects/:id/export/check
//   3. If issues → grouped modal (parse / analyze / wizard /
//      helps / examples), exit (user fixes and retries).
//   4. If OK → fetch GET /api/v1/projects/:id/export/zip as blob,
//      trigger download via temporary <a download>. Show progress
//      toast. The /zip endpoint may itself return 409 + JSON
//      issues (defence-in-depth re-validation); detect via
//      Content-Type and show the same issues modal.
//
// All UI strings flow through translate.T() so a future locale
// gets first-class support without code edits. Default fallbacks
// are English — the caller's locale is honoured if the bundle has
// the key.
//
// Português: módulo do fluxo "Github package". Lida com modais,
// chamadas de API e download. Acoplamento com projects.js segue o
// mesmo padrão do help_files.js: estado vive em projects.js, este
// módulo só recebe o que precisa.

import { api } from '../api.js';
import { toast, t } from '../utils.js';

// ─── Public entry point ──────────────────────────────────────────

/**
 * projExportRun is called by projects.js's toolbar handler. The
 * caller is responsible for resolving projectId and the dirty flag
 * — this module never touches Monaco or backup state directly.
 *
 * @param {string} projectId
 * @param {{ isDirty: boolean }} opts
 */
export async function projExportRun(projectId, opts) {
    if (!projectId) {
        toast('error', t('export.no_project', 'No project context.'));
        return;
    }

    // Self-inject the small CSS scoped to this module's modals.
    // Idempotent. Same lazy-load rationale as help_files.js: most
    // page loads never reach the export flow.
    _ensureStylesheetLoaded();

    if (opts && opts.isDirty) {
        // The /check endpoint validates the SAVED source, but the
        // user's mental model is "what's in the editor right now".
        // Block the export until the two are aligned — otherwise
        // the user sees pre-flight failures for code they already
        // fixed but didn't save.
        _showSaveFirstModal();
        return;
    }

    // Pre-flight check.
    let checkResp;
    try {
        checkResp = await api(
            'POST',
            `/api/v1/projects/${encodeURIComponent(projectId)}/export/check`,
            {},
        );
    } catch (e) {
        toast('error', t('export.network_error',
            'Network error during pre-flight check: ') + e.message);
        return;
    }
    if (checkResp?.metadata?.status !== 200) {
        toast('error', t('export.check_failed',
            'Pre-flight check failed: ') +
            (checkResp?.metadata?.error || 'unknown'));
        return;
    }
    const data = checkResp.data || {};
    if (!data.ok) {
        _showIssuesModal(Array.isArray(data.issues) ? data.issues : []);
        return;
    }

    // All clear. Fetch the ZIP and trigger download.
    await _downloadZip(projectId);
}

// ─── Save-first modal ────────────────────────────────────────────

function _showSaveFirstModal() {
    const overlay = _modalShell();
    overlay.querySelector('.pe-modal').innerHTML = `
        <header>
            <h3>${_esc(t('export.unsaved.title', 'Save before exporting'))}</h3>
            <button type="button" class="pe-close" data-cancel
                    title="${_esc(t('common.close', 'Close'))}">
                <i class="fa-solid fa-xmark"></i>
            </button>
        </header>
        <div class="pe-body">
            <p>${_esc(t('export.unsaved.body',
                'You have unsaved changes. Save the project first, then try the export again.'))}</p>
        </div>
        <footer>
            <button type="button" class="btn btn-ghost" data-cancel>
                ${_esc(t('common.ok', 'OK'))}
            </button>
        </footer>
    `;
    _wireDismiss(overlay);
}

// ─── Issues modal (grouped by category) ──────────────────────────

// Categories must mirror the IssueCategory constants in
// server/projectexport/validator.go. Order here is the order the
// modal renders sections in — most actionable first.
const CATEGORY_META = [
    { key: 'parse_errors',      icon: 'fa-circle-xmark',     classifier: 'pe-cat-error' },
    { key: 'analyze_errors',    icon: 'fa-triangle-exclamation', classifier: 'pe-cat-error' },
    { key: 'analyze_warnings',  icon: 'fa-circle-exclamation',   classifier: 'pe-cat-warn' },
    { key: 'wizard_incomplete', icon: 'fa-wand-magic-sparkles',  classifier: 'pe-cat-warn' },
    { key: 'help_missing',      icon: 'fa-file-circle-question', classifier: 'pe-cat-warn' },
    { key: 'examples_missing',  icon: 'fa-image',                classifier: 'pe-cat-warn' },
];

function _showIssuesModal(issues) {
    const overlay = _modalShell();
    // Group issues by category, preserving the order defined above.
    const grouped = {};
    for (const it of issues) {
        const k = it.category || 'other';
        (grouped[k] = grouped[k] || []).push(it);
    }

    const sections = CATEGORY_META.map(meta => {
        const list = grouped[meta.key];
        if (!list || list.length === 0) return '';
        const label = t('export.section.' + meta.key,
            _defaultCategoryLabel(meta.key));
        const items = list.map(it => {
            // Issues with a line number get a small badge prefix —
            // helpful for parse/analyze diagnostics where the user
            // can jump to the line in the editor.
            const lineBadge = it.line
                ? `<span class="pe-line">L${it.line}${it.col ? ':' + it.col : ''}</span> `
                : '';
            return `<li>${lineBadge}${_esc(it.detail)}</li>`;
        }).join('');
        return `
            <section class="${meta.classifier}">
                <h4><i class="fa-solid ${meta.icon}"></i>
                    ${_esc(label)} <span class="pe-count">(${list.length})</span></h4>
                <ul>${items}</ul>
            </section>
        `;
    }).join('');

    overlay.querySelector('.pe-modal').innerHTML = `
        <header>
            <h3>${_esc(t('export.cannot_export.title',
                'Cannot export — fix the following first'))}</h3>
            <button type="button" class="pe-close" data-cancel
                    title="${_esc(t('common.close', 'Close'))}">
                <i class="fa-solid fa-xmark"></i>
            </button>
        </header>
        <div class="pe-body pe-issues">
            <p>${_esc(t('export.cannot_export.body',
                'The export contract requires a clean parse, no analyze warnings, all wizard cards completed, help files for the readme and every method, and at least one example.'))}</p>
            ${sections}
        </div>
        <footer>
            <button type="button" class="btn btn-primary" data-cancel>
                ${_esc(t('common.ok', 'OK'))}
            </button>
        </footer>
    `;
    _wireDismiss(overlay);
}

function _defaultCategoryLabel(key) {
    switch (key) {
        case 'parse_errors':      return 'Parse errors';
        case 'analyze_errors':    return 'Analyze errors';
        case 'analyze_warnings':  return 'Analyze warnings';
        case 'wizard_incomplete': return 'Wizard incomplete';
        case 'help_missing':      return 'Help files missing';
        case 'examples_missing':  return 'Examples missing';
        default:                  return key;
    }
}

// ─── Download ────────────────────────────────────────────────────

async function _downloadZip(projectId) {
    // Show a dismissable "downloading" toast. The fetch+blob path
    // doesn't expose progress events without extra plumbing
    // (XHR + progress listener); for sub-MB payloads the toast is
    // enough feedback. If projects routinely grow to tens of MB,
    // upgrading to fetch + ReadableStream + size accounting is a
    // future iteration.
    toast('info', t('export.downloading', 'Downloading package…'));

    let resp;
    try {
        // Use raw fetch (not api()) because we need access to the
        // Response object to inspect Content-Type and read either
        // a blob or a JSON body. api() always parses JSON.
        const tok = _bearerToken();
        resp = await fetch(
            `/api/v1/projects/${encodeURIComponent(projectId)}/export/zip`,
            {
                method:  'GET',
                headers: tok ? { Authorization: 'Bearer ' + tok } : {},
                credentials: 'same-origin',
            }
        );
    } catch (e) {
        toast('error', t('export.network_error',
            'Network error during download: ') + e.message);
        return;
    }

    const ctype = (resp.headers.get('Content-Type') || '').toLowerCase();

    // Defence-in-depth: the /zip endpoint may have re-validated and
    // returned 409 + JSON. Detect via Content-Type and re-show the
    // issues modal so the user gets the same UX as a /check failure.
    if (resp.status === 409 || ctype.startsWith('application/json')) {
        try {
            const j = await resp.json();
            const issues = (j?.data?.issues) || [];
            _showIssuesModal(issues);
        } catch {
            toast('error', t('export.export_failed',
                'Export failed and the server response could not be read.'));
        }
        return;
    }

    if (!resp.ok) {
        toast('error', t('export.export_failed',
            'Export failed: ') + resp.status);
        return;
    }

    // Read the binary, save via a temporary <a download>. The
    // filename comes from the Content-Disposition header sent by
    // the server; browsers do honour `download` when the same-origin
    // response includes that header.
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const filename = _filenameFromDisposition(
        resp.headers.get('Content-Disposition')) || 'project.zip';

    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    // Append-click-remove dance — required by Firefox.
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    // Revoke after a tick so the browser has time to start the
    // download. Holding the URL forever leaks RAM equal to the
    // payload size for the lifetime of the tab.
    setTimeout(() => URL.revokeObjectURL(url), 1500);

    toast('success', t('export.done', 'Package downloaded.'));
}

// ─── DOM / utility helpers ───────────────────────────────────────

function _modalShell() {
    const overlay = document.createElement('div');
    overlay.className = 'pe-overlay';
    overlay.innerHTML = `<div class="pe-modal" role="dialog" aria-modal="true"></div>`;
    document.body.appendChild(overlay);
    return overlay;
}

function _wireDismiss(overlay) {
    const close = () => {
        document.removeEventListener('keydown', onKey);
        overlay.remove();
    };
    overlay.addEventListener('click', (e) => {
        // Click on the dim backdrop (overlay itself) or any
        // [data-cancel] / .pe-close element dismisses.
        if (e.target === overlay
            || e.target.closest('[data-cancel]')
            || e.target.closest('.pe-close')) {
            close();
        }
    });
    const onKey = (e) => {
        if (e.key === 'Escape') {
            e.stopPropagation();
            close();
        }
    };
    document.addEventListener('keydown', onKey);
}

function _esc(s) {
    return String(s ?? '')
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function _bearerToken() {
    // Same key the rest of the SPA uses (api.js reads it from
    // here too). Kept inline rather than imported from api.js to
    // avoid a circular-ish dependency for one trivial lookup.
    try { return localStorage.getItem('token'); } catch { return null; }
}

function _filenameFromDisposition(header) {
    if (!header) return null;
    // Match `filename="..."` first (server emits this), fall back
    // to bare `filename=...` for robustness.
    let m = header.match(/filename\s*=\s*"([^"]+)"/i);
    if (m) return m[1];
    m = header.match(/filename\s*=\s*([^;]+)/i);
    return m ? m[1].trim() : null;
}

// ─── CSS injection ───────────────────────────────────────────────

function _ensureStylesheetLoaded() {
    if (document.getElementById('proj-export-styles')) return;
    const style = document.createElement('style');
    style.id = 'proj-export-styles';
    style.textContent = `
.pe-overlay {
    position: fixed; inset: 0; z-index: 10001;
    background: rgba(0, 0, 0, 0.45);
    display: flex; align-items: center; justify-content: center;
    padding: 16px;
    font-family: var(--font);
}
.pe-modal {
    background: var(--bg-card);
    color: var(--text-primary);
    border: 1px solid var(--border);
    border-radius: var(--rl, 12px);
    width: 100%; max-width: 640px; max-height: 84vh;
    display: flex; flex-direction: column;
    overflow: hidden;
    box-shadow: var(--shh, 0 8px 24px rgba(0,0,0,0.35));
}
.pe-modal header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border);
}
.pe-modal header h3 {
    margin: 0; font-size: 16px; font-weight: 600;
}
.pe-modal .pe-close {
    background: none; border: none; cursor: pointer;
    color: var(--text-muted); font-size: 16px; padding: 4px 8px;
    border-radius: 6px;
}
.pe-modal .pe-close:hover { background: rgba(255,255,255,0.05); color: var(--text-primary); }
.pe-modal .pe-body {
    padding: 16px 18px; overflow-y: auto; flex: 1; line-height: 1.5;
}
.pe-modal .pe-body > p { margin: 0 0 12px; color: var(--text-muted); font-size: 13px; }
.pe-modal footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 12px 18px; border-top: 1px solid var(--border);
    background: var(--bg-surface, transparent);
}
.pe-issues section {
    margin: 14px 0; padding: 10px 12px;
    border: 1px solid var(--border); border-radius: 8px;
    background: var(--bg-surface, transparent);
}
.pe-issues section h4 {
    margin: 0 0 8px; font-size: 13px; font-weight: 600;
    display: flex; align-items: center; gap: 8px;
}
.pe-issues .pe-count { color: var(--text-muted); font-weight: 400; font-size: 12px; }
.pe-issues ul { margin: 0; padding-left: 22px; font-size: 13px; }
.pe-issues li { margin: 3px 0; }
.pe-issues .pe-line {
    display: inline-block; padding: 1px 6px; margin-right: 4px;
    background: rgba(255,255,255,0.06); border-radius: 3px;
    font-family: ui-monospace, monospace; font-size: 11px;
    color: var(--text-muted);
}
.pe-cat-error h4 { color: var(--danger, #f38ba8); }
.pe-cat-warn  h4 { color: var(--warning, #f9e2af); }
    `.trim();
    document.head.appendChild(style);
}
