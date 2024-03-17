let AppConfig = {
    PathMappings: [] // This will be filled with the fetched configuration
};

console.log("We're loading");

self.addEventListener('message', (event) => {
    console.log('Received message in service worker:', event.data);
    if (event.data && event.data.type === 'DUMMY_EVENT') {
        console.log('Handling dummy event');
        // Handle your dummy event here
    }
});

// Use self.skipWaiting() to force the waiting service worker to become active
self.addEventListener('install', event => {
    console.log('Service Worker installing.');
    // Force this installing service worker to become the active service worker
    event.waitUntil(self.skipWaiting());
});

// Fetch and apply configuration upon service worker activation
self.addEventListener('activate', event => {
    console.log('Service Worker activating.');
    // Take immediate control of the page
    event.waitUntil(
        fetchConfig().then(config => {
            AppConfig = config;
            console.log('Service Worker configuration updated', AppConfig);
        }).catch(err => {
            console.error('Error during service worker activation or fetching config:', err);
        }).then(() => self.clients.claim())
    );
});

self.addEventListener('fetch', event => {
    const url = new URL(event.request.url);

    // Check if the fetch event is for a navigation request
    if (event.request.mode === 'navigate') {
        // Update configuration before continuing with the fetch
        event.respondWith(
            fetchConfig().then(config => {
                AppConfig = config; // Update the global AppConfig with the new configuration
                console.log('Service Worker configuration updated', AppConfig);
                return handleFetchRequest(event.request, AppConfig);
            }).catch(err => {
                console.error('Error fetching config:', err);
            })
        );
    } else {
        // Handle non-navigation fetch events as before
        event.respondWith(handleFetchRequest(event.request, AppConfig));
    }
});

async function handleFetchRequest(request, config) {
    const url = new URL(request.url);
    console.log("Checking for override of " + url.pathname)

    for (const mapping of config.PathMappings) {
        console.log("  " + mapping.urlPrefix);
        if (url.pathname.startsWith(mapping.urlPrefix)) {
            console.log('Match found:', mapping);
            return serveFromStorage(mapping.volume, mapping.path);
        }
    }

    // If no special handling is needed, proceed with a standard fetch
    return fetch(request);
}

async function serveFromStorage(volume, path) {
    try {
        const resourceUrl = `/grits/v1/content/${volume}/${path}`;
        console.log('Overriding fetch from ' + resourceUrl);
        
        const response = await fetch(resourceUrl);
        if (!response.ok) throw new Error('Resource fetch failed');
        return response;
    } catch (error) {
        return new Response('Resource not found or error fetching', { status: 404 });
    }
}

async function fetchConfig() {
    try {
        // Attempt to fetch the configuration from the specified URL
        const response = await fetch('/grits/v1/serviceworker/config');
        if (!response.ok) {
            // If the fetch fails, throw an error with the status
            throw new Error(`Configuration fetch failed with status: ${response.status}`);
        }
        // Decode the response as JSON
        const config = await response.json();
        // Update the global AppConfig with the fetched configuration
        AppConfig = config;
        // Log the fetched configuration to the console
        console.log('Fetched configuration:', AppConfig);
        // Return the fetched configuration for any further use
        return config;
    } catch (error) {
        // Log any errors to the console
        console.error('Failed to fetch configuration:', error);
        throw error; // Re-throw the error to handle it in the calling context
    }
}
