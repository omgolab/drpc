import { createLibp2pHost } from "../../src/client/core/libp2p-host";
import { discoverOptimalConnectPath } from "./discover";

async function getTargetFromRelay(): Promise<string> {
    const response = await fetch('http://localhost:8080/relay-node');
    const data = await response.json();
    return data.libp2p_ma;
}

export async function main(): Promise<void> {
    console.log('🚀 Starting discoverOptimalConnectPath tests...');

    const h = await createLibp2pHost();
    const circuitAddr = await getTargetFromRelay();

    // Extract components for test cases
    const parts = circuitAddr.split('/');
    const targetPeerId = parts[parts.length - 1];

    console.log(`🎯 Base circuit address: ${circuitAddr}`);
    console.log(`🎯 Target peer ID: ${targetPeerId}\n`);

    // Test cases
    const testCases = [
        {
            name: "Type 1: Circuit relay path",
            input: circuitAddr,
            description: "Full circuit relay address"
        },
        {
            name: "Type 2: Only p2p multiaddr - no address",
            input: `/p2p/${targetPeerId}`,
            description: "Peer ID with /p2p/ prefix only"
        },
        {
            name: "Type 3: Direct multiaddr with address",
            input: `/ip4/127.0.0.1/tcp/57905/p2p/${targetPeerId}`, // Use actual port from circuit addr
            description: "Direct address with peer ID"
        },
        {
            name: "Type 4: Only peer ID",
            input: targetPeerId,
            description: "Raw peer ID string"
        }
    ];

    for (let i = 0; i < testCases.length; i++) {
        const testCase = testCases[i];
        console.log(`\n=== Test ${i + 1}/4: ${testCase.name} ===`);
        console.log(`📝 Description: ${testCase.description}`);
        console.log(`🔗 Input: ${testCase.input}`);

        try {
            const startTime = Date.now();
            const result = await discoverOptimalConnectPath(h, testCase.input);
            const testTime = Date.now() - startTime;

            if (result.addr) {
                console.log(`✅ SUCCESS via ${result.method}!`);
                console.log(`   └─ Address: ${result.addr}`);
                console.log(`   └─ Status: ${result.status}`);
                console.log(`   └─ Connect time: ${result.connectTime}ms`);
                console.log(`   └─ Total time: ${result.totalTime.toFixed(2)}s`);
                console.log(`   └─ Test time: ${testTime}ms`);

                // Quick verification
                const verifyStart = Date.now();
                const connection = await h.dial(result.addr).catch(() => null);
                const verifyTime = Date.now() - verifyStart;
                console.log(`   └─ Verification: ${connection ? '✅' : '❌'} (${verifyTime}ms)`);
            } else {
                console.log(`❌ FAILED after ${result.totalTime.toFixed(2)}s`);
                console.log(`   └─ Status: ${result.status}`);
            }
        } catch (error) {
            console.error(`❌ ERROR: ${error}`);
        }

        // Short delay between tests
        if (i < testCases.length - 1) {
            console.log('\n⏳ Waiting 2s before next test...');
            await new Promise(resolve => setTimeout(resolve, 2000));
        }
    }

    console.log('\n🏁 All tests completed!');
    await h.stop();
}