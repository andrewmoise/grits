import { Config } from '../src/config';
import { FileCache } from '../src/filecache';
import * as fs from 'fs/promises';
import * as path from 'path';
import * as os from 'os';
import * as crypto from 'crypto';

test('add a file and read from the cache', async () => {
    // Arrange
    const cacheDir = await fs.mkdtemp(path.join(os.tmpdir(), 'cache-'));
    const config = new Config('127.0.0.1', 1787);
    config.storageDirectory = cacheDir;
    config.storageSize = 5000000; // 5MB cache size
    const fileCache = new FileCache(config);

    const testFileContent = 'Hello, World!';
    const testFilePath = path.join(cacheDir, 'testFile');
    await fs.writeFile(testFilePath, testFileContent);

    // Act
    const writtenFile = await fileCache.addFile(testFilePath);
    const cachedFile = await fileCache.readFile(writtenFile.fileAddr);
    
    expect(cachedFile).not.toBeNull();

    const buffer = await cachedFile!.read(0, testFileContent.length);
    
    // Assert
    expect(buffer).not.toBeNull();
    expect(buffer!.toString()).toEqual(testFileContent);

    // Clean up
    await fs.rm(cacheDir, { recursive: true });
});

test('add files to overflow the cache and verify the cache behaves correctly',
    async () => {
        // Arrange
        const cacheDir = await fs.mkdtemp(path.join(os.tmpdir(), 'cache-'));

        const config = new Config('127.0.0.1', 1787);
        config.storageDirectory = cacheDir;
        config.storageSize = 1000000; // 1MB cache size

        const fileCache = new FileCache(config);
        const numberOfFiles = 20;
        const fileSize = 100000; // 100kB
        const fileHashes = [];

        // Act
        // Write files to the cache
        for (let i = 0; i < numberOfFiles; i++) {
            const testFileContent = String(i).padStart(2, '0').repeat(
                fileSize / 2);
            const testFilePath = path.join(cacheDir, 'testFile' + i);
            await fs.writeFile(testFilePath, testFileContent);

            const writtenFile = await fileCache.addFile(testFilePath);
            fileHashes.push(writtenFile.fileAddr);
        }

        // Assert
        // Check that the first 10 files are not in the cache
        for (let i = 0; i < 10; i++) {
            const cachedFile = await fileCache.readFile(fileHashes[i]);
            expect(cachedFile).toBeNull();
        }

        // Check that the last 10 files are in the cache
        for (let i = 10; i < numberOfFiles; i++) {
            const cachedFile = await fileCache.readFile(fileHashes[i]);
            expect(cachedFile).not.toBeNull();

            const buffer = await cachedFile!.read(0, fileSize);
            expect(buffer).not.toBeNull();
            
            const testFileContent = String(i).padStart(2, '0').repeat(
                fileSize / 2);
            expect(buffer!.toString()).toEqual(testFileContent);
        }

        // Clean up
        await fs.rm(cacheDir, { recursive: true });
    });
