import * as assert from 'assert';
import * as crypto from 'crypto';
import * as fs from 'fs';
import { promises as fsPromises } from 'fs';
import * as path from 'path';

import { Config } from './config';

import { Mutex } from 'async-mutex';

interface CachedFile {
    path: string;
    size: number;
    refCount: number;
    hexHash: string;
}

class FileCache {
    private cacheDir: string;
    private maxSize: number;
    private currentSize: number;
    private lru: string[];
    private files: Map<string, CachedFile>;
    private mutex: Mutex;

    constructor(config: Config) {
        this.cacheDir = config.storageDirectory;
        this.maxSize = config.storageSize;
        this.currentSize = 0;
        this.lru = [];
        this.files = new Map<string, CachedFile>();
        this.mutex = new Mutex();
    }

    private async _ensureSpaceFor(size: number) {
        console.log(`Trying to make space for ${size} / ${this.maxSize}`);
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
                await fsPromises.unlink(removeFile.path);

                removeIndex--;
            }
        } finally {
            release();
        }
    }

    public async addFile(inFilename: string, inHexHash: string|null = null)
        : Promise<CachedFile>
    {
        console.log(`addFile(${inFilename})`);
        
        if (!inHexHash) {
            const hash = crypto.createHash('sha256');
            const input = fs.createReadStream(inFilename);

            for await (const chunk of input) {
                hash.update(chunk);
            }
        
            inHexHash = hash.digest('hex');
        }
        
        const newFilePath = path.join(this.cacheDir, inHexHash);
        await fsPromises.rename(inFilename, newFilePath);
        const fileStats = await fsPromises.stat(newFilePath);
        await this._ensureSpaceFor(fileStats.size);
        const newFile: CachedFile = {
            path: newFilePath,
            size: fileStats.size,
            refCount: 0,
            hexHash: inHexHash,
        };
        this.files.set(inHexHash, newFile);
        this.lru.unshift(inHexHash);
        this.currentSize += fileStats.size;

        console.log(`  Returning added file ${newFilePath}`);
        return newFile;
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
        : Promise<Buffer | null>
    {
        console.log(`Finding file for ${hash}`);
        const file = this.files.get(hash);
        if (file) {
            file.refCount += 1;
            const buffer = Buffer.allocUnsafe(length);
            try {
                const fd = await fsPromises.open(file.path, 'r');
                try {
                    await fd.read(buffer, 0, length, offset);
                } finally {
                    await fd.close();
                }
            } finally {
                file.refCount -= 1;
            }
            await this.touch(hash);
            console.log(`  Found ${file.path}, returning`);
            return buffer;
        } else {
            return null;
        }
    }
}

export { CachedFile, FileCache };
