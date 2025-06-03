import { createLibp2pHost } from "../../src/client/core/libp2p-host";
import { discoverOptimalConnectPath } from "../../src/client/core/discover";

async function getTargetFromRelay(): Promise<string> {
    const response = await fetch('http://localhost:8080/relay-node');
    const data = await response.json();
    return data.libp2p_ma;
}

export interface TestCase {
    name: string;
    input: string;
    description: string;
}

export interface TestResult {
    success: boolean;
    testTime: number;
    result?: any;
    error?: string;
    verificationSuccess?: boolean;
    verificationTime?: number;
}

export async function getTestCases(): Promise<TestCase[]> {
    const circuitAddr = await getTargetFromRelay();
    const parts = circuitAddr.split('/');
    const targetPeerId = parts[parts.length - 1];

    return [
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
            input: `/ip4/127.0.0.1/tcp/57905/p2p/${targetPeerId}`,
            description: "Direct address with peer ID"
        },
        {
            name: "Type 4: Only peer ID",
            input: targetPeerId,
            description: "Raw peer ID string"
        }
    ];
}

export async function runSingleTest(h: any, testCase: TestCase, testIndex: number): Promise<TestResult> {
    console.log(`\n=== Test ${testIndex + 1}/4: ${testCase.name} ===`);
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
            const verificationSuccess = !!connection;
            console.log(`   └─ Verification: ${verificationSuccess ? '✅' : '❌'} (${verifyTime}ms)`);

            return {
                success: true,
                testTime,
                result,
                verificationSuccess,
                verificationTime: verifyTime
            };
        } else {
            console.log(`❌ FAILED after ${result.totalTime.toFixed(2)}s`);
            console.log(`   └─ Status: ${result.status}`);
            return {
                success: false,
                testTime,
                result,
                error: result.status
            };
        }
    } catch (error) {
        console.error(`❌ ERROR: ${error}`);
        return {
            success: false,
            testTime: 0,
            error: String(error)
        };
    }
}

export async function main(): Promise<void> {
    console.log('🚀 Starting discoverOptimalConnectPath tests...');

    const h = await createLibp2pHost();
    const testCases = await getTestCases();

    console.log(`🎯 Running ${testCases.length} test cases\n`);

    for (let i = 0; i < testCases.length; i++) {
        const testResult = await runSingleTest(h, testCases[i], i);

        // Short delay between tests
        if (i < testCases.length - 1) {
            console.log('\n⏳ Waiting 2s before next test...');
            await new Promise(resolve => setTimeout(resolve, 2000));
        }
    }

    console.log('\n🏁 All tests completed!');
    await h.stop();
}

// if browser environment, run main immediately
if (typeof window === 'undefined') {
    // Node.js specific execution
    await main().catch(console.error);
    process.exit(0);
}