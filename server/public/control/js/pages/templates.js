// /ide/server/public/static/js/pages/templates.js — Template Package manager.
import { showAlert } from '../utils.js';
//
// This page is the UI for specialists to manage template packages.
// A template package is a ZIP the specialist uploads. It contains:
//   - devices/*.go — IDS-compliant Go structs (visual IDE blocks)
//   - output/*     — static project files with {{.VarName}} injection
//   - template.json — manifest declaring var → device.field mappings
//
// Version field rule:
//   The "version" field in template.json MUST be a positive integer string
//   (e.g. "1", "2", "42"). The server parser enforces this. Only the highest
//   version number is used — previous versions are not retained.
//
// Features:
//   - Upload a ZIP → worker parses it asynchronously
//   - Auto-poll pending templates every 2s until ready or error
//   - Expand/collapse soft parse warnings
//   - Device preview panel (names, ports) shown when status=ready
//   - Toggle visibility (private ↔ public)
//   - Publishing flags (publishToFeed, publishToSearch, readyToUse)
//     via PUT /api/v1/templates/:id/publishing — only for public+ready templates
//   - Safe delete: requires the specialist to type the template name before deletion
//
// API endpoints used:
//   GET    /api/v1/templates?mine=true          — list caller's own templates
//   POST   /api/v1/templates                    — upload new template ZIP (multipart)
//   GET    /api/v1/templates/:id                — poll for status change
//   PUT    /api/v1/templates/:id/visibility     — change visibility
//   PUT    /api/v1/templates/:id/publishing     — set publishing flags
//   DELETE /api/v1/templates/:id                — delete template + ZIP on disk
//
// Auth: all API calls require a valid Bearer token (set in S.token).

import { S }   from '../state.js';
import { api } from '../api.js';

// ── Page state ────────────────────────────────────────────────────────────────

let _templates  = [];          // current list of TemplatePackage objects
let _pollTimers = {};          // { [id]: intervalId } — active polls for pending templates
let _uploading  = false;       // true while a ZIP upload is in flight

// _modalOverlay is the shared overlay element for all dialogs on this page.
// Only one modal can be open at a time.
let _modalOverlay = null;

const POLL_INTERVAL_MS = 2000; // how often to re-fetch a pending template

// ── Entry point ───────────────────────────────────────────────────────────────

export function renderTemplates(root) {
    // Clear any polls left over from a previous visit to this page.
    _clearAllPolls();
    _templates = [];
    _uploading = false;

    root.innerHTML = buildShell();
    _modalOverlay = document.getElementById('tpl-modal-overlay');
    _loadList();
}

// Called by the router when leaving this page.
export function leaveTemplates() {
    _clearAllPolls();
}

// ── Shell ─────────────────────────────────────────────────────────────────────

function buildShell() {
    return `
<div class="pw" style="max-width:960px">

  <div style="display:flex;align-items:center;gap:12px;margin-bottom:24px;flex-wrap:wrap">
    <h1 style="font-size:24px;font-weight:800;color:var(--primary);
               display:flex;align-items:center;gap:10px;margin:0">
      <i class="fa-solid fa-file-export"></i>Templates
    </h1>
    <span style="font-size:13px;color:var(--text-muted)">
      Upload a ZIP package — makers use your template in the IDE to generate configured projects.
    </span>

    <!-- Upload button — triggers a hidden file input -->
    <label id="tpl-upload-btn"
           style="margin-left:auto;cursor:pointer"
           title="Upload a template ZIP">
      <span class="btn btn-primary btn-sm" id="tpl-upload-label">
        <i class="fa-solid fa-upload"></i> Upload Template
      </span>
      <input type="file" id="tpl-file-input" accept=".zip"
             style="display:none" onchange="window._tplFileSelected(this)">
    </label>
  </div>

  <!-- Upload progress / error banner -->
  <div id="tpl-upload-alert" style="display:none;margin-bottom:16px"></div>

  <!-- Template list -->
  <div id="tpl-list">
    <div class="lspinner"><div class="spinner"></div></div>
  </div>

</div>

<!-- Shared modal overlay — used for delete confirmations and quality commitment dialog -->
<div id="tpl-modal-overlay" style="display:none"></div>

${_buildStyles()}`;
}

function _buildStyles() {
    return `<style>
/* ── Template card ── */
.tpl-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--rl);
  padding: 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  box-shadow: var(--sh);
  transition: box-shadow .15s;
  margin-bottom: 16px;
}
.tpl-card:hover { box-shadow: var(--shh) }

/* ── Status badge ── */
.tpl-status {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 700;
  padding: 3px 10px;
  border-radius: 99px;
}
.tpl-status.pending { background:#FFF8E1;color:#B7770D;border:1px solid #FFD97D }
.tpl-status.ready   { background:#F0FDF4;color:#15803D;border:1px solid #86EFAC }
.tpl-status.error   { background:#FEF2F2;color:#B91C1C;border:1px solid #FCA5A5 }

/* ── Warnings accordion ── */
.tpl-warn-toggle {
  background:none;border:none;cursor:pointer;font-size:12px;
  color:var(--warning,#B7770D);padding:0;display:flex;
  align-items:center;gap:5px;font-family:var(--font);
}
.tpl-warn-body {
  margin-top:8px;padding:10px 14px;background:#FFF8E1;
  border:1px solid #FFD97D;border-radius:var(--r);
  font-size:12px;color:#7A5C00;line-height:1.7;
}
.tpl-warn-body ul { margin:0;padding-left:18px }

/* ── Device preview panel ── */
.tpl-devices {
  background:var(--bg-page,#f8f9fa);
  border:1px solid var(--border);
  border-radius:var(--r);padding:12px 16px;font-size:12px;
}
.tpl-devices-title {
  font-size:11px;font-weight:700;text-transform:uppercase;
  letter-spacing:.05em;color:var(--text-muted);margin-bottom:8px;
}
.tpl-device-row {
  display:flex;align-items:flex-start;gap:10px;
  padding:6px 0;border-bottom:1px solid var(--border);
}
.tpl-device-row:last-child { border-bottom:none }
.tpl-device-name { font-weight:600;color:var(--text-primary);min-width:120px }
.tpl-device-ports { display:flex;flex-wrap:wrap;gap:4px }
.tpl-port-tag {
  font-size:11px;padding:1px 7px;border-radius:99px;font-family:monospace;
}
.tpl-port-tag.input  { background:#EFF6FF;color:#1D4ED8;border:1px solid #BFDBFE }
.tpl-port-tag.output { background:#F0FDF4;color:#15803D;border:1px solid #86EFAC }
.tpl-port-tag.prop   { background:#FAF5FF;color:#6B21A8;border:1px solid #D8B4FE }

/* ── Publishing flags section ── */
.tpl-publish-section {
  border:1px solid var(--border);border-radius:var(--r);
  padding:14px 16px;display:flex;flex-direction:column;gap:10px;
}
.tpl-publish-header {
  font-size:12px;font-weight:700;color:var(--text-secondary);
  display:flex;align-items:center;gap:6px;margin-bottom:2px;
}
.tpl-publish-row { display:flex;align-items:flex-start;gap:10px }
.tpl-publish-row input[type="checkbox"] {
  margin-top:2px;cursor:pointer;flex-shrink:0;
}
.tpl-publish-row input[type="checkbox"]:disabled { cursor:not-allowed;opacity:.5 }
.tpl-publish-label { font-size:13px;font-weight:600;color:var(--text-primary);cursor:pointer }
.tpl-publish-label.disabled { color:var(--text-muted);cursor:not-allowed }
.tpl-publish-hint { font-size:11px;color:var(--text-muted);margin-top:1px }
.tpl-publish-note { font-size:11px;color:var(--text-muted);font-style:italic }

/* ── Visibility toggle button ── */
.tpl-vis-btn {
  font-size:12px;display:inline-flex;align-items:center;gap:5px;cursor:pointer;
}

/* ── Empty state ── */
.tpl-empty {
  display:flex;flex-direction:column;align-items:center;
  justify-content:center;padding:60px 20px;
  color:var(--text-muted);text-align:center;gap:12px;
}
.tpl-empty i { font-size:40px;opacity:.2 }
</style>`;
}

// ── List ──────────────────────────────────────────────────────────────────────

async function _loadList() {
    const el = document.getElementById('tpl-list');
    if (!el) return;
    el.innerHTML = '<div class="lspinner"><div class="spinner"></div></div>';

    try {
        const res = await api('GET', '/api/v1/templates?mine=true');

        if (res?.metadata?.status >= 400) {
            el.innerHTML = `<div class="alert alert-danger">
              \u2717 ${_esc(res.metadata.error || 'Could not load templates')}
              <button class="btn btn-ghost btn-sm" onclick="window._tplReload()"
                      style="margin-left:12px">Retry</button>
            </div>`;
            return;
        }

        _templates = res?.data || [];

        if (!_templates.length) {
            el.innerHTML = `<div class="tpl-empty">
              <i class="fa-solid fa-file-export"></i>
              <p style="font-size:15px;font-weight:600;margin:0">No templates yet</p>
              <p style="font-size:13px;margin:0">
                Upload a ZIP to create your first template package.
              </p>
            </div>`;
            return;
        }

        _renderList();

        // Start polling for any templates that are still pending.
        _templates.forEach(t => {
            if (t.status === 'pending') _startPoll(t.id);
        });

        // For ready templates that do not have a cached def yet, fetch it now
        // so the device preview panel can be rendered.
        _templates.forEach(t => {
            if (t.status === 'ready' && !t._def) _fetchAndAttachDef(t.id);
        });

    } catch (e) {
        if (el) el.innerHTML = `<div class="alert alert-danger">
          \u2717 Network error: ${_esc(e?.message || String(e))}
          <button class="btn btn-ghost btn-sm" onclick="window._tplReload()"
                  style="margin-left:12px">Retry</button>
        </div>`;
    }
}

function _renderList() {
    const el = document.getElementById('tpl-list');
    if (!el) return;

    if (!_templates.length) {
        el.innerHTML = `<div class="tpl-empty">
          <i class="fa-solid fa-file-export"></i>
          <p style="font-size:15px;font-weight:600;margin:0">No templates yet</p>
          <p style="font-size:13px;margin:0">Upload a ZIP to create your first template.</p>
        </div>`;
        return;
    }

    el.innerHTML = _templates.map(_buildCard).join('');
}

// ── Card builder ───────────────────────────────────────────────────────────────

function _buildCard(t) {
    const statusBadge = _buildStatusBadge(t.status);
    const warnsHtml   = _buildWarnings(t);
    const devicesHtml = _buildDevicePreview(t);
    const publishHtml = _buildPublishSection(t);
    const visHtml     = _buildVisibilityToggle(t);

    // Meta tags: version and visibility pill.
    const meta = [];
    if (t.version) meta.push(`<span class="tag">v${_esc(t.version)}</span>`);
    if (t.visibility) meta.push(
        `<span class="tag">${t.visibility === 'public' ? '\uD83C\uDF10 Public' : '\uD83D\uDD12 Private'}</span>`
    );

    return `<div class="tpl-card" id="tpl-card-${_esc(t.id)}">

  <!-- Header row -->
  <div style="display:flex;align-items:flex-start;gap:12px;flex-wrap:wrap">
    <div style="flex:1;min-width:200px">
      <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
        <h3 style="font-size:16px;font-weight:700;margin:0;color:var(--text-primary)">
          ${_esc(t.name || '(unnamed)')}
        </h3>
        ${statusBadge}
      </div>
      ${t.description
        ? `<p style="font-size:13px;color:var(--text-muted);margin:4px 0 0">${_esc(t.description)}</p>`
        : ''}
    </div>

    <!-- Actions -->
    <div style="display:flex;align-items:center;gap:8px;flex-shrink:0">
      ${visHtml}
      <button class="btn btn-ghost btn-sm"
              style="color:var(--danger)"
              onclick="window._tplConfirmDelete('${_esc(t.id)}', '${_esc(t.name || '')}')"
              title="Delete this template">
        <i class="fa-solid fa-trash"></i>
      </button>
    </div>
  </div>

  <!-- Meta tags row -->
  ${meta.length ? `<div style="display:flex;gap:6px;flex-wrap:wrap">${meta.join('')}</div>` : ''}

  <!-- Device preview (only when ready and def is loaded) -->
  ${devicesHtml}

  <!-- Warnings / errors section -->
  ${warnsHtml}

  <!-- Publishing flags (only when ready) -->
  ${publishHtml}

</div>`;
}

// ── Status badge ───────────────────────────────────────────────────────────────

function _buildStatusBadge(status) {
    switch (status) {
        case 'ready':
            return `<span class="tpl-status ready">
              <i class="fa-solid fa-circle-check"></i> Ready
            </span>`;
        case 'pending':
            return `<span class="tpl-status pending">
              <i class="fa-solid fa-circle-notch fa-spin"></i> Processing\u2026
            </span>`;
        case 'error':
            return `<span class="tpl-status error">
              <i class="fa-solid fa-circle-xmark"></i> Error
            </span>`;
        default:
            return `<span class="tpl-status pending">${_esc(status)}</span>`;
    }
}

// ── Device preview panel ───────────────────────────────────────────────────────
//
// Shown only when status=ready and the local template object has a _def
// property attached. The _def is fetched via GET /api/v1/templates/:id and
// attached by _fetchAndAttachDef. The panel shows device names and their
// port tags coloured by category (input/output/prop).

function _buildDevicePreview(t) {
    if (t.status !== 'ready' || !t._def?.devices?.length) return '';

    const rows = t._def.devices.map(dev => {
        const ports = _buildPortTags(dev);
        return `<div class="tpl-device-row">
          <div class="tpl-device-name">${_esc(dev.name || dev.structName || '?')}</div>
          <div class="tpl-device-ports">${ports}</div>
        </div>`;
    }).join('');

    return `<div class="tpl-devices">
      <div class="tpl-devices-title">
        <i class="fa-solid fa-microchip" style="margin-right:4px"></i>
        Devices (${t._def.devices.length})
      </div>
      ${rows}
    </div>`;
}

// _buildPortTags renders coloured pill tags for each port category.
// Three categories are supported: input (blue), output (green), prop (purple).
function _buildPortTags(dev) {
    const tags = [];

    (dev.inputs  || []).forEach(p =>
        tags.push(`<span class="tpl-port-tag input" title="input">${_esc(p.label || p.fieldName)}</span>`)
    );
    (dev.outputs || []).forEach(p =>
        tags.push(`<span class="tpl-port-tag output" title="output">${_esc(p.label || p.fieldName)}</span>`)
    );
    (dev.props   || []).forEach(p =>
        tags.push(`<span class="tpl-port-tag prop" title="prop">${_esc(p.label || p.fieldName)}</span>`)
    );

    return tags.join('') || '<span style="color:var(--text-muted);font-size:11px">no ports</span>';
}

// _fetchAndAttachDef loads the full template def from the server and attaches
// it to the local object so _buildDevicePreview can render. Called on page
// load for ready templates and after polling transitions a template to ready.
async function _fetchAndAttachDef(id) {
    try {
        const res = await api('GET', `/api/v1/templates/${id}`);
        if (res?.metadata?.status !== 200) return;

        const def = res?.data?.def;
        if (!def) return;

        const idx = _templates.findIndex(t => t.id === id);
        if (idx < 0) return;

        _templates[idx]._def = def;

        // Re-render the single card so the device preview appears.
        _replaceCard(id);
    } catch {
        // Non-critical — the preview is cosmetic. Fail silently.
    }
}

// ── Warnings / errors section ──────────────────────────────────────────────────

function _buildWarnings(t) {
    const warns = t.parseErrors;
    if (!warns || !warns.length) return '';

    const label = t.status === 'error'
        ? `<i class="fa-solid fa-circle-xmark" style="color:var(--danger)"></i>
           <strong style="color:var(--danger)">${warns.length} parse error(s)</strong>`
        : `<i class="fa-solid fa-triangle-exclamation"></i>
           ${warns.length} warning(s)`;

    const items    = warns.map(w => `<li>${_esc(w)}</li>`).join('');
    const toggleId = `tpl-warns-${_esc(t.id)}`;

    return `<div>
  <button class="tpl-warn-toggle"
          onclick="document.getElementById('${toggleId}').style.display =
                   document.getElementById('${toggleId}').style.display === 'none'
                   ? '' : 'none'">
    ${label}
    <i class="fa-solid fa-chevron-down" style="font-size:10px"></i>
  </button>
  <div id="${toggleId}" style="display:none">
    <div class="tpl-warn-body"><ul>${items}</ul></div>
  </div>
</div>`;
}

// ── Visibility toggle ──────────────────────────────────────────────────────────

function _buildVisibilityToggle(t) {
    if (t.status !== 'ready') return '';

    if (t.visibility === 'public') {
        return `<button class="btn btn-secondary btn-sm tpl-vis-btn"
                        title="Make private — remove from the IDE template picker"
                        onclick="window._tplSetVisibility('${_esc(t.id)}', 'private')">
          <i class="fa-solid fa-lock"></i> Make Private
        </button>`;
    }
    return `<button class="btn btn-secondary btn-sm tpl-vis-btn"
                    title="Publish — makers can use this in the IDE"
                    onclick="window._tplSetVisibility('${_esc(t.id)}', 'public')">
          <i class="fa-solid fa-globe"></i> Publish
        </button>`;
}

// ── Publishing flags section ───────────────────────────────────────────────────
//
// Mirrors the same three-flag UI from projects.js.
// Flags are always visible when status=ready, but are only clickable when
// visibility=public. When private, a note explains why they are disabled.

function _buildPublishSection(t) {
    if (t.status !== 'ready') return '';

    const canPublish = t.visibility === 'public';

    const feedChange   = canPublish ? `window._tplPublishFlagChange('${_esc(t.id)}')` : '';
    const searchChange = canPublish ? `window._tplPublishFlagChange('${_esc(t.id)}')` : '';
    const readyChange  = canPublish ? `window._tplReadyToUseChange('${_esc(t.id)}')` : '';

    const note = !canPublish
        ? `<p class="tpl-publish-note">
             <i class="fa-solid fa-lock" style="margin-right:4px"></i>
             Make this template <strong>public</strong> to enable publishing options.
           </p>`
        : '';

    return `<div class="tpl-publish-section">
  <div class="tpl-publish-header">
    <i class="fa-solid fa-share-nodes"></i> Community Publishing
  </div>
  ${note}
  <div class="tpl-publish-row">
    <input type="checkbox" id="tpl-feed-${_esc(t.id)}"
           ${t.publishToFeed ? 'checked' : ''}
           ${canPublish ? '' : 'disabled'}
           onchange="${feedChange}">
    <div>
      <label class="tpl-publish-label ${canPublish ? '' : 'disabled'}"
             for="tpl-feed-${_esc(t.id)}">Publish to feed</label>
      <div class="tpl-publish-hint">Show this project in the community feed tabs</div>
    </div>
  </div>
  <div class="tpl-publish-row">
    <input type="checkbox" id="tpl-search-${_esc(t.id)}"
           ${t.publishToSearch ? 'checked' : ''}
           ${canPublish ? '' : 'disabled'}
           onchange="${searchChange}">
    <div>
      <label class="tpl-publish-label ${canPublish ? '' : 'disabled'}"
             for="tpl-search-${_esc(t.id)}">Publish to search</label>
      <div class="tpl-publish-hint">Include this project in marketplace search results</div>
    </div>
  </div>
  <div class="tpl-publish-row">
    <input type="checkbox" id="tpl-ready-${_esc(t.id)}"
           ${t.readyToUse ? 'checked' : ''}
           ${canPublish ? '' : 'disabled'}
           onchange="${readyChange}">
    <div>
      <label class="tpl-publish-label ${canPublish ? '' : 'disabled'}"
             for="tpl-ready-${_esc(t.id)}">
        Ready to use
        <span style="font-size:11px;font-weight:400;color:var(--text-muted)">quality commitment</span>
      </label>
      <div class="tpl-publish-hint">I certify this project is documented and ready for use by others</div>
    </div>
  </div>
</div>`;
}

// ── Upload ────────────────────────────────────────────────────────────────────

window._tplFileSelected = function(input) {
    const file = input.files?.[0];
    if (!file) return;
    input.value = '';
    _uploadZip(file);
};

async function _uploadZip(file) {
    if (_uploading) return;
    _uploading = true;

    const btn   = document.getElementById('tpl-upload-label');
    const alert = document.getElementById('tpl-upload-alert');
    if (btn)   { btn.innerHTML = '<i class="fa-solid fa-circle-notch fa-spin"></i> Uploading\u2026'; }
    if (alert) { alert.style.display = 'none'; alert.innerHTML = ''; }

    try {
        const form = new FormData();
        form.append('file', file);

        const opts = { method: 'POST', body: form };
        if (S.token) opts.headers = { Authorization: 'Bearer ' + S.token };

        const r    = await fetch('/api/v1/templates', opts);
        const json = await r.json().catch(() => ({ metadata: { status: r.status, error: 'parse error' } }));

        if (!r.ok || json?.metadata?.status >= 400) {
            _showUploadAlert('danger',
                '\u2717 ' + (json?.metadata?.error || `HTTP ${r.status}`));
            return;
        }

        const newTemplate = json?.data || json;
        _templates.unshift(newTemplate);
        _renderList();
        _showUploadAlert('success',
            `\u2713 "${_esc(newTemplate.name || file.name)}" uploaded \u2014 processing\u2026`);

        if (newTemplate.id && newTemplate.status === 'pending') {
            _startPoll(newTemplate.id);
        }

    } catch (e) {
        _showUploadAlert('danger', '\u2717 Network error: ' + _esc(e?.message || String(e)));
    } finally {
        _uploading = false;
        if (btn) btn.innerHTML = '<i class="fa-solid fa-upload"></i> Upload Template';
    }
}

function _showUploadAlert(type, html) {
    const el = document.getElementById('tpl-upload-alert');
    if (!el) return;
    el.style.display = 'block';
    el.innerHTML = `<div class="alert alert-${type}">${html}</div>`;
    if (type === 'success') setTimeout(() => { el.style.display = 'none'; }, 5000);
}

// ── Visibility toggle ─────────────────────────────────────────────────────────

window._tplSetVisibility = async function(id, visibility) {
    try {
        const res = await api('PUT', `/api/v1/templates/${id}/visibility`, { visibility });

        if (res?.metadata?.status >= 400) {
            await showAlert('danger', 'Could not update visibility: ' +
                (res?.metadata?.error || 'unknown error'));
            return;
        }

        const t = _templates.find(x => x.id === id);
        if (t) {
            t.visibility = visibility;
            // When switching to private, zero publishing flags locally.
            // The server also enforces this on the next publishing call.
            if (visibility === 'private') {
                t.publishToFeed   = false;
                t.publishToSearch = false;
                t.readyToUse      = false;
            }
        }
        _replaceCard(id);

    } catch (e) {
        await showAlert('danger', 'Network error: ' + (e?.message || String(e)));
    }
};

// ── Publishing flags ───────────────────────────────────────────────────────────

// _tplPublishFlagChange handles publishToFeed and publishToSearch changes.
// Reads all three checkboxes and sends a single atomic update.
window._tplPublishFlagChange = async function(id) {
    const feed   = document.getElementById(`tpl-feed-${id}`)?.checked   ?? false;
    const search = document.getElementById(`tpl-search-${id}`)?.checked ?? false;
    const ready  = document.getElementById(`tpl-ready-${id}`)?.checked  ?? false;
    await _savePublishingFlags(id, feed, search, ready);
};

// _tplReadyToUseChange handles the "Ready to use" checkbox.
// Checking it shows a quality-commitment confirmation before saving.
// Unchecking saves immediately without a dialog.
window._tplReadyToUseChange = async function(id) {
    const readyEl = document.getElementById(`tpl-ready-${id}`);
    if (!readyEl) return;

    if (readyEl.checked) {
        _openReadyCommitmentDialog(id);
        return;
    }

    const feed   = document.getElementById(`tpl-feed-${id}`)?.checked   ?? false;
    const search = document.getElementById(`tpl-search-${id}`)?.checked ?? false;
    await _savePublishingFlags(id, feed, search, false);
};

// _openReadyCommitmentDialog shows a modal asking the specialist to commit to
// quality before readyToUse can be set to true. On cancel the checkbox reverts.
function _openReadyCommitmentDialog(id) {
    if (!_modalOverlay) return;

    _modalOverlay.style.cssText =
        'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.45);' +
        'z-index:9000;align-items:center;justify-content:center;';

    _modalOverlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:440px;box-shadow:var(--shh);
            border:1px solid var(--border);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;margin-bottom:8px">
    <i class="fa-solid fa-certificate" style="color:var(--primary);margin-right:8px"></i>
    Quality Commitment
  </h2>
  <p style="color:var(--text-secondary);font-size:14px;margin-bottom:20px">
    By marking this template as <strong>Ready to use</strong>, you certify that:
  </p>
  <ul style="font-size:13px;color:var(--text-secondary);padding-left:20px;margin-bottom:20px;line-height:1.8">
    <li>The template is fully documented</li>
    <li>All devices are correctly configured</li>
    <li>Output files have been tested</li>
    <li>It is ready to be used by other makers without guidance from you</li>
  </ul>
  <div style="display:flex;gap:10px;margin-top:8px">
    <button class="btn btn-secondary btn-sm" style="flex:1"
            onclick="window._tplCancelReadyCommitment('${_esc(id)}')">
      Cancel
    </button>
    <button class="btn btn-primary btn-sm" style="flex:2"
            onclick="window._tplConfirmReadyCommitment('${_esc(id)}')">
      <i class="fa-solid fa-certificate"></i> I Commit to Quality
    </button>
  </div>
</div>`;
}

window._tplCancelReadyCommitment = function(id) {
    const readyEl = document.getElementById(`tpl-ready-${id}`);
    if (readyEl) readyEl.checked = false;
    _closeModal();
};

window._tplConfirmReadyCommitment = async function(id) {
    _closeModal();
    const feed   = document.getElementById(`tpl-feed-${id}`)?.checked   ?? false;
    const search = document.getElementById(`tpl-search-${id}`)?.checked ?? false;
    await _savePublishingFlags(id, feed, search, true);
};

// _savePublishingFlags calls PUT /api/v1/templates/:id/publishing and syncs
// the local state. On error the card is re-rendered to revert checkbox state.
async function _savePublishingFlags(id, feed, search, ready) {
    try {
        const res = await api('PUT', `/api/v1/templates/${id}/publishing`, {
            publishToFeed:   feed,
            publishToSearch: search,
            readyToUse:      ready,
        });

        if (res?.metadata?.status >= 400) {
            await showAlert('danger', 'Could not update publishing flags: ' +
                (res?.metadata?.error || 'unknown error'));
            _replaceCard(id);
            return;
        }

        const t = _templates.find(x => x.id === id);
        if (t) {
            t.publishToFeed   = feed;
            t.publishToSearch = search;
            t.readyToUse      = ready;
        }

    } catch (e) {
        await showAlert('danger', 'Network error: ' + (e?.message || String(e)));
        _replaceCard(id);
    }
}

// ── Safe delete with name confirmation ────────────────────────────────────────
//
// The specialist must type the exact template name before the "Delete Forever"
// button becomes active. Mirrors confirmDeleteProject in projects.js.

window._tplConfirmDelete = function(id, name) {
    if (!_modalOverlay) return;

    _modalOverlay.style.cssText =
        'display:flex;position:fixed;inset:0;background:rgba(0,0,0,.55);' +
        'z-index:9000;align-items:center;justify-content:center;';

    _modalOverlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:32px;
            width:100%;max-width:440px;box-shadow:var(--shh);
            border:2px solid var(--danger);animation:fi .2s ease">
  <h2 style="font-size:18px;font-weight:700;color:var(--danger);margin-bottom:8px">
    <i class="fa-solid fa-triangle-exclamation"></i> Delete Template
  </h2>
  <p style="color:var(--text-secondary);font-size:14px;margin-bottom:16px">
    This action is <strong>permanent and cannot be undone</strong>.
    The template ZIP and all its data will be deleted.
  </p>
  <p style="font-size:13px;color:var(--text-secondary);margin-bottom:8px">
    Type <strong>${_esc(name)}</strong> to confirm:
  </p>
  <input id="tpl-del-input" class="fc" type="text"
    placeholder="${_esc(name)}"
    oninput="window._tplOnDeleteInput('${_esc(name)}')">
  <div id="tpl-del-err" class="alert alert-danger" style="display:none;margin-top:12px"></div>
  <div style="display:flex;gap:10px;margin-top:20px">
    <button class="btn btn-secondary btn-sm" style="flex:1"
            onclick="window._tplCloseDeleteModal()">Cancel</button>
    <button class="btn btn-danger btn-sm" id="tpl-del-submit" disabled style="flex:2"
            onclick="window._tplExecuteDelete('${_esc(id)}')">
      <i class="fa-solid fa-trash-can"></i> Delete Forever
    </button>
  </div>
</div>`;
};

window._tplOnDeleteInput = function(expected) {
    const input = document.getElementById('tpl-del-input');
    const btn   = document.getElementById('tpl-del-submit');
    if (!input || !btn) return;
    btn.disabled = input.value !== expected;
};

window._tplCloseDeleteModal = function() { _closeModal(); };

window._tplExecuteDelete = async function(id) {
    const btn   = document.getElementById('tpl-del-submit');
    const errEl = document.getElementById('tpl-del-err');
    if (btn) { btn.disabled = true; btn.textContent = 'Deleting\u2026'; }

    try {
        const res = await api('DELETE', `/api/v1/templates/${id}`);

        if (res?.metadata?.status >= 400) {
            if (errEl) {
                errEl.textContent = res.metadata.error || 'Could not delete.';
                errEl.style.display = 'block';
            }
            if (btn) {
                btn.disabled = false;
                btn.innerHTML = '<i class="fa-solid fa-trash-can"></i> Delete Forever';
            }
            return;
        }

        _closeModal();
        _stopPoll(id);
        _templates = _templates.filter(t => t.id !== id);
        _renderList();

    } catch (e) {
        if (errEl) {
            errEl.textContent = 'Network error: ' + (e?.message || String(e));
            errEl.style.display = 'block';
        }
        if (btn) {
            btn.disabled = false;
            btn.innerHTML = '<i class="fa-solid fa-trash-can"></i> Delete Forever';
        }
    }
};

// ── Poll (pending → ready / error) ────────────────────────────────────────────

function _startPoll(id) {
    if (_pollTimers[id]) return;
    _pollTimers[id] = setInterval(() => _pollOne(id), POLL_INTERVAL_MS);
}

function _stopPoll(id) {
    if (_pollTimers[id]) {
        clearInterval(_pollTimers[id]);
        delete _pollTimers[id];
    }
}

function _clearAllPolls() {
    Object.keys(_pollTimers).forEach(_stopPoll);
}

async function _pollOne(id) {
    try {
        const res = await api('GET', `/api/v1/templates/${id}`);
        if (res?.metadata?.status >= 400) return;

        const updated = res?.data?.template;
        const def     = res?.data?.def;
        if (!updated) return;

        const idx = _templates.findIndex(t => t.id === id);
        if (idx < 0) return;

        // Merge the server update, preserving any locally attached _def.
        _templates[idx] = { ..._templates[idx], ...updated };
        if (def) _templates[idx]._def = def;

        if (updated.status === 'ready' || updated.status === 'error') {
            _stopPoll(id);

            // If now ready but no def yet, fetch it separately.
            if (updated.status === 'ready' && !_templates[idx]._def) {
                _fetchAndAttachDef(id);
                return; // _fetchAndAttachDef calls _replaceCard when done
            }
        }

        _replaceCard(id);

    } catch {
        // Network hiccup — retry next tick.
    }
}

// ── Card in-place replacement ─────────────────────────────────────────────────

// _replaceCard rebuilds a single card in-place, avoiding scroll-position jumps.
function _replaceCard(id) {
    const t = _templates.find(x => x.id === id);
    if (!t) return;

    const cardEl = document.getElementById(`tpl-card-${id}`);
    if (!cardEl) { _renderList(); return; }

    const tmp = document.createElement('div');
    tmp.innerHTML = _buildCard(t);
    cardEl.replaceWith(tmp.firstElementChild);
}

// ── Modal helper ───────────────────────────────────────────────────────────────

function _closeModal() {
    if (_modalOverlay) {
        _modalOverlay.style.display = 'none';
        _modalOverlay.innerHTML = '';
    }
}

// ── Reload helper ─────────────────────────────────────────────────────────────

window._tplReload = function() { _loadList(); };

// ── HTML helper ───────────────────────────────────────────────────────────────

function _esc(s) {
    return (s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;')
        .replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
