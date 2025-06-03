import { createLibp2p } from 'libp2p';
import { webSockets } from '@libp2p/websockets';
import { webRTC, webRTCDirect } from '@libp2p/webrtc';
import { tcp } from '@libp2p/tcp';
import { webTransport } from '@libp2p/webtransport';
import { noise } from '@chainsafe/libp2p-noise';
import { yamux } from '@chainsafe/libp2p-yamux';
import { identify } from '@libp2p/identify';
import { circuitRelayTransport } from '@libp2p/circuit-relay-v2';

async function testTransport(name: string, transports: any[]) {
    console.log(`\n🧪 Testing ${name} transport...`);
    try {
        const node = await createLibp2p({
            addresses: { listen: [] },
            transports,
            connectionEncrypters: [noise()],
            streamMuxers: [yamux()],
            services: {
                identify: identify()
            }
        });
        
        console.log(`✅ ${name}: Successfully created node`);
        console.log(`   Node ID: ${node.peerId.toString()}`);
        
        await node.stop();
        return true;
    } catch (error) {
        console.log(`❌ ${name}: Failed to create node`);
        console.log(`   Error: ${error instanceof Error ? error.message : String(error)}`);
        return false;
    }
}

async function main() {
    console.log('🔬 Testing individual transports in Node.js environment...\n');
    
    const transports = [
        { name: 'TCP', transport: [tcp()] },
        { name: 'WebSockets', transport: [webSockets()] },
        { name: 'WebRTC', transport: [webRTC(), circuitRelayTransport()] },
        { name: 'WebRTC Direct', transport: [webRTCDirect()] },
        { name: 'WebTransport', transport: [webTransport()] },
    ];
    
    const results: Record<string, boolean> = {};
    
    for (const { name, transport } of transports) {
        results[name] = await testTransport(name, transport);
    }
    
    console.log('\n📊 Summary:');
    Object.entries(results).forEach(([name, success]) => {
        console.log(`   ${success ? '✅' : '❌'} ${name}`);
    });
    
    console.log('\n🎯 Testing combination that should work...');
    const workingTransports = Object.entries(results)
        .filter(([, success]) => success)
        .map(([name]) => name);
    
    if (workingTransports.length > 0) {
        console.log(`   Using: ${workingTransports.join(', ')}`);
        
        const combinedTransports = [];
        if (results['TCP']) combinedTransports.push(tcp());
        if (results['WebSockets']) combinedTransports.push(webSockets());
        if (results['WebRTC']) {
            combinedTransports.push(webRTC());
            combinedTransports.push(circuitRelayTransport()); // WebRTC requires this
        }
        if (results['WebRTC Direct']) combinedTransports.push(webRTCDirect());
        
        const success = await testTransport('Combined Working', combinedTransports);
        console.log(`   Combined result: ${success ? '✅' : '❌'}`);
    } else {
        console.log('   ❌ No working transports found!');
    }
}

main().catch(console.error);
