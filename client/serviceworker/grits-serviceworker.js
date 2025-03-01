// grits-serviceworker.js
let appConfig = null;
let currentSwDirHash = null;
let swScriptAddr = null;
let swConfigAddr = null;

// Wait for the init config message before doing anything
self.addEventListener('message', event => {
    if (event.data && event.data.type === 'INIT_CONFIG') {
        currentSwDirHash = event.data.swDirHash;
        swConfigHash = event.data.swConfigHash;
        
        // Now fetch the actual config
        fetchConfig().then(config => {
            appConfig = config;
            console.log('[Grits] Service worker initialized with config');
        });
    }
});

// Fetch the configuration from blob address
async function fetchConfig() {
    try {
        const response = await fetch(`/grits-serviceworker-config.json?hash=${swConfigHash}`);
        if (!response.ok) {
            throw new Error(`Config fetch failed: ${response.status}`);
        }
        return await response.json();
    } catch (error) {
        console.error('[Grits] Error fetching config:', error);
        return null;
    }
}

// Check all responses for the service worker hash header
self.addEventListener('fetch', event => {
    event.respondWith(
        fetch(event.request).then(response => {
            // Check if the hash has changed
            const newHash = response.headers.get('X-Grits-Service-Worker-Hash');
            if (newHash && currentSwDirHash && newHash !== currentSwDirHash) {
                console.log('[Grits] Service worker config changed, notifying clients');
                
                // Notify all controlled clients about the update
                self.clients.matchAll().then(clients => {
                    clients.forEach(client => {
                        client.postMessage({
                            type: 'UPDATE_SERVICE_WORKER',
                            newHash: newHash
                        });
                    });
                });
            }
            
            return response;
        })
    );
});