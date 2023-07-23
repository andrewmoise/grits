import { assert } from 'console';

import { PeerProxy } from "./structures";

export enum MessageType {
    HEARTBEAT_MESSAGE = 0,
    HEARTBEAT_RESPONSE = 1,
    
    DATA_REQUEST_MESSAGE = 2,
    DATA_RESPONSE_OK = 3,
    DATA_RESPONSE_ELSEWHERE = 4,
    DATA_RESPONSE_UNKNOWN = 5,

    DHT_LOCATION_MESSAGE = 6,
    DHT_LOCATION_RESPONSE = 7,
}

export abstract class Message {
    type: MessageType;
    
    constructor(type: MessageType) {
        this.type = type;
    }

    abstract encode(): Uint8Array;
}

export class HeartbeatMessage extends Message {
    constructor() {
        super(MessageType.HEARTBEAT_MESSAGE);
    }

    static fromBuffer(buffer: Buffer): HeartbeatMessage {
        return new HeartbeatMessage();
    }

    encode(): Buffer {
        const encodedMessage = Buffer.concat([
        ]);

        return encodedMessage;
    }
}

export class HeartbeatResponse extends Message {
    nodeMapFileAddr: string | null;
    
    static zeroBuffer = Buffer.alloc(38, 0);
    
    constructor(nodeMapFileAddr: string | null) {
        super(MessageType.HEARTBEAT_RESPONSE);
        this.nodeMapFileAddr = nodeMapFileAddr;
    }

    static fromBuffer(buffer: Buffer): HeartbeatResponse {
        let offset = 0;
        if (buffer.slice(offset, offset + 38).equals(this.zeroBuffer)) {
            return new HeartbeatResponse(null);
        } else {
            const hash = buffer.slice(offset, offset + 32).toString('hex');
            offset += 32;
            const size = buffer.readBigUInt64BE(offset).toString();
            offset += 8;
            return new HeartbeatResponse(`${hash}:${size}`);
        }
    }

    encode(): Buffer {
        let buffer = Buffer.alloc(40);
        let offset = 0;
        if (this.nodeMapFileAddr === null) {
            HeartbeatResponse.zeroBuffer.copy(buffer, offset, offset, offset + 40);
            offset += 40;
        } else {
            const [hash, size] = this.nodeMapFileAddr.split(':');
            const hashBuffer = Buffer.from(hash, 'hex');
            hashBuffer.copy(buffer, offset);
            offset += hashBuffer.length;

            const sizeBuffer = Buffer.alloc(8);
            sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
            sizeBuffer.copy(buffer, offset);
            offset += sizeBuffer.length;
        }
        return buffer;
    }
}

export class DataRequestMessage extends Message {
    fileAddr: string;
    offset: number;
    length: number;
    transferId: string;

    constructor(fileAddr: string, offset: number, length: number, transferId: string) {
        super(MessageType.DATA_REQUEST_MESSAGE);
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
        this.transferId = transferId;
    }

    static fromBuffer(buffer: Buffer): DataRequestMessage {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        const messageOffset = buffer.readInt32BE(offset);
        offset += 4;
        const length = buffer.readInt32BE(offset);
        offset += 4;
        const transferId = buffer.slice(offset, offset + 8).toString('binary');
        offset += 8;

        return new DataRequestMessage(fileAddr, messageOffset, length, transferId);
    }

    encode(): Buffer {
        let offset = 0;
        const buffer = Buffer.allocUnsafe(56);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, offset);
        offset += 32;

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, offset);
        offset += 8;

        buffer.writeInt32BE(this.offset, offset);
        offset += 4;
        buffer.writeInt32BE(this.length, offset);
        offset += 4;

        buffer.write(this.transferId, offset, 'binary');
        offset += 8;

        return buffer;
    }
}

export class DataResponseOk extends Message {
    fileAddr: string;
    offset: number;
    length: number;
    data: Buffer;

    constructor(fileAddr: string, offset: number, length: number, data: Buffer) {
        super(MessageType.DATA_RESPONSE_OK);
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
        this.data = data;
    }

    static fromBuffer(buffer: Buffer): DataResponseOk {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        const messageOffset = buffer.readInt32BE(offset);
        offset += 4;
        const length = buffer.readInt32BE(offset);
        offset += 4;
        const data = buffer.slice(offset);

        return new DataResponseOk(fileAddr, messageOffset, length, data);
    }

    encode(): Buffer {
        let offset = 0;
        const headerBuffer = Buffer.allocUnsafe(48);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(headerBuffer, offset);
        offset += 32;

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(headerBuffer, offset);
        offset += 8;

        headerBuffer.writeInt32BE(this.offset, offset);
        offset += 4;
        headerBuffer.writeInt32BE(this.length, offset);
        offset += 4;

        return Buffer.concat([headerBuffer, this.data]);
    }
}

export class DataResponseElsewhere extends Message {
    fileAddr: string;
    nodeInfo: Array<{ ip: string, port: number }>;

    constructor(fileAddr: string, nodeInfo: Array<{ ip: string, port: number }>)
    {
        console.log(`Constructing elsewhere: ${nodeInfo.length}`);
        
        super(MessageType.DATA_RESPONSE_ELSEWHERE);
        this.fileAddr = fileAddr;
        this.nodeInfo = nodeInfo;
    }

    static fromBuffer(buffer: Buffer): DataResponseElsewhere {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        const nodeInfo: Array<{ ip: string, port: number }> = [];

        console.log(`fromBuffer elsewhere`);
        
        while (offset < buffer.length) {
            const protocolType = buffer.readUInt8(offset);
            offset++;

            console.log(`  offset ${offset}`);
            
            if (protocolType === 97) {
                if (offset !== buffer.length) {
                    throw new Error("Malformed DataResponseElsewhere");
                }
                break;
            } else if (protocolType === 98) {
                console.log('    read');
                
                const nodeSize = buffer.readUInt8(offset);
                offset++;
                if (nodeSize !== 6) {
                    throw new Error("Malformed nodeInfo in DataResponseElsewhere");
                }
                const ipBytes = buffer.slice(offset, offset + 4);
                offset += 4;
                const ip = Array.from(ipBytes).join('.');
                const port = buffer.readUInt16BE(offset);
                offset += 2;
                nodeInfo.push({ ip, port });
            } else {
                const nodeSize = buffer.readUInt8(offset);
                offset += nodeSize;
            }
        }

        return new DataResponseElsewhere(fileAddr, nodeInfo);
    }

    encode(): Buffer {
        let offset = 0;
        const headerBuffer = Buffer.allocUnsafe(40);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(headerBuffer, offset);
        offset += 32;

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(headerBuffer, offset);
        offset += 8;

        const nodeInfoBuffers = this.nodeInfo.map(({ ip, port }) => {
            const buffer = Buffer.allocUnsafe(8);
            buffer.writeUInt8(98, 0);
            buffer.writeUInt8(6, 1);
            ip.split('.').map(Number).forEach((byte, i) => {
                buffer.writeUInt8(byte, 2 + i);
            });
            buffer.writeUInt16BE(port, 6);
            return buffer;
        });

        console.log(`encode elsewhere: ${nodeInfoBuffers.length}`);
        
        const endSignal = Buffer.alloc(1);
        endSignal.writeUInt8(97, 0);

        return Buffer.concat([headerBuffer, ...nodeInfoBuffers, endSignal]);
    }
}

export class DataResponseUnknown extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DATA_RESPONSE_UNKNOWN);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DataResponseUnknown {
        let offset = 0;

        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;

        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;

        const fileAddr = `${hash}:${size}`;
        return new DataResponseUnknown(fileAddr);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(40);
        let offset = 0;

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, offset);
        offset += hashBuffer.length;

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, offset);
        offset += sizeBuffer.length;
        
        return buffer;
    }
}

export class DhtLocationMessage extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DHT_LOCATION_MESSAGE);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DhtLocationMessage {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        return new DhtLocationMessage(fileAddr);
    }

    encode(): Buffer {
        let offset = 0;
        const buffer = Buffer.allocUnsafe(40);
        
        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, offset);
        offset += 32;

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, offset);
        offset += 8;
        
        return buffer;
    }
}

export class DhtLocationResponse extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DHT_LOCATION_RESPONSE);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DhtLocationResponse {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        return new DhtLocationResponse(fileAddr);
    }

    encode(): Buffer {
        let offset = 0;
        const buffer = Buffer.allocUnsafe(40);
        
        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, offset);
        offset += 32;

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, offset);
        offset += 8;
        
        return buffer;
    }
}

module.exports = {
    MessageType,
    Message,
    
    HeartbeatMessage,
    HeartbeatResponse,

    DataRequestMessage,
    DataResponseOk,
    DataResponseElsewhere,
    DataResponseUnknown,

    DhtLocationMessage,
    DhtLocationResponse,
};
