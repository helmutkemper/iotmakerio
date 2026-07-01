// pages/editor.js — markdown post editor (Monaco or textarea fallback)
import { S }                    from '../state.js';
import { esc, ea, alert2, ld } from '../utils.js';
import { md }                  from '../markdown.js';
import { api, apiUp }          from '../api.js';

let monacoEditor = null;
let monacoReady  = false;
let editorImgs   = [];

// ─── Render ───────────────────────────────────────────────────────────────────

export function renderEditor(root) {
    if (S.user?.role !== 'admin') { window.nav('home'); return; }

    const p      = S.editPost;
    const isEdit = !!p?.id;
    editorImgs   = p?.images ? [...p.images] : [];
    const init   = p?.content || '';

    root.innerHTML = `
<div class="editor-page">
  <div class="etoolbar">
    <button class="back-btn" style="margin:0" onclick="nav('admin')">
      <i class="fa-solid fa-arrow-left"></i> Voltar
    </button>
    <input type="text" id="post-title"
      placeholder="Título do post..."
      value="${isEdit ? ea(p.title) : ''}">
    <label style="display:flex;align-items:center;gap:6px;font-size:14px;font-weight:600;cursor:pointer;white-space:nowrap">
      <input type="checkbox" id="post-pub" ${isEdit && p.published ? 'checked' : ''}>
      Publicar
    </label>
    <button class="btn btn-secondary btn-sm" id="btn-draft" onclick="savePost(false)">
      <i class="fa-regular fa-floppy-disk"></i> Salvar
    </button>
    <button class="btn btn-primary btn-sm" id="btn-pub" onclick="savePost(true)">
      <i class="fa-solid fa-rocket"></i> Publicar
    </button>
  </div>

  <div id="ea" style="margin-bottom:8px"></div>

  <div class="uzone" id="uz"
    onclick="document.getElementById('ii').click()"
    ondragover="event.preventDefault();this.classList.add('dov')"
    ondragleave="this.classList.remove('dov')"
    ondrop="onDrop(event)">
    <i class="fa-solid fa-image"></i> Clique ou arraste imagens aqui
  </div>
  <input type="file" id="ii" accept="image/*" multiple style="display:none" onchange="onFileIn(this)">
  <div class="thumbs" id="thumbs"></div>

  <div class="elayout">
    <div id="mec">
      <textarea id="fallback-editor"
        placeholder="Escreva em Markdown aqui..."
        oninput="syncPrev()">${esc(init)}</textarea>
    </div>
    <div class="eprev">
      <h3><i class="fa-regular fa-eye"></i> Pré-visualização</h3>
      <div class="post-body" id="prev-body">${md(init)}</div>
    </div>
  </div>
</div>`;

    renderThumbs();
    loadMonaco(init);
}

// ─── Preview sync ─────────────────────────────────────────────────────────────

export function syncPrev() {
    const pb = document.getElementById('prev-body');
    if (pb) pb.innerHTML = md(getContent());
}

// ─── Monaco loader ────────────────────────────────────────────────────────────

function loadMonaco(init) {
    if (monacoReady && window.monaco) { mountMonaco(init); return; }

    const s   = document.createElement('script');
    s.src     = '/static/monaco/vs/loader.js';
    s.onload  = () => {
        require.config({ paths: { vs: '/static/monaco/vs' } });
        require(['vs/editor/editor.main'], () => { monacoReady = true; mountMonaco(init); });
    };
    s.onerror = () => console.info('[Editor] Monaco não encontrado — usando textarea.');
    document.head.appendChild(s);
}

function mountMonaco(init) {
    const c  = document.getElementById('mec');
    if (!c) return;
    const ta = document.getElementById('fallback-editor');
    if (ta) ta.remove();
    if (monacoEditor) { monacoEditor.dispose(); monacoEditor = null; }

    monacoEditor = monaco.editor.create(c, {
        value: init, language: 'markdown', theme: 'vs',
        wordWrap: 'on', lineNumbers: 'on',
        minimap: { enabled: false },
        scrollBeyondLastLine: false,
        fontSize: 15,
        fontFamily: "'Fira Code','Consolas',monospace",
        automaticLayout: true,
    });
    monacoEditor.onDidChangeModelContent(syncPrev);
}

function getContent() {
    if (monacoEditor) return monacoEditor.getValue();
    return document.getElementById('fallback-editor')?.value || '';
}

// ─── Image handling ───────────────────────────────────────────────────────────

function renderThumbs() {
    const el = document.getElementById('thumbs');
    if (!el) return;
    el.innerHTML = editorImgs.map((u, i) => `
<div class="tw">
  <img src="${ea(u)}" loading="lazy">
  <button class="tr-btn" onclick="rmImg(${i})">×</button>
</div>`).join('');
}

export function rmImg(i) {
    editorImgs.splice(i, 1);
    renderThumbs();
}

export async function upImg(f) {
    const r = await apiUp(f);
    if (r?.data?.url) { editorImgs.push(r.data.url); renderThumbs(); }
    else alert2('ea', 'danger', 'Erro ao enviar imagem.');
}

export function onFileIn(inp) {
    Array.from(inp.files).forEach(upImg);
    inp.value = '';
}

export function onDrop(e) {
    e.preventDefault();
    document.getElementById('uz').classList.remove('dov');
    Array.from(e.dataTransfer.files)
        .filter(f => f.type.startsWith('image/'))
        .forEach(upImg);
}

// ─── Save ─────────────────────────────────────────────────────────────────────

export async function savePost(publish) {
    const title = document.getElementById('post-title').value.trim();
    if (!title) { alert2('ea', 'warning', 'O título é obrigatório.'); return; }

    const published = publish || document.getElementById('post-pub').checked;
    const content   = getContent();
    const p         = S.editPost;
    const isEdit    = !!p?.id;
    const bid       = publish ? 'btn-pub' : 'btn-draft';

    ld(bid, true);
    try {
        const pl = { title, content, published, images: editorImgs };
        const r  = isEdit
            ? await api('PUT',  `/api/blog/admin/posts/${p.id}`, pl)
            : await api('POST', '/api/blog/admin/posts', pl);

        if (r?.metadata?.status !== 200) {
            alert2('ea', 'danger', r?.metadata?.error || 'Erro ao salvar.');
            return;
        }
        S.editPost = r.data;
        alert2('ea', 'success', published ? 'Post publicado! ✓' : 'Rascunho salvo! ✓');
    } finally { ld(bid, false); }
}
