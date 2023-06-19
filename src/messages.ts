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
    nodeMapHash: string | null;

    static zeroBuffer = Buffer.alloc(32, 0);
    
    constructor(nodeMapHash: string | null) {
        super(MessageType.ROOT_HEARTBEAT);
        this.nodeMapHash = nodeMapHash;
    }

    static fromBuffer(buffer: Buffer): RootHeartbeatMessage {
        if (buffer.equals(this.zeroBuffer))
            return new RootHeartbeatMessage(null);
        else
            return new RootHeartbeatMessage(buffer.toString('hex'));
    }

    encode(): Buffer {
        if (this.nodeMapHash === null)
            return RootHeartbeatMessage.zeroBuffer;
        else
            return Buffer.from(this.nodeMapHash, 'hex');
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
    dataHash: Buffer;
    offset: number;
    length: number;

    constructor(dataHash: Buffer, offset: number, length: number) {
        super(MessageType.DATA_REQUEST);
        this.dataHash = dataHash;
        this.offset = offset;
        this.length = length;
    }

    static fromBuffer(buffer: Buffer): DataRequestMessage {
        const dataHash = Buffer.alloc(32);
        buffer.copy(dataHash, 0, 0, 32);
        const offset = buffer.readInt32BE(32);
        const length = buffer.readInt32BE(36);
        return new DataRequestMessage(dataHash, offset, length);
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(40);
        // (32 bytes dataHash, 4 bytes offset, 4 bytes length)
        this.dataHash.copy(buffer, 0);
        buffer.writeInt32BE(this.offset, 32);
        buffer.writeInt32BE(this.length, 36);
        return buffer;
    }
}

export class DataResponseOkMessage extends Message {
    dataHash: Buffer;
    offset: number;
    length: number;
    data: Buffer;

    constructor(dataHash: Buffer, offset: number, length: number, data: Buffer) {
        super(MessageType.DATA_RESPONSE_OK);
        this.dataHash = dataHash;
        this.offset = offset;
        this.length = length;
        this.data = data;
    }

    static fromBuffer(buffer: Buffer): DataResponseOkMessage {
        const dataHash = Buffer.alloc(32);
        buffer.copy(dataHash, 0, 0, 32);
        const offset = buffer.readInt32BE(32);
        const length = buffer.readInt32BE(36);
        const data = buffer.slice(40);
        return new DataResponseOkMessage(dataHash, offset, length, data);
    }

    encode(): Buffer {
        const headerBuffer = Buffer.allocUnsafe(40);
        // (32 bytes dataHash, 4 bytes offset, 4 bytes length)
        headerBuffer.copy(this.dataHash);
        headerBuffer.writeInt32BE(this.offset, 32);
        headerBuffer.writeInt32BE(this.length, 36);
        return Buffer.concat([headerBuffer, this.data]);
    }
}

export class DataResponseElsewhereMessage extends Message {
    dataHash: Buffer;

    constructor(dataHash: Buffer) {
        super(MessageType.DATA_RESPONSE_ELSEWHERE);
        this.dataHash = dataHash;
    }

    static fromBuffer(buffer: Buffer): DataResponseElsewhereMessage {
        return new DataResponseElsewhereMessage(buffer);
    }

    encode(): Buffer {
        return Buffer.from(this.dataHash);
    }
}

export class DataResponseUnknownMessage extends Message {
    dataHash: Buffer;

    constructor(dataHash: Buffer) {
        super(MessageType.DATA_RESPONSE_UNKNOWN);
        this.dataHash = dataHash;
    }

    static fromBuffer(buffer: Buffer): DataResponseUnknownMessage {
        return new DataResponseUnknownMessage(buffer);
    }

    encode(): Buffer {
        return Buffer.from(this.dataHash);
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
