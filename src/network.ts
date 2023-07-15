import * as dgram from 'dgram';
import * as os from 'os';

import { Config } from './config';
import { Logger } from './logger';
import { UpstreamManager } from "./traffic";

import {
    MessageType,
    Message,
    HeartbeatResponse,
    HeartbeatMessage,
    DataRequestMessage,
    DataResponseOk,
    DataResponseElsewhere,
    DataResponseUnknown,
    DhtLocationMessage,
    DhtLocationResponse,
} from './messages';

type RequestHandler = (address: string, port: number, message: Message) => void;

const MAGIC_BYTES = Buffer.from([140, 98]);

interface NetworkManager {
    start(overrideHandler?: (data: Buffer,
                             rinfo: dgram.RemoteInfo) => void)
    : void;
    stop(): void;

    send(message: Message, ipAddress: string, port: number): void;

    registerRequestHandler(
        type: number,
        handler: (address: string, port: number, message: Message) => void)
    : void;
    unregisterRequestHandler(type: number): void;

    handleIncomingMessage(data: Buffer, rinfo: dgram.RemoteInfo): void;
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
    
    requestHandlers: Map<number, RequestHandler>;
    socket: dgram.Socket | null;
    trafficManager: UpstreamManager;

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

        this.requestHandlers = new Map();
        this.socket = null;
        this.trafficManager = new UpstreamManager(config);
    }

    start(overrideHandler?: (data: Buffer, rinfo: dgram.RemoteInfo) => void)
    : void {
        const socketOptions: dgram.SocketOptions = {
            type: 'udp4',
            reuseAddr: true,
        };

        this.socket = dgram.createSocket(socketOptions);
        this.socket.bind(this.config.thisPort, this.config.thisHost);

        this.socket.on(
            'message',
            overrideHandler || this.handleIncomingMessage.bind(this));
    }

    stop(): void {
        if (this.socket) {
            this.socket.close();
            this.socket = null;
        }
    }

    send(message: Message, ipAddress: string, port: number): void {
        const encodedContent = message.encode();
        const header = Buffer.concat([
            MAGIC_BYTES,
            Buffer.from([message.type])
        ]);
        const encodedMessage = Buffer.concat([header, encodedContent]);

        if (!this.socket) {
            console.error('Socket is not initialized');
            return;
        }

        this.socket.send(encodedMessage, 0, encodedMessage.length, port,
            ipAddress, (error) => {
                if (error) {
                    console.error('Error sending message:', error);
                    return;
                }
            });
    }

    handleIncomingMessage(data: Buffer, rinfo: dgram.RemoteInfo): void {
        const { address, port } = rinfo;
        const messageType = data.readUInt8(2);
        const messageData = data.slice(3);

        let message;
        switch (messageType) {
            case MessageType.HEARTBEAT_MESSAGE:
                message = HeartbeatMessage.fromBuffer(messageData);
                break;
            case MessageType.HEARTBEAT_RESPONSE:
                message = HeartbeatResponse.fromBuffer(messageData);
                break;
            case MessageType.DATA_REQUEST_MESSAGE:
                message = DataRequestMessage.fromBuffer(messageData);
                break;
            case MessageType.DATA_RESPONSE_OK:
                message = DataResponseOk.fromBuffer(messageData);
                break;
            case MessageType.DATA_RESPONSE_ELSEWHERE:
                message = DataResponseElsewhere.fromBuffer(messageData);
                break;
            case MessageType.DATA_RESPONSE_UNKNOWN:
                message = DataResponseUnknown.fromBuffer(messageData);
                break;
            case MessageType.DHT_LOCATION_MESSAGE:
                message = DhtLocationMessage.fromBuffer(messageData);
                break;
            case MessageType.DHT_LOCATION_RESPONSE:
                message = DhtLocationResponse.fromBuffer(messageData);
                break;
            default:
                throw new Error('Unknown message type: ' + messageType);
        }

        if (this.requestHandlers.has(message.type)) {
            const handler = this.requestHandlers.get(message.type);
            handler && handler(address, port, message);
            return;
        } else {
            this.logger.log(
                new Date(), 'network',
                `Unhandled message, type ${message.type}`);
        }
    }
    
    registerRequestHandler(type: number, handler: RequestHandler): void {
        this.requestHandlers.set(type, handler);
    }

    unregisterRequestHandler(type: number): void {
        this.requestHandlers.delete(type);
    }
}

export {
    RequestHandler, NetworkManager, UdpNetworkManager
};
