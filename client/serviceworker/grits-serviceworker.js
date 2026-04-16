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

function mimeFromPath(pathname) {
    const ext = pathname.split('.').pop().toLowerCase();
    const mimeTypes = {
        'html':  'text/html',
        'css':   'text/css',
        'js':    'application/javascript',
        'json':  'application/json',
        'png':   'image/png',
        'jpg':   'image/jpeg',
        'jpeg':  'image/jpeg',
        'svg':   'image/svg+xml',
        'woff':  'font/woff',
        'woff2': 'font/woff2',
        'ttf':   'font/ttf',
        'eot':   'application/vnd.ms-fontobject',
    };
    return mimeTypes[ext] ?? 'application/octet-stream';
}

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
        let path = `${hostname}/content${decodeURIComponent(parsed.pathname)}`;

        console.log(`[Grits] fetch: intercepting ${url} → lookup path "${path}"`);

        try {
            const vol = getVolume('sites');
            console.log(`[Grits] fetch: calling vol.lookup("${path}")`);
            let file = await vol.lookup(path);
            console.log(`[Grits] fetch: lookup done, file=${file}, calling checkForUpdate`);

            let headers = {}

            if (file.isDir()) {
                console.log("Getting index");
                file = await file.indexHtml();
                path += "/index.html";
            }

            const rawResponse = await file.get();
            return new Response(rawResponse.body, {
                status: rawResponse.status,
                statusText: rawResponse.statusText,
                headers: {
                    ...Object.fromEntries(rawResponse.headers),
                    'Content-Type': mimeFromPath(parsed.pathname),
                    'Cache-Control': 'no-store',
                },
            });
        } catch (err) {
            console.error(`[Grits] fetch: failed for "${path}":`, err);
            return new Response(`<h1>Error</h1><pre>${err.message}</pre>`, {
                status: 500,
                headers: { 'Content-Type': 'text/html' },
            });
        }
    })());
});

self.addEventListener('unload', () => {
    cleanupClient();
});