// pages/i18n.js — gerenciador de traduções integrado na SPA
// Sem guard de autenticação — qualquer visitante pode acessar.

const API     = '/api/v1/translations';
const LOCALES = ['pt-BR', 'en-US'];

const trState = {};
LOCALES.forEach(loc => { trState[loc] = { bundle: null, messages: [], dirty: false }; });

let activeLoc = 'pt-BR';
let pageRoot  = null;

// ─── Entry point chamado pelo router ─────────────────────────────────────────

export async function renderI18n(root) {
    pageRoot = root;
    root.innerHTML = shell();
    await loadAll();
}

// ─── HTML da página ───────────────────────────────────────────────────────────

function shell() {
    return `
<div class="pw" style="max-width:1100px">

  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:28px;flex-wrap:wrap;gap:12px">
    <div>
      <h1 style="font-size:24px;font-weight:800;color:var(--primary);display:flex;align-items:center;gap:10px">
        <i class="fa-solid fa-language"></i> Traduções
      </h1>
      <p style="font-size:14px;color:var(--text-muted);margin-top:4px">
        Edite as strings de interface para cada idioma.
      </p>
    </div>
    <div style="display:flex;gap:8px">
      <button class="btn btn-secondary btn-sm" onclick="i18nExportPreview()">
        <i class="fa-regular fa-eye"></i> Preview JSON
      </button>
      <button class="btn btn-secondary btn-sm" onclick="i18nExportDownload()">
        <i class="fa-solid fa-download"></i> Download
      </button>
    </div>
  </div>

  <!-- Abas -->
  <div class="tr-tabs">
    <button class="tr-tab active" data-loc="pt-BR" onclick="i18nSetTab('pt-BR')">🇧🇷 Português</button>
    <button class="tr-tab"        data-loc="en-US" onclick="i18nSetTab('en-US')">🇺🇸 English</button>
    <button class="tr-tab"        data-loc="io"    onclick="i18nSetTab('io')">
      <i class="fa-solid fa-arrows-rotate"></i> Import / Export
    </button>
  </div>

  <!-- Painel pt-BR -->
  <div class="tr-panel active" id="tr-panel-pt-BR">
    <div id="tr-body-pt-BR"><div class="lspinner"><div class="spinner"></div></div></div>
  </div>

  <!-- Painel en-US -->
  <div class="tr-panel" id="tr-panel-en-US">
    <div id="tr-body-en-US"><div class="lspinner"><div class="spinner"></div></div></div>
  </div>

  <!-- Painel Import/Export -->
  <div class="tr-panel" id="tr-panel-io">${ioPanel()}</div>

</div>

<style>
.tr-tabs{display:flex;gap:4px;margin-bottom:-1px;flex-wrap:wrap}
.tr-tab{
  padding:9px 22px;background:var(--bg-surface);
  border:1px solid var(--border);border-bottom:none;
  border-radius:var(--r) var(--r) 0 0;
  font-size:14px;font-weight:600;cursor:pointer;
  color:var(--text-muted);font-family:var(--font);
  transition:background .15s,color .15s;
}
.tr-tab:hover:not(.active){background:var(--border);color:var(--text-primary)}
.tr-tab.active{
  background:var(--bg-card);color:var(--primary);
  border-color:var(--border);border-bottom-color:var(--bg-card);
  position:relative;z-index:1;
}
.tr-panel{
  display:none;
  border:1px solid var(--border);border-radius:0 var(--r) var(--r) var(--r);
  background:var(--bg-card);padding:24px;box-shadow:var(--sh);
}
.tr-panel.active{display:block}
.tr-toolbar{display:flex;align-items:center;gap:8px;margin-bottom:16px;flex-wrap:wrap}
.tr-meta{font-size:12px;color:var(--text-muted);margin-bottom:14px;display:flex;gap:6px;align-items:center}
.tr-meta code{background:var(--bg-surface);border:1px solid var(--border);border-radius:4px;padding:1px 6px;font-family:var(--mono);font-size:11px}
.tr-table{width:100%;border-collapse:collapse;border:1px solid var(--border);border-radius:var(--r);overflow:hidden}
.tr-table th{background:var(--bg-surface);padding:10px 12px;text-align:left;font-size:11px;color:var(--text-muted);text-transform:uppercase;letter-spacing:.05em;font-weight:700;white-space:nowrap}
.tr-table td{padding:7px 10px;border-top:1px solid var(--border);vertical-align:middle}
.tr-table tr:hover td{background:var(--bg-surface)}
.tr-table tr.tr-missing td{background:#fffbec}
.tr-table tr.tr-missing td:nth-child(3) input{border-color:var(--warning)}
.tr-field{width:100%;padding:6px 9px;border:1px solid var(--border);border-radius:var(--r);background:var(--bg-input);color:var(--text-primary);font-size:13px;font-family:var(--mono);transition:border-color .15s,box-shadow .15s}
.tr-field:focus{outline:none;border-color:var(--border-focus);box-shadow:0 0 0 3px rgba(34,85,170,.10)}
.tr-field.tr-id{max-width:200px;font-weight:600}
.tr-del{background:none;border:none;cursor:pointer;color:var(--text-muted);font-size:15px;padding:3px 7px;border-radius:var(--r);transition:color .15s,background .15s}
.tr-del:hover{color:var(--danger);background:var(--danger-bg)}
.tr-badge{font-size:12px;font-weight:700;padding:4px 12px;border-radius:99px;margin-left:auto}
.tr-badge.ok{background:var(--success-bg);color:var(--success)}
.tr-badge.warn{background:var(--warning-bg);color:var(--warning)}
.tr-status{font-size:13px;display:none}
.tr-io-area{width:100%;min-height:340px;padding:14px;border:1px solid var(--border);border-radius:var(--r);background:var(--bg-surface);color:var(--text-primary);font-family:var(--mono);font-size:13px;line-height:1.6;resize:vertical;outline:none;transition:border-color .15s,box-shadow .15s}
.tr-io-area:focus{border-color:var(--border-focus);box-shadow:0 0 0 3px rgba(34,85,170,.10)}
.tr-section-title{font-size:16px;font-weight:700;margin-bottom:6px;color:var(--text-primary);display:flex;align-items:center;gap:8px}
.tr-section-desc{font-size:13px;color:var(--text-muted);margin-bottom:14px;line-height:1.6}
.tr-section-desc strong{color:var(--warning)}
.tr-empty{text-align:center;padding:48px 24px;color:var(--text-muted)}
.tr-empty i{font-size:36px;margin-bottom:12px;display:block;color:var(--border)}
</style>`;
}

function ioPanel() {
    return `
<div style="display:grid;grid-template-columns:1fr 1fr;gap:32px">
  <div>
    <p class="tr-section-title"><i class="fa-solid fa-download" style="color:var(--primary)"></i> Export</p>
    <p class="tr-section-desc">Baixa todas as traduções de todos os idiomas como um único arquivo JSON.</p>
    <div style="display:flex;gap:8px;margin-bottom:20px">
      <button class="btn btn-primary btn-sm" onclick="i18nExportDownload()">
        <i class="fa-solid fa-download"></i> Download JSON
      </button>
      <button class="btn btn-secondary btn-sm" onclick="i18nExportPreview()">
        <i class="fa-regular fa-eye"></i> Visualizar
      </button>
    </div>

    <hr style="border:none;border-top:1px solid var(--border);margin:20px 0">

    <p class="tr-section-title"><i class="fa-solid fa-upload" style="color:var(--primary)"></i> Import</p>
    <p class="tr-section-desc">
      Cole JSON abaixo ou abra um arquivo.<br>
      As traduções existentes serão <strong>substituídas</strong>.
    </p>
    <div style="display:flex;gap:8px;margin-bottom:10px">
      <button class="btn btn-primary btn-sm" onclick="document.getElementById('tr-file-inp').click()">
        <i class="fa-solid fa-folder-open"></i> Abrir arquivo
      </button>
      <button class="btn btn-ghost btn-sm" onclick="i18nImportFromArea()">
        <i class="fa-regular fa-clipboard"></i> Importar do campo
      </button>
      <input type="file" id="tr-file-inp" accept=".json" style="display:none" onchange="i18nImportFile(this)">
    </div>
    <div id="tr-io-alert"></div>
  </div>

  <div style="display:flex;flex-direction:column;gap:8px">
    <label style="font-size:12px;font-weight:700;color:var(--text-muted);text-transform:uppercase;letter-spacing:.05em">
      JSON Preview / Editar
    </label>
    <textarea id="tr-io-area" class="tr-io-area"
      placeholder='Cole JSON aqui ou clique em "Visualizar"...'></textarea>
  </div>
</div>`;
}

// ─── Tabs ─────────────────────────────────────────────────────────────────────

export function i18nSetTab(loc) {
    activeLoc = loc;
    pageRoot.querySelectorAll('.tr-tab').forEach(t =>
        t.classList.toggle('active', t.dataset.loc === loc));
    pageRoot.querySelectorAll('.tr-panel').forEach(p =>
        p.classList.remove('active'));
    const panel = pageRoot.querySelector(`#tr-panel-${loc}`);
    if (panel) panel.classList.add('active');
}

// ─── Carga da API ─────────────────────────────────────────────────────────────

async function loadAll() {
    await Promise.all(LOCALES.map(load));
    syncIds();
}

async function load(loc) {
    const s = trState[loc];
    try {
        const res  = await fetch(`${API}/${loc}`);
        const json = await res.json();
        if (json?.metadata?.status === 200 && json.data) {
            s.bundle   = json.data;
            s.messages = (json.data.messages || []).map(m => ({ ...m }));
        } else {
            s.bundle = null; s.messages = [];
        }
    } catch { s.bundle = null; s.messages = []; }
    s.dirty = false;
    paintLocale(loc);
}

// ─── Renderiza tabela do locale ───────────────────────────────────────────────

function paintLocale(loc) {
    const s  = trState[loc];
    const el = pageRoot.querySelector(`#tr-body-${loc}`);
    if (!el) return;

    const total   = s.messages.filter(m => m.id?.trim()).length;
    const missing = s.messages.filter(m => m.id?.trim() && !m.other?.trim()).length;
    const flag    = loc === 'pt-BR' ? '🇧🇷 Português (pt-BR)' : '🇺🇸 English (en-US)';

    let h = `<div class="tr-toolbar">
  <button class="btn btn-primary btn-sm" onclick="i18nSave('${loc}')">
    <i class="fa-solid fa-floppy-disk"></i> Salvar ${flag}
  </button>
  <button class="btn btn-secondary btn-sm" onclick="i18nReload('${loc}')">
    <i class="fa-solid fa-rotate"></i> Recarregar
  </button>
  <button class="btn btn-ghost btn-sm" onclick="i18nAddRow('${loc}')">
    <i class="fa-solid fa-plus"></i> Nova chave
  </button>
  ${ missing > 0
        ? `<span class="tr-badge warn">${missing} sem tradução</span>`
        : total > 0 ? `<span class="tr-badge ok">${total} traduzidas ✓</span>` : '' }
  <span id="tr-status-${loc}" class="tr-status"></span>
</div>`;

    if (s.bundle) {
        h += `<div class="tr-meta">
  Bundle: <code>${xe(s.bundle.bundleId)}</code>
  &nbsp;·&nbsp; Atualizado: <code>${new Date(s.bundle.updatedAt).toLocaleString('pt-BR')}</code>
</div>`;
    }

    if (!s.messages.length) {
        h += `<div class="tr-empty">
  <i class="fa-regular fa-file-lines"></i>
  <p>Nenhuma tradução ainda.<br>Clique em <strong>Nova chave</strong> para começar.</p>
</div>`;
    } else {
        h += `<div style="overflow-x:auto"><table class="tr-table">
  <thead><tr>
    <th style="width:32px"></th>
    <th style="width:200px">ID</th>
    <th>Tradução (other)</th>
    <th style="width:150px">Singular (one)</th>
    <th style="width:180px">Descrição</th>
  </tr></thead>
  <tbody>`;

        s.messages.forEach((m, i) => {
            const cls = (m.id && !m.other?.trim()) ? ' class="tr-missing"' : '';
            h += `<tr${cls}>
  <td><button class="tr-del" onclick="i18nDelRow('${loc}',${i})" title="Remover">
    <i class="fa-solid fa-xmark"></i></button></td>
  <td><input class="tr-field tr-id" type="text" value="${xe(m.id)}"
       placeholder="chave.exemplo"
       onchange="i18nUpd('${loc}',${i},'id',this.value)"></td>
  <td><input class="tr-field" type="text" value="${xe(m.other)}"
       placeholder="Texto traduzido…"
       onchange="i18nUpd('${loc}',${i},'other',this.value)"></td>
  <td><input class="tr-field" type="text" value="${xe(m.one||'')}"
       placeholder="Singular…"
       onchange="i18nUpd('${loc}',${i},'one',this.value)"></td>
  <td><input class="tr-field" type="text" value="${xe(m.description||'')}"
       placeholder="Nota…"
       onchange="i18nUpd('${loc}',${i},'description',this.value)"></td>
</tr>`;
        });

        h += `  </tbody></table></div>`;
    }

    el.innerHTML = h;
}

function xe(s) {
    return (s || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
}

// ─── Mutações em memória ──────────────────────────────────────────────────────

export function i18nUpd(loc, idx, field, value) {
    trState[loc].messages[idx][field] = value;
    trState[loc].dirty = true;
    if (field === 'id') {
        LOCALES.forEach(l => {
            if (l !== loc && idx < trState[l].messages.length) {
                trState[l].messages[idx].id = value;
                trState[l].dirty = true;
            }
        });
        LOCALES.forEach(l => { if (l !== loc) paintLocale(l); });
    }
}

export function i18nAddRow(loc) {
    LOCALES.forEach(l => {
        trState[l].messages.push({ id: '', other: '', one: '', description: '' });
        trState[l].dirty = true;
    });
    LOCALES.forEach(paintLocale);
    setTimeout(() => {
        const fields = pageRoot.querySelectorAll(`#tr-body-${loc} .tr-id`);
        if (fields.length) fields[fields.length - 1].focus();
    }, 30);
}

export function i18nDelRow(loc, idx) {
    LOCALES.forEach(l => {
        if (idx < trState[l].messages.length) {
            trState[l].messages.splice(idx, 1);
            trState[l].dirty = true;
        }
    });
    LOCALES.forEach(paintLocale);
}

// ─── Salvar via API ───────────────────────────────────────────────────────────

export async function i18nSave(loc) {
    const msgs = trState[loc].messages.filter(m => m.id?.trim() && m.other?.trim());
    try {
        const res  = await fetch(`${API}/${loc}`, {
            method:  'PUT',
            headers: { 'Content-Type': 'application/json' },
            body:    JSON.stringify({ messages: msgs }),
        });
        const json = await res.json();
        if (json?.metadata?.status === 200) {
            flash(loc, '✓ Salvo!', false);
            await load(loc);
            syncIds();
        } else {
            flash(loc, '✗ ' + (json?.metadata?.error || 'Erro'), true);
        }
    } catch { flash(loc, '✗ Sem conexão', true); }
}

export async function i18nReload(loc) {
    await load(loc);
    syncIds();
}

function flash(loc, msg, err) {
    const el = pageRoot.querySelector(`#tr-status-${loc}`);
    if (!el) return;
    el.textContent  = msg;
    el.style.color  = err ? 'var(--danger)' : 'var(--success)';
    el.style.display = 'inline';
    setTimeout(() => { el.style.display = 'none'; }, 3000);
}

// ─── Sincroniza IDs entre locales ─────────────────────────────────────────────

function syncIds() {
    const seen = new Set(), allIds = [];
    LOCALES.forEach(l => trState[l].messages.forEach(m => {
        if (m.id && !seen.has(m.id)) { seen.add(m.id); allIds.push(m.id); }
    }));
    LOCALES.forEach(l => {
        const byId = {};
        trState[l].messages.forEach(m => { if (m.id) byId[m.id] = m; });
        trState[l].messages = allIds.map(id => byId[id] || { id, other: '', one: '', description: '' });
        paintLocale(l);
    });
}

// ─── Export ───────────────────────────────────────────────────────────────────

function buildExport() {
    const out = {};
    LOCALES.forEach(l => {
        out[l] = {
            messages: trState[l].messages
                .filter(m => m.id?.trim())
                .map(m => {
                    const e = { id: m.id, other: m.other || '' };
                    if (m.one?.trim())         e.one = m.one;
                    if (m.description?.trim()) e.description = m.description;
                    return e;
                }),
        };
    });
    return out;
}

export function i18nExportPreview() {
    const area = pageRoot.querySelector('#tr-io-area');
    if (area) area.value = JSON.stringify(buildExport(), null, 2);
    i18nSetTab('io');
}

export function i18nExportDownload() {
    const blob = new Blob([JSON.stringify(buildExport(), null, 2)], { type: 'application/json' });
    const url  = URL.createObjectURL(blob);
    Object.assign(document.createElement('a'), {
        href:     url,
        download: 'translations_' + new Date().toISOString().slice(0, 10) + '.json',
    }).click();
    URL.revokeObjectURL(url);
}

// ─── Import ───────────────────────────────────────────────────────────────────

export function i18nImportFile(input) {
    const file = input.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = e => {
        const area = pageRoot.querySelector('#tr-io-area');
        if (area) area.value = e.target.result;
        applyImport(e.target.result);
    };
    reader.readAsText(file);
    input.value = '';
}

export function i18nImportFromArea() {
    const area = pageRoot.querySelector('#tr-io-area');
    if (!area?.value.trim()) { ioAlert('Campo vazio.', 'warning'); return; }
    applyImport(area.value.trim());
}

async function applyImport(text) {
    let data;
    try { data = JSON.parse(text); }
    catch (e) { ioAlert('JSON inválido: ' + e.message, 'danger'); return; }

    const found = Object.keys(data).filter(k => Array.isArray(data[k]?.messages));
    if (!found.length) { ioAlert('Nenhum locale válido no JSON.', 'warning'); return; }

    let saved = 0, errors = [];
    for (const loc of found) {
        try {
            const res  = await fetch(`${API}/${loc}`, {
                method:  'PUT',
                headers: { 'Content-Type': 'application/json' },
                body:    JSON.stringify({ messages: data[loc].messages }),
            });
            const json = await res.json();
            json?.metadata?.status === 200 ? saved++ : errors.push(loc + ': ' + (json?.metadata?.error || 'erro'));
        } catch { errors.push(loc + ': sem conexão'); }
    }

    errors.length
        ? ioAlert('Erros: ' + errors.join(', '), 'danger')
        : ioAlert(`${saved} locale(s) importado(s) ✓`, 'success');

    await loadAll();
}

function ioAlert(msg, type) {
    const el = pageRoot.querySelector('#tr-io-alert');
    if (!el) return;
    el.innerHTML = `<div class="alert alert-${type}" style="margin-bottom:12px">${msg}</div>`;
    setTimeout(() => { el.innerHTML = ''; }, 4000);
}
