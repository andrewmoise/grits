const swDirHash = "{{SW_DIR_HASH}}";

const DEBUG = true;

self.addEventListener('install', e => e.waitUntil(self.skipWaiting()));
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));

self.addEventListener('message', event => {
    if (event.data?.type === 'NAVIGATE') {
        if (DEBUG) console.log('[SW] Navigation message received');
    }
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

function deleteCooldownCookie() {
    document.cookie = 'grits-sw-loading=; path=/; max-age=0; SameSite=Lax';
}

async function unregisterAndPassThrough() {
    if (DEBUG) console.log('[SW] Hash mismatch — unregistering');
    // Delete cooldown cookie so the interstitial can fire immediately
    // We can't access document from SW, so we notify all clients to delete it
    const clients = await self.clients.matchAll({ type: 'window' });
    for (const client of clients) {
        client.postMessage({ type: 'DELETE_COOLDOWN_COOKIE' });
    }
    await self.registration.unregister();
}

self.addEventListener('fetch', event => {
    const parsed = new URL(event.request.url);
    if (event.request.method !== 'GET') return;
    if (parsed.origin !== self.location.origin) return;
    if (parsed.pathname.startsWith('/grits/')) return;

    event.respondWith(handleRequest(parsed));
});

// fetch() wrapper that adds sentinel header and checks for SW hash mismatch
async function swFetch(path, options = {}) {
    const url = `${self.location.origin}${path}`;
    console.log(`[SW] swFetch: ${options.method ?? 'GET'} ${url}`);
    const resp = await fetch(url, {
        ...options,
        headers: {
            ...(options.headers ?? {}),
            'X-Grits-SW-Sentinel': '1',
        },
    });
    console.log(`[SW] swFetch: ${resp.status} ${url}`);
    const serverHash = resp.headers.get('X-Grits-SW-Hash');
    console.log(`[SW] swFetch: X-Grits-SW-Hash=${serverHash} swDirHash=${swDirHash} match=${serverHash === swDirHash}`);
    if (serverHash && serverHash !== swDirHash) {
        console.log(`[SW] hash mismatch — triggering unregister`);
        unregisterAndPassThrough();
    }
    return resp;
}

async function lookupLeaf(path) {
    console.log(`[SW] lookupLeaf: ${path}`);
    const resp = await swFetch('/grits/v1/lookup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ volume: 'sites', paths: [path] }),
    });
    if (!resp.ok) {
        console.log(`[SW] lookupLeaf: failed ${resp.status} for ${path}`);
        return null;
    }
    const result = await resp.json();
    console.log(`[SW] lookupLeaf: result paths=${JSON.stringify(result.paths?.map(p => p.path))} isPartial=${result.isPartial}`);
    const leaf = result.paths?.find(e => e.path === path);
    if (!leaf) console.log(`[SW] lookupLeaf: no leaf found for path "${path}" in result`);
    if (result.isPartial) console.log(`[SW] lookupLeaf: result is partial`);
    if (!leaf || result.isPartial) return null;
    console.log(`[SW] lookupLeaf: found addr=${leaf.addr}`);
    return leaf; 
}

async function handleRequest(parsed) {
    const hostname = parsed.hostname;
    const urlPath = decodeURIComponent(parsed.pathname);
    // Strip trailing slash — root / becomes empty string, giving us hostname/content
    const lookupPath = `${hostname}/content${urlPath}`.replace(/\/+$/, '');

    console.log(`[SW] handleRequest: url=${parsed.href} lookupPath=${lookupPath}`);

    const leaf = await lookupLeaf(lookupPath);
    if (!leaf) {
        console.log(`[SW] handleRequest: no leaf for ${lookupPath}, returning 404`);
        return new Response('Not found', { status: 404 });
    }

    console.log(`[SW] handleRequest: fetching metadata blob ${leaf.addr}`);
    const metaResp = await swFetch(`/grits/v1/blob/${leaf.addr}`);
    if (!metaResp.ok) {
        console.log(`[SW] handleRequest: metadata fetch failed ${metaResp.status}`);
        return new Response('Metadata not found', { status: 404 });
    }
    const meta = await metaResp.json();
    console.log(`[SW] handleRequest: metadata type=${meta.type} contentHash=${meta.contentHash}`);

    let contentHash = meta.contentHash;
    let finalPath = urlPath;

    if (meta.type === 'dir') {
        if (!parsed.pathname.endsWith('/')) {
            console.log(`[SW] handleRequest: directory without trailing slash, redirecting`);
            return Response.redirect(parsed.href + '/', 302);
        }
        console.log(`[SW] handleRequest: is directory with trailing slash, looking up index.html`);
        const indexLeaf = await lookupLeaf(`${lookupPath}/index.html`);
        if (!indexLeaf) {
            console.log(`[SW] handleRequest: no index.html found`);
            return new Response('Not found', { status: 404 });
        }
        const indexMetaResp = await swFetch(`/grits/v1/blob/${indexLeaf.addr}`);
        if (!indexMetaResp.ok) {
            console.log(`[SW] handleRequest: index.html metadata fetch failed ${indexMetaResp.status}`);
            return new Response('Metadata not found', { status: 404 });
        }
        const indexMeta = await indexMetaResp.json();
        contentHash = indexMeta.contentHash;
        finalPath = urlPath + 'index.html';
    }

    console.log(`[SW] handleRequest: fetching content blob ${contentHash}`);
    const blobResp = await swFetch(`/grits/v1/blob/${contentHash}`);
    if (!blobResp.ok) {
        console.log(`[SW] handleRequest: blob fetch failed ${blobResp.status}`);
        return new Response('Blob not found', { status: 404 });
    }

    const contentType = mimeFromPath(finalPath);
    console.log(`[SW] handleRequest: serving ${finalPath} as ${contentType}`);

    return new Response(blobResp.body, {
        status: 200,
        headers: {
            'Content-Type': contentType,
            'Cache-Control': 'no-store',
        },
    });
}