// pages/admin.js — admin post management list
import { S }               from '../state.js';
import { alert2, esc, fmtD, showAlert, showConfirm } from '../utils.js';
import { api }             from '../api.js';

export async function renderAdmin(root) {
    if (S.user?.role !== 'admin') { window.nav('home'); return; }

    root.innerHTML = `
<div class="pw">
  <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:24px">
    <h2 style="font-size:24px;font-weight:700">
      <i class="fa-solid fa-list-ul" style="color:var(--primary);margin-right:8px"></i>Gerenciar posts
    </h2>
    <button class="btn btn-primary" onclick="nav('editor',null)">
      <i class="fa-solid fa-plus"></i> Novo post
    </button>
  </div>
  <div id="aa"></div>
  <div id="apl"><div class="lspinner"><div class="spinner"></div></div></div>
</div>`;

    try {
        const r = await api('GET', '/api/blog/admin/posts?limit=100');
        if (r?.metadata?.status !== 200) throw new Error(r?.metadata?.error);

        const posts = r.data?.posts || [];
        const el    = document.getElementById('apl');

        if (!posts.length) {
            el.innerHTML = `
<div class="empty">
  <div class="ei"><i class="fa-regular fa-file-lines"></i></div>
  <h3>Nenhum post ainda</h3>
  <p style="margin-top:12px">
    <button class="btn btn-primary btn-sm" onclick="nav('editor',null)">Criar primeiro post</button>
  </p>
</div>`;
            return;
        }

        el.innerHTML = posts.map(p => `
<div class="arow">
  <div class="atitle">${esc(p.title)}</div>
  <div class="adate">${fmtD(p.createdAt)}</div>
  <span class="badge ${p.published ? 'badge-pub' : 'badge-dft'}">
    ${p.published ? 'Publicado' : 'Rascunho'}
  </span>
  <button class="btn btn-ghost btn-sm" onclick='doEdit(${JSON.stringify(p)})'>
    <i class="fa-solid fa-pen"></i> Editar
  </button>
  <button class="btn btn-danger btn-sm" onclick="delPost('${p.id}')">
    <i class="fa-solid fa-trash"></i>
  </button>
</div>`).join('');
    } catch (e) {
        alert2('aa', 'danger', 'Erro: ' + e.message);
    }
}

/** Called from inline onclick — opens a post in the editor */
export function doEdit(p) {
    S.editPost = p;
    window.nav('editor');
}

/** Called from inline onclick — deletes a post */
export async function delPost(id) {
    if (!await showConfirm('Excluir este post?', { okLabel: 'Excluir', danger: true })) return;
    const r = await api('DELETE', `/api/blog/admin/posts/${id}`);
    if (r?.metadata?.status === 200) {
        window.nav('admin');
    } else {
        await showAlert('danger', r?.metadata?.error || 'Erro ao excluir.');
    }
}
