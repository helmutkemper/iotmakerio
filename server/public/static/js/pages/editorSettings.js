// server/public/static/js/pages/editorSettings.js — Editor Settings page.
//
// Three sub-pages accessible via left sidebar navigation:
//
//   1. General — Interface Language + IDE Menu Profile dropdowns
//      (moved from the Profile page to keep profile focused on identity)
//
//   2. Menu — Full menu tree with checkboxes for per-item visibility.
//      The maker can hide items they don't use (e.g., Loop, Mul, Div).
//      Changes are saved automatically with a short debounce.
//
//   3. Stage — Workspace behaviour knobs: zoom sensitivity, pan
//      behaviour, cursor hints. Changes save with a debounce (slider)
//      or immediately (checkbox). A refresh of the IDE applies them;
//      no hot-reload yet.
//
// Endpoints used:
//   GET  /api/v1/profile              — user locale + menu profile info
//   PUT  /api/v1/profile/locale       — update locale
//   PUT  /api/v1/profile/menu-profile — update menu profile
//   GET  /api/v1/editor/menu-prefs    — menu tree + hidden items
//   PUT  /api/v1/editor/menu-prefs    — batch update hidden items
//   DELETE /api/v1/editor/menu-prefs  — reset to admin defaults
//   GET  /api/v1/editor/stage-prefs   — stage knobs + server defaults
//   PUT  /api/v1/editor/stage-prefs   — patch one or more knobs
//   DELETE /api/v1/editor/stage-prefs — reset to defaults

import { S }   from '../state.js';
import { api } from '../api.js';
import { t, showConfirm }   from '../utils.js';

// ── State ────────────────────────────────────────────────────────────────────

let _subPage  = 'general';  // 'general' | 'menu'
let _menuTree = [];          // MenuTreeNode[] from the server
let _hidden   = new Set();   // slot_ids currently hidden by the user
let _saveTimer = null;       // debounce timer for auto-save

// ── Main render ──────────────────────────────────────────────────────────────

export async function renderEditorSettings(root) {
    root.innerHTML = `
<div style="max-width:960px;margin:0 auto;padding:24px 20px">
  <h2 style="margin:0 0 20px;font-size:20px;font-weight:700;color:var(--text-primary)">
    <i class="fa-solid fa-sliders" style="margin-right:8px;color:var(--primary)"></i>
    ${t('editorSettings.title', 'Editor Settings')}
  </h2>

  <div style="display:flex;gap:24px;min-height:500px">
    <!-- Left nav -->
    <nav id="es-nav" style="width:180px;flex-shrink:0">
      ${_renderNav()}
    </nav>

    <!-- Content -->
    <div id="es-content" style="flex:1;min-width:0">
      <div class="spinner-wrap"><div class="spinner"></div></div>
    </div>
  </div>
</div>`;

    await _loadSubPage();
}

// ── Sub-page navigation ──────────────────────────────────────────────────────

function _renderNav() {
    const items = [
        { id: 'general', icon: 'fa-solid fa-gear',    label: t('editorSettings.general', 'General') },
        { id: 'menu',    icon: 'fa-solid fa-sitemap',  label: t('editorSettings.menu',    'Menu') },
        { id: 'stage',   icon: 'fa-solid fa-vector-square', label: t('editorSettings.stage', 'Stage') },
    ];

    return items.map(it => `
        <button class="es-nav-btn${_subPage === it.id ? ' es-nav-active' : ''}"
                onclick="esNav('${it.id}')" style="display:flex;align-items:center;gap:8px;
                width:100%;padding:10px 14px;border:none;border-radius:8px;cursor:pointer;
                font-size:13px;font-weight:${_subPage === it.id ? '600' : '400'};
                background:${_subPage === it.id ? 'var(--bg-surface)' : 'transparent'};
                color:${_subPage === it.id ? 'var(--primary)' : 'var(--text-muted)'};
                border:1px solid ${_subPage === it.id ? 'var(--border)' : 'transparent'};
                transition:all .15s">
            <i class="${it.icon}" style="width:16px;text-align:center"></i>
            <span>${it.label}</span>
        </button>`).join('');
}

window.esNav = async function(page) {
    _subPage = page;
    const nav = document.getElementById('es-nav');
    if (nav) nav.innerHTML = _renderNav();
    await _loadSubPage();
};

async function _loadSubPage() {
    const content = document.getElementById('es-content');
    if (!content) return;

    if (_subPage === 'general') {
        await _renderGeneral(content);
    } else if (_subPage === 'menu') {
        await _renderMenu(content);
    } else if (_subPage === 'stage') {
        await _renderStage(content);
    }
}

// ══════════════════════════════════════════════════════════════════════════════
//  GENERAL — Language + Menu Profile
// ══════════════════════════════════════════════════════════════════════════════

async function _renderGeneral(container) {
    container.innerHTML = '<div class="spinner-wrap"><div class="spinner"></div></div>';

    const r = await api('GET', '/api/v1/profile');
    if (r?.metadata?.status !== 200) {
        container.innerHTML = `<p style="color:var(--danger)">
            ${t('editorSettings.loadError', 'Could not load settings.')}
        </p>`;
        return;
    }

    const { user } = r.data;
    const menuProfileId = r.data.menuProfileId || '';
    const menuProfiles  = r.data.menuProfiles  || [];
    const currentCountry = user.countryCode || '';

    const currentLocale = user.preferredLocale || S.locale;
    const localeOptions = S.locales.length
        ? S.locales.map(l =>
            `<option value="${_esc(l.code)}"${l.code === currentLocale ? ' selected' : ''}>${_esc(l.display)}</option>`
        ).join('')
        : `<option value="${_esc(currentLocale)}">${_esc(currentLocale)}</option>`;

    const menuProfileOptions = [
        `<option value=""${menuProfileId === '' ? ' selected' : ''}>Default</option>`,
        ...menuProfiles.filter(p => !p.is_default).map(p =>
            `<option value="${_esc(p.profile_id)}"${p.profile_id === menuProfileId ? ' selected' : ''}>${_esc(p.name)}</option>`
        ),
    ].join('');

    const countries = [
        { code: '',   label: 'Not set' },
        { code: 'BR', label: 'Brasil' },
        { code: 'US', label: 'United States' },
        { code: 'GB', label: 'United Kingdom' },
        { code: 'DE', label: 'Deutschland' },
        { code: 'FR', label: 'France' },
        { code: 'ES', label: 'España' },
        { code: 'PT', label: 'Portugal' },
        { code: 'IT', label: 'Italia' },
        { code: 'JP', label: 'Japan' },
        { code: 'CN', label: 'China' },
        { code: 'IN', label: 'India' },
        { code: 'AR', label: 'Argentina' },
        { code: 'MX', label: 'México' },
        { code: 'CL', label: 'Chile' },
        { code: 'CO', label: 'Colombia' },
        { code: 'KR', label: 'South Korea' },
        { code: 'AU', label: 'Australia' },
        { code: 'CA', label: 'Canada' },
    ];
    const countryOptions = countries.map(c =>
        `<option value="${_esc(c.code)}"${c.code === currentCountry ? ' selected' : ''}>${c.code ? c.code + ' — ' : ''}${_esc(c.label)}</option>`
    ).join('');

    container.innerHTML = `
<div style="max-width:480px">
  <div id="es-alert" style="display:none;margin-bottom:16px;padding:10px 14px;
       border-radius:8px;font-size:13px"></div>

  <!-- Interface Language -->
  <div style="margin-bottom:24px">
    <label style="display:block;font-size:13px;font-weight:600;color:var(--text-primary);margin-bottom:6px">
      <i class="fa-solid fa-language" style="margin-right:4px;color:var(--primary)"></i>
      ${t('editorSettings.language', 'Interface Language')}
    </label>
    <select class="fc" id="es-locale" onchange="esChangeLocale(this.value)"
            style="max-width:300px">
      ${localeOptions}
    </select>
    <div style="font-size:12px;color:var(--text-muted);margin-top:4px">
      ${t('editorSettings.languageHint', 'Changes take effect immediately.')}
    </div>
  </div>

  <!-- IDE Menu Profile -->
  <div style="margin-bottom:24px">
    <label style="display:block;font-size:13px;font-weight:600;color:var(--text-primary);margin-bottom:6px">
      <i class="fa-solid fa-sitemap" style="margin-right:4px;color:var(--primary)"></i>
      ${t('editorSettings.menuProfile', 'IDE Menu Profile')}
    </label>
    <select class="fc" id="es-menu-profile" onchange="esChangeMenuProfile(this.value)"
            style="max-width:300px">
      ${menuProfileOptions}
    </select>
    <div style="font-size:12px;color:var(--text-muted);margin-top:4px">
      ${t('editorSettings.menuProfileHint', 'Controls which items appear in the IDE sidebar. Reload the IDE to apply.')}
    </div>
  </div>

  <!-- Country -->
  <div style="margin-bottom:24px">
    <label style="display:block;font-size:13px;font-weight:600;color:var(--text-primary);margin-bottom:6px">
      <i class="fa-solid fa-globe" style="margin-right:4px;color:var(--primary)"></i>
      ${t('editorSettings.country', 'Country')}
    </label>
    <select class="fc" id="es-country" onchange="esChangeCountry(this.value)"
            style="max-width:300px">
      ${countryOptions}
    </select>
    <div style="font-size:12px;color:var(--text-muted);margin-top:4px">
      ${t('editorSettings.countryHint', 'Used for menu visibility rules. Some items may only be available in specific countries.')}
    </div>
  </div>
</div>`;
}

window.esChangeLocale = async function(code) {
    const r = await api('PUT', '/api/v1/profile/locale', { locale: code });
    if (r?.metadata?.status === 200) {
        S.locale = code;
        localStorage.setItem('locale', code);
        document.documentElement.lang = code;
        _showAlert('success', t('editorSettings.localeSaved', 'Language updated.'));
    } else {
        _showAlert('danger', r?.metadata?.error || 'Error');
    }
};

window.esChangeMenuProfile = async function(profileId) {
    const r = await api('PUT', '/api/v1/profile/menu-profile', { profileId });
    if (r?.metadata?.status === 200) {
        _showAlert('success', t('editorSettings.menuProfileSaved', 'Menu profile updated. Reload the IDE to apply.'));
    } else {
        _showAlert('danger', r?.metadata?.error || 'Error');
    }
};

window.esChangeCountry = async function(code) {
    const r = await api('PUT', '/api/v1/profile/country', { countryCode: code });
    if (r?.metadata?.status === 200) {
        _showAlert('success', t('editorSettings.countrySaved', 'Country updated. Reload the IDE to apply.'));
    } else {
        _showAlert('danger', r?.metadata?.error || 'Error');
    }
};

// ══════════════════════════════════════════════════════════════════════════════
//  MENU — Checkbox tree for per-item visibility
// ══════════════════════════════════════════════════════════════════════════════

async function _renderMenu(container) {
    container.innerHTML = '<div class="spinner-wrap"><div class="spinner"></div></div>';

    const r = await api('GET', '/api/v1/editor/menu-prefs');
    if (r?.metadata?.status !== 200) {
        container.innerHTML = `<p style="color:var(--danger)">
            ${t('editorSettings.loadError', 'Could not load menu preferences.')}
        </p>`;
        return;
    }

    _menuTree = r.data.tree || [];
    _hidden = new Set(r.data.hidden || []);
    const hideOverlay = r.data.hide_overlay;

    if (hideOverlay) {
        container.innerHTML = `
<div style="padding:24px;text-align:center;color:var(--text-muted)">
  <i class="fa-solid fa-lock" style="font-size:32px;margin-bottom:12px"></i>
  <p style="font-size:14px">${t('editorSettings.prefsDisabled',
            'Menu customization is disabled for your current profile.')}</p>
  <p style="font-size:12px">${t('editorSettings.prefsDisabledHint',
            'Contact an administrator to change your menu profile.')}</p>
</div>`;
        return;
    }

    container.innerHTML = `
<div>
  <div style="display:flex;align-items:center;gap:8px;margin-bottom:12px">
    <input type="text" class="fc" id="es-search" placeholder="${t('editorSettings.searchPlaceholder', 'Search items...')}"
           oninput="esFilterMenu()" style="flex:1;max-width:300px;font-size:13px">
    <button class="btn btn-sm" onclick="esResetPrefs()" style="font-size:12px;white-space:nowrap"
            title="${t('editorSettings.resetTitle', 'Restore all items to admin defaults')}">
      <i class="fa-solid fa-rotate-left"></i> ${t('editorSettings.reset', 'Reset')}
    </button>
  </div>

  <div style="font-size:12px;color:var(--text-muted);margin-bottom:12px">
    ${t('editorSettings.menuHint', 'Uncheck items to hide them from the IDE menu. Changes are saved automatically.')}
  </div>

  <div id="es-tree" style="border:1px solid var(--border);border-radius:8px;
       background:var(--bg-surface);padding:8px 0;max-height:60vh;overflow-y:auto">
    ${_renderTreeCheckboxes(_menuTree, 0)}
  </div>

  <div id="es-save-status" style="margin-top:8px;font-size:12px;color:var(--text-muted);min-height:18px"></div>
</div>`;
}

// ── Render checkbox tree ─────────────────────────────────────────────────────

function _renderTreeCheckboxes(nodes, depth) {
    if (!nodes || nodes.length === 0) return '';

    return nodes.map(node => {
        const indent = depth * 20;
        const isHidden = _hidden.has(node.slot_id);
        const hasChildren = node.children && node.children.length > 0;
        const label = node.label || node.label_fallback || node.slot_id;

        // Skip system items that should not be toggled (Exit, My Items).
        if (node.slot_id === 'SysExit' || node.slot_id === 'SysMyItems') return '';

        // Icon based on slot type.
        const icon = node.slot_type === 'section'
            ? `<i class="fa-solid fa-layer-group" style="color:${_esc(node.color_brand || 'var(--primary)')};width:14px;text-align:center;font-size:11px"></i>`
            : node.slot_type === 'device'
                ? '<i class="fa-solid fa-microchip" style="color:#6c8eff;width:14px;text-align:center;font-size:11px"></i>'
                : node.slot_type === 'template'
                    ? '<i class="fa-solid fa-box-open" style="color:#3a9a5c;width:14px;text-align:center;font-size:11px"></i>'
                    : node.icon_fa
                        ? `<i class="fa-solid fa-${_esc(node.icon_fa)}" style="color:var(--text-muted);width:14px;text-align:center;font-size:11px"></i>`
                        : '<span style="width:14px"></span>';

        const childrenHtml = hasChildren ? _renderTreeCheckboxes(node.children, depth + 1) : '';

        return `
<div class="es-tree-item" data-slot="${_esc(node.slot_id)}" style="opacity:${isHidden ? '0.5' : '1'}">
  <label style="display:flex;align-items:center;gap:6px;padding:5px 12px 5px ${12 + indent}px;
         cursor:pointer;transition:background .1s;font-size:13px"
         onmouseenter="this.style.background='rgba(255,255,255,0.04)'"
         onmouseleave="this.style.background='none'">
    <input type="checkbox" class="es-check"
           data-slot="${_esc(node.slot_id)}"
           ${isHidden ? '' : 'checked'}
           onchange="esToggleItem('${_esc(node.slot_id)}', this.checked)"
           style="width:15px;height:15px;accent-color:var(--primary);flex-shrink:0">
    ${icon}
    <span style="flex:1;font-weight:${depth === 0 ? '600' : '400'}">${_esc(label)}</span>
    ${node.slot_type === 'section'
            ? `<span style="font-size:10px;padding:1px 5px;border-radius:3px;background:${_esc(node.color_brand || '#185FA5')}22;color:${_esc(node.color_brand || '#185FA5')}">${_esc(node.slot_type)}</span>`
            : ''}
  </label>
  ${childrenHtml}
</div>`;
    }).join('');
}

// ── Checkbox handlers ────────────────────────────────────────────────────────

window.esToggleItem = function(slotId, checked) {
    if (checked) {
        _hidden.delete(slotId);
    } else {
        _hidden.add(slotId);
    }

    // Update the visual opacity immediately.
    const item = document.querySelector(`.es-tree-item[data-slot="${slotId}"]`);
    if (item) item.style.opacity = checked ? '1' : '0.5';

    // Auto-save with debounce.
    _scheduleSave();
};

window.esFilterMenu = function() {
    const q = (document.getElementById('es-search')?.value || '').toLowerCase();
    if (!q) {
        // No filter — show everything.
    document.querySelectorAll('.es-tree-item').forEach(el => {
            el.style.display = '';
        });
        return;
    }

    // Recursive: returns true if this element or any descendant matches.
    // When a child matches, all ancestors stay visible (display:'').
    // When nothing matches, the element is hidden.
    function filterNode(el) {
        const label = el.querySelector(':scope > label span')?.textContent?.toLowerCase() || '';
        const slot = el.dataset.slot || '';
        const selfMatch = label.includes(q) || slot.toLowerCase().includes(q);

        // Check children recursively.
        let childMatch = false;
        el.querySelectorAll(':scope > .es-tree-item').forEach(child => {
            if (filterNode(child)) childMatch = true;
    });

        const visible = selfMatch || childMatch;
        el.style.display = visible ? '' : 'none';
        return visible;
    }

    // Apply to root-level items only; recursion handles the rest.
    document.querySelectorAll('#es-tree > .es-tree-item').forEach(el => filterNode(el));
};

window.esResetPrefs = async function() {
    const status = document.getElementById('es-save-status');

    const r = await api('DELETE', '/api/v1/editor/menu-prefs');
    if (r?.metadata?.status === 200) {
        _hidden.clear();
        if (status) status.textContent = t('editorSettings.resetDone', 'All items restored to defaults.');
        // Re-render the tree to update checkboxes.
        const treeEl = document.getElementById('es-tree');
        if (treeEl) treeEl.innerHTML = _renderTreeCheckboxes(_menuTree, 0);
    } else {
        if (status) {
            status.textContent = r?.metadata?.error || 'Error';
            status.style.color = 'var(--danger)';
        }
    }
};

// ── Auto-save with debounce ──────────────────────────────────────────────────

function _scheduleSave() {
    if (_saveTimer) clearTimeout(_saveTimer);
    const status = document.getElementById('es-save-status');
    if (status) {
        status.textContent = t('editorSettings.saving', 'Saving...');
        status.style.color = 'var(--text-muted)';
    }

    _saveTimer = setTimeout(async () => {
        const r = await api('PUT', '/api/v1/editor/menu-prefs', {
            hidden: Array.from(_hidden),
        });
        if (status) {
            if (r?.metadata?.status === 200) {
                status.textContent = t('editorSettings.saved', 'Saved. Reload the IDE to apply.');
                status.style.color = 'var(--success, #16a34a)';
            } else {
                status.textContent = r?.metadata?.error || 'Save failed';
                status.style.color = 'var(--danger)';
            }
        }
    }, 600);
}

// ══════════════════════════════════════════════════════════════════════════════
//  STAGE — Zoom, Pan, Cursor
// ══════════════════════════════════════════════════════════════════════════════
//
// Three controls here:
//
//   - Zoom sensitivity: slider 0.01–0.15, step 0.01. Saves with a
//     debounce so dragging the slider doesn't hit the API on every
//     pixel.
//   - Pan empty area: checkbox. Saves immediately on toggle.
//   - Grab cursor hint: checkbox. Saves immediately on toggle.
//
// A "Reset to defaults" button at the bottom wipes the user's row
// and reloads the page with the server's default values. An info
// banner at the top reminds the user to refresh the IDE to apply.
//
// Português: Três controles — zoom, pan, cursor. Zoom com debounce,
// checkboxes salvam na hora. Banner lembra de atualizar o IDE.

let _stagePrefs    = null;  // resolved user prefs
let _stageDefaults = null;  // server-reported defaults
let _stageSaveTimer = null; // debounce timer for slider

async function _renderStage(container) {
    container.innerHTML = '<div class="spinner-wrap"><div class="spinner"></div></div>';

    const r = await api('GET', '/api/v1/editor/stage-prefs');
    if (r?.metadata?.status !== 200) {
        container.innerHTML = `<p style="color:var(--danger)">
            ${t('editorSettings.loadError', 'Could not load settings.')}
        </p>`;
        return;
    }

    _stagePrefs    = r.data.prefs;
    _stageDefaults = r.data.defaults;

    container.innerHTML = _stageHTML();
    _stageBindEvents();
}

function _stageHTML() {
    // Zoom sensitivity shown as whole-percent for readability
    // (0.03 reads as "3%"). We keep the raw float in state and only
    // format for display.
    const zoomPct = Math.round(_stagePrefs.zoomStep * 100);
    const zoomMinPct = 1;
    const zoomMaxPct = 15;

    return `
<div id="es-alert" style="display:none;padding:10px 14px;border-radius:6px;margin-bottom:16px;font-size:13px"></div>

<div style="background:var(--bg-surface);border:1px solid var(--border);border-radius:8px;padding:16px 20px;margin-bottom:16px;font-size:13px;color:var(--text-muted)">
  <i class="fa-solid fa-circle-info" style="color:var(--primary);margin-right:6px"></i>
  ${t('editorSettings.stage.applyHint', 'Changes apply the next time you open or refresh the IDE.')}
</div>

<h3 style="margin:0 0 12px;font-size:15px;font-weight:600;color:var(--text-primary)">
  ${t('editorSettings.stage.zoomHeading', 'Zoom sensitivity')}
</h3>
<p style="margin:0 0 12px;font-size:13px;color:var(--text-muted);line-height:1.5">
  ${t('editorSettings.stage.zoomHelp', 'How much the stage zooms per scroll notch. Lower values feel smoother on laptop trackpads; higher values are faster on traditional wheel mice.')}
</p>
<div style="display:flex;align-items:center;gap:12px;margin-bottom:24px">
  <input type="range" id="es-stage-zoom"
    min="${zoomMinPct}" max="${zoomMaxPct}" step="1" value="${zoomPct}"
    style="flex:1">
  <span id="es-stage-zoom-val"
    style="min-width:54px;font-family:monospace;font-size:13px;color:var(--text-primary);text-align:right">
    ${zoomPct}%
  </span>
</div>

<h3 style="margin:0 0 12px;font-size:15px;font-weight:600;color:var(--text-primary)">
  ${t('editorSettings.stage.panHeading', 'Pan behaviour')}
</h3>
<label style="display:flex;align-items:flex-start;gap:10px;padding:10px 0;cursor:pointer">
  <input type="checkbox" id="es-stage-pan"
    ${_stagePrefs.panEmptyArea ? 'checked' : ''}
    style="margin-top:3px;flex-shrink:0">
  <span>
    <span style="display:block;font-size:14px;color:var(--text-primary)">
      ${t('editorSettings.stage.panEmptyArea', 'Left-click and drag on an empty spot to move the stage')}
    </span>
    <span style="display:block;font-size:12px;color:var(--text-muted);margin-top:2px">
      ${t('editorSettings.stage.panEmptyAreaHelp', 'Useful on laptops without a middle mouse button. Desktop mouse only; touch devices are unaffected.')}
    </span>
  </span>
</label>

<label style="display:flex;align-items:flex-start;gap:10px;padding:10px 0;cursor:pointer;margin-bottom:24px">
  <input type="checkbox" id="es-stage-grab"
    ${_stagePrefs.showGrabCursor ? 'checked' : ''}
    style="margin-top:3px;flex-shrink:0">
  <span>
    <span style="display:block;font-size:14px;color:var(--text-primary)">
      ${t('editorSettings.stage.showGrabCursor', 'Show grab cursor when hovering empty area')}
    </span>
    <span style="display:block;font-size:12px;color:var(--text-muted);margin-top:2px">
      ${t('editorSettings.stage.showGrabCursorHelp', 'Hint that left-drag will pan the stage. Only visible if the pan option above is on.')}
    </span>
  </span>
</label>

<hr style="border:none;border-top:1px solid var(--border);margin:24px 0">

<button id="es-stage-reset"
    style="padding:8px 14px;border:1px solid var(--border);border-radius:6px;
    background:transparent;color:var(--text-muted);cursor:pointer;font-size:13px">
  <i class="fa-solid fa-rotate-left" style="margin-right:6px"></i>
  ${t('editorSettings.stage.reset', 'Reset to defaults')}
</button>
`;
}

function _stageBindEvents() {
    const zoomSlider = document.getElementById('es-stage-zoom');
    const zoomVal    = document.getElementById('es-stage-zoom-val');
    const panCheck   = document.getElementById('es-stage-pan');
    const grabCheck  = document.getElementById('es-stage-grab');
    const resetBtn   = document.getElementById('es-stage-reset');

    // Zoom slider: update the on-screen percent live, debounce the
    // network save. 400ms matches the pattern in _renderMenu.
    if (zoomSlider) {
        zoomSlider.addEventListener('input', () => {
            const pct = parseInt(zoomSlider.value, 10);
            zoomVal.textContent = pct + '%';
            // Stash the raw float on state even before the save
            // lands — if the user switches tabs and back, the
            // visual already matches.
            _stagePrefs.zoomStep = pct / 100;

            clearTimeout(_stageSaveTimer);
            _stageSaveTimer = setTimeout(async () => {
                await _stageSave({ zoomStep: _stagePrefs.zoomStep });
            }, 400);
        });
    }

    if (panCheck) {
        panCheck.addEventListener('change', async () => {
            _stagePrefs.panEmptyArea = panCheck.checked;
            await _stageSave({ panEmptyArea: panCheck.checked });
        });
    }

    if (grabCheck) {
        grabCheck.addEventListener('change', async () => {
            _stagePrefs.showGrabCursor = grabCheck.checked;
            await _stageSave({ showGrabCursor: grabCheck.checked });
        });
    }

    if (resetBtn) {
        resetBtn.addEventListener('click', async () => {
            // Use the portal's async showConfirm dialog, not the
            // browser's native confirm(). Native prompts are
            // forbidden by CLAUDE_KNOWN_ISSUES.md §3.2 — they
            // freeze the JS main thread, break on some mobile
            // contexts, and look out of place against the rest
            // of the UI.
            //
            // Português: Usa o diálogo showConfirm do portal em
            // vez do confirm() nativo. Pegadinha #3.2 de
            // CLAUDE_KNOWN_ISSUES.md — nativos travam a thread
            // principal, falham em alguns contextos mobile, e
            // destoam visualmente do resto da UI.
            const ok = await showConfirm(
                t('editorSettings.stage.resetConfirm',
                    'Reset all stage preferences to defaults?'),
                t('common.reset', 'Reset'),
                t('common.cancel', 'Cancel'),
            );
            if (!ok) return;
            const r = await api('DELETE', '/api/v1/editor/stage-prefs');
            if (r?.metadata?.status !== 200) {
                _showAlert('danger',
                    t('editorSettings.saveError', 'Could not save. Please try again.'));
                return;
            }
            _stagePrefs    = r.data.prefs;
            _stageDefaults = r.data.defaults;
            // Re-render the panel with the fresh values.
            const container = document.getElementById('es-content');
            if (container) {
                container.innerHTML = _stageHTML();
                _stageBindEvents();
            }
            _showAlert('success',
                t('editorSettings.stage.resetOk', 'Reset to defaults.'));
        });
    }
}

// _stageSave sends a partial PUT. On failure it shows an alert but
// leaves the UI optimistic — the next successful save will reconcile.
// Not rolling back on failure is a deliberate UX choice: if the
// server is briefly unavailable, the user should not see their
// slider snap back while they're still adjusting it.
async function _stageSave(patch) {
    const r = await api('PUT', '/api/v1/editor/stage-prefs', patch);
    if (r?.metadata?.status !== 200) {
        _showAlert('danger',
            t('editorSettings.saveError', 'Could not save. Please try again.'));
        return;
    }
    _stagePrefs    = r.data.prefs;
    _stageDefaults = r.data.defaults;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function _showAlert(type, msg) {
    const el = document.getElementById('es-alert');
    if (!el) return;
    el.style.display = 'block';
    el.style.background = type === 'success' ? 'var(--success-bg, #d1fae5)' : 'var(--danger-bg, #fef2f2)';
    el.style.color = type === 'success' ? 'var(--success, #065f46)' : 'var(--danger, #b91c1c)';
    el.textContent = msg;
    setTimeout(() => { el.style.display = 'none'; }, 4000);
}

function _esc(s) {
    return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/"/g,'&quot;');
}
