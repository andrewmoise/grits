import {
    MessageType,
    Message,
    RootHeartbeatMessage,
    ProxyHeartbeatMessage,
    DataRequestMessage,
    DataResponseOkMessage,
    DataResponseElsewhereMessage,
    DataResponseUnknownMessage,
} from './messages';

import { Config } from './config';
import { FileCache } from './filecache';
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

abstract class ProxyManagerBase {
    config: Config;
    networkingManager: NetworkingManager;
    peerProxies: Map<string, PeerProxy>;
    fileCache: FileCache;

    constructor(config: Config) {
        this.config = config;
        this.networkingManager = new NetworkingManager(config);
        this.peerProxies = new Map();
        this.fileCache = new FileCache(config);
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
            console.log("About to return peer: " + peerProxy);
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
    }

    stop(): void {
        this.networkingManager.unregisterRequestHandler(
            MessageType.DATA_REQUEST);

        this.networkingManager.stop();
    }

    async handleDataRequest(senderIp: string, senderPort: number,
        message: Message): Promise<void> {
        if (!(message instanceof DataRequestMessage))
            throw new Error("Data request of wrong TS type!");
        const dataRequestMessage = message as DataRequestMessage;

        const data = await this.fileCache.readFile(
            dataRequestMessage.dataHash, 0, dataRequestMessage.length);
        if (data !== null) {
            const responseMessage = new DataResponseOkMessage(
                dataRequestMessage.dataHash, dataRequestMessage.offset,
                dataRequestMessage.length, data);
            this.networkingManager.send(responseMessage, senderIp, senderPort);
        } else {
            const responseMessage = new DataResponseUnknownMessage(
                dataRequestMessage.dataHash);
            this.networkingManager.send(responseMessage, senderIp, senderPort);
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

    handleRootHeartbeat(senderIp: string, senderPort: number,
        message: Message) {
        // Validate the sender
        if (senderIp !== this.rootProxy.ip
            || senderPort !== this.rootProxy.port) {
            console.warn(`Received root heartbeat from unknown source ${senderIp}:${senderPort}.`);
            return;
        }

        // Update the lastSeen time of the root proxy
        this.rootProxy.updateLastSeen();

        console.log("Got root heartbeat.");
    }

    sendProxyHeartbeat() {
        console.log("Send proxy heartbeat");

        const heartbeatMessage = new ProxyHeartbeatMessage();
        this.networkingManager.send(heartbeatMessage,
            this.rootProxy.ip,
            this.rootProxy.port);
    }
}

class RootProxyManager extends ProxyManagerBase {
    rootProxy: PeerProxy;
    heartbeatIntervalId?: NodeJS.Timeout;
    proxyMapBuffer: Buffer;

    constructor(config: Config) {
        super(config);

        let rootProxy = new PeerProxy(
            this.networkingManager.config.thisHost,
            this.networkingManager.config.thisPort);
        this.rootProxy = rootProxy;

        this.proxyMapBuffer = Buffer.alloc(0);
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
        message: Message): void {
        console.log("We got a heartbeat.");

        let peerProxy = this.getPeerProxy(senderIp, senderPort);
        if (!peerProxy) {
            peerProxy = this.addPeerProxy(senderIp, senderPort);
            console.log("Adding");
        }
        console.log("About to update " + peerProxy);

        peerProxy.updateLastSeen();

        // Send root heartbeat back to the proxy
        const heartbeatMessage = new RootHeartbeatMessage(
            this.proxyMapBuffer);
        this.networkingManager.send(heartbeatMessage, senderIp, senderPort);
    }

    createProxyMapBuffer(): void {
        const peerList = Array.from(this.peerProxies.values()).map(peerProxy =>
        ({
            ip: peerProxy.ip,
            port: peerProxy.port,
        }));
        const peerListJson = JSON.stringify(peerList);
        this.proxyMapBuffer = Buffer.from(peerListJson, 'utf-8');
    }

    updatePeerList(): void {
        for (const [key, proxy] of this.peerProxies.entries())
            if (proxy.timeSinceSeen() > this.config.rootProxyDropTimeout)
                this.peerProxies.delete(key);

        this.createProxyMapBuffer();
    }
}

export {
    ProxyManager,
    RootProxyManager,
};
