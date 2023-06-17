import { Config } from '../src/config';
import { ProxyManager, RootProxyManager } from '../src/proxy';
import { NetworkingManager } from '../src/network';
import { FileCache } from '../src/filecache';
import { MessageType } from '../src/messages';

describe('Proxy heartbeat exchange', () => {
    it('should exchange heartbeat messages', (done) => {
        // Create root node
        const rootConfig = new Config();
        rootConfig.thisHost = 'localhost';
        rootConfig.thisPort = 1787;
        rootConfig.isRootNode = true;

        const rootNode = new RootProxyManager(
            rootConfig,
            new NetworkingManager(rootConfig),
            new FileCache(rootConfig)
        );

        // Create non-root node
        const nonRootConfig = new Config();
        nonRootConfig.thisHost = 'localhost';
        nonRootConfig.thisPort = 1800;
        nonRootConfig.isRootNode = false;
        nonRootConfig.rootHost = 'localhost';
        nonRootConfig.rootPort = 1787;
        nonRootConfig.proxyHeartbeatPeriod = 1;

        const nonRootNode = new ProxyManager(
            nonRootConfig,
            new NetworkingManager(nonRootConfig),
            new FileCache(nonRootConfig)
        );

        // Counters for heartbeat messages
        let rootHeartbeatCount = 0;
        let nonRootHeartbeatCount = 0;

        // Start nodes
        rootNode.start();
        nonRootNode.start();

        // Listen to root heartbeat messages
        nonRootNode.networkingManager.registerRequestHandler(
            MessageType.ROOT_HEARTBEAT,
            () => {
                nonRootHeartbeatCount++;
            }
        );

        // Listen to proxy heartbeat messages
        rootNode.networkingManager.registerRequestHandler(
            MessageType.PROXY_HEARTBEAT,
            () => {
                rootHeartbeatCount++;
            }
        );

        // Wait for 5 seconds and then stop both nodes
        setTimeout(() => {
            rootNode.stop();
            nonRootNode.stop();

            // Check if the heartbeat counters have increased
            expect(rootHeartbeatCount).toBeGreaterThan(0);
            expect(nonRootHeartbeatCount).toBeGreaterThan(0);

            done();
        }, 5000);
    });
});
