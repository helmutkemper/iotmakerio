// pages/home.js — public post list with infinite scroll
import { S }          from '../state.js';
import { t, esc, ea, exc, fmtD } from '../utils.js';
import { api }        from '../api.js';

export function renderHome(root) {
    // Reset pagination state
    S.posts = []; S.postsOff = 0; S.postsMore = true; S.postsLoading = false;

    root.innerHTML = `
<div class="pw">
  <div class="posts-grid" id="pc"></div>
  <div id="scroll-sentinel"></div>
  <div class="lspinner" id="ps" style="display:none">
    <div class="spinner"></div>
    <span>${t('home.loading', 'Carregando...')}</span>
  </div>
</div>`;

    loadPosts();

    // Infinite scroll observer
    const sentinel = document.getElementById('scroll-sentinel');
    const obs = new IntersectionObserver(entries => {
        if (entries[0].isIntersecting && !S.postsLoading && S.postsMore) loadPosts();
    }, { rootMargin: '200px' });
    obs.observe(sentinel);
}

async function loadPosts() {
    if (S.postsLoading || !S.postsMore) return;
    S.postsLoading = true;

    const spinner = document.getElementById('ps');
    if (spinner) spinner.style.display = 'flex';

    try {
        const res = await api('GET', `/api/blog/posts?offset=${S.postsOff}&limit=10`);
        if (res?.metadata?.status !== 200) throw new Error(res?.metadata?.error || 'Erro');

        const posts   = res.data?.posts  || [];
        S.postsMore   = res.data?.hasMore || false;
        S.postsOff   += posts.length;

        const pc = document.getElementById('pc');
        if (!pc) return;

        if (posts.length === 0 && S.postsOff === 0) {
            pc.innerHTML = `
<div class="empty">
  <div class="ei"><i class="fa-regular fa-newspaper"></i></div>
  <h3>${t('home.empty.title', 'Nenhuma publicação ainda')}</h3>
  <p>${t('home.empty.sub', 'Volte em breve!')}</p>
</div>`;
            return;
        }

        posts.forEach(p => {
            const card    = document.createElement('article');
            card.className = 'post-card';
            // Store before nav to avoid stale reference
            card.onclick  = () => { S.currentPost = p; window.nav('post'); };

            const cover = p.images?.[0]
                ? `<img class="post-cover" src="${ea(p.images[0])}" alt="${ea(p.title)}" loading="lazy">`
                : '';

            card.innerHTML = `${cover}
<h2>${esc(p.title)}</h2>
<div class="post-meta">
  <span><i class="fa-solid fa-pen-nib"></i> ${esc(p.authorName)}</span>
  <span><i class="fa-regular fa-calendar"></i> ${fmtD(p.createdAt)}</span>
</div>
<p class="post-excerpt">${esc(exc(p.content, 220))}</p>
<span class="read-more">
  ${t('home.readmore', 'Ler mais')}
  <i class="fa-solid fa-arrow-right" style="font-size:11px"></i>
</span>`;

            pc.appendChild(card);
        });
    } catch (e) {
        const pc = document.getElementById('pc');
        if (pc) pc.innerHTML = `<div class="alert alert-danger">Erro ao carregar posts: ${esc(e.message)}</div>`;
    } finally {
        S.postsLoading = false;
        const spinner  = document.getElementById('ps');
        if (spinner) spinner.style.display = 'none';
    }
}
