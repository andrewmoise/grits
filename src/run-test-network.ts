import * as fs from 'fs';
import * as path from 'path';

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
const rootConfig = new Config();
rootConfig.thisPort = 1787;
rootConfig.isRootNode = true;
rootConfig.storageDirectory = path.join(dirPath, 'root');
proxyManagers.push(new RootProxyManager(rootConfig));

// Create configs for other nodes
for (let i = 0; i < 50; i++) {
    const config = new Config();
    config.thisPort = 1800 + i;
    config.rootHost = '127.0.0.1';
    config.rootPort = 1787;
    
    config.storageDirectory = path.join(dirPath, (i + 1).toString());
    if (!fs.existsSync(config.storageDirectory)) {
        fs.mkdirSync(config.storageDirectory);
    }

    const proxyManager = new ProxyManager(config);
    proxyManagers.push(proxyManager);

    // Choose 10 random images to add to this node's cache
    for (let j = 0; j < 10; j++) {
        const randomImageIndex = Math.floor(Math.random() * allImages.length);
        const imageFilename = allImages[randomImageIndex];
        proxyManager.fileCache.addFile(path.join(imageDir, imageFilename), null, false, true);
    }

    if (i == 0) {
        const httpServer = new HttpServer(proxyManager, 1234);
        httpServer.start();
    }   
}

// Start the proxy managers
proxyManagers.forEach(proxyManager => {
    proxyManager.start();
    console.log(`Starting event loop for proxy on port ${proxyManager.config.thisPort}...`);
});
