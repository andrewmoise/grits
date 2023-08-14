import { Config } from '../src/config';
import { LogfileLogger, NullLogger } from '../src/logger';

import {
    Message, MessageType, DataFetchMessage, DataFetchResponseOk
} from '../src/messages';

import { InRequest, NetworkManagerImpl } from '../src/network';
import { AllPeerNodes, PeerNode, DOWNLOAD_CHUNK_SIZE } from '../src/structures';

describe('Traffic Management', () => {
    let nm1: NetworkManagerImpl;
    let nm2: NetworkManagerImpl;
    let peerNodes: AllPeerNodes;

    const NUMBER_REQUESTS = 10;
    const NUMBER_CHUNKS = 10;
    const MAX_SPEED = 10240;
    
    beforeEach(() => {
        peerNodes = new AllPeerNodes();
    });

    it('should limit aggregate upstream speed', async () => {
        const BASE_PORT = 1900;

        const config1 = new Config('127.0.0.1', 1787);
        config1.thisPort = BASE_PORT;
        //config1.logFile = 'grits-1.log';
        const logger1 = new NullLogger();
        await logger1.start();
        nm1 = new NetworkManagerImpl(peerNodes, logger1, config1);

        const config2 = new Config('127.0.0.1', 1787);
        config2.thisPort = BASE_PORT + 1;
        //config2.logFile = 'grits-2.log';
        config2.maxUpstreamSpeed = 10240;
        const logger2 = new NullLogger();
        await logger2.start();
        nm2 = new NetworkManagerImpl(peerNodes, logger2, config2);

        nm1.start();
        nm2.start();

        const startTime = Date.now();

        nm2.registerRequestHandler(
            MessageType.DATA_FETCH_MESSAGE,
            async (request: InRequest, message: Message) => {
                const dataFetchMsg = message as DataFetchMessage;

                for (let i = 0; i < NUMBER_CHUNKS; i++) {
                    // random data with length 1400
                    const randomData = Buffer.alloc(
                        DOWNLOAD_CHUNK_SIZE, Math.random().toString(36));
                    const response = new DataFetchResponseOk(
                        dataFetchMsg.fileAddr, i * DOWNLOAD_CHUNK_SIZE,
                        DOWNLOAD_CHUNK_SIZE, randomData);

                    await nm2.requestTransfer(request.source, response);
                    request.sendResponse(response);
                }

                return Promise.resolve();
            }
        );

        const testMessage = new DataFetchMessage('0000000000000000000000000000000000000000000000000000000000000001:8192', 0, NUMBER_CHUNKS * DOWNLOAD_CHUNK_SIZE, 'abcdefgh');
        
        for (let i = 0; i < NUMBER_REQUESTS; i++) {
            let dest: PeerNode = peerNodes.getPeerNode(
                '127.0.0.1', BASE_PORT + 1);
            
            await nm1.requestTransfer(dest, testMessage);
            const outRequest = nm1.newRequest(dest, testMessage);
            while(true) {
                const response = await outRequest.getResponse();
                if (response === null)
                    break;

                expect(response).toBeInstanceOf(DataFetchResponseOk);

                if ((response as DataFetchResponseOk).offset
                    >= DOWNLOAD_CHUNK_SIZE * (NUMBER_CHUNKS-1))
                {
                    break;
                }
            }

            outRequest.close();
        }

        const endTime = Date.now();
        const totalTime = (endTime - startTime) / 1000;
        const targetTime = NUMBER_REQUESTS * NUMBER_CHUNKS * DOWNLOAD_CHUNK_SIZE
            / MAX_SPEED;
        
        // Here, we assert that the time is roughly right, with a
        // tolerance of +/- 20%
        expect(totalTime).toBeGreaterThanOrEqual(targetTime * .8);
        expect(totalTime).toBeLessThanOrEqual(targetTime * 1.2);

        nm1.stop();
        nm2.stop();

        logger1.stop();
        logger2.stop();
        
    }, 30000); // setting timeout for 20 seconds

    it('should limit aggregate downstream speed', async () => {
        const BASE_PORT = 2000;
        
        const config1 = new Config('127.0.0.1', 1787);
        config1.thisPort = BASE_PORT + 0;
        config1.maxDownstreamSpeed = 10240;
        config1.logFile = 'grits-1.log';
        const logger1 = new LogfileLogger(config1);
        await logger1.start();
        nm1 = new NetworkManagerImpl(peerNodes, logger1, config1);

        const config2 = new Config('127.0.0.1', 1787);
        config2.thisPort = BASE_PORT + 1;
        config2.logFile = 'grits-2.log';
        const logger2 = new LogfileLogger(config2);
        await logger2.start();
        nm2 = new NetworkManagerImpl(peerNodes, logger2, config2);

        nm1.start();
        nm2.start();

        const startTime = Date.now();

        nm2.registerRequestHandler(
            MessageType.DATA_FETCH_MESSAGE,
            async (request: InRequest, message: Message) => {
                const dataFetchMsg = message as DataFetchMessage;

                for (let i = 0; i < NUMBER_CHUNKS; i++) {
                    // random data with length 1400
                    const randomData = Buffer.alloc(
                        DOWNLOAD_CHUNK_SIZE, Math.random().toString(36));
                    const response = new DataFetchResponseOk(
                        dataFetchMsg.fileAddr, i * DOWNLOAD_CHUNK_SIZE,
                        DOWNLOAD_CHUNK_SIZE, randomData);

                    await nm2.requestTransfer(request.source, response);
                    request.sendResponse(response);
                }

                return Promise.resolve();
            }
        );

        const testMessage = new DataFetchMessage('0000000000000000000000000000000000000000000000000000000000000001:8192', 0, NUMBER_CHUNKS * DOWNLOAD_CHUNK_SIZE, 'abcdefgh');
        
        for (let i = 0; i < NUMBER_REQUESTS; i++) {
            let dest: PeerNode = peerNodes.getPeerNode(
                '127.0.0.1', BASE_PORT + 1);

            logger1.log('test', `Request transfer ${i}`);
            await nm1.requestTransfer(dest, testMessage);
            logger1.log('test', `Success request`);

            const outRequest = nm1.newRequest(dest, testMessage);
            while(true) {
                const response = await outRequest.getResponse();
                if (response === null)
                    break;

                expect(response).toBeInstanceOf(DataFetchResponseOk);

                if ((response as DataFetchResponseOk).offset
                    >= DOWNLOAD_CHUNK_SIZE * (NUMBER_CHUNKS-1))
                {
                    break;
                }
            }

            outRequest.close();
        }

        const endTime = Date.now();
        const totalTime = (endTime - startTime) / 1000;
        const targetTime = NUMBER_REQUESTS * NUMBER_CHUNKS * DOWNLOAD_CHUNK_SIZE
            / MAX_SPEED;
        
        // Here, we assert that the time is roughly right, with a
        // tolerance of +/- 20%
        expect(totalTime).toBeGreaterThanOrEqual(targetTime * .8);
        expect(totalTime).toBeLessThanOrEqual(targetTime * 1.2);

        nm1.stop();
        nm2.stop();

        logger1.stop();
        logger2.stop();
        
    }, 30000); // setting timeout for 20 seconds

});
