import * as fs from 'fs';
import * as path from 'path';
import { Config } from './config';
import { ProxyManager, RootProxyManager } from './proxy';

// Create a directory, blowing it away if it already exists.
const dirPath = 'grits-test-run';
const subdirs = ['root', '1', '2', '3', '4', '5'];

if (fs.existsSync(dirPath)) {
    fs.rmSync(dirPath, { recursive: true, force: true });
}

fs.mkdirSync(dirPath);
subdirs.forEach(subdir => fs.mkdirSync(path.join(dirPath, subdir)));

// Create ProxyManagers with their unique configs
const proxyManagers: (ProxyManager | RootProxyManager)[] = [];

// Create config for root
const rootConfig = new Config();
rootConfig.thisPort = 1787;
rootConfig.isRootNode = true;
rootConfig.storageDirectory = path.join(dirPath, 'root');
proxyManagers.push(new RootProxyManager(rootConfig));

// Create configs for other nodes
for (let i = 0; i < 5; i++) {
    const config = new Config();
    config.thisPort = 1800 + i;
    config.rootHost = '127.0.0.1';
    config.rootPort = 1787;
    config.storageDirectory = path.join(dirPath, (i + 1).toString());
    proxyManagers.push(new ProxyManager(config));
}

// Start the proxy managers
proxyManagers.forEach(proxyManager => {
    proxyManager.start();
    console.log(`Starting event loop for proxy on port ${proxyManager.config.thisPort}...`);
});
