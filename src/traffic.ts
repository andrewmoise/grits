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
    private bytesBudgeted: number;
    private lastUpdate: number;

    private config: Config;
    private maxSpeed: number; // bytes per second
    private queueDepth: number; // bytes    
    
    constructor(config: Config, maxSpeed: number, queueDepth: number) {
        this.bytesBudgeted = 0;
        this.lastUpdate = performance.now();

        this.config = config;
        this.maxSpeed = maxSpeed;
        this.queueDepth = queueDepth;
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
        this.bytesBudgeted += maxBytes;

        if (this.bytesBudgeted >= this.queueDepth+1) {
            const waitTime =
                (this.bytesBudgeted - this.queueDepth / 2)
                / this.maxSpeed * 1000;
            await sleep(waitTime);
        }

        return maxBytes;
    }   

    public async requestDownload(peerProxies: PeerProxy[],
                                 bytesRequested: number)
    : Promise<PotentialDownloadBurst[] | null> {
        const bytesAllowed = await this.requestTransfer(bytesRequested);
        
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
}

export { PotentialDownloadBurst, TrafficManager };
