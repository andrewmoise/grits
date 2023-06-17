import * as dgram from 'dgram';
import * as os from 'os';

import { Config } from './config';

import {
    MessageType,
    Message,
    RootHeartbeatMessage,
    ProxyHeartbeatMessage,
    DataRequestMessage,
    DataResponseOkMessage,
    DataResponseElsewhereMessage,
    DataResponseUnknownMessage,
} from './messages';

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

type RequestHandler = (address: string, port: number, message: Message)
    => void;

class NetworkingManager {
    config: Config;
    requestHandlers: Map<number, RequestHandler>;
    socket: dgram.Socket | null;

    constructor(config: Config) {
        if (config.thisHost === null) {
            let addresses = getLocalIPAddresses();
            if (addresses.length !== 1) {
                throw new Error('Expected 1 local address, but found '
                    + addresses.length);
            }
            config.thisHost = addresses[0];
        }

        this.config = config;
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
        this.socket.on('message', this.handleIncomingMessage.bind(this));
    }

    stop(): void {
        if (this.socket) {
            this.socket.close();
            this.socket = null;
        }
    }

    send(message: Message, ipAddress: string, port: number): void {
        const encodedMessage = message.encode();

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
        const message = this.decodeMessage(data);
        const { address, port } = rinfo;

        console.log("Received message - port " + port + ", type " + message.type);

        if (this.requestHandlers.has(message.type)) {
            const handler = this.requestHandlers.get(message.type);
            handler && handler(address, port, message);
            return;
        }
    }

    registerRequestHandler(type: number, handler: RequestHandler): void {
        this.requestHandlers.set(type, handler);
    }

    unregisterRequestHandler(type: number): void {
        this.requestHandlers.delete(type);
    }

    decodeMessage(data: Buffer): Message {
        const messageType = data.readUInt8(2);
        let message;

        switch (messageType) {
            case MessageType.PROXY_HEARTBEAT:
                message = new ProxyHeartbeatMessage();
                break;
            case MessageType.ROOT_HEARTBEAT:
                message = new RootHeartbeatMessage();
                break;
            //case MessageType.DATA_REQUEST:
            //    message = new DataRequestMessage();
            //    break;
            //case MessageType.DATA_RESPONSE_OK:
            //    message = new DataResponseOkMessage();
            //    break;
            //case MessageType.DATA_RESPONSE_ELSEWHERE:
            //    message = new DataResponseElsewhereMessage();
            //    break;
            //case MessageType.DATA_RESPONSE_UNKNOWN:
            //    message = new DataResponseUnknownMessage();
            //    break;
            default:
                throw new Error('Unknown message type: ' + messageType);
        }

        message.decode(data);
        return message;
    }
}

export {
    NetworkingManager,
};
