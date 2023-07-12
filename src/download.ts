import * as util from 'util';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as timers from 'timers';
import * as events from 'events';

import { assert } from 'console';

import { Config } from './config';
import { FileCache } from './filecache';
import { PotentialDownloadBurst, DownstreamManager } from './traffic';

import {
    CachedFile, FileRetrievalError, PeerProxy, DOWNLOAD_CHUNK_SIZE
} from "./structures";

import {
    DataRequestMessage, DataResponseElsewhereMessage, DataResponseOkMessage,
    DataResponseUnknownMessage,
    Message
} from "./messages";

import { NetworkingManager } from "./network";
import { ProxyManagerBase } from "./proxy";
import { UpstreamManager } from "./traffic";

const TRANSFER_ID_CHARACTERS =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';

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

class DownloadInProgress {
    downloadManager: DownloadManager;
    
    fileAddr: string;
    size: number;
    transferId: string;
    
    completionPromise: Promise<CachedFile>;
    resolveCompletion!: (value: CachedFile | PromiseLike<CachedFile>) => void;
    rejectCompletion!: (reason?: any) => void;

    outputFile: fs.promises.FileHandle | null;
    tempDownloadFile?: string;
    timerId: NodeJS.Timeout | null;
    
    activeBursts: DownloadBurst[];
    availHosts: Set<PeerProxy>;
    rejectedHosts: Set<PeerProxy>;
    //dhtHosts: Set<PeerProxy>;
    
    unreceivedOffsets: Set<number>;
    downloadOffset: number;
    
    constructor(downloadManager: DownloadManager, fileAddr: string) {
        this.downloadManager = downloadManager;
        this.fileAddr = fileAddr;
        const [hexHash, sizeStr] = fileAddr.split(':');
        this.size = parseInt(sizeStr);

        this.transferId = '';
        for (let i = 0; i < 8; i++)
            this.transferId += TRANSFER_ID_CHARACTERS.charAt(
                Math.floor(Math.random() * TRANSFER_ID_CHARACTERS.length));
            
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
        //this.dhtHosts = new Set();
    }

    log(msg: string) {
        this.downloadManager.proxyManager.logger.log(new Date(), this.transferId, msg);
    }        
    
    async run(seedProxies: PeerProxy[]) {
        this.log(`Run download for ${this.fileAddr}`);

        const randomSuffix = Math.floor(Math.random() * 1000000).toString();
        this.tempDownloadFile = `${this.downloadManager.proxyManager.config.tempDownloadDirectory}/${this.fileAddr}-${randomSuffix}`;
        this.outputFile = await fs.promises.open(this.tempDownloadFile, 'w');

        this.timerId = setInterval(
            () => this.checkBurstTimeouts(),
            this.downloadManager.proxyManager.config.downloadTickPeriod);

        for(let proxy of seedProxies) {
            if (proxy === this.downloadManager.proxyManager.thisProxy)
                this.rejectedHosts.add(proxy);
            else
                this.availHosts.add(proxy);
        }

        //this.log(`Ready; avail is ${this.availHosts.size}, dht is ${this.dhtHosts.size}`);
        
        while (this.outputFile && this.downloadOffset < this.size) {
            //const hosts = this.availHosts;
                //this.availHosts.size ? this.availHosts : this.dhtHosts;

            this.log(`Actual hosts is ${this.availHosts.size}`);

            this.log('download() -> downloadFrom()');
            await this.downloadFrom(this.availHosts);
        }

        this.log('  All initiated');
    }

    async downloadFrom(hosts: Set<PeerProxy>): Promise<void> {
        this.log(`  Iter downloadFrom() from ${hosts.size} hosts`);
        
        if (this.downloadOffset >= this.size || !this.outputFile) {
            this.log('    Early bail 1');
            return;
        }

        let bursts =
            await this.downloadManager.downstreamManager.requestDownload(
                Array.from(hosts), this.size - this.downloadOffset);

        if (this.downloadOffset >= this.size || !this.outputFile) {
            this.log('    Early bail 2');
            return;
        }
        assert(bursts !== null, 'Null from queued requestDownload()');
        if (bursts!.length <= 0) {
            this.stop('No available hosts from downstreamManager');
            return;
        }
        
        this.log(`    We have ${bursts!.length} potential bursts`);
        
        for (let potentialBurst of bursts!) {            
            if (this.downloadOffset >= this.size)
                break;

            let burstLength = potentialBurst.bytesAllowed;
            burstLength = DOWNLOAD_CHUNK_SIZE * Math.ceil(
                burstLength / DOWNLOAD_CHUNK_SIZE);
            
            if (burstLength > this.size - this.downloadOffset)
                burstLength = this.size - this.downloadOffset;

            this.log(`      We create burst for ${this.downloadOffset}[${burstLength}] from ${potentialBurst.source.ip}:${potentialBurst.source.port}`);
            
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
        }

        this.log('    Done creating real bursts.');
    }
    
    handlePossibleFailure() {
        if (this.outputFile)
            if (this.availHosts.size === 0) // && this.dhtHosts.size === 0)
                this.stop('No available or DHT hosts remaining.');
    }
    
    async stop(failureMessage: string | null = null): Promise<void> {
        if (!this.outputFile)
            return; // Already stopped

        if (this.timerId) {
            clearInterval(this.timerId);
            this.timerId = null;
        }

        try {
            const outputFile = this.outputFile;
            this.outputFile = null;
            await outputFile.close();
            
            if (failureMessage) {
                this.rejectCompletion(failureMessage);
            } else {
                const cachedFile =
                    await this.downloadManager.proxyManager.fileCache.addFile(
                        this.tempDownloadFile!,
                        this.fileAddr, true, false);
                this.resolveCompletion(cachedFile);
            }
        } catch(err) {
            this.rejectCompletion(err);
        }
    }
    
    async checkBurstTimeouts() {
        let currentTime = Date.now();

        assert(this.tempDownloadFile,
               'checkBurstTimeouts() when burst finished');

        let burstsToRemove: DownloadBurst[] = [];
        
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
                this.log(`  Finish ${this.fileAddr}@${burst.offset}[${burst.length}]`);
                if (isTimedOut)
                    this.log(`    Timeout!`);

                burstsToRemove.push(burst);
            }
        }

        this.activeBursts = this.activeBursts.filter(
            burst => !burstsToRemove.includes(burst));

        for(let burst of burstsToRemove) {                
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
        }

        // TODO: We should report metrics to the
        // DownstreamManager for the completed burst, so it can make
        // good decisions about where to request more bursts from
    }
    
    requestBurst(burst: DownloadBurst) {
        console.log(`Requesting ${this.fileAddr}@${burst.offset}[${burst.length}] from ${burst.source.ip}:${burst.source.port} ${this.transferId}`);
        
        const packet = new DataRequestMessage(
            burst.burstId, burst.fileAddr, burst.offset, burst.length,
            this.transferId);

        this.downloadManager.proxyManager.networkingManager.send(
            packet, burst.source.ip, burst.source.port);
    }

    async handleDataResponseOk(source: PeerProxy,
                               message: DataResponseOkMessage)
    {
        if (!this.outputFile)
            return;

        this.log(`Got OK ${this.fileAddr}@${message.offset}[${message.length}] from ${source.ip}:${source.port}`);
        
        if (this.unreceivedOffsets.has(message.offset)) {
            assert(message.offset + message.length <= this.size, 
                   'Received offset + length is greater than the file size');
            assert(message.length <= DOWNLOAD_CHUNK_SIZE,
                   `Received chunk size ${message.length} is greater than DOWNLOAD_CHUNK_SIZE`);

            try {
                await this.outputFile!.write(
                    message.data,
                    0, message.length, message.offset);
            } catch(err) {
                this.stop(`${err}`);
            }
            
            this.unreceivedOffsets.delete(message.offset);
        }
        
        // If all chunks have been received, close the file, add it
        // to the cache, and resolve the promise
        if (this.unreceivedOffsets.size === 0)
            this.stop();
    }

    async handleDataResponseUnknown(source: PeerProxy,
                                    message: DataResponseUnknownMessage)
    {
        this.log(`Got Unk ${this.fileAddr} from ${source.ip}:${source.port}`);
        
        if (!this.outputFile) {
            this.log('Bail early');
            return;
        }
            
        this.availHosts.delete(source);
        this.rejectedHosts.add(source);
        this.handlePossibleFailure();
    }

    async handleDataResponseElsewhere(source: PeerProxy,
                                      message: DataResponseElsewhereMessage)
    {
        this.log(`Got elsewhere ${this.fileAddr} from ${source.ip}:${source.port}`);
        
        if (!this.outputFile) {
            this.log('  Bail early');
            return;
        }

        this.availHosts.delete(source);
        //this.dhtHosts.delete(source);
        this.rejectedHosts.add(source);

        const newHosts = new Set<PeerProxy>();
        for (const {ip, port} of message.nodeInfo) {
            const newHost = this.downloadManager.proxyManager.getPeerProxy(
                ip, port);
            if(!newHost) {
                this.log(`Unknown proxy: ${ip}:${port}`);
                continue;
            }

            if (!this.availHosts.has(newHost)
                && !this.rejectedHosts.has(newHost)
                && newHost != this.downloadManager.proxyManager.thisProxy)
            {
                this.log(`  Adding ${newHost.ip}:${newHost.port}`);
                this.availHosts.add(newHost);
                newHosts.add(newHost);
            } else {
                this.log(`  Skipping ${newHost.ip}:${newHost.port}`);
            }
        }

        this.log('Data response elsewhere; downloadFrom()');
        if (newHosts.size > 0)
            await this.downloadFrom(newHosts);
    }
}

class DownloadManager {
    proxyManager: ProxyManagerBase;
    downstreamManager: DownstreamManager;

    nextBurstId: number;
    
    private activeDownloads: Map<string, DownloadInProgress>;

    constructor(proxyManager: ProxyManagerBase) {
        this.proxyManager = proxyManager;
        this.downstreamManager = new DownstreamManager(proxyManager.config);
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
        downloadInProgress = new DownloadInProgress(this, fileAddr);
        downloadInProgress.log(`Starting new download for ${fileAddr}`);
        this.activeDownloads.set(fileAddr, downloadInProgress);

        const seedProxies = this.proxyManager.blobFinder.getClosestProxies(
            fileAddr, this.proxyManager.config.dhtNotifyNumber);
        downloadInProgress.log(`Got ${seedProxies.length} seed proxies`);
        downloadInProgress.run(seedProxies);

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
            this.proxyManager.logger.log(
                new Date(), 'download', 'Dispatch elsewhere msg');
            
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
