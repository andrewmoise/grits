import * as dgram from 'dgram';

import { performance } from 'perf_hooks';
import { MessageType, Message } from './messages';
import { RequestHandler, NetworkManager, UdpNetworkManager } from './network';

class DegradedNetworkManager implements NetworkManager {
    private baseManager: NetworkManager;

    downstreamBandwidthLimit: number; // in bytes per second
    downstreamQueueDepth: number; // in bytes
    upstreamBandwidthLimit: number; // in bytes per second
    upstreamQueueDepth: number; // in bytes
    latency: number; // in milliseconds
    packetLossChance: number; // from 0 to 1

    private lastUpdate: number;
    private upstreamBudget: number;
    private downstreamBudget: number;
    
    constructor(
        manager: NetworkManager,
        downstreamBandwidthLimit: number,
        downstreamQueueDepth: number,
        upstreamBandwidthLimit: number,
        upstreamQueueDepth: number,
        latency: number,
        packetLossChance: number)
    {
        this.baseManager = manager;
        
        this.downstreamBandwidthLimit = downstreamBandwidthLimit;
        this.downstreamQueueDepth = downstreamQueueDepth;
        this.upstreamBandwidthLimit = upstreamBandwidthLimit;
        this.upstreamQueueDepth = upstreamQueueDepth;
        this.latency = latency;
        this.packetLossChance = packetLossChance;

        this.lastUpdate = performance.now();
        this.upstreamBudget = 0;
        this.downstreamBudget = 0;
    }

    start(overrideHandler?: (data: Buffer, rinfo: dgram.RemoteInfo) => void)
    : void {
        this.baseManager.start(this.handleIncomingMessage.bind(this));
    }

    stop(): void {
        this.baseManager.stop();
    }

    private updateBudgets() {
        const now = performance.now();
        const deltaTime = (now - this.lastUpdate) / 1000;
        this.lastUpdate = now;

        this.upstreamBudget = Math.max(
            0, this.upstreamBudget
                - this.upstreamBandwidthLimit * deltaTime);
        this.downstreamBudget = Math.max(
            0, this.downstreamBudget
                - this.downstreamBandwidthLimit * deltaTime);   
    }
    
    send(message: Message, ipAddress: string, port: number): void {
        if (Math.random() < this.packetLossChance)
            return;

        // FIXME - this isn't ideal
        const length = message.encode().length + 2 + 28;

        this.updateBudgets();
        if (this.upstreamBudget + length > this.upstreamQueueDepth)
            return;

        this.upstreamBudget += length;

        const delay = this.latency
            + this.upstreamBudget / this.upstreamBandwidthLimit * 1000;

        setTimeout(() => {
            this.baseManager.send(message, ipAddress, port);
        }, delay);
    }
    
    registerRequestHandler(type: number, handler: RequestHandler): void {
        this.baseManager.registerRequestHandler(type, handler);
    }

    unregisterRequestHandler(type: number): void {
        this.baseManager.unregisterRequestHandler(type);
    }

    handleIncomingMessage(data: Buffer, rinfo: dgram.RemoteInfo): void {
        if (Math.random() < this.packetLossChance)
            return;

        const length = data.length + 28;

        this.updateBudgets();
        if (this.downstreamBudget + length > this.downstreamQueueDepth)
            return;

        this.downstreamBudget += length;

        const delay = this.latency
            + this.downstreamBudget / this.downstreamBandwidthLimit * 1000;

        setTimeout(() => {
            this.baseManager.handleIncomingMessage(data, rinfo);
        }, delay);
    }
}

export {
    DegradedNetworkManager,
};
