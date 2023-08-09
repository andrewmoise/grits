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

        nm2.registerRequestHandler(
            MessageType.HEARTBEAT_MESSAGE,
            (request: InRequest, message: Message) => {
                const response = new HeartbeatResponse(null);
                request.sendResponse(response);
                return Promise.resolve();
            });
    });

    afterEach(() => {
        nm1.stop();
        nm2.stop();
    });

    it('should send requests and receive responses', async () => {
        const testMessage: Message = new HeartbeatMessage();

        const outRequest = nm1.newRequest(
            peerNodes.getPeerNode('127.0.0.1', 1801),
            testMessage
        );
        outRequest.sendRequest(testMessage);

        const response = await outRequest.getResponse();
        
        expect(response instanceof HeartbeatResponse).toEqual(true);
    }, 10000); // setting timeout for 10 seconds

});
