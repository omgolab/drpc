import { discoverOptimalConnection } from '../../src/client/core/peer-discovery.js';
import { createLibp2pHost } from 'src/client/core/libp2p-host.js';

async function getTargetPeerIdFromRelay(): Promise<string> {
    try {
        const response = await fetch('http://localhost:8080/relay-node');
        const data = await response.json();

        // Extract the last peer ID from the libp2p_ma multiaddr
        // Format: /ip4/127.0.0.1/tcp/63258/p2p/12D3KooWA67R3DCqmRWLzMPvxhaUkRtzur56AaFbTbf1uCovhYRz/p2p-circuit/p2p/12D3KooWPssUeEn7dpVVYXZJzkQRZ8X5FkZi9N33zUBaHgWKGbtJ
        const libp2pMa = data.libp2p_ma;
        return libp2pMa
        // const parts = libp2pMa.split('/');
        // const targetPeerId = parts[parts.length - 1];

        // log(`🎯 Extracted target peer ID: ${targetPeerId}`);
        // return targetPeerId;
    } catch (error) {
        throw new Error(`Failed to fetch relay node: ${error instanceof Error ? error.message : String(error)}`);
    }
}

function log(message: string, ...args: any[]) {
    console.log(`[${new Date().toISOString()}] ${message}`, ...args);
}

async function main(): Promise<void> {
    // Get the target peer ID from the relay endpoint
    const TARGET_PEER_ID_STR = await getTargetPeerIdFromRelay();

    log(`Looking for target peer: ${TARGET_PEER_ID_STR}`);

    // Create a new libp2p host
    const h = await createLibp2pHost();

    console.log(`Node started with ID: ${h.peerId.toString()}`);

    // Try to connect with custom search options 
    // These values can be optimized using the optimize-interval.test.ts tool
    const result = await discoverOptimalConnection(h, TARGET_PEER_ID_STR, {
        timeoutMs: 60000,          // Maximum time to wait for connection
        dialTimeoutMs: 1000,       // Timeout for individual dial attempts  
        connectIntervalMs: 100     // Interval between connection retry attempts
    });

    if (result.success) {
        console.log(`✅ Connection successful in ${result.totalTimeSeconds}s`);
        console.log(`📋 Method: ${result.method}`);
        if (result.multiaddr) {
            console.log(`🔗 Address: ${result.multiaddr}`);
        }
        process.exit(0);
    } else {
        console.log(`❌ Connection failed after ${result.totalTimeSeconds}s: ${result.error}`);
        process.exit(1);
    }
}

main().catch(console.error);
