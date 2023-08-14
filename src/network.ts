import * as dgram from 'dgram';
import * as os from 'os';

import { performance } from 'perf_hooks';

import { promisify } from 'util';
const sleep = promisify(setTimeout);

import { Config } from './config';
import { Logger } from './logger';

import {
    AllPeerNodes, PeerNode, TelemetryInfo, DOWNLOAD_CHUNK_SIZE
} from './structures';

import {
    TrafficManager, TrafficManagerImpl,
    QUEUE_DEPTH_LIMIT, QUEUE_CHECK_RATE, QUEUE_MESSAGE_LIMIT, TrafficShaper,
} from "./traffic";

import {
    MessageType,
    Message,
    
    HeartbeatMessage,
    HeartbeatResponse,
    DataFetchMessage,
    DataFetchResponseOk,
    DataFetchResponseNo,
    DhtStoreMessage,
    DhtStoreResponse,
    DhtLookupMessage,
    DhtLookupResponse,
    TelemetryFetchMessage,
    TelemetryFetchResponse,
} from './messages';

const MAGIC_BYTES = Buffer.from([140, 98]);

const DEFAULT_TIMEOUT: number = 500;

type RequestHandler = (request: InRequest, message: Message) => Promise<void>;

interface QueuedTransfer {
    peerNodes: PeerNode[];
    attemptRequest: () => boolean;
}

interface AllowedTransfer {
    source: PeerNode;
    downloadBytesAllowed: number;
    allTelemetryBatchId: number;
    peerTelemetryBatchId: number;
}

interface NetworkManager {
    start(): void;
    stop(): void;

    newRequest(peerNode: PeerNode, message: Message): OutRequest;
    
    registerRequestHandler(
        type: number,
        handler: RequestHandler,
    ): void;
    unregisterRequestHandler(type: number): void;

    requestTransfer(source: PeerNode, message: Message)
    : Promise<AllowedTransfer>;
    
    requestDownload(sources: PeerNode[], bytes: number)
    : Promise<AllowedTransfer[] | null>;
    
    newTelemetryId(): number;
    log(msg: string): void;
}

class InRequest {
    networkManager: NetworkManagerImpl;
    requestId: number;
    source: PeerNode;
    message: Message;
    
    constructor(manager: NetworkManagerImpl,
                source: PeerNode,
                id: number, message: Message)
    {
        this.networkManager = manager;
        this.requestId = id;
        this.source = source;
        this.message = message;
    }
    
    sendResponse(message: Message): void {
        this.networkManager.log(`Send response, message ${message.type}, request ID ${this.requestId}`);
        this.networkManager.send(message, this.requestId, this.source);
    }
}

class OutRequest {
    networkManager: NetworkManagerImpl;
    requestId: number;
    dest: PeerNode;
    
    timeoutTimeout: NodeJS.Timeout | null;
    sendTimestamp: number | null = null;
    firstResponseTimestamp: number | null = null;
    
    messageQueue: Message[] = [];
    resolver: ((msg: Message | null) => void) | null = null;
    isClosed: boolean = false;

    constructor(networkManager: NetworkManagerImpl,
                requestId: number,
                dest: PeerNode)
    {
        networkManager.log('OutRequest constructor');

        this.networkManager = networkManager;
        this.requestId = requestId;
        this.dest = dest;
        
        this.timeoutTimeout = setTimeout(
            this.timeoutFn.bind(this), DEFAULT_TIMEOUT);
        
        this.networkManager.outRequests.set(this.requestId, this);
    }
    
    sendRequest(message: Message): void {
        this.networkManager.log('OutRequest sendRequest()');
        
        this.sendTimestamp = performance.now();
        this.networkManager.send(message, this.requestId, this.dest);
    }

    getResponse(): Promise<Message | null> {
        this.networkManager.log(`OutRequest getResponse() - queue len ${this.messageQueue.length}`);
        
        if (this.isClosed) {
            throw new Error("The request is already closed.");
        }
        if (this.resolver) {
            throw new Error("Only one call to getResponse() can be pending.");
        }
        
        if (this.messageQueue.length > 0) {
            return Promise.resolve(this.messageQueue.shift()!);
        } else {
            return new Promise(resolve => {
                this.resolver = resolve;
            });
        }
    }
    
    handleMessage(message: Message): void {
        this.networkManager.log('OutRequest handleMessage()');
        
        if (this.firstResponseTimestamp === null) {
            this.firstResponseTimestamp = performance.now();
            const peerTrafficManager = this.networkManager.getTrafficManager(this.dest);
            peerTrafficManager.notifyLatency(
                this.firstResponseTimestamp - this.sendTimestamp!);
            peerTrafficManager.notifyPacketLoss(0);
        }
        
        // Restart the timeout timer
        if (this.timeoutTimeout) {
            clearTimeout(this.timeoutTimeout);
            this.timeoutTimeout = setTimeout(
                this.timeoutFn.bind(this), DEFAULT_TIMEOUT);
        }

        // Handle the message
        if (this.resolver) {
            this.resolver(message);
            this.resolver = null;
        } else {
            this.messageQueue.push(message);
        }
    }
    
    close(): void {
        this.isClosed = true;

        // Clear the timeout
        if (this.timeoutTimeout) {
            clearTimeout(this.timeoutTimeout);
            this.timeoutTimeout = null;
        }

        // Resolve the resolver if any
        if (this.resolver) {
            this.resolver(null);
            this.resolver = null;
        }

        this.networkManager.outRequests.delete(this.requestId);
    }

    private timeoutFn() {
        this.networkManager.log('OutRequest timeout!');
        
        this.timeoutTimeout = null;

        if (this.firstResponseTimestamp === null) {
            this.networkManager.getTrafficManager(this.dest).notifyPacketLoss(1);
        }
        
        if (this.resolver) {
            this.resolver(null);
            this.resolver = null;
        }
    }
}

class NetworkManagerImpl {
    allPeerNodes: AllPeerNodes;
    logger: Logger;
    config: Config;

    outRequests: Map<number, OutRequest>;
    nextRequestId: number;
    
    requestHandlers: Map<number, RequestHandler>;
    socket: dgram.Socket | null;

    nextTelemetryBatchId: number;
    allTrafficManager: TrafficManagerImpl;

    requestQueue: QueuedTransfer[];
    processRequestQueueTimeout: NodeJS.Timeout | undefined;

    degradeDownstreamShaper: TrafficShaper | null = null;
    degradeUpstreamShaper: TrafficShaper | null = null;

    constructor(allPeerNodes: AllPeerNodes, logger: Logger, config: Config) {
        if (config.thisHost === null) {
            let addresses = getLocalIPAddresses();
            if (addresses.length !== 1) {
                throw new Error('Expected 1 local address, but found ' + addresses.length);
            }
            config.thisHost = addresses[0];
        }

        this.allPeerNodes = allPeerNodes;
        this.logger = logger;
        this.config = config;

        this.outRequests = new Map();
        this.nextRequestId = 0;
        
        this.requestHandlers = new Map();
        this.socket = null;

        this.nextTelemetryBatchId = 0;
        this.allTrafficManager = new TrafficManagerImpl(this, config);

        this.requestQueue = [];
        this.processRequestQueueTimeout = undefined;
    }

    start(): void {
        const socketOptions: dgram.SocketOptions = {
            type: 'udp4',
            reuseAddr: true,
        };

        this.socket = dgram.createSocket(socketOptions);
        this.socket.bind(this.config.thisPort, this.config.thisHost);

        this.socket.on(
            'message',
            this.degradeIncomingPacket.bind(this));

        this.registerRequestHandler(
            MessageType.TELEMETRY_FETCH_MESSAGE,
            this.handleTelemetryFetchMessage.bind(this));
    }
  
    stop(): void {
        this.unregisterRequestHandler(
            MessageType.TELEMETRY_FETCH_MESSAGE);
        
        if (this.socket) {
            this.socket.close();
            this.socket = null;
        }
    }

    newRequest(peerNode: PeerNode, message: Message): OutRequest {
        this.log(`Create request ${message} for ${peerNode.ip}:${peerNode.port}`);
        const requestId = this.nextRequestId++;
        const result = new OutRequest(this, requestId, peerNode);
        result.sendRequest(message);
        return result;
    }
    
    send(message: Message, requestId: number, peerNode: PeerNode): void {
        this.log(`Send request ${message} to ${peerNode.ip}:${peerNode.port}`);
        const encodedContent = message.encode();

        const headerBuffer = Buffer.allocUnsafe(13);
        let offset = 0;

        headerBuffer.writeInt32BE(requestId, offset);
        offset += 4;

        headerBuffer.writeInt8(message.type, offset);
        offset++;
        
        headerBuffer.writeInt32BE(
            this.allTrafficManager.upstreamTelemetryBatchId, offset);
        offset += 4;

        const peerTrafficManager = this.getTrafficManager(peerNode);

        headerBuffer.writeInt32BE(
            peerTrafficManager.upstreamTelemetryBatchId, offset);
        offset += 4;
        
        const encodedMessage = Buffer.concat([
            MAGIC_BYTES,
            headerBuffer,
            encodedContent,
        ]);

        if (!this.socket) {
            this.log('Socket is not initialized');
            return;
        }

        let send = () => {
            this.socket!.send(encodedMessage, 0, encodedMessage.length,
                             peerNode.port, peerNode.ip,
                             (error) => {
                                 if (error) {
                                     this.log(`Error sending message: ${error}`);
                                     return;
                                 }
                             });
        };

        if (!this.config.degradeUpstreamSpeed) {
            send();
        } else {
            if (!this.degradeUpstreamShaper)
                this.degradeUpstreamShaper = new TrafficShaper(
                    this.config.degradeUpstreamSpeed);

            let delay = this.degradeBandwidthDelay(
                encodedMessage.length, this.degradeUpstreamShaper,
                this.config.degradeUpstreamSpeed,
                this.config.degradeUpstreamQueue);
            if (delay === null)
                return;                
                    
            setTimeout(send, delay);
        }
    }

    degradeBandwidthDelay(
        bytes: number, trafficShaper: TrafficShaper, maxSpeed: number,
        queueDepth: number)
    : number | null {
        trafficShaper.updateBudget();
        trafficShaper.maxSpeed = maxSpeed;
        if (trafficShaper.bytesBudgeted + 28 + bytes > queueDepth) {
            return null;
        } else {
            trafficShaper.consume(28 + bytes);
            return trafficShaper.bytesBudgeted / maxSpeed * 1000;
        }
    }

    degradeIncomingPacket(data: Buffer, rinfo: dgram.RemoteInfo): void {
        if (!this.config.degradeDownstreamSpeed
            && !this.config.degradeLatency
            && !this.config.degradeJitter
            && !this.config.degradePacketLoss)
        {
            this.handleIncomingPacket(data, rinfo);
        } else {
            // We have some kind of degradation to apply.
            let delay: number | null = 0;

            if (this.config.degradeDownstreamSpeed) {
                if (!this.degradeDownstreamShaper)
                    this.degradeDownstreamShaper = new TrafficShaper(
                        this.config.degradeDownstreamSpeed);

                delay = this.degradeBandwidthDelay(
                    data.length, this.degradeDownstreamShaper,
                    this.config.degradeDownstreamSpeed,
                    this.config.degradeDownstreamQueue);
                if (delay === null)
                    return;                
            }

            if (Math.random() < this.config.degradePacketLoss)
                return;
            
            delay += this.config.degradeLatency;
            delay += Math.random() * this.config.degradeJitter;

            setTimeout(() => { this.handleIncomingPacket(data, rinfo) }, delay);
        }

    }
    
    handleIncomingPacket(data: Buffer, rinfo: dgram.RemoteInfo): void {
        const { address, port } = rinfo;

        let offset: number = 0;
        
        const magicNumber = data.slice(offset, MAGIC_BYTES.length);
        if (!magicNumber.equals(MAGIC_BYTES))
            throw new Error('Invalid magic number');
        offset += MAGIC_BYTES.length;
        
        const requestId = data.readInt32BE(offset);
        offset += 4;
        const messageType = data.readUInt8(offset);
        offset++;
        const allTelemetryId = data.readInt32BE(offset);
        offset += 4;
        const peerTelemetryId = data.readInt32BE(offset);
        offset += 4;
        
        const messageData = data.slice(offset);
        
        let isInRequest: boolean;
        let message: Message;
        
        switch (messageType) {
            case MessageType.HEARTBEAT_MESSAGE:
                message = HeartbeatMessage.fromBuffer(messageData);
                isInRequest = true;
                break;
            case MessageType.HEARTBEAT_RESPONSE:
                message = HeartbeatResponse.fromBuffer(messageData);
                isInRequest = false;
                break;
            case MessageType.DATA_FETCH_MESSAGE:
                message = DataFetchMessage.fromBuffer(messageData);
                isInRequest = true;
                break;
            case MessageType.DATA_FETCH_RESPONSE_OK:
                message = DataFetchResponseOk.fromBuffer(messageData);
                isInRequest = false;
                break;
            case MessageType.DATA_FETCH_RESPONSE_NO:
                message = DataFetchResponseNo.fromBuffer(messageData);
                isInRequest = false;
                break;
            case MessageType.DHT_STORE_MESSAGE:
                message = DhtStoreMessage.fromBuffer(messageData);
                isInRequest = true;
                break;
            case MessageType.DHT_STORE_RESPONSE:
                message = DhtStoreResponse.fromBuffer(messageData);
                isInRequest = false;
                break;
            case MessageType.DHT_LOOKUP_MESSAGE:
                message = DhtLookupMessage.fromBuffer(messageData);
                isInRequest = true;
                break;
            case MessageType.DHT_LOOKUP_RESPONSE:
                message = DhtLookupResponse.fromBuffer(messageData);
                isInRequest = false;
                break;
            case MessageType.TELEMETRY_FETCH_MESSAGE:
                message = TelemetryFetchMessage.fromBuffer(messageData);
                isInRequest = true;
            case MessageType.TELEMETRY_FETCH_RESPONSE:
                message = TelemetryFetchResponse.fromBuffer(messageData);
                isInRequest = false;
            default:
                throw new Error('Unknown message type: ' + messageType);
        }

        this.log(`Got packet from ${address}:${port}, msg ${message}`);
        
        const peerNode = this.allPeerNodes.getPeerNode(
            address, port);
        let inBytes = data.length + 28; // 28 for UDP header
        let allBytes = inBytes;
        const now = performance.now();
        
        let telemetryInfo = peerNode.telemetryInfo.get(allTelemetryId);
        if (telemetryInfo)
            telemetryInfo.update(now, inBytes);
        else
            telemetryInfo = new TelemetryInfo(now);

        telemetryInfo = peerNode.telemetryInfo.get(peerTelemetryId)
        if (telemetryInfo)
            telemetryInfo.update(now, inBytes);
        else
            telemetryInfo = new TelemetryInfo(now);
        
        // TODO -- Need to treat DataFetchResponseOk messages special, so
        // we can detect downstream congestion
        this.allTrafficManager.notifyDownload(
            inBytes, allBytes, isInRequest);
        this.getTrafficManager(peerNode).notifyDownload(
            inBytes, allBytes, isInRequest);
        
        if (isInRequest) {
            if (this.requestHandlers.has(message.type)) {
                const handler = this.requestHandlers.get(message.type);
                if (handler) {
                    this.log('  dispatch to InReq handler');
                    const request = new InRequest(this, peerNode,
                                                  requestId, message);
                    handler(request, message).catch(err =>
                        this.log(`Exception from message handler for ${message} from ${peerNode.ip}:${peerNode.port}: ${err}`));
                } else {
                    this.log( `No handler for ${message.type}`);
                }
                return;
            }
        } else {
            const request = this.outRequests.get(requestId);
            if (request === undefined) {
                this.log(`Unknown request ID ${requestId}`);
                return;
            }
            this.log('  dispatch to response handler');
            request.handleMessage(message);
            
            return;
        }
        
        this.log(`Unhandled message, type ${message.type}`);
    }
    
    registerRequestHandler(type: number, handler: RequestHandler): void {
        this.requestHandlers.set(type, handler);
    }
    
    unregisterRequestHandler(type: number): void {
        this.requestHandlers.delete(type);
    }
    
    requestTransferNoDelay(source: PeerNode, message: Message)
    : AllowedTransfer | null {
        const allManager = this.allTrafficManager;
        const sourceManager = this.getTrafficManager(source);

        let upBytes = 28 + 6 + message.transferSize();
        let downBytes = message.responseSize();
        if (downBytes)
            downBytes += 28 + 6;
        
        this.log(`  RTND ${upBytes} up, ${downBytes} down`);

        if ((!downBytes || allManager.requestedDownstream.readyToGo(this.logger))
            && allManager.upstream.readyToGo(this.logger)
            && (!downBytes || sourceManager.requestedDownstream.readyToGo(this.logger))
            && sourceManager.upstream.readyToGo(this.logger))
        {
            allManager.requestedDownstream.consume(downBytes);
            allManager.upstream.consume(upBytes);
            sourceManager.requestedDownstream.consume(downBytes);
            sourceManager.upstream.consume(upBytes);

            return {
                source,
                downloadBytesAllowed: downBytes,
                allTelemetryBatchId: allManager.upstreamTelemetryBatchId,
                peerTelemetryBatchId: sourceManager.upstreamTelemetryBatchId
            };
        } else {
            return null;
        }
    }

    requestDownloadNoDelay(sources: PeerNode[],
                           maxDownBytes: number)
    : AllowedTransfer[] | null {
        const allManager = this.allTrafficManager;

        this.log(`  RDND ${maxDownBytes} max down`);
        
        if (!allManager.requestedDownstream.readyToGo(this.logger) ||
            !allManager.upstream.readyToGo(this.logger))
        {
            return null;
        }

        let result: AllowedTransfer[] = [];

        const upBytes = 28 + 6 + 56;
        const downHeaderBytes = 28 + 6 + 48;
        
        for (let peerNode of sources) {
            let sourceManager = this.getTrafficManager(peerNode);
                
            if (!sourceManager.requestedDownstream.readyToGo(this.logger) ||
                !sourceManager.upstream.readyToGo(this.logger))
            {
                continue;
            }

            let allowedBytes = Math.min(
                sourceManager.requestedDownstream.maxSpeed
                    * (QUEUE_DEPTH_LIMIT + QUEUE_MESSAGE_LIMIT)
                    - sourceManager.requestedDownstream.bytesBudgeted,
                allManager.requestedDownstream.maxSpeed
                    * (QUEUE_DEPTH_LIMIT + QUEUE_MESSAGE_LIMIT)
                    - allManager.requestedDownstream.bytesBudgeted);
            
            if (allowedBytes <= 0)
                continue;

            allowedBytes = Math.min(
                DOWNLOAD_CHUNK_SIZE
                    * Math.ceil(allowedBytes / DOWNLOAD_CHUNK_SIZE),
                maxDownBytes);
            
            allManager.requestedDownstream.consume(
                downHeaderBytes + allowedBytes);
            allManager.upstream.consume(upBytes);
            
            sourceManager.requestedDownstream.consume(
                downHeaderBytes + allowedBytes);
            sourceManager.upstream.consume(upBytes);

            result.push({
                source: peerNode,
                downloadBytesAllowed: allowedBytes,
                allTelemetryBatchId: allManager.upstreamTelemetryBatchId,
                peerTelemetryBatchId: sourceManager.upstreamTelemetryBatchId,
            });

            maxDownBytes -= allowedBytes;
            if (maxDownBytes <= 0)
                break;
        }
        
        if (result.length > 0)
            return result;
        else
            return null;
    }

    requestTransfer(source: PeerNode, message: Message)
    : Promise<AllowedTransfer> {
        this.log(`Request transfer to ${source.ip}:${source.port}`);
        return new Promise(resolve => {
            let newQueueEntry: QueuedTransfer = {
                peerNodes: [source],
                attemptRequest: () => {
                    let result = this.requestTransferNoDelay(
                        source, message);
                    if (result) {
                        resolve(result);
                        return true;
                    } else {
                        return false;
                    }
                }
            };
            this.requestQueue.push(newQueueEntry);
            this.processRequestQueue();
        });
    }

    requestDownload(sources: PeerNode[], maxBytes: number)
    : Promise<AllowedTransfer[]> {
        return new Promise(resolve => {
            let newTransfer: QueuedTransfer = {
                peerNodes: sources,
                attemptRequest: () => {
                    let result = this.requestDownloadNoDelay(sources, maxBytes);
                    if (result) {
                        resolve(result);
                        return true;
                    } else {
                        return false;
                    }
                }
            };
            this.requestQueue.push(newTransfer);
            this.processRequestQueue();
        });
    }        
    
    processRequestQueue(): void {
        this.log(`processRequestQueue(), ${this.requestQueue.length} waiting`);
        this.log(`  all budget: ${this.allTrafficManager.requestedDownstream.bytesBudgeted} down, ${this.allTrafficManager.upstream.bytesBudgeted} up`);
        
        this.allTrafficManager.updateBudgets();
        for(let queuedRequest of this.requestQueue)
            for(let peerNode of queuedRequest.peerNodes)
                this.getTrafficManager(peerNode).updateBudgets();
            
        this.requestQueue = this.requestQueue.filter(
            queuedRequest => !queuedRequest.attemptRequest());
        
        clearTimeout(this.processRequestQueueTimeout)

        if (this.requestQueue.length > 0)
            this.processRequestQueueTimeout = setTimeout(
                this.processRequestQueue.bind(this),
                QUEUE_CHECK_RATE * 1000);
        else
            this.processRequestQueueTimeout = undefined;
    }

    async handleTelemetryFetchMessage(request: InRequest, rawMessage: Message)
    : Promise<void> {
        if (!(rawMessage instanceof TelemetryFetchMessage))
            throw new Error(`Wrong message type! ${rawMessage}`);
        const message = rawMessage as TelemetryFetchMessage;

        const peerNode = request.source;
        const telemetryInfo = peerNode.telemetryInfo.get(
            message.telemetryBatchId)
        if (!telemetryInfo) {
            this.logger.log('telemetry', `Request from ${peerNode.ip}:${peerNode.port} for nonexistent telemetry ${message.telemetryBatchId}`);
            return;
        }

        const telemetryMessage = new TelemetryFetchResponse(
            telemetryInfo.bytesSeen);
        await this.requestTransfer(peerNode, telemetryMessage);
        request.sendResponse(telemetryMessage);
    }
        
    newTelemetryId(): number {
        return this.nextTelemetryBatchId++;
    }

    log(msg: string): void {
        this.logger.log('network', msg);
    }

    getTrafficManager(peer: PeerNode): TrafficManagerImpl {
        if (peer.trafficManager === null)
            peer.trafficManager = new TrafficManagerImpl(this, this.config)
        return (peer.trafficManager as TrafficManagerImpl);
    }
}

function getLocalIPAddresses(): string[] {
    const interfaces = os.networkInterfaces();
    const addresses: string[] = [];

    for (const interfaceName in interfaces) {
        const interfaceInfo = interfaces[interfaceName];

        if (interfaceInfo)
            for (const netInterface of interfaceInfo)
                if (netInterface.family === 'IPv4')
                    if (netInterface.address !== '127.0.0.1')
                        addresses.push(netInterface.address);
    }

    return addresses;
}

export {
    InRequest, OutRequest, RequestHandler, NetworkManager, NetworkManagerImpl,
    AllowedTransfer
};
