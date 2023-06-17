import { FileCache } from '../src/cache'; // replace with your actual file path
import * as fs from 'fs/promises';
import * as path from 'path';
import * as os from 'os';
import * as crypto from 'crypto';

test('add a file and read from the cache', async () => {
    // Arrange
    const cacheDir = await fs.mkdtemp(path.join(os.tmpdir(), 'cache-'));
    const fileCache = new FileCache(cacheDir, 5000000); // 5MB cache size

    const testFileContent = 'Hello, World!';
    const testFilePath = path.join(cacheDir, 'testFile');
    await fs.writeFile(testFilePath, testFileContent);

    const hash = crypto.createHash('sha1');
    hash.update(testFileContent);
    const fileHash = hash.digest('hex');

    // Act
    await fileCache.addFile(testFilePath, fileHash);

    const buffer = await fileCache.readFile(fileHash, 0,
        testFileContent.length);

    // Assert
    expect(buffer !== null);
    expect(buffer!.toString()).toEqual(testFileContent);

    // Clean up
    await fs.rm(cacheDir, { recursive: true });
});

test('add files to overflow the cache and verify the cache behaves correctly',
    async () => {
        // Arrange
        const cacheDir = await fs.mkdtemp(path.join(os.tmpdir(), 'cache-'));
        const fileCache = new FileCache(cacheDir, 1000000); // 1MB cache size
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

            const hash = crypto.createHash('sha1');
            hash.update(testFileContent);
            const fileHash = hash.digest('hex');

            await fileCache.addFile(testFilePath, fileHash);
            fileHashes.push(fileHash);
        }

        // Assert
        // Check that the first 10 files are not in the cache
        for (let i = 0; i < 10; i++) {
            const buffer = await fileCache.readFile(
                fileHashes[i], 0, fileSize);
            expect(buffer === null);
        }

        // Check that the last 10 files are in the cache
        for (let i = 10; i < numberOfFiles; i++) {
            const buffer = await fileCache.readFile(
                fileHashes[i], 0, fileSize);
            expect(buffer !== null);
            const testFileContent = String(i).padStart(2, '0').repeat(
                fileSize / 2);
            expect(buffer!.toString()).toEqual(testFileContent);
        }

        // Clean up
        await fs.rm(cacheDir, { recursive: true });
    });
