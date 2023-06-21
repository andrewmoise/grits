export class Config {
    // General proxy configuration
    thisHost: string;
    thisPort: number;
    isRootNode: boolean;
    rootHost: string | null;
    rootPort: number | null;

    // Storage configuration
    storageDirectory: string;
    storageSize: number; // in bytes
    tempDownloadDirectory: string;

    // File-sharing params
    dhtNotifyNumber: number;
    dhtNotifyPeriod: number; // In seconds
    dhtMaxResponseNodes: number;
    
    // Traffic configuration
    maxUpstreamSpeed: number;   // bytes per second
    maxDownstreamSpeed: number; // bytes per second

    // Various less-relevant params
    maxProxyMapAge: number; // In seconds
    proxyMapCleanupPeriod: number; // In seconds
    proxyHeartbeatPeriod: number; // In seconds
    rootHeartbeatPeriod: number; // In seconds
    rootProxyDropTimeout: number; // In seconds
    
    constructor() {
        this.thisHost = '127.0.0.1';
        this.thisPort = 1787;
        this.isRootNode = false;
        this.rootHost = null;
        this.rootPort = null;
        
        this.maxProxyMapAge = 24 * 60 * 60;
        this.proxyMapCleanupPeriod = 60 * 60;
        this.proxyHeartbeatPeriod = 5;
        this.rootHeartbeatPeriod = 5;
        this.rootProxyDropTimeout = 30;

        this.storageDirectory = 'cache';
        this.storageSize = 20 * 1024 * 1024;
        this.tempDownloadDirectory = 'tmp-download';

        this.dhtNotifyNumber = 5;
        this.dhtNotifyPeriod = 30;
        this.dhtMaxResponseNodes = 10;
        
        this.maxUpstreamSpeed = 100 * 1024;   // 100 kb/s
        this.maxDownstreamSpeed = 100 * 1024; // 100 kb/s
    }
}
