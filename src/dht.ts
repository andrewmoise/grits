import * as crypto from 'crypto';

import { AllPeerNodes, PeerNode } from './structures';

function convertPeerToInteger(peer: PeerNode): number {
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
    private sortedPeers: Array<[number, PeerNode]>;
    
    constructor() {
        this.sortedPeers = [];
    }
    
    updatePeers(allPeers: AllPeerNodes) {
        const peers: Map<string, PeerNode> = allPeers.peerNodes;

        this.sortedPeers = Array.from(peers.values())
            .map((peer): [number, PeerNode] => [
                convertPeerToInteger(peer), peer])
            .sort(([intIpPortA], [intIpPortB]) => intIpPortA - intIpPortB);
    }
    
    getClosestPeers(fileAddr: string, n: number): Array<PeerNode> {
        //console.log(`    GCP ${fileAddr} from ${this.sortedPeers.length}`);

        if (this.sortedPeers.length <= 0) {
            throw new Error('No peers');
            //console.log('      Early bail, no peers');
            return [];
        }
        
        const target = convertFileAddrToInteger(fileAddr);
        //console.log(`      Searching for ${target}`);
            
        let left = 0;
        let right = this.sortedPeers.length - 1;
        while (right - left > 1) {
            //console.log(`        [${left}: ${this.sortedPeers[left][0]}] - [${right}: ${this.sortedPeers[right][0]}]`);
            const mid = Math.floor((right + left) / 2);
            if (this.sortedPeers[mid][0] <= target)
                left = mid;
            else
                right = mid;
        }

        const result: PeerNode[] = [];
        let i = left;
        let j = 0; // Count of added elements
        while (j < n) {
            // Use modulo to wrap around if we reach the end of the array
            let index = i % this.sortedPeers.length;
            
            // Add to result if this peer is not already included
            if (!result.includes(this.sortedPeers[index][1]))
                result.push(this.sortedPeers[index][1]);
            else
                break;
            
            i++;
            j++;
        }
        
        //console.log(`Returning GCP: ${result.length}`);
        return result;
    }
}

export { BlobFinder };
