// grits-serviceworker.js
const swConfigHash = "{{SW_CONFIG_HASH}}";
const swDirHash = "{{SW_DIR_HASH}}";
const swScriptHash = "{{SW_SCRIPT_HASH}}";
let pathConfig = null;

const debugServiceworker = false;

// Map of clients by volume
let gritsClients = new Map();

// Import GritsClient
importScripts('/grits/v1/content/client/GritsClient-sw.js');

// Initialize GritsClient for a specific volume
function initializeGritsClient(volume) {
    if (!GritsClient) {
        console.error('[Grits] Failed to import GritsClient');
        return null;
    }
    
    // We need to determine server URL in the service worker context
    const serverUrl = self.location.origin;
    
    // Creating new client instance for the specific volume
    if (debugServiceworker) {
        console.log(`[Grits] Initializing GritsClient for volume '${volume}' with server:`, serverUrl);
    }

    const client = new GritsClient({
        serverUrl: serverUrl,
        volume: volume
    });
    
    // Store in our map
    gritsClients.set(volume, client);
    
    return client;
}

// Initialize all required clients based on pathConfig
function initializeAllClients() {
    if (!pathConfig || !Array.isArray(pathConfig)) {
        console.error('[Grits] No valid path config available for client initialization');
        return false;
    }
    
    // Get unique set of volumes from path config
    const volumes = new Set();
    for (const mapping of pathConfig) {
        console.log(`[Grits] Path config: ${JSON.stringify(mapping, null, 2)}`);
        volumes.add(mapping.volume);
    }
    
    console.log(`[Grits] Initializing clients for ${volumes.size} volumes:`, Array.from(volumes));

    // Initialize a client for each volume
    volumes.forEach(volume => {
        initializeGritsClient(volume);
    });
    
    // Always ensure we have a client for the internal 'client' volume
    if (!gritsClients.has('client')) {
        initializeGritsClient('client');
    }
    
    return true;
}

// Destroy clients
function cleanupClients() {
    console.log('[Grits] Cleaning up Grits clients');
    for (const [volume, client] of gritsClients.entries()) {
      if (client && typeof client.destroy === 'function') {
        console.log(`[Grits] Destroying client for volume: ${volume}`);
        client.destroy();
      }
    }
  }

// Get the appropriate client for a volume
function getClientForVolume(volume) {
    // If we don't have a client for this volume yet, initialize one
    if (!gritsClients.has(volume)) {
        return initializeGritsClient(volume);
    }
    
    return gritsClients.get(volume);
}

// Normalize path to start with a slash and never end with one
function normalizePath(path) {
    // Ensure path starts with a slash
    if (!path.startsWith('/')) {
      path = '/' + path;
    }
    
    // Remove trailing slashes
    path = path.replace(/\/+$/g, '');

    // Remove double slashes
    path = path.replace(/\/+/g, '/');

    return path;
  }

  let isUpdatePending = false;

// Check if a request should be handled through the merkle tree
function shouldHandleWithGrits(url) {
    if (!pathConfig) {
        console.debug('[Grits] No path config available');
        return false;
    }
        
    // If we're in update mode, don't handle any requests
    if (isUpdatePending) {
        return false;
    }

    // Check if any client needs update
    for (const [volume, client] of gritsClients.entries()) {
        if (client.getServiceWorkerHash() && client.getServiceWorkerHash() !== swDirHash) {
            console.log(`[Grits] Service worker update needed, current: ${swDirHash}, new: ${client.getServiceWorkerHash()}`);
            
            isUpdatePending = true;

            // Trigger update but don't interfere with this request
            setTimeout(() => {
                cleanupClients();
                self.registration.update().catch(err => {
                    console.error('[Grits] Failed to update service worker:', err);
                });
            }, 0);
            
            // During update transition, don't handle any requests
            return false;
        }
    }
    
    // Convert URL to path and normalize it
    const parsedUrl = new URL(url);
    const path = normalizePath(parsedUrl.pathname);
    const hostname = parsedUrl.host;
    
    if (debugServiceworker) {
        console.debug(`[Grits] Checking if path should be handled: ${path} on host: ${hostname}`);
    }

    // Now check both hostname and path
    for (const mapping of pathConfig) {
        // Skip if hostname doesn't match (if hostname is specified in the mapping)
        if (mapping.hostName !== hostname) {
            continue;
        }
        
        if (path.startsWith(mapping.urlPrefix)) {
            // Calculate the relative path within the volume
            const relativePath = path.slice(mapping.urlPrefix.length);
            
            // Combine with the volume path
            const fullPath = `${mapping.path}${relativePath}`;
                
            if (debugServiceworker) {
                console.debug(`[Grits] Path match found: ${path} → ${mapping.volume}:${fullPath}`);
            }
            
            return {
                volume: mapping.volume,
                path: fullPath
            };
        }
    }
    
    if (debugServiceworker) {
        console.debug(`[Grits] No mapping found for: ${path} on host: ${hostname}`);
    }
    return false;
}

// Fetch a resource using the appropriate GritsClient
async function fetchFromGrits(mapping, request) {
    try {
        // Get the client for this volume
        const client = getClientForVolume(mapping.volume);
        
        if (!client) {
            throw new Error(`No client available for volume ${mapping.volume}`);
        }
        
        // Get the requested path from the URL
        
        if (debugServiceworker) {
            console.log(`[Grits] Mapped URL ${request.url} to volume path ${mapping.path} in volume ${mapping.volume}`);
        }

        let response = client.fetchFile(mapping.path);
        return response;
    } catch (error) {
        console.error(`[Grits] Error fetching from Grits: ${error.status || ""} ${error.message}`);
        return new Response(`Failed to fetch resource: ${error.status || ""} ${error.message}`, {
            status: error.status || 500,
            headers: { 'Content-Type': 'text/plain' }
        });
    }
}

self.addEventListener('install', event => {
    console.log('[Grits] New service worker installing');
    event.waitUntil(
        fetchConfig()
            .then(() => {
                console.log('[Grits] Config fetched, initializing clients');
                return initializeAllClients();
            })
            .then(() => {
                console.log('[Grits] Clients initialized, skipping waiting');
                return self.skipWaiting();
            })
            .catch(error => {
                console.error('[Grits] Install failed during config fetch:', error);
                // More detailed error reporting
                console.error('[Grits] Error details:', {
                    message: error.message,
                    stack: error.stack,
                    name: error.name
                });
                throw error; // Important to ensure the installation fails properly
            })
    );
});

self.addEventListener('activate', event => {
    console.log('[Grits] New service worker activating and claiming clients');
    // Make sure we have clients initialized
    if (pathConfig && gritsClients.size === 0) {
        initializeAllClients();
    }
    event.waitUntil(self.clients.claim());
});

// Navigation events should trigger a resetRoot to ensure fresh data
self.addEventListener('message', event => {
    if (event.data && event.data.type === 'NAVIGATE') {
        if (debugServiceworker) {
            console.log('[Grits] Navigation detected, resetting roots');
        }
        // Reset all clients
        gritsClients.forEach(client => {
            client.resetRoot();
        });
    }
});
  

self.addEventListener('fetch', event => {
    const url = event.request.url;
    if (debugServiceworker) {
        console.debug(`[Grits] Fetch event for: ${url}`);
    }

    // Only handle GET requests
    if (event.request.method !== 'GET') {
        if (debugServiceworker) {
            console.debug(`[Grits] Ignoring non-GET request: ${event.request.method}`);
        }
        return;
    }
    
    // Check if this is a navigation request
    if (event.request.mode === 'navigate') {
        if (debugServiceworker) {
            console.log('[Grits] Navigation request detected, will reset root on completion');
        }
        // After navigation completes, we'll want to reset the root
        // We can't do it immediately as the page isn't loaded yet
    }
    
    // Skip handling our own API requests to avoid loops
    if (url.includes('/grits/v1/') || url.includes('/grits-')) {
        if (debugServiceworker) {
            console.debug('[Grits] Ignoring Grits API request');
        }
        return;
    }

    // Check if we should handle this request with Grits
    const gritsMapping = shouldHandleWithGrits(url);
    
    if (gritsMapping) {
        if (debugServiceworker) {
            console.log(`[Grits] Intercepting request: ${url}`);
        }
        event.respondWith(fetchFromGrits(gritsMapping, event.request));

        return;
    }
});

// Fetch the configuration from blob address
async function fetchConfig() {
    try {
        const response = await fetch(`/grits/v1/content/client/serviceworker/grits-serviceworker-config.json`);
        if (!response.ok) {
            throw new Error(`Config fetch failed: ${response.status}`);
        }
        
        // Get the raw config
        const rawConfig = await response.json();
        
        // Apply normalization to each mapping
        pathConfig = rawConfig.map(mapping => ({
            hostName: mapping.hostName,
            urlPrefix: normalizePath(mapping.urlPrefix),
            volume: mapping.volume,
            path: normalizePath(mapping.path)
        }));
        
        console.log('[Grits] Config loaded successfully:', pathConfig);
        return pathConfig;
    } catch (error) {
        console.error('[Grits] Error fetching config:', error);
        throw error;
    }
}

self.addEventListener('unload', () => {
    console.log('[Grits] Service worker unloading, cleaning up clients');
    cleanupClients();
});