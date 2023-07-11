import * as util from 'util';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as timers from 'timers';
import * as events from "events";

import { assert } from 'console';

import { Config } from "./config";
import { FileCache } from "./filecache";
import { CachedFile, FileRetrievalError, PeerProxy } from "./structures";
import {
    DataRequestMessage, DataResponseElsewhereMessage, DataResponseOkMessage,
    DataResponseUnknownMessage,
    Message
} from "./messages";
import { NetworkingManager } from "./network";
import { ProxyManagerBase } from "./proxy";
import { UpstreamManager } from "./traffic";

const DOWNLOAD_CHUNK_SIZE = 1400;

class TimeoutError extends Error {
    constructor() {
        super("Timeout");
        this.name = "TimeoutError";
    }
}

interface PotentialDownloadBurst {
    source: PeerProxy;
    bytesAllowed: number;
}

class DownloadBurst {
    burstId: number;
    source: PeerProxy;
    bytesAllowed: number;
    
    fileAddr: string;
    offset: number;
    length: number;

    requestTimestamp: number;
    firstPacketTimestamp?: number;
    lastPacketTimestamp?: number;
    lastPacketOffset?: number;
    tickByteCount?: number;
    
    constructor(downloadManager: DownloadManager, source: PeerProxy,
                bytesAllowed: number,
                fileAddr: string, offset: number, length: number)
    {
        this.burstId = downloadManager.nextBurstId++;
        this.source = source;
        this.bytesAllowed = bytesAllowed;
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
        this.requestTimestamp = Date.now();
    }
}


class DownstreamManager {
    private downloadManager: DownloadManager;
    private config: Config;
    
    private bytesRequestedThisTick: number;
    private bytesReceivedThisTick: number;
    
    private downloadQueue: {
        resolve: Function,
        peerProxies: PeerProxy[],
        bytesRequested: number,
    }[];

    constructor(downloadManager: DownloadManager, config: Config) {
        this.downloadManager = downloadManager;
        this.config = config;

        this.bytesRequestedThisTick = 0;
        this.bytesReceivedThisTick = 0;
        this.downloadQueue = [];

        setInterval(() => this.tick(), config.downloadTickPeriod);
    }

    async requestDownload(
        peerProxies: PeerProxy[],
        bytesRequested: number, queueOnFailure=true)
    : Promise<PotentialDownloadBurst[] | null>
    {
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

            return downloadBursts;
        } else if (queueOnFailure) {
            // Too many bytes requested this tick, queue up the request
            // and return a promise.
            return new Promise((resolve) => {
                this.downloadQueue.push({
                    resolve, peerProxies, bytesRequested });
            });
        } else {
            return null;
        }
    }

    private tick() {
        // Reset byte count for this tick.
        this.bytesRequestedThisTick = 0;
        this.bytesReceivedThisTick = 0;

        // Process queued downloads.
        while (this.downloadQueue.length > 0) {
            const { resolve, peerProxies, bytesRequested } =
                this.downloadQueue[0];

            const bursts = this.requestDownload(peerProxies, bytesRequested,
                                                false);

            if (bursts !== null) {
                this.downloadQueue.shift();
                resolve(bursts);
            } else {
                break;
            }
        }
    }
}


class DownloadInProgress {
    fileAddr: string;
    size: number;
    
    completionPromise: Promise<CachedFile>;
    resolveCompletion!: (value: CachedFile | PromiseLike<CachedFile>) => void;
    rejectCompletion!: (reason?: any) => void;

    outputFile: fs.promises.FileHandle | null;
    tempDownloadFile?: string;
    timerId: NodeJS.Timeout | null;
    
    activeBursts: DownloadBurst[];
    availHosts: Set<PeerProxy>;
    rejectedHosts: Set<PeerProxy>;
    dhtHosts: Set<PeerProxy>;
    
    unreceivedOffsets: Set<number>;
    downloadOffset: number;
    
    downloadManager: DownloadManager;

    shouldContinue: boolean;
    
    constructor(downloadManager: DownloadManager, fileAddr: string) {
        this.downloadManager = downloadManager;
        this.fileAddr = fileAddr;
        const [hexHash, sizeStr] = fileAddr.split(':');
        this.size = parseInt(sizeStr);

        this.completionPromise = new Promise((resolve, reject) => {
            this.resolveCompletion = resolve;
            this.rejectCompletion = reject;
        });

        this.outputFile = null;
        this.timerId = null;
        this.activeBursts = [];

        this.unreceivedOffsets = new Set(
            Array.from(
                { length: Math.ceil(this.size / DOWNLOAD_CHUNK_SIZE) },
                (_, i) => i * DOWNLOAD_CHUNK_SIZE
            )
        );

        this.downloadOffset = 0;
        
        this.availHosts = new Set();
        this.rejectedHosts = new Set();
        this.dhtHosts = new Set();

        this.shouldContinue = true;
    }

    async createTempFile() {
        const randomSuffix = Math.floor(Math.random() * 1000000).toString();
        const tempFilePath = `${this.downloadManager.proxyManager.config.tempDownloadDirectory}/${this.fileAddr}-${randomSuffix}`;
        this.outputFile = await fs.promises.open(tempFilePath, 'w');
        return tempFilePath;
    }

    async download(seedProxies: PeerProxy[]) {
        this.downloadManager.proxyManager.logger.log(
            new Date(), 'download',
            `Run download for ${this.fileAddr}`);

        this.tempDownloadFile = await this.createTempFile();
        
        this.timerId = setInterval(
            () => this.checkBurstTimeouts(),
            this.downloadManager.proxyManager.config.downloadTickPeriod);

        for(let proxy of seedProxies) {
            if (proxy == this.downloadManager.proxyManager.thisProxy)
                this.rejectedHosts.add(proxy);
            else
                this.dhtHosts.add(proxy);
        }
        
        while (this.shouldContinue && this.downloadOffset < this.size) {
            const hosts = this.availHosts
                this.availHosts.size ? this.availHosts : this.dhtHosts;

            this.downloadManager.proxyManager.logger.log(
                new Date(), 'download',
                'download() -> downloadFrom()');
            await this.downloadFrom(hosts);
        }

        this.downloadManager.proxyManager.logger.log(
            new Date(), 'download',
            '  All done');
    }

    async downloadFrom(hosts: Set<PeerProxy>) {
        this.downloadManager.proxyManager.logger.log(
            new Date(), 'download',
            '  Iter downloadFrom()');

        if (this.downloadOffset >= this.size || !this.shouldContinue) {
            this.downloadManager.proxyManager.logger.log(
                new Date(), 'download',
                '    Early bail tho');
            return;
        }

        let bursts =
            await this.downloadManager.downstreamManager.requestDownload(
                Array.from(hosts), this.size - this.downloadOffset);

        assert(bursts !== null, 'Null from queued requestDownload()');
        
        if (!this.shouldContinue) {
            this.downloadManager.proxyManager.logger.log(
                new Date(), 'download',
                '    Secondary early bail');
            return;
        }

        this.downloadManager.proxyManager.logger.log(
            new Date(), 'download',
            `    We have ${bursts!.length} potential bursts`);
        
        for (let potentialBurst of bursts!) {
            let burstLength = potentialBurst.bytesAllowed;
            burstLength = DOWNLOAD_CHUNK_SIZE * Math.ceil(
                burstLength / DOWNLOAD_CHUNK_SIZE);
            
            if (burstLength > this.size - this.downloadOffset)
                burstLength = this.size - this.downloadOffset;

        this.downloadManager.proxyManager.logger.log(
            new Date(), 'download',
            `      We create burst for ${this.downloadOffset}[${burstLength}] from ${potentialBurst.source.ip}:${potentialBurst.source.port}`);
            
            let burst = new DownloadBurst(
                this.downloadManager,
                potentialBurst.source, 
                burstLength,
                this.fileAddr,
                this.downloadOffset, 
                burstLength
            );

            this.downloadOffset += burst.length;
            
            this.requestBurst(burst);
            this.activeBursts.push(burst);

            if (this.downloadOffset >= this.size)
                break;
        }

        this.downloadManager.proxyManager.logger.log(
            new Date(), 'download',
            '    Done creating real bursts.');
    }
    
    handlePossibleFailure() {
        if (this.shouldContinue) {
            if (this.availHosts.size === 0 && this.dhtHosts.size === 0) {
                this.stopDownload();
                this.rejectCompletion('No available or DHT hosts remaining.');
            }
        }
    }
    
    stopDownload() {
        this.shouldContinue = false;

        if (this.timerId) {
            clearInterval(this.timerId);
            this.timerId = null;
        }
    }
    
    async checkBurstTimeouts() {
        let currentTime = Date.now();

        for (let burst of this.activeBursts) {
            // Check if burst has timed out
            const isTimedOut = burst.requestTimestamp
                + this.downloadManager.proxyManager.config.burstTimeout
                < currentTime;

            // Also check if the last packet for this burst has been received
            let isBurstComplete = !this.unreceivedOffsets.has(
                burst.offset + burst.length - DOWNLOAD_CHUNK_SIZE);

            // If the burst has timed out or all packets have been received,
            // re-request missing offsets
            
            if (isTimedOut || isBurstComplete) {
                let missingOffsets = [];
                for (let i = burst.offset;
                     i < burst.offset + burst.length;
                     i += DOWNLOAD_CHUNK_SIZE)
                {
                    if (this.unreceivedOffsets.has(i))
                        missingOffsets.push(i);
                }

                for (const offset of missingOffsets) {
                    const length = Math.min(DOWNLOAD_CHUNK_SIZE,
                                            this.size - offset);

                    const potentialBursts = await
                        this.downloadManager.downstreamManager.requestDownload(
                            Array.from(this.availHosts), length);
                    assert(potentialBursts !== null,
                           'Null from queued requestDownload()');

                    for (let potentialBurst of potentialBursts!) {
                        const newBurst: DownloadBurst = new DownloadBurst(
                            this.downloadManager,
                            potentialBurst.source,
                            potentialBurst.bytesAllowed,
                            this.fileAddr,
                            offset,
                            length);
                        
                        this.requestBurst(newBurst);
                        this.activeBursts.push(newBurst);
                        break;
                    }
                }

                // TODO: We should report metrics to the
                // DownstreamManager for the completed burst, so it can make
                // good decisions about where to request more bursts from

                this.activeBursts = this.activeBursts.filter(b => b !== burst);
            }
        }
    }
    
    requestBurst(burst: DownloadBurst) {
        const packet = new DataRequestMessage(
            burst.burstId, burst.fileAddr, burst.offset, burst.length);

        this.downloadManager.proxyManager.networkingManager.send(
            packet, burst.source.ip, burst.source.port);
    }

    async handleDataResponseOk(source: PeerProxy,
                               message: DataResponseOkMessage)
    {
        if (!this.shouldContinue)
            return;

        assert(this.outputFile, 'Output file undefined');
        
        if (this.unreceivedOffsets.has(message.offset)) {
            assert(message.offset + message.length <= this.size, 
                   'Received offset + length is greater than the file size');
            assert(message.length <= DOWNLOAD_CHUNK_SIZE,
                   'Received chunk size is greater than DOWNLOAD_CHUNK_SIZE');
            
            await this.outputFile!.write(
                message.data,
                0, message.length, message.offset);
        
            this.unreceivedOffsets.delete(message.offset);
        }
        
        // If all chunks have been received, close the file, add it
        // to the cache, and resolve the promise
        if (this.unreceivedOffsets.size === 0) {
            await this.outputFile!.close();
            this.outputFile = null;
            const cachedFile =
                await this.downloadManager.proxyManager.fileCache.addFile(
                    this.tempDownloadFile!,
                    this.fileAddr, true, false);
            this.resolveCompletion(cachedFile);
        }
    }

    async handleDataResponseUnknown(source: PeerProxy,
                                    message: DataResponseUnknownMessage)
    {
        this.availHosts.delete(source);
        this.rejectedHosts.add(source);
        this.handlePossibleFailure();
    }

    async handleDataResponseElsewhere(source: PeerProxy,
                                      message: DataResponseElsewhereMessage)
    {
        this.availHosts.delete(source);
        this.dhtHosts.delete(source);
        this.rejectedHosts.add(source);

        const newHosts = new Set<PeerProxy>();
        for (const {ip, port} of message.nodeInfo) {
            const newHost = this.downloadManager.proxyManager.getPeerProxy(
                ip, port);
            if(!newHost) {
                this.downloadManager.proxyManager.logger.log(
                    new Date(), 'download',
                    `Unknown proxy: ${ip}:${port}`);
                continue;
            }

            if (!this.availHosts.has(newHost)
                && !this.rejectedHosts.has(newHost)
                && newHost != this.downloadManager.proxyManager.thisProxy)
            {
                newHosts.add(newHost);
            }
        }

        this.downloadManager.proxyManager.logger.log(
            new Date(), 'download',
            'Data response elsewhere; downloadFrom()');
        await this.downloadFrom(newHosts);
        
        for(const newHost of newHosts)
            if (!this.rejectedHosts.has(newHost))
                this.availHosts.add(newHost);
    }
}

class DownloadManager {
    proxyManager: ProxyManagerBase;
    downstreamManager: DownstreamManager;

    nextBurstId: number;
    
    private activeDownloads: Map<string, DownloadInProgress>;

    constructor(proxyManager: ProxyManagerBase) {
        this.proxyManager = proxyManager;
        this.downstreamManager = new DownstreamManager(
            this, proxyManager.config);
        this.nextBurstId = 0;
        this.activeDownloads = new Map();
    }

    async download(fileAddr: string): Promise<CachedFile> {
        this.proxyManager.logger.log(
            new Date(), 'download',
            `DownloadManager.download(${fileAddr})`);
        
        // If a download already exists for the given file, return its promise.
        let downloadInProgress = this.activeDownloads.get(fileAddr);

        if (downloadInProgress) {
            return downloadInProgress.completionPromise;
        }

        // Otherwise, create a new DownloadInProgress and return its promise.
        this.proxyManager.logger.log(
            new Date(), 'download',
            `Starting new download for ${fileAddr}`);
        downloadInProgress = new DownloadInProgress(this, fileAddr);
        this.activeDownloads.set(fileAddr, downloadInProgress);

        const seedProxies = this.proxyManager.blobFinder.getClosestProxies(
            fileAddr, this.proxyManager.config.dhtNotifyNumber);
        downloadInProgress.download(seedProxies);

        this.proxyManager.logger.log(
            new Date(), 'download',
            '  Return promise')
        
        return downloadInProgress.completionPromise;
    }

    handleDataResponseOk(host: string, port: number, rawMessage: Message) {
        if (!(rawMessage instanceof DataResponseOkMessage))
            throw new Error("Data request of wrong TS type!");
        const message =
            rawMessage as DataResponseOkMessage;
        const download = this.activeDownloads.get(message.fileAddr);

        const source = this.proxyManager.getPeerProxy(host, port);
        if (!source) {
            this.proxyManager.logger.log(
                new Date(), 'download',
                `No PeerProxy found for ${host}:${port}`);
            return;
        }
        
        if (download) {
            download.handleDataResponseOk(source, message);
        } else {
            this.proxyManager.logger.log(
                new Date(), 'download',
                `Received DataResponseOk for unknown fileAddr ${message.fileAddr}`);
        }
    }

    handleDataResponseUnknown(host: string, port: number, rawMessage: Message) {
        if (!(rawMessage instanceof DataResponseUnknownMessage))
            throw new Error("Data request of wrong TS type!");
        const message =
            rawMessage as DataResponseUnknownMessage;
        const download = this.activeDownloads.get(message.fileAddr);

        const source = this.proxyManager.getPeerProxy(host, port);
        if (!source) {
            this.proxyManager.logger.log(
                new Date(), 'download',
                `No PeerProxy found for ${host}:${port}`);
            return;
        }
        
        if (download) {
            download.handleDataResponseUnknown(source, message);
        } else {
            this.proxyManager.logger.log(
                new Date(), 'download',
                `Received DataResponseUnknown for unknown fileAddr ${message.fileAddr}`);
        }
    }

    handleDataResponseElsewhere(host: string, port: number,
                                rawMessage: Message)
    {
        if (!(rawMessage instanceof DataResponseElsewhereMessage))
            throw new Error("Data request of wrong TS type!");
        const message =
            rawMessage as DataResponseElsewhereMessage;
        const download = this.activeDownloads.get(message.fileAddr);

        const source = this.proxyManager.getPeerProxy(host, port);
        if (!source) {
            this.proxyManager.logger.log(
                new Date(), 'download',
                `No PeerProxy found for ${host}:${port}`);
            return;
        }
        
        if (download) {
            download.handleDataResponseElsewhere(source, message);
        } else {
            this.proxyManager.logger.log(
                new Date(), 'download',
                `Received DataResponseElsewhere for unknown fileAddr ${message.fileAddr}`);
        }
    }
}



export {
    DownloadManager,
};
