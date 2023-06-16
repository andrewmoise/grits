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

    heartbeatIndex: number;
    nodeMapHash: Buffer | null;

    constructor(heartbeatIndex = -1, nodeMapHash: Buffer | null = null) {
        super(MessageType.ROOT_HEARTBEAT);
        this.heartbeatIndex = heartbeatIndex;
        this.nodeMapHash = nodeMapHash;
    }

    encode(): Buffer {
        const heartbeatIndexBuffer = Buffer.alloc(4);
        heartbeatIndexBuffer.writeInt32BE(this.heartbeatIndex, 0);

        const encodedMessage = Buffer.concat([
            RootHeartbeatMessage.headerBuffer,
            heartbeatIndexBuffer,
            this.nodeMapHash!,
        ])

        return encodedMessage;
    }

    decode(encodedMessage: Buffer): void {
        const heartbeatIndex = encodedMessage.readUInt32BE(3);
        const nodeMapHash = encodedMessage.slice(7, 39);

        this.heartbeatIndex = heartbeatIndex;
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
    sequenceNumber: number;
    dataHash: string;
    offset: number;
    length: number;

    constructor(sequenceNumber: number, dataHash: string, offset: number, length: number) {
        super(MessageType.DATA_REQUEST);
        this.sequenceNumber = sequenceNumber;
        this.dataHash = dataHash;
        this.offset = offset;
        this.length = length;
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(38); // 2 bytes sequenceNumber, 32 bytes dataHash, 2 bytes offset, 2 bytes length
        buffer.writeUInt16BE(this.sequenceNumber, 0);
        buffer.write(this.dataHash, 2);
        buffer.writeUInt16BE(this.offset, 34);
        buffer.writeUInt16BE(this.length, 36);
        return buffer;
    }

    decode(bytes: Buffer): void {
        this.sequenceNumber = bytes.readUInt16BE(0);
        this.dataHash = bytes.toString('utf-8', 2, 34);
        this.offset = bytes.readUInt16BE(34);
        this.length = bytes.readUInt16BE(36);
    }
}

export class DataResponseOkMessage extends Message {
    sequenceNumber: number;
    offset: number;
    data: Buffer;

    constructor(sequenceNumber: number, offset: number, data: Buffer) {
        super(MessageType.DATA_RESPONSE_OK);
        this.sequenceNumber = sequenceNumber;
        this.offset = offset;
        this.data = data;
    }

    encode(): Buffer {
        const headerBuffer =
            Buffer.allocUnsafe(4); // 2 bytes sequenceNumber, 2 bytes offset
        headerBuffer.writeUInt16BE(this.sequenceNumber, 0);
        headerBuffer.writeUInt16BE(this.offset, 2);
        return Buffer.concat([headerBuffer, this.data]);
    }

    decode(bytes: Buffer): void {
        this.sequenceNumber = bytes.readUInt16BE(0);
        this.offset = bytes.readUInt16BE(2);
        this.data = bytes.slice(4);
    }
}

export class DataResponseElsewhereMessage extends Message {
    sequenceNumber: number;
    nodeCount: number;
    nodeAddresses: string[];

    constructor(sequenceNumber: number, nodeCount: number,
        nodeAddresses: string[]) {
        super(MessageType.DATA_RESPONSE_ELSEWHERE);
        this.sequenceNumber = sequenceNumber;
        this.nodeCount = nodeCount;
        this.nodeAddresses = nodeAddresses;
    }

    encode(): Buffer {
        // each IP address will take 4 bytes
        const addressBuffer = Buffer.allocUnsafe(this.nodeAddresses.length * 4);

        this.nodeAddresses.forEach((address, index) => {
            const [part1, part2, part3, part4] = address.split('.').map(Number);
            addressBuffer[index * 4] = part1;
            addressBuffer[index * 4 + 1] = part2;
            addressBuffer[index * 4 + 2] = part3;
            addressBuffer[index * 4 + 3] = part4;
        });
        const headerBuffer =
            Buffer.allocUnsafe(4); // 2 bytes sequenceNumber, 2 bytes nodeCount
        headerBuffer.writeUInt16BE(this.sequenceNumber, 0);
        headerBuffer.writeUInt16BE(this.nodeCount, 2);
        return Buffer.concat([headerBuffer, addressBuffer]);
    }

    decode(bytes: Buffer): void {
        this.sequenceNumber = bytes.readUInt16BE(0);
        this.nodeCount = bytes.readUInt16BE(2);
        this.nodeAddresses = [];
        for (let i = 4; i < bytes.length; i += 4) {
            const part1 = bytes[i].toString();
            const part2 = bytes[i + 1].toString();
            const part3 = bytes[i + 2].toString();
            const part4 = bytes[i + 3].toString();
            const address = `${part1}.${part2}.${part3}.${part4}`;
            this.nodeAddresses.push(address);
        }
    }
}

export class DataResponseUnknownMessage extends Message {
    sequenceNumber: number;

    constructor(sequenceNumber: number) {
        super(MessageType.DATA_RESPONSE_UNKNOWN);
        this.sequenceNumber = sequenceNumber;
    }

    encode(): Buffer {
        const buffer = Buffer.allocUnsafe(2); // 2 bytes sequenceNumber
        buffer.writeUInt16BE(this.sequenceNumber, 0);
        return buffer;
    }

    decode(bytes: Buffer): void {
        this.sequenceNumber = bytes.readUInt16BE(0);
    }
}

module.exports = {
    MessageType,
    Message,
    RootHeartbeatMessage,
    ProxyHeartbeatMessage,
};
