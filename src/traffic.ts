import { performance } from 'perf_hooks';
import { promisify } from 'util';
const sleep = promisify(setTimeout);

import { Config } from './config';

class UpstreamManager {
    private maxUpstream: number;
    private maxDownstream: number;
    private upstreamBudget: number;
    private downstreamBudget: number;
    private lastUpdate: number;

    constructor(config: Config) {
        this.maxUpstream = Math.floor(config.maxUpstreamSpeed / 1000);
        this.maxDownstream = Math.floor(config.maxDownstreamSpeed / 1000);
        this.upstreamBudget = 0;
        this.downstreamBudget = 0;
        this.lastUpdate = performance.now();
    }

    private updateBudgets() {
        const now = performance.now();
        const deltaTime = now - this.lastUpdate;
        this.lastUpdate = now;

        this.upstreamBudget = Math.max(
            0, this.upstreamBudget - this.maxUpstream * deltaTime);
        this.downstreamBudget = Math.max(
            0, this.downstreamBudget - this.maxDownstream * deltaTime);
    }

    public async requestUpload(bytes: number) {
        this.updateBudgets();

        const timeToWait = this.upstreamBudget;
        this.upstreamBudget += bytes;
        if (timeToWait > 0)
            await sleep(timeToWait);
    }

    public async requestDownload(bytes: number) {
        this.updateBudgets();

        const timeToWait = this.downstreamBudget;
        this.downstreamBudget += bytes;
        if (timeToWait > 0)
            await sleep(timeToWait);
    }
}

export { UpstreamManager };
