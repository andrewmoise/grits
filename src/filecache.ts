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
    fileAddr: string;
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

    public async addFile(inFilename: string, inFileAddr: string|null = null)
        : Promise<CachedFile>
    {
        console.log(`addFile(${inFilename})`);

        const fileStats = await fsPromises.stat(inFilename);
        
        const hash = crypto.createHash('sha256');
        const input = fs.createReadStream(inFilename);

        for await (const chunk of input)
            hash.update(chunk);

        const computedHexHash = hash.digest('hex');
        const computedSize = fileStats.size;

        let inFileHash, inFileSize;
        if (!inFileAddr) {
            inFileHash = computedHexHash;
            inFileSize = computedSize;
        } else {
            console.log(inFileAddr);
            
            [inFileHash, inFileSize] = inFileAddr.split(':');
            inFileSize = Number(inFileSize);

            console.log(`Verify hash: ${inFileHash} <-> ${computedHexHash} and ${inFileSize} <-> ${computedSize}`);
            
            if (inFileHash !== computedHexHash || inFileSize !== computedSize) {
                throw new Error('Hash or size does not match the provided file address');
            }
        }

        const finalFileAddr = `${inFileHash}:${inFileSize}`;
        const newFilePath = path.join(this.cacheDir, inFileHash);
        
        await fsPromises.rename(inFilename, newFilePath);
        await this._ensureSpaceFor(computedSize);
        const newFile: CachedFile = {
            path: newFilePath,
            size: computedSize,
            refCount: 0,
            fileAddr: finalFileAddr,
        };
        this.files.set(finalFileAddr, newFile);
        this.lru.unshift(finalFileAddr);
        this.currentSize += computedSize;

        console.log(`  Returning added file ${newFilePath}`);
        return newFile;
    }

    public async touch(fileAddr: string): Promise<void> {
        const release = await this.mutex.acquire();
        try {
            const index = this.lru.indexOf(fileAddr);
            assert(index != -1);

            this.lru.splice(index, 1);
            this.lru.unshift(fileAddr);
        } finally {
            release();
        }
    }

    public async readFile(fileAddr: string, offset: number, length: number)
        : Promise<Buffer | null>
    {
        const [hash] = fileAddr.split(':');
        console.log(`Finding file for ${hash}`);
        const file = this.files.get(fileAddr);
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
            await this.touch(fileAddr);
            console.log(`  Found ${file.path}, returning`);
            return buffer;
        } else {
            return null;
        }
    }
}

export { CachedFile, FileCache };
