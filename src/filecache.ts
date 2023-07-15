import * as assert from 'assert';
import * as crypto from 'crypto';
import * as fs from 'fs';
import { promises as fsPromises } from 'fs';
import * as path from 'path';
import { Mutex } from 'async-mutex';

import { Config } from './config';
import { PeerProxy, CachedFile } from './structures';

class FileCache {
    private cacheDir: string;
    /*private*/ maxSize: number;
    /*private*/ currentSize: number;
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
        //console.log(`Trying to make space for ${size} / ${this.maxSize}`);
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

                const relativePath = path.relative(
                    this.cacheDir, removeFile.path);
                if (relativePath.startsWith('..')
                    || path.isAbsolute(relativePath))
                {
                    throw new Error(`Attempting to remove a file outside of the mutable cache directory: ${removeFile.path}`);
                }
                
                await fsPromises.unlink(removeFile.path);

                removeIndex--;
            }
        } finally {
            release();
        }
    }
        
    public getFiles(): IterableIterator<CachedFile> {
        return this.files.values();
    }

    // FIXME - This interface, and the whole thing of handling refCount from
    // the outside, has gotten unwieldy. This should be replaced with
    // a withFile() function that takes a callback which gets called with
    // the refCount held during execution.
    
    public async addFile(inFilename: string, inFileAddr: string|null = null,
                         moveFile: boolean = false,
                         takeReference: boolean = false,
                         addInPlace: boolean = false,
                         addSize: boolean = true)
        : Promise<CachedFile>
    {
        //console.log(`addFile(${inFilename})`);

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
            //console.log(inFileAddr);
            
            [inFileHash, inFileSize] = inFileAddr.split(':');
            inFileSize = Number(inFileSize);

            //console.log(`Verify hash: ${inFileHash} <-> ${computedHexHash} and ${inFileSize} <-> ${computedSize}`);
            
            if (inFileHash !== computedHexHash || inFileSize !== computedSize) {
                throw new Error('Hash or size does not match the provided file address');
            }
        }

        const finalFileAddr = `${inFileHash}:${inFileSize}`;
        
        // Check if the file already exists in the cache
        const existingFile = this.files.get(finalFileAddr);
        if (existingFile) {
            if (takeReference)
                existingFile.refCount++;
            return existingFile;
        }

        // Put the file in the mutable cache, if necessary
        let newFilePath;
        if (addInPlace)
            newFilePath = inFilename;
        else
            newFilePath = path.join(this.cacheDir, inFileHash);

        if (moveFile) {
            try {
                await fsPromises.rename(inFilename, newFilePath);
            } catch (err) {
                console.error(`Failed to rename file: ${err}`);
                throw err; // You can rethrow the error to stop execution if required
            }
        } else {
            await fsPromises.copyFile(inFilename, newFilePath);
        }
        
        await this._ensureSpaceFor(computedSize);
        const newFile = new CachedFile(newFilePath, computedSize,
                                       takeReference ? 1 : 0,
                                       finalFileAddr);

        this.files.set(finalFileAddr, newFile);
        this.lru.unshift(finalFileAddr);

        if (addSize)
            this.currentSize += computedSize;

        //console.log(`  Returning added file ${newFilePath}`);
        return newFile;
    }

    public async addDirectory(directoryPath: string, moveFiles: boolean = false)
        : Promise<Array<CachedFile>>
    {
        const addedFiles: Array<CachedFile> = [];

        // Read the content of the directory
        const directoryContent = await fsPromises.readdir(directoryPath, { withFileTypes: true });

        for (const dirent of directoryContent) {
            const fullPath = path.join(directoryPath, dirent.name);

            console.log(`Adding ${fullPath}`);
            
            if (dirent.isFile()) {
                // If it's a file, add it to the cache
                const cachedFile = await this.addFile(fullPath, null, moveFiles);
                addedFiles.push(cachedFile);
            } else if (dirent.isDirectory()) {
                // If it's a directory, recursively add its content to the cache
                const cachedFilesFromDir = await this.addDirectory(fullPath, moveFiles);
                addedFiles.push(...cachedFilesFromDir);
            }
        }

        return addedFiles;
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

    public async readFile(fileAddr: string)
        : Promise<CachedFile | null>
    {
        //console.log(`Finding file for ${fileAddr}`);
        const file = this.files.get(fileAddr);
        if (file) {
            file.refCount += 1;
            await this.touch(file.fileAddr);
            return file;
        } else {
            return null;
        }
    }
}

export { FileCache };
