import { performance } from 'perf_hooks';
import { promisify } from 'util';
const sleep = promisify(setTimeout);

import { Config } from './config';

class UpstreamManager {
    private maxUpstream: number;
    private upstreamBudget: number;
    private lastUpdate: number;

    private config: Config;
    
    constructor(config: Config) {
        this.maxUpstream = Math.floor(config.maxUpstreamSpeed / 1000);
        this.upstreamBudget = 0;
        this.lastUpdate = performance.now();

        this.config = config;
    }

    private updateBudgets() {
        const now = performance.now();
        const deltaTime = now - this.lastUpdate;
        this.lastUpdate = now;

        this.upstreamBudget = Math.max(
            0, this.upstreamBudget - this.maxUpstream * deltaTime);
    }

    public async requestUpload(bytes: number): Promise<number> {
        this.updateBudgets();

        if (this.upstreamBudget < this.config.maxUpstreamQueue) {
            const result = Math.min(
                bytes, this.config.maxUpstreamQueue - this.upstreamBudget);
            this.upstreamBudget += result;
            return result;
        } else {
            const waitTime =
                (this.upstreamBudget - this.config.maxUpstreamQueue)
                / this.maxUpstream * 1000;
            const result = Math.min(bytes, this.config.maxUpstreamQueue);
            this.upstreamBudget += result;
            await sleep(waitTime);
            return result;
        }
    }   
}

export { UpstreamManager };
