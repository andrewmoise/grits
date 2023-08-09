import {
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
} from '../src/messages';

describe('Message Encoding and Decoding', () => {

    it('should encode and decode HeartbeatMessage correctly', () => {
        const originalMessage = new HeartbeatMessage();

        const encodedMessage = originalMessage.encode();
        const decodedMessage = HeartbeatMessage.fromBuffer(encodedMessage);
        
        expect(decodedMessage).toBeInstanceOf(HeartbeatMessage);
        expect(encodedMessage.length).toBe(originalMessage.transferSize());

        const originalResponse = new HeartbeatResponse('0000000000000000000000000000000000000000000000000000000000000001:1');

        const encodedResponse = originalResponse.encode();
        const decodedResponse = HeartbeatResponse.fromBuffer(encodedResponse);

        expect(decodedResponse).toBeInstanceOf(HeartbeatResponse);
        expect(decodedResponse.nodeMapFileAddr).toBe(
            originalResponse.nodeMapFileAddr);
        expect(encodedResponse.length).toBe(originalMessage.responseSize());
        expect(encodedResponse.length).toBe(originalResponse.transferSize());
    });

    it('should encode and decode DataFetchMessage correctly', () => {
        const mockData = 'Some mock data';
        const originalMessage = new DataFetchMessage('0000000000000000000000000000000000000000000000000000000000000001:1', 0, mockData.length, 'abcdefgh');

        const encodedMessage = originalMessage.encode();
        const decodedMessage = DataFetchMessage.fromBuffer(encodedMessage);

        expect(decodedMessage).toBeInstanceOf(DataFetchMessage);
        expect(decodedMessage.fileAddr).toBe(originalMessage.fileAddr);
        expect(decodedMessage.offset).toBe(originalMessage.offset);
        expect(decodedMessage.length).toBe(originalMessage.length);
        expect(decodedMessage.transferId).toBe(originalMessage.transferId);
        expect(encodedMessage.length).toBe(originalMessage.transferSize());

        const dataBuffer = Buffer.from(mockData);
        const originalResponse = new DataFetchResponseOk('0000000000000000000000000000000000000000000000000000000000000001:1', 0, dataBuffer.length, dataBuffer);

        const encodedResponse = originalResponse.encode();
        const decodedResponse = DataFetchResponseOk.fromBuffer(encodedResponse);

        expect(decodedResponse).toBeInstanceOf(DataFetchResponseOk);
        expect(decodedResponse.fileAddr).toBe(originalResponse.fileAddr);
        expect(decodedResponse.offset).toBe(originalResponse.offset);
        expect(decodedResponse.length).toBe(originalResponse.length);
        expect(decodedResponse.data.toString()).toBe(dataBuffer.toString());
        expect(encodedResponse.length).toBe(originalMessage.responseSize());
        expect(encodedResponse.length).toBe(originalResponse.transferSize());
    });

    it('should encode and decode DataFetchResponseNo correctly', () => {
        const originalResponse = new DataFetchResponseNo('0000000000000000000000000000000000000000000000000000000000000001:1');

        const encodedResponse = originalResponse.encode();
        const decodedResponse = DataFetchResponseNo.fromBuffer(encodedResponse);

        expect(decodedResponse).toBeInstanceOf(DataFetchResponseNo);
        expect(decodedResponse.fileAddr).toBe(originalResponse.fileAddr);
        expect(encodedResponse.length).toBe(originalResponse.transferSize());
    });

    it('should encode and decode DhtStoreMessage correctly', () => {
        const originalMessage = new DhtStoreMessage('0000000000000000000000000000000000000000000000000000000000000001:1');

        const encodedMessage = originalMessage.encode();
        const decodedMessage = DhtStoreMessage.fromBuffer(encodedMessage);

        expect(decodedMessage).toBeInstanceOf(DhtStoreMessage);
        expect(decodedMessage.fileAddr).toBe(originalMessage.fileAddr);
        expect(encodedMessage.length).toBe(originalMessage.transferSize());

        const originalResponse = new DhtStoreResponse('0000000000000000000000000000000000000000000000000000000000000001:1');

        const encodedResponse = originalResponse.encode();
        const decodedResponse = DhtStoreResponse.fromBuffer(encodedResponse);

        expect(decodedResponse).toBeInstanceOf(DhtStoreResponse);
        expect(decodedResponse.fileAddr).toBe(originalResponse.fileAddr);
        expect(encodedResponse.length).toBe(originalMessage.responseSize());
        expect(encodedResponse.length).toBe(originalResponse.transferSize());
    });

    it('should encode and decode DhtLookupMessage correctly', () => {
        const originalMessage = new DhtLookupMessage('0000000000000000000000000000000000000000000000000000000000000001:1', 'abcdefgh');

        const encodedMessage = originalMessage.encode();
        const decodedMessage = DhtLookupMessage.fromBuffer(encodedMessage);

        expect(decodedMessage).toBeInstanceOf(DhtLookupMessage);
        expect(decodedMessage.fileAddr).toBe(originalMessage.fileAddr);
        expect(decodedMessage.transferId).toBe(originalMessage.transferId);
        expect(encodedMessage.length).toBe(originalMessage.transferSize());

        const nodes = [
            { ip: '192.168.1.1', port: 8081 },
            { ip: '192.168.1.2', port: 8082 },
            { ip: '192.168.1.3', port: 8083 },
            { ip: '192.168.1.4', port: 8084 },
            { ip: '192.168.1.5', port: 8085 },
            { ip: '192.168.1.6', port: 8086 },
            { ip: '192.168.1.7', port: 8087 },
            { ip: '192.168.1.8', port: 8088 },
            { ip: '192.168.1.9', port: 8089 },
            { ip: '192.168.1.10', port: 8090 },
        ];
        const originalResponse = new DhtLookupResponse('0000000000000000000000000000000000000000000000000000000000000001:1', nodes);

        const encodedResponse = originalResponse.encode();
        const decodedResponse = DhtLookupResponse.fromBuffer(encodedResponse);

        expect(decodedResponse).toBeInstanceOf(DhtLookupResponse);
        expect(decodedResponse.fileAddr).toBe(originalResponse.fileAddr);
        expect(decodedResponse.nodeInfo.length).toBe(nodes.length);
        decodedResponse.nodeInfo.forEach((node, index) => {
            expect(node.ip).toBe(nodes[index].ip);
            expect(node.port).toBe(nodes[index].port);
        });
        expect(encodedResponse.length).toBe(originalMessage.responseSize());
        expect(encodedResponse.length).toBe(originalResponse.transferSize());
    });

    it('should encode and decode TelemetryFetchMessage correctly', () => {
        const originalMessage = new TelemetryFetchMessage(12345);

        const encodedMessage = originalMessage.encode();
        const decodedMessage = TelemetryFetchMessage.fromBuffer(encodedMessage);

        expect(decodedMessage).toBeInstanceOf(TelemetryFetchMessage);
        expect(decodedMessage.telemetryBatchId).toBe(originalMessage.telemetryBatchId);
        expect(encodedMessage.length).toBe(originalMessage.transferSize());

        const originalResponse = new TelemetryFetchResponse(12345);

        const encodedResponse = originalResponse.encode();
        const decodedResponse = TelemetryFetchResponse.fromBuffer(
            encodedResponse);

        expect(decodedResponse).toBeInstanceOf(TelemetryFetchResponse);
        expect(decodedResponse.byteCount).toBe(originalResponse.byteCount);
        expect(encodedResponse.length).toBe(originalMessage.responseSize());
        expect(encodedResponse.length).toBe(originalResponse.transferSize());
    });
    
});
