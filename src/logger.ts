import * as fs from 'fs';

import { Config } from './config';

class Logger {
    private fileHandle: fs.promises.FileHandle | null = null;
    private config: Config;

    private writeQueue: string[];
    private writeError: any | null;
    private writePromise: Promise<void> | null;
    
    constructor(config: Config) {
        this.config = config;
        
        this.writeQueue = [];
        this.writeError = null;
        this.writePromise = null;
    }

    async start(): Promise<void> {
        this.fileHandle = await fs.promises.open(this.config.logFile, 'a');
    }

    log(segmentId: string, message: string): void {
        if (!this.fileHandle) {
            throw new Error('Logger not started - call start() first');
        }

        const formattedTimestamp = new Date().toISOString();
        const formattedMessage = `${formattedTimestamp} ${segmentId} ${this.config.thisHost}:${this.config.thisPort} ${message}\n`;

        this.writeQueue.push(formattedMessage);
        if (this.writePromise === null)
            this.writePromise = this.processQueue();

        if (this.writeError) {
            const error = this.writeError;
            this.writeError = null;
            throw error;
        }
    }

    private async processQueue(): Promise<void> {
        while (this.writeQueue.length > 0) {
            try {
                const message = this.writeQueue.shift();
                await this.fileHandle!.write(message!);
            } catch (err) {
                this.writeError = err;
                throw err;
            }
        }
        this.writePromise = null;
    }
    
    async stop(): Promise<void> {
        while (this.writePromise)
            await this.writePromise;
            
        if (this.fileHandle) {
            await this.fileHandle.close();
            this.fileHandle = null;
        }
    }
}

export {
    Logger
};
