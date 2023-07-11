import { PeerProxy } from "./structures";

export enum MessageType {
    ROOT_HEARTBEAT = 0,
    PROXY_HEARTBEAT = 1,
    DATA_REQUEST = 2,
    DATA_RESPONSE_OK = 3,
    DATA_RESPONSE_ELSEWHERE = 4,
    DATA_RESPONSE_UNKNOWN = 5,
    DATA_IS_HERE = 6,
}

export abstract class Message {
    type: MessageType;

    constructor(type: MessageType) {
        this.type = type;
    }

    abstract encode(): Uint8Array;
}

export class RootHeartbeatMessage extends Message {
    nodeMapFileAddr: string | null;
    
    static zeroBuffer = Buffer.alloc(38, 0);
    
    constructor(nodeMapFileAddr: string | null) {
        super(MessageType.ROOT_HEARTBEAT);
        this.nodeMapFileAddr = nodeMapFileAddr;
    }

    static fromBuffer(buffer: Buffer): RootHeartbeatMessage {
        if (buffer.slice(0, 38).equals(this.zeroBuffer)) {
            return new RootHeartbeatMessage(null);
        } else {
            const hash = buffer.slice(0, 32).toString('hex');
            const size = buffer.readBigUInt64BE(32).toString();
            return new RootHeartbeatMessage(`${hash}:${size}`);
        }
    }

    encode(): Buffer {
        let buffer = Buffer.alloc(40);
        if (this.nodeMapFileAddr === null) {
            RootHeartbeatMessage.zeroBuffer.copy(buffer, 0, 0, 40);
        } else {
            const [hash, size] = this.nodeMapFileAddr.split(':');
            const hashBuffer = Buffer.from(hash, 'hex');
            hashBuffer.copy(buffer, 0);

            const sizeBuffer = Buffer.alloc(8);
            sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
            sizeBuffer.copy(buffer, 32);
        }
        return buffer;
    }
}

export class ProxyHeartbeatMessage extends Message {
    constructor() {
        super(MessageType.PROXY_HEARTBEAT);
    }

    static fromBuffer(buffer: Buffer): ProxyHeartbeatMessage {
        return new ProxyHeartbeatMessage();
    }

    encode(): Buffer {
        const encodedMessage = Buffer.concat([
        ]);

        return encodedMessage;
    }
}

export class DataRequestMessage extends Message {
    burstId: number;
    fileAddr: string;
    offset: number;
    length: number;

    constructor(burstId: number, fileAddr: string, offset: number,
                length: number)
    {
        super(MessageType.DATA_REQUEST);
        this.burstId = burstId;
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
    }

    static fromBuffer(buffer: Buffer): DataRequestMessage {
        const burstId = buffer.readUInt16BE(0);
        const hash = buffer.slice(2, 34).toString('hex');
        const size = buffer.readBigUInt64BE(34).toString();
        const fileAddr = `${hash}:${size}`;
        const offset = buffer.readInt32BE(42);
        const length = buffer.readInt32BE(46);
        return new DataRequestMessage(burstId, fileAddr, offset, length);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(50); 
        buffer.writeUInt16BE(this.burstId, 0);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, 2);

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, 34);

        buffer.writeInt32BE(this.offset, 42);
        buffer.writeInt32BE(this.length, 46);
        return buffer;
    }
}

export class DataResponseOkMessage extends Message {
    burstId: number;
    fileAddr: string;
    offset: number;
    length: number;
    data: Buffer;

    constructor(burstId: number, fileAddr: string, offset: number,
                length: number, data: Buffer)
    {
        super(MessageType.DATA_RESPONSE_OK);
        this.burstId = burstId;
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
        this.data = data;
    }

    static fromBuffer(buffer: Buffer): DataResponseOkMessage {
        const burstId = buffer.readUInt16BE(0);
        const hash = buffer.slice(2, 34).toString('hex');
        const size = buffer.readBigUInt64BE(34).toString();
        const fileAddr = `${hash}:${size}`;
        const offset = buffer.readInt32BE(42);
        const length = buffer.readInt32BE(46);
        const data = buffer.slice(50);
        return new DataResponseOkMessage(burstId, fileAddr, offset, length,
                                         data);
    }

    encode(): Buffer {
        const headerBuffer = Buffer.allocUnsafe(50);
        headerBuffer.writeUInt16BE(this.burstId, 0);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(headerBuffer, 2);

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(headerBuffer, 34);

        headerBuffer.writeInt32BE(this.offset, 42);
        headerBuffer.writeInt32BE(this.length, 46);
        return Buffer.concat([headerBuffer, this.data]);
    }
}

export class DataResponseElsewhereMessage extends Message {
    burstId: number;
    fileAddr: string;
    nodeInfo: Array<{ ip: string, port: number }>; 

    constructor(burstId: number, fileAddr: string,
                nodeInfo: Array<{ ip: string, port: number }>)
    {
        super(MessageType.DATA_RESPONSE_ELSEWHERE);
        this.burstId = burstId;
        this.fileAddr = fileAddr;
        this.nodeInfo = nodeInfo;
    }

    static fromBuffer(buffer: Buffer): DataResponseElsewhereMessage {
        const burstId = buffer.readUInt16BE(0);
        const hash = buffer.slice(2, 34).toString('hex');
        const size = buffer.readBigUInt64BE(34).toString();
        const fileAddr = `${hash}:${size}`;

        let offset = 42;
        const nodeInfo = [];
        while (true) {
            const protocolType = buffer.readUInt8(offset);
            offset += 1;
            
            if (protocolType === 97) {
                if (offset !== buffer.length) { 
                    throw new Error("Malformed DataResponseElsewhereMessage");
                }
                break;
            } else if (protocolType !== 98) {
                console.log(`Unknown protocol type: ${protocolType}`);
                const nodeSize = buffer.readUInt8(offset);
                offset += nodeSize;
                continue;
            }
            
            const nodeSize = buffer.readUInt8(offset);
            offset += 1;
            if (nodeSize !== 6) {
                throw new Error("Malformed nodeInfo in DataResponseElsewhereMessage");
                continue;
            }
            
            const ipBytes = [buffer.readUInt8(offset),
                             buffer.readUInt8(offset + 1),
                             buffer.readUInt8(offset + 2),
                             buffer.readUInt8(offset + 3)];
            const ip = ipBytes.join('.'); 
            offset += 4;
            const port = buffer.readUInt16BE(offset);
            offset += 2;
            nodeInfo.push({ ip, port });
        }

        return new DataResponseElsewhereMessage(burstId, fileAddr, nodeInfo);
    }

    encode(): Buffer {
        const headerBuffer = Buffer.allocUnsafe(44);
        headerBuffer.writeUInt16BE(this.burstId, 0);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(headerBuffer, 2);

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(headerBuffer, 34);

        const nodeInfoBufferParts = this.nodeInfo.map(({ ip, port }) => {
            const nodeBuffer = Buffer.allocUnsafe(8);
            nodeBuffer.writeUInt8(98, 0);
            nodeBuffer.writeUInt8(6, 1);
            const ipBytes = ip.split('.').map(byte => parseInt(byte));
            for (let i = 0; i < 4; i++)
                nodeBuffer.writeUInt8(ipBytes[i], 2 + i);
            nodeBuffer.writeUInt16BE(port, 6);
            return nodeBuffer;
        });

        const endSignal = Buffer.alloc(1);
        endSignal.writeUInt8(97, 0); 

        return Buffer.concat([headerBuffer, ...nodeInfoBufferParts, endSignal]);
    }
}

export class DataResponseUnknownMessage extends Message {
    burstId: number;
    fileAddr: string;

    constructor(burstId: number, fileAddr: string) {
        super(MessageType.DATA_RESPONSE_UNKNOWN);
        this.burstId = burstId;
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DataResponseUnknownMessage {
        const burstId = buffer.readUInt16BE(0);
        const hash = buffer.slice(2, 34).toString('hex');
        const size = buffer.readBigUInt64BE(34).toString();
        const fileAddr = `${hash}:${size}`;
        return new DataResponseUnknownMessage(burstId, fileAddr);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(42);
        buffer.writeUInt16BE(this.burstId, 0);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, 2);

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, 34);
        return buffer;
    }
}

export class DataIsHereMessage extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DATA_IS_HERE);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DataIsHereMessage {
        const hash = buffer.slice(0, 32).toString('hex');
        const size = buffer.readBigUInt64BE(32).toString();
        const fileAddr = `${hash}:${size}`;
        return new DataIsHereMessage(fileAddr);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(40);
        
        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, 0);

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, 32);
        
        return buffer;
    }
}



module.exports = {
    MessageType,
    Message,
    RootHeartbeatMessage,
    ProxyHeartbeatMessage,
    DataRequestMessage,
    DataResponseOkMessage,
    DataResponseElsewhereMessage,
    DataResponseUnknownMessage,
    DataIsHereMessage,
};
