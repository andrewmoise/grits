import * as fs from 'fs';
import * as path from 'path';

import {
    MessageType,
    Message,
    RootHeartbeatMessage,
    ProxyHeartbeatMessage,
    DataRequestMessage,
    DataResponseOkMessage,
    DataResponseElsewhereMessage,
    DataResponseUnknownMessage,
    DataIsHereMessage,
} from './messages';

import { Config } from './config';
import { DownloadManager } from './download';
import { FileCache } from "./filecache";
import { CachedFile, PeerProxy, FileRetrievalError } from "./structures";
import { NetworkingManager } from './network';
import { BlobFinder } from './dht';
import { Logger } from './logger';

// ProxyDataMap class to track the most recent proxies where a file has been
// seen.
class ProxyDataMap {
    fileAddrToProxy: Map<
        string, { timestamp: Date, proxies: Array<PeerProxy> }>;
    config: Config;
    
    constructor(config: Config) {
        this.config = config;
        this.fileAddrToProxy = new Map();
    }

    // Adds a new fileAddr - proxy mapping.
    updateData(fileAddr: string, proxy: PeerProxy) {
        const proxyData = this.fileAddrToProxy.get(fileAddr);

        if (proxyData) {
            proxyData.timestamp = new Date();

            // Check if the proxy is already in the array.
            const existingProxy = proxyData.proxies.find(
                p => p.ip === proxy.ip && p.port === proxy.port);

            if (!existingProxy)
                proxyData.proxies.push(proxy);
        } else {
            this.fileAddrToProxy.set(
                fileAddr, { timestamp: new Date(), proxies: [proxy] });
        }
    }

    // Removes expired entries (i.e., entries older than 24 hours).
    cleanup() {
        const currentTime = new Date();
        const maxAge = this.config.maxProxyMapAge;

        this.fileAddrToProxy.forEach((value, key) => {
            if (Math.floor((currentTime.getTime() - value.timestamp.getTime())
                / 1000) >= maxAge) // maxAge is seconds
            {
                this.fileAddrToProxy.delete(key);
            }
        });
    }
}

abstract class ProxyManagerBase {
    config: Config;
    networkingManager: NetworkingManager;
    fileCache: FileCache;
    downloadManager: DownloadManager;
    logger: Logger;
    
    peerProxies: Map<string, PeerProxy>;
    rootProxy: PeerProxy;
    thisProxy: PeerProxy;
    
    proxyDataMap: ProxyDataMap;
    blobFinder: BlobFinder;
    cleanupIntervalId?: NodeJS.Timeout;
    dhtNotifyIntervalId?: NodeJS.Timeout;
    printStateIntervalId?: NodeJS.Timeout;
    
    constructor(config: Config) {
        this.config = config;
        this.networkingManager = new NetworkingManager(config);
        this.fileCache = new FileCache(config);
        this.downloadManager = new DownloadManager(this);
        this.logger = new Logger(config);

        this.peerProxies = new Map();
        this.rootProxy = this.addPeerProxy(
            this.config.rootHost, this.config.rootPort);

        const thisProxy = this.getPeerProxy(
            this.config.thisHost, this.config.thisPort);

        if (thisProxy)
            this.thisProxy = thisProxy;
        else
            this.thisProxy = this.addPeerProxy(
                this.config.thisHost, this.config.thisPort);
        
        this.proxyDataMap = new ProxyDataMap(config);

        this.blobFinder = new BlobFinder();
        this.blobFinder.updateProxies(this.peerProxies);
    }

    generateProxyKey(ip: string, port: number): string {
        return `${ip}:${port}`;
    }

    getPeerProxy(ip: string, port: number): PeerProxy | undefined {
        const key = this.generateProxyKey(ip, port);
        return this.peerProxies.get(key);
    }

    addPeerProxy(ip: string, port: number): PeerProxy {
        const key = this.generateProxyKey(ip, port);
        if (!this.peerProxies.has(key)) {
            const peerProxy = new PeerProxy(ip, port);
            this.peerProxies.set(key, peerProxy);
            return peerProxy;
        } else {
            throw new Error(
                `Proxy with address ${ip} and port ${port} already exists.`);
        }
    }

    removePeerProxy(ip: string, port: number) {
        const key = this.generateProxyKey(ip, port);
        if (this.peerProxies.has(key))
            this.peerProxies.delete(key);
    }

    async start(): Promise<void> {
        await this.logger.start();

        this.logger.log(new Date(), 'proxies',
                        `Init with ${this.peerProxies.size} proxies`);
        
        console.log("Set up proxy + start");

        this.networkingManager.start();

        this.networkingManager.registerRequestHandler(
            MessageType.DATA_REQUEST, this.handleDataRequest.bind(this));

        this.networkingManager.registerRequestHandler(
            MessageType.DATA_RESPONSE_OK,
            this.downloadManager.handleDataResponseOk.bind(
                this.downloadManager));

        this.networkingManager.registerRequestHandler(
            MessageType.DATA_RESPONSE_UNKNOWN,
            this.downloadManager.handleDataResponseUnknown.bind(
                this.downloadManager));

        this.networkingManager.registerRequestHandler(
            MessageType.DATA_IS_HERE, this.handleDataIsHereMessage.bind(this));
        
        this.cleanupIntervalId = setInterval(
            this.proxyDataMap.cleanup.bind(this.proxyDataMap),
            this.config.proxyMapCleanupPeriod);

        this.dhtNotifyIntervalId = setInterval(
            this.dhtNotifyTask.bind(this),
            this.config.dhtNotifyPeriod * 1000);

        this.printStateIntervalId = setInterval(
            this.printState.bind(this),
            15000);
    }

    stop(): void {
        clearInterval(this.printStateIntervalId);
        clearInterval(this.dhtNotifyIntervalId);
        clearInterval(this.cleanupIntervalId);

        this.networkingManager.unregisterRequestHandler(
            MessageType.DATA_IS_HERE);
        this.networkingManager.unregisterRequestHandler(
            MessageType.DATA_REQUEST);
        this.networkingManager.unregisterRequestHandler(
            MessageType.DATA_RESPONSE_OK);
        this.networkingManager.unregisterRequestHandler(
            MessageType.DATA_RESPONSE_UNKNOWN);

        this.networkingManager.stop();

        this.logger.stop();
    }

    async handleDataRequest(senderIp: string, senderPort: number,
                            message: Message): Promise<void>
    {
        if (!(message instanceof DataRequestMessage))
            throw new Error("Data request of wrong TS type!");
        const dataRequestMessage = message as DataRequestMessage;

        const fileAddr = dataRequestMessage.fileAddr;

        // Try to find in local storage.
        const file = await this.fileCache.readFile(fileAddr);
        if (file) {
            try {
                const data = await file.read(
                    dataRequestMessage.offset, dataRequestMessage.length);

                const responseMessage = new DataResponseOkMessage(
                    dataRequestMessage.burstId,
                    fileAddr, dataRequestMessage.offset,
                    dataRequestMessage.length, data);

                this.networkingManager.send(responseMessage, senderIp,
                                            senderPort);
            } finally {
                file.release();
            }

            return;
        }

        // Try to find in other nodes.
        const fileProxies = this.proxyDataMap.fileAddrToProxy.get(
            fileAddr);
        if (fileProxies) {
            const recentProxies = fileProxies.proxies.slice(
                -this.config.dhtMaxResponseNodes);
                
            const nodeInfo = recentProxies.map(
                proxy => ({ ip: proxy.ip, port: proxy.port }));
                
            const responseMessage = new DataResponseElsewhereMessage(
                dataRequestMessage.burstId,
                fileAddr, nodeInfo);

            this.networkingManager.send(
                responseMessage, senderIp, senderPort);
            return;
        }

        // Return failure, since we can't find it here or elsewhere.
        const responseMessage = new DataResponseUnknownMessage(
            dataRequestMessage.burstId, fileAddr);
        this.networkingManager.send(responseMessage, senderIp, senderPort);
        return;
    }
    
    async handleDataIsHereMessage(senderIp: string, senderPort: number,
                                  message: Message): Promise<void>
    {
        if (!(message instanceof DataIsHereMessage))
            throw new Error("Received wrong message type!");
        const dataIsHereMessage = message as DataIsHereMessage;

        const senderProxy = this.getPeerProxy(senderIp, senderPort);
        if (senderProxy) {
            this.proxyDataMap.updateData(dataIsHereMessage.fileAddr,
                                         senderProxy);
        }
    }
    
    handleWrongMessage(senderIp: string, senderPort: number, message: any) {
        throw new Error("Received wrong message! Code " + message + " from "
            + senderIp);
    }

    async dhtNotifyTask(): Promise<void> {
        //console.log(`DHT Notify for ${this.config.thisHost}:${this.config.thisPort}`);
        
        for (const cachedFile of this.fileCache.getFiles()) {
            //console.log(`  ${cachedFile.path}`);
            const closestNodes = await this.blobFinder.getClosestProxies(
                cachedFile.fileAddr, this.config.dhtNotifyNumber);
            
            for (const node of closestNodes) {
                //console.log(`    Notify ${node.ip}:${node.port}`);
                const message = new DataIsHereMessage(cachedFile.fileAddr);
                this.networkingManager.send(message, node.ip, node.port);
            }
        }
    }

    async retrieveFile(fileAddr: string): Promise<CachedFile> {
        console.log(`Retrieve file: ${fileAddr}`);

        // Try to find in local storage.
        const file = await this.fileCache.readFile(fileAddr);
        if (file)
            return file;

        console.log("  Not in storage.");
        
        // If the file isn't in the cache, attempt to download it
        const downloadedFile = await this.downloadManager.download(fileAddr);
        return downloadedFile;
    }

    async printState(): Promise<void> {
        console.log("---------- State of Proxy Manager " + this.config.thisHost
            + ":" + this.config.thisPort + " ----------");
        console.log("Number of peer proxies: " + this.peerProxies.size);

        let fileCount = 0;
        let pinnedCount = 0;
        for (const file of this.fileCache.getFiles()) {
            fileCount++;
            if (file.refCount !== 0)
                pinnedCount++;
        }

        console.log("Number of files in cache: " + fileCount.toString());
        console.log("  " + this.fileCache.currentSize + " / "
            + this.fileCache.maxSize + " bytes");
        console.log("  " + pinnedCount.toString() + " pinned or in use");
        
        console.log("Number of files known in network: "
            + this.proxyDataMap.fileAddrToProxy.size);
        console.log("------------------------------------------------");
    }
}

class ProxyManager extends ProxyManagerBase {
    sendProxyHeartbeatIntervalId?: NodeJS.Timeout;

    // added these to track the promise
    resolveFirstHeartbeatPromise: (() => void) | null = null;
    rejectFirstHeartbeatPromise: ((reason?: any) => void) | null = null;
    
    constructor(config: Config) {
        super(config);
    }

    async start() {
        await super.start();

        this.networkingManager.registerRequestHandler(
            MessageType.PROXY_HEARTBEAT, this.handleWrongMessage.bind(this));
        this.networkingManager.registerRequestHandler(
            MessageType.ROOT_HEARTBEAT, this.handleRootHeartbeat.bind(this));

        // Do an initial heartbeat to get things set up
        let retryCount = 0;
        const maxRetries = 3;

        while (retryCount < maxRetries) {
            await new Promise((resolve: ((value?: unknown) => void) | null,
                               reject: ((reason?: any) => void) | null) =>
                {
                    this.resolveFirstHeartbeatPromise = resolve;
                    this.rejectFirstHeartbeatPromise = reject;
                    this.sendProxyHeartbeat();
                    // set a timeout for the promise
                    setTimeout(() => {
                        if (this.rejectFirstHeartbeatPromise) {
                            this.rejectFirstHeartbeatPromise(
                                "Heartbeat timeout");
                        }
                    }, 500);
                }).catch((err) => {
                    this.logger.log(
                        new Date(), 'heartbeat',
                        `Error: ${err}`);

                    retryCount++;
                    if (retryCount === maxRetries) {
                        throw new Error(
                            "Failed to establish connection after "
                                + maxRetries + " attempts");
                    }
                });

            // break if the promise was resolved
            if (!this.rejectFirstHeartbeatPromise) {
                break;
            }
        }

        // And keep doing it periodically after that
        this.sendProxyHeartbeatIntervalId = setInterval(
            this.sendProxyHeartbeat.bind(this),
            this.config.proxyHeartbeatPeriod * 1000);
    }

    stop() {
        console.log("Close down proxy + end");

        // Clear the interval for sending proxy heartbeats
        clearInterval(this.sendProxyHeartbeatIntervalId!);

        this.networkingManager.unregisterRequestHandler(
            MessageType.PROXY_HEARTBEAT);
        this.networkingManager.unregisterRequestHandler(
            MessageType.ROOT_HEARTBEAT);

        super.stop();
    }

    async handleRootHeartbeat(senderIp: string, senderPort: number,
                              message: Message)
    {
        this.logger.log(
            new Date(), 'heartbeat',
            `Got root heartbeat, ${this.peerProxies.size} proxies`);

        const resolvePromise = this.resolveFirstHeartbeatPromise;
        if (resolvePromise) {
            this.resolveFirstHeartbeatPromise = null;
            this.rejectFirstHeartbeatPromise = null;
        }
        
        if (!(message instanceof RootHeartbeatMessage))
            throw new Error("Data request of wrong TS type!");
        const rootHeartbeatMessage = message as RootHeartbeatMessage;

        // Validate the sender
        if (senderIp !== this.rootProxy.ip
            || senderPort !== this.rootProxy.port)
        {
            console.warn(`Received root heartbeat from unknown source ${senderIp}:${senderPort}.`);
            return;
        }

        // Update the lastSeen time of the root proxy
        this.rootProxy.updateLastSeen();

        //console.log("Got root heartbeat.");

        const nodeMapFileAddr = rootHeartbeatMessage.nodeMapFileAddr;
        if (nodeMapFileAddr) {
            const nodeMapFile = await this.downloadManager.download(
                nodeMapFileAddr);
            //console.log(`Successful download: ${nodeMapFile.path}`);

            // Read the downloaded file and update local peerProxies
            const data = await fs.promises.readFile(nodeMapFile.path, 'utf-8');
            const newPeers = JSON.parse(data);

            // Create a temporary set for easy lookup
            const newPeersSet = new Set();
            newPeers.forEach((peer: { ip: string; port: number; }) => {
                newPeersSet.add(this.generateProxyKey(peer.ip, peer.port));
            });

            // Update existing and add new peers
            for (let newPeer of newPeers) {
                const { ip, port } = newPeer;
                if (!this.peerProxies.has(this.generateProxyKey(ip, port))) {
                    const peerProxy = new PeerProxy(ip, port);
                    this.peerProxies.set(this.generateProxyKey(ip, port),
                                         peerProxy);
                } else {
                    // update lastSeen timestamp for the existing peer
                    this.peerProxies.get(
                        this.generateProxyKey(ip, port))!.updateLastSeen();
                }
            }

            // Remove peers not present in the new list
            for (let [key, proxy] of this.peerProxies.entries()) {
                if (!newPeersSet.has(key)) {
                    this.peerProxies.delete(key);
                } else {
                    //console.log(
                    //    `  have peer: ${this.peerProxies.get(key)!.port}`);
                }
            }

            this.blobFinder.updateProxies(this.peerProxies);
            
            //console.log(`Updated local peerProxies.`);
        } else {
            this.logger.log(
                new Date(), 'heartbeat',
                'Root heartbeat, but no map.');
        }

        if (resolvePromise)
            resolvePromise();
    }

    sendProxyHeartbeat() {
        this.logger.log(new Date(), 'heartbeat', 'Sending proxy heartbeat');

        const heartbeatMessage = new ProxyHeartbeatMessage();
        this.networkingManager.send(heartbeatMessage,
            this.rootProxy.ip,
            this.rootProxy.port);
    }
}

class RootProxyManager extends ProxyManagerBase {
    heartbeatIntervalId?: NodeJS.Timeout;
    proxyMapFile: CachedFile | null = null;

    constructor(config: Config) {
        super(config);
    }

    async start() {
        await this.createProxyMapBuffer();
        
        await super.start();

        this.networkingManager.registerRequestHandler(
            MessageType.ROOT_HEARTBEAT, this.handleWrongMessage.bind(this));
        this.networkingManager.registerRequestHandler(
            MessageType.PROXY_HEARTBEAT, this.handleProxyHeartbeat.bind(this));

        this.heartbeatIntervalId = setInterval(
            this.updatePeerList.bind(this),
            this.config.rootUpdatePeerListPeriod * 1000);
    }

    stop() {
        clearInterval(this.heartbeatIntervalId!);

        this.networkingManager.unregisterRequestHandler(
            MessageType.PROXY_HEARTBEAT);
        this.networkingManager.unregisterRequestHandler(
            MessageType.ROOT_HEARTBEAT);

        super.stop();
    }

    handleProxyHeartbeat(senderIp: string, senderPort: number,
                         message: Message): void
    {
        this.logger.log(new Date(), 'heartbeat',
                        `Got proxy heartbeat from ${senderIp}:${senderPort}`);

        let peerProxy = this.getPeerProxy(senderIp, senderPort);
        if (!peerProxy) {
            peerProxy = this.addPeerProxy(senderIp, senderPort);
        }

        peerProxy.updateLastSeen();

        this.logger.log(new Date(), 'heartbeat',
                        `Send root heartbeat to ${senderIp}:${senderPort}`);

        const heartbeatMessage = new RootHeartbeatMessage(
            this.proxyMapFile ? this.proxyMapFile.fileAddr : null);
        this.networkingManager.send(heartbeatMessage, senderIp, senderPort);
    }

    async createProxyMapBuffer(): Promise<void> {
        //this.blobFinder.updateProxies(this.peerProxies);
        
        const peerList = Array.from(this.peerProxies.values()).map(peerProxy =>
        ({
            ip: peerProxy.ip,
            port: peerProxy.port,
        }));

        const randomSuffix = Math.floor(Math.random() * 1000000).toString()
        
        const filePath = path.join(this.config.tempDownloadDirectory,
                                   `proxies-${randomSuffix}`);
        await fs.promises.writeFile(filePath, JSON.stringify(peerList, null, 4),
                                    'utf-8');

        const file: CachedFile = await this.fileCache.addFile(
            filePath, null, true, true);
        if (file !== this.proxyMapFile) {
            if (this.proxyMapFile)
                this.proxyMapFile.release();
            this.proxyMapFile = file;
        } else {
            file.release();
        }
    }

    async updatePeerList(): Promise<void> {
        for (const [key, proxy] of this.peerProxies.entries())
            if (proxy.timeSinceSeen() > this.config.rootProxyDropTimeout)
                this.peerProxies.delete(key);

        this.blobFinder.updateProxies(this.peerProxies);
        await this.createProxyMapBuffer();
    }
}

export {
    ProxyManagerBase,
    ProxyManager,
    RootProxyManager,
};
