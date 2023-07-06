import * as util from 'util';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as timers from 'timers';
import * as events from "events";

import { Config } from "./config";
import { FileCache } from "./filecache";
import { CachedFile, FileRetrievalError } from "./structures";
import {
    DataRequestMessage, DataResponseOkMessage, DataResponseUnknownMessage,
    Message
} from "./messages";
import { NetworkingManager } from "./network";
import { ProxyManagerBase } from "./proxy";
import { TrafficManager } from "./traffic";

const DOWNLOAD_CHUNK_SIZE = 1400;

class TimeoutError extends Error {
    constructor() {
        super("Timeout");
        this.name = "TimeoutError";
    }
}

class DownloadBurst {
    burstId: number;
    fileAddr: string;
    offset: number;
    length: number;

    firstPacketTimestamp?: number;
    lastPacketTimestamp?: number;
    lastPacketOffset: number;
    tickByteCount: number;
    
    constructor(burstId, fileAddr, offset, length) {
        this.burstId = burstId;
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
        
        this.nextPacketOffset = 0; // needs to be initialized
        this.firstPacketTimestamp = null;
        this.lastPacketTimestamp = null;
        this.tickByteCount = 0;
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

    unrequestedOffsets: number[];
    unreceivedOffsets: Set<number>;

    availHosts: Set<PeerProxy>;
    rejectedHosts: Set<PeerProxy>;
    dhtHosts: Set<PeerProxy>;
    
    constructor(fileAddr) {
        this.fileAddr = fileAddr;
        const [hexHash, sizeStr] = fileAddr.split(':');
        this.size = parseInt(sizeStr);

        this.completionPromise = new Promise((resolve, reject) => {
            this.resolveCompletion = resolve;
            this.rejectCompletion = reject;
        });

        this.tempDownloadFile = this.createTempFile();
        
        this.unrequestedOffsets = Array.from(
            { length: Math.ceil(this.size / DOWNLOAD_CHUNK_SIZE) },
            (_, i) => i * DOWNLOAD_CHUNK_SIZE
        );
        this.unreceivedOffsets = new Set(this.unrequestedOffsets);

        this.availHosts = new Set();
        this.rejectedHosts = new Set();
        this.dhtHosts = new Set();
        
        this.lastPacketTimestamp = null; // Will be set to Date.now() when a packet is received.
    }

    async createTempFile() {
        const randomSuffix = Math.floor(Math.random() * 1000000).toString();
        const tempFilePath = `${this.proxyManager.config.tempDownloadDirectory}/${this.fileAddr}-${randomSuffix}`;
        this.outputFile = await fs.promises.open(tempFilePath, 'w');
        return tempFilePath;
    }

}

class DownloadManager {
    private proxyManager: ProxyManagerBase;
    private tickIntervalId|null: NodeJS.Timeout;

    private activeDownloads: Map<string, DownloadInProgress>;
    private activeBursts: Map<number, DownloadBurst>;

    newBurstIndex: number;

    bytesBudget: number;
    bytesRequested: number;
    bytesReceived: number;
    
    constructor(proxyManager) {
        this.proxyManager = proxyManager;
        this.tickIntervalId = null;
        this.activeDownloads = new Map();
        this.activeBursts = new Map();
        this.newBurstIndex = 0;

        this.bytesBudget = 10240;
        this.bytesRequested = 0;
        this.bytesReceived = 0;
    }

    async download(fileAddr) {
        let downloadInProgress = this.activeDownloads.get(fileAddr);
        
        if (downloadInProgress) {
            return downloadInProgress.completionPromise;
        } else {
            console.log("Need to start new download.");
            const seedProxies = this.proxyManager.blobFinder.getClosestProxies(
                fileAddr, this.proxyManager.config.dhtNotifyNumber);
            downloadInProgress = new DownloadInProgress(fileAddr);
            this.activeDownloads.set(fileAddr, downloadInProgress);

            for(const seed of seedProxies) {
                downloadInProgress.dhtHosts.add(seed);

                const length = min(DOWNLOAD_CHUNK_SIZE,
                                   downloadInProgress.length);
            
                const burstId = this.newBurstIndex++;
                const packet = new DataRequestMessage(
                    burstId, fileAddr, 0, length);
                this.proxyManager.networkingManager.send(
                    packet, seed.ip, seed.port);

                const burst = new DownloadBurst(burstId, fileAddr, 0, length);
                this.activeBursts.set(burst.burstId, burst);
            }
            return downloadInProgress.completionPromise;
        }
    }

    tick() {
        const now = Date.now();
        const burstTimeout = this.proxyManager.config.burstTimeout;

        const bytesQueued: Map<string, number> = new Map();

        this.bytesRequested = 0;
        this.bytesReceived = 0;

        for (const [burstId, burst] of this.activeBursts.entries()) {
            if (burst.lastPacketTimestamp < now - burstTimeout) {
                // Remove burst from this.activeBursts
                this.activeBursts.delete(burstId);

                // Move the host from availHosts to rejectedHosts
                const download = this.activeDownloads.get(burst.fileAddr);
                if (download) {
                    for (const host of download.availHosts) {
                        if (host.ip === burst.senderIp
                            && host.port === burst.senderPort)
                        {
                            download.availHosts.delete(host);
                            download.rejectedHosts.add(host);
                        }
                    }
                }
                continue;
            }

            const byteCount = burst.tickByteCount;
            burst.tickByteCount = 0;

            const nextByteCount = Math.min(
                byteCount,
                burst.offset + burst.length - burst.lastPacketOffset);

            // Add nextByteCount to bytesQueued[burst.fileAddr]
            if (bytesQueued.has(burst.fileAddr)) {
                bytesQueued.set(
                    burst.fileAddr,
                    bytesQueued.get(burst.fileAddr) + nextByteCount);
            } else {
                bytesQueued.set(burst.fileAddr, nextByteCount);
            }

            this.bytesRequested += nextByteCount;
        }

        for (const [fileAddr, downloadInProgress]
             of this.activeDownloads.entries())
        {
            if (Date.now() - downloadInProgress.lastPacketTimestamp > 500) {
                downloadInProgress.rejectCompletion(new Error(
                    "Download failed due to 500ms without a packet."));
                // Remove from this.activeDownloads
                this.activeDownloads.delete(fileAddr);
            }
        }

        while(this.bytesRequested < this.bytesBudget) {
            // Request more data
            const downloadInProgressArr = Array.from(
                this.activeDownloads.values());
            
            if (downloadInProgressArr.length > 0) {
                // Just take the first one for now, but it could be
                // optimized to prioritize differently
                const downloadInProgress = downloadInProgressArr[0];

                const availHostsArr = Array.from(downloadInProgress.availHosts);
                if (availHostsArr.length > 0) {
                    this.requestMoreData(downloadInProgress, availHostsArr);
                }
            }
        }
    }

    private async requestMoreData(downloadInProgress: DownloadInProgress,
                                  hosts: PeerProxy[])
    {
        let chunkSize = Math.min(
            DOWNLOAD_CHUNK_SIZE,
            downloadInProgress.size - Math.max(
                ...downloadInProgress.unrequestedOffsets));
        let chunksPerHost = Math.ceil(chunkSize / hosts.length);

        for (let host of hosts) {
            let burstId = this.newBurstIndex++;
            let offset = downloadInProgress.unrequestedOffsets.shift();
            let length = Math.min(chunksPerHost,
                                  downloadInProgress.size - offset);
            const packet = new DataRequestMessage(
                burstId, downloadInProgress.fileAddr, offset, length);

            this.proxyManager.networkingManager.send(
                packet, host.ip, host.port);
            const burst = new DownloadBurst(
                burstId, downloadInProgress.fileAddr, offset, length);
            this.activeBursts.set(burst.burstId, burst);

            this.bytesRequested += length;
            if (this.bytesRequested >= this.bytesBudget) {
                break;
            }
        }
    }

    handleDataResponseOk(senderIp: string, senderPort: number, message: Message)
    {
        if (!(message instanceof DataResponseOkMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseOkMessage = message as DataResponseOkMessage;
        
        let downloadInProgress = this.activeDownloads.get(
            dataResponseOkMessage.fileAddr);
        
        if (!downloadInProgress) {
            throw new Error("No active download for the received data packet");
        }

        // Write the data to the output file and remove the offset from
        // unreceivedOffsets

        await downloadInProgress.outputFile.write(
            dataResponseOkMessage.data,
            0, dataResponseOkMessage.length, dataResponseOkMessage.offset);
        
        downloadInProgress.unreceivedOffsets.delete(
            dataResponseOkMessage.offset);
        
        // If all chunks have been received, close the file, add it
        // to the cache, and resolve the promise
        if (downloadInProgress.unreceivedOffsets.size === 0) {
            await downloadInProgress.outputFile.close();
            const cachedFile = await this.proxyManager.fileCache.addFile(
                downloadInProgress.tempDownloadFile,
                downloadInProgress.fileAddr, true, false);
            downloadInProgress.resolveCompletion(cachedFile);
        }
    }

    handleDataResponseUnknown(senderIp: string, senderPort: number,
                              message: Message)
    {
        if (!(message instanceof DataResponseUnknownMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseUnknownMessage =
            message as DataResponseUnknownMessage;
        
        let downloadInProgress = this.activeDownloads.get(
            dataResponseUnknownMessage.fileAddr);
        
        if (!downloadInProgress)
            throw new Error("No active download for the received data packet");

        // Remove the sender from availHosts and dhtHosts and add to
        // rejectedHosts
        const sender = new PeerProxy(senderIp, senderPort);
        downloadInProgress.availHosts.delete(sender);
        downloadInProgress.dhtHosts.delete(sender);
        downloadInProgress.rejectedHosts.add(sender);

        if (downloadInProgress.availHosts.size === 0
            && downloadInProgress.dhtHosts.size === 0)
        {
            this.activeDownloads.delete(downloadInProgress.fileAddr);
            downloadInProgress.rejectCompletion(new Error(
                "All hosts rejected the download request"));
        }
    }

    handleDataResponseElsewhere(senderIp: string, senderPort: number,
                                message: Message)
    {
        if (!(message instanceof DataResponseElsewhereMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseElsewhereMessage =
            message as DataResponseElsewhereMessage;
        
        let downloadInProgress = this.activeDownloads.get(
            dataResponseElsewhereMessage.fileAddr);
        
        if (!downloadInProgress)
            throw new Error("No active download for the received data packet");

        let newHosts: PeerProxy[] = [];

        // Check each node in the received nodeInfo, add new nodes to
        // availHosts and request data from them
        
        for (let node of dataResponseElsewhereMessage.nodeInfo) {
            const host = new PeerProxy(node.ip, node.port);
            if (!downloadInProgress.availHosts.has(host)) {
                downloadInProgress.availHosts.add(host);
                newHosts.push(host);
            }
        }

        if (newHosts.length > 0) {
            this.requestMoreData(downloadInProgress, newHosts);
        }
    }
}

export {
    DownloadManager,
};
