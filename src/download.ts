import * as util from 'util';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as timers from 'timers';
import * as events from 'events';

import { assert } from 'console';

import { Config } from './config';
import { FileCache } from './filecache';
import { PotentialDownloadBurst, TrafficManager } from './traffic';
import { NetworkManager } from "./network";
import { ProxyManagerBase } from "./proxy";

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

    runningRequests: Map<number, Promise<number>>;
    runningRequestIndex: number;

    needDownloads: number[][];
    
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
        this.needDownloads = [[0, this.size]];
        
        this.downloadManager.activeDownloads.set(fileAddr, this);
    }

    async run(seedProxies: PeerProxy[]): Promise<void> {
        try {
            this.log(`Run download for ${this.fileAddr}`);

            const randomSuffix = Math.floor(Math.random() * 1000000).toString();
            this.tempDownloadFile = `${this.downloadManager.proxyManager.config.tempDownloadDirectory}/${this.fileAddr}-${randomSuffix}`;
            this.outputFile = await fs.promises.open(
                this.tempDownloadFile, 'w');

            for(let proxy of seedProxies) {
                if (proxy !== this.downloadManager.proxyManager.thisProxy) {
                    let requestIndex = this.runningRequestIndex++;
                    this.runningRequests.set(
                        requestIndex, this.lookupFrom(proxy, requestIndex));
                } else {
                    // We are one of the DHT proxies -- we can right away
                    // populate our answers, if any.
                    const proxyManager = this.downloadManager.proxyManager;
                    const dataMap = proxyManager.proxyDataMap.fileAddrToProxy;
                    const dhtHosts = dataMap.get(this.fileAddr);
                    if (dhtHosts)
                        this.availHosts = new Set(dhtHosts.proxies);

                    this.log(`Found self DHT proxy; init ${this.availHosts.size} hosts`);
                }
            }

            this.log(`Ready; queued all DHT downloads - ${this.runningRequests.size}`);

            while (this.availHosts.size <= 0) {
                let completed = await Promise.race(
                    this.runningRequests.values());

                this.runningRequests.delete(completed);

                if (this.runningRequests.size <= 0
                    && this.availHosts.size <= 0)
                {
                    this.log(`Couldn't find ${this.fileAddr} in DHT`);
                    throw new Error(`Couldn't find ${this.fileAddr} in DHT`);
                }
            }

            let requestIndex = this.runningRequestIndex++;
            this.runningRequests.set(requestIndex,
                                     this.makeRequest(requestIndex));
            
            while(true) {
                let completed = await Promise.race(
                    this.runningRequests.values());

                this.log(`Got completed promise`);
                this.log(`  ${this.unreceivedOffsets.size} unreceived offsets`);                this.log('  needDownloads is:');
                for(let download of this.needDownloads)
                    this.log(`    ${download[0]} ${download[1]}`);
                this.log(`Maybe queue: ${this.availHosts.size} avail hosts`);
                
                this.runningRequests.delete(completed);
                if (this.unreceivedOffsets.size <= 0)
                    break;
                if (this.availHosts.size <= 0) {
                    this.log("  Couldn't complete download!");
                    throw new Error("Couldn't complete download");
                }
            }
            
            const outputFile = this.outputFile;
            this.outputFile = null;
            await outputFile.close();
            
            const cachedFile =
                await this.downloadManager.proxyManager.fileCache.addFile(
                    this.tempDownloadFile!,
                    this.fileAddr, true, false);

            this.downloadManager.activeDownloads.delete(this.fileAddr);
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

            this.downloadManager.activeDownloads.delete(this.fileAddr);
            this.rejectCompletion(err);

            // FIXME - We leave some resources un-cleaned-up, even in
            // a legit case like just being unable to find a DHT host
            // for fileAddr
        }

        this.log(`Promise resolved for download ${this.fileAddr}`);
        
        // We've already resolved the main promise -- now we hang around and
        // clean up outstanding network requests. If we get an exception
        // at this stage, we blow up the server process, since we can't
        // anymore reject the promise. Hopefully we don't get exceptions.
        
        while (this.runningRequests.size > 0) {
            let completed = await Promise.race(
                this.runningRequests.values());
            this.runningRequests.delete(completed);
        }
        
        this.log(`All done with cleanup for download ${this.fileAddr}`);
    }

    maybeRequeueDownload(offset: number, length: number)
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
                this.needDownloads.push([currentChunk, realLength]);
                currentChunk = null;
            }
        }
    }

    async makeRequest(requestIndex: number): Promise<number> {
        this.log(`  In MR, ND length ${this.needDownloads.length}`);

        if (this.needDownloads.length <= 0
            || this.availHosts.size <= 0
            || this.outputFile === null)
        {
            // All done! Whether from success or failure, we don't need
            // any more request initiations.
            
            // If we've got to the end, we may wind up needing more requests
            // for failed chunks, but we'll do all that via requeues.
            return requestIndex;
        }

        this.log(
            `  Popping ${this.needDownloads[this.needDownloads.length-1]}`);

        assert(this.needDownloads[this.needDownloads.length-1].length == 2,
               'needDownloads is malformed');
        assert(this.availHosts.size > 0, 'No hosts!');
        let [offset, length] = this.needDownloads.pop()!;

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
            const thisLen = Math.min(length, burst.bytesAllowed);
            
            assert(offset % DOWNLOAD_CHUNK_SIZE == 0);
            assert(thisLen % DOWNLOAD_CHUNK_SIZE == 0
                || (offset+thisLen) == this.size);
            
            const newRequestIndex = this.runningRequestIndex++;
            this.runningRequests.set(
                newRequestIndex,
                this.downloadFrom(burst.source, offset, thisLen,
                                  newRequestIndex));

            offset += thisLen;
            length -= thisLen;
        }

        if (length > 0)
            this.needDownloads.push([offset, length]);
        
        if (this.outputFile && this.needDownloads.length > 0) {
            this.log(`  Repeat download queue call`);

            const newRequestIndex = this.runningRequestIndex++;
            this.runningRequests.set(
                newRequestIndex,
                this.makeRequest(newRequestIndex));
        }
        
        this.log(
            `  All done -- needDownloads has ${this.needDownloads.length}`);

        return requestIndex;
    }

    async lookupFrom(host: PeerProxy, id: number): Promise<number> {
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
                return id;
            } else {
                throw new Error(`Wrong type result: ${response}`);
            }
        }

        throw new Error("Couldn't communicate with ${host.ip}:${host.port}");
    }

    async downloadFrom(
        host: PeerProxy, offset: number, length: number, id: number)
    : Promise<number> {
        this.log(`  Iter downloadFrom() ${host.ip}:${host.port} at [${offset}+${length}]`);

        const network = this.downloadManager.proxyManager.networkManager;
        const message = new DataRequestMessage(
            this.fileAddr, offset, length,
            this.transferId);
        const request = network.newRequest(host.ip, host.port, message);

        try {
            while(true) {
                let response = await request.getResponse();
                
                if (!this.outputFile || this.unreceivedOffsets.size <= 0) {
                    this.log(`    All done - downloadFrom() ${host.ip}:${host.port} at [${offset}+${length}] returning`);
                    return id;
                }
                
                if (response === null) {
                    this.log(`Response is null.`);
                    // Timeout
                    
                    // FIXME - we need a little better handling for this; maybe
                    // availHosts can be a Map, and we can track consecutive
                    // timeouts for each host, and we can bail on the overall
                    // transfer IFF we have no availHosts with no timeouts.

                    //if (this.availHosts.has(host)) {
                    //    this.availHosts.delete(host);
                    //    this.rejectedHosts.add(host);
                    //}

                    return id;
                } else if (response instanceof DataResponseUnknown) {
                    this.log(`Response is unknown.`);
                    await this.handleDataResponseUnknown(host, response!);
                    return id;
                } else if (response instanceof DataResponseOk) {
                    this.log(`Response is ok. availHosts has ${this.availHosts.size}.`);
                    await this.handleDataResponseOk(host, response!);
                    if (this.unreceivedOffsets.size <= 0
                        || response.offset+response.length === offset+length)
                    {
                        return id;
                    }
                } else {
                    this.log(`Wrong response class!`);
                    throw new Error(`Unrecognized response: ${response}`);
                }
            }
        } finally {
            request.close();
            this.maybeRequeueDownload(offset, length);
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
            this.transferId, msg);
    }
}

class DownloadManager {
    proxyManager: ProxyManagerBase;
    downstreamManager: TrafficManager;

    nextBurstId: number;
    
    activeDownloads: Map<string, DownloadInProgress>;

    constructor(proxyManager: ProxyManagerBase) {
        this.proxyManager = proxyManager;
        this.downstreamManager = new TrafficManager(
            proxyManager.config,
            proxyManager.config.maxDownstreamSpeed,
            proxyManager.config.maxDownstreamQueue,
        );
        this.nextBurstId = 0;
        this.activeDownloads = new Map();
    }

    download(fileAddr: string): Promise<CachedFile> {
        this.proxyManager.logger.log(
            'download',
            `DownloadManager.download(${fileAddr})`);
        
        let downloadInProgress = this.activeDownloads.get(fileAddr);

        if (!downloadInProgress) {
            // Create a new DownloadInProgress and return its promise.
            downloadInProgress = new DownloadInProgress(this, fileAddr);
            downloadInProgress.log(`Starting new download for ${fileAddr}`);
            
            const seedProxies = this.proxyManager.blobFinder.getClosestProxies(
                fileAddr, this.proxyManager.config.dhtNotifyNumber);
            downloadInProgress.log(`Got ${seedProxies.length} seed proxies`);
            downloadInProgress.run(seedProxies);
        }
        
        return downloadInProgress.completionPromise;
    }
};



export {
    DownloadManager,
};
