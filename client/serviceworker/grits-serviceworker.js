const swDirHash = "{{SW_DIR_HASH}}";
const SW_START_TIME = Date.now();

importScripts('/grits-GritsClient-sw.js');

const gritsClient = new GritsClient();

// Test hash override — set when a request arrives carrying X-Grits-Sw-Hash-Override.
// null   = no override active, use real server hash
// 'none' = suppress the hash header (server told to omit it)
// string = fake hash to send to server on subsequent lookups
let testHashOverride = null;

// Once we've decided to unregister, stop intercepting navigate requests so the
// interstitial's window.location.replace('/') doesn't re-trigger the hash check.
let _unregistering = false;

function getVolume() {
    return gritsClient.volume(self.location.origin, 'sites');
}

self.addEventListener('install',  e => {
    console.log('[SW] install event, startTime=' + SW_START_TIME + ' swDirHash=' + swDirHash.slice(0, 16) + '…');
    e.waitUntil(self.skipWaiting());
});
self.addEventListener('activate', e => {
    console.log('[SW] activate event');
});

const mimeTypes = {
    'html':  'text/html',
    'css':   'text/css',
    'js':    'application/javascript',
    'json':  'application/json',
    'png':   'image/png',
    'jpg':   'image/jpeg',
    'jpeg':  'image/jpeg',
    'svg':   'image/svg+xml',
    'ico':   'image/x-icon',
    'woff':  'font/woff',
    'woff2': 'font/woff2',
    'ttf':   'font/ttf',
    'eot':   'application/vnd.ms-fontobject',
};

function mimeFromPath(path) {
    const ext = path.split('.').pop().toLowerCase();
    return mimeTypes[ext] ?? 'application/octet-stream';
}

async function unregisterAndRedirectToInterstitial(originalURL) {
    _unregistering = true;
    console.log('[SW] *** UNREGISTER PATH ENTERED *** originalURL=' + originalURL);

    const clients = await self.clients.matchAll({ type: 'window' });
    console.log('[SW] matched ' + clients.length + ' window clients to notify');
    for (const client of clients) {
        client.postMessage({ type: 'DELETE_COOLDOWN_COOKIE' });
    }

    self.registration.unregister().then(
        ok => console.log('[SW] registration.unregister() resolved: ' + ok),
        err => console.log('[SW] registration.unregister() rejected: ' + err)
    );

    const interstitialURL =
        '/grits/v1/sw-interstitial?target=' + encodeURIComponent(originalURL);
    console.log('[SW] returning 302 redirect to: ' + interstitialURL);
    return Response.redirect(interstitialURL, 302);
}

async function swFetch(path, options = {}, override = null) {
    const url = `${self.location.origin}${path}`;
    return fetch(url, {
        ...options,
        headers: {
            ...(options.headers ?? {}),
            'X-Grits-SW-Sentinel': '1',
            ...(override !== null
                ? { 'X-Grits-Sw-Hash-Override': override }
                : {}),
        },
    });
}

self.addEventListener('fetch', event => {
    const parsed = new URL(event.request.url);

    if (parsed.pathname === '/grits/v1/swtest/ping') {
        event.respondWith(new Response(JSON.stringify({
            startTime:        SW_START_TIME,
            swDirHash:        swDirHash,
            testHashOverride: testHashOverride,
        }), {
            status:  200,
            headers: { 'Content-Type': 'application/json' },
        }));
        return;
    }

    if (event.request.method !== 'GET') return;
    if (parsed.origin !== self.location.origin) return;
    if (parsed.pathname.startsWith('/grits/')) return;

    if (_unregistering && event.request.mode === 'navigate') return;

    if (event.request.mode === 'navigate') {
        console.log('[SW] navigate intercepted: ' + parsed.pathname + parsed.search);
    }

    event.respondWith(handleRequest(parsed, event.request));
});

async function handleRequest(parsed, request) {
    const hostname = parsed.hostname;
    const urlPath  = decodeURIComponent(parsed.pathname);
    const isNavigate = request.mode === 'navigate';

    console.log(
        '[SW] handleRequest ENTRY: path=' + parsed.pathname +
        ' search=' + parsed.search +
        ' mode=' + request.mode +
        ' isNavigate=' + isNavigate +
        ' currentOverride=' + testHashOverride
    );

    // Determine per-request override (do NOT persist globally)
    let override = null;

    const incomingOverride = request.headers.get('X-Grits-Sw-Hash-Override');
    const qsOverride = parsed.searchParams.get('grits-sw-hash-override');

    if (qsOverride !== null) {
        override = qsOverride;
        console.log('[SW] using query override (request-scoped): ' + override);
    } else if (incomingOverride !== null) {
        override = incomingOverride;
        console.log('[SW] using header override (request-scoped): ' + override);
    } else if (testHashOverride !== null) {
        // one-shot test override
        override = testHashOverride;
        testHashOverride = null;
        console.log('[SW] using one-shot test override, now cleared');
    }

    // For navigate requests, do a dedicated lightweight hash check before the
    // content lookup. This avoids the fast-walk race: vol.lookup() may return
    // from local cache before the background slow-lookup updates
    // _serviceWorkerHashes, so checking vol.getServiceWorkerHash() after the
    // lookup can return a stale value. A direct request to /grits/ (which hits
    // the catch-all 404 handler) is non-cacheable and always carries the current
    // X-Grits-Sw-Hash, with no race.
    if (isNavigate) {
        try {
            const hashResp = await swFetch('/grits/v1/sw-hash-check', {}, override);
            const serverHash = hashResp.headers.get('X-Grits-Sw-Hash');
            console.log(
                '[SW] NAVIGATE HASH CHECK: server=' + (serverHash ?? '(none)') +
                ' local=' + swDirHash +
                ' match=' + (serverHash === swDirHash)
            );
            if (serverHash && serverHash !== swDirHash) {
                console.warn('[SW] HASH MISMATCH on navigate — server=' + serverHash + ' local=' + swDirHash);
                return unregisterAndRedirectToInterstitial(parsed.href);
            }
            console.log('[SW] hash check passed (or no server hash), continuing');
        } catch (hashErr) {
            console.log('[SW] hash check request failed (ignoring): ' + hashErr.message);
        }
    }

    let lookupPath = `${hostname}/content${urlPath}`.replace(/\/+$/, '');

    console.log('[SW] about to lookup: ' + lookupPath);

    const vol = getVolume();

    try {
        let file = await vol.lookup(lookupPath);

        console.log('[SW] lookup resolved for ' + lookupPath + ', isDir=' + file.isDir());

        if (file.isDir()) {
            if (!urlPath.endsWith('/')) {
                console.log('[SW] redirect: ' + urlPath + ' → ' + urlPath + '/');
                return Response.redirect(parsed.href + '/', 302);
            }
            lookupPath = `${lookupPath}/index.html`;
            console.log('[SW] directory → ' + lookupPath);
            file = await vol.lookup(lookupPath);
        }

        console.log('[SW] serving: ' + lookupPath + ' contentCID=' + file.contentCID());

        const blobResp = await swFetch(`/grits/v1/blob/${file.contentCID()}`, {}, override);
        if (!blobResp.ok) {
            console.error('[SW] blob fetch failed: ' + blobResp.status + ' for ' + file.contentCID());
            return new Response('Blob not found', { status: 404 });
        }

        return new Response(blobResp.body, {
            status:  200,
            headers: {
                'Content-Type':           mimeFromPath(lookupPath),
                'Cache-Control':          'no-store',
                'X-Grits-SW-Controlled':  '1',
                'X-Grits-SW-Self-Hash':   swDirHash,
            },
        });

    } catch (err) {
        console.error('[SW] error handling ' + lookupPath + ':', err);
        return new Response('<h1>Error</h1><pre>' + err.message + '</pre>', {
            status:  500,
            headers: { 'Content-Type': 'text/html' },
        });
    }
}

self.addEventListener('message', event => {
    if (event.data?.type === 'GET_STATE') {
        event.ports[0].postMessage({
            startTime:        SW_START_TIME,
            swDirHash:        swDirHash,
            testHashOverride: testHashOverride,
        });
    } else if (event.data?.type === 'SET_HASH_OVERRIDE') {
        const val = event.data.value;
        testHashOverride = val;
        if (val !== null) {
            gritsClient.extraHeaders['X-Grits-Sw-Hash-Override'] = val;
        } else {
            delete gritsClient.extraHeaders['X-Grits-Sw-Hash-Override'];
        }
        if (event.ports[0]) event.ports[0].postMessage({ done: true });
    }
});
