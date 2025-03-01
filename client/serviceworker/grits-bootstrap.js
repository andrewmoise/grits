// grits-bootstrap.js
(function() {
    // Configuration values that will be replaced by the server
    const SW_DIR_HASH = "{{SW_DIR_HASH}}";
    const SW_SCRIPT_HASH = "{{SW_SCRIPT_HASH}}";
    const SW_CONFIG_HASH = "{{SW_CONFIG_HASH}}";
    
    // Track the current hash to detect changes
    let currentSwHash = SW_DIR_HASH;
    
    function registerServiceWorker() {
        if (!('serviceWorker' in navigator)) {
            console.error('[Grits] Service Workers not supported in this browser');
            return Promise.reject('Service Workers not supported');
        }
        
        // Register with the hash in filename for cache busting
        return navigator.serviceWorker.register(`/grits-serviceworker.js?hash=${SW_SCRIPT_HASH}`)
            .then(function(registration) {
                console.log('[Grits] ServiceWorker registration successful');
                
                // Send configuration to the service worker
                function sendConfig(worker) {
                    worker.postMessage({
                        type: 'INIT_CONFIG',
                        swDirHash: hash,
                        swConfigAddr: SW_CONFIG_HASH
                    });
                }
                
                // Wait for the service worker to be ready
                if (registration.installing) {
                    registration.installing.addEventListener('statechange', function() {
                        if (this.state === 'activated') {
                            sendConfig(registration.active);
                        }
                    });
                } else if (registration.active) {
                    sendConfig(registration.active);
                }
                
                return registration;
            })
            .catch(function(err) {
                console.error('[Grits] ServiceWorker registration failed:', err);
                throw err;
            });
    }
    
    // Function to update service worker when hash changes
    function updateServiceWorker(newHash) {
        if (newHash === currentSwHash) {
            return Promise.resolve('Already at latest version');
        }
        
        console.log(`[Grits] Updating service worker from ${currentSwHash} to ${newHash}`);
        
        // Unregister existing service worker
        return navigator.serviceWorker.getRegistration()
            .then(registration => {
                if (registration) {
                    return registration.unregister();
                }
                return Promise.resolve();
            })
            .then(() => {
                // Register the new one with new hash
                currentSwHash = newHash;
                return registerServiceWorker(newHash);
            });
    }
    
    // Set up message listener right away
    if ('serviceWorker' in navigator) {
        navigator.serviceWorker.addEventListener('message', event => {
            if (event.data && event.data.type === 'UPDATE_SERVICE_WORKER') {
                updateServiceWorker(event.data.newHash);
            }
        });
    }
    
    // Initialize on page load
    window.addEventListener('load', function() {
        registerServiceWorker(currentSwHash);
    });
    
    // Expose the update function globally for manual updates
    window.gritsUpdateServiceWorker = function(newHash) {
        return updateServiceWorker(newHash);
    };

})();