import * as util from 'util';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as timers from 'timers';
import * as events from 'events';

import { assert } from 'console';

import { Config } from './config';
import { FileCache } from './filecache';
import { NetworkManager } from "./network";
import { ProxyManagerBase } from "./proxy";

import {
    CachedFile, FileRetrievalError, PeerNode, DOWNLOAD_CHUNK_SIZE
} from "./structures";

import {
    Message,
    DataFetchMessage, DataFetchResponseOk, DataFetchResponseNo,
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
    
    availSources: Set<PeerNode>;
    rejectedSources: Set<PeerNode>;
    
    unreceivedOffsets: Set<number>;

    runningSteps: Map<number, Promise<number>>;
    nextRunningStepId: number;

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

        this.availSources = new Set();
        this.rejectedSources = new Set();
        
        this.runningSteps = new Map();
        this.nextRunningStepId = 0;
        this.needDownloads = [[0, this.size]];
        
        this.downloadManager.activeDownloads.set(fileAddr, this);
    }

    async run(seedPeers: PeerNode[]): Promise<void> {
        try {
            this.log(`Run download for ${this.fileAddr}`);

            const randomSuffix = Math.floor(Math.random() * 1000000).toString();
            this.tempDownloadFile = `${this.downloadManager.proxyManager.config.tempDownloadDirectory}/${this.fileAddr}-${randomSuffix}`;
            this.outputFile = await fs.promises.open(
                this.tempDownloadFile, 'w');

            // Populate this.availSources via DHT lookup, pausing until at least
            // one of the DHT peers gets back to us
            await this.populateAvailPeers(seedPeers);
            
            // Start up the first of the main download-queueing steps.
            let stepId = this.nextRunningStepId++;
            this.runningSteps.set(stepId,
                                  this.makeNewBurstsStep(stepId, null));

            // Main loop, waiting for any step to complete and then finishing
            // if after it, we're done.
            while(true) {
                let completed = await Promise.race(
                    this.runningSteps.values());

                this.log(`Got completed promise`);
                this.log(`  ${this.unreceivedOffsets.size} unreceived offsets`);                this.log('  needDownloads is:');
                for(let download of this.needDownloads)
                    this.log(`    ${download[0]} ${download[1]}`);
                this.log(`Maybe queue: ${this.availSources.size} avail peers`);
                
                this.runningSteps.delete(completed);
                if (this.unreceivedOffsets.size <= 0)
                    break;
                if (this.availSources.size <= 0) {
                    this.log("  Couldn't complete download!");
                    throw new Error("Couldn't complete download");
                }
            }

            // All done. Close the file, put it in the cache.
            const outputFile = this.outputFile;
            this.outputFile = null;
            await outputFile.close();
            
            const cachedFile =
                await this.downloadManager.proxyManager.fileCache.addFile(
                    this.tempDownloadFile!,
                    this.fileAddr, true, false);

            // Download is no longer in progress now; resolve to anyone
            // waiting for us.
            this.downloadManager.activeDownloads.delete(this.fileAddr);
            this.resolveCompletion(cachedFile);

            this.log(`Promise resolved for download ${this.fileAddr}`);
        } catch(err) {
            // Errors can happen from legit things like timeouts or fileAddrs
            // that aren't anywhere in the DHT. Make sure we clean up properly
            // and wait for all the steps we're running to finish before we
            // end for real.
            
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

            this.log(`Promise rejected for download ${this.fileAddr}`);
        } finally {
            // We resolved the promise right away so no one has to wait for
            // the lame-duck transfer steps to complete, but we still need
            // to hang around until all the Promise resources are cleaned up.

            while (this.runningSteps.size > 0) {
                let completed = await Promise.race(
                    this.runningSteps.values());
                this.runningSteps.delete(completed);
            }
        
            this.log(`All done with cleanup for download ${this.fileAddr}`);
        }
    }

    async populateAvailPeers(seedPeers: PeerNode[]): Promise<void> {
        for(let proxy of seedPeers) {
            this.log(`Populating, checking ${proxy.ip}:${proxy.port}`);
            if (proxy !== this.downloadManager.proxyManager.thisNode) {
                this.log('  yes');
                let stepId = this.nextRunningStepId++;
                this.runningSteps.set(
                    stepId, this.dhtLookupStep(proxy, stepId));
            } else {
                // We are one of the DHT proxies -- we can right away
                // populate our answers, if any.
                const proxyManager = this.downloadManager.proxyManager;
                const dataMap = proxyManager.proxyDataMap.fileAddrToNode;
                const dhtPeers = dataMap.get(this.fileAddr);
                if (dhtPeers)
                    this.availSources = new Set(dhtPeers.map(
                        proxyInfo => proxyInfo.node));
                
                this.log(`Found self DHT proxy; init ${this.availSources.size} peers`);
            }
        }
        
        this.log(`Ready; queued all DHT downloads - ${this.runningSteps.size}`);
        
        while (this.availSources.size <= 0) {
            let completed = await Promise.race(
                this.runningSteps.values());
            
            this.runningSteps.delete(completed);
            
            if (this.runningSteps.size <= 0
                && this.availSources.size <= 0)
            {
                this.log(`Couldn't find ${this.fileAddr} in DHT`);
                throw new Error(`Couldn't find ${this.fileAddr} in DHT`);
            }
        }
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

    async dhtLookupStep(peer: PeerNode, id: number): Promise<number> {
        this.log(`  Iter lookup ${peer.ip}:${peer.port}`);

        const network = this.downloadManager.proxyManager.networkManager;
        const message = new DhtLookupMessage(this.fileAddr, this.transferId);

        for(let attempts = 0; attempts < 10; attempts++) {
            await network.requestTransfer(peer, message);
            const request = network.newRequest(peer, message);
            const response = await request.getResponse();
            if (response === null) {
                continue;
            } else if (response instanceof DhtLookupResponse) {
                await this.handleDhtLookupResponse(peer, response!);
                return id;
            } else {
                throw new Error(`Wrong type result: ${response}`);
            }
        }

        this.log(`Couldn't communicate with ${peer.ip}:${peer.port}`);
        return id;
    }

    async makeNewBurstsStep(stepId: number, localAvailPeers: PeerNode[] | null)
    : Promise<number> {
        this.log(`  In MR, ND length ${this.needDownloads.length}`);

        if (this.needDownloads.length <= 0
            || this.availSources.size <= 0
            || this.outputFile === null)
        {
            // All done! Whether from success or failure, we don't need
            // any more step initiations.
            
            // If we've got to the end, we may wind up needing more data
            // reqeusts for failed chunks, but we'll do all that via requeues.
            return stepId;
        }

        this.log(
            `  Popping ${this.needDownloads[this.needDownloads.length-1]}`);

        assert(this.needDownloads[this.needDownloads.length-1].length == 2,
               'needDownloads is malformed');
        assert(this.availSources.size > 0, 'No peers!');
        let [offset, length] = this.needDownloads.pop()!;

        this.log(`Do queue download [${offset}+${length}]`);
        this.log(`  Avail peers ${this.availSources.size}`);

        const network = this.downloadManager.proxyManager.networkManager;
        
        const potentialBursts =
            await network.requestDownload(
                localAvailPeers ? localAvailPeers : Array.from(this.availSources),
                length);
        if (potentialBursts === null || potentialBursts.length <= 0)
            throw new Error('Null return from requestDownload()');

        for (let burst of potentialBursts) {
            if (length <= 0)
                break;

            const thisLen = Math.min(length, burst.downloadBytesAllowed);
            
            assert(offset % DOWNLOAD_CHUNK_SIZE == 0);
            assert(thisLen % DOWNLOAD_CHUNK_SIZE == 0
                || (offset+thisLen) == this.size);
            
            const newStepId = this.nextRunningStepId++;
            this.runningSteps.set(
                newStepId,
                this.downloadBurstStep(
                    burst.source, offset, thisLen, newStepId));

            offset += thisLen;
            length -= thisLen;
        }

        if (length > 0)
            this.needDownloads.push([offset, length]);
        
        if (!localAvailPeers
            && this.outputFile
            && this.needDownloads.length > 0)
        {
            this.log(`  Repeat download queue call`);

            const newStepId = this.nextRunningStepId++;
            this.runningSteps.set(
                newStepId,
                this.makeNewBurstsStep(newStepId, null));
        }
        
        this.log(
            `  All done -- needDownloads has ${this.needDownloads.length}`);

        return stepId;
    }
    
    async downloadBurstStep(peer: PeerNode, offset: number, length: number,
                            id: number)
    : Promise<number> {
        this.log(`  Iter downloadBurstStep() ${peer.ip}:${peer.port} at [${offset}+${length}]`);

        const network = this.downloadManager.proxyManager.networkManager;
        const message = new DataFetchMessage(
            this.fileAddr, offset, length,
            this.transferId);
        // No requestTransfer -- we already budgeted for it in
        // makeNewBurstsStep();
        const request = network.newRequest(peer, message);

        try {
            while(true) {
                let response = await request.getResponse();
                
                if (!this.outputFile || this.unreceivedOffsets.size <= 0) {
                    this.log(`    All done - downloadBurstStep() ${peer.ip}:${peer.port} at [${offset}+${length}] returning`);
                    return id;
                }
                
                if (response === null) {
                    this.log(`Response is null.`);
                    // Timeout
                    
                    // FIXME - we need a little better handling for this; maybe
                    // availSources can be a Map, and we can track consecutive
                    // timeouts for each peer, and we can bail on the overall
                    // transfer IFF we have no availSources with no timeouts.

                    //if (this.availSources.has(peer)) {
                    //    this.availSources.delete(peer);
                    //    this.rejectedSources.add(peer);
                    //}

                    return id;
                } else if (response instanceof DataFetchResponseNo) {
                    this.log(`Response is unknown.`);
                    await this.handleDataFetchResponseNo(peer, response!);
                    return id;
                } else if (response instanceof DataFetchResponseOk) {
                    this.log(`Response is ok. availSources has ${this.availSources.size}.`);
                    await this.handleDataFetchResponseOk(peer, response!);
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

        throw new Error('Fell out of downloadBurstStep() main loop');
    }

    async handleDataFetchResponseOk(source: PeerNode,
                                    message: DataFetchResponseOk)
    : Promise<void> {
        this.log(`Got OK ${this.fileAddr}@${message.offset}[${message.length}] from ${source.ip}:${source.port}`);

        try {
        
            this.log(`  Before: ${this.unreceivedOffsets.size} unreceived`);
            
            // FIXME - this is for DHT peers that unexpectedly have real data;
            // this should be handled better though:
            if (!this.availSources.has(source))
                this.availSources.add(source);
            
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

    async handleDataFetchResponseNo(source: PeerNode,
                                    message: DataFetchResponseNo)
    : Promise<void> {
        this.log(`Got Unk ${this.fileAddr} from ${source.ip}:${source.port}`);
        
        if (this.availSources.has(source))
            this.availSources.delete(source)
        this.rejectedSources.add(source);
    }

    async handleDhtLookupResponse(source: PeerNode,
                                  message: DhtLookupResponse)
    : Promise<void> {
        this.log(`Got DHT response ${this.fileAddr} from ${source.ip}:${source.port}`);
        
        if (!this.outputFile) {
            this.log('  Bail early');
            return;
        }

        for (const {ip, port} of message.nodeInfo) {
            this.log(`Elsewhere ${ip}:${port}`);
            
            const newPeer =
                this.downloadManager.proxyManager.allPeerNodes.getPeerNode(
                    ip, port);
            
            if(!newPeer) {
                this.log(`Unknown proxy: ${ip}:${port}`);
                continue;
            }

            if (!this.availSources.has(newPeer)
                && !this.rejectedSources.has(newPeer)
                && newPeer != this.downloadManager.proxyManager.thisNode)
            {
                this.log(`  Adding ${newPeer.ip}:${newPeer.port}`);
                this.availSources.add(newPeer);

                let stepId = this.nextRunningStepId++;
                this.runningSteps.set(
                    stepId, this.makeNewBurstsStep(stepId, [newPeer]));
            } else {
                this.log(`  Skipping ${newPeer.ip}:${newPeer.port}`);
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

    nextBurstId: number;
    
    activeDownloads: Map<string, DownloadInProgress>;

    constructor(proxyManager: ProxyManagerBase) {
        this.proxyManager = proxyManager;
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
            
            const seedPeers = this.proxyManager.blobFinder.getClosestPeers(
                fileAddr, this.proxyManager.config.dhtNotifyNumber);
            downloadInProgress.log(`Got ${seedPeers.length} seed proxies`);
            downloadInProgress.run(seedPeers);
        }
        
        return downloadInProgress.completionPromise;
    }
};

export {
    DownloadManager,
};
