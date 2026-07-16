// /ide/server/public/control/js/pages/wizardExamples.js — School gallery curation.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Two views in one page module (design session 2026-07-15):
//   LIST   — the curation table (order, visibility, quick actions).
//   EDITOR — a FULL PAGE beside the sidebar ("a janela tem que ser mais
//            larga… use o espaço inteiro"): left = what the student sees,
//            LIVE, plus the frozen file snapshot; right = lesson steps as
//            an accordion whose collapsed rows read as sentences.
// The machine writes the JSON; the human declares intent — same
// philosophy as the wizard's port modal. Only the quick "Snapshot from
// project" prompt remains a modal.
//
// Português: Duas visões num módulo: LISTA (curadoria) e EDITOR (página
// inteira ao lado do sidebar): esquerda = o que o aluno vê, AO VIVO, e o
// snapshot congelado; direita = passos em acordeão cujas linhas fechadas
// se leem como frases. A máquina escreve o JSON; o humano declara
// intenção. Só o "Snapshot from project" segue modal.

import { cpApi }              from '../api.js';
import { cpToast, cpConfirm } from '../toast.js';
import { esc }                from '../utils.js';

const API = '/api/control/v1/school/examples';

let _root = null;

// _ensureMonaco lazy-loads the SAME Monaco the portal serves at
// /monaco/vs — same task, same tool (design correction 2026-07-15: the
// admin was getting a worse editor than the user). Português: Carrega o
// MESMO Monaco do portal — mesma tarefa, mesma ferramenta.
let _monacoP = null;
function _ensureMonaco() {
    if (window.monaco) return Promise.resolve(window.monaco);
    if (_monacoP) return _monacoP;
    _monacoP = new Promise((resolve, reject) => {
        const s = document.createElement('script');
        s.src = '/monaco/vs/loader.js';
        s.onload = () => {
            window.require.config({ paths: { vs: '/monaco/vs' } });
            window.require(['vs/editor/editor.main'],
                () => resolve(window.monaco));
        };
        s.onerror = () => reject(new Error('monaco loader failed'));
        document.head.appendChild(s);
    });
    return _monacoP;
}

function _langOf(path) {
    const p = (path || '').toLowerCase();
    if (p.endsWith('.c') || p.endsWith('.h')) return 'c';
    if (p.endsWith('.go'))   return 'go';
    if (p.endsWith('.json')) return 'json';
    if (p.endsWith('.md'))   return 'markdown';
    if (p.endsWith('.yaml') || p.endsWith('.yml')) return 'yaml';
    if (p.endsWith('.html')) return 'html';
    if (p.endsWith('.css'))  return 'css';
    return 'plaintext';
}

export async function renderWizardExamples(root) {
    _root = root;
    await _renderList();
}

// ═════════════════════════════════════════════════════════════════════════
//  LIST view
// ═════════════════════════════════════════════════════════════════════════

async function _renderList() {
    _root.innerHTML = `
<div class="cp-page">
  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:18px">
    <h2 style="margin:0"><i class="fa-solid fa-graduation-cap" style="color:var(--primary);margin-right:8px"></i>School</h2>
    <button class="cp-btn cp-btn-primary" id="cp-school-snap">
      <i class="fa-solid fa-camera"></i> Snapshot from project</button>
  </div>
  <p style="margin:0 0 16px;font-size:13px;color:var(--text-dim)">
    Examples shown under Devices → + New → School. Snapshots are frozen —
    editing the source project changes nothing here until you re-snapshot.
  </p>
  <div id="cp-school-list"><i class="fa-solid fa-spinner fa-spin"></i></div>
</div>`;
    document.getElementById('cp-school-snap')
        .addEventListener('click', () => _snapModal());

    const box = document.getElementById('cp-school-list');
    const r = await cpApi('GET', API);
    if (r?.metadata?.status !== 200) {
        box.innerHTML = `<p style="color:var(--danger)">${esc(r?.metadata?.error || 'Load failed.')}</p>`;
        return;
    }
    const list = r.data?.examples || [];
    if (!list.length) {
        box.innerHTML = '<p style="color:var(--text-dim)">No examples yet — snapshot a project to plant the first.</p>';
        return;
    }
    box.innerHTML = `
<table class="cp-table">
  <thead><tr>
    <th>Ord</th><th>Lang</th><th>Level</th><th>Title</th>
    <th>Files</th><th>Steps</th><th>Source</th><th>Visible</th><th></th>
  </tr></thead>
  <tbody>
    ${list.map(e => `
    <tr>
      <td>${e.ord}</td>
      <td><code>${esc(e.language)}</code></td>
      <td>${e.level === 'mission'
            ? '<span class="cp-badge" style="background:rgba(150,100,255,.18);color:#a98bff">mission</span>'
            : '<span class="cp-badge" style="background:rgba(40,180,100,.18);color:#37c07a">ready</span>'}</td>
      <td><strong>${esc(e.title)}</strong><br>
          <span style="font-size:11px;color:var(--text-dim)">${esc(e.id)}</span></td>
      <td>${e.filesBytes} B</td>
      <td>${e.stepsBytes > 2 ? e.stepsBytes + ' B' : '—'}</td>
      <td>${e.sourceProjectId
            ? `<code style="font-size:11px">${esc(e.sourceProjectId)}</code>`
            : '<span style="color:var(--text-dim)">seed</span>'}</td>
      <td>
        <button class="cp-btn cp-btn-ghost cp-btn-sm" data-vis="${esc(e.id)}"
                data-vis-now="${e.visible ? 1 : 0}"
                title="${e.visible ? 'Visible — click to hide' : 'Hidden — click to show'}">
          <i class="fa-solid ${e.visible ? 'fa-eye' : 'fa-eye-slash'}"
             style="${e.visible ? '' : 'color:var(--text-dim)'}"></i>
        </button>
      </td>
      <td style="white-space:nowrap">
        <button class="cp-btn cp-btn-ghost cp-btn-sm" title="Edit lesson" data-edit="${esc(e.id)}">
          <i class="fa-solid fa-pen"></i></button>
        <button class="cp-btn cp-btn-ghost cp-btn-sm" title="Delete" data-del="${esc(e.id)}">
          <i class="fa-solid fa-trash" style="color:var(--danger)"></i></button>
      </td>
    </tr>`).join('')}
  </tbody>
</table>`;
    box.querySelectorAll('[data-edit]').forEach(b =>
        b.addEventListener('click', () => _renderEditor(b.dataset.edit)));
    box.querySelectorAll('[data-del]').forEach(b =>
        b.addEventListener('click', () => _del(b.dataset.del)));
    // Visibility is a one-click affair (field: "finalizar o /control"):
    // the eye flips the flag via GET→PUT of the whole row — the form's
    // word stays final, the trip to the editor stays optional.
    // Português: Visibilidade em um clique — o olho alterna via GET→PUT
    // da linha inteira; abrir o editor fica opcional.
    box.querySelectorAll('[data-vis]').forEach(b =>
        b.addEventListener('click', async () => {
            const id = b.dataset.vis;
            const r = await cpApi('GET', `${API}/${encodeURIComponent(id)}`);
            if (r?.metadata?.status !== 200) {
                cpToast('danger', r?.metadata?.error || 'Load failed.');
                return;
            }
            const e = r.data?.example || {};
            const pr = await cpApi('PUT', `${API}/${encodeURIComponent(id)}`, {
                language: e.language, level: e.level, title: e.title,
                subtitle: e.subtitle, ord: e.ord,
                visible: !(b.dataset.visNow === '1'),
                steps: e.steps, files: e.files,
                sourceProjectId: r.data?.sourceProjectId || '',
            });
            if (pr?.metadata?.status === 200) {
                await _renderList();
            } else {
                cpToast('danger', pr?.metadata?.error || 'Toggle failed.');
            }
        }));
}

async function _del(id) {
    const yes = await cpConfirm('Delete example',
        `Remove "${id}" from the School permanently?`, 'Delete');
    if (!yes) return;
    const r = await cpApi('DELETE', `${API}/${encodeURIComponent(id)}`);
    if (r?.metadata?.status === 200) {
        cpToast('success', 'Example deleted.');
        await _renderList();
    } else {
        cpToast('danger', r?.metadata?.error || 'Delete failed.');
    }
}

// ═════════════════════════════════════════════════════════════════════════
//  EDITOR view — the full page
// ═════════════════════════════════════════════════════════════════════════

const CHECK_KINDS = [
    { kind: 'parseOk',        needsParse: true,  args: [] },
    { kind: 'fileIsDict',     needsParse: false, args: ['path', 'minItems'] },
    { kind: 'portHasLang',    needsParse: true,  args: ['fn', 'port'] },
    { kind: 'portHasDict',    needsParse: true,  args: ['fn', 'port'] },
    { kind: 'sliceCollapsed', needsParse: true,  args: ['fn', 'port'] },
];

// _asArray: json.RawMessage arrives from fetch ALREADY PARSED, but a
// hand-edited row may hold a string — accept both (the "[object Object]"
// lesson of 2026-07-15). Português: RawMessage chega parseado; linha
// editada à mão pode ser string — aceita os dois.
function _asArray(v) {
    if (Array.isArray(v)) return v;
    if (typeof v === 'string') {
        try { const p = JSON.parse(v || '[]'); return Array.isArray(p) ? p : []; }
        catch (_) { return []; }
    }
    return [];
}

// _checkSentence renders a predicate as PROSE — the collapsed accordion
// row and the student-preview both lean on it. Português: O predicado
// como FRASE — a linha fechada do acordeão e o preview usam isto.
function _checkSentence(check, files) {
    const k = check?.kind || 'parseOk';
    switch (k) {
        case 'parseOk':
            return 'the project parses clean';
        case 'fileIsDict': {
            const mi = check.minItems ? ` with at least ${check.minItems} item(s)` : '';
            return `the file <code>${esc(check.path || '?')}</code> is a dictionary${mi}`;
        }
        case 'portHasLang':
            return `the port <code>${esc(check.fn || '?')}.${esc(check.port || '?')}</code> has an editor language`;
        case 'portHasDict':
            return `the port <code>${esc(check.fn || '?')}.${esc(check.port || '?')}</code> has a dictionary`;
        case 'sliceCollapsed':
            return `the port <code>${esc(check.fn || '?')}.${esc(check.port || '?')}</code> collapsed into a collection`;
        default:
            return k;
    }
}

// Local dictionary sniff — mirrors the wizard's: an array where every
// item carries a string label. Used by "Test mission".
function _isDict(content, minItems) {
    try {
        const v = JSON.parse(content || '');
        if (!Array.isArray(v) || !v.length) return false;
        if (!v.every(it => it && typeof it.label === 'string')) return false;
        return !minItems || v.length >= minItems;
    } catch (_) { return false; }
}

async function _renderEditor(id) {
    const r = await cpApi('GET', `${API}/${encodeURIComponent(id)}`);
    if (r?.metadata?.status !== 200) {
        cpToast('danger', r?.metadata?.error || 'Load failed.');
        return;
    }
    const e = r.data?.example || {};
    const state = {
        id,
        title: e.title || '',
        subtitle: e.subtitle || '',
        language: e.language || 'c',
        level: e.level || 'ready',
        ord: e.ord ?? 0,
        visible: r.data?.visible ?? true,
        source: r.data?.sourceProjectId || '',
        steps: _asArray(e.steps),
        files: _asArray(e.files),
        open: 0,   // which step is expanded
        tab: 0,    // active file tab
    };

    _root.innerHTML = `
<div class="cp-page">
  <div class="cp-lesson-head">
    <button class="cp-btn cp-btn-ghost" id="cpl-back">
      <i class="fa-solid fa-arrow-left"></i> School</button>
    <input class="cp-input cp-lesson-title" id="cpl-title" value="${esc(state.title)}"
           placeholder="Example title">
    <select class="cp-input" id="cpl-level" style="width:auto">
      <option value="ready"   ${state.level === 'ready' ? 'selected' : ''}>ready — open &amp; tweak</option>
      <option value="mission" ${state.level === 'mission' ? 'selected' : ''}>mission — guided steps</option>
    </select>
    <label style="display:flex;gap:6px;align-items:center;font-size:13px;white-space:nowrap">
      <input type="checkbox" id="cpl-vis" ${state.visible ? 'checked' : ''}> Visible</label>
    <button class="cp-btn cp-btn-primary" id="cpl-save">
      <i class="fa-solid fa-floppy-disk"></i> Save</button>
  </div>
  <input class="cp-input" id="cpl-sub" value="${esc(state.subtitle)}"
         placeholder="Subtitle — one line the card shows"
         style="width:100%;margin:8px 0 4px">
  <div style="display:flex;gap:14px;align-items:center;font-size:12px;color:var(--text-dim);margin-bottom:16px">
    <span>id <code>${esc(id)}</code></span>
    <label>lang
      <select class="cp-input cp-btn-sm" id="cpl-lang" style="width:auto;margin-left:4px">
        <option value="c"  ${state.language === 'c' ? 'selected' : ''}>c</option>
        <option value="go" ${state.language === 'go' ? 'selected' : ''}>go</option>
      </select></label>
    <label>order
      <input class="cp-input cp-btn-sm" id="cpl-ord" type="number" value="${state.ord}"
             style="width:64px;margin-left:4px"></label>
    ${state.source ? `<span>source <code>${esc(state.source)}</code></span>` : '<span>seed — no source project</span>'}
  </div>

  <div class="cp-lesson-grid">
    <div>
      <div class="cp-lesson-tabs" id="cpl-tabs"></div>
      <div id="cpl-monaco" class="cp-lesson-monaco"></div>
      <p style="margin:8px 0 0;font-size:12px;color:var(--text-dim)" id="cpl-source"></p>
      <input type="file" id="cpl-upload" multiple style="display:none">
    </div>

    <div>
      <p class="cp-lesson-label">What the student sees — live</p>
      <div class="cp-lesson-card" id="cpl-preview" style="margin-bottom:14px"></div>
      <div style="display:flex;align-items:center;margin-bottom:8px">
        <p class="cp-lesson-label" style="margin:0">Lesson steps</p>
        <span style="flex:1"></span>
        <button class="cp-btn cp-btn-ghost cp-btn-sm" id="cpl-step-add">
          <i class="fa-solid fa-plus"></i> Add step</button>
      </div>
      <div id="cpl-steps"></div>
      <div class="cp-lesson-test" id="cpl-test"></div>
    </div>
  </div>
</div>`;

    // ── header wiring ─────────────────────────────────────────────────────
    const $ = (sel) => _root.querySelector(sel);

    // Focus-follows-work (field design 2026-07-15: "eu gosto que o monaco
    // ocupe mais espaço quando eu for trabalhar… clicar nele e ele
    // expandir, e o mesmo com o editor de etapas"): the GESTURE is the
    // intent — mousedown on a column grows it (3:1), the other shrinks
    // but stays readable (closed steps are one-line sentences by design;
    // the open step re-expands its column the moment you touch it). No
    // buttons, no learning curve; Monaco relayouts via automaticLayout.
    // Português: O layout segue o FOCO — mousedown numa coluna a expande
    // (3:1); a outra encolhe mas segue legível. Sem botões.
    {
        const grid = _root.querySelector('.cp-lesson-grid');
        const cols = grid ? grid.children : [];
        if (grid && cols.length === 2) {
            cols[0].addEventListener('mousedown', () => {
                grid.classList.add('focus-left');
                grid.classList.remove('focus-right');
            }, true);
            cols[1].addEventListener('mousedown', () => {
                grid.classList.add('focus-right');
                grid.classList.remove('focus-left');
            }, true);
        }
    }
    $('#cpl-back').addEventListener('click', () => _renderList());
    ['#cpl-title', '#cpl-sub'].forEach(sel =>
        $(sel).addEventListener('input', () => { _harvestHead(); renderPreview(); }));
    $('#cpl-level').addEventListener('change', () => { _harvestHead(); renderAll(); });
    $('#cpl-save').addEventListener('click', save);
    $('#cpl-step-add').addEventListener('click', () => {
        harvestSteps();
        state.steps.push({ title: '', detail: '', check: { kind: 'parseOk' } });
        state.open = state.steps.length - 1;
        renderAll();
    });

    function _harvestHead() {
        state.title = $('#cpl-title').value.trim();
        state.subtitle = $('#cpl-sub').value.trim();
        state.level = $('#cpl-level').value;
        state.language = $('#cpl-lang').value;
        state.ord = parseInt($('#cpl-ord').value, 10) || 0;
        state.visible = $('#cpl-vis').checked;
    }

    // ── student preview (left) ────────────────────────────────────────────
    function renderPreview() {
        const box = $('#cpl-preview');
        if (state.level !== 'mission') {
            box.innerHTML = `
              <span class="cp-badge" style="background:rgba(40,180,100,.18);color:#37c07a">Ready to run</span>
              <p style="margin:8px 0 2px;font-size:15px;font-weight:600">${esc(state.title || 'Untitled')}</p>
              <p style="margin:0;font-size:12.5px;color:var(--text-dim)">${esc(state.subtitle || '')}</p>
              <p style="margin:10px 0 0;font-size:12px;color:var(--text-dim)">
                Opens as a project — no steps, the student explores.</p>`;
            return;
        }
        box.innerHTML = `
          <p style="margin:0 0 10px;font-size:11px;letter-spacing:.05em;color:var(--text-dim)">
            MISSION · 0 OF ${state.steps.length}</p>
          ${state.steps.map((s, i) => i === 0 ? `
          <div style="display:flex;gap:8px;background:rgba(150,100,255,.12);border-radius:8px;padding:8px">
            <i class="fa-solid fa-1" style="margin-top:3px"></i>
            <div>
              <p style="margin:0;font-size:13px;font-weight:600">${esc(s.title || '(untitled step)')}</p>
              <p style="margin:2px 0 0;font-size:12px;opacity:.85">${esc(s.detail || '')}</p>
              ${s.action?.kind === 'openPort'
                ? `<button class="cp-btn cp-btn-sm" style="margin-top:6px" disabled>Open ${esc(s.action.port || '?')} port</button>` : ''}
            </div>
          </div>` : `
          <div style="display:flex;gap:8px;padding:8px;opacity:.5">
            <i class="fa-solid fa-${i + 1}" style="margin-top:3px"></i>
            <p style="margin:0;font-size:13px">${esc(s.title || '(untitled step)')}</p>
          </div>`).join('')
            || '<p style="font-size:13px;color:var(--text-dim)">No steps yet.</p>'}`;
    }

    // ── file editor (left): wizard-style tabs + Monaco ───────────────────
    // The models ARE the truth while the page lives; Save harvests them.
    // Português: Os models SÃO a verdade enquanto a página vive; o Save
    // os colhe.
    let editor = null;
    const models = new Map(); // path → monaco model

    const _isBinary = (f) => f?.encoding === 'base64';

    function modelFor(i) {
        const f = state.files[i];
        if (!f) return null;
        if (_isBinary(f)) return null; // assets have no text model
        if (!models.has(f.path)) {
            models.set(f.path, monaco.editor.createModel(
                f.content || '', _langOf(f.path)));
        }
        return models.get(f.path);
    }

    function harvestFiles() {
        // Mutates in place — every field the bundle carries (kind:"help"
        // for manuals, 2026-07-15) survives the harvest untouched.
        // Português: Muta no lugar — o kind dos manuais sobrevive.
        state.files.forEach(f => {
            const m = models.get(f.path);
            if (m) f.content = m.getValue();
        });
    }

    function renderTabs() {
        const box = $('#cpl-tabs');
        box.innerHTML = `
          ${state.files.map((f, i) => `
          <span class="cp-lesson-tab ${i === state.tab ? 'is-active' : ''}" data-tab="${i}">
            ${f.kind === 'help' ? '<i class="fa-solid fa-book" title="Manual file" style="font-size:10px;opacity:.7"></i> ' : ''}${_isBinary(f) ? '<i class="fa-solid fa-image" title="Image asset"></i> ' : ''}${esc(f.path || '(unnamed)')}
            <i class="fa-solid fa-xmark" data-tab-rm="${i}" title="Remove file"></i>
          </span>`).join('')}
          <button class="cp-btn cp-btn-ghost cp-btn-sm" id="cpl-file-new" title="New file">
            <i class="fa-solid fa-plus"></i></button>
          <button class="cp-btn cp-btn-ghost cp-btn-sm" id="cpl-file-up" title="Upload files">
            <i class="fa-solid fa-upload"></i></button>
          <span id="cpl-file-namer" style="display:none">
            <input class="cp-input cp-btn-sm" id="cpl-file-name"
                   placeholder="new_file.c" style="width:150px"></span>`;

        box.querySelectorAll('[data-tab]').forEach(t =>
            t.addEventListener('click', () => {
                harvestFiles();
                state.tab = +t.dataset.tab;
                renderTabs();
                const m = modelFor(state.tab);
                editor?.setModel(m);
                // Binary asset: no text to edit — say so where the code
                // would be. Português: Binário não se edita — avisa.
                $('#cpl-monaco').style.opacity = m ? '1' : '.25';
                if (!m) cpToast('info',
                    'Image asset — replace it by uploading a file with the same name.');
            }));
        box.querySelectorAll('[data-tab-rm]').forEach(x =>
            x.addEventListener('click', async (ev) => {
                ev.stopPropagation();
                const i = +x.dataset.tabRm;
                const f = state.files[i];
                const yes = await cpConfirm('Remove file',
                    `Remove "${f?.path}" from this example?`, 'Remove');
                if (!yes) return;
                harvestFiles();
                models.get(f.path)?.dispose();
                models.delete(f.path);
                state.files.splice(i, 1);
                state.tab = Math.max(0, Math.min(state.tab, state.files.length - 1));
                renderTabs();
                editor?.setModel(state.files.length ? modelFor(state.tab) : null);
                renderTest();
            }));
        box.querySelector('#cpl-file-new').addEventListener('click', () => {
            const wrap = box.querySelector('#cpl-file-namer');
            wrap.style.display = 'inline';
            const inp = box.querySelector('#cpl-file-name');
            inp.focus();
            inp.addEventListener('keydown', (ev) => {
                if (ev.key === 'Escape') { wrap.style.display = 'none'; return; }
                if (ev.key !== 'Enter') return;
                const name = inp.value.trim();
                if (!name) return;
                harvestFiles();
                state.files.push({ path: name, content: '' });
                state.tab = state.files.length - 1;
                renderTabs();
                editor?.setModel(modelFor(state.tab));
            });
        });
        box.querySelector('#cpl-file-up').addEventListener('click', () =>
            $('#cpl-upload').click());
    }

    $('#cpl-upload').addEventListener('change', (ev) => {
        harvestFiles();
        const picked = Array.from(ev.target.files || []);
        let pending = picked.length;
        picked.forEach(file => {
            // Images ride as base64 assets (kind:help — the manual is
            // their only home); .md defaults to help too (readmes);
            // everything else is code. Português: Imagem = asset base64
            // do manual; .md = manual; o resto é código.
            const isImg = /\.(png|jpe?g|gif|webp|svg)$/i.test(file.name)
                || (file.type || '').startsWith('image/');
            const rd = new FileReader();
            rd.onload = () => {
                const existing = state.files.find(f => f.path === file.name);
                const entry = existing
                    || { path: file.name, content: '' };
                if (isImg) {
                    entry.content = String(rd.result || '').split(',')[1] || '';
                    entry.encoding = 'base64';
                    entry.kind = 'help';
                    models.get(entry.path)?.dispose();
                    models.delete(entry.path);
                } else {
                    entry.content = String(rd.result || '');
                    delete entry.encoding;
                    if (/\.md$/i.test(file.name) && !entry.kind) entry.kind = 'help';
                    models.get(entry.path)?.setValue(entry.content);
                }
                if (!existing) state.files.push(entry);
                if (--pending === 0) {
                    state.tab = state.files.length - 1;
                    renderTabs();
                    editor?.setModel(modelFor(state.tab));
                    renderTest();
                }
            };
            if (isImg) rd.readAsDataURL(file); else rd.readAsText(file);
        });
        ev.target.value = '';
    });

    $('#cpl-source').innerHTML = state.source
        ? `source <code>${esc(state.source)}</code> ·
           <span id="cpl-resnap" style="color:var(--primary);cursor:pointer">Re-snapshot from project</span>`
        : 'seed — files live only in this example';
    $('#cpl-source').querySelector('#cpl-resnap')
        ?.addEventListener('click', () => _resnap());

    _ensureMonaco().then(() => {
        editor = monaco.editor.create($('#cpl-monaco'), {
            theme: 'vs-dark',
            fontSize: 13,
            minimap: { enabled: false },
            scrollBeyondLastLine: false,
            automaticLayout: true,
        });
        if (state.files.length) editor.setModel(modelFor(state.tab));
        // Live test: the dictionary predicates watch the JSON as you type.
        editor.onDidChangeModelContent(() => { harvestFiles(); renderTest(); });
    }).catch(() => {
        $('#cpl-monaco').textContent = 'Monaco failed to load — reload the page.';
    });

    async function _resnap() {
        const yes = await cpConfirm('Re-snapshot',
            'Freeze the source project\u2019s LATEST saved version over these files? Steps and metadata are kept.',
            'Re-snapshot');
        if (!yes) return;
        const rr = await cpApi('POST', `${API}/from-project`, {
            projectId: state.source, id: state.id, level: state.level,
            title: state.title, subtitle: state.subtitle,
            ord: state.ord, visible: state.visible,
        });
        if (rr?.metadata?.status !== 200) {
            cpToast('danger', rr?.metadata?.error || 'Snapshot failed.');
            return;
        }
        // from-project resets steps — write them back, then reload files.
        harvestSteps();
        await _putState();
        const fresh = await cpApi('GET', `${API}/${encodeURIComponent(state.id)}`);
        state.files = _asArray(fresh?.data?.example?.files);
        models.forEach(m => m.dispose());
        models.clear();
        state.tab = 0;
        cpToast('success', `Re-snapshot done (${rr.data?.files} file(s)).`);
        renderAll();
    }

    // ── steps accordion (right) ───────────────────────────────────────────
    function stepRow(s, i) {
        if (i !== state.open) {
            return `
            <div class="cp-lesson-step" data-open="${i}">
              <strong style="font-size:13px">Step ${i + 1} · ${esc(s.title || '(untitled)')}</strong>
              <span style="flex:1"></span>
              <span style="font-size:12px;color:var(--text-dim)">
                Done when ${_checkSentence(s.check, state.files)}${s.action?.kind === 'openPort' ? ' · button opens it' : ''}</span>
            </div>`;
        }
        const kind = s.check?.kind || 'parseOk';
        const meta = CHECK_KINDS.find(k => k.kind === kind) || CHECK_KINDS[0];
        const fileOpts = state.files.map(f =>
            `<option value="${esc(f.path)}" ${s.check?.path === f.path ? 'selected' : ''}>${esc(f.path)}</option>`).join('');
        return `
        <div class="cp-lesson-step is-open">
          <div style="display:flex;align-items:center;gap:4px;width:100%">
            <strong style="font-size:13px">Step ${i + 1}</strong>
            <span style="flex:1"></span>
            <button class="cp-btn cp-btn-ghost cp-btn-sm" data-up="${i}"   title="Move up"><i class="fa-solid fa-arrow-up"></i></button>
            <button class="cp-btn cp-btn-ghost cp-btn-sm" data-down="${i}" title="Move down"><i class="fa-solid fa-arrow-down"></i></button>
            <button class="cp-btn cp-btn-ghost cp-btn-sm" data-rm="${i}"   title="Remove"><i class="fa-solid fa-trash" style="color:var(--danger)"></i></button>
          </div>
          <input class="cp-input" data-f="title" value="${esc(s.title || '')}"
                 placeholder="Title — what the user sees" style="width:100%;margin:6px 0">
          <input class="cp-input" data-f="detail" value="${esc(s.detail || '')}"
                 placeholder="Detail — the nudge under the current step" style="width:100%;margin-bottom:10px">
          <div class="cp-lesson-sentence">
            <span>Done when</span>
            <select class="cp-input" data-f="kind" style="width:auto">
              <option value="parseOk"        ${kind === 'parseOk' ? 'selected' : ''}>the project parses clean</option>
              <option value="fileIsDict"     ${kind === 'fileIsDict' ? 'selected' : ''}>a file is a dictionary…</option>
              <option value="portHasLang"    ${kind === 'portHasLang' ? 'selected' : ''}>a port has a language…</option>
              <option value="portHasDict"    ${kind === 'portHasDict' ? 'selected' : ''}>a port has a dictionary…</option>
              <option value="sliceCollapsed" ${kind === 'sliceCollapsed' ? 'selected' : ''}>a port collapsed…</option>
            </select>
            ${meta.args.includes('path') ? `
            <span>— the file</span>
            <select class="cp-input" data-f="path" style="width:auto">${fileOpts}</select>` : ''}
            ${meta.args.includes('minItems') ? `
            <span>with at least</span>
            <input class="cp-input" data-f="minItems" type="number"
                   value="${esc(String(s.check?.minItems ?? ''))}" style="width:64px" placeholder="2">
            <span>items</span>` : ''}
            ${meta.args.includes('fn') ? `
            <span>— function</span>
            <input class="cp-input" data-f="fn" value="${esc(s.check?.fn || '')}" style="width:120px" placeholder="app_config">
            <span>port</span>
            <input class="cp-input" data-f="port" value="${esc(s.check?.port || '')}" style="width:90px" placeholder="cfg">` : ''}
          </div>
          <label style="display:flex;gap:8px;align-items:center;font-size:13px;margin:8px 0 2px">
            <input type="checkbox" data-f="actOn" ${s.action?.kind === 'openPort' ? 'checked' : ''}>
            Button on the step: opens a port's editor</label>
          <div class="cp-lesson-sentence" data-act
               style="display:${s.action?.kind === 'openPort' ? 'flex' : 'none'}">
            <span>function</span>
            <input class="cp-input" data-f="actFn" value="${esc(s.action?.fn || '')}" style="width:120px" placeholder="app_config">
            <span>port</span>
            <input class="cp-input" data-f="actPort" value="${esc(s.action?.port || '')}" style="width:90px" placeholder="cfg">
          </div>
        </div>`;
    }

    function harvestSteps() {
        const card = $('#cpl-steps .cp-lesson-step.is-open');
        if (!card) return;
        const i = state.open;
        if (i < 0 || i >= state.steps.length) return;
        const v = (f) => card.querySelector(`[data-f="${f}"]`)?.value?.trim() ?? '';
        const kind = v('kind') || 'parseOk';
        const check = { kind };
        if (kind === 'fileIsDict') {
            check.path = v('path');
            const mi = parseInt(v('minItems'), 10);
            if (mi > 0) check.minItems = mi;
        } else if (kind !== 'parseOk') {
            check.fn = v('fn');
            check.port = v('port');
        }
        const s = { title: v('title'), detail: v('detail'), check };
        if (card.querySelector('[data-f="actOn"]')?.checked) {
            s.action = { kind: 'openPort', fn: v('actFn'), port: v('actPort') };
        }
        state.steps[i] = s;
    }

    function renderSteps() {
        const box = $('#cpl-steps');
        if (state.level !== 'mission') {
            box.innerHTML = `<p style="font-size:13px;color:var(--text-dim)">
              A "ready" example has no steps — switch Level to
              <strong>mission</strong> to write a lesson.</p>`;
            return;
        }
        box.innerHTML = state.steps.map(stepRow).join('')
            || '<p style="font-size:13px;color:var(--text-dim)">No steps yet — a mission needs at least one.</p>';

        box.querySelectorAll('[data-open]').forEach(row =>
            row.addEventListener('click', () => {
                harvestSteps();
                state.open = +row.dataset.open;
                renderAll();
            }));
        box.querySelectorAll('[data-rm]').forEach(b =>
            b.addEventListener('click', (ev) => {
                ev.stopPropagation();
                harvestSteps();
                state.steps.splice(+b.dataset.rm, 1);
                state.open = Math.max(0, state.open - 1);
                renderAll();
            }));
        const move = (i, d) => {
            harvestSteps();
            const j = i + d;
            if (j < 0 || j >= state.steps.length) return;
            [state.steps[i], state.steps[j]] = [state.steps[j], state.steps[i]];
            state.open = j;
            renderAll();
        };
        box.querySelectorAll('[data-up]').forEach(b =>
            b.addEventListener('click', (ev) => { ev.stopPropagation(); move(+b.dataset.up, -1); }));
        box.querySelectorAll('[data-down]').forEach(b =>
            b.addEventListener('click', (ev) => { ev.stopPropagation(); move(+b.dataset.down, 1); }));
        box.querySelectorAll('.is-open [data-f]').forEach(inp =>
            inp.addEventListener('input', () => { harvestSteps(); renderPreview(); renderTest(); }));
        box.querySelector('.is-open [data-f="kind"]')
            ?.addEventListener('change', () => { harvestSteps(); renderAll(); });
        box.querySelector('.is-open [data-f="actOn"]')
            ?.addEventListener('change', (ev) => {
                const act = ev.target.closest('.is-open').querySelector('[data-act]');
                act.style.display = ev.target.checked ? 'flex' : 'none';
                harvestSteps();
                renderPreview();
            });
    }

    // ── Test mission (right, bottom) ─────────────────────────────────────
    // File predicates run HERE against the snapshot; parse-dependent ones
    // are honest about needing the project. The classic authoring bug —
    // a step that is ALREADY TRUE with the starter files — is called out.
    // Português: Predicados de arquivo rodam AQUI; os que dependem de
    // parse são honestos sobre precisar do projeto. O bug clássico — passo
    // que JÁ NASCE verdadeiro — é denunciado.
    // _evalCheck: one predicate against (parsed facts, files). The
    // client-side twin of the wizard's _missionCheck — parsed may be null
    // (live typing mode), where parse-dependent kinds return undefined
    // ("unknown"). Português: Um predicado contra (fatos do parse,
    // arquivos) — parsed pode ser null (modo digitação), onde os tipos
    // dependentes de parse devolvem undefined ("desconhecido").
    function _evalCheck(check, parsed) {
        const k = check?.kind || 'parseOk';
        if (k === 'fileIsDict') {
            const f = state.files.find(x => x.path === check.path
                || (x.path || '').endsWith('/' + check.path));
            return !!(f && _isDict(f.content, check.minItems));
        }
        if (!parsed) return undefined;
        if (k === 'parseOk') return !!parsed.parseOk;
        const fn = (parsed.functions || []).find(f => f.name === check.fn);
        const p = fn?.inputs?.find(x => x.name === check.port);
        if (!p) return false;
        switch (k) {
            case 'portHasLang':    return !!p.editorLang;
            case 'portHasDict':    return !!p.editorDict;
            case 'sliceCollapsed': return (p.goType || '').startsWith('[]') || !!p.sliceLenName;
            default:               return false;
        }
    }

    let _lastParse = null; // facts from the last "Run full test"

    function renderTest() {
        const box = $('#cpl-test');
        if (state.level !== 'mission' || !state.steps.length) {
            box.innerHTML = '';
            return;
        }
        const marks = state.steps.map(s => _evalCheck(s.check, _lastParse));
        const freebies = marks
            .map((m, i) => (m === true ? i + 1 : 0)).filter(Boolean);
        const unknown = marks.filter(m => m === undefined).length;

        const chips = state.steps.map((s, i) => {
            const m = marks[i];
            const icon = m === true ? 'fa-circle-check'
                : m === false ? 'fa-circle-xmark' : 'fa-circle-question';
            return `<span class="cp-test-chip"><i class="fa-solid ${icon}"></i> ${i + 1}</span>`;
        }).join('');

        let msg;
        if (freebies.length) {
            msg = `step${freebies.length > 1 ? 's' : ''} ${freebies.join(', ')}
                   already pass${freebies.length > 1 ? '' : 'es'} with the starter
                   files — the student gets ${freebies.length > 1 ? 'them' : 'it'} for free.`;
            box.className = 'cp-lesson-test is-warn';
        } else if (unknown) {
            msg = `${unknown} parse-dependent step(s) unverified —
                   run the full test to parse these files.`;
            box.className = 'cp-lesson-test is-ok';
        } else {
            msg = 'no step starts pre-completed. Lesson looks healthy.';
            box.className = 'cp-lesson-test is-ok';
        }
        box.innerHTML = `
          <i class="fa-solid fa-flask"></i>
          <span style="flex:1">Test: ${msg}</span>
          ${chips}
          <button class="cp-btn cp-btn-sm" id="cpl-test-run">Run full test</button>`;
        box.querySelector('#cpl-test-run')
            .addEventListener('click', runFullTest);
    }

    // runFullTest: the CURRENT Monaco buffers through the same parser the
    // wizard uses (POST /school/parse) — the author sees every predicate
    // verified without leaving the lesson. Português: Os buffers ATUAIS
    // pelo mesmo parser do wizard — todo predicado verificado sem sair.
    async function runFullTest() {
        harvestFiles();
        harvestSteps();
        const r = await cpApi('POST', '/api/control/v1/school/parse', {
            language: state.language,
            files: state.files.map(f => ({ path: f.path, content: f.content })),
        });
        if (r?.metadata?.status !== 200) {
            cpToast('danger', r?.metadata?.error || 'Parse failed.');
            return;
        }
        _lastParse = r.data || null;
        if (_lastParse && !_lastParse.parseOk) {
            cpToast('warning', `Parse failed: ${_lastParse.error || 'see the source'}`);
        }
        renderTest();
    }

    // ── save ──────────────────────────────────────────────────────────────
    async function _putState() {
        return cpApi('PUT', `${API}/${encodeURIComponent(state.id)}`, {
            language: state.language, level: state.level,
            title: state.title, subtitle: state.subtitle,
            ord: state.ord, visible: state.visible,
            steps: state.level === 'mission' ? state.steps : [],
            files: state.files,
            sourceProjectId: state.source,
        });
    }

    async function save() {
        _harvestHead();
        harvestSteps();
        harvestFiles();
        const pr = await _putState();
        if (pr?.metadata?.status === 200) {
            cpToast('success', 'Lesson saved.');
        } else {
            cpToast('danger', pr?.metadata?.error || 'Save failed.');
        }
    }

    function renderAll() {
        renderPreview();
        renderTabs();
        renderSteps();
        renderTest();
    }
    renderAll();
}

// ═════════════════════════════════════════════════════════════════════════
//  Snapshot modal — the one prompt that stays a modal
// ═════════════════════════════════════════════════════════════════════════

function _snapModal() {
    document.getElementById('cp-school-modal')?.remove();
    const bd = document.createElement('div');
    bd.id = 'cp-school-modal';
    bd.className = 'cp-modal-backdrop';
    bd.innerHTML = `
    <div class="cp-modal" style="max-width:520px">
      <h3><i class="fa-solid fa-camera" style="margin-right:8px"></i>Snapshot from project</h3>
      <p style="margin-top:6px;font-size:13px;color:var(--text-dim)">
        Freezes the project's latest saved code version into a School card.</p>
      <div class="cp-field" style="margin-top:14px"><span>Project</span>
        <input class="cp-input" id="cps-search" placeholder="Type to filter your projects…"></div>
      <div id="cps-list" class="cp-snap-list"><i class="fa-solid fa-spinner fa-spin"></i></div>
      <input type="hidden" id="cps-pid">
      <p style="margin:4px 0 10px;font-size:12px;color:var(--text-dim)">
        <span id="cps-picked">No project selected.</span>
        · <span id="cps-manual" style="color:var(--primary);cursor:pointer">paste an id instead</span></p>
      <div class="cp-field"><span>Title (blank = project name)</span>
        <input class="cp-input" id="cps-title"></div>
      <div style="display:flex;gap:12px">
        <div class="cp-field" style="flex:1"><span>Level</span>
          <select class="cp-input" id="cps-level">
            <option value="ready">ready</option>
            <option value="mission">mission</option>
          </select></div>
        <div class="cp-field" style="flex:1"><span>Order</span>
          <input class="cp-input" id="cps-ord" type="number" value="10"></div>
      </div>
      <label style="display:flex;gap:8px;align-items:center;font-size:13px;margin:4px 0 14px">
        <input type="checkbox" id="cps-vis" checked> Visible in the School</label>
      <div style="display:flex;gap:8px;justify-content:flex-end">
        <button class="cp-btn cp-btn-ghost" data-close>Cancel</button>
        <button class="cp-btn cp-btn-primary" id="cps-go">
          <i class="fa-solid fa-camera"></i> Snapshot</button>
      </div>
    </div>`;
    document.body.appendChild(bd);
    bd.addEventListener('click', (ev) => { if (ev.target === bd) bd.remove(); });
    bd.querySelectorAll('[data-close]').forEach(b =>
        b.addEventListener('click', () => bd.remove()));

    // The picker: humans choose projects by NAME (field principle
    // 2026-07-15); the id rides hidden. A manual-id escape hatch stays
    // one click away. Português: Humano escolhe por NOME; o id viaja
    // escondido. Colar id à mão fica a um clique.
    let _projs = [];
    const listBox = bd.querySelector('#cps-list');
    const renderList = (filter) => {
        const f = (filter || '').toLowerCase();
        const rows = _projs.filter(p =>
            !f || (p.name || '').toLowerCase().includes(f)
              || (p.id || '').includes(f));
        listBox.innerHTML = rows.slice(0, 30).map(p => `
          <div class="cp-snap-row ${bd.querySelector('#cps-pid').value === p.id ? 'is-picked' : ''}"
               data-pick="${esc(p.id)}" data-pick-name="${esc(p.name)}">
            <span class="cp-badge" style="background:rgba(127,127,127,.15)">${esc(p.language)}</span>
            <strong>${esc(p.name)}</strong>
            <code style="margin-left:auto;font-size:10px;color:var(--text-dim)">${esc((p.id || '').slice(0, 10))}…</code>
          </div>`).join('')
          || '<p style="font-size:12px;color:var(--text-dim);padding:6px">Nothing matches.</p>';
        listBox.querySelectorAll('[data-pick]').forEach(r =>
            r.addEventListener('click', () => {
                bd.querySelector('#cps-pid').value = r.dataset.pick;
                bd.querySelector('#cps-picked').textContent =
                    `Selected: ${r.dataset.pickName}`;
                if (!bd.querySelector('#cps-title').value.trim()) {
                    bd.querySelector('#cps-title').value = r.dataset.pickName;
                }
                renderList(bd.querySelector('#cps-search').value);
            }));
    };
    cpApi('GET', '/api/control/v1/school/projects').then(r => {
        _projs = r?.data?.projects || [];
        renderList('');
    });
    bd.querySelector('#cps-search').addEventListener('input', (ev) =>
        renderList(ev.target.value));
    bd.querySelector('#cps-manual').addEventListener('click', () => {
        const cur = bd.querySelector('#cps-pid').value;
        const inp = document.createElement('input');
        inp.className = 'cp-input';
        inp.placeholder = 'paste the project id';
        inp.value = cur;
        bd.querySelector('#cps-manual').replaceWith(inp);
        inp.focus();
        inp.addEventListener('input', () => {
            bd.querySelector('#cps-pid').value = inp.value.trim();
            bd.querySelector('#cps-picked').textContent =
                inp.value.trim() ? 'Manual id set.' : 'No project selected.';
        });
    });

    bd.querySelector('#cps-go').addEventListener('click', async () => {
        const pid = bd.querySelector('#cps-pid').value.trim();
        if (!pid) { cpToast('warning', 'Project id is required.'); return; }
        const r = await cpApi('POST', `${API}/from-project`, {
            projectId: pid,
            title: bd.querySelector('#cps-title').value.trim(),
            level: bd.querySelector('#cps-level').value,
            ord: parseInt(bd.querySelector('#cps-ord').value, 10) || 0,
            visible: bd.querySelector('#cps-vis').checked,
        });
        if (r?.metadata?.status === 200) {
            cpToast('success', `Snapshot created: ${r.data?.id} (${r.data?.files} file(s)).`);
            bd.remove();
            await _renderList();
        } else {
            cpToast('danger', r?.metadata?.error || 'Snapshot failed.');
        }
    });
}
