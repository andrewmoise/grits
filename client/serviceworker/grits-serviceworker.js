const swDirHash = "{{SW_DIR_HASH}}";
const swScriptHash = "{{SW_SCRIPT_HASH}}";

const debugServiceworker = true;

importScripts('/grits-GritsClient-sw.js');

const gritsClient = new GritsClient();

function getVolume(volumeName) {
    return gritsClient.volume(self.location.origin, volumeName);
}

function cleanupClient() {
    gritsClient.destroy();
}

function checkForUpdate() {
    const serverHash = gritsClient.getServiceWorkerHash();
    console.log(`[Grits] checkForUpdate: serverHash=${JSON.stringify(serverHash)}, swDirHash=${swDirHash}`);
    if (serverHash === undefined) {
        console.log('[Grits] checkForUpdate: no server contact yet, doing nothing');
        return false;
    }
    if (serverHash === null) {
        console.log('[Grits] checkForUpdate: server returned no SW hash header, unregistering');
        cleanupClient();
        self.registration.unregister();
        return true;
    }
    if (serverHash !== swDirHash) {
        console.log(`[Grits] checkForUpdate: hash mismatch, updating`);
        cleanupClient();
        self.registration.update().catch(err =>
            console.error('[Grits] Failed to trigger SW update:', err));
        return true;
    }
    console.log('[Grits] checkForUpdate: hash matches, no action');
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
        getVolume('sites').resetRoot();
    }
});

self.addEventListener('fetch', event => {
    const url = event.request.url;

    if (event.request.method !== 'GET') return;

    const parsed = new URL(url);
    if (parsed.origin !== self.location.origin) return;

    // Pass lookup/link requests through to the server unchanged, but tag the
    // response so the page-side GritsClient knows a SW is active.
    if (url.includes('/grits/v1/lookup') || url.includes('/grits/v1/link')) {
        event.respondWith((async () => {
            const resp = await fetch(event.request);
            // GritsClient will have updated _serviceWorkerHash via _updateServiceWorkerHash
            // inside _slowLookup / _serverMultiLink. The SW itself doesn't need to act
            // here — checkForUpdate() is called after vol.lookup() in the content path.
            const headers = new Headers(resp.headers);
            headers.set('X-Grits-Served-By', 'sw');
            return new Response(resp.body, {
                status:     resp.status,
                statusText: resp.statusText,
                headers,
            });
        })());
        return;
    }

    // Don't intercept other grits API or SW script fetches.
    if (url.includes('/grits/v1/') || url.includes('/grits-')) return;

    event.respondWith((async () => {
        const hostname = parsed.hostname;
        const path = `${hostname}/content${parsed.pathname}`;

        console.log(`[Grits] fetch: intercepting ${url} → lookup path "${path}"`);

        try {
            const vol = getVolume('sites');
            console.log(`[Grits] fetch: calling vol.lookup("${path}")`);
            const file = await vol.lookup(path);
            console.log(`[Grits] fetch: lookup done, file=${file}, calling checkForUpdate`);

            if (checkForUpdate()) {
                console.log(`[Grits] fetch: checkForUpdate triggered action, falling back to fetch`);
                return fetch(event.request);
            }

            return file.get();
        } catch (err) {
            console.error(`[Grits] fetch: lookup/get failed for "${path}":`, err);
            return fetch(event.request);
        }
    })());
});

self.addEventListener('unload', () => {
    cleanupClient();
});