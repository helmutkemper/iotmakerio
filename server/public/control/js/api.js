// server/public/control/js/api.js — HTTP client for the control panel.
//
// All requests to /api/control/v1/* use the control token (short-lived, 1h).
// The token exchange itself (POST /api/auth/control-token) uses the portal token.

import { CS } from './state.js';

/**
 * Makes an authenticated request to a control panel endpoint.
 * Automatically attaches the control panel Bearer token.
 *
 * @param {string} method  - HTTP method
 * @param {string} path    - path starting with /api/control/v1/...
 * @param {*}      [body]  - optional JSON body
 * @returns {Promise<{metadata: {status: number, error?: string}, data: any}>}
 */
export async function cpApi(method, path, body) {
    const opts = {
        method,
        headers: { 'Content-Type': 'application/json' },
    };
    if (CS.controlToken) {
        opts.headers['Authorization'] = 'Bearer ' + CS.controlToken;
    }
    if (body !== undefined) {
        opts.body = JSON.stringify(body);
    }

    const r = await fetch(path, opts);
    return r.json().catch(() => ({
        metadata: { status: r.status, error: 'parse error' },
        data: null,
    }));
}

/**
 * Exchanges the portal JWT for a short-lived control panel JWT.
 * Called once on login.
 *
 * @param {string} portalToken - portal Bearer token from localStorage
 * @returns {Promise<{token: string, expires_in: number} | null>}
 */
export async function exchangeForControlToken(portalToken) {
    const r = await fetch('/api/auth/control-token', {
        method: 'POST',
        headers: { 'Authorization': 'Bearer ' + portalToken },
    });
    const json = await r.json().catch(() => null);
    if (!json || json.metadata?.status !== 200) {
        // Return the error envelope so the caller can inspect the reason
        // and clear stale tokens when appropriate.
        return { error: json?.metadata?.error || 'unknown error', status: json?.metadata?.status || 0 };
    }
    return json.data;
}
