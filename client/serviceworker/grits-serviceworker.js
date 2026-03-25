// grits-serviceworker.js
const swDirHash = "{{SW_DIR_HASH}}";
const swScriptHash = "{{SW_SCRIPT_HASH}}";

const debugServiceworker = false;

// Import GritsClient
importScripts('/grits/v1/content/client/GritsClient-sw.js');

// One client per volume, keyed by volume name.
// We always need one for 'content' (user content) and one for 'client' (our own JS).
const gritsClients = new Map();

function getClient(volume) {
    if (!gritsClients.has(volume)) {
        const client = new GritsClient({
            serverUrl: self.location.origin,
            volume: volume,
        });
        gritsClients.set(volume, client);
    }
    return gritsClients.get(volume);
}

function cleanupClients() {
    for (const [, client] of gritsClients) {
        if (typeof client.destroy === 'function') client.destroy();
    }
    gritsClients.clear();
}

// Check whether the server has pushed a new service worker version.
// Returns true if an update was triggered (caller should stop handling the request).
function checkForUpdate(response) {
    const serverHash = response?.headers?.get('X-Grits-Service-Worker-Hash');
    if (serverHash && serverHash !== swDirHash) {
        if (debugServiceworker) {
            console.log(`[Grits] SW update needed: have ${swDirHash}, server has ${serverHash}`);
        }
        cleanupClients();
        self.registration.update().catch(err =>
            console.error('[Grits] Failed to trigger SW update:', err));
        return true;
    }
    return false;
}

self.addEventListener('install', event => {
    console.log('[Grits] Installing');
    event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', event => {
    console.log('[Grits] Activating');
    event.waitUntil(self.clients.claim());
});

self.addEventListener('message', event => {
    if (event.data?.type === 'NAVIGATE') {
        if (debugServiceworker) console.log('[Grits] Navigation: resetting roots');
        for (const [, client] of gritsClients) client.resetRoot();
    }
});

self.addEventListener('fetch', event => {
    const url = event.request.url;

    // Only intercept GET requests.
    if (event.request.method !== 'GET') return;

    // Never intercept our own API calls.
    if (url.includes('/grits/v1/') || url.includes('/grits-')) return;

    // Only intercept same-origin requests.
    const parsed = new URL(url);
    if (parsed.origin !== self.location.origin) return;

    event.respondWith((async () => {
        // Map the URL path to content/{hostname}/public/{path} in the content volume.
        const hostname = parsed.hostname;
        const path = `${hostname}/public${parsed.pathname}`;

        if (debugServiceworker) {
            console.debug(`[Grits] Intercepting ${parsed.pathname} → content:${path}`);
        }

        try {
            const client = getClient('content');
            const response = await client.fetchFile(path);

            // Piggyback update detection on real responses.
            checkForUpdate(response);

            return response;
        } catch (err) {
            console.error(`[Grits] Fetch failed for ${path}:`, err);
            // Fall back to network on error.
            return fetch(event.request);
        }
    })());
});

self.addEventListener('unload', () => {
    cleanupClients();
});