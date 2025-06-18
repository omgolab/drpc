import { createLibp2pHost } from "../../src/client/core/libp2p-host";
import { discoverOptimalConnectPath } from "../../src/client/core/discover";

// Browser-safe util server functions
const isBrowserEnvironment = typeof window !== 'undefined';

async function getTargetFromRelay(): Promise<string> {
    if (isBrowserEnvironment) {
        // In browser environment, we can't start the server automatically
        // User must manually start the util server before running the test
        console.log('Browser environment detected - checking if util server is accessible...');

        try {
            const response = await fetch('http://localhost:8080/relay-node');
            if (!response.ok) {
                throw new Error(`Server returned ${response.status}: ${response.statusText}`);
            }
            const relayInfo = await response.json();
            console.log('✅ Util server is accessible, retrieved relay info');
            return relayInfo.libp2p_ma;
        } catch (error) {
            throw new Error(
                `❌ Unable to connect to util server at http://localhost:8080\n` +
                `Please manually start the util server by running:\n` +
                `  bun run build\n` +
                `  ./tmp/util-server\n` +
                `Error: ${error}`
            );
        }
    } else {
        // Node.js environment - use the util server helper
        const { getUtilServer, isUtilServerAccessible } = await import("../../src/tests/util/util-server");

        const utilServer = getUtilServer();
        if (!(await isUtilServerAccessible())) {
            console.log('Starting util server...');
            await utilServer.startServer();
        }

        const relayInfo = await utilServer.getRelayNodeInfo();
        return relayInfo.libp2p_ma;
    }
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

// Generic test runner for individual address types
async function runTestByIndex(testIndex: number): Promise<TestResult> {
    const h = await createLibp2pHost();
    try {
        const testCases = await getTestCases();
        const testCase = testCases[testIndex];
        if (!testCase) {
            throw new Error(`Test case ${testIndex} not found`);
        }
        return await runSingleTest(h, testCase, testIndex);
    } finally {
        await h.stop();
    }
}

// Individual test functions for each address type
export async function testCircuitRelayPath(): Promise<TestResult> {
    return runTestByIndex(0); // Type 1: Circuit relay path
}

export async function testP2PMultiaddr(): Promise<TestResult> {
    return runTestByIndex(1); // Type 2: P2P multiaddr
}

export async function testDirectMultiaddr(): Promise<TestResult> {
    return runTestByIndex(2); // Type 3: Direct multiaddr
}

export async function testRawPeerId(): Promise<TestResult> {
    return runTestByIndex(3); // Type 4: Raw peer ID
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
            console.log(`   └─ Method: ${result.method} (${result.trackDescription})`);
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

export async function runAllTests(): Promise<void> {
    console.log('🚀 Starting discoverOptimalConnectPath tests...');

    const h = await createLibp2pHost();
    try {
        const testCases = await getTestCases();
        console.log(`🎯 Running ${testCases.length} test cases\n`);

        for (let i = 0; i < testCases.length; i++) {
            await runSingleTest(h, testCases[i], i);

            // Short delay between tests (except for the last one)
            if (i < testCases.length - 1) {
                console.log('\n⏳ Waiting 2s before next test...');
                await new Promise(resolve => setTimeout(resolve, 2000));
            }
        }

        console.log('\n🏁 All tests completed!');
    } finally {
        await h.stop();
    }
}