import * as fs from 'fs';
import * as path from 'path';

import {
    MessageType,
    Message,
    HeartbeatResponse,
    HeartbeatMessage,
    DataRequestMessage,
    DataResponseOk,
    DataResponseElsewhere,
    DataResponseUnknown,
    DhtLocationMessage,
    DhtLocationResponse,
} from './messages';

import { Config } from './config';
import { DownloadManager } from './download';
import { FileCache } from "./filecache";

import {
    CachedFile, PeerProxy, FileRetrievalError, DOWNLOAD_CHUNK_SIZE
} from "./structures";

import { NetworkManager, UdpNetworkManager } from './network';
import { BlobFinder } from './dht';
import { Logger } from './logger';
import { UpstreamManager } from './traffic';

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
    networkManager: NetworkManager;
    fileCache: FileCache;
    downloadManager: DownloadManager;
    upstreamManager: UpstreamManager;
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
        this.logger = new Logger(config);

        this.networkManager = new UdpNetworkManager(this.logger, config);
        this.fileCache = new FileCache(config);
        this.downloadManager = new DownloadManager(this);
        this.upstreamManager = new UpstreamManager(config);

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
        
        //console.log("Set up proxy + start");

        this.networkManager.start();

        this.networkManager.registerRequestHandler(
            MessageType.DATA_REQUEST_MESSAGE, this.handleDataRequest.bind(this));

        this.networkManager.registerRequestHandler(
            MessageType.DATA_RESPONSE_OK,
            this.downloadManager.handleDataResponseOk.bind(
                this.downloadManager));

        this.networkManager.registerRequestHandler(
            MessageType.DATA_RESPONSE_ELSEWHERE,
            this.downloadManager.handleDataResponseElsewhere.bind(
                this.downloadManager));

        this.networkManager.registerRequestHandler(
            MessageType.DATA_RESPONSE_UNKNOWN,
            this.downloadManager.handleDataResponseUnknown.bind(
                this.downloadManager));

        this.networkManager.registerRequestHandler(
            MessageType.DHT_LOCATION_MESSAGE,
            this.handleDhtLocationMessage.bind(this));

        this.networkManager.registerRequestHandler(
            MessageType.DHT_LOCATION_RESPONSE,
            this.handleDhtResponse.bind(this));

        
        this.cleanupIntervalId = setInterval(
            this.proxyDataMap.cleanup.bind(this.proxyDataMap),
            this.config.proxyMapCleanupPeriod);

        this.dhtNotifyIntervalId = setInterval(
            this.dhtNotifyAll.bind(this),
            this.config.dhtNotifyPeriod * 1000);

        //this.printStateIntervalId = setInterval(
        //    this.printState.bind(this),
        //    15000);
    }

    stop(): void {
        //clearInterval(this.printStateIntervalId);
        clearInterval(this.dhtNotifyIntervalId);
        clearInterval(this.cleanupIntervalId);

        this.networkManager.unregisterRequestHandler(
            MessageType.DHT_LOCATION_RESPONSE);
        this.networkManager.unregisterRequestHandler(
            MessageType.DHT_LOCATION_MESSAGE);
        this.networkManager.unregisterRequestHandler(
            MessageType.DATA_REQUEST_MESSAGE);
        this.networkManager.unregisterRequestHandler(
            MessageType.DATA_RESPONSE_OK);
        this.networkManager.unregisterRequestHandler(
            MessageType.DATA_RESPONSE_UNKNOWN);

        this.networkManager.stop();

        this.logger.stop();
    }

    async handleDataRequest(senderIp: string, senderPort: number,
                            message: Message): Promise<void>
    {
        if (!(message instanceof DataRequestMessage))
            throw new Error("Data request of wrong TS type!");
        const dataRequestMessage = message as DataRequestMessage;

        const fileAddr = dataRequestMessage.fileAddr;

        if (!message.transferId.match("^[A-Za-z0-9]{8}$"))
            throw new Error(`Malformed transfer ID! ${message.transferId}`);
        
        this.logger.log(new Date(), message.transferId,
                        `Data request for ${fileAddr}`);
        
        // Try to find in local storage.
        const file = await this.fileCache.readFile(fileAddr);
        if (file) {
            try {
                let offset = dataRequestMessage.offset;
                let length = dataRequestMessage.length;
                let budget = 0;
                
                while (length > 0) {
                    this.logger.log(new Date(), message.transferId,
                                    `Request budget ${length-budget}`);
                    budget += await this.upstreamManager.requestUpload(
                        length - budget);
                    
                    while(budget >= Math.min(length, DOWNLOAD_CHUNK_SIZE)
                        && length > 0)
                    {
                        const packetLen = Math.min(length, DOWNLOAD_CHUNK_SIZE);

                        const data = await file.read(offset, packetLen);
                        
                        this.logger.log(new Date(), message.transferId,
                                        `Send data response ${packetLen}`);

                        const responseMessage = new DataResponseOk(
                            dataRequestMessage.burstId,
                            fileAddr, offset,
                            packetLen, data);
                        
                        this.networkManager.send(responseMessage, senderIp,
                                                    senderPort);
                        
                        offset += packetLen;
                        budget -= packetLen;
                        length -= packetLen;
                    }
                }
            } finally {
                file.release();
            }

            return;
        }

        // Try to find in other nodes.
        const fileProxies = this.proxyDataMap.fileAddrToProxy.get(
            fileAddr);
        if (fileProxies) {
            this.logger.log(new Date(), message.transferId,
                            `Send elsewhere, ${fileProxies.proxies.length} hosts`);

            const recentProxies = fileProxies.proxies.slice(
                -this.config.dhtMaxResponseNodes);
                
            const nodeInfo = recentProxies.map(
                proxy => ({ ip: proxy.ip, port: proxy.port }));
                
            const responseMessage = new DataResponseElsewhere(
                dataRequestMessage.burstId,
                fileAddr, nodeInfo);

            this.networkManager.send(
                responseMessage, senderIp, senderPort);
            return;
        }

        // Return failure, since we can't find it here or elsewhere.
        this.logger.log(new Date(), message.transferId,
                        `fileAddr is unknown`);

        const responseMessage = new DataResponseUnknown(
            dataRequestMessage.burstId, fileAddr);
        this.networkManager.send(responseMessage, senderIp, senderPort);
        return;
    }
    
    async handleDhtLocationMessage(senderIp: string, senderPort: number,
                                   rawMessage: Message)
    : Promise<void> {
        if (!(rawMessage instanceof DhtLocationMessage))
            throw new Error("Received wrong message type!");
        const message = rawMessage as DhtLocationMessage;

        this.logger.log(new Date(), 'dht', `Got location ${message.fileAddr} at ${senderIp}:${senderPort}`);
        
        const senderProxy = this.getPeerProxy(senderIp, senderPort);
        if (senderProxy) {
            this.proxyDataMap.updateData(message.fileAddr,
                                         senderProxy);
        } else {
            this.logger.log(new Date(), 'dht', '  But no proxy found!');
        }

        this.upstreamManager.requestUpload(40); // FIXME
        this.logger.log(new Date(), 'dht', `Sending location response for ${message.fileAddr} to ${senderIp}:${senderPort}`);
        
        const response = new DhtLocationResponse(message.fileAddr);
        this.networkManager.send(response, senderIp, senderPort);
    }
    
    async handleWrongMessage(senderIp: string, senderPort: number, message: any)
    : Promise<void> {
        throw new Error("Received wrong message! Code " + message + " from "
            + senderIp);
    }

    async dhtNotify(cachedFile: CachedFile): Promise<void> {
        this.logger.log(new Date(), 'dht', `DHT Notify ${cachedFile.fileAddr}`);

        const closestNodes = await this.blobFinder.getClosestProxies(
            cachedFile.fileAddr, this.config.dhtNotifyNumber);
        const refreshAge = this.config.dhtRefreshTime;
        
        for (const node of closestNodes) {
            // Don't process if we have a recent refresh timestamp
            const lastRefresh: Date|undefined =
                node.dhtStoredData.get(cachedFile);
            
            if (lastRefresh) {
                if (new Date().getTime() - lastRefresh.getTime() < refreshAge)
                    continue;
            }

            // We have no refresh timestamp, or an old one - send notification

            // FIXME - maybe this should be in the network manager:
            await this.upstreamManager.requestUpload(40);

            this.logger.log(
                new Date(), 'dht', `DHT Refresh ${cachedFile.fileAddr} on ${node.ip}:${node.port}`);
            
            const message = new DhtLocationMessage(cachedFile.fileAddr);
            this.networkManager.send(message, node.ip, node.port);
        }
    }

    notifyAllRunning: boolean = false;
    
    async dhtNotifyAll(): Promise<void> {
        this.logger.log(new Date(), 'dht', 'DHT notify loop');

        if (this.notifyAllRunning) {
            this.logger.log(new Date(), 'dht', '  early return');
            return;
        }
        
        this.notifyAllRunning = true;
        for (const cachedFile of this.fileCache.getFiles())
            await this.dhtNotify(cachedFile);
        this.notifyAllRunning = false;
    }

    async handleDhtResponse(senderIp: string, senderPort: number,
                            rawMessage: Message): Promise<void>
    {
        if (!(rawMessage instanceof DhtLocationResponse))
            throw new Error("Data request of wrong TS type!");
        const message = rawMessage as DhtLocationResponse;

        this.logger.log(new Date(), 'dht', `DHT ack from ${senderIp}:${senderPort} for ${message.fileAddr}`);
        
        const node = this.getPeerProxy(senderIp, senderPort);
        if (!node) {
            this.logger.log(new Date(), 'dht', '  No peer found');
            return;
        }

        const cachedFile = await this.fileCache.readFile(message.fileAddr);
        if (!cachedFile) {
            this.logger.log(new Date(), 'dht', '  No file found');
            return;
        }

        node.dhtStoredData.set(cachedFile, new Date());
        cachedFile.release(); // FIXME - needs new API
    }
    
    async retrieveFile(fileAddr: string): Promise<CachedFile> {
        this.logger.log(new Date(), 'proxy', `Retrieve file: ${fileAddr}`);

        // Try to find in local storage.
        const file = await this.fileCache.readFile(fileAddr);
        if (file)
            return file;

        this.logger.log(new Date(), 'proxy', "  Not in storage.");
        
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

        this.networkManager.registerRequestHandler(
            MessageType.HEARTBEAT_MESSAGE, this.handleWrongMessage.bind(this));
        this.networkManager.registerRequestHandler(
            MessageType.HEARTBEAT_RESPONSE, this.handleRootHeartbeat.bind(this));

        // Do an initial heartbeat to get things set up
        //let retryCount = 0;
        //const maxRetries = 10;

        while (true) {//retryCount < maxRetries) {
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

                    //retryCount++;
                    //if (retryCount === maxRetries) {
                    //    throw new Error(
                    //        "Failed to establish connection after "
                    //            + maxRetries + " attempts");
                    //}
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
        //console.log("Close down proxy + end");

        // Clear the interval for sending proxy heartbeats
        clearInterval(this.sendProxyHeartbeatIntervalId!);

        this.networkManager.unregisterRequestHandler(
            MessageType.HEARTBEAT_MESSAGE);
        this.networkManager.unregisterRequestHandler(
            MessageType.HEARTBEAT_RESPONSE);

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
        
        if (!(message instanceof HeartbeatResponse))
            throw new Error("Data request of wrong TS type!");
        const rootHeartbeatMessage = message as HeartbeatResponse;

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

        const heartbeatMessage = new HeartbeatMessage();
        this.networkManager.send(heartbeatMessage,
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

        this.networkManager.registerRequestHandler(
            MessageType.HEARTBEAT_RESPONSE, this.handleWrongMessage.bind(this));
        this.networkManager.registerRequestHandler(
            MessageType.HEARTBEAT_MESSAGE, this.handleProxyHeartbeat.bind(this));

        this.heartbeatIntervalId = setInterval(
            this.updatePeerList.bind(this),
            this.config.rootUpdatePeerListPeriod * 1000);
    }

    stop() {
        clearInterval(this.heartbeatIntervalId!);

        this.networkManager.unregisterRequestHandler(
            MessageType.HEARTBEAT_MESSAGE);
        this.networkManager.unregisterRequestHandler(
            MessageType.HEARTBEAT_RESPONSE);

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

        const heartbeatMessage = new HeartbeatResponse(
            this.proxyMapFile ? this.proxyMapFile.fileAddr : null);
        this.networkManager.send(heartbeatMessage, senderIp, senderPort);
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
