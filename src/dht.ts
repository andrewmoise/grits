import * as crypto from 'crypto';

import { PeerProxy } from './structures';

function convertPeerProxyToInteger(peer: PeerProxy): number {
    const sha = crypto.createHash('sha256');
    sha.update(peer.ip + ':' + peer.port + ':evenly distribute hash, please');
    const hash = sha.digest('hex');
    return parseInt(hash.slice(0, 8), 16);
}

function convertFileAddrToInteger(fileAddr: string): number {
    const firstEightChars = fileAddr.slice(0, 8);
    const intValue = parseInt(firstEightChars, 16);
    return intValue;
}

class BlobFinder {
    private sortedProxies: Array<[number, PeerProxy]>;
    
    constructor() {
        this.sortedProxies = [];
    }
    
    updateProxies(proxies: Map<string, PeerProxy>) {
        this.sortedProxies = Array.from(proxies.values())
            .map((proxy): [number, PeerProxy] => [
                convertPeerProxyToInteger(proxy), proxy])
            .sort(([intIpPortA], [intIpPortB]) => intIpPortA - intIpPortB);

    }
    
    getClosestProxies(fileAddr: string, n: number): Array<PeerProxy> {
        //console.log(`    GCP ${fileAddr}`);

        if (this.sortedProxies.length <= 0) {
            console.log('      Early bail, no proxies');
            return [];
        }
        
        const target = convertFileAddrToInteger(fileAddr);
        //console.log(`      Searching for ${target}`);
            
        let left = 0;
        let right = this.sortedProxies.length - 1;
        while (right - left > 1) {
            //console.log(`        [${left}: ${this.sortedProxies[left][0]}] - [${right}: ${this.sortedProxies[right][0]}]`);
            const mid = Math.floor((right + left) / 2);
            if (this.sortedProxies[mid][0] <= target)
                left = mid;
            else
                right = mid;
        }

        //console.log(`      ${target} -> ${left}: ${this.sortedProxies[left][0]}`);

        // Apply wrapping logic if required
        const range = left + n <= this.sortedProxies.length
            ? this.sortedProxies.slice(left, left + n)
            : this.sortedProxies.slice(left).concat(this.sortedProxies.slice(
                0, n + left - this.sortedProxies.length));

        return range.map(([, proxy]) => proxy);
    }
}

export { BlobFinder };
