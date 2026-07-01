// /ide/server/public/static/js/api.js
// api.js — HTTP client, i18n loader, auth bootstrap, and locale management.
//
// This module is the single gateway for all server communication in the SPA.
// It also owns the locale lifecycle:
//
//   loadTranslations()       — fetches the translation bundle for S.locale
//   loadAvailableLocales()   — fetches the list of supported locales into S.locales
//   switchLocale(code)       — changes locale (state + localStorage + translations + DB)
//   checkAuth()              — validates token, populates S.user, syncs locale from DB
//
// Circular dependency note:
//   api.js must NOT import from router.js or sidebar.js. After calling
//   switchLocale(), the caller is responsible for re-rendering the UI
//   (e.g. renderSB() + render()). This keeps the dependency graph acyclic.
import { S } from './state.js';

// ─── JSON API ─────────────────────────────────────────────────────────────────

/**
 * JSON API call.
 * Automatically attaches Bearer token if S.token is set.
 */
export async function api(method, path, body) {
    const opts = {
        method,
        headers: { 'Content-Type': 'application/json' },
    };
    if (S.token) opts.headers['Authorization'] = 'Bearer ' + S.token;
    if (body !== undefined) opts.body = JSON.stringify(body);

    const r = await fetch(path, opts);
    return r.json().catch(() => ({ metadata: { status: r.status, error: 'parse error' } }));
}

// ─── File upload ──────────────────────────────────────────────────────────────

/**
 * Multipart file upload.
 * Attaches Bearer token if available.
 */
export async function apiUp(file) {
    const form = new FormData();
    form.append('file', file);
    const opts = { method: 'POST', body: form };
    if (S.token) opts.headers = { Authorization: 'Bearer ' + S.token };
    const r = await fetch('/api/upload', opts);
    return r.json();
}

// ─── i18n: load translations ──────────────────────────────────────────────────

/**
 * Load i18n translations for the current S.locale into S.tr.
 * Called during boot and after every locale switch.
 */
export async function loadTranslations() {
    try {
        const r = await api('GET', `/api/v1/translations/${S.locale}`);
        if (r?.data?.messages) {
            S.tr = {};
            r.data.messages.forEach(m => { S.tr[m.id] = m.other; });
        }
    } catch (_) { /* silent fail — fallback strings used */ }
}

// ─── i18n: available locales ──────────────────────────────────────────────────

/**
 * Load the list of supported UI locales into S.locales.
 *
 * Reuses the register-config endpoint which is public and lightly cached (60s).
 * The response shape is { locales: [{code, display}, ...] }.
 * Called once during boot so the sidebar and profile page can render locale
 * switchers without extra requests.
 */
export async function loadAvailableLocales() {
    try {
        const r = await api('GET', '/api/auth/register-config');
        if (r?.metadata?.status === 200 && Array.isArray(r.data?.locales)) {
            S.locales = r.data.locales;
        }
    } catch (_) { /* silent fail — switcher will be empty */ }
}

// ─── i18n: switch locale ──────────────────────────────────────────────────────

/**
 * Change the active UI locale.
 *
 * Steps:
 *   1. Update S.locale and persist to localStorage.
 *   2. Reload translations for the new locale.
 *   3. If authenticated, persist the preference to the DB via the profile
 *      locale endpoint (fire-and-forget — a failed save is non-fatal).
 *
 * The caller is responsible for re-rendering the UI after this returns
 * (e.g. renderSB() + render()) because api.js cannot import router.js
 * without creating a circular dependency.
 *
 * @param {string} newLocale — locale code, e.g. "en-US" or "pt-BR"
 */
export async function switchLocale(newLocale) {
    if (newLocale === S.locale) return;

    S.locale = newLocale;
    localStorage.setItem('locale', newLocale);

    // Update the <html lang="..."> attribute for accessibility and SEO.
    document.documentElement.lang = newLocale;

    await loadTranslations();

    // Persist to DB if the user is logged in. Fire-and-forget: we already
    // saved to localStorage, so the preference survives even if this fails.
    if (S.token) {
        api('PUT', '/api/v1/profile/locale', { locale: newLocale }).catch(() => {});
    }
}

// ─── Auth bootstrap ───────────────────────────────────────────────────────────

/**
 * Verify existing token, populate S.user, and load S.profile.
 * Clears the token if invalid.
 *
 * Locale sync: when the user's DB-stored preferredLocale differs from the
 * browser's localStorage locale, the DB value wins. This ensures that
 * logging in on a new device picks up the user's saved preference.
 *
 * S.profile is populated so the sidebar can display the avatar immediately
 * without the profile page needing to fetch it separately.
 */
export async function checkAuth() {
    if (!S.token) return;
    try {
        const r = await api('GET', '/api/auth/me');
        if (r?.metadata?.status === 200) {
            S.user = r.data;

            // Sync locale from the DB. The user's persisted preference takes
            // precedence over whatever is in localStorage. This handles the
            // case where the user logs in on a new browser/device.
            if (S.user.preferredLocale && S.user.preferredLocale !== S.locale) {
                S.locale = S.user.preferredLocale;
                localStorage.setItem('locale', S.locale);
                document.documentElement.lang = S.locale;
                // Reload translations for the corrected locale.
                await loadTranslations();
            }

            // Load profile in the background — non-fatal if it fails.
            // The profile page will re-fetch if needed.
            api('GET', '/api/v1/profile').then(pr => {
                if (pr?.metadata?.status === 200) {
                    S.profile = pr.data?.profile || null;
                }
            }).catch(() => { /* silent — sidebar falls back to initial */ });
        } else {
            S.token   = null;
            S.profile = null;
            localStorage.removeItem('token');
        }
    } catch (_) {
        S.token   = null;
        S.profile = null;
        localStorage.removeItem('token');
    }
}
