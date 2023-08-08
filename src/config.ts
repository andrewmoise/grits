export class Config {
    // General proxy configuration
    thisHost: string = '127.0.0.1';
    thisPort: number = 1787;
    isRootNode: boolean = false;
    rootHost: string;
    rootPort: number;
    logFile: string = 'grits.log';
    
    // Storage configuration
    storageDirectory: string = 'cache';
    storageSize: number = 20 * 1024 * 1024; // in bytes
    tempDownloadDirectory: string = 'tmp-download';

    // DHT params
    dhtNotifyNumber: number = 5;
    dhtNotifyPeriod: number = 20; // In seconds
    dhtMaxResponseNodes: number = 10;
    dhtRefreshTime: number = 8 * 60 * 60; // Seconds
    dhtExpiryTime: number = 24 * 60 * 60; // Seconds
    
    // Traffic configuration
    maxUpstreamSpeed: number = 100 * 1024;   // bytes per second
    maxDownstreamSpeed: number = 100 * 1024; // bytes per second
    performanceUpdateStiffness: number = 0.95;
    telemetryFetchRetries: number = 3;

    // Degraded-network config
    degradeUpstreamSpeed: number | null = null;
    degradeUpstreamQueue: number = 10000;
    degradeDownstreamSpeed: number | null = null;
    degradeDownstreamQueue: number = 10000;
    degradeLatency: number = 0;
    degradeJitter: number = 0;
    degradePacketLoss: number = 0;
    
    // Various less-relevant params
    maxProxyMapAge: number = 24 * 60 * 60; // In seconds
    proxyMapCleanupPeriod: number = 60 * 60; // In seconds
    proxyHeartbeatPeriod: number = 10; // In seconds
    rootUpdatePeerListPeriod: number = 8; // In seconds
    rootProxyDropTimeout: number = 180; // In seconds
    
    constructor(rootHost: string, rootPort: number) {
        this.rootHost = rootHost;
        this.rootPort = rootPort;
    }
}
