import * as util from 'util';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as timers from 'timers';
import * as events from "events";

import { Config } from "./config";
import { FileCache } from "./filecache";
import { CachedFile } from "./structures";
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

class DownloadInProgress {
    promise: Promise<CachedFile>;
    packets: Message[] = [];
    packetEmitter: events.EventEmitter = new events.EventEmitter();
    proxyManager: ProxyManagerBase;

    constructor(proxyManager: ProxyManagerBase, fileAddr: string) {
        this.proxyManager = proxyManager;
        this.promise = this.doDownload(fileAddr);
    }

    private async doDownload(fileAddr: string): Promise<CachedFile> {
        const config: Config = this.proxyManager.config;

        const [hexHash, sizeStr] = fileAddr.split(':');
        const size = parseInt(sizeStr);
        const hash: Buffer = Buffer.from(hexHash, 'hex');
        
        if (config.thisHost === config.rootHost
            && config.thisPort === config.rootPort) {
            throw new Error("Cannot download on root proxy yet!");
        }

        const networkingManager = this.proxyManager.networkingManager;
        const numChunks = Math.ceil(size / DOWNLOAD_CHUNK_SIZE);

        const randomSuffix = Math.floor(Math.random() * 1000000).toString()
            .padStart(6, '0');
        const tempDownloadFile = path.join(config.tempDownloadDirectory,
                                           hexHash + '-' + randomSuffix);

        const chunksReceived = Array(numChunks).fill(false);
        await this.requestMissingChunks(fileAddr, chunksReceived, size);

        const fd = await fs.promises.open(tempDownloadFile, 'w');
        let lastPacketTimestamp = Date.now();

        try {
            while (chunksReceived.includes(false)) {
                try {
                    const packet = await this.waitForPacket();

                    const offset = packet.offset;
                    const chunkIndex = Math.floor(offset / DOWNLOAD_CHUNK_SIZE);
                    chunksReceived[chunkIndex] = true;
                    await fd.write(packet.data, 0, packet.data.length, offset);

                    lastPacketTimestamp = Date.now();
                } catch (error: any) {
                    if (error instanceof TimeoutError) {
                        if (Date.now() - lastPacketTimestamp < 600) {
                            console.log(
                                "Timed out waiting for packets; we retry");
                            await this.requestMissingChunks(
                                fileAddr, chunksReceived, size);
                        } else {
                            throw new Error(
                                "No packets received for 600ms, aborting.");
                        }
                    } else {
                        // If the error isn't a TimeoutError, rethrow it
                        throw error;
                    }
                }
            }
        } finally {
            await fd.close();
        }

        const file = await this.proxyManager.fileCache.addFile(
            tempDownloadFile, fileAddr);
        return file;
    }

    async requestMissingChunks(fileAddr: string, chunksReceived: boolean[],
                               size: number)
    {
        const config = this.proxyManager.config;
        if (!config.rootHost || !config.rootPort)
            throw new Error("For now we must have a root to download.");

        for (let i = 0; i < chunksReceived.length; i++) {
            if (!chunksReceived[i]) {
                const offset = i * DOWNLOAD_CHUNK_SIZE;
                const length = Math.min(DOWNLOAD_CHUNK_SIZE, size - offset);
                const request = new DataRequestMessage(
                    fileAddr, offset, length);
                this.proxyManager.networkingManager.send(
                    request, config.rootHost, config.rootPort);
            }
        }
    }

    async waitForPacket(): Promise<DataResponseOkMessage> {
        return new Promise((resolve, reject) => {
            const timeout = setTimeout(
                () => reject(new TimeoutError()), 200);

            this.packetEmitter.once(
                'packet', (packet: DataResponseOkMessage) => {
                    clearTimeout(timeout);
                    resolve(packet);
                });
        });
    }
}

class DownloadManager {
    private activeDownloads: Map<string, DownloadInProgress>;
    private proxyManager: ProxyManagerBase;
    
    constructor(proxyManager: ProxyManagerBase) {
        this.activeDownloads = new Map();
        this.proxyManager = proxyManager;
    }

    download(fileAddr: string): Promise<CachedFile> {
        const existingDownload = this.activeDownloads.get(fileAddr);
        if (existingDownload) {
            // Wait for an already-in-progress download to complete.
            return existingDownload.promise;
        } else {
            // Start a new download.
            const downloadInProgress = new DownloadInProgress(
                this.proxyManager, fileAddr);
            this.activeDownloads.set(fileAddr, downloadInProgress);
            return downloadInProgress.promise;
        }
    }
    
    handleDataResponseOk(senderIp: string, senderPort: number,
                         message: Message)
    {
        if (!(message instanceof DataResponseOkMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseOkMessage = message as DataResponseOkMessage;

        const fileAddr: string = dataResponseOkMessage.fileAddr;
        
        const download: DownloadInProgress | undefined =
            this.activeDownloads.get(fileAddr);

        if (download) {
            download.packets.push(message);
            download.packetEmitter.emit('packet', dataResponseOkMessage);
        } else {
            console.log(`Received ${fileAddr}[${dataResponseOkMessage.offset}+${dataResponseOkMessage.length}] with no download active`);
        }
    }

    handleDataResponseUnknown(senderIp: string, senderPort: number,
                              message: Message)
    {
        if (!(message instanceof DataResponseUnknownMessage))
            throw new Error("Data request of wrong TS type!");
        const dataResponseUnknownMessage = message as DataResponseUnknownMessage;

        const fileAddr: string = dataResponseUnknownMessage.fileAddr;
        
        console.log(`Got unknown data response from ${senderIp}:${senderPort} for ${fileAddr}`);
    }
}
export {
    DownloadManager,
};
