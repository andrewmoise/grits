import * as assert from 'assert';
import * as crypto from 'crypto';
import * as fs from 'fs/promises';
import * as path from 'path';

import { Config } from './config';

import { Mutex } from 'async-mutex';

interface File {
    path: string;
    size: number;
    refCount: number;
}

class FileCache {
    private cacheDir: string;
    private maxSize: number;
    private currentSize: number;
    private lru: string[];
    private files: Map<string, File>;
    private mutex: Mutex;

    constructor(cacheDir: string, maxSize: number) {
        this.cacheDir = cacheDir;
        this.maxSize = maxSize;
        this.currentSize = 0;
        this.lru = [];
        this.files = new Map<string, File>();
        this.mutex = new Mutex();
    }

    private async _ensureSpaceFor(size: number) {
        assert(size <= this.maxSize);

        const release = await this.mutex.acquire();
        try {

            let removeIndex = this.lru.length - 1;
            while (removeIndex > 0 && this.currentSize + size > this.maxSize) {
                const removeHash = this.lru[removeIndex];
                const removeFile = this.files.get(removeHash);
                assert(removeFile);

                if (removeFile.refCount > 0)
                    continue;

                this.currentSize -= removeFile.size;
                this.lru.splice(removeIndex, 1);
                this.files.delete(removeHash);
                await fs.unlink(removeFile.path);

                removeIndex--;
            }
        } finally {
            release();
        }
    }

    public async addFile(inFilename: string, inHash: string): Promise<void> {
        const newFilePath = path.join(this.cacheDir, inHash);
        await fs.rename(inFilename, newFilePath);
        const fileStats = await fs.stat(newFilePath);
        await this._ensureSpaceFor(fileStats.size);
        this.files.set(inHash, { path: newFilePath, size: fileStats.size, refCount: 0 });
        this.lru.unshift(inHash);
        this.currentSize += fileStats.size;
    }

    public async touch(hash: string): Promise<void> {
        const release = await this.mutex.acquire();
        try {
            const index = this.lru.indexOf(hash);
            assert(index != -1);

            this.lru.splice(index, 1);
            this.lru.unshift(hash);
        } finally {
            release();
        }
    }

    public async readFile(hash: string, offset: number, length: number)
        : Promise<Buffer | null> {
        const file = this.files.get(hash);
        if (file) {
            file.refCount += 1;
            const buffer = Buffer.allocUnsafe(length);
            try {
                const fd = await fs.open(file.path, 'r');
                try {
                    await fd.read(buffer, 0, length, offset);
                } finally {
                    await fd.close();
                }
            } finally {
                file.refCount -= 1;
            }
            await this.touch(hash);
            return buffer;
        } else {
            return null;
        }
    }
}

export { FileCache };
