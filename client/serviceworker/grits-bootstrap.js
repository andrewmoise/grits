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
            const registration = await navigator.serviceWorker.register(
                '/grits-serviceworker.js'
            );
                    
            if (registration.installing) {
                registration.installing.addEventListener('statechange', function() {
                    if (this.state === 'activated') {
                        sendConfig(registration.active);
                    } else if (this.state === 'redundant') {
                        console.warn('[Grits] Service worker installation failed');
                    }
                });
            }
        } catch (err) {
            console.error('[Grits] Service worker installation failed:', err);
            return null;
        }
    });
})();