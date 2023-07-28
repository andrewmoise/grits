import * as fs from 'fs';
import { promises as fsPromises } from 'fs';

class PeerProxy {
    ip: string;
    port: number;
    lastSeen: Date | null;

    dhtStoredData: WeakMap<CachedFile, Date>;
    
    constructor(ip: string, port: number) {
        this.ip = ip;
        this.port = port;
        this.lastSeen = null;

        this.dhtStoredData = new WeakMap();
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

export { PeerProxy, CachedFile, FileRetrievalError, DOWNLOAD_CHUNK_SIZE };
