import { Config } from '../src/config';
import { ConsoleLogger } from '../src/logger';

import {
    Message, HeartbeatMessage, HeartbeatResponse, MessageType
} from '../src/messages';

import {
    NetworkManagerImpl, InRequest, OutRequest
} from '../src/network';

import { PeerNode, AllPeerNodes } from '../src/structures';

describe('NetworkManager', () => {

    let nm1: NetworkManagerImpl;
    let nm2: NetworkManagerImpl;
    let peerNodes: AllPeerNodes;

    beforeEach(() => {
        peerNodes = new AllPeerNodes();

        const config1 = new Config('127.0.0.1', 1787);
        config1.thisPort = 1800;
        const logger1 = new ConsoleLogger(config1);
        nm1 = new NetworkManagerImpl(peerNodes, logger1, config1);

        const config2 = new Config('127.0.0.1', 1787);
        config2.thisPort = 1801;
        const logger2 = new ConsoleLogger(config2);
        nm2 = new NetworkManagerImpl(peerNodes, logger2, config2);

        nm1.start();
        nm2.start();
    });

    afterEach(() => {
        nm1.stop();
        nm2.stop();
    });

    it('should send requests and receive responses', async () => {
        nm2.registerRequestHandler(
            MessageType.HEARTBEAT_MESSAGE,
            (request: InRequest, message: Message) => {
                const response = new HeartbeatResponse(null);
                request.sendResponse(response);
                return Promise.resolve();
            });

        const testMessage: Message = new HeartbeatMessage();

        const outRequest = nm1.newRequest(
            peerNodes.getPeerNode('127.0.0.1', 1801),
            testMessage
        );
        outRequest.sendRequest(testMessage);

        const response = await outRequest.getResponse();
        
        expect(response instanceof HeartbeatResponse).toEqual(true);
    }, 10000); // setting timeout for 10 seconds

    it('should handle no response and return null', async () => {
        const testMessage: Message = new HeartbeatMessage();
        const outRequest = nm1.newRequest(
            peerNodes.getPeerNode('127.0.0.1', 1801),
            testMessage
        );
        
        outRequest.sendRequest(testMessage);
        const response = await outRequest.getResponse();
        expect(response).toBeNull();
    }, 10000);

    it('should handle multiple responses in correct order', async () => {
        const testMessage: Message = new HeartbeatMessage();
        
        nm2.registerRequestHandler(
            MessageType.HEARTBEAT_MESSAGE,
            async (request: InRequest, message: Message) => {
                // Sleep before responding
                await new Promise(r => setTimeout(r, 100));
                
                // Send 10 responses
                for (let i = 0; i < 10; i++) {
                    request.sendResponse(new HeartbeatResponse(
                        `0000000000000000000000000000000000000000000000000000000000000001:${i}`));
                }
            }
        );
        
        const outRequest = nm1.newRequest(
            peerNodes.getPeerNode('127.0.0.1', 1801),
            testMessage
        );
        
        outRequest.sendRequest(testMessage);
        for (let i = 0; i < 10; i++) {
            const response = await outRequest.getResponse();
            expect(response).toBeInstanceOf(HeartbeatResponse);
            expect((response as HeartbeatResponse).nodeMapFileAddr).toEqual(`0000000000000000000000000000000000000000000000000000000000000001:${i}`);
        }
    }, 12000);

    it('should handle multiple messages with timeout', async () => {
        const testMessage: Message = new HeartbeatMessage();

        let responded = false;

        nm2.registerRequestHandler(
            MessageType.HEARTBEAT_MESSAGE,
            async (request: InRequest, message: Message) => {
                // Wait for a second
                await new Promise(r => setTimeout(r, 100));
                
                if (!responded) {
                    for (let i = 0; i < 3; i++) {
                        request.sendResponse(new HeartbeatResponse(`0000000000000000000000000000000000000000000000000000000000000001:${i}`));
                    }
                    responded = true;
                }
            }
        );
        
        const outRequest = nm1.newRequest(
            peerNodes.getPeerNode('127.0.0.1', 1801),
            testMessage
        );
        
        outRequest.sendRequest(testMessage);

        const responses: (Message | null)[] = [];
        
        for (let i = 0; i < 4; i++) {
            const response = await outRequest.getResponse();
            responses.push(response);
        }

        // First 3 responses should be valid
        for (let i = 0; i < 3; i++) {
            expect(responses[i]).toBeInstanceOf(HeartbeatResponse);
            if (responses[i])
                expect((responses[i] as HeartbeatResponse).nodeMapFileAddr).toEqual(`0000000000000000000000000000000000000000000000000000000000000001:${i}`);
        }
        
        // Remaining should be null (timed out)
        expect(responses[3]).toBeNull();
    }, 15000);
     
});
