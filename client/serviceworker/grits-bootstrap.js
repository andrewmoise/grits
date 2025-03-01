(function() {
    if (!('serviceWorker' in navigator)) {
        console.warn('[Grits] Service Workers not supported');
        return null;
    }

    navigator.serviceWorker.getRegistration().then(async registration => {
        if (registration && registration.active) {
            console.log('[Grits] Service worker already active, skipping initialization');
            return;
        }

        try {

            let isUpdating = false;

            const SW_DIR_HASH = "{{SW_DIR_HASH}}";
            const SW_SCRIPT_HASH = "{{SW_SCRIPT_HASH}}";
            const SW_CONFIG_HASH = "{{SW_CONFIG_HASH}}";
            
            // Register with new hash
            const registration = await navigator.serviceWorker.register(
                `/grits-serviceworker.js?hash=${SW_SCRIPT_HASH}`
            );
            
            navigator.serviceWorker.addEventListener('message', async event => {
                if (event.data?.type === 'UPDATE_SERVICE_WORKER' && !isUpdating) {
                    console.log(`[Grits] Updating service worker`);
                    isUpdating = true;
                    
                    try {
                        // Fetch the latest bootstrap code with new hash values
                        const response = await fetch('/grits-bootstrap.js', { 
                            cache: 'no-store' 
                        });
                        const newBootstrapCode = await response.text();

                        // Unregister out of date service worker
                        const registration = await navigator.serviceWorker.getRegistration();
                        if (registration) {
                            const unregistered = await registration.unregister();
                            console.log('[Grits] Unregistered old service worker:', unregistered);
                        }

                        // Execute the new bootstrap code to redefine initGritsServiceWorker
                        // with new hash values
                        eval(newBootstrapCode);
                    } catch (err) {
                        console.error('[Grits] Service worker update failed:', err);
                        return null;
                    }
            
                }    
            });

            // Send configuration when appropriate
            const sendConfig = (worker) => {
                worker.postMessage({
                    type: 'INIT_CONFIG',
                    swDirHash: SW_DIR_HASH,
                    swConfigHash: SW_CONFIG_HASH
                });
            };
                    
            if (registration.installing) {
                registration.installing.addEventListener('statechange', function() {
                    if (this.state === 'activated') {
                        sendConfig(registration.active);
                    } else if (this.state === 'redundant') {
                        console.warn('[Grits] Service worker installation failed');
                    }
                });
            } else if (registration.active) {
                sendConfig(registration.active);
            }
        } catch (err) {
            console.error('[Grits] Service worker installation failed:', err);
            return null;
        }
    });
})();