export enum MessageType {
    ROOT_HEARTBEAT = 0,
    PROXY_HEARTBEAT = 1,
    DATA_REQUEST = 2,
    DATA_RESPONSE_OK = 3,
    DATA_RESPONSE_ELSEWHERE = 4,
    DATA_RESPONSE_UNKNOWN = 5,
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
    fileAddr: string;
    offset: number;
    length: number;

    constructor(fileAddr: string, offset: number, length: number) {
        super(MessageType.DATA_REQUEST);
        this.fileAddr = fileAddr;
        this.offset = offset;
        this.length = length;
    }

    static fromBuffer(buffer: Buffer): DataRequestMessage {
        const hash = buffer.slice(0, 32).toString('hex');
        const size = buffer.readBigUInt64BE(32).toString();
        const fileAddr = `${hash}:${size}`;
        const offset = buffer.readInt32BE(40);
        const length = buffer.readInt32BE(44);
        return new DataRequestMessage(fileAddr, offset, length);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(48);
        // (32 bytes hash, 8 bytes size, 4 bytes offset, 4 bytes length)
        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(buffer, 0);

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(buffer, 32);

        buffer.writeInt32BE(this.offset, 40);
        buffer.writeInt32BE(this.length, 44);
        return buffer;
    }
}

export class DataResponseOkMessage extends Message {
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

    static fromBuffer(buffer: Buffer): DataResponseOkMessage {
        const hash = buffer.slice(0, 32).toString('hex');
        const size = buffer.readBigUInt64BE(32).toString();
        const fileAddr = `${hash}:${size}`;
        const offset = buffer.readInt32BE(40);
        const length = buffer.readInt32BE(44);
        const data = buffer.slice(48);
        return new DataResponseOkMessage(fileAddr, offset, length, data);
    }

    encode(): Buffer {
        const headerBuffer = Buffer.allocUnsafe(48);
        // (32 bytes hash, 8 bytes size, 4 bytes offset, 4 bytes length)
        const [hash, size] = this.fileAddr.split(':');
        const hashBuffer = Buffer.from(hash, 'hex');
        hashBuffer.copy(headerBuffer, 0);

        const sizeBuffer = Buffer.alloc(8);
        sizeBuffer.writeBigUInt64BE(BigInt(size), 0);
        sizeBuffer.copy(headerBuffer, 32);

        headerBuffer.writeInt32BE(this.offset, 40);
        headerBuffer.writeInt32BE(this.length, 44);
        return Buffer.concat([headerBuffer, this.data]);
    }
}

export class DataResponseElsewhereMessage extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DATA_RESPONSE_ELSEWHERE);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DataResponseElsewhereMessage {
        const hash = buffer.slice(0, 32).toString('hex');
        const size = buffer.readBigUInt64BE(32).toString();
        const fileAddr = `${hash}:${size}`;
        return new DataResponseElsewhereMessage(fileAddr);
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

export class DataResponseUnknownMessage extends Message {
    fileAddr: string;

    constructor(fileAddr: string) {
        super(MessageType.DATA_RESPONSE_UNKNOWN);
        this.fileAddr = fileAddr;
    }

    static fromBuffer(buffer: Buffer): DataResponseUnknownMessage {
        const hash = buffer.slice(0, 32).toString('hex');
        const size = buffer.readBigUInt64BE(32).toString();
        const fileAddr = `${hash}:${size}`;
        return new DataResponseUnknownMessage(fileAddr);
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
};
