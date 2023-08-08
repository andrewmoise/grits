import * as fs from 'fs';
import { promises as fsPromises } from 'fs';

import { Config } from './config';
import { NetworkManager } from './network';
import { TrafficManager, TELEMETRY_CHECK_RATE } from './traffic';

class TelemetryInfo {
    bytesSeen: number;
    startTimestamp: number;
    isOpen: boolean;

    constructor(now: number) {
        this.bytesSeen = 0;
        this.startTimestamp = now;
        this.isOpen = true;
    }
    
    update(now: number, inBytes: number): void {
        if (this.startTimestamp + TELEMETRY_CHECK_RATE < now) {
            this.bytesSeen += inBytes;
        } else if (this.isOpen) {
            this.bytesSeen += inBytes / 2;
            this.isOpen = false;
        }
    }
}

class PeerNode {
    ip: string;
    port: number;
    lastSeen: Date | null;

    dhtStoredData: WeakMap<CachedFile, Date>;
    trafficManager: TrafficManager | null;

    telemetryInfo: Map<number, TelemetryInfo>;
    
    constructor(ip: string, port: number) {
        this.ip = ip;
        this.port = port;
        this.lastSeen = null;

        this.dhtStoredData = new WeakMap();
        this.trafficManager = null;

        this.telemetryInfo = new Map();
    }

    updateLastSeen() {
        this.lastSeen = new Date();
    }

    // Returns seconds
    timeSinceSeen() {
        if (this.lastSeen === null)
            return -1;
        const currentTime = new Date();
        return Math.floor(
            (currentTime.getTime() - this.lastSeen.getTime()) / 1000);
    }
}

class AllPeerNodes {
    peerNodes: Map<string, PeerNode>;

    constructor() {
        this.peerNodes = new Map();
    }

    generateNodeKey(ip: string, port: number): string {
        return `${ip}:${port}`;
    }

    getPeerNode(ip: string, port: number): PeerNode {
        const key = this.generateNodeKey(ip, port);

        let result = this.peerNodes.get(key);
        if (result) return result;

        // FIXME
        console.log(`Unknown node ${ip}:${port}! For now we add it.`);
        return this.addPeerNode(ip, port);
    }

    addPeerNode(ip: string, port: number): PeerNode {
        const key = this.generateNodeKey(ip, port);
        if (!this.peerNodes.has(key)) {
            const peerNode = new PeerNode(ip, port);
            this.peerNodes.set(key, peerNode);
            return peerNode;
        } else {
            throw new Error(
                `Node with address ${ip} and port ${port} already exists.`
            );
        }
    }

    removePeerNode(ip: string, port: number) {
        const key = this.generateNodeKey(ip, port);
        if (this.peerNodes.has(key))
            this.peerNodes.delete(key);
    }
}

class CachedFile {
    public path: string;
    public size: number;
    public refCount: number;
    public fileAddr: string;

    constructor(path: string, size: number, refCount: number, fileAddr: string)
    {
        this.path = path;
        this.size = size;
        this.refCount = refCount;
        this.fileAddr = fileAddr;
    }
    
    public async read(offset: number, length: number): Promise<Buffer> {
        const buffer = Buffer.allocUnsafe(length);
        const fd = await fsPromises.open(this.path, 'r');
        try {
            await fd.read(buffer, 0, length, offset);
        } finally {
            await fd.close();
        }
        return buffer;
    }

    public release(): void {
        this.refCount -= 1;
    }
}

class FileRetrievalError extends Error {
    constructor(message: string) {
        super(message);
        this.name = "FileRetrievalError";
    }
}

const DOWNLOAD_CHUNK_SIZE = 1400;

export {
    AllPeerNodes, PeerNode, CachedFile, FileRetrievalError, TelemetryInfo,
    DOWNLOAD_CHUNK_SIZE
};
