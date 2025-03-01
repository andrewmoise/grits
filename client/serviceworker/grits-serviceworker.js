// grits-serviceworker.js
const swConfigHash = "{{SW_CONFIG_HASH}}";
const swDirHash = "{{SW_DIR_HASH}}";
let pathConfig = null;

self.addEventListener('install', event => {
    console.log('[Grits] New service worker installing');
    event.waitUntil(
        fetchConfig()
            .then(() => self.skipWaiting())
            .catch(error => {
                console.error('[Grits] Install failed during config fetch:', error);
                throw new Error('Failed to load grits SW configuration');
            })
    );
});

self.addEventListener('activate', event => {
    console.log('[Grits] New service worker activating and claiming clients');
    event.waitUntil(self.clients.claim());
});
  
self.addEventListener('fetch', event => {
    event.respondWith(
        fetch(event.request).then(async response => {
            // Check if config hash has changed
            const newDirHash = response.headers.get('X-Grits-Service-Worker-Hash');
            console.log(`[Grits] Intercepted fetch, hash is ${newDirHash}`);
            if (newDirHash && swDirHash !== newDirHash) {
                console.log('[Grits] Configuration has changed, updating');
                await fetchConfig();
                // Add error handling for both operations
                self.registration.update().catch(err => {
                    console.error('[Grits] Failed to update service worker:', err);
                });
            }
            
            return response;
        }).catch(error => {
            console.error('[Grits] Fetch error:', error);
            return fetch(event.request); // Fallback to network
        })
    );
});

// Fetch the configuration from blob address
async function fetchConfig() {
    try {
        const response = await fetch(`/grits-serviceworker-config.json?dirHash=${swDirHash}`);
        if (!response.ok) {
            throw new Error(`Config fetch failed: ${response.status}`);
        }
        pathConfig = await response.json();
        console.log('[Grits] Config initialized successfully');
    } catch (error) {
        console.error('[Grits] Error fetching config:', error);
        return null;
    }
}