import * as fs from 'fs';
import * as express from 'express';
import { Server } from 'http';

import { ProxyManagerBase } from "./proxy";
import { CachedFile, FileRetrievalError } from "./structures";

class HttpServer {
    private app: express.Express;
    private server: Server | null = null;
    private proxyManager: ProxyManagerBase;
    private port: number;

    constructor(proxyManager: ProxyManagerBase, port: number) {
        this.app = express();
        this.proxyManager = proxyManager;
        this.port = port;

        this.app.get('/blob/:fileAddr', async (req, res) => {
            const fileAddr = req.params.fileAddr;
            let file: CachedFile | null = null;

            proxyManager.logger.log(new Date(), 'web', "Web request: " + fileAddr);

            try {
                proxyManager.logger.log(new Date(), 'web',
                                 `Trying to serve ${fileAddr}`);
                file = await this.proxyManager.retrieveFile(fileAddr);
                res.setHeader('Content-Type', 'application/octet-stream');
                const readStream = fs.createReadStream(file.path);
                readStream.on('end', () => {
                    if (file) {
                        file.release();
                        file = null;
                    }
                });
                readStream.pipe(res);
            } catch (err) {
                proxyManager.logger.log(new Date(), 'web', `Retrieval error: ${err}`);
                if (err instanceof FileRetrievalError) {
                    res.status(404).send(err.message);
                } else {
                    res.status(500).send(`Internal Server Error: ${err}`);
                }
            } finally {
                // Make sure to release the file if it was successfully fetched
                if (file) {
                    file.release();
                    file = null;
                }
            }
        });
    }

    start(): void {
        this.server = this.app.listen(
            this.port, () => this.proxyManager.logger.log(
                new Date(), 'web', `HTTP server started on port ${this.port}`));
    }

    stop(): void {
        if (this.server) {
            this.server.close(() => this.proxyManager.logger.log(
                new Date(), 'web', `HTTP server stopped on port ${this.port}`));
        }
    }
}

export { HttpServer };
