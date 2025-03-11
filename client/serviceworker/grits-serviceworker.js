// grits-serviceworker.js
const swConfigHash = "{{SW_CONFIG_HASH}}";
const swDirHash = "{{SW_DIR_HASH}}";
const swScriptHash = "{{SW_SCRIPT_HASH}}";
let pathConfig = null;

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
    console.log(`[Grits] Initializing GritsClient for volume '${volume}' with server:`, serverUrl);
    
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

// Check if a request should be handled through the merkle tree
function shouldHandleWithGrits(url) {
    if (!pathConfig) {
        console.debug('[Grits] No path config available');
        return false;
    }
    
    // Convert URL to path and normalize it
    const parsedUrl = new URL(url);
    const path = normalizePath(parsedUrl.pathname);
    
    console.debug(`[Grits] Checking if path should be handled: ${path}`);
    
    // Now both the path and mapping.urlPrefix are normalized
    for (const mapping of pathConfig) {
        if (path.startsWith(mapping.urlPrefix)) {
            // Calculate the relative path within the volume
            const relativePath = path.slice(mapping.urlPrefix.length);
            
            // Combine with the volume path
            const fullPath = `${mapping.path}${relativePath}`;
                
            console.debug(`[Grits] Path match found: ${path} → ${mapping.volume}:${fullPath}`);
            
            return {
                volume: mapping.volume,
                path: fullPath
            };
        }
    }
    
    console.debug(`[Grits] No mapping found for: ${path}`);
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
        
        console.log(`[Grits] Mapped URL ${request.url} to volume path ${mapping.path} in volume ${mapping.volume}`);

        // Look up metadata to determine content type
        let metadata = await client.lookupPath(mapping.path);
        console.debug(`[Grits] Metadata for ${mapping.path}:`, metadata);
        
        let resolvedPath = mapping.path;

        if (metadata.type !== 'blob') {
            // Maybe an index? FIXME, make better + add other options
            resolvedPath += "/index.html";
            metadata = await client.lookupPath(resolvedPath);
        }

        if (metadata.type !== 'blob') {
            console.warn(`[Grits] Requested resource is not a blob: ${resolvedPath} (type: ${metadata.type})`);
            return new Response(`Resource is not a file: ${resolvedPath}`, {
                status: 404,
                headers: { 'Content-Type': 'text/plain' }
            });
        }
        
        // Fetch the actual content
        const blob = await client.fetchFile(resolvedPath);
        console.debug(`[Grits] Successfully fetched blob for ${resolvedPath}, size: ${blob.size} bytes`);
        
        // Determine content type based on file extension or metadata
        let contentType = 'application/octet-stream'; // Default
        
        // Try to infer from path extension (FIXME)
        const fileExtension = resolvedPath.split('.').pop().toLowerCase();
        if (fileExtension) {
            const mimeTypes = {
                'html': 'text/html',
                'css': 'text/css',
                'js': 'application/javascript',
                'json': 'application/json',
                'png': 'image/png',
                'jpg': 'image/jpeg',
                'jpeg': 'image/jpeg',
                'gif': 'image/gif',
                'svg': 'image/svg+xml',
                'txt': 'text/plain'
            };
            
            if (mimeTypes[fileExtension]) {
                contentType = mimeTypes[fileExtension];
            }
        }
        
        // Return the blob with appropriate headers
        return new Response(blob, {
            status: 200,
            headers: { 'Content-Type': contentType }
        });
    } catch (error) {
        console.error(`[Grits] Error fetching from Grits: ${error.message}`);
        return new Response(`Failed to fetch resource: ${error.message}`, {
            status: 500,
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
                initializeAllClients();
                return self.skipWaiting();
            })
            .catch(error => {
                console.error('[Grits] Install failed during config fetch:', error);
                throw new Error('Failed to load grits SW configuration');
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
        console.log('[Grits] Navigation detected, resetting roots');
        // Reset all clients
        gritsClients.forEach(client => {
            client.resetRoot();
        });
    }
});
  
self.addEventListener('fetch', event => {
    const url = event.request.url;
    console.debug(`[Grits] Fetch event for: ${url}`);
    
    // Only handle GET requests
    if (event.request.method !== 'GET') {
        console.debug(`[Grits] Ignoring non-GET request: ${event.request.method}`);
        return;
    }
    
    // Check if this is a navigation request
    if (event.request.mode === 'navigate') {
        console.log('[Grits] Navigation request detected, will reset root on completion');
        // After navigation completes, we'll want to reset the root
        // We can't do it immediately as the page isn't loaded yet
    }
    
    // Skip handling our own API requests to avoid loops
    if (url.includes('/grits/v1/') || url.includes('/grits-')) {
        console.debug('[Grits] Ignoring Grits API request');
        return;
    }

    // Check if we should handle this request with Grits
    const gritsMapping = shouldHandleWithGrits(url);
    
    if (gritsMapping) {
        console.log(`[Grits] Intercepting request: ${url}`);
        event.respondWith(fetchFromGrits(gritsMapping, event.request));
        return;
    }
    
    // For all other requests, proceed with normal fetch but check for hash changes
    event.respondWith(
        fetch(event.request).then(async response => {
            // Check if config hash has changed
            const newDirHash = response.headers.get('X-Grits-Service-Worker-Hash');
            if (newDirHash && swDirHash !== newDirHash) {
                console.log(`[Grits] Configuration has changed (old: ${swDirHash}, new: ${newDirHash}), updating`);
                cleanupClients();
                await fetchConfig();
                // Reset all clients
                gritsClients.forEach(client => {
                    client.resetRoot();
                });
                // Update the service worker
                self.registration.update().catch(err => {
                    console.error('[Grits] Failed to update service worker:', err);
                });
            }
            
            return response;
        }).catch(error => {
            console.error(`[Grits] Fetch error for ${url}:`, error);
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
        
        // Get the raw config
        const rawConfig = await response.json();
        
        // Apply normalization to each mapping
        pathConfig = rawConfig.map(mapping => ({
            volume: mapping.volume,
            urlPrefix: normalizePath(mapping.urlPrefix),
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