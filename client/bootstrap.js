window.AppConfig = {
    peers: ["http://localhost:1788/grits/v1/blob/"],
    cachePrefix: "http://localhost:1787/grits/v1/blob/"
};

if ('serviceWorker' in navigator) {
    // Check if a Service Worker is already active
    if (navigator.serviceWorker.controller) {
        // Service Worker is already active and controlling the page
        console.log('Service Worker is already active.');

        // You can still post messages to the active Service Worker if needed
        navigator.serviceWorker.controller.postMessage({
            type: 'CONFIG_UPDATE',
            data: window.AppConfig
        });
    } else {
        // No Service Worker is controlling the page, proceed to register
        window.addEventListener('load', function() {
            navigator.serviceWorker.register('/grits/v1/service-worker.js').then(function(registration) {
                console.log('ServiceWorker registration successful with scope: ', registration.scope);

                // Listen for the service worker to be ready, then post message
                navigator.serviceWorker.ready.then(function(registration) {
                    registration.active.postMessage({
                        type: 'CONFIG_UPDATE',
                        data: window.AppConfig
                    });
                });
            }).catch(function(err) {
                console.log('ServiceWorker registration failed: ', err);
            });
        });
    }
}
