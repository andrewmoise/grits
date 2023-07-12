import { performance } from 'perf_hooks';
import { promisify } from 'util';
const sleep = promisify(setTimeout);

import { Config } from './config';
import { DOWNLOAD_CHUNK_SIZE, PeerProxy } from './structures';

interface PotentialDownloadBurst {
    source: PeerProxy;
    bytesAllowed: number;
}

class DownstreamManager {
    private config: Config;
    
    private bytesRequestedThisTick: number;
    private bytesReceivedThisTick: number;
    
    private downloadQueue: {
        resolve: Function,
        peerProxies: PeerProxy[],
        bytesRequested: number,
    }[];

    constructor(config: Config) {
        this.config = config;

        this.bytesRequestedThisTick = 0;
        this.bytesReceivedThisTick = 0;
        this.downloadQueue = [];

        setInterval(() => this.tick(), config.downloadTickPeriod);
    }

    requestDownload(
        peerProxies: PeerProxy[],
        bytesRequested: number, queueOnFailure=true)
    : Promise<PotentialDownloadBurst[] | null>
    {
        console.log(`requestDownload() for ${bytesRequested}`);
        
        let bytesBudget = this.config.maxDownstreamSpeed / 1000
            * this.config.downloadTickPeriod;
        bytesBudget -= this.bytesRequestedThisTick;

        if (bytesBudget > 0) {
            if (bytesRequested > bytesBudget)
                bytesRequested = bytesBudget;

            this.bytesRequestedThisTick += bytesRequested;

            const burstsPerPeer = Math.ceil(
                bytesRequested / DOWNLOAD_CHUNK_SIZE / peerProxies.length);
            
            const downloadBursts: PotentialDownloadBurst[] = [];
            
            for (let i = 0; i < peerProxies.length; i++) {
                downloadBursts.push({
                    source: peerProxies[i],
                    bytesAllowed: burstsPerPeer * DOWNLOAD_CHUNK_SIZE
                });
            }

            console.log(`  Return right away, ${downloadBursts.length} bursts`);
            
            return Promise.resolve(downloadBursts);
        } else if (queueOnFailure) {
            console.log(`  Queue up for later`);
            
            // Too many bytes requested this tick, queue up the request
            // and return a promise.
            return new Promise((resolve) => {
                this.downloadQueue.push({
                    resolve, peerProxies, bytesRequested });
            });
        } else {
            console.log('  Return null');
            
            return Promise.resolve(null);
        }
    }

    private async tick(): Promise<void> {
        // Reset byte count for this tick.
        this.bytesRequestedThisTick = 0;
        this.bytesReceivedThisTick = 0;

        // Process queued downloads.
        while (this.downloadQueue.length > 0) {
            console.log('Process from queue');

            const queuedDownload = this.downloadQueue[0];
            this.downloadQueue.shift();
            const { resolve, peerProxies, bytesRequested } = queuedDownload;
            
            const bursts = await this.requestDownload(
                peerProxies, bytesRequested, false);

            if (bursts !== null) {
                console.log('  Non null, resolve');
                resolve(bursts);
            } else {
                console.log('  Null - all done for now');
                this.downloadQueue.unshift(queuedDownload);
                break;
            }
        }
    }
}

class UpstreamManager {
    private upstreamBudget: number;
    private lastUpdate: number;

    private config: Config;
    
    constructor(config: Config) {
        this.upstreamBudget = 0;
        this.lastUpdate = performance.now();

        this.config = config;
    }

    private updateBudgets() {
        const now = performance.now();
        const deltaTime = (now - this.lastUpdate) / 1000;
        this.lastUpdate = now;

        this.upstreamBudget = Math.max(
            0, this.upstreamBudget - this.config.maxUpstreamSpeed * deltaTime);
    }

    public async requestUpload(bytes: number): Promise<number> {
        this.updateBudgets();

        const maxBytes = Math.min(bytes, this.config.maxUpstreamQueue);
        this.upstreamBudget += maxBytes;

        if (this.upstreamBudget >= this.config.maxUpstreamQueue+1) {
            const waitTime =
                (this.upstreamBudget - this.config.maxUpstreamQueue / 2)
                / this.config.maxUpstreamSpeed * 1000;
            await sleep(waitTime);
        }

        return maxBytes;
    }   
}

export { PotentialDownloadBurst, DownstreamManager, UpstreamManager };
