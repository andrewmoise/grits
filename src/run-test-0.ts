import * as fs from 'fs';
import * as path from 'path';
import * as process from 'process';

import { Config } from './config';
import { ProxyManager, RootProxyManager } from './proxy';
import { HttpServer } from './web';

// Create a test data directory, DELETING IT RECURSIVELY if it already exists.
const dirPath = 'grits-test-run';
if (fs.existsSync(dirPath))
    fs.rmSync(dirPath, { recursive: true, force: true });
fs.mkdirSync(dirPath);
fs.mkdirSync(path.join(dirPath, 'root'));

// Load the list of image files
const imageDir = 'test-images';
const allImages = fs.readdirSync(imageDir);

// Create ProxyManagers with their unique configs
const proxyManagers: (ProxyManager | RootProxyManager)[] = [];

// Create config for root
const rootConfig = new Config('127.0.0.1', 1787);
rootConfig.thisPort = 1787;
rootConfig.isRootNode = true;
rootConfig.storageDirectory = path.join(dirPath, 'root');
rootConfig.logFile = path.join(rootConfig.storageDirectory, 'grits.log');
proxyManagers.push(new RootProxyManager(rootConfig));

// Create configs for other nodes
for (let i = 0; i < 5; i++) {
    const config = new Config('127.0.0.1', 1787);
    config.thisPort = 1800 + i;
    
    config.storageDirectory = path.join(dirPath, (i + 1).toString());
    if (!fs.existsSync(config.storageDirectory)) {
        fs.mkdirSync(config.storageDirectory);
    }

    config.logFile = path.join(config.storageDirectory, 'grits.log');
    
    const proxyManager = new ProxyManager(config);
    proxyManagers.push(proxyManager);

    // Choose 10 random images to add to this node's cache
    for (let j = 0; j < 10; j++) {
        const randomImageIndex = Math.floor(Math.random() * allImages.length);
        const imageFilename = allImages[randomImageIndex];
        proxyManager.fileCache.addFile(path.join(imageDir, imageFilename), null, false, true);
    }
}

(async () => {
    // Start the proxy managers
    await Promise.all(proxyManagers.map(proxyManager => {
        return proxyManager.start();
    }));

    // Start the http service
    const httpServer = new HttpServer(proxyManagers[1], 1234);
    httpServer.start();
})();

process.on('SIGINT', async () => {
    console.log('\nCaught interrupt signal, shutting down loggers...');
    const stopPromises = proxyManagers.map(proxyManager => {
        return proxyManager.logger.stop();
    });
    await Promise.all(stopPromises);

    console.log('All loggers have shut down, exiting.');
    process.exit();
});

process.on('SIGTERM', async () => {
    console.log('\nCaught terminate signal, shutting down loggers...');
    const stopPromises = proxyManagers.map(proxyManager => {
        return proxyManager.logger.stop();
    });
    await Promise.all(stopPromises);

    console.log('All loggers have shut down, exiting.');
    process.exit();
});
