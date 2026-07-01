// pages/post.js — single post reader
import { S }         from '../state.js';
import { t, esc, ea, fmtD } from '../utils.js';
import { md }        from '../markdown.js';

export function renderPost(root) {
    const p = S.currentPost;
    if (!p) { window.nav('home'); return; }

    const cover = p.images?.[0]
        ? `<img class="post-cover" src="${ea(p.images[0])}" alt="${ea(p.title)}">`
        : '';

    root.innerHTML = `
<div class="post-full">
  <button class="back-btn" onclick="nav('home')">
    <i class="fa-solid fa-arrow-left"></i> ${t('post.back', 'Voltar')}
  </button>
  <h1 class="post-title">${esc(p.title)}</h1>
  <div class="post-meta">
    <span><i class="fa-solid fa-pen-nib"></i> ${esc(p.authorName)}</span>
    <span><i class="fa-regular fa-calendar"></i> ${fmtD(p.createdAt)}</span>
  </div>
  ${cover}
  <div class="post-body">${md(p.content)}</div>
</div>`;
}
