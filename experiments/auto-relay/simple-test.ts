#!/usr/bin/env tsx

/**
 * Simple test: Fetch target peer, apply tag, discover path, re-dial discovered path
 */

import { discoverOptimalConnection } from 'src/client/core/peer-discovery.js';
import { createLibp2pHost } from 'src/client/core/libp2p-host.js';
import { peerIdFromString } from '@libp2p/peer-id';

async function getTargetPeerIdFromRelay(): Promise<string> {
    try {
        const response = await fetch('http://localhost:8080/relay-node');
        const data = await response.json();
        const libp2pMa = data.libp2p_ma;
        const parts = libp2pMa.split('/');
        const targetPeerId = parts[parts.length - 1];
        console.log(`🎯 Target peer: ${targetPeerId}`);
        return targetPeerId;
    } catch (error) {
        throw new Error(`Failed to fetch relay node: ${error instanceof Error ? error.message : String(error)}`);
    }
}

async function simpleTest() {
    console.log('🔍 Simple Test: Fetch → Tag → Discover → Re-dial\n');

    // 1. Fetch target peer
    const targetPeerStr = await getTargetPeerIdFromRelay();
    const targetPeer = peerIdFromString(targetPeerStr);

    // 2. Create host
    const host = await createLibp2pHost();
    console.log(`🆔 Host ID: ${host.peerId.toString()}`);

    // 3. Discover path FIRST (without tag)
    console.log('\n🔍 Discovering path...');
    const result1 = await discoverOptimalConnection(host, targetPeerStr, {
        timeoutMs: 60000,
        dialTimeoutMs: 1000,
        connectIntervalMs: 100
    });

    if (!result1.success) {
        console.log(`❌ Discovery failed: ${result1.error}`);
        await host.stop();
        return;
    }

    console.log(`✅ Path discovered in ${result1.totalTimeSeconds.toFixed(2)}s`);
    console.log(`📍 Address: ${result1.multiaddr?.toString()}`);

    // 4. Apply high-priority tag to target peer AFTER successful connection
    console.log('\n🏷️  Applying priority tag...');
    await host.peerStore.merge(targetPeer, {
        tags: new Map([['target-connection', {
            value: 100,  // Maximum priority
            ttl: undefined  // No expiry
        }]])
    });
    console.log('✅ Tag applied');

    // 5. Re-dial the discovered path
    console.log('\n🔄 Re-dialing discovered path...');

    // Brief pause to simulate disconnection
    await new Promise(resolve => setTimeout(resolve, 1000));

    const result2 = await discoverOptimalConnection(host, targetPeerStr, {
        timeoutMs: 15000,  // Shorter timeout for re-dial
        dialTimeoutMs: 1000,
        connectIntervalMs: 100
    });

    if (result2.success) {
        console.log(`✅ Re-dial successful in ${result2.totalTimeSeconds.toFixed(2)}s`);
        console.log(`📍 Address: ${result2.multiaddr?.toString()}`);

        // Compare performance
        const improvement = ((result1.totalTimeSeconds - result2.totalTimeSeconds) / result1.totalTimeSeconds * 100);
        if (improvement > 0) {
            console.log(`⚡ ${improvement.toFixed(1)}% faster on re-dial!`);
        }
    } else {
        console.log(`❌ Re-dial failed: ${result2.error}`);
    }

    // 6. Show connection status
    const connections = host.getConnections();
    console.log(`\n📊 Final connections: ${connections.length}`);
    for (const conn of connections) {
        const peer = conn.remotePeer.toString().slice(0, 20) + '...';
        console.log(`   • ${peer} (${conn.status})`);
    }

    await host.stop();
    console.log('\n✅ Test completed!');
    process.exit(0);
}

if (import.meta.url === `file://${process.argv[1]}`) {
    simpleTest().catch(console.error);
}
