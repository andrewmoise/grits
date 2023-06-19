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
    tempDownloadDirectory: string;

    // Traffic configuration
    maxUpstreamSpeed: number;   // bytes per second
    maxDownstreamSpeed: number; // bytes per second

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
        this.tempDownloadDirectory = 'tmp-download';

        this.maxUpstreamSpeed = 100000;   // 100 kb/s
        this.maxDownstreamSpeed = 100000; // 100 kb/s
    }
}
