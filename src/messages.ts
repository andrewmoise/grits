const MAGIC_BYTES = [140, 98];

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
    abstract decode(bytes: Uint8Array): void;
}

export class RootHeartbeatMessage extends Message {
    static headerBuffer = Buffer.from([
        MAGIC_BYTES[0],
        MAGIC_BYTES[1],
        MessageType.ROOT_HEARTBEAT,
    ]);

    nodeMapHash: Buffer | null;

    constructor(nodeMapHash: Buffer | null = null) {
        super(MessageType.ROOT_HEARTBEAT);
        this.nodeMapHash = nodeMapHash;
    }

    encode(): Buffer {
        const encodedMessage = Buffer.concat([
            RootHeartbeatMessage.headerBuffer,
            this.nodeMapHash!,
        ])

        return encodedMessage;
    }

    decode(encodedMessage: Buffer): void {
        const nodeMapHash = encodedMessage.slice(7, 39);
        this.nodeMapHash = nodeMapHash;
    }
}

export class ProxyHeartbeatMessage extends Message {
    static headerBuffer = Buffer.from([
        MAGIC_BYTES[0],
        MAGIC_BYTES[1],
        MessageType.PROXY_HEARTBEAT,
    ]);

    proxyId: number;

    constructor(proxyId = -1) {
        super(MessageType.PROXY_HEARTBEAT);
        this.proxyId = proxyId;
    }

    encode(): Buffer {
        const proxyIdBuffer = Buffer.alloc(4);
        proxyIdBuffer.writeInt32BE(this.proxyId, 0);

        const encodedMessage = Buffer.concat([
            ProxyHeartbeatMessage.headerBuffer,
            proxyIdBuffer,
        ]);

        return encodedMessage;
    }

    decode(encodedMessage: Buffer): void {
        this.proxyId = encodedMessage.readUInt32BE(3);
    }
}

export class DataRequestMessage extends Message {
    dataHash: string;
    offset: number;
    length: number;

    constructor(dataHash: string, offset: number, length: number) {
        super(MessageType.DATA_REQUEST);
        this.dataHash = dataHash;
        this.offset = offset;
        this.length = length;
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(40);
        // (32 bytes dataHash, 4 bytes offset, 4 bytes length)
        buffer.write(this.dataHash, 0);
        buffer.writeInt32BE(this.offset, 32);
        buffer.writeInt32BE(this.length, 36);
        return buffer;
    }

    decode(bytes: Buffer): void {
        this.dataHash = bytes.toString('utf-8', 0, 32);
        this.offset = bytes.readInt32BE(32);
        this.length = bytes.readInt32BE(36);
    }
}

export class DataResponseOkMessage extends Message {
    dataHash: string;
    offset: number;
    length: number;
    data: Buffer;

    constructor(dataHash: string, offset: number, length: number, data: Buffer) {
        super(MessageType.DATA_RESPONSE_OK);
        this.dataHash = dataHash;
        this.offset = offset;
        this.length = length;
        this.data = data;
    }

    encode(): Buffer {
        const headerBuffer = Buffer.allocUnsafe(40);
        // (32 bytes dataHash, 4 bytes offset, 4 bytes length)

        headerBuffer.write(this.dataHash, 0);
        headerBuffer.writeInt32BE(this.offset, 32);
        headerBuffer.writeInt32BE(this.length, 36);
        return Buffer.concat([headerBuffer, this.data]);
    }

    decode(bytes: Buffer): void {
        this.dataHash = bytes.toString('utf-8', 0, 32);
        this.offset = bytes.readInt32BE(32);
        this.length = bytes.readInt32BE(36);
        this.data = bytes.slice(40);
    }
}

export class DataResponseElsewhereMessage extends Message {
    dataHash: string;

    constructor(dataHash: string) {
        super(MessageType.DATA_RESPONSE_ELSEWHERE);
        this.dataHash = dataHash;
    }

    encode(): Buffer {
        const buffer = Buffer.from(this.dataHash);
        return buffer;
    }

    decode(bytes: Buffer): void {
        this.dataHash = bytes.toString('utf-8', 0, 32);
    }
}

export class DataResponseUnknownMessage extends Message {
    dataHash: string;

    constructor(dataHash: string) {
        super(MessageType.DATA_RESPONSE_UNKNOWN);
        this.dataHash = dataHash;
    }

    encode(): Buffer {
        const buffer = Buffer.from(this.dataHash);
        return buffer;
    }

    decode(bytes: Buffer): void {
        this.dataHash = bytes.toString('utf-8', 0, 32);
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
