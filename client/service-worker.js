// Global variable to hold the AppConfig
let AppConfig = {};

self.addEventListener('message', event => {
    if (event.data.type === 'CONFIG_UPDATE') {
        AppConfig = event.data.data;
        console.log('Service Worker configuration updated', AppConfig);
    }
});

self.addEventListener('fetch', event => {
    const { request } = event;
    const url = new URL(request.url);

    console.log('Service Worker intercepted fetch event for:', url.href);
    console.log('  Cache prefix is: ', AppConfig.cachePrefix);

    if (AppConfig.cachePrefix && url.href.startsWith(AppConfig.cachePrefix)) {
        console.log('Service Worker handling cache request:', url.href);

        // Correctly pass the async function to event.respondWith
        event.respondWith((async () => {
            await delay(3000); // 3000 ms delay
            console.log('Handling fetch after delay:', url.href);

            const cachePrefixLength = new URL(AppConfig.cachePrefix).pathname.length;
            const pathAfterPrefix = url.pathname.substring(cachePrefixLength);
            const [hash, length] = pathAfterPrefix.split(':');
            const lengthNum = parseInt(length, 10);

            if (hash && !isNaN(lengthNum)) {
                console.log("  match");
                // Ensure handleCacheFetch is returned
                return handleCacheFetch(request, AppConfig.cachePrefix, hash, lengthNum);
            } else {
                // If conditions fail, return a fetch to the network as a fallback
                return fetch(request);
            }
        })());
    } else {
        console.log('Service Worker skipping cache handling:', url.href);
        // For other cases, just fetch normally
        event.respondWith(fetch(request));
    }
});

function delay(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

async function handleCacheFetch(request, prefix, hash, length) {
    // Try to match in the browser cache first using the original request URL
    const cacheResponse = await caches.match(request);
    if (cacheResponse) { return cacheResponse; }

    // If not in cache, fetch from the first peer
    const peerUrl = new URL(AppConfig.peers[0] + hash + ':' + length);
    const networkResponse = await fetch(peerUrl);
    const clonedResponse = networkResponse.clone();

    // Validate length and hash of the response from the peer
    const responseBuffer = await clonedResponse.arrayBuffer();
    const hashBuffer = await crypto.subtle.digest('SHA-256', responseBuffer);
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    const hashHex = hashArray.map(b => b.toString(16).padStart(2, '0')).join('');

    if (hashHex === hash && responseBuffer.byteLength === length) {
        // If the hash and length match, cache the response and return it
        const cache = await caches.open('v1');
        // Use the original request for caching to ensure future matches
        cache.put(request, networkResponse.clone());
        return networkResponse;
    } else {
        // If the hash or length do not match, return an error response
        return new Response('Resource validation failed!', { status: 403 });
    }
}
