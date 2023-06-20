import * as fs from 'fs';
import * as express from 'express';
import { Server } from 'http';
import { FileCache, CachedFile } from './filecache';

class HttpServer {
    private app: express.Express;
    private server: Server | null = null;
    private fileCache: FileCache;
    private port: number;

    constructor(fileCache: FileCache, port: number) {
        this.app = express();
        this.fileCache = fileCache;
        this.port = port;

        this.app.get('/blob/:fileAddr', async (req, res) => {
            const fileAddr = req.params.fileAddr;
            let file: CachedFile | null = null;

            console.log("Web request: " + fileAddr);

            try {
                file = await this.fileCache.readFile(fileAddr);
                if (file === null) {
                    res.status(404).send('File not found in cache');
                    return;
                }

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
                console.error(err);
                res.status(500).send('Internal Server Error');
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
        this.server = this.app.listen(this.port, () => console.log(`HTTP server started on port ${this.port}`));
    }

    stop(): void {
        if (this.server) {
            this.server.close(() => console.log(`HTTP server stopped on port ${this.port}`));
        }
    }
}

export { HttpServer };
