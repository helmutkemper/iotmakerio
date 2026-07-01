// pages/feed.js — Marketplace / community feed.
import { showAlert, showConfirm } from '../utils.js';
//
// Four tabs share the same card grid and filter bar:
//
//   Discover  — weighted mix of recency + popularity + randomness (default)
//   Recent    — newest updated_at first, cursor-based infinite scroll
//   Popular   — highest avg_rating first, page-based pagination
//   Following — activity from followed users (requires auth)
//
// Only projects marked as "ready to use" appear in any tab — enforced server-side.
//
// ── Community interaction per card ───────────────────────────────────────────
//
// Each card has two action buttons at the bottom:
//
//   💬 Comments — toggles a panel below the card with:
//       - Paginated comment list (author avatar, name, body, sub-ratings)
//       - A form to post a new comment with optional doc/code sub-ratings
//
//   🚩 Report   — opens a centered modal to file a moderation report with:
//       - Four reason options (offensive, off-topic, spam, misleading)
//       - Optional free-text details field
//       - Shows "already reported" state if the viewer has filed one before
//
// ── Tab active indicator ─────────────────────────────────────────────────────
//
// The tab bar has id="feed-tab-bar" so feedSetTab can find it reliably without
// fragile style-string queries.
//
// API endpoints:
//   GET    /api/v1/feed                             — main feed
//   PUT    /api/v1/projects/:id/rating              — rate a project
//   DELETE /api/v1/projects/:id/rating              — clear rating
//   GET    /api/v1/projects/:id/comments            — list comments
//   POST   /api/v1/projects/:id/comments            — create comment
//   DELETE /api/v1/projects/:id/comments/:cid       — delete comment
//   GET    /api/v1/projects/:id/report              — check own report
//   POST   /api/v1/projects/:id/report              — file a report
//   POST   /api/v1/users/:username/follow           — follow author
//   DELETE /api/v1/users/:username/follow           — unfollow author
//   GET    /api/v1/projects/meta/readme-config      — taxonomy + languages

import { api } from '../api.js';
import { S }   from '../state.js';

// ─── Module state ─────────────────────────────────────────────────────────────

let _root        = null;
let _tab         = 'discover';
let _cards       = [];
let _cursor      = '';
let _page        = 1;
let _loading     = false;
let _exhausted   = false;
let _filterTimer = null;
let _meta        = null;

let _filter = { tab: 'discover', category: '', sub: '', lang: '', q: '' };

// Per-card comment panel state.
// _commentPanels[projectId] = { open: bool, page: int, total: int,
//                                comments: [], loading: bool }
const _commentPanels = {};

// ─── Entry point ──────────────────────────────────────────────────────────────

export async function renderFeed(root) {
    _root       = root;
    _tab        = 'discover';
    _filter     = { tab: 'discover', category: '', sub: '', lang: '', q: '' };
    _cards      = [];
    _cursor     = '';
    _page       = 1;
    _loading    = false;
    _exhausted  = false;

    root.innerHTML = _buildShell();
    await Promise.all([_loadMeta(), _loadPage()]);
    _populateFilterSelects();
    _renderCards();
    _attachScrollListener();
}

// ─── Shell ────────────────────────────────────────────────────────────────────

function _buildShell() {
    return `
<div style="max-width:1280px;margin:0 auto;padding:0 16px 40px">

  <div style="padding:28px 0 20px;border-bottom:1px solid var(--border);margin-bottom:24px">
    <h1 style="font-size:24px;font-weight:800;margin:0 0 4px">
      <i class="fa-solid fa-globe" style="color:var(--primary);margin-right:10px"></i>Community
    </h1>
    <p style="color:var(--text-muted);font-size:14px;margin:0">
      Discover, rate, and follow open IoT components
    </p>
  </div>

  <!-- Tab bar — id is required by feedSetTab (see comment at top of file) -->
  <div id="feed-tab-bar"
       style="display:flex;gap:2px;margin-bottom:20px;border-bottom:1px solid var(--border)">
    ${_tabBtn('discover', 'fa-solid fa-shuffle',    'Discover')}
    ${_tabBtn('recent',   'fa-solid fa-clock',      'Recent')}
    ${_tabBtn('popular',  'fa-solid fa-star',       'Popular')}
    ${_tabBtn('following','fa-solid fa-user-group', 'Following', !S.user)}
  </div>

  <!-- Filter bar -->
  <div style="display:flex;gap:10px;flex-wrap:wrap;margin-bottom:20px;align-items:center">
    <div style="flex:1;min-width:180px;max-width:340px;position:relative">
      <i class="fa-solid fa-magnifying-glass"
         style="position:absolute;left:10px;top:50%;transform:translateY(-50%);
                color:var(--text-muted);font-size:13px;pointer-events:none"></i>
      <input id="feed-search" class="fc" type="text"
             placeholder="Search title, description, keywords…"
             style="padding-left:32px;font-size:13px"
             oninput="feedSearchInput(this.value)">
    </div>
    <select id="feed-cat" class="fc" style="flex:0 0 auto;font-size:13px;min-width:140px"
            onchange="feedFilterChange()">
      <option value="">All categories</option>
    </select>
    <select id="feed-sub" class="fc"
            style="flex:0 0 auto;font-size:13px;min-width:140px;display:none"
            onchange="feedFilterChange()">
      <option value="">All subcategories</option>
    </select>
    <select id="feed-lang" class="fc" style="flex:0 0 auto;font-size:13px;min-width:120px"
            onchange="feedFilterChange()">
      <option value="">All languages</option>
    </select>
    <button class="btn btn-ghost btn-sm" onclick="feedClearFilters()"
            id="feed-clear-btn" style="display:none">
      <i class="fa-solid fa-xmark"></i> Clear
    </button>
  </div>

  <!-- Card grid -->
  <div id="feed-grid"
       style="display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:20px">
    ${_loadingPlaceholders(6)}
  </div>

  <div id="feed-status" style="text-align:center;padding:32px;color:var(--text-muted);font-size:13px"></div>
</div>`;
}

function _tabBtn(key, icon, label, disabled = false) {
    const isActive = _tab === key;
    const disStyle = disabled ? 'opacity:.4;cursor:not-allowed;' : 'cursor:pointer;';
    return `
<button onclick="${disabled ? '' : `feedSetTab('${key}')`}"
  style="padding:10px 18px;background:none;border:none;
         border-bottom:3px solid ${isActive ? 'var(--primary)' : 'transparent'};
         font-size:13px;font-weight:700;
         color:${isActive ? 'var(--primary)' : 'var(--text-muted)'};
         font-family:var(--font);transition:all .15s;
         display:flex;align-items:center;gap:6px;${disStyle}"
  ${disabled ? 'title="Log in to see the following feed"' : ''}>
  <i class="${icon}" style="font-size:12px"></i> ${label}
</button>`;
}

function _loadingPlaceholders(n) {
    return Array.from({length: n}, () => `
<div style="background:var(--bg-surface);border:1px solid var(--border);
            border-radius:var(--r);overflow:hidden;animation:pulse 1.5s ease-in-out infinite">
  <div style="height:160px;background:var(--bg-card)"></div>
  <div style="padding:14px">
    <div style="height:14px;background:var(--border);border-radius:4px;margin-bottom:8px;width:70%"></div>
    <div style="height:12px;background:var(--border);border-radius:4px;width:90%"></div>
    <div style="height:12px;background:var(--border);border-radius:4px;margin-top:6px;width:60%"></div>
  </div>
</div>`).join('');
}

// ─── Data loading ─────────────────────────────────────────────────────────────

async function _loadMeta() {
    const [readmeCfg, langCfg] = await Promise.all([
        api('GET', '/api/v1/projects/meta/readme-config'),
        api('GET', '/api/v1/projects/meta/languages'),
    ]);
    _meta = {
        categories:    readmeCfg?.data?.categories    || [],
        subcategories: readmeCfg?.data?.subcategories || [],
        languages:     langCfg?.data || [],
    };
}

async function _loadPage() {
    if (_loading || _exhausted) return;
    _loading = true;
    _setStatus('<i class="fa-solid fa-spinner fa-spin"></i> Loading…');

    const params = new URLSearchParams({ tab: _filter.tab });
    if (_filter.category) params.set('category', _filter.category);
    if (_filter.sub)      params.set('sub',      _filter.sub);
    if (_filter.lang)     params.set('lang',     _filter.lang);
    if (_filter.q)        params.set('q',        _filter.q);

    if (_filter.tab === 'recent' || _filter.tab === 'following') {
        if (_cursor) params.set('cursor', _cursor);
    } else {
        params.set('page', String(_page));
    }

    const r = await api('GET', `/api/v1/feed?${params}`);
    _loading = false;

    if (r?.metadata?.status !== 200) {
        _setStatus('Could not load feed. Please try again.');
        return;
    }

    const { cards, nextCursor, pageSize } = r.data;

    if (!cards || cards.length === 0) {
        _exhausted = true;
        _setStatus(_cards.length === 0 ? 'No components found.' : '— end of feed —');
        return;
    }

    _cards  = [..._cards, ...cards];
    _cursor = nextCursor || '';
    _page++;

    if (cards.length < pageSize) {
        _exhausted = true;
        _setStatus('— end of feed —');
    } else {
        _setStatus('');
    }
}

// ─── Card rendering ───────────────────────────────────────────────────────────

function _renderCards() {
    const grid = document.getElementById('feed-grid');
    if (!grid) return;
    grid.innerHTML = _cards.map(c => _buildCard(c)).join('');
}

// _buildCard renders a single project card.
//
// Each card is wrapped in a <div data-proj-id="..."> container so that the
// comment panel (rendered below the card body) can be toggled in-place
// without touching the rest of the grid.
function _buildCard(c) {
    const imgHtml = c.cardImage
        ? `<img src="${esc(c.cardImage)}" alt="${esc(c.cardTitle)}"
               style="width:100%;height:160px;object-fit:cover;display:block"
               onerror="this.style.display='none'">`
        : `<div style="height:160px;background:var(--bg-surface);display:flex;
                       align-items:center;justify-content:center">
             <i class="fa-solid fa-microchip"
                style="font-size:32px;opacity:.15;color:var(--primary)"></i>
           </div>`;

    const catBadge = c.categoryName
        ? `<span style="font-size:10px;font-weight:600;background:var(--info-bg);
                        color:var(--primary);border:1px solid var(--primary);
                        border-radius:99px;padding:1px 8px;white-space:nowrap;flex-shrink:0">
             ${esc(c.categoryName)}${c.subcategoryName ? ' › ' + esc(c.subcategoryName) : ''}
           </span>`
        : '';

    const stars = _buildStars(c.projectId, c.avgRating, c.ownRating, c.ratingCount);

    const avatarHtml = c.authorAvatarUrl
        ? `<img src="${esc(c.authorAvatarUrl)}"
               style="width:22px;height:22px;border-radius:50%;object-fit:cover">`
        : `<span style="width:22px;height:22px;border-radius:50%;background:var(--primary);
                        color:#fff;font-size:11px;font-weight:700;display:flex;
                        align-items:center;justify-content:center;flex-shrink:0">
             ${esc((c.authorDisplayName || c.authorUsername || '?')[0].toUpperCase())}
           </span>`;

    const displayName = c.authorDisplayName || c.authorUsername;

    const followBtn = S.user && S.user.username !== c.authorUsername
        ? `<button id="follow-btn-${esc(c.projectId)}"
             onclick="feedToggleFollow('${esc(c.projectId)}','${esc(c.authorUsername)}',${c.isFollowing})"
             style="font-size:11px;padding:2px 8px;
                    background:${c.isFollowing ? 'var(--bg-surface)' : 'var(--primary)'};
                    color:${c.isFollowing ? 'var(--text-muted)' : '#fff'};
                    border:1px solid ${c.isFollowing ? 'var(--border)' : 'var(--primary)'};
                    border-radius:99px;cursor:pointer;white-space:nowrap;flex-shrink:0">
             ${c.isFollowing ? 'Following' : '+ Follow'}
           </button>`
        : '';

    const eventBadge = c.eventType
        ? `<span style="font-size:10px;color:var(--text-muted);margin-bottom:6px;display:block">
             ${_eventLabel(c.eventType)}
           </span>`
        : '';

    // Comment panel state for this card (initialised lazily).
    const panelState = _commentPanels[c.projectId];
    const commentCount = panelState?.total ?? 0;

    return `
<div data-proj-id="${esc(c.projectId)}"
     style="background:var(--bg-card);border:1px solid var(--border);
            border-radius:var(--r);overflow:hidden;display:flex;
            flex-direction:column;transition:box-shadow var(--tr)"
     onmouseover="this.style.boxShadow='var(--shh)'"
     onmouseout="this.style.boxShadow='none'">

  <!-- Cover image -->
  <div style="flex-shrink:0">${imgHtml}</div>

  <!-- Body -->
  <div style="padding:14px 16px;flex:1;display:flex;flex-direction:column;gap:8px">
    ${eventBadge}

    <div style="display:flex;align-items:flex-start;gap:8px;flex-wrap:wrap">
      <span style="font-size:14px;font-weight:700;color:var(--text-primary);flex:1;
                   min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap"
            title="${esc(c.cardTitle)}">
        ${esc(c.cardTitle || c.name)}
      </span>
      ${catBadge}
    </div>

    <p style="font-size:12px;color:var(--text-secondary);margin:0;line-height:1.5;
              display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;
              overflow:hidden">
      ${esc(c.cardDescription || '')}
    </p>

    ${stars}

    <!-- Author row -->
    <div style="display:flex;align-items:center;gap:7px;margin-top:auto;padding-top:8px;
                border-top:1px solid var(--border)">
      ${avatarHtml}
      <button onclick="nav('profile', {username: '${esc(c.authorUsername)}'})"
        style="background:none;border:none;cursor:pointer;font-size:12px;
               color:var(--text-secondary);text-align:left;padding:0;flex:1;
               overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
        ${esc(displayName)}
      </button>
      ${followBtn}
    </div>

    <div style="font-size:11px;color:var(--text-muted)">
      <i class="fa-brands fa-golang" style="margin-right:4px"></i>${esc(c.langDisplay)}
    </div>
  </div>

  <!-- Community action bar -->
  <div style="display:flex;border-top:1px solid var(--border);background:var(--bg-surface)">
    <!-- Comments toggle -->
    <button onclick="feedToggleComments('${esc(c.projectId)}')"
      style="flex:1;padding:8px;background:none;border:none;border-right:1px solid var(--border);
             cursor:pointer;font-size:12px;color:var(--text-muted);display:flex;
             align-items:center;justify-content:center;gap:6px;transition:background var(--tr)"
      onmouseover="this.style.background='var(--bg-card)'"
      onmouseout="this.style.background='none'"
      title="Comments">
      <i class="fa-regular fa-comment"></i>
      <span id="feed-comment-count-${esc(c.projectId)}">${commentCount > 0 ? commentCount : ''}</span>
      Comments
    </button>
    <!-- Report -->
    <button onclick="feedOpenReport('${esc(c.projectId)}')"
      style="flex:0 0 auto;padding:8px 14px;background:none;border:none;
             cursor:pointer;font-size:12px;color:var(--text-muted);display:flex;
             align-items:center;gap:6px;transition:background var(--tr)"
      onmouseover="this.style.background='var(--bg-card)'"
      onmouseout="this.style.background='none'"
      title="${S.user ? 'Report this project' : 'Log in to report'}">
      <i class="fa-solid fa-flag" style="font-size:11px"></i>
    </button>
  </div>

  <!-- Comment panel — hidden by default, toggled by feedToggleComments -->
  <div id="feed-comments-${esc(c.projectId)}" style="display:none;
       border-top:1px solid var(--border)">
  </div>
</div>`;
}

// ─── Stars ────────────────────────────────────────────────────────────────────

function _buildStars(projectId, avgRating, ownRating, ratingCount) {
    const rounded = Math.round(avgRating * 2) / 2;
    const stars = Array.from({length: 5}, (_, i) => {
        const filled   = i + 1 <= rounded;
        const halfFill = !filled && i + 0.5 === rounded;
        const icon = filled ? 'fa-star' : halfFill ? 'fa-star-half-stroke' : 'fa-star';
        const col  = (filled || halfFill) ? '#F59E0B' : 'var(--border)';
        const size = ownRating === i + 1 ? '17px' : '15px';
        if (!S.user) {
            return `<i class="fa-solid ${icon}" style="color:${col};font-size:${size}"></i>`;
        }
        return `<i class="fa-solid ${icon}"
            style="color:${ownRating === i+1 ? '#F59E0B' : col};font-size:${size};
                   cursor:pointer;transition:transform .1s"
            onmouseover="this.style.transform='scale(1.3)'"
            onmouseout="this.style.transform=''"
            onclick="feedRate('${esc(projectId)}', ${i + 1})"
            title="Rate ${i + 1} star${i + 1 > 1 ? 's' : ''}"></i>`;
    }).join('');

    const countStr = ratingCount > 0
        ? `<span style="font-size:11px;color:var(--text-muted);margin-left:5px">
             ${avgRating.toFixed(1)} (${ratingCount})
           </span>`
        : `<span style="font-size:11px;color:var(--text-muted);margin-left:5px">
             No ratings yet
           </span>`;

    const clearBtn = S.user && ownRating > 0
        ? `<button onclick="feedClearRating('${esc(projectId)}')"
             style="font-size:10px;background:none;border:none;color:var(--text-muted);
                    cursor:pointer;margin-left:6px;padding:0"
             title="Clear my rating">✕</button>`
        : '';

    return `<div style="display:flex;align-items:center;gap:2px">
      ${stars}${countStr}${clearBtn}
    </div>`;
}

function _eventLabel(type) {
    const map = {
        'project_created':  '🆕 New component',
        'code_updated':     '💾 Code updated',
        'readme_updated':   '📄 Readme updated',
    };
    return map[type] || type;
}

// ─── Tab switching ────────────────────────────────────────────────────────────

// feedSetTab switches to a different feed tab and re-renders the tab bar.
// Uses getElementById('feed-tab-bar') — see comment at the top of the file.
export function feedSetTab(tab) {
    if (tab === 'following' && !S.user) return;
    _tab           = tab;
    _filter.tab    = tab;
    _cards         = [];
    _cursor        = '';
    _page          = 1;
    _exhausted     = false;

    const tabBar = document.getElementById('feed-tab-bar');
    if (tabBar) {
        tabBar.innerHTML =
            _tabBtn('discover', 'fa-solid fa-shuffle',    'Discover') +
            _tabBtn('recent',   'fa-solid fa-clock',      'Recent') +
            _tabBtn('popular',  'fa-solid fa-star',       'Popular') +
            _tabBtn('following','fa-solid fa-user-group', 'Following', !S.user);
    }

    const grid = document.getElementById('feed-grid');
    if (grid) grid.innerHTML = _loadingPlaceholders(6);

    _loadPage().then(() => _renderCards());
}

// ─── Filters ──────────────────────────────────────────────────────────────────

function _populateFilterSelects() {
    if (!_meta) return;
    const catSel = document.getElementById('feed-cat');
    if (catSel && _meta.categories.length) {
        catSel.innerHTML = '<option value="">All categories</option>' +
            _meta.categories.map(c =>
                `<option value="${esc(c.id)}">${esc(c.name)}</option>`
            ).join('');
    }
    const langSel = document.getElementById('feed-lang');
    if (langSel && _meta.languages.length) {
        langSel.innerHTML = '<option value="">All languages</option>' +
            _meta.languages.map(l =>
                `<option value="${esc(l.id)}">${esc(l.display)}</option>`
            ).join('');
    }
}

function _updateSubcategorySelect(categoryId) {
    const subSel = document.getElementById('feed-sub');
    if (!subSel || !_meta) return;
    if (!categoryId) { subSel.style.display = 'none'; subSel.value = ''; return; }
    const subs = _meta.subcategories.filter(s => s.categoryId === categoryId);
    if (!subs.length) { subSel.style.display = 'none'; subSel.value = ''; return; }
    subSel.innerHTML = '<option value="">All subcategories</option>' +
        subs.map(s => `<option value="${esc(s.id)}">${esc(s.name)}</option>`).join('');
    subSel.style.display = '';
}

export function feedFilterChange() {
    const catId  = document.getElementById('feed-cat')?.value  || '';
    const subId  = document.getElementById('feed-sub')?.value  || '';
    const langId = document.getElementById('feed-lang')?.value || '';
    _updateSubcategorySelect(catId);
    _filter.category = catId;
    _filter.sub      = subId;
    _filter.lang     = langId;
    const clearBtn = document.getElementById('feed-clear-btn');
    if (clearBtn) clearBtn.style.display = (catId || subId || langId || _filter.q) ? '' : 'none';
    _resetAndReload();
}

export function feedSearchInput(value) {
    clearTimeout(_filterTimer);
    _filterTimer = setTimeout(() => {
        _filter.q = value.trim();
        const clearBtn = document.getElementById('feed-clear-btn');
        if (clearBtn) clearBtn.style.display = _filter.q ? '' : 'none';
        _resetAndReload();
    }, 400);
}

export function feedClearFilters() {
    _filter.category = '';
    _filter.sub      = '';
    _filter.lang     = '';
    _filter.q        = '';
    const catSel   = document.getElementById('feed-cat');
    const subSel   = document.getElementById('feed-sub');
    const langSel  = document.getElementById('feed-lang');
    const search   = document.getElementById('feed-search');
    const clearBtn = document.getElementById('feed-clear-btn');
    if (catSel)   catSel.value  = '';
    if (subSel)   { subSel.value = ''; subSel.style.display = 'none'; }
    if (langSel)  langSel.value = '';
    if (search)   search.value  = '';
    if (clearBtn) clearBtn.style.display = 'none';
    _resetAndReload();
}

function _resetAndReload() {
    _cards     = [];
    _cursor    = '';
    _page      = 1;
    _exhausted = false;
    const grid = document.getElementById('feed-grid');
    if (grid) grid.innerHTML = _loadingPlaceholders(6);
    _loadPage().then(() => _renderCards());
}

// ─── Infinite scroll ──────────────────────────────────────────────────────────

function _attachScrollListener() {
    window.addEventListener('scroll', _onScroll, { passive: true });
}

function _onScroll() {
    if (_exhausted || _loading) return;
    if (window.innerHeight + window.scrollY >= document.body.scrollHeight - 400) {
        _loadPage().then(() => _renderCards());
    }
}

// ─── Rating ───────────────────────────────────────────────────────────────────

export async function feedRate(projectId, rating) {
    if (!S.user) { await showAlert('warning', 'Faça login para avaliar projetos.'); return; }
    const r = await api('PUT', `/api/v1/projects/${projectId}/rating`, { rating });
    if (r?.metadata?.status === 200) {
        const idx = _cards.findIndex(c => c.projectId === projectId);
        if (idx >= 0) {
            _cards[idx].avgRating   = r.data.avgRating;
            _cards[idx].ratingCount = r.data.ratingCount;
            _cards[idx].ownRating   = r.data.rating;
            _reRenderCard(idx);
        }
    }
}

export async function feedClearRating(projectId) {
    if (!S.user) return;
    const r = await api('DELETE', `/api/v1/projects/${projectId}/rating`);
    if (r?.metadata?.status === 200) {
        const idx = _cards.findIndex(c => c.projectId === projectId);
        if (idx >= 0) {
            _cards[idx].avgRating   = r.data.avgRating;
            _cards[idx].ratingCount = r.data.ratingCount;
            _cards[idx].ownRating   = 0;
            _reRenderCard(idx);
        }
    }
}

// ─── Follow ───────────────────────────────────────────────────────────────────

export async function feedToggleFollow(projectId, authorUsername, currentlyFollowing) {
    if (!S.user) { await showAlert('warning', 'Faça login para seguir usuários.'); return; }
    const method = currentlyFollowing ? 'DELETE' : 'POST';
    const r = await api(method, `/api/v1/users/${encodeURIComponent(authorUsername)}/follow`);
    if (r?.metadata?.status === 200) {
        const nowFollowing = !currentlyFollowing;
        _cards.forEach((c, idx) => {
            if (c.authorUsername === authorUsername) {
                _cards[idx].isFollowing = nowFollowing;
                _reRenderCard(idx);
            }
        });
    }
}

// ─── Comments ────────────────────────────────────────────────────────────────
//
// Comments are loaded lazily when the user first opens the panel.
// Subsequent toggles reuse the cached data unless the user posts a new comment.
//
// Each comment can include two optional sub-ratings:
//   docRating  (1–5) — documentation quality
//   codeRating (1–5) — code quality
//
// Sub-ratings are rendered as small star rows labelled "Docs" and "Code"
// inside the comment card, only when the value is > 0.

// feedToggleComments opens or closes the comment panel for a project card.
export async function feedToggleComments(projectId) {
    const panel = document.getElementById(`feed-comments-${projectId}`);
    if (!panel) return;

    const isOpen = panel.style.display !== 'none';
    if (isOpen) {
        panel.style.display = 'none';
        if (_commentPanels[projectId]) _commentPanels[projectId].open = false;
        return;
    }

    // First open: initialise state and load first page.
    if (!_commentPanels[projectId]) {
        _commentPanels[projectId] = { open: true, page: 1, total: 0, comments: [], loading: false };
    } else {
        _commentPanels[projectId].open = true;
    }

    panel.style.display = 'block';
    _renderCommentPanel(projectId);

    // Load if not yet loaded.
    if (_commentPanels[projectId].comments.length === 0 && !_commentPanels[projectId].loading) {
        await _loadComments(projectId, 1);
        _renderCommentPanel(projectId);
    }
}

// _loadComments fetches a page of comments and merges them into the cache.
async function _loadComments(projectId, page) {
    const state = _commentPanels[projectId];
    if (!state || state.loading) return;
    state.loading = true;
    const r = await api('GET', `/api/v1/projects/${projectId}/comments?page=${page}`);
    state.loading = false;
    if (r?.metadata?.status !== 200) return;
    const { comments, total, pageSize } = r.data;
    state.total    = total;
    state.pageSize = pageSize;
    state.page     = page;
    // Append new page results (avoid duplicates by rebuilding on page 1).
    if (page === 1) {
        state.comments = comments || [];
    } else {
        state.comments = [...state.comments, ...(comments || [])];
    }
    // Update the comment count badge on the action bar.
    const badge = document.getElementById(`feed-comment-count-${projectId}`);
    if (badge) badge.textContent = total > 0 ? total : '';
}

// _renderCommentPanel builds the HTML for the comment panel of a card.
function _renderCommentPanel(projectId) {
    const panel = document.getElementById(`feed-comments-${projectId}`);
    if (!panel) return;
    const state = _commentPanels[projectId];
    if (!state) { panel.innerHTML = ''; return; }

    const commentList = (state.comments || []).map(cm => _buildCommentRow(projectId, cm)).join('');

    const loadedCount  = state.comments.length;
    const totalCount   = state.total;
    const hasMore      = loadedCount < totalCount;
    const loadMoreBtn  = hasMore
        ? `<button onclick="feedLoadMoreComments('${esc(projectId)}')"
             style="width:100%;padding:8px;background:none;border:none;
                    border-top:1px solid var(--border);cursor:pointer;
                    font-size:12px;color:var(--primary);transition:background var(--tr)"
             onmouseover="this.style.background='var(--bg-surface)'"
             onmouseout="this.style.background='none'">
             Load more (${totalCount - loadedCount} remaining)
           </button>`
        : '';

    // Comment form — only shown to logged-in users.
    const form = S.user ? `
<div style="padding:12px 16px;border-top:1px solid var(--border);
            background:var(--bg-surface)">
  <div style="font-size:12px;font-weight:600;color:var(--text-secondary);
              margin-bottom:8px">Add a comment</div>
  <textarea id="feed-comment-body-${esc(projectId)}"
    style="width:100%;min-height:72px;padding:8px 10px;font-size:13px;
           border:1px solid var(--border);border-radius:var(--r);resize:vertical;
           font-family:var(--font);background:var(--bg-input);
           color:var(--text-primary);box-sizing:border-box"
    placeholder="Share your experience with this project…"
    maxlength="1000"></textarea>

  <!-- Optional sub-ratings row -->
  <div style="display:flex;gap:20px;margin-top:8px;align-items:center;flex-wrap:wrap">
    <div style="display:flex;align-items:center;gap:6px">
      <span style="font-size:11px;color:var(--text-muted);white-space:nowrap">Docs quality:</span>
      ${_buildSubRatingInput('doc', projectId)}
    </div>
    <div style="display:flex;align-items:center;gap:6px">
      <span style="font-size:11px;color:var(--text-muted);white-space:nowrap">Code quality:</span>
      ${_buildSubRatingInput('code', projectId)}
    </div>
    <button onclick="feedSubmitComment('${esc(projectId)}')"
      style="margin-left:auto;padding:6px 16px;background:var(--primary);color:#fff;
             border:none;border-radius:var(--r);cursor:pointer;font-size:13px;
             font-weight:600;transition:opacity .15s"
      onmouseover="this.style.opacity='.85'"
      onmouseout="this.style.opacity='1'">
      Post
    </button>
  </div>
  <div id="feed-comment-err-${esc(projectId)}"
       style="font-size:12px;color:var(--danger);margin-top:6px;display:none"></div>
</div>` : `
<div style="padding:10px 16px;border-top:1px solid var(--border);
            font-size:12px;color:var(--text-muted);background:var(--bg-surface)">
  <a href="#" onclick="nav('login');return false"
     style="color:var(--primary)">Log in</a> to post a comment.
</div>`;

    panel.innerHTML = `
<div>
  ${state.loading && state.comments.length === 0
        ? `<div style="padding:16px;text-align:center;color:var(--text-muted);font-size:13px">
         <i class="fa-solid fa-spinner fa-spin"></i> Loading comments…
       </div>`
        : state.comments.length === 0
            ? `<div style="padding:16px;text-align:center;color:var(--text-muted);font-size:12px">
           No comments yet. Be the first to share your experience.
         </div>`
            : `<div>${commentList}</div>`
    }
  ${loadMoreBtn}
  ${form}
</div>`;
}

// _buildCommentRow renders a single comment inside the panel.
function _buildCommentRow(projectId, cm) {
    const avatarHtml = cm.authorAvatarUrl
        ? `<img src="${esc(cm.authorAvatarUrl)}"
               style="width:28px;height:28px;border-radius:50%;object-fit:cover;flex-shrink:0">`
        : `<span style="width:28px;height:28px;border-radius:50%;background:var(--primary);
                        color:#fff;font-size:12px;font-weight:700;display:flex;
                        align-items:center;justify-content:center;flex-shrink:0">
             ${esc((cm.authorDisplayName || cm.authorUsername || '?')[0].toUpperCase())}
           </span>`;

    const subRatings = [];
    if (cm.docRating  > 0) subRatings.push(`<span style="font-size:11px;color:var(--text-muted)">Docs: ${_miniStars(cm.docRating)}</span>`);
    if (cm.codeRating > 0) subRatings.push(`<span style="font-size:11px;color:var(--text-muted)">Code: ${_miniStars(cm.codeRating)}</span>`);
    const subRatingHtml = subRatings.length
        ? `<div style="display:flex;gap:12px;margin-top:4px">${subRatings.join('')}</div>`
        : '';

    // Show a delete button to the comment author or an admin.
    const canDelete = S.user && (S.user.id === cm.userId || S.user.role === 'admin');
    const deleteBtn = canDelete
        ? `<button onclick="feedDeleteComment('${esc(projectId)}','${esc(cm.id)}')"
             style="background:none;border:none;cursor:pointer;font-size:11px;
                    color:var(--text-muted);padding:0;margin-left:auto;flex-shrink:0"
             onmouseover="this.style.color='var(--danger)'"
             onmouseout="this.style.color='var(--text-muted)'"
             title="Delete comment">
             <i class="fa-solid fa-trash-can"></i>
           </button>`
        : '';

    const relTime = _relativeTime(new Date(cm.createdAt));

    return `
<div style="padding:10px 16px;border-bottom:1px solid var(--border);display:flex;gap:10px">
  ${avatarHtml}
  <div style="flex:1;min-width:0">
    <div style="display:flex;align-items:center;gap:8px">
      <span style="font-size:12px;font-weight:600;color:var(--text-primary)">
        ${esc(cm.authorDisplayName || cm.authorUsername)}
      </span>
      <span style="font-size:11px;color:var(--text-muted)">${relTime}</span>
      ${deleteBtn}
    </div>
    <p style="font-size:13px;color:var(--text-secondary);margin:4px 0 0;
              line-height:1.5;word-break:break-word">${esc(cm.body)}</p>
    ${subRatingHtml}
  </div>
</div>`;
}

// _buildSubRatingInput renders an interactive 1-5 star selector for doc/code sub-ratings.
// The selected value is stored in data attributes so feedSubmitComment can read it.
function _buildSubRatingInput(kind, projectId) {
    const inputId = `feed-${kind}-rating-${projectId}`;
    return `<div id="${inputId}" data-value="0" style="display:flex;gap:2px">` +
        Array.from({length: 5}, (_, i) =>
            `<i class="fa-regular fa-star"
                data-v="${i + 1}"
                style="font-size:14px;color:var(--border);cursor:pointer;transition:color .1s"
                onmouseover="feedSubRatingHover('${inputId}',${i+1})"
                onmouseout="feedSubRatingOut('${inputId}')"
                onclick="feedSubRatingPick('${inputId}',${i+1})"></i>`
        ).join('') +
        '</div>';
}

// feedSubRatingHover highlights stars up to the hovered index.
export function feedSubRatingHover(inputId, n) {
    const el = document.getElementById(inputId);
    if (!el) return;
    el.querySelectorAll('i').forEach((star, i) => {
        star.className = i < n ? 'fa-solid fa-star' : 'fa-regular fa-star';
        star.style.color = i < n ? '#F59E0B' : 'var(--border)';
    });
}

// feedSubRatingOut restores stars to the currently selected value.
export function feedSubRatingOut(inputId) {
    const el = document.getElementById(inputId);
    if (!el) return;
    const v = parseInt(el.dataset.value || '0', 10);
    el.querySelectorAll('i').forEach((star, i) => {
        star.className = i < v ? 'fa-solid fa-star' : 'fa-regular fa-star';
        star.style.color = i < v ? '#F59E0B' : 'var(--border)';
    });
}

// feedSubRatingPick locks in the selected star value.
export function feedSubRatingPick(inputId, n) {
    const el = document.getElementById(inputId);
    if (!el) return;
    // Toggle off if clicking the currently selected value.
    const current = parseInt(el.dataset.value || '0', 10);
    const newVal  = current === n ? 0 : n;
    el.dataset.value = String(newVal);
    feedSubRatingOut(inputId); // re-render with new selected value
}

// feedLoadMoreComments loads the next page of comments for a project.
export async function feedLoadMoreComments(projectId) {
    const state = _commentPanels[projectId];
    if (!state) return;
    const nextPage = state.page + 1;
    await _loadComments(projectId, nextPage);
    _renderCommentPanel(projectId);
}

// feedSubmitComment posts the comment form content to the server.
export async function feedSubmitComment(projectId) {
    if (!S.user) { await showAlert('warning', 'Faça login para comentar.'); return; }

    const bodyEl = document.getElementById(`feed-comment-body-${projectId}`);
    const errEl  = document.getElementById(`feed-comment-err-${projectId}`);
    if (!bodyEl) return;

    const body = bodyEl.value.trim();
    if (!body) {
        if (errEl) { errEl.textContent = 'Comment cannot be empty.'; errEl.style.display = 'block'; }
        return;
    }

    // Read optional sub-ratings.
    const docEl  = document.getElementById(`feed-doc-rating-${projectId}`);
    const codeEl = document.getElementById(`feed-code-rating-${projectId}`);
    const docRating  = docEl  ? parseInt(docEl.dataset.value  || '0', 10) : 0;
    const codeRating = codeEl ? parseInt(codeEl.dataset.value || '0', 10) : 0;

    if (errEl) errEl.style.display = 'none';

    const r = await api('POST', `/api/v1/projects/${projectId}/comments`, {
        body, docRating, codeRating,
    });

    if (r?.metadata?.status === 200 || r?.metadata?.status === 201) {
        // Clear the form.
        bodyEl.value = '';
        if (docEl)  { docEl.dataset.value  = '0'; feedSubRatingOut(`feed-doc-rating-${projectId}`); }
        if (codeEl) { codeEl.dataset.value = '0'; feedSubRatingOut(`feed-code-rating-${projectId}`); }

        // Reload the first page so the new comment appears at the top.
        if (_commentPanels[projectId]) _commentPanels[projectId].comments = [];
        await _loadComments(projectId, 1);
        _renderCommentPanel(projectId);
    } else {
        const msg = r?.metadata?.error || 'Could not post comment.';
        if (errEl) { errEl.textContent = msg; errEl.style.display = 'block'; }
    }
}

// feedDeleteComment removes a comment after a brief confirmation.
export async function feedDeleteComment(projectId, commentId) {
    if (!await showConfirm('Excluir este comentário?', { okLabel: 'Excluir', danger: true })) return;
    const r = await api('DELETE', `/api/v1/projects/${projectId}/comments/${commentId}`);
    if (r?.metadata?.status === 200) {
        // Remove from local cache and re-render.
        const state = _commentPanels[projectId];
        if (state) {
            state.comments = state.comments.filter(c => c.id !== commentId);
            state.total    = Math.max(0, state.total - 1);
        }
        _renderCommentPanel(projectId);
        const badge = document.getElementById(`feed-comment-count-${projectId}`);
        if (badge && state) badge.textContent = state.total > 0 ? state.total : '';
    }
}

// ─── Reports ─────────────────────────────────────────────────────────────────
//
// The report dialog is a centered overlay with four reason options and an
// optional free-text field. It uses inline styles for self-containment.
//
// If the user has already reported the project, the dialog shows a status
// message instead of the form.

// feedOpenReport opens the report dialog for a project.
export async function feedOpenReport(projectId) {
    if (!S.user) { await showAlert('warning', 'Faça login para reportar projetos.'); return; }

    // Check if the user has already reported this project.
    const check = await api('GET', `/api/v1/projects/${projectId}/report`);
    const alreadyReported = check?.data?.reported === true;
    const existingStatus  = check?.data?.status || 'pending';
    const existingReason  = check?.data?.reason || '';

    _showReportDialog(projectId, alreadyReported, existingStatus, existingReason);
}

function _showReportDialog(projectId, alreadyReported, existingStatus, existingReason) {
    document.getElementById('feed-report-dialog')?.remove();

    const overlay = document.createElement('div');
    overlay.id = 'feed-report-dialog';
    overlay.style.cssText =
        'position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:10000;' +
        'display:flex;align-items:center;justify-content:center;padding:16px';

    const statusLabel = {
        pending:   'Your report is pending review.',
        reviewed:  'Your report has been reviewed.',
        dismissed: 'Your report was reviewed and dismissed.',
    };

    const alreadyHtml = `
<div style="text-align:center;padding:8px 0 16px">
  <i class="fa-solid fa-flag" style="font-size:28px;color:var(--warning);display:block;margin-bottom:10px"></i>
  <p style="font-size:14px;color:var(--text-secondary);margin:0 0 6px">
    You have already reported this project.
  </p>
  <p style="font-size:13px;color:var(--text-muted);margin:0">
    Reason: <strong>${esc(existingReason)}</strong> &nbsp;·&nbsp;
    ${statusLabel[existingStatus] || existingStatus}
  </p>
</div>`;

    const reasons = [
        { value: 'offensive',   label: 'Offensive content',
            hint: 'Harmful, abusive, or violates community norms' },
        { value: 'off_topic',   label: 'Off-topic / irrelevant',
            hint: 'Random files unrelated to IoT / embedded systems' },
        { value: 'spam',        label: 'Spam or advertisement',
            hint: 'Duplicate or low-value content with no technical merit' },
        { value: 'misleading',  label: 'Misleading or deceptive',
            hint: 'Description or code does not match what is claimed' },
    ];

    const formHtml = `
<p style="font-size:13px;color:var(--text-secondary);margin:0 0 16px;line-height:1.6">
  Help us keep the community healthy. Reports are reviewed by moderators
  and are never visible to the project author.
</p>
${reasons.map(r => `
<label style="display:flex;align-items:flex-start;gap:10px;margin-bottom:10px;cursor:pointer;
              padding:10px 12px;border:1px solid var(--border);border-radius:var(--r);
              transition:border-color .15s;background:var(--bg-card)"
       onmouseover="this.style.borderColor='var(--primary)'"
       onmouseout="_reportHoverOut(this,'${r.value}')">
  <input type="radio" name="report-reason" value="${r.value}"
         onchange="_reportReasonChange()"
         style="margin-top:3px;accent-color:var(--primary);flex-shrink:0">
  <div>
    <div style="font-size:13px;font-weight:600;color:var(--text-primary)">${r.label}</div>
    <div style="font-size:11px;color:var(--text-muted)">${r.hint}</div>
  </div>
</label>`).join('')}
<div style="margin-top:8px">
  <label style="font-size:12px;color:var(--text-muted);display:block;margin-bottom:4px">
    Additional context <span style="opacity:.7">(optional)</span>
  </label>
  <textarea id="report-details"
    style="width:100%;min-height:60px;padding:8px 10px;font-size:13px;
           border:1px solid var(--border);border-radius:var(--r);resize:vertical;
           font-family:var(--font);background:var(--bg-input);
           color:var(--text-primary);box-sizing:border-box"
    placeholder="Describe the problem…"
    maxlength="500"></textarea>
</div>
<div id="report-err" style="font-size:12px;color:var(--danger);margin-top:8px;display:none"></div>`;

    overlay.innerHTML = `
<div style="background:var(--bg-card);border-radius:var(--rl);padding:28px;
            width:100%;max-width:440px;box-shadow:0 8px 40px rgba(0,0,0,.4);
            border:1px solid var(--border);animation:fi .2s ease;max-height:90vh;overflow-y:auto">
  <div style="display:flex;align-items:center;gap:10px;margin-bottom:16px">
    <i class="fa-solid fa-flag" style="color:var(--danger);font-size:16px"></i>
    <h2 style="font-size:16px;font-weight:700;margin:0">Report Project</h2>
    <button onclick="feedCloseReport()"
      style="margin-left:auto;background:none;border:none;cursor:pointer;
             font-size:20px;color:var(--text-muted);line-height:1">×</button>
  </div>
  ${alreadyReported ? alreadyHtml : formHtml}
  <div style="display:flex;gap:10px;margin-top:20px">
    <button class="btn btn-secondary btn-sm" onclick="feedCloseReport()" style="flex:1">
      ${alreadyReported ? 'Close' : 'Cancel'}
    </button>
    ${!alreadyReported ? `
    <button class="btn btn-danger btn-sm" id="report-submit-btn"
            onclick="feedSubmitReport('${esc(projectId)}')" style="flex:2" disabled>
      <i class="fa-solid fa-flag"></i> Submit Report
    </button>` : ''}
  </div>
</div>`;

    overlay.addEventListener('click', e => {
        if (e.target === overlay) feedCloseReport();
    });

    document.body.appendChild(overlay);
}

// _reportHoverOut restores the label border when the mouse leaves,
// unless the radio inside it is checked.
window._reportHoverOut = function(label, value) {
    const radio = label.querySelector('input[type="radio"]');
    if (!radio?.checked) label.style.borderColor = 'var(--border)';
};

// _reportReasonChange enables the submit button once a reason is selected
// and highlights the selected label.
window._reportReasonChange = function() {
    const selected = document.querySelector('input[name="report-reason"]:checked')?.value;
    const btn = document.getElementById('report-submit-btn');
    if (btn) btn.disabled = !selected;

    // Highlight selected label, reset others.
    document.querySelectorAll('input[name="report-reason"]').forEach(radio => {
        const label = radio.closest('label');
        if (label) {
            label.style.borderColor = radio.checked ? 'var(--danger)' : 'var(--border)';
            label.style.background  = radio.checked ? '#fff5f5'       : 'var(--bg-card)';
        }
    });
};

export function feedCloseReport() {
    document.getElementById('feed-report-dialog')?.remove();
}

export async function feedSubmitReport(projectId) {
    const reason  = document.querySelector('input[name="report-reason"]:checked')?.value;
    const details = document.getElementById('report-details')?.value?.trim() || '';
    const errEl   = document.getElementById('report-err');
    const btn     = document.getElementById('report-submit-btn');

    if (!reason) {
        if (errEl) { errEl.textContent = 'Please select a reason.'; errEl.style.display = 'block'; }
        return;
    }

    if (btn) { btn.disabled = true; btn.innerHTML = '<i class="fa-solid fa-spinner fa-spin"></i> Sending…'; }

    const r = await api('POST', `/api/v1/projects/${projectId}/report`, { reason, details });

    if (r?.metadata?.status === 200 || r?.metadata?.status === 201) {
        feedCloseReport();
        // Show a brief success toast via the page alert mechanism if available.
        _showFeedToast('Report submitted. Thank you for helping keep the community safe.');
    } else if (r?.metadata?.status === 409) {
        if (errEl) {
            errEl.textContent = 'You have already reported this project.';
            errEl.style.display = 'block';
        }
        if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-flag"></i> Submit Report'; }
    } else {
        const msg = r?.metadata?.error || 'Could not submit report.';
        if (errEl) { errEl.textContent = msg; errEl.style.display = 'block'; }
        if (btn) { btn.disabled = false; btn.innerHTML = '<i class="fa-solid fa-flag"></i> Submit Report'; }
    }
}

// ─── Utilities ────────────────────────────────────────────────────────────────

// _miniStars returns a small inline star string like "★★★☆☆" for sub-ratings.
function _miniStars(rating) {
    return Array.from({length: 5}, (_, i) =>
        `<i class="fa-${i < rating ? 'solid' : 'regular'} fa-star"
            style="font-size:10px;color:${i < rating ? '#F59E0B' : 'var(--border)'}"></i>`
    ).join('');
}

// _relativeTime converts a Date to a human-readable relative string.
function _relativeTime(date) {
    const secs = Math.round((Date.now() - date) / 1000);
    if (secs < 60)           return 'just now';
    if (secs < 3600)         return `${Math.round(secs / 60)}m ago`;
    if (secs < 86400)        return `${Math.round(secs / 3600)}h ago`;
    if (secs < 86400 * 30)   return `${Math.round(secs / 86400)}d ago`;
    if (secs < 86400 * 365)  return `${Math.round(secs / 86400 / 30)}mo ago`;
    return `${Math.round(secs / 86400 / 365)}y ago`;
}

// _showFeedToast briefly displays a toast message at the bottom of the page.
function _showFeedToast(msg) {
    const existing = document.getElementById('feed-toast');
    if (existing) existing.remove();
    const toast = document.createElement('div');
    toast.id = 'feed-toast';
    toast.style.cssText =
        'position:fixed;bottom:24px;left:50%;transform:translateX(-50%);' +
        'background:#1E293B;color:#fff;padding:10px 20px;border-radius:var(--r);' +
        'font-size:13px;z-index:10001;box-shadow:0 4px 16px rgba(0,0,0,.3);' +
        'animation:fi .2s ease;white-space:nowrap';
    toast.textContent = msg;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 4000);
}

function _reRenderCard(idx) {
    const grid = document.getElementById('feed-grid');
    if (!grid) return;
    const children = grid.children;
    if (children[idx]) {
        const tmp = document.createElement('div');
        tmp.innerHTML = _buildCard(_cards[idx]);
        grid.replaceChild(tmp.firstElementChild, children[idx]);
    }
}

function _setStatus(html) {
    const el = document.getElementById('feed-status');
    if (el) el.innerHTML = html;
}

function esc(str) {
    if (!str && str !== 0) return '';
    return String(str)
        .replace(/&/g,'&amp;').replace(/</g,'&lt;')
        .replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}
