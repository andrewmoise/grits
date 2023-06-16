export class Config {
    thisHost: string;
    thisPort: number;
    isRootNode: boolean;
    rootHeartbeatPeriod: number; // In seconds
    proxyHeartbeatPeriod: number; // In seconds
    rootHost: string | null;
    rootPort: number | null;
    proxyLastSeenDropLimit: number;

    constructor() {
        this.thisHost = '127.0.0.1';
        this.thisPort = 1787;
        this.isRootNode = false;
        this.rootHeartbeatPeriod = 5; // In seconds
        this.proxyHeartbeatPeriod = 5; // In seconds
        this.rootHost = null;
        this.rootPort = null;
        this.proxyLastSeenDropLimit = 30;
    }
}
