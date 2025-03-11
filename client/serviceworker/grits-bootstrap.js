(function() {
    if (!('serviceWorker' in navigator)) {
        console.warn('[Grits] Service Workers not supported');
        return null;
    }

    // Track navigation events to notify the service worker
    let lastPathname = window.location.pathname;
    
    // Function to send a message to the service worker
    function sendMessageToServiceWorker(message) {
        if (navigator.serviceWorker.controller) {
            navigator.serviceWorker.controller.postMessage(message);
        }
    }
    
    // Listen for popstate events (history navigation)
    window.addEventListener('popstate', () => {
        if (window.location.pathname !== lastPathname) {
            console.log('[Grits] Navigation detected via popstate');
            lastPathname = window.location.pathname;
            sendMessageToServiceWorker({ type: 'NAVIGATE' });
        }
    });
    
    // Intercept link clicks
    document.addEventListener('click', event => {
        // Check if it's a link click that will navigate
        const link = event.target.closest('a');
        if (link && link.href && 
            link.href.startsWith(window.location.origin) && 
            !link.target && 
            link.pathname !== lastPathname) {
            
            console.log('[Grits] Navigation detected via link click');
            lastPathname = link.pathname;
            // We'll fire the message after a slight delay to ensure the navigation happens
            setTimeout(() => {
                sendMessageToServiceWorker({ type: 'NAVIGATE' });
            }, 100);
        }
    });

    // Initialize service worker
    navigator.serviceWorker.getRegistration().then(async registration => {
        // Handle initial page load
        setTimeout(() => {
            sendMessageToServiceWorker({ type: 'NAVIGATE' });
        }, 500);

        if (registration && registration.active) {
            console.log('[Grits] Service worker already active');
            return;
        }

        try {
            const registration = await navigator.serviceWorker.register(
                '/grits-serviceworker.js'
            );
                    
            if (registration.installing) {
                registration.installing.addEventListener('statechange', function() {
                    if (this.state === 'activated') {
                        console.log('[Grits] Service worker activated');
                        sendMessageToServiceWorker({ type: 'NAVIGATE' });
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