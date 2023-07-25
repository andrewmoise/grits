import * as dgram from 'dgram';
import * as os from 'os';
import { performance } from 'perf_hooks';

import { Config } from './config';
import { Logger } from './logger';
import { TrafficManager } from "./traffic";

import {
    MessageType,
    Message,
    
    HeartbeatMessage,
    HeartbeatResponse,

    DataRequestMessage,
    DataResponseOk,
    DataResponseUnknown,

    DhtStoreMessage,
    DhtStoreResponse,

    DhtLookupMessage,
    DhtLookupResponse,
} from './messages';

const MAGIC_BYTES = Buffer.from([140, 98]);
const DEFAULT_TIMEOUT: number = 500;

type RequestHandler = (request: InRequest, message: Message) => void;

interface NetworkManager {
    start(): void;
    stop(): void;

    newRequest(ipAddress: string, port: number, message: Message): OutRequest;
    
    registerRequestHandler(
        type: number,
        handler: RequestHandler,
    ): void;
    unregisterRequestHandler(type: number): void;
}

class InRequest {
    manager: UdpNetworkManager;
    requestId: number;
    ip: string;
    port: number;
    message: Message;
    
    constructor(manager: UdpNetworkManager,
                ip: string, port: number,
                id: number, message: Message)
    {
        this.manager = manager;
        this.requestId = id;
        this.ip = ip;
        this.port = port;
        this.message = message;
    }
    
    sendResponse(message: Message): void {
        this.manager.send(message, this.requestId, this.ip, this.port);
    }
}

class OutRequest {
    manager: UdpNetworkManager;
    requestId: number;
    ip: string;
    port: number;

    timeoutTimeout: NodeJS.Timeout | null;
    
    messageQueue: Message[] = [];
    resolverQueue: ((msg: Message | null) => void)[] = [];

    constructor(manager: UdpNetworkManager, id: number,
                ip: string, port: number)
    {
        this.manager = manager;
        this.requestId = id;
        this.ip = ip;
        this.port = port;
        
        this.timeoutTimeout = setTimeout(
            this.timeoutFn.bind(this), DEFAULT_TIMEOUT);
        
        this.manager.outRequests.set(this.requestId, this);
    }
    
    sendRequest(message: Message): void {
        this.manager.send(message, this.requestId, this.ip, this.port);
    }

    getResponse(): Promise<Message | null> {
        if (this.messageQueue.length > 0) {
            return Promise.resolve(this.messageQueue.shift()!);
        } else {
            return new Promise(resolve => {
                this.resolverQueue.push(resolve);
            });
        }
    }
    
    handleMessage(message: Message): void {
        // Restart the timeout timer
        if (this.timeoutTimeout) {
            clearTimeout(this.timeoutTimeout);
            this.timeoutTimeout = setTimeout(
                this.timeoutFn.bind(this), DEFAULT_TIMEOUT);
        }

        // Handle the message
        if (this.resolverQueue.length > 0) {
            const resolve = this.resolverQueue.shift();
            if (resolve) {
                resolve(message);
            }
        } else {
            this.messageQueue.push(message);
        }
    }
    
    close(): void {
        // Clear the timeout
        if (this.timeoutTimeout) {
            clearTimeout(this.timeoutTimeout);
            this.timeoutTimeout = null;
        }

        // Clear the resolver queue
        const resolverQueue = this.resolverQueue;
        this.resolverQueue = [];
        for (let resolve of resolverQueue)
            resolve(null);

        this.manager.outRequests.delete(this.requestId);
    }

    private timeoutFn() {
        this.timeoutTimeout = null;
        
        if (this.resolverQueue.length > 0) {
            const resolve = this.resolverQueue.shift();
            if (resolve) {
                resolve(null);
            }
        }
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

class UdpNetworkManager {
    logger: Logger;
    config: Config;

    outRequests: Map<number, OutRequest>;
    nextRequestId: number;
    
    requestHandlers: Map<number, RequestHandler>;
    socket: dgram.Socket | null;

    constructor(logger: Logger, config: Config) {
        if (config.thisHost === null) {
            let addresses = getLocalIPAddresses();
            if (addresses.length !== 1) {
                throw new Error('Expected 1 local address, but found '
                    + addresses.length);
            }
            config.thisHost = addresses[0];
        }

        this.logger = logger;
        this.config = config;

        this.outRequests = new Map();
        this.nextRequestId = 0;
        
        this.requestHandlers = new Map();
        this.socket = null;
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
            this.handleIncomingMessage.bind(this));
    }

    stop(): void {
        if (this.socket) {
            this.socket.close();
            this.socket = null;
        }
    }

    newRequest(ipAddress: string, port: number, message: Message)
    : OutRequest {
        const requestId = this.nextRequestId++;

        const request = new OutRequest(this, requestId, ipAddress, port);
        this.outRequests.set(requestId, request);
        request.sendRequest(message);

        return request;
    }
    
    send(message: Message, requestId: number, ipAddress: string, port: number)
    : void {
        const encodedContent = message.encode();

        const requestIdBuffer = Buffer.alloc(4);
        requestIdBuffer.writeInt32BE(requestId, 0);
        
        const header = Buffer.concat([
            MAGIC_BYTES,
            requestIdBuffer,
            Buffer.from([message.type])
        ]);
        const encodedMessage = Buffer.concat([header, encodedContent]);

        if (!this.socket) {
            this.logger.log('network', 'Socket is not initialized');
            return;
        }

        this.socket.send(encodedMessage, 0, encodedMessage.length, port,
            ipAddress, (error) => {
                if (error) {
                    this.logger.log('network',
                                    `Error sending message: ${error}`);
                    return;
                }
            });
    }

    handleIncomingMessage(data: Buffer, rinfo: dgram.RemoteInfo): void {
        const { address, port } = rinfo;

        const magicNumber = data.slice(0, MAGIC_BYTES.length);
        if (!magicNumber.equals(MAGIC_BYTES))
            throw new Error('Invalid magic number');

        const requestId = data.readInt32BE(MAGIC_BYTES.length);
        const messageType = data.readUInt8(MAGIC_BYTES.length + 4);
        const messageData = data.slice(MAGIC_BYTES.length + 5);

        let isInRequest: boolean;
        
        let message;
        switch (messageType) {
            case MessageType.HEARTBEAT_MESSAGE:
                message = HeartbeatMessage.fromBuffer(messageData);
                isInRequest = true;
                break;
            case MessageType.HEARTBEAT_RESPONSE:
                message = HeartbeatResponse.fromBuffer(messageData);
                isInRequest = false;
                break;
            case MessageType.DATA_REQUEST_MESSAGE:
                message = DataRequestMessage.fromBuffer(messageData);
                isInRequest = true;
                break;
            case MessageType.DATA_RESPONSE_OK:
                message = DataResponseOk.fromBuffer(messageData);
                isInRequest = false;
                break;
            case MessageType.DATA_RESPONSE_UNKNOWN:
                message = DataResponseUnknown.fromBuffer(messageData);
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
            default:
                throw new Error('Unknown message type: ' + messageType);
        }

        if (isInRequest) {
            if (this.requestHandlers.has(message.type)) {
                const handler = this.requestHandlers.get(message.type);
                if (handler) {
                    const request = new InRequest(this, address, port,
                                                  requestId, message);
                    handler(request, message);
                } else {
                    this.logger.log('network',
                                    `No handler for ${message.type}`);
                }
                return;
            }
        } else {
            const request = this.outRequests.get(requestId);
            if (request === undefined) {
                this.logger.log('network',
                                `Unknown request ID ${requestId}`);
                return;
            }
            request.handleMessage(message);

            return;
        }

        this.logger.log(
            'network',
            `Unhandled message, type ${message.type}`);
    }
    
    registerRequestHandler(type: number, handler: RequestHandler): void {
        this.requestHandlers.set(type, handler);
    }

    unregisterRequestHandler(type: number): void {
        this.requestHandlers.delete(type);
    }
}

export {
    InRequest, OutRequest, RequestHandler, NetworkManager, UdpNetworkManager
};
