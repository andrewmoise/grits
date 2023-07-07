import * as util from 'util';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as timers from 'timers';
import * as events from "events";

import { assert } from 'console';

import { Config } from "./config";
import { FileCache } from "./filecache";
import { CachedFile, FileRetrievalError } from "./structures";
import {
    DataRequestMessage, DataResponseElsewhereMessage, DataResponseOkMessage, DataResponseUnknownMessage,
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

class DownloadBurst {
    burstId: number;
    source: PeerProxy;
    bytesAllowed: number;
    
    fileAddr?: string;
    offset?: number;
    length?: number;

    requestTimestamp: number;
    firstPacketTimestamp?: number;
    lastPacketTimestamp?: number;
    lastPacketOffset?: number;
    tickByteCount?: number;
    
    constructor(burstId: number, source: PeerProxy, bytesAllowed: number) {
        this.burstId = burstId;
        this.source = source;
        this.bytesAllowed = bytesAllowed;

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
    : Promise<DownloadBurst[]>
    {
        const bytesBudget = this.config.maxDownstreamSpeed / 1000
            * this.config.downloadTickPeriod;
        bytesBudget -= this.bytesRequestedThisTick;

        if (bytesBudget > 0) {
            if (bytesRequested > bytesBudget)
                bytesRequested = bytesBudget;

            this.bytesRequestedThisTick += bytesRequested;

            const burstsPerPeer = Math.ceil(
                bytesRequested / DOWNLOAD_CHUNK_SIZE / peerProxies.length);
            
            const downloadBursts: DownloadBurst[] = [];
            
            for (let i = 0; i < peerProxies.length; i++) {
                downloadBursts.push(new DownloadBurst(
                    Date.now(), peerProxies[i], burstsPerPeer * DOWNLOAD_CHUNK_SIZE));
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

            if (bursts) {
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

    outputFile: fs.promises.FileHandle;
    tempDownloadFile: string;
    timerId?: NodeJS.Timeout;
    
    activeBursts: DownloadBurst[];
    
    unreceivedOffsets: Set<number>;

    availHosts: Set<PeerProxy>;
    rejectedHosts: Set<PeerProxy>;
    dhtHosts: Set<PeerProxy>;
    
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

        this.activeBursts = [];
        
        this.tempDownloadFile = this.createTempFile();

        this.unreceivedOffsets = new Set(
            Array.from(
                { length: Math.ceil(this.size / DOWNLOAD_CHUNK_SIZE) },
                (_, i) => i * DOWNLOAD_CHUNK_SIZE
            )
        );
        
        this.availHosts = new Set();
        this.rejectedHosts = new Set();
        this.dhtHosts = new Set();

        this.shouldContinue = true;
    }

    async createTempFile() {
        const randomSuffix = Math.floor(Math.random() * 1000000).toString();
        const tempFilePath = `${this.downloadManager.config.tempDownloadDirectory}/${this.fileAddr}-${randomSuffix}`;
        this.outputFile = await fs.promises.open(tempFilePath, 'w');
        return tempFilePath;
    }

    async download() {
        let offset = 0;
        let remaining = this.size;

        this.timerId = setInterval(
            () => this.checkBurstTimeouts(),
            this.downloadManager.config.downloadTickPeriod);
        
        while (remaining > 0) {
            let length = Math.min(this.downloadManager.config.downloadChunkSize,
                                  remaining);

            let bursts =
                await this.downloadManager.downstreamManager.requestDownload(
                    Array.from(this.availHosts), length);

            if (!this.shouldContinue)
                break;
            
            for (let burst of bursts) {
                burst.fileAddr = this.fileAddr;
                burst.offset = offset;
                burst.length = Math.min(burst.bytesAllowed, length);

                this.requestBurst(burst);
                this.activeBursts.push(burst);

                offset += burst.length;
                remaining -= burst.length;
            }
        }
    }

    stopDownload() {
        this.shouldContinue = false;

        if (this.timerId !== null) {
            clearInterval(this.timerId);
            this.timerId = null;
        }
    }
    
    async checkBurstTimeouts() {
        let currentTime = Date.now();

        for (let burst of this.activeBursts) {
            // Check if burst has timed out
            const isTimedOut = burst.requestTimestamp
                + this.downloadManager.config.burstTimeout
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

                for (let offset of missingOffsets) {
                    let newBursts = await
                        this.downloadManager.downstreamManager.requestDownload(
                            Array.from(this.availHosts), DOWNLOAD_CHUNK_SIZE);
                    
                    for (let newBurst of newBursts) {
                        newBurst.fileAddr = this.fileAddr;
                        newBurst.offset = offset;
                        newBurst.length = DOWNLOAD_CHUNK_SIZE;
                        
                        this.requestBurst(newBurst);
                        this.activeBursts.push(newBurst);
                    }
                }

                // Finally, we should report metrics back to the
                // DownstreamManager and remove the burst
                this.downloadManager.downstreamManager.closeBurst(
                    burst, missingOffsets.length);
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
        
        if (this.unreceivedOffsets.has(message.offset)) {
            assert(message.offset + message.length <= this.size, 
                   'Received offset + length is greater than the file size');
            assert(message.length <= DOWNLOAD_CHUNK_SIZE,
                   'Received chunk size is greater than DOWNLOAD_CHUNK_SIZE');

            await this.outputFile.write(
                message.data,
                0, message.length, message.offset);
        
            this.unreceivedOffsets.delete(message.offset);
        }
        
        // If all chunks have been received, close the file, add it
        // to the cache, and resolve the promise
        if (this.unreceivedOffsets.size === 0) {
            await this.outputFile.close();
            const cachedFile =
                await this.downloadManager.proxyManager.fileCache.addFile(
                    this.tempDownloadFile,
                    this.fileAddr, true, false);
            this.resolveCompletion(cachedFile);
        }
    }

    async handleDataResponseUnknown(source: PeerProxy,
                                    message: DataResponseUnknownMessage)
    {
        this.availHosts.delete(source);
        this.rejectedHosts.add(source);
    }

    handleDataResponseElsewhere(source: PeerProxy,
                                message: DataResponseElsewhereMessage)
    {
        this.downloadManager.notifyDataElsewhere(this, message);

        this.availHosts.delete(source);
        this.dhtHosts.delete(source);
        this.rejectedHosts.add(source);
    }
}


class DownloadManager {
    private proxyManager: ProxyManagerBase;

    private activeDownloads: Map<string, DownloadInProgress>;

    constructor(proxyManager: ProxyManagerBase) {
        this.proxyManager = proxyManager;
        this.activeDownloads = new Map();
    }

    async download(fileAddr: string): Promise<CachedFile> {
        // If a download already exists for the given file, return its promise.
        let downloadInProgress = this.activeDownloads.get(fileAddr);

        if (downloadInProgress) {
            return downloadInProgress.completionPromise;
        }

        // Otherwise, create a new DownloadInProgress and return its promise.
        console.log("Starting new download for", fileAddr);
        downloadInProgress = new DownloadInProgress(this, fileAddr);
        this.activeDownloads.set(fileAddr, downloadInProgress);

        const seedProxies = this.proxyManager.blobFinder.getClosestProxies(
            fileAddr, this.proxyManager.config.dhtNotifyNumber);
        downloadInProgress.download(seedProxies);

        return downloadInProgress.completionPromise;
    }

    handleDataResponseOk(source: PeerProxy, message: Message) {
        if (!(message instanceof DataResponseOkMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseElsewhereMessage =
            message as DataResponseElsewhereMessage;
        const download = this.activeDownloads.get(message.fileAddr);

        if (download) {
            download.handleDataResponseOk(source, message);
        } else {
            console.warn(`Received DataResponseOk for unknown fileAddr ${message.fileAddr}`);
        }
    }

    handleDataResponseUnknown(source: PeerProxy, message: DataResponseUnknownMessage) {
        if (!(message instanceof DataResponseElsewhereMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseElsewhereMessage =
            message as DataResponseElsewhereMessage;
        const download = this.activeDownloads.get(message.fileAddr);

        if (download) {
            download.handleDataResponseUnknown(source, message);
        } else {
            console.warn(`Received DataResponseUnknown for unknown fileAddr ${message.fileAddr}`);
        }
    }

    handleDataResponseElsewhere(source: PeerProxy, message: DataResponseElsewhereMessage) {
        if (!(message instanceof DataResponseElsewhereMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseElsewhereMessage =
            message as DataResponseElsewhereMessage;
        const download = this.activeDownloads.get(message.fileAddr);

        if (download) {
            download.handleDataResponseElsewhere(source, message);
        } else {
            console.warn(`Received DataResponseElsewhere for unknown fileAddr ${message.fileAddr}`);
        }
    }
}



export {
    DownloadManager,
};
