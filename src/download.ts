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
import { NetworkManager } from "./network";
import { ProxyManagerBase } from "./proxy";
import { UpstreamManager } from "./traffic";

import {
    CachedFile, FileRetrievalError, PeerProxy, DOWNLOAD_CHUNK_SIZE
} from "./structures";

import {
    Message,
    DataRequestMessage, DataResponseOk, DataResponseUnknown,
    DhtLookupMessage, DhtLookupResponse,
} from "./messages";

const TRANSFER_ID_CHARACTERS =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';

type DownloadTaskResult = {
  id: number;
  offset: number;
  length: number;
};

class DownloadInProgress {
    downloadManager: DownloadManager;
    
    fileAddr: string;
    size: number;
    transferId: string;
    
    completionPromise: Promise<CachedFile>;
    resolveCompletion!: (value: CachedFile | PromiseLike<CachedFile>) => void;
    rejectCompletion!: (reason?: any) => void;

    outputFile: fs.promises.FileHandle | null;
    tempDownloadFile: string | null;
    
    availHosts: Set<PeerProxy>;
    rejectedHosts: Set<PeerProxy>;
    
    unreceivedOffsets: Set<number>;

    runningRequests: Map<number, Promise<DownloadTaskResult>>;
    runningRequestIndex: number;
    
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
        this.tempDownloadFile = null;
        
        this.unreceivedOffsets = new Set(
            Array.from(
                { length: Math.ceil(this.size / DOWNLOAD_CHUNK_SIZE) },
                (_, i) => i * DOWNLOAD_CHUNK_SIZE
            )
        );

        this.availHosts = new Set();
        this.rejectedHosts = new Set();
        
        this.runningRequests = new Map();
        this.runningRequestIndex = 0;
    }

    async run(seedProxies: PeerProxy[]): Promise<void> {
        try {
            this.log(`Run download for ${this.fileAddr}`);

            const randomSuffix = Math.floor(Math.random() * 1000000).toString();
            this.tempDownloadFile = `${this.downloadManager.proxyManager.config.tempDownloadDirectory}/${this.fileAddr}-${randomSuffix}`;
            this.outputFile = await fs.promises.open(
                this.tempDownloadFile, 'w');

            let dhtLength = Math.min(this.size, DOWNLOAD_CHUNK_SIZE);
        
            for(let proxy of seedProxies) {
                if (proxy === this.downloadManager.proxyManager.thisProxy) {
                    this.rejectedHosts.add(proxy);
                } else {
                    this.runningRequests.set(
                        this.runningRequestIndex,
                        this.lookupFrom(proxy, this.runningRequestIndex));
                    this.runningRequestIndex++;
                }
            }

            this.log(`Ready; queued all DHT downloads - ${this.runningRequests.size}`);

            let needDownloads = [[0, this.size]];
         
            while(true) {
                let completed = await Promise.race(
                    this.runningRequests.values());

                this.log(`Got completed promise for [${completed.offset}+${completed.length}]`);

                this.log(`  ${this.unreceivedOffsets.size} unreceived offsets`);
                
                this.log('  needDownloads is:');
                for(let download of needDownloads)
                    this.log(`    ${download[0]} ${download[1]}`);
                
                if (this.unreceivedOffsets.size <= 0)
                    break;
                
                this.runningRequests.delete(completed.id);
                this.maybeRequeueDownload(needDownloads,
                                          completed.offset, completed.length);

                if (needDownloads.length <= 0) {
                    this.log('Not done but no needDownloads!');
                    throw new Error('Not done but no needDownloads!');
                }

                this.log('  after requeue, needDownloads is:');
                for(let download of needDownloads)
                    this.log(`    ${download[0]} ${download[1]}`);

                this.log(`Maybe queue: ${this.availHosts.size} avail hosts`);
                
                if (this.availHosts.size > 0) {
                    this.log('  yes QD');
                    await this.queueDownload(needDownloads);
                } else if (this.runningRequests.size <= 0) {
                    throw new Error("Couldn't complete download");
                }

                this.log('  all done, needDownloads is:');
                for(let download of needDownloads)
                    this.log(`    ${download[0]} ${download[1]}`);
            }
            
            const outputFile = this.outputFile;
            this.outputFile = null;
            await outputFile.close();
            
            const cachedFile =
                await this.downloadManager.proxyManager.fileCache.addFile(
                    this.tempDownloadFile!,
                    this.fileAddr, true, false);
            this.resolveCompletion(cachedFile);
        } catch(err) {
            if (this.outputFile) {
                this.outputFile.close();
                this.outputFile = null;
            }

            if (this.tempDownloadFile) {
                const tempDownloadFile = this.tempDownloadFile;
                this.tempDownloadFile = null;
                await fs.promises.unlink(tempDownloadFile);
            }
            
            this.rejectCompletion(err);
        }

        this.log(`Promise resolved for download ${this.fileAddr}`);
        
        // We've already resolved the main promise -- now we hang around and
        // clean up outstanding network requests. If we get an exception
        // at this stage, we blow up the server process, since we can't
        // anymore reject the promise. Hopefully we don't get exceptions.
        
        while (this.runningRequests.size > 0) {
            let completed = await Promise.race(
                this.runningRequests.values());
            this.runningRequests.delete(completed.id);
        }
        
        this.log(`All done with cleanup for download ${this.fileAddr}`);
    }

    maybeRequeueDownload(needDownloads: number[][], offset: number,
                         length: number)
    : void {
        this.log(`Maybe requeue ${offset}+${length}`);
        let currentChunk = null;

        for(let i=offset;
            i < length+DOWNLOAD_CHUNK_SIZE;
            i += DOWNLOAD_CHUNK_SIZE)
        {
            let needThisOne = (i<length && this.unreceivedOffsets.has(i));
            if (!needThisOne)
                continue;
            
            let nextI = i+DOWNLOAD_CHUNK_SIZE;
            let needNextOne =
                (nextI < length && this.unreceivedOffsets.has(nextI));

            if (currentChunk === null)
                currentChunk = i;

            if (needThisOne && !needNextOne) {
                let realLength = Math.min(offset+length, nextI) - currentChunk;
                this.log(`  Do requeue ${currentChunk}+${realLength}`);
                needDownloads.push([currentChunk, realLength]);
                currentChunk = null;
            }
        }
    }
    
    async queueDownload(needDownloads: number[][]): Promise<void> {
        this.log(`  In QD, ND length ${needDownloads.length}`);
        this.log(`  Popping ${needDownloads[needDownloads.length-1]}`);

        assert(needDownloads.length > 0
            && needDownloads[needDownloads.length-1].length == 2,
               'needDownloads is malformed');
        let [offset, length] = needDownloads.pop()!;

        this.log(`Do queue download [${offset}+${length}]`);
        this.log(`  Avail hosts ${this.availHosts.size}`);
        
        const potentialBursts =
            await this.downloadManager.downstreamManager.requestDownload(
                Array.from(this.availHosts), length);
        if (potentialBursts === null || potentialBursts.length <= 0)
            throw new Error('Null return from downstreamManager');

        for (let burst of potentialBursts) {
            if (length <= 0)
                break;
            
            const requestId = this.runningRequestIndex++;
            const realLen = Math.min(burst.bytesAllowed, length);
            
            this.runningRequests.set(requestId, this.downloadFrom(
                burst.source, offset, length, requestId));
            offset += realLen;
            length -= realLen;
        }

        if (length > 0)
            needDownloads.push([offset, length]);
    }

    async lookupFrom(host: PeerProxy, id: number): Promise<DownloadTaskResult> {
        this.log(`  Iter lookup ${host.ip}:${host.port}`);

        const network = this.downloadManager.proxyManager.networkManager;
        const message = new DhtLookupMessage(this.fileAddr, this.transferId);

        for(let attempts = 0; attempts < 30; attempts++) {
            const request = network.newRequest(host.ip, host.port, message);
            const response = await request.getResponse();
            if (response === null) {
                continue;
            } else if (response instanceof DhtLookupResponse) {
                await this.handleDhtLookupResponse(host, response!);
                return {id, offset: 0, length: 0};
            } else {
                throw new Error(`Wrong type result: ${response}`);
            }
        }

        throw new Error("Couldn't communicate with ${host.ip}:${host.port}");
    }
        
    async downloadFrom(
        host: PeerProxy, offset: number, length: number, id: number)
    : Promise<DownloadTaskResult> {
        this.log(`  Iter downloadFrom() ${host.ip}:${host.port} at [${offset}+${length}]`);

        const network = this.downloadManager.proxyManager.networkManager;
        const message = new DataRequestMessage(
            this.fileAddr, offset, length,
            this.transferId);
        const request = network.newRequest(host.ip, host.port, message);

        try {
            while(true) {
                let response = await request.getResponse();
                
                if (!this.outputFile || this.unreceivedOffsets.size <= 0)
                    return {id, offset, length};
                
                if (response === null) {
                    // Timeout
                    
                    // FIXME - we need a little better handling for this; maybe
                    // availHosts can be a Map, and we can track consecutive
                    // timeouts for each host, and we can bail on the overall
                    // transfer IFF we have no availHosts with no timeouts.

                    //if (this.availHosts.has(host)) {
                    //    this.availHosts.delete(host);
                    //    this.rejectedHosts.add(host);
                    //}

                    return {id, offset, length};
                } else if (response instanceof DataResponseUnknown) {
                    await this.handleDataResponseUnknown(host, response!);
                    return {id, offset, length};
                } else if (response instanceof DataResponseOk) {
                    await this.handleDataResponseOk(host, response!);
                    this.log(`Response is ok. availHosts has ${this.availHosts.size}.`);
                    if (this.unreceivedOffsets.size <= 0
                        || response.offset+response.length === offset+length)
                    {
                        return {id, offset, length};
                    }
                } else {
                    throw new Error(`Unrecognized response: ${response}`);
                }
            }
        } finally {
            request.close();
        }

        throw new Error('Fell out of downloadFrom() main loop');
    }

    async handleDataResponseOk(source: PeerProxy,
                               message: DataResponseOk)
    {
        this.log(`Got OK ${this.fileAddr}@${message.offset}[${message.length}] from ${source.ip}:${source.port}`);

        try {
        
            this.log(`  Before: ${this.unreceivedOffsets.size} unreceived`);
            
            // FIXME - this is for DHT hosts that unexpectedly have real data;
            // this should be handled better though:
            if (!this.availHosts.has(source))
                this.availHosts.add(source);
            
            if (this.unreceivedOffsets.has(message.offset)) {
                this.log('    has');
                
                assert(message.offset >= 0,
                       'Received offset negative');
                assert(message.offset + message.length <= this.size,
                       'Received end offset too large');
                assert(message.length <= DOWNLOAD_CHUNK_SIZE,
                       `Received chunk size ${message.length} is greater than DOWNLOAD_CHUNK_SIZE`);
                
                await this.outputFile!.write(
                    message.data,
                    0, message.length, message.offset);
                
                this.unreceivedOffsets.delete(message.offset);
            }
            
            this.log(`  After: ${this.unreceivedOffsets.size} unreceived`);
        } catch(err) {
            this.log(`Caught error! ${err}`);
            throw err;
        }
    }

    async handleDataResponseUnknown(source: PeerProxy,
                                    message: DataResponseUnknown)
    {
        this.log(`Got Unk ${this.fileAddr} from ${source.ip}:${source.port}`);
        
        if (this.availHosts.has(source))
            this.availHosts.delete(source)
        this.rejectedHosts.add(source);
    }

    async handleDhtLookupResponse(source: PeerProxy,
                                  message: DhtLookupResponse)
    {
        this.log(`Got DHT response ${this.fileAddr} from ${source.ip}:${source.port}`);
        
        if (!this.outputFile) {
            this.log('  Bail early');
            return;
        }

        for (const {ip, port} of message.nodeInfo) {
            this.log(`Elsewhere ${ip}:${port}`);
            
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
            } else {
                this.log(`  Skipping ${newHost.ip}:${newHost.port}`);
            }
        }
    }

    log(msg: string) {
        this.downloadManager.proxyManager.logger.log(
            new Date(), this.transferId, msg);
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
};



export {
    DownloadManager,
};
