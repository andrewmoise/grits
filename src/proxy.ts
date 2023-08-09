import * as fs from 'fs';
import * as path from 'path';

import {
    MessageType,
    Message,
    HeartbeatResponse,
    HeartbeatMessage,
    DataFetchMessage,
    DataFetchResponseOk,
    DataFetchResponseNo,
    DhtStoreMessage,
    DhtStoreResponse,
    DhtLookupMessage,
    DhtLookupResponse,
} from './messages';

import { Config } from './config';
import { DownloadManager } from './download';
import { FileCache } from "./filecache";

import {
    AllPeerNodes, CachedFile, PeerNode, FileRetrievalError, DOWNLOAD_CHUNK_SIZE
} from "./structures";

import {
    InRequest, OutRequest, NetworkManager, NetworkManagerImpl
} from './network';

import { BlobFinder } from './dht';
import { LogfileLogger } from './logger';
import { TrafficManagerImpl } from './traffic';

class ProxyDataMap {
    fileAddrToNode: Map<string, Array<{ timestamp: Date, node: PeerNode }>>;
    config: Config;
    
    constructor(config: Config) {
        this.config = config;
        this.fileAddrToNode = new Map();
    }

    // Adds a new fileAddr - node mapping.
    updateData(fileAddr: string, node: PeerNode) {
        let nodeData = this.fileAddrToNode.get(fileAddr);

        if (nodeData) {
            // Check if the node is already in the array.
            let existingNodeData = nodeData.find(
                p => p.node.ip === node.ip && p.node.port === node.port);

            if (existingNodeData) {
                // Update timestamp for the existing node.
                existingNodeData.timestamp = new Date();
            } else {
                // Add new node with current timestamp.
                nodeData.push({ timestamp: new Date(), node });
            }
        } else {
            this.fileAddrToNode.set(fileAddr, [{ timestamp: new Date(), node }]);
        }
    }

    // Removes expired entries (i.e., entries older than 24 hours).
    cleanup() {
        const currentTime = new Date();
        const maxAge = this.config.maxProxyMapAge;

        this.fileAddrToNode.forEach((value, key) => {
            const filteredNodeData = value.filter(nodeData =>
                Math.floor(
                    (currentTime.getTime() - nodeData.timestamp.getTime())
                        / 1000) < maxAge); // maxAge is in seconds

            if (filteredNodeData.length > 0) {
                this.fileAddrToNode.set(key, filteredNodeData);
            } else {
                this.fileAddrToNode.delete(key);
            }
        });
    }
}

abstract class ProxyManagerBase {
    config: Config;
    networkManager: NetworkManager;
    fileCache: FileCache;
    downloadManager: DownloadManager;
    logger: Logger;
    allPeerNodes: AllPeerNodes;
    
    rootNode: PeerNode;
    thisNode: PeerNode;

    proxyDataMap: ProxyDataMap;
    blobFinder: BlobFinder;
    cleanupIntervalId?: NodeJS.Timeout;
    dhtNotifyIntervalId?: NodeJS.Timeout;
    printStateIntervalId?: NodeJS.Timeout;
    
    constructor(config: Config) {
        this.config = config;
        this.logger = new LogfileLogger(config);
        this.allPeerNodes = new AllPeerNodes();

        this.networkManager = new NetworkManagerImpl(
            this.allPeerNodes, this.logger, config);
        
        this.fileCache = new FileCache(config);
        this.downloadManager = new DownloadManager(this);

        this.rootNode = this.allPeerNodes.addPeerNode(
            this.config.rootHost, this.config.rootPort);
        
        if (this.config.rootHost == this.config.thisHost
            && this.config.rootPort == this.config.thisPort)
        {
            this.thisNode = this.rootNode;
        } else {
            this.thisNode = this.allPeerNodes.addPeerNode(
                this.config.thisHost, this.config.thisPort);
        }

        console.log(`Created, this is ${this.config.thisPort}, root is ${this.config.rootPort}`);
        
        this.proxyDataMap = new ProxyDataMap(config);
        this.blobFinder = new BlobFinder();
        this.blobFinder.updatePeers(this.allPeerNodes);
    }
    
    async start(): Promise<void> {
        await this.logger.start();

        this.logger.log('proxies',
                        `Init with ${this.allPeerNodes.peerNodes.size} proxies`);

        this.networkManager.start();

        this.networkManager.registerRequestHandler(
            MessageType.DATA_FETCH_MESSAGE,
            this.handleDataFetchMessage.bind(this));

        this.networkManager.registerRequestHandler(
            MessageType.DHT_STORE_MESSAGE,
            this.handleDhtStoreMessage.bind(this));

        this.networkManager.registerRequestHandler(
            MessageType.DHT_LOOKUP_MESSAGE,
            this.handleDhtLookupRequest.bind(this));
        
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
            MessageType.DHT_STORE_MESSAGE);
        this.networkManager.unregisterRequestHandler(
            MessageType.DATA_FETCH_MESSAGE);

        this.networkManager.stop();

        this.logger.stop();
    }

    async handleDataFetchMessage(request: InRequest, message: Message)
    : Promise<void> {
        if (!(message instanceof DataFetchMessage))
            throw new Error("Data request of wrong TS type!");
        const dataRequestMessage = message as DataFetchMessage;

        const source = request.source;
        const fileAddr = dataRequestMessage.fileAddr;

        if (!message.transferId.match("^[A-Za-z0-9]{8}$"))
            throw new Error(`Malformed transfer ID! ${message.transferId}`);
        
        this.logger.log(message.transferId,
                        `Data request for ${fileAddr}`);
        
        // Try to find in local storage.
        const file = await this.fileCache.readFile(fileAddr);
        if (file) {
            try {
                let offset = dataRequestMessage.offset;
                let length = dataRequestMessage.length;

                while (length > 0) {
                    const packetLen = Math.min(DOWNLOAD_CHUNK_SIZE, length);

                    // TODO - theoretically we could do this file read in
                    // parallel
                    const data = await file.read(offset, packetLen);

                    const responseMessage = new DataFetchResponseOk(
                        fileAddr, offset, packetLen, data);
                    await this.networkManager.requestTransfer(
                        request.source, responseMessage);

                    request.sendResponse(responseMessage);
                        
                    offset += packetLen;
                    length -= packetLen;
                }

                this.logger.log(message.transferId,
                                `All done with upload`);
            } finally {
                file.release();
            }

            return;
        }

        // Return failure, since we can't find it here.
        this.logger.log(message.transferId,
                        `fileAddr is unknown`);

        const responseMessage = new DataFetchResponseNo(
            fileAddr);
        request.sendResponse(responseMessage);
        return;
    }
    
    async handleDhtStoreMessage(request: InRequest, rawMessage: Message)
    : Promise<void> {
        if (!(rawMessage instanceof DhtStoreMessage))
            throw new Error("Received wrong message type!");
        const message = rawMessage as DhtStoreMessage;

        const senderProxy = request.source;
        this.logger.log('dht', `Got location ${message.fileAddr} at ${senderProxy.ip}:${senderProxy.port}`);
        
        if (senderProxy) {
            this.proxyDataMap.updateData(message.fileAddr,
                                         senderProxy);
        } else {
            this.logger.log('dht', '  But no proxy found!');
        }

        this.logger.log('dht', `Sending location response for ${message.fileAddr} to ${senderProxy.ip}:${senderProxy.port}`);
        
        const response = new DhtStoreResponse(message.fileAddr);
        await this.networkManager.requestTransfer(senderProxy, response);
        request.sendResponse(response);
    }

    async handleDhtLookupRequest(request: InRequest, message: Message)
    : Promise<void> {
        if (!(message instanceof DhtLookupMessage))
            throw new Error("DHT request of wrong TS type!");
        const dhtLookupMessage = message as DhtLookupMessage;

        const fileAddr = dhtLookupMessage.fileAddr;

        if (!message.transferId.match("^[A-Za-z0-9]{8}$"))
            throw new Error(`Malformed transfer ID! ${message.transferId}`);
        
        this.logger.log(message.transferId,
                        `DHT request for ${fileAddr}`);
        
        let fileProxies = this.proxyDataMap.fileAddrToNode.get(fileAddr);
        let nodeInfo: Array<{ip: string, port: number}> = [];

        if (fileProxies) {
            this.logger.log(message.transferId,
                            `  Found ${fileProxies.length} remote sources`);

            // Sort proxies by timestamp in descending order and take the most recent ones
            let recentProxies = fileProxies.sort(
                (a, b) => b.timestamp.getTime() - a.timestamp.getTime()
            ).slice(0, this.config.dhtMaxResponseNodes);
            
            nodeInfo = recentProxies.map(
                proxyData => ({ ip: proxyData.node.ip, port: proxyData.node.port }));
        } else {
            this.logger.log(message.transferId,
                            `  Got nothing from remote source lookup`);

            nodeInfo = [];
        }

        let localFile = await this.fileCache.readFile(fileAddr);
        if (localFile) {
            this.logger.log(message.transferId,
                            '  Found in local storage');
            
            nodeInfo.push({ip: this.thisNode.ip, port: this.thisNode.port});
            localFile.refCount--;
        }

        this.logger.log(message.transferId,
                        `  Finished lookup: ${nodeInfo.length} nodes`);
        
        const responseMessage = new DhtLookupResponse(
            fileAddr, nodeInfo);
        await this.networkManager.requestTransfer(
            request.source, responseMessage);
        request.sendResponse(responseMessage);
    }

    
    async dhtNotify(cachedFile: CachedFile): Promise<void> {
        this.logger.log('dht', `DHT Notify ${cachedFile.fileAddr}`);

        const closestNodes = await this.blobFinder.getClosestPeers(
            cachedFile.fileAddr, this.config.dhtNotifyNumber);
        const refreshAge = this.config.dhtRefreshTime;
        
        for (const node of closestNodes) {
            // Don't process if this is us
            if (node == this.thisNode)
                continue;
            
            // Don't process if we have a recent refresh timestamp
            const lastRefresh: Date|undefined =
                node.dhtStoredData.get(cachedFile);
            
            if (lastRefresh) {
                if (new Date().getTime() - lastRefresh.getTime() < refreshAge) {
                    this.logger.log('dht', `Too recent for refresh: ${new Date()} <-> ${lastRefresh}`);
                    continue;
                } else {
                    this.logger.log('dht', `Too old, we refresh: ${new Date()} <-> ${lastRefresh}`);
                }                    
            }

            // We have no refresh timestamp, or an old one - send notification

            this.logger.log(
                'dht', `DHT Refresh ${cachedFile.fileAddr} on ${node.ip}:${node.port}`);
            
            const message = new DhtStoreMessage(cachedFile.fileAddr);
            await this.networkManager.requestTransfer(node, message);
            const request = this.networkManager.newRequest(node, message);
            const response = await request.getResponse();

            if (response)
                this.handleDhtResponse(node.ip, node.port, response);
            request.close();
        }
    }

    notifyAllRunning: boolean = false;
    
    async dhtNotifyAll(): Promise<void> {
        this.logger.log('dht', 'DHT notify loop');

        if (this.notifyAllRunning) {
            this.logger.log('dht', '  early return');
            return;
        }
        
        this.notifyAllRunning = true;
        for (const cachedFile of this.fileCache.getFiles())
            await this.dhtNotify(cachedFile);
        this.notifyAllRunning = false;
    }

    async handleDhtResponse(senderIp: string, senderPort: number,
                            rawMessage: Message)
    : Promise<void> {
        if (!(rawMessage instanceof DhtStoreResponse))
            throw new Error("Data request of wrong TS type!");
        const message = rawMessage as DhtStoreResponse;

        this.logger.log('dht', `DHT ack from ${senderIp}:${senderPort} for ${message.fileAddr}`);
        
        const node = this.allPeerNodes.getPeerNode(senderIp, senderPort);
        if (!node) {
            this.logger.log('dht', '  No peer found');
            return;
        }

        const cachedFile = await this.fileCache.readFile(message.fileAddr);
        if (!cachedFile) {
            this.logger.log('dht', '  No file found');
            return;
        }

        node.dhtStoredData.set(cachedFile, new Date());
        cachedFile.release(); // FIXME - needs new API
    }
    
    async retrieveFile(fileAddr: string): Promise<CachedFile> {
        this.logger.log('proxy', `Retrieve file: ${fileAddr}`);

        // Try to find in local storage.
        const file = await this.fileCache.readFile(fileAddr);
        if (file)
            return file;

        this.logger.log('proxy', "  Not in storage.");
        
        // If the file isn't in the cache, attempt to download it
        const downloadedFile = await this.downloadManager.download(fileAddr);
        return downloadedFile;
    }

    async printState(): Promise<void> {
        console.log("---------- State of Proxy Manager " + this.config.thisHost
            + ":" + this.config.thisPort + " ----------");
        console.log("Number of peer proxies: " + this.allPeerNodes.peerNodes.size);

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
            + this.proxyDataMap.fileAddrToNode.size);
        console.log("------------------------------------------------");
    }
}

class ProxyManager extends ProxyManagerBase {
    sendProxyHeartbeatIntervalId?: NodeJS.Timeout;

    constructor(config: Config) {
        super(config);
    }

    async start() {
        await super.start();

        this.sendProxyHeartbeatIntervalId = setInterval(
            this.sendProxyHeartbeat.bind(this),
            this.config.proxyHeartbeatPeriod * 1000);
    }

    stop() {
        clearInterval(this.sendProxyHeartbeatIntervalId!);
        super.stop();
    }

    async sendProxyHeartbeat(): Promise<void> {
        this.logger.log('heartbeat', 'Sending proxy heartbeat');

        const heartbeatMessage = new HeartbeatMessage();
        await this.networkManager.requestTransfer(
            this.rootNode, heartbeatMessage);
        const request = this.networkManager.newRequest(
            this.rootNode, heartbeatMessage);

        try {
            const responseMessage = await request.getResponse();
            await this.handleRootHeartbeat(
                this.rootNode.ip,
                this.rootNode.port,
                responseMessage);
        } catch (err) {
            this.logger.log(
                'heartbeat',
                `Error: ${err}`);
        } finally {
            request.close();
        }
    }

    async handleRootHeartbeat(senderIp: string, senderPort: number,
                              message: Message | null)
    : Promise<void> {
        this.logger.log(
            'heartbeat',
            `Got root heartbeat, ${this.allPeerNodes.peerNodes.size} proxies`);

        if (!(message instanceof HeartbeatResponse))
            throw new Error("Data request of wrong type or no response!");
        
        const rootHeartbeatMessage = message as HeartbeatResponse;

        // Validate the sender
        if (senderIp !== this.rootNode.ip
            || senderPort !== this.rootNode.port)
        {
            console.warn(`Received root heartbeat from unknown source ${senderIp}:${senderPort}.`);
            return;
        }

        // Update the lastSeen time of the root node
        this.rootNode.updateLastSeen();

        //console.log("Got root heartbeat.");

        const nodeMapFileAddr = rootHeartbeatMessage.nodeMapFileAddr;
        if (nodeMapFileAddr) {
            const nodeMapFile = await this.downloadManager.download(
                nodeMapFileAddr);
            //console.log(`Successful download: ${nodeMapFile.path}`);

            // Read the downloaded file and update local peerNodes
            const data = await fs.promises.readFile(nodeMapFile.path, 'utf-8');
            const newPeers = JSON.parse(data);

            // Create a temporary set for easy lookup
            const newPeersSet = new Set();
            newPeers.forEach((peer: { ip: string; port: number; }) => {
                newPeersSet.add(this.allPeerNodes.generateNodeKey(peer.ip, peer.port));
                const peerNode = this.allPeerNodes.getPeerNode(
                    peer.ip, peer.port);
                peerNode.updateLastSeen();
            });

            // Remove peers not present in the new list
            for (let [key, proxy] of this.allPeerNodes.peerNodes.entries()) {
                if (!newPeersSet.has(key)) {
                    this.allPeerNodes.peerNodes.delete(key);
                } else {
                    //console.log(
                    //    `  have peer: ${this.allPeerNodes.peerNodes.get(key)!.port}`);
                }
            }

            this.blobFinder.updatePeers(this.allPeerNodes);
            
            //console.log(`Updated local peerNodes.`);
        } else {
            this.logger.log(
                'heartbeat',
                'Root heartbeat, but no map.');
        }
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
            MessageType.HEARTBEAT_MESSAGE, this.handleProxyHeartbeat.bind(this));

        this.heartbeatIntervalId = setInterval(
            this.updatePeerList.bind(this),
            this.config.rootUpdatePeerListPeriod * 1000);
    }

    stop() {
        clearInterval(this.heartbeatIntervalId!);

        this.networkManager.unregisterRequestHandler(
            MessageType.HEARTBEAT_MESSAGE);

        super.stop();
    }

    async handleProxyHeartbeat(request: InRequest, message: Message): Promise<void> {
        let peerNode = this.allPeerNodes.getPeerNode(
            request.source.ip, request.source.port);
        
        this.logger.log(
            'heartbeat',
            `Got proxy heartbeat from ${peerNode.ip}:${peerNode.port}`);

        peerNode.updateLastSeen();

        this.logger.log(
            'heartbeat',
            `Send root heartbeat to ${peerNode.ip}:${peerNode.port}`);

        const heartbeatMessage = new HeartbeatResponse(
            this.proxyMapFile ? this.proxyMapFile.fileAddr : null);
        await this.networkManager.requestTransfer(peerNode, heartbeatMessage);
        request.sendResponse(heartbeatMessage);
    }

    async createProxyMapBuffer(): Promise<void> {
        const peerList = Array.from(this.allPeerNodes.peerNodes.values()).map(peerNode => ({
            ip: peerNode.ip,
            port: peerNode.port,
        }));

        const randomSuffix = Math.floor(Math.random() * 1000000).toString();

        const filePath = path.join(this.config.tempDownloadDirectory, `proxies-${randomSuffix}`);
        await fs.promises.writeFile(filePath, JSON.stringify(peerList, null, 4), 'utf-8');

        const file: CachedFile = await this.fileCache.addFile(filePath, null, true, true);
        if (file !== this.proxyMapFile) {
            if (this.proxyMapFile) this.proxyMapFile.release();
            this.proxyMapFile = file;
        } else {
            file.release();
        }

        this.proxyDataMap.updateData(file.fileAddr, this.thisNode); // updated from thisProxy to thisNode
    }

    async updatePeerList(): Promise<void> {
        for (const [key, node] of this.allPeerNodes.peerNodes.entries())
            if (node.timeSinceSeen() > this.config.rootProxyDropTimeout)
                this.allPeerNodes.removePeerNode(node.ip, node.port);

        this.blobFinder.updatePeers(this.allPeerNodes);
        await this.createProxyMapBuffer();
    }
}

export {
    ProxyManagerBase,
    ProxyManager,
    RootProxyManager,
};
