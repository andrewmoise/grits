if ('serviceWorker' in navigator) {
    // Check if a Service Worker is already active
    if (navigator.serviceWorker.controller) {
        // Service Worker is already active and controlling the page
        console.log('Service Worker is already active.');
    } else {
        // No Service Worker is controlling the page, proceed to register
        window.addEventListener('load', function() {
            navigator.serviceWorker.register('/grits-serviceworker.js').then(function(registration) {
                console.log('ServiceWorker registration successful with scope: ', registration.scope);
            }).catch(function(err) {
                console.log('ServiceWorker registration failed: ', err);
            });
        });
    }
}
