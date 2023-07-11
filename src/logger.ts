import * as fs from 'fs';

import { Config } from './config';

class Logger {
    private fileHandle: fs.promises.FileHandle | null = null;
    private config: Config;
    private queue: Promise<void>;  // Our queue is a promise chain

    constructor(config: Config) {
        this.config = config;
        this.queue = Promise.resolve();  // Initialize the promise chain
    }

    async start(): Promise<void> {
        this.fileHandle = await fs.promises.open(this.config.logFile, 'a');
    }

    log(timestamp: Date, transferId: string, message: string): void {
        if (!this.fileHandle) {
            throw new Error('Logger not started - call start() first');
        }

        const formattedTimestamp = timestamp.toISOString();
        const formattedMessage = `${formattedTimestamp} ${transferId} ${this.config.thisHost}:${this.config.thisPort} ${message}\n`;

        // Append a new task to the queue. This task will wait for all previous tasks to finish before running
        this.queue = this.queue.then(() => this.fileHandle!.writeFile(
            formattedMessage));
    }

    async stop(): Promise<void> {
        // Wait for all remaining tasks to finish before stopping
        await this.queue;

        if (this.fileHandle) {
            await this.fileHandle.close();
            this.fileHandle = null;
        }
    }
}

export {
    Logger
};
