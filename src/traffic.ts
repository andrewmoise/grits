import { performance } from 'perf_hooks';
import { promisify } from 'util';
const sleep = promisify(setTimeout);

import { Config } from './config';
import { NetworkManager, AllowedTransfer } from './network';
import { DOWNLOAD_CHUNK_SIZE, PeerProxy } from './structures';
import { TelemetryFetchMessage, TelemetryFetchResponse } from './messages';

const RATE_UPDATE_THRESHOLD = 0.1; // in seconds

class TrafficShaper {
    bytesBudgeted: number;
    lastUpdate: number;

    maxSpeed: number; // bytes per second
    queueDepth: number; // bytes    

    constructor(maxSpeed: number, queueDepth: number) {
        this.bytesBudgeted = 0;
        this.lastUpdate = performance.now();

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

    public async requestTransfer(bytes: number, blockUntilReady: boolean = true)
    : Promise<number> {
        this.updateBudgets();

        const maxBytes = Math.min(bytes, this.queueDepth);

        this.bytesBudgeted += maxBytes;

        if (blockUntilReady && this.bytesBudgeted >= this.queueDepth+1) {
            const waitTime =
                (this.bytesBudgeted - this.queueDepth / 2)
                / this.maxSpeed * 1000;
            await sleep(waitTime);
        }

        return maxBytes;
    }   
}

class TrafficManager {
    networkManager: NetworkManager;
    config: Config;
    
    upstream: TrafficShaper;
    requestedDownstream: TrafficShaper;
    observedDownstream: TrafficShaper;
    
    upstreamByteCounter: number;
    upstreamBurstStart: number;
    upstreamTelemetryId: number;
    upstreamHosts: Set<PeerProxy>;
    
    downstreamByteCounter: number;
    downstreamBurstStart: number;

    latency: number | null;
    packetLoss: number | null;
    
    constructor(networkManager: NetworkManager, config: Config) {
        this.networkManager = networkManager;
        this.config = config;
        
        this.upstream = new TrafficShaper(
            config.maxUpstreamSpeed, config.maxUpstreamQueue);

        this.requestedDownstream = new TrafficShaper(
            config.maxDownstreamSpeed, config.maxDownstreamQueue);

        this.observedDownstream = new TrafficShaper(
            config.maxDownstreamSpeed, 99999999999);
        
        this.upstreamByteCounter = 0;
        this.upstreamBurstStart = 0;
        this.upstreamTelemetryId = -1;
        this.upstreamHosts = new Set();

        this.downstreamByteCounter = 0;
        this.downstreamBurstStart = 0;

        this.latency = null;
        this.packetLoss = null;
    }

    async requestUpload(peerProxy: PeerProxy, bytes: number)
    : Promise<AllowedTransfer> {
        let bytesAllowed = await this.upstream.requestTransfer(bytes);

        if (this.upstreamBurstStart == 0
            || this.upstream.bytesBudgeted == bytesAllowed)
        {
            // We're starting a new burst.
            this.upstreamByteCounter = 0;
            this.upstreamHosts = new Set();
            this.upstreamBurstStart = performance.now();
            this.upstreamTelemetryId = this.networkManager.newTelemetryId();
        }

        this.upstreamByteCounter += bytesAllowed;
        this.upstreamHosts.add(peerProxy);

        const result: AllowedTransfer = {
            source: peerProxy,
            bytesAllowed,
            telemetryId: this.upstreamTelemetryId
        };
        
        // Disabled temporarily
        if (false && this.upstreamByteCounter
            >= this.upstream.maxSpeed * RATE_UPDATE_THRESHOLD)
        {
            // We have a sustained burst, including this request -- initiate
            // a recovery of how many bytes went through, and reset the burst
            // so the next request will get a new telemetry ID
            
            this.upstream.maxSpeed *= this.config.performanceUpdateStiffness;
            this.upstreamBurstStart = 0;

            // Request telemetry from each remote host
            const message = new TelemetryFetchMessage(
                this.upstreamTelemetryId);
            for (let proxy of this.upstreamHosts)
                this.recoverTelemetry(proxy, message);
        }
            
        return result;
    }

    private async recoverTelemetry(peerProxy: PeerProxy,
                                   message: TelemetryFetchMessage)
    : Promise<void> {
        try {
            for(let attempt = 0;
                attempt < this.config.telemetryFetchRetries;
                attempt++)
            {
                const request = this.networkManager.newRequest(
                    peerProxy.ip, peerProxy.port, message);
                const rawResponse = await request.getResponse();
                request.close();
                
                if (rawResponse == null)
                    continue;
                if (!(rawResponse instanceof TelemetryFetchResponse))
                    throw new Error('Wrong response type!');

                const response = rawResponse as TelemetryFetchResponse;
                this.upstream.maxSpeed +=
                    (1 - this.config.performanceUpdateStiffness)
                    * response.byteCount / RATE_UPDATE_THRESHOLD;

                return;
            }

            this.networkManager.log(`Couldn't get telemetry from ${peerProxy.ip}:${peerProxy.port}!`);
        } catch (err) {
            this.networkManager.log(`Error in traffic: ${err}`);
        }
    }
    
    public async requestDownload(peerProxies: PeerProxy[],
                                 bytesRequested: number)
    : Promise<AllowedTransfer[] | null> {
        const bytesAllowed = await this.requestedDownstream.requestTransfer(
            bytesRequested);

        const burstsPerPeer = Math.ceil(
            bytesRequested / DOWNLOAD_CHUNK_SIZE / peerProxies.length);
            
        const downloadBursts: AllowedTransfer[] = [];
            
        for (let i = 0; i < peerProxies.length; i++) {
            const bytesThisPeer = Math.min(burstsPerPeer * DOWNLOAD_CHUNK_SIZE,
                                           bytesRequested);
            bytesRequested -= bytesThisPeer;

            downloadBursts.push({
                source: peerProxies[i],
                telemetryId: -1,
                bytesAllowed: bytesThisPeer,
            });

            if (bytesRequested <= 0)
                break;
        }

        return downloadBursts;
    }

    // Disabled temporarily
    public async notifyDownload(actualBytes: number, wholeTransferBytes: number) {
        await this.observedDownstream.requestTransfer(wholeTransferBytes, false);

        const now = performance.now();
        
        if (this.downstreamBurstStart == 0
            || this.observedDownstream.bytesBudgeted == wholeTransferBytes)
        {
            // We're starting a new burst.

            this.downstreamByteCounter = 0;
            this.downstreamBurstStart = now;
        } else {
            // We don't count the first packet's bytes to prevent
            // an off-by-one error in the rate calculation
            
            this.downstreamByteCounter += actualBytes;
        }
        
        const burstLength = now - this.downstreamBurstStart;
        if (burstLength >= RATE_UPDATE_THRESHOLD) {
            const rate = this.downstreamByteCounter / burstLength;

            this.requestedDownstream.maxSpeed *=
                this.config.performanceUpdateStiffness;
            this.requestedDownstream.maxSpeed +=
                rate * (1 - this.config.performanceUpdateStiffness);
            
            this.observedDownstream.maxSpeed =
                this.requestedDownstream.maxSpeed;

            // And again, we don't count the first packet's bytes
            this.downstreamBurstStart = now;
            this.downstreamByteCounter = 0;
        }
    }

    public notifyLatency(latency: number) {
        if (this.latency === null)
            this.latency = latency;
        else
            this.latency = this.config.performanceUpdateStiffness * this.latency
                + latency * (1 - this.config.performanceUpdateStiffness);
    }

    public notifyPacketLoss(loss: number) {
        if (this.packetLoss === null)
            this.packetLoss = loss;
        else
            this.packetLoss = this.config.performanceUpdateStiffness * this.packetLoss
            + loss * (1 - this.config.performanceUpdateStiffness);
    }
};

export { AllowedTransfer, TrafficShaper, TrafficManager };
