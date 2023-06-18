import { Config } from './config';

import { ProxyManager, RootProxyManager } from './proxy';

const config = new Config();

// Parse command-line arguments
process.argv.slice(2).forEach((arg, index, array) => {
    if (arg === '-p' && index < array.length - 1) {
        config.thisPort = parseInt(array[index + 1]);
    } else if (arg === '-r' && index < array.length - 1) {
        const [hostname, portStr] = array[index + 1].split(':');
        config.rootHost = hostname;
        config.rootPort = parseInt(portStr);
    } else if (arg === '-0') {
        config.isRootNode = true;
    }
});

// Create the proxy manager
const proxyManager = config.isRootNode
    ? new RootProxyManager(config)
    : config.rootHost === null
        ? (() => {
            throw new Error("Must specify -r for root proxy location");
        })()
        : new ProxyManager(config);

// Start

proxyManager.start();

console.log("Starting event loop...");
