import { assert } from 'console';

export enum MessageType {
    HEARTBEAT_MESSAGE = 0,
    HEARTBEAT_RESPONSE = 1,
    
    DATA_FETCH_MESSAGE = 2,
    DATA_FETCH_RESPONSE_OK = 3,
    DATA_FETCH_RESPONSE_NO = 4,

    DHT_STORE_MESSAGE = 5,
    DHT_STORE_RESPONSE = 6,

    DHT_LOOKUP_MESSAGE = 7,
    DHT_LOOKUP_RESPONSE = 8,

    TELEMETRY_FETCH_MESSAGE = 9,
    TELEMETRY_FETCH_RESPONSE = 10,
}

export abstract class Message {
    type: MessageType;
    
    constructor(type: MessageType) {
        this.type = type;
    }

    abstract encode(): Uint8Array;

    abstract transferSize(): number;
    responseSize(): number {
        return 0;
    }
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

    transferSize(): number {
        return 0;
    }
    responseSize() {
        return 40;
    }

    toString(): string {
        return 'HeartbeatMessage';
    }
}

export class HeartbeatResponse extends Message {
    nodeMapFileAddr: string | null;
    
    static zeroBuffer = Buffer.alloc(40, 0);
    
    constructor(nodeMapFileAddr: string | null) {
        super(MessageType.HEARTBEAT_RESPONSE);
        this.nodeMapFileAddr = nodeMapFileAddr;
    }

    static fromBuffer(buffer: Buffer): HeartbeatResponse {
        let offset = 0;
        if (buffer.slice(offset, offset + 40).equals(this.zeroBuffer)) {
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

    transferSize(): number {
        return 40;
    }
    
    toString(): string {
        return 'HeartbeatResponse';
    }
}

export class DataFetchMessage extends Message {
    fileAddr: string;
    offset: number;
    length: number;
    transferId: string;

    constructor(fileAddr: string, offset: number, length: number, transferId: string) {
        super(MessageType.DATA_FETCH_MESSAGE);
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
        this.transferId = transferId;
    }

    static fromBuffer(buffer: Buffer): DataFetchMessage {
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

        return new DataFetchMessage(fileAddr, messageOffset, length, transferId);
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

    transferSize(): number {
        return 56;
    }

    responseSize(): number {
        // Nothing really wrong with it, but we don't expect this path to happen
        throw new Error('Direct responseSize() for data fetch');
    }

    toString(): string {
        return 'DataFetchMessage';
    }
}

export class DataFetchResponseOk extends Message {
    fileAddr: string;
    offset: number;
    length: number;
    data: Buffer;

    constructor(fileAddr: string, offset: number, length: number, data: Buffer)
    {
        super(MessageType.DATA_FETCH_RESPONSE_OK);
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
        this.data = data;
    }

    static fromBuffer(buffer: Buffer): DataFetchResponseOk {
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

        return new DataFetchResponseOk(fileAddr, messageOffset, length, data);
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

    transferSize(): number {
        return 48 + this.length;
    }

    toString(): string {
        return 'DataFetchResponseOk';
    }
}

export class DataFetchResponseNo extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DATA_FETCH_RESPONSE_NO);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DataFetchResponseNo {
        let offset = 0;

        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;

        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;

        const fileAddr = `${hash}:${size}`;
        return new DataFetchResponseNo(fileAddr);
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

    transferSize(): number {
        return 40;
    }

    toString(): string {
        return 'DataFetchResponseNo';
    }

}

export class DhtStoreMessage extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DHT_STORE_MESSAGE);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DhtStoreMessage {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        return new DhtStoreMessage(fileAddr);
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

    transferSize(): number {
        return 40;
    }
    responseSize() {
        return 40;
    }

    toString(): string {
        return 'DhtStoreMessage';
    }
}

export class DhtStoreResponse extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DHT_STORE_RESPONSE);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DhtStoreResponse {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        return new DhtStoreResponse(fileAddr);
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

    transferSize(): number {
        return 40;
    }

    toString(): string {
        return 'DhtStoreResponse';
    }
}

export class DhtLookupMessage extends Message {
    fileAddr: string;
    transferId: string;

    constructor(fileAddr: string, transferId: string)
    {
        super(MessageType.DHT_LOOKUP_MESSAGE);
        this.fileAddr = fileAddr;
        this.transferId = transferId;
    }

    static fromBuffer(buffer: Buffer): DhtLookupMessage {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const transferId = buffer.slice(offset, offset + 8).toString('binary');
        offset += 8;

        return new DhtLookupMessage(`${hash}:${size}`, transferId);
    }

    encode(): Buffer {
        let offset = 0;
        const buffer = Buffer.allocUnsafe(48);

        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, offset);
        offset += 32;

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, offset);
        offset += 8;

        buffer.write(this.transferId, offset, 'binary');
        offset += 8;

        return buffer;
    }

    transferSize(): number {
        return 48;
    }
    responseSize() {
        // Assume 10 nodes in the response
        return 40 + 8 * 10 + 1;
    }

    toString(): string {
        return 'DhtLookupMessage';
    }
}

export class DhtLookupResponse extends Message {
    fileAddr: string;
    nodeInfo: Array<{ ip: string, port: number }>;

    constructor(fileAddr: string, nodeInfo: Array<{ ip: string, port: number }>)
    {
        super(MessageType.DHT_LOOKUP_RESPONSE);
        this.fileAddr = fileAddr;
        this.nodeInfo = nodeInfo;
    }

    static fromBuffer(buffer: Buffer): DhtLookupResponse {
        let offset = 0;
        const hash = buffer.slice(offset, offset + 32).toString('hex');
        offset += 32;
        const size = buffer.readBigUInt64BE(offset).toString();
        offset += 8;
        const fileAddr = `${hash}:${size}`;
        const nodeInfo: Array<{ ip: string, port: number }> = [];

        while (offset < buffer.length) {
            const protocolType = buffer.readUInt8(offset);
            offset++;

            if (protocolType === 97) {
                if (offset !== buffer.length) {
                    throw new Error("Malformed DhtLookupResponse");
                }
                break;
            } else if (protocolType === 98) {
                const nodeSize = buffer.readUInt8(offset);
                offset++;
                if (nodeSize !== 6) {
                    throw new Error("Malformed nodeInfo in DhtLookupResponse");
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

        return new DhtLookupResponse(fileAddr, nodeInfo);
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

        const endSignal = Buffer.alloc(1);
        endSignal.writeUInt8(97, 0);

        return Buffer.concat([headerBuffer, ...nodeInfoBuffers, endSignal]);
    }

    transferSize(): number {
        return 40 + 8 * this.nodeInfo.length + 1;
    }

    toString(): string {
        return 'DhtLookupResponse';
    }
}

export class TelemetryFetchMessage extends Message {
    telemetryBatchId: number;
    
    constructor(telemetryBatchId: number) {
        super(MessageType.TELEMETRY_FETCH_MESSAGE);
        this.telemetryBatchId = telemetryBatchId;
    }

    static fromBuffer(buffer: Buffer): TelemetryFetchMessage {
        const telmetryBatchId = buffer.readUInt32BE(0);
        return new TelemetryFetchMessage(telmetryBatchId);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(4);
        buffer.writeUInt32BE(this.telemetryBatchId, 0);
        return buffer;
    }

    transferSize(): number {
        return 4;
    }
    responseSize() {
        return 4;
    }

    toString(): string {
        return 'TelemetryFetchMessage';
    }
}

export class TelemetryFetchResponse extends Message {
    byteCount: number;

    constructor(byteCount: number) {
        super(MessageType.TELEMETRY_FETCH_RESPONSE);
        this.byteCount = byteCount;
    }

    static fromBuffer(buffer: Buffer): TelemetryFetchResponse {
        const byteCount = buffer.readUInt32BE(0);
        return new TelemetryFetchResponse(byteCount);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(4);
        buffer.writeUInt32BE(this.byteCount, 0);
        return buffer;
    }

    transferSize(): number {
        return 4;
    }

    toString(): string {
        return 'TelemetryFetchResponse';
    }
}

module.exports = {
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
};
