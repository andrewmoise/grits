export class Config {
    // General proxy configuration
    thisHost: string;
    thisPort: number;
    isRootNode: boolean;
    proxyHeartbeatPeriod: number; // In seconds
    rootHost: string | null;
    rootPort: number | null;

    // Root proxy configuration
    rootHeartbeatPeriod: number; // In seconds
    rootProxyDropTimeout: number;

    // Storage configuration
    storageDirectory: string;
    storageSize: number; // in bytes

    constructor() {
        this.thisHost = '127.0.0.1';
        this.thisPort = 1787;
        this.isRootNode = false;
        this.proxyHeartbeatPeriod = 5; // In seconds
        this.rootHost = null;
        this.rootPort = null;

        this.rootHeartbeatPeriod = 5; // In seconds
        this.rootProxyDropTimeout = 30;

        this.storageDirectory = 'cache';
        this.storageSize = 20;
    }
}
