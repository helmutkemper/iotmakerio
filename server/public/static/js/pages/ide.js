// server/public/static/js/pages/ide.js — fullscreen IDE (embedded via iframe)
//
// Auth token handshake:
//   Template API calls from the WASM IDE require a Bearer JWT. The IDE runs
//   inside an iframe with COOP/COEP isolation headers, so the token must be
//   passed via window.postMessage (not via a shared global or URL parameter).
//
//   Protocol:
//     1. renderIDE() creates the iframe.
//     2. The iframe's index.html boots the WASM binary.
//     3. When the WASM is running, the iframe sends { type: "IDE_READY" }.
//     4. On receiving IDE_READY, this file sends the Bearer token:
//        { type: "IDE_AUTH_TOKEN", token: "Bearer " + S.token }
//     5. The WASM picks up the token from window._ideAuthToken via
//        rulesServer.GetAuthToken() during workspace initialization.
//
//   Security:
//     - The message listener only accepts IDE_READY from the same origin.
//     - The token is sent back only to the iframe's contentWindow.
//     - COOP: same-origin guarantees the iframe cannot be hijacked by
//       cross-origin pop-ups.
//
//   Why postMessage and not a URL parameter?
//     - URL parameters are visible in server logs and browser history.
//     - The iframe src URL is fixed (/ide/index.html); adding a token
//       to the query string would cause unnecessary re-loads.
//     - postMessage is the standard mechanism for same-origin iframe
//       communication when isolation headers are required.
//
//   Why wait for IDE_READY instead of sending immediately?
//     - The WASM starts loading asynchronously. If we send the token before
//       the message listener in index.html is registered, the token is lost.
//     - IDE_READY guarantees the listener is active before we send.
import { S } from '../state.js';

/**
 * Renders the fullscreen IDE page.
 * Creates the iframe wrapper once and reuses it on subsequent visits.
 * Registers a one-time message listener to forward the auth token after
 * the WASM signals it is ready.
 *
 * @param {HTMLElement} root - The SPA's root element (unused for IDE mode).
 */
export function renderIDE(root) {
    if (!S.user) { window.nav('login'); return; }

    document.body.classList.add('ide-mode');
    root.innerHTML = '';

    // Create the iframe wrapper once. On subsequent nav('ide') calls the
    // existing wrapper is reused and the token is re-sent in case the WASM
    // was reloaded (e.g. the user navigated away and back).
    let wrap = document.getElementById('ide-frame-wrap');
    const isNew = !wrap;

    if (isNew) {
        wrap    = document.createElement('div');
        wrap.id = 'ide-frame-wrap';

        // Append ?project= to the iframe src if a project ID is available.
        // Sources (in priority order):
        //   1. S.currentProjectID — set by the SPA when navigating to IDE from a project
        //   2. window._testProjectID — manual override for testing via browser console
        // The WASM reads window._ideProjectID from the URL param at boot time.
        const projID = S.currentProjectID || window._testProjectID || '';
        const projParam = projID ? '?project=' + encodeURIComponent(projID) : '';
        wrap.innerHTML = `
<iframe
  id="ide-iframe"
  src="/ide/index.html${projParam}"
  title="Editor IDE"
  allow="cross-origin-isolated">
</iframe>`;
        // The exit button is handled by the WASM panel's close button.
        // window._ideExit is called from inside the iframe via the panel rail.
        window._ideExit = () => nav('home');
        document.body.appendChild(wrap);

        // Persistent listener for IDE_EXIT posted by the WASM exit button.
        // Registered once per iframe lifetime and never removed, so it works
        // regardless of when the user clicks Exit (before or after IDE_READY).
        if (!window._ideExitListener) {
            window._ideExitListener = (event) => {
                if (event.origin !== window.location.origin) return;
                if (event.data && event.data.type === 'IDE_EXIT') {
                    nav('home');
                }
            };
            window.addEventListener('message', window._ideExitListener);
        }
    }

    // Register the token-forwarding listener.
    // We always re-register it (not just for new iframes) because the WASM
    // may have been reloaded since the last visit. The listener removes itself
    // after the first IDE_READY message to avoid accumulating handlers.
    _registerTokenForwarder();
}

/**
 * Registers a one-time listener for the IDE_READY signal.
 * When received, forwards the Bearer token to the iframe.
 *
 * Any previous listener registered by this function is removed first to
 * prevent duplicate handlers if the user navigates to the IDE multiple times.
 */
function _registerTokenForwarder() {
    // Remove any previously registered forwarder.
    if (window._ideTokenForwarder) {
        window.removeEventListener('message', window._ideTokenForwarder);
        window._ideTokenForwarder = null;
    }

    /**
     * Handles the IDE_READY message from the WASM iframe.
     * Sends the Bearer token back to the iframe and removes itself.
     *
     * @param {MessageEvent} event
     */
    function handleIDEReady(event) {
        // Only accept messages from the same origin.
        if (event.origin !== window.location.origin) return;

        const msg = event.data;
        if (!msg || msg.type !== 'IDE_READY') return;

        // Remove self — we only need to forward the token once per WASM boot.
        window.removeEventListener('message', handleIDEReady);
        window._ideTokenForwarder = null;

        // Find the iframe and send the token.
        const iframe = document.getElementById('ide-iframe');
        if (!iframe || !iframe.contentWindow) {
            console.warn('[ide.js] IDE_READY received but iframe not found');
            return;
        }

        if (!S.token) {
            // User logged out between creating the iframe and the WASM booting.
            // The WASM will start without a token — template endpoints will be
            // unavailable but the rest of the IDE works fine.
            console.warn('[ide.js] IDE_READY received but S.token is empty — templates disabled');
            return;
        }

        iframe.contentWindow.postMessage(
            { type: 'IDE_AUTH_TOKEN', token: 'Bearer ' + S.token },
            window.location.origin
        );
        console.log('[ide.js] Auth token forwarded to WASM IDE');

        // Send the current project ID for the live communication system.
        // The WASM reads window._ideProjectID to open a WebSocket scoped
        // to this project. If no project is active, live features are disabled.
        //
        // Português: Envia o ID do projeto atual para o sistema de comunicação
        // live. Se não houver projeto ativo, funcionalidades live são desabilitadas.
        const projectID = S.currentProjectID || '';
        if (projectID) {
            iframe.contentWindow.postMessage(
                { type: 'IDE_PROJECT_ID', projectID: projectID },
                window.location.origin
            );
            console.log('[ide.js] Project ID forwarded to WASM IDE:', projectID);
        }
    }

    window._ideTokenForwarder = handleIDEReady;
    window.addEventListener('message', handleIDEReady);
}

/**
 * Called by the router when leaving the IDE page.
 * Removes the iframe wrapper and cleans up the token forwarder.
 */
export function leaveIDE() {
    document.body.classList.remove('ide-mode');

    // Clean up the pending token forwarder to avoid a stale listener.
    if (window._ideTokenForwarder) {
        window.removeEventListener('message', window._ideTokenForwarder);
        window._ideTokenForwarder = null;
    }

    if (window._ideExitListener) {
        window.removeEventListener('message', window._ideExitListener);
        window._ideExitListener = null;
    }

    const wrap = document.getElementById('ide-frame-wrap');
    if (wrap) wrap.remove();
}
