import { performance } from 'perf_hooks';
import { promisify } from 'util';

const sleep = promisify(setTimeout);

import { assert } from 'console';

import { Config } from './config';
import { Logger } from './logger';
import { NetworkManager, AllowedTransfer, OutRequest } from './network';
import { DOWNLOAD_CHUNK_SIZE, PeerProxy } from './structures';
import { TelemetryFetchMessage, TelemetryFetchResponse } from './messages';

const TELEMETRY_CHECK_RATE = 0.1;
const UPSTREAM_TELEMETRY_CHECK_RATE = 0.12;

const QUEUE_DEPTH_LIMIT: number = 0.05;
const QUEUE_CHECK_RATE: number = 0.025;
const QUEUE_MESSAGE_LIMIT: number = 0.025;

interface TrafficManager {
    notifyDownload(
        actualBytes: number, wholeTransferBytes: number, isInRequest:boolean)
    : void;
    
    notifyLatency(latency: number): void;
    notifyPacketLoss(loss: number): void;

    updateBudgets(): void;
}

class TrafficShaper {
    bytesBudgeted: number;
    lastUpdate: number;

    maxSpeed: number; // bytes per second
    
    constructor(initialMaxSpeed: number) {
        this.bytesBudgeted = 0;
        this.lastUpdate = performance.now();

        this.maxSpeed = initialMaxSpeed;
    }

    updateBudget(): void {
        const now = performance.now();
        const deltaTime = (now - this.lastUpdate) / 1000;
        this.lastUpdate = now;

        this.bytesBudgeted = Math.max(
            0, this.bytesBudgeted - this.maxSpeed * deltaTime);
    }

    consume(bytes: number): void {
        this.bytesBudgeted += bytes;
    }
    
    public readyToGo(logger: Logger): boolean {
        logger.log(
            'traffic',
            `    RTG ${this.bytesBudgeted} <= ${QUEUE_DEPTH_LIMIT * this.maxSpeed}`);
        
        return this.bytesBudgeted <= QUEUE_DEPTH_LIMIT * this.maxSpeed;
    }
}

class TrafficManagerImpl implements TrafficManager {
    networkManager: NetworkManager;
    config: Config;
    
    upstream: TrafficShaper;
    requestedDownstream: TrafficShaper;
    observedDownstream: TrafficShaper;
    
    upstreamByteCounter: number = 0;
    upstreamBurstStart: number = 0;
    upstreamTelemetryBatchId: number = -1;
    upstreamHosts: Set<PeerProxy> = new Set();
    
    downstreamByteCounter: number = 0;
    downstreamBurstStart: number = 0;

    latency: number | null = null;
    packetLoss: number | null = null;

    requestQueue: OutRequest[] = [];
    processRequestQueueInterval: NodeJS.Timeout | null = null;
    
    constructor(networkManager: NetworkManager, config: Config) {
        this.networkManager = networkManager;
        this.config = config;
        
        this.upstream = new TrafficShaper(
            config.maxUpstreamSpeed);

        this.requestedDownstream = new TrafficShaper(
            config.maxDownstreamSpeed);

        this.observedDownstream = new TrafficShaper(
            config.maxDownstreamSpeed);
    }

    private async recoverTelemetry(peerProxy: PeerProxy,
                                   message: TelemetryFetchMessage)
    : Promise<void> {
        await sleep(UPSTREAM_TELEMETRY_CHECK_RATE - TELEMETRY_CHECK_RATE);

        try {
            for(let attempt = 0;
                attempt < this.config.telemetryFetchRetries;
                attempt++)
            {
                await this.networkManager.requestTransfer(peerProxy, message);
                const request = this.networkManager.newRequest(
                    peerProxy, message);
                const rawResponse = await request.getResponse();
                request.close();
                
                if (rawResponse == null)
                    continue;
                if (!(rawResponse instanceof TelemetryFetchResponse))
                    throw new Error('Wrong response type!');

                const response = rawResponse as TelemetryFetchResponse;
                this.upstream.maxSpeed +=
                    (1 - this.config.performanceUpdateStiffness)
                    * response.byteCount / TELEMETRY_CHECK_RATE;

                return;
            }

            this.networkManager.log(`Couldn't get telemetry from ${peerProxy.ip}:${peerProxy.port}!`);
        } catch (err) {
            this.networkManager.log(`Error in traffic: ${err}`);
        }
    }
    
    public async notifyDownload(actualBytes: number, wholeTransferBytes: number,
                                isInRequest: boolean)
    {
        const now = performance.now();

        this.observedDownstream.updateBudget();
        if (this.observedDownstream.bytesBudgeted <= 0) {
            this.downstreamBurstStart = now;
            this.downstreamByteCounter = 0;
        } else {
            // Note: "else" because we don't count the first packet's bytes
            // to avoid an off-by-one error
            this.downstreamByteCounter += actualBytes;
        }
        
        this.observedDownstream.consume(wholeTransferBytes);

        const burstLength = (now - this.downstreamBurstStart) / 1000;
        if (burstLength >= TELEMETRY_CHECK_RATE) {
            const bandwidth = this.downstreamByteCounter / burstLength;
            
            this.networkManager.log(
                `Update download estimation (${burstLength} burst)`);
            this.networkManager.log(
                `  byte counter is ${this.downstreamByteCounter}`);
            this.networkManager.log(
                `  Averaging in ${bandwidth}`);

            this.requestedDownstream.maxSpeed *=
                this.config.performanceUpdateStiffness;
            this.requestedDownstream.maxSpeed +=
                bandwidth * (1 - this.config.performanceUpdateStiffness);
            
            this.observedDownstream.maxSpeed =
                this.requestedDownstream.maxSpeed;

            // And again, we don't count the first packet's bytes
            this.downstreamBurstStart = now;
            this.downstreamByteCounter = 0;
        }

        if (isInRequest)
            this.requestedDownstream.consume(actualBytes);
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

    public updateBudgets(): void {
        this.requestedDownstream.updateBudget();
        
        this.upstream.updateBudget();
        if (this.upstream.bytesBudgeted <= 0) {
            this.upstreamTelemetryBatchId = this.networkManager.newTelemetryId();
            this.upstreamBurstStart = performance.now();
            this.upstreamHosts = new Set();
        }
    }

    public checkUploadTelemetry(): void {
        assert(this.upstreamBurstStart > 0,
               'checkUploadTelemetry() without budgets updated');
        
        const now = performance.now();
        const burstLength = (now - this.upstreamBurstStart) / 1000;
        
        if (burstLength >= UPSTREAM_TELEMETRY_CHECK_RATE) {
            this.upstream.maxSpeed *=
                this.config.performanceUpdateStiffness;

            const message = new TelemetryFetchMessage(
                this.upstreamTelemetryBatchId);
            
            for (let peerProxy of this.upstreamHosts)
                this.recoverTelemetry(peerProxy, message);

            this.upstreamBurstStart = 0;
        }
    }
};

export {
    AllowedTransfer, TrafficShaper, TrafficManager, TrafficManagerImpl,
    TELEMETRY_CHECK_RATE, UPSTREAM_TELEMETRY_CHECK_RATE,
    QUEUE_DEPTH_LIMIT, QUEUE_CHECK_RATE, QUEUE_MESSAGE_LIMIT,
};
