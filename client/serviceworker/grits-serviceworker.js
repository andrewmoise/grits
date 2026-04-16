const swDirHash = "{{SW_DIR_HASH}}";
importScripts('/grits-GritsClient-sw.js');

const gritsClient = new GritsClient();

function getVolume() {
    return gritsClient.volume(self.location.origin, 'sites');
}

self.addEventListener('install', e => e.waitUntil(self.skipWaiting()));
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));

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

async function unregisterAndPassThrough() {
    console.log('[SW] Hash mismatch — unregistering');
    const clients = await self.clients.matchAll({ type: 'window' });
    for (const client of clients) {
        client.postMessage({ type: 'DELETE_COOLDOWN_COOKIE' });
    }
    await self.registration.unregister();
}

async function swFetch(path, options = {}) {
    const url = `${self.location.origin}${path}`;
    const resp = await fetch(url, {
        ...options,
        headers: {
            ...(options.headers ?? {}),
            'X-Grits-SW-Sentinel': '1',
        },
    });
    const serverHash = resp.headers.get('X-Grits-SW-Hash');
    if (serverHash && serverHash !== swDirHash) {
        console.log(`[SW] hash mismatch server=${serverHash} local=${swDirHash}`);
        unregisterAndPassThrough();
    }
    return resp;
}

self.addEventListener('fetch', event => {
    const parsed = new URL(event.request.url);
    if (event.request.method !== 'GET') return;
    if (parsed.origin !== self.location.origin) return;
    if (parsed.pathname.startsWith('/grits/')) return;

    event.respondWith(handleRequest(parsed));
});

async function handleRequest(parsed) {
    const hostname = parsed.hostname;
    const urlPath = decodeURIComponent(parsed.pathname);

    // Strip trailing slash for lookup — root '/' becomes 'hostname/content'
    let lookupPath = `${hostname}/content${urlPath}`.replace(/\/+$/, '');

    if (DEBUG) console.log(`[SW] handleRequest: ${urlPath} → ${lookupPath}`);

    try {
        const vol = getVolume();
        let file = await vol.lookup(lookupPath);

        if (file.isDir()) {
            if (!urlPath.endsWith('/')) {
                if (DEBUG) console.log(`[SW] redirect: ${urlPath} → ${urlPath}/`);
                return Response.redirect(parsed.href + '/', 302);
            }
            lookupPath = `${lookupPath}/index.html`;
            if (DEBUG) console.log(`[SW] directory → ${lookupPath}`);
            file = await vol.lookup(lookupPath);
        }

        if (DEBUG) console.log(`[SW] serving: ${lookupPath} contentCID=${file.contentCID()}`);

        const blobResp = await swFetch(`/grits/v1/blob/${file.contentCID()}`);
        if (!blobResp.ok) {
            console.error(`[SW] blob fetch failed: ${blobResp.status} for ${file.contentCID()}`);
            return new Response('Blob not found', { status: 404 });
        }

        return new Response(blobResp.body, {
            status: 200,
            headers: {
                'Content-Type': mimeFromPath(lookupPath),
                'Cache-Control': 'no-store',
                'X-Grits-SW-Controlled': '1',
            },
        });

    } catch (err) {
        console.error(`[SW] error handling ${lookupPath}:`, err);
        return new Response(`<h1>Error</h1><pre>${err.message}</pre>`, {
            status: 500,
            headers: { 'Content-Type': 'text/html' },
        });
    }
}