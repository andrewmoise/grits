import * as fs from 'fs';
import * as path from 'path';
import * as process from 'process';

import { Config } from './config';
import { ProxyManager, RootProxyManager } from './proxy';
import { HttpServer } from './web';
import { DegradedNetworkManager } from './degraded';

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
rootConfig.logFile = path.join(rootConfig.storageDirectory, 'grits.log');

const rootProxy = new RootProxyManager(rootConfig);

console.log('    Ready to read images dir');

fs.readdirSync(imageDir).forEach(file => {
    const absolutePath = path.join(imageDir, file);
    if (fs.statSync(absolutePath).isFile())
        rootProxy.fileCache.addFile(absolutePath, null,
                                    /* moveFile= */ false,
                                    /* takeReference= */ true,
                                    /* addInPlace= */ true,
                                    /* addSize= */ false);
});

rootProxy.networkManager = new DegradedNetworkManager(
    rootProxy.networkManager, 50000, 10000, 50000, 10000, 25, 0);

proxyManagers.push(rootProxy);

console.log('  Create other proxy managers');

// Create configs for other nodes
for (let i = 0; i < 50; i++) {
    const config = new Config('127.0.0.1', 1787);
    config.thisPort = 1800 + i;
    
    config.storageDirectory = path.join(dirPath, (i + 1).toString());
    if (!fs.existsSync(config.storageDirectory)) {
        fs.mkdirSync(config.storageDirectory);
    }

    config.logFile = path.join(config.storageDirectory, 'grits.log');
    
    const proxyManager = new ProxyManager(config);
    proxyManagers.push(proxyManager);
}

console.log('  Starting proxy managers');

(async () => {
    // Start the proxy managers
    await Promise.all(proxyManagers.map(proxyManager => {
        return proxyManager.start();
    }));

    // Start the http service
    const httpServer = new HttpServer(proxyManagers[1], 1234);
    httpServer.start();
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

console.log('  Set request timers');

setTimeout(() => {
    let loopCount = 0;

    const allFiles = Array.from(rootProxy.fileCache.getFiles()).map(
        (file) => { return file.fileAddr; });

    console.log(`Timeout starts! ${allFiles.length} files`);
    
    const intervalId = setInterval(async () => {
        if (loopCount++ >= 20) {
            clearInterval(intervalId);
            return;
        }

        // Pick a random proxy from the network
        const randomProxyIndex = Math.floor(Math.random() * proxyManagers.length);
        const randomProxy = proxyManagers[randomProxyIndex];

        // Pick a random file from the root proxy's fileCache
        const randomFileIndex = Math.floor(Math.random() * allFiles.length);
        const randomFileAddr = allFiles[randomFileIndex];

        console.log(`  Request ${randomFileAddr}`);
        
        // Request the file and measure the time it takes
        try {
            const startTime = performance.now();
            const file = await randomProxy.retrieveFile(randomFileAddr);
            const endTime = performance.now();
            console.log(`    File size: ${file.size}, Fetch time: ${endTime - startTime}ms`);
        } catch (err) {
            console.log(`Error retrieving file: ${err}`);
        }
    }, 15000);
}, 45000);  // Wait 30 seconds before starting

console.log('  Init done, event loop starts');
