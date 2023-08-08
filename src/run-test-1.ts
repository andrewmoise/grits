import * as fs from 'fs';
import * as path from 'path';
import * as process from 'process';

import { Config } from './config';
import { ProxyManager, RootProxyManager } from './proxy';
import { HttpServer } from './web';
import { DegradedNetworkManager } from './degraded';
import { writeHeapSnapshot } from 'v8';

//console.log('Init heap monitor');

//setInterval(() => {
//    let file = Date.now() + '.heapsnapshot';
//    writeHeapSnapshot(file);
//    console.log('Heap dump written to', file);
//}, 15000);

console.log('Run test load');

console.log('  Test data init');

// Create a test data directory, DELETING IT RECURSIVELY if it already exists.
const dirPath = 'grits-test-run';
if (fs.existsSync(dirPath))
    fs.rmSync(dirPath, { recursive: true, force: true });
fs.mkdirSync(dirPath);
fs.mkdirSync(path.join(dirPath, 'root'));

// Load the list of image files
const imageDir = 'test-images';

console.log('  Create root proxy manager');

// Create ProxyManagers with their unique configs
const proxyManagers: (ProxyManager | RootProxyManager)[] = [];

// Create config for root
const rootConfig = new Config('127.0.0.1', 1787);
rootConfig.thisPort = 1787;
rootConfig.isRootNode = true;
rootConfig.storageDirectory = path.join(dirPath, 'root');
rootConfig.storageSize = 500 * 1024 * 1024;
rootConfig.logFile = path.join(rootConfig.storageDirectory, 'grits.log');

rootConfig.degradeDownstreamSpeed = 20000 + Math.random() * 80000;
rootConfig.degradeUpstreamSpeed = 20000 + Math.random() * 80000;
rootConfig.degradeLatency = 50 + Math.random() * 150;
rootConfig.degradePacketLoss = Math.max(0, Math.random() - .8);

console.log(`Root:`);
console.log(`  Bandwidth ${rootConfig.degradeDownstreamSpeed}/${rootConfig.degradeUpstreamSpeed}, latency ${rootConfig.degradeLatency}, packet loss ${rootConfig.degradePacketLoss}`);

const rootProxy = new RootProxyManager(rootConfig);

console.log('    Ready to read images dir');

const allImages = fs.readdirSync(imageDir);

for (let j = 0; j < 100; j++) {
    const randomImageIndex = Math.floor(Math.random() * allImages.length);
    const imageFilename = allImages[randomImageIndex];
    rootProxy.fileCache.addFile(path.join(imageDir, imageFilename), null, false, true);
}

proxyManagers.push(rootProxy);

console.log('  Create other proxy managers');

// Create configs for other nodes
for (let i = 1; i <= 5; i++) {
    const config = new Config('127.0.0.1', 1787);
    config.thisPort = 1800 + i;
    
    config.storageDirectory = path.join(dirPath, (i + 1).toString());
    if (!fs.existsSync(config.storageDirectory)) {
        fs.mkdirSync(config.storageDirectory);
    }

    config.logFile = path.join(config.storageDirectory, 'grits.log');
    
    config.degradeDownstreamSpeed = 20000 + Math.random() * 80000;
    config.degradeUpstreamSpeed = 20000 + Math.random() * 80000;
    config.degradeLatency = 50 + Math.random() * 150;
    config.degradePacketLoss = Math.max(0, Math.random() - .8);

    console.log(`Port ${config.thisPort}:`);
    console.log(`  Bandwidth ${config.degradeDownstreamSpeed}/${config.degradeUpstreamSpeed}, latency ${config.degradeLatency}, packet loss ${config.degradePacketLoss}`);

    const proxyManager = new ProxyManager(config);
    proxyManagers.push(proxyManager);

    console.log(`Adding proxy manager for ${proxyManager.thisProxy.port}, currently ${proxyManagers.length}`);
}

console.log('  Starting proxy managers');

(async () => {
    // Start the proxy managers
    await Promise.all(proxyManagers.map(proxyManager => {
        return proxyManager.start();
    }));

    for(let i=1; i<5; i++) {
        console.log(`Connecting port ${12340+i} to proxy manager on ${proxyManagers[i].thisProxy.port} config ${proxyManagers[i].config.thisPort}`);
        
        // Start the http service
        const httpServer = new HttpServer(proxyManagers[i], 12340 + i);
        httpServer.start();
    }
})();

console.log('  Signal handlers');

//process.on('SIGINT', async () => {
//    console.log('\nCaught interrupt signal, shutting down loggers...');
//    const stopPromises = proxyManagers.map(proxyManager => {
//        return proxyManager.logger.stop();
//    });
//    await Promise.all(stopPromises);
//
//    console.log('All loggers have shut down, exiting.');
//    process.exit();
//});

//process.on('SIGTERM', async () => {
//    console.log('\nCaught terminate signal, shutting down loggers...');
//    const stopPromises = proxyManagers.map(proxyManager => {
//        return proxyManager.logger.stop();
//    });
//    await Promise.all(stopPromises);
//
//    console.log('All loggers have shut down, exiting.');
//    process.exit();
//});

console.log('  Init done, event loop starts');
