export class Config {
    // General proxy configuration
    thisHost: string;
    thisPort: number;
    isRootNode: boolean;
    rootHost: string;
    rootPort: number;
    logFile: string;
    
    // Storage configuration
    storageDirectory: string;
    storageSize: number; // in bytes
    tempDownloadDirectory: string;

    // File-sharing params
    dhtNotifyNumber: number;
    dhtNotifyPeriod: number; // In seconds
    dhtMaxResponseNodes: number;
    
    // Download params
    maxBursts: number;
    defaultBandwidth: number; // assumed bandwidth in bytes/s when no info
    downloadTickPeriod: number; // in ms
    burstTimeout: number; // in ms
    
    // Traffic configuration
    maxUpstreamSpeed: number;   // bytes per second
    maxDownstreamSpeed: number; // bytes per second
    maxDownloadStreams: number;
    
    // Various less-relevant params
    maxProxyMapAge: number; // In seconds
    proxyMapCleanupPeriod: number; // In seconds
    proxyHeartbeatPeriod: number; // In seconds
    rootUpdatePeerListPeriod: number; // In seconds
    rootProxyDropTimeout: number; // In seconds
    
    constructor(rootHost: string, rootPort: number) {
        this.thisHost = '127.0.0.1';
        this.thisPort = 1787;
        this.isRootNode = false;
        this.rootHost = rootHost;
        this.rootPort = rootPort;

        this.logFile = 'grits.log';
        
        this.storageDirectory = 'cache';
        this.storageSize = 20 * 1024 * 1024;
        this.tempDownloadDirectory = 'tmp-download';

        this.dhtNotifyNumber = 5;
        this.dhtNotifyPeriod = 5;
        this.dhtMaxResponseNodes = 10;

        this.maxBursts = 5;
        this.defaultBandwidth = 100 * 1024;
        this.downloadTickPeriod = 100;
        this.burstTimeout = 500;
        
        this.maxUpstreamSpeed = 100 * 1024;   // 100 kb/s
        this.maxDownstreamSpeed = 100 * 1024; // 100 kb/s
        this.maxDownloadStreams = 10;

        this.maxProxyMapAge = 24 * 60 * 60;
        this.proxyMapCleanupPeriod = 60 * 60;
        this.proxyHeartbeatPeriod = 20;
        this.rootUpdatePeerListPeriod = 5;
        this.rootProxyDropTimeout = 180;
    }
}
