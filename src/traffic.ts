import { performance } from 'perf_hooks';
import { promisify } from 'util';
const sleep = promisify(setTimeout);

import { Config } from './config';

class UpstreamManager {
    private upstreamBudget: number;
    private lastUpdate: number;

    private config: Config;
    
    constructor(config: Config) {
        this.upstreamBudget = 0;
        this.lastUpdate = performance.now();

        this.config = config;
    }

    private updateBudgets() {
        const now = performance.now();
        const deltaTime = (now - this.lastUpdate) / 1000;
        this.lastUpdate = now;

        this.upstreamBudget = Math.max(
            0, this.upstreamBudget - this.config.maxUpstreamSpeed * deltaTime);
    }

    public async requestUpload(bytes: number): Promise<number> {
        this.updateBudgets();

        const maxBytes = Math.min(bytes, this.config.maxUpstreamQueue);
        this.upstreamBudget += maxBytes;

        if (this.upstreamBudget >= this.config.maxUpstreamQueue+1) {
            const waitTime =
                (this.upstreamBudget - this.config.maxUpstreamQueue / 2)
                / this.config.maxUpstreamSpeed * 1000;
            await sleep(waitTime);
        }

        return maxBytes;
    }   
}

export { UpstreamManager };
