import { performance } from 'perf_hooks';
import { promisify } from 'util';
const sleep = promisify(setTimeout);

import { Config } from './config';
import { DOWNLOAD_CHUNK_SIZE, PeerProxy } from './structures';

interface PotentialDownloadBurst {
    source: PeerProxy;
    bytesAllowed: number;
}

class TrafficManager {
    upstream: TrafficShaper;
    downstream: TrafficShaper;
    downstreamReceived: TrafficShaper;

    constructor(config: Config) {
        this.upstream = new TrafficShaper(
            config.maxUpstreamSpeed, config.maxUpstreamQueue);

        this.downstream = new TrafficShaper(
            config.maxDownstreamSpeed, config.maxDownstreamQueue);

        this.downstreamReceived = new TrafficShaper(
            config.maxDownstreamSpeed, config.maxDownstreamQueue);
    }

    async requestUpload(bytes: number): Promise<number> {
        return await this.upstream.requestTransfer(bytes);
    }

    public async requestDownload(peerProxies: PeerProxy[],
                                 bytesRequested: number)
    : Promise<PotentialDownloadBurst[] | null> {
        const bytesAllowed = await this.downstream.requestTransfer(
            bytesRequested);
        
        const burstsPerPeer = Math.ceil(
            bytesRequested / DOWNLOAD_CHUNK_SIZE / peerProxies.length);
            
        const downloadBursts: PotentialDownloadBurst[] = [];
            
        for (let i = 0; i < peerProxies.length; i++) {
            const bytesThisPeer = Math.min(burstsPerPeer * DOWNLOAD_CHUNK_SIZE,
                                           bytesRequested);
            bytesRequested -= bytesThisPeer;

            downloadBursts.push({
                source: peerProxies[i],
                bytesAllowed: bytesThisPeer,
            });

            if (bytesRequested <= 0)
                break;
        }

        return downloadBursts;
    }
};

class TrafficShaper {
    bytesBudgeted: number;
    lastUpdate: number;

    maxSpeed: number; // bytes per second
    queueDepth: number; // bytes    

    resetTimestamp: number | null;
    bytesSinceReset: number;
    
    constructor(maxSpeed: number, queueDepth: number) {
        this.bytesBudgeted = 0;
        this.lastUpdate = performance.now();

        this.maxSpeed = maxSpeed;
        this.queueDepth = queueDepth;

        this.resetTimestamp = null;
        this.bytesSinceReset = 0;
    }

    private updateBudgets() {
        const now = performance.now();
        const deltaTime = (now - this.lastUpdate) / 1000;
        this.lastUpdate = now;

        this.bytesBudgeted = Math.max(
            0, this.bytesBudgeted - this.maxSpeed * deltaTime);
    }

    public async requestTransfer(bytes: number): Promise<number> {
        this.updateBudgets();

        const maxBytes = Math.min(bytes, this.queueDepth);

        if (this.bytesBudgeted == 0) {
            this.resetTimestamp = performance.now();
            this.bytesSinceReset = 0;
        }

        this.bytesSinceReset += maxBytes;
        this.bytesBudgeted += maxBytes;

        if (this.bytesBudgeted >= this.queueDepth+1) {
            const waitTime =
                (this.bytesBudgeted - this.queueDepth / 2)
                / this.maxSpeed * 1000;
            await sleep(waitTime);
        }

        return maxBytes;
    }   
}

export { PotentialDownloadBurst, TrafficShaper, TrafficManager };
