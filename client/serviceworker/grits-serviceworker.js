const DEBUG = true;

self.addEventListener('install', e => e.waitUntil(self.skipWaiting()));
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));

self.addEventListener('fetch', event => {
    const parsed = new URL(event.request.url);
    if (event.request.method !== 'GET') return;
    if (parsed.origin !== self.location.origin) return;
    if (parsed.pathname.startsWith('/grits/')) return;

    event.respondWith(handleRequest(parsed));
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

function mimeFromPath(pathname) {
    const ext = pathname.split('.').pop().toLowerCase();
    return mimeTypes[ext] ?? 'application/octet-stream';
}

async function handleRequest(parsed) {
    const hostname = parsed.hostname;
    const path = `${hostname}/content${decodeURIComponent(parsed.pathname)}`;

    if (DEBUG) console.log(`[SW] lookup: ${path}`);

    const lookupResp = await fetch(`${self.location.origin}/grits/v1/lookup`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ volume: 'sites', paths: [path] }),
    });

    if (!lookupResp.ok) return new Response('Not found', { status: 404 });

    const result = await lookupResp.json();
    const leaf = result.paths?.find(e => e.path === path);
    if (!leaf || result.isPartial) return new Response('Not found', { status: 404 });

    if (DEBUG) console.log(`[SW] metadata CID: ${leaf.addr}`);

    const metaResp = await fetch(`${self.location.origin}/grits/v1/blob/${leaf.addr}`);
    if (!metaResp.ok) return new Response('Metadata not found', { status: 404 });

    const meta = await metaResp.json();
    if (DEBUG) console.log(`[SW] content CID: ${meta.contentHash}`);

    const blobResp = await fetch(`${self.location.origin}/grits/v1/blob/${meta.contentHash}`);
    if (!blobResp.ok) return new Response('Blob not found', { status: 404 });

    return new Response(blobResp.body, {
        status: 200,
        headers: {
            'Content-Type': mimeFromPath(parsed.pathname),
            'Cache-Control': 'no-store',
        },
    });
}