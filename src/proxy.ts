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
import { FileCache, CachedFile } from './filecache';
import { NetworkingManager } from './network';

class PeerProxy {
    ip: string;
    port: number;
    lastSeen: Date | null;

    constructor(ip: string, port: number) {
        this.ip = ip;
        this.port = port;
        this.lastSeen = null;
    }

    updateLastSeen() {
        this.lastSeen = new Date();
    }

    // Returns seconds
    timeSinceSeen() {
        if (this.lastSeen === null)
            return -1;
        const currentTime = new Date();
        return Math.floor(
            (currentTime.getTime() - this.lastSeen.getTime()) / 1000);
    }
}

// ProxyDataMap class to track the most recent proxies where a file has been
// seen.
class ProxyDataMap {
    fileAddrToProxy: Map<
        string, { timestamp: Date, proxies: Array<PeerProxy> }>;

    constructor() {
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
    cleanup(maxAge: number) {
        const currentTime = new Date();
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
    peerProxies: Map<string, PeerProxy>;
    fileCache: FileCache;
    downloadManager: DownloadManager;
    proxyDataMap: ProxyDataMap;
    cleanupIntervalId?: number;
    
    constructor(config: Config) {
        this.config = config;
        this.networkingManager = new NetworkingManager(config);
        this.peerProxies = new Map();
        this.fileCache = new FileCache(config);
        this.downloadManager = new DownloadManager(this);
        this.proxyDataMap = new ProxyDataMap();
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

    start(): void {
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
    
    }

    stop(): void {
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
    }

    async handleDataRequest(senderIp: string, senderPort: number,
                            message: Message): Promise<void>
    {
        if (!(message instanceof DataRequestMessage))
            throw new Error("Data request of wrong TS type!");
        const dataRequestMessage = message as DataRequestMessage;

        const fileAddr = dataRequestMessage.fileAddr;
        const file = await this.fileCache.readFile(fileAddr);

        if (!file) {
            const responseMessage = new DataResponseUnknownMessage(
                fileAddr);
            this.networkingManager.send(responseMessage, senderIp, senderPort);
            return;
        }
        
        try {
            const data = await file.read(
                dataRequestMessage.offset, dataRequestMessage.length);

            const responseMessage = new DataResponseOkMessage(
                fileAddr, dataRequestMessage.offset,
                dataRequestMessage.length, data);
            this.networkingManager.send(responseMessage, senderIp, senderPort);
        } finally {
            file.release();
        }
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
}

class ProxyManager extends ProxyManagerBase {
    rootProxy: PeerProxy;
    sendProxyHeartbeatIntervalId?: NodeJS.Timeout;

    constructor(config: Config) {
        super(config);

        if (!config.rootHost || !config.rootPort)
            throw new Error("Must define root host and port!");

        let rootProxy = new PeerProxy(config.rootHost, config.rootPort);
        this.rootProxy = rootProxy;
    }

    start() {
        console.log("Set up proxy + start");

        super.start();

        this.networkingManager.registerRequestHandler(
            MessageType.PROXY_HEARTBEAT, this.handleWrongMessage.bind(this));
        this.networkingManager.registerRequestHandler(
            MessageType.ROOT_HEARTBEAT, this.handleRootHeartbeat.bind(this));

        // Send proxy heartbeat to root every 60 seconds
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

        console.log("Got root heartbeat.");

        const nodeMapFileAddr = rootHeartbeatMessage.nodeMapFileAddr;
        if (nodeMapFileAddr) {
            const nodeMapFile = await this.downloadManager.download(
                nodeMapFileAddr);
            console.log(`Successful download: ${nodeMapFile.path}`);

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

            //console.log(`Updated local peerProxies.`);
        } else {
            console.log("Root heartbeat, but no map.");
        }
    }

    sendProxyHeartbeat() {
        //console.log("Send proxy heartbeat");

        const heartbeatMessage = new ProxyHeartbeatMessage();
        this.networkingManager.send(heartbeatMessage,
            this.rootProxy.ip,
            this.rootProxy.port);
    }
}

class RootProxyManager extends ProxyManagerBase {
    rootProxy: PeerProxy;
    heartbeatIntervalId?: NodeJS.Timeout;
    proxyMapFile: CachedFile | null;

    constructor(config: Config) {
        super(config);

        let rootProxy = new PeerProxy(
            this.networkingManager.config.thisHost,
            this.networkingManager.config.thisPort);
        this.rootProxy = rootProxy;

        this.proxyMapFile = null;
    }

    start() {
        super.start();

        this.networkingManager.registerRequestHandler(
            MessageType.ROOT_HEARTBEAT, this.handleWrongMessage.bind(this));
        this.networkingManager.registerRequestHandler(
            MessageType.PROXY_HEARTBEAT, this.handleProxyHeartbeat.bind(this));

        this.heartbeatIntervalId = setInterval(
            this.updatePeerList.bind(this),
            this.config.rootHeartbeatPeriod * 1000);
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
        console.log("We got a heartbeat.");

        let peerProxy = this.getPeerProxy(senderIp, senderPort);
        if (!peerProxy) {
            peerProxy = this.addPeerProxy(senderIp, senderPort);
            console.log(`Adding ${senderIp}:${senderPort}`);
        }

        peerProxy.updateLastSeen();

        // Send root heartbeat back to the proxy
        const heartbeatMessage = new RootHeartbeatMessage(
            this.proxyMapFile ? this.proxyMapFile.fileAddr : null);
        this.networkingManager.send(heartbeatMessage, senderIp, senderPort);
    }

    async createProxyMapBuffer(): Promise<void> {
        const peerList = Array.from(this.peerProxies.values()).map(peerProxy =>
        ({
            ip: peerProxy.ip,
            port: peerProxy.port,
        }));

        const filePath = path.join(this.config.tempDownloadDirectory,
                                   `proxies-${this.config.thisPort}`);
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

        await this.createProxyMapBuffer();
    }
}

export {
    ProxyManagerBase,
    ProxyManager,
    RootProxyManager,
};
