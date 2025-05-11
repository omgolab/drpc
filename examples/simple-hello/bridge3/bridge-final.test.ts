import {
    afterAll,
    afterEach,
    beforeAll,
    beforeEach,
    describe,
    expect,
    it
} from 'vitest'
import { Libp2p } from 'libp2p'
import { spawn, type ChildProcess } from 'node:child_process'
import { createInterface } from 'node:readline'
import { multiaddr } from '@multiformats/multiaddr'
import { type PeerId } from '@libp2p/interface-peer-id'
import { peerIdFromString } from '@libp2p/peer-id'
import * as path from 'path'
import { ConnectClient, unaryContentTypes, streamingContentTypes } from './connect-client';
import { getCodecFns, createNode } from './test-utils'

// Type definitions for test setup
interface TestSetup {
    node: Libp2p<any>;
    goProc: ChildProcess;
    goServerPeerIdString: string;
    goServerMultiaddrString: string;
    goServerPeerId: PeerId;
}

// Cleanup functions
let cleanupFunctions: Array<() => Promise<void>> = [];

// Start the Go server
async function startGoServer(): Promise<{ goProc: ChildProcess; multiAddr: string }> {
    const goServerPath = path.resolve(__dirname, "bridge-server.go");
    console.log("Starting Go server from:", goServerPath);

    const goProc = spawn("go", ["run", goServerPath], {
        cwd: __dirname,
        stdio: ["pipe", "pipe", "pipe"],
        env: { ...process.env },
    });

    let rl: ReturnType<typeof createInterface> | null = null;
    let detectedMultiAddr: string | null = null;
    let isServerReady = false;
    let isPromiseSettled = false;
    let readinessTimeout: NodeJS.Timeout;

    const cleanupListeners = () => {
        if (rl) {
            rl.removeAllListeners('line');
            rl.close();
            rl = null;
        }
        goProc.removeAllListeners('exit');
        goProc.removeAllListeners('error');
    };

    // Centralized promise settlement function
    const settlePromise = (resolver: (value: any) => void, rejecter: (reason?: any) => void, error?: Error, successValue?: any) => {
        if (isPromiseSettled) return;
        isPromiseSettled = true;
        clearTimeout(readinessTimeout);
        cleanupListeners();

        if (error) {
            console.error(`[test script] Settling promise with error: ${error.message}`);
            rejecter(error);
        } else if (successValue) {
            console.log(`[test script] Go server ready. Multiaddr: ${successValue.multiAddr}`);
            resolver(successValue);
        } else {
            console.error("[test script] SettlePromise called without error or success value.");
            rejecter(new Error("Internal error in SettlePromise"));
        }
    };

    // Setup stdout/stderr listeners for debugging
    goProc.stdout.on("data", (data) => {
        if (!rl) console.log("[go stdout] " + data.toString().trim());
    });

    goProc.stderr.on("data", (data) => {
        console.error("[go stderr] " + data.toString().trim());
    });

    return new Promise<{ goProc: ChildProcess; multiAddr: string }>((resolve, reject) => {
        readinessTimeout = setTimeout(() => {
            settlePromise(resolve, reject, new Error("Timeout: Go server did not provide multiaddr and become ready within 25 seconds."));
        }, 25000);

        rl = createInterface({ input: goProc.stdout });

        rl.on('line', (line) => {
            if (isPromiseSettled) return;
            const trimmedLine = line.trim();
            console.log(`[test script] Server output: "${trimmedLine}"`);

            const multiAddrPrefix = "BRIDGE_SERVER_MULTIADDR_P2P:";
            const expectedReadinessLine = "Go server is running and listening for libp2p connections...";

            if (!detectedMultiAddr && trimmedLine.startsWith(multiAddrPrefix)) {
                let addrPart = trimmedLine.substring(multiAddrPrefix.length);
                const readinessIndexInAddrPart = addrPart.indexOf(expectedReadinessLine);
                if (readinessIndexInAddrPart !== -1) {
                    addrPart = addrPart.substring(0, readinessIndexInAddrPart);
                }
                detectedMultiAddr = addrPart.replace(/\\n.*$/g, '').trim();
                console.log(`[test script] Detected multiaddr: ${detectedMultiAddr}`);
            }

            if (!isServerReady && trimmedLine.includes(expectedReadinessLine)) {
                isServerReady = true;
                console.log("[test script] Go server reported READY.");
            }

            if (detectedMultiAddr && isServerReady) {
                settlePromise(resolve, reject, undefined, { goProc, multiAddr: detectedMultiAddr });
            }

            // Error checks
            if (trimmedLine.toLowerCase().includes("panic") ||
                trimmedLine.toLowerCase().includes("error building source") ||
                trimmedLine.toLowerCase().includes("failed to run")) {
                settlePromise(resolve, reject, new Error(`Go server error: ${trimmedLine}`));
            }
        });

        goProc.once('exit', (code, signal) => {
            if (!isPromiseSettled) {
                settlePromise(resolve, reject, new Error(`Go server exited prematurely with code ${code} and signal ${signal} before becoming ready.`));
            }
        });

        goProc.on('error', (err) => {
            settlePromise(resolve, reject, new Error(`Failed to start Go server process: ${err.message}`));
        });

        // Add cleanup function for the Go process
        cleanupFunctions.push(async () => {
            console.log("Cleaning up Go process...");
            if (goProc && !goProc.killed) {
                if (!isPromiseSettled) {
                    settlePromise(() => { }, () => { }, new Error("Go process cleanup initiated before server readiness settled."));
                }
                goProc.kill('SIGTERM');
                await new Promise<void>(termResolve => {
                    const killTimeout = setTimeout(() => {
                        console.warn("Go process didn't exit cleanly with SIGTERM after 2s, forcing with SIGKILL");
                        if (goProc && !goProc.killed) goProc.kill('SIGKILL');
                        termResolve();
                    }, 2000);
                    goProc.once('exit', () => {
                        clearTimeout(killTimeout);
                        termResolve();
                    });
                });
            }
        });
    });
}

// Define service path
const greeterServicePath = "/greeter.v1.GreeterService";

// Test suite for Connect RPC over Libp2p
describe('Connect RPC over Libp2p (Using Client Library)', () => {
    let testSetupInstance: TestSetup | null = null;
    let connectClient: ConnectClient;

    beforeAll(async () => {
        console.log("[test script] Setting up test environment...");
        cleanupFunctions = [];
        try {
            const goServerInfo = await startGoServer();
            const node = await createNode();
            console.log('[test script] Libp2p node started with PeerID:', node.peerId.toString());

            const { multiAddr: goServerMultiaddrString, goProc } = goServerInfo;
            const goServerPeerIdString = multiaddr(goServerMultiaddrString).getPeerId();
            if (!goServerPeerIdString) {
                throw new Error(`[test script] Could not extract PeerId from Go server multiaddr: ${goServerMultiaddrString}`);
            }
            const goServerPeerId = peerIdFromString(goServerPeerIdString) as any as PeerId;

            testSetupInstance = {
                node,
                goProc,
                goServerPeerIdString,
                goServerMultiaddrString,
                goServerPeerId,
            };

            if (testSetupInstance) {
                console.log(`[test script] Go server PeerID: ${testSetupInstance.goServerPeerId.toString()}`);
                const serverMaInstance = multiaddr(testSetupInstance.goServerMultiaddrString);
                if (testSetupInstance.goServerPeerId && serverMaInstance) {
                    await testSetupInstance.node.peerStore.patch(testSetupInstance.goServerPeerId as any, { multiaddrs: [serverMaInstance] });
                    console.log(`[test script] Added Go server multiaddr to client peer store`);

                    // Create our Connect client
                    connectClient = new ConnectClient(node, greeterServicePath, true);
                    console.log('[test script] Created Connect client');
                } else {
                    throw new Error("[test script] Failed to obtain Go server PeerId or Multiaddr for peer store.");
                }
            } else {
                throw new Error("[test script] testSetupInstance is null after assignment.");
            }

            console.log('[test script] Setup complete.');

            // Add cleanup functions
            cleanupFunctions.push(async () => {
                if (testSetupInstance && testSetupInstance.node) {
                    await testSetupInstance.node.stop();
                }
            });
            cleanupFunctions.push(async () => {
                if (goServerInfo && goServerInfo.goProc) {
                    goServerInfo.goProc.kill();
                    await new Promise(resolve => setTimeout(resolve, 100));
                }
            });

        } catch (error) {
            console.error('[test script] Error during setup:', error);
            if (testSetupInstance && testSetupInstance.goProc && !testSetupInstance.goProc.killed) {
                testSetupInstance.goProc.kill();
            }
            throw error;
        }
    }, 60000);

    afterAll(async () => {
        console.log('[test script] Starting cleanup...');

        // First, explicitly kill any Go processes that might be running
        if (testSetupInstance && testSetupInstance.goProc && !testSetupInstance.goProc.killed) {
            console.log('[test script] Explicitly killing Go process...');
            try {
                // Try with SIGINT first
                testSetupInstance.goProc.kill('SIGINT');

                // Wait for process to exit with a timeout
                await new Promise<void>(resolve => {
                    const killTimeout = setTimeout(() => {
                        console.log('[test script] Process did not exit with SIGINT, trying SIGKILL...');
                        try {
                            if (testSetupInstance && testSetupInstance.goProc && !testSetupInstance.goProc.killed) {
                                testSetupInstance.goProc.kill('SIGKILL');
                            }

                            // Also look for and kill any other related Go processes
                            const pidCmd = spawn('pgrep', ['-f', 'bridge-server'], { stdio: 'pipe' });
                            pidCmd.stdout.on('data', data => {
                                const pids = data.toString().trim().split('\n');
                                pids.forEach(pid => {
                                    if (pid) {
                                        console.log(`[test script] Also killing related process ${pid}`);
                                        try {
                                            process.kill(parseInt(pid), 'SIGKILL');
                                        } catch (e) {
                                            // Ignore errors if process doesn't exist
                                        }
                                    }
                                });
                            });
                        } catch (e) {
                            console.error('[test script] Error during forced cleanup:', e);
                        }
                        resolve();
                    }, 1000);

                    testSetupInstance?.goProc.once('exit', () => {
                        clearTimeout(killTimeout);
                        resolve();
                    });
                });
            } catch (e) {
                console.error('[test script] Error killing Go process:', e);
            }
        }

        // Then run other cleanup functions
        for (const cleanup of cleanupFunctions.reverse()) {
            try {
                await cleanup();
            } catch (err) {
                console.error('[test script] Error during cleanup:', err);
            }
        }

        // Make sure everything is reset
        cleanupFunctions = [];
        testSetupInstance = null;

        // Wait a bit to ensure processes have time to fully terminate
        await new Promise(resolve => setTimeout(resolve, 500));

        console.log('[test script] Cleanup complete.');
    }, 60000);

    beforeEach(async () => {
        if (!testSetupInstance || !testSetupInstance.node || testSetupInstance.node.status !== 'started') {
            throw new Error("Test setup not initialized or node not started");
        }
        const { node, goServerPeerId, goServerMultiaddrString } = testSetupInstance;
        if (node.getPeers().length === 0 || node.getConnections(goServerPeerId as any).length === 0) {
            console.log('[test script] Attempting to connect to Go server...');
            try {
                const serverMa = multiaddr(goServerMultiaddrString);
                await node.dial(serverMa);
                console.log('[test script] Successfully dialed Go server.');
            } catch (error) {
                console.error('[test script] Failed to dial Go server:', error);
            }
        }
    });

    afterEach(async () => {
        console.log("[test script] Test completed.");
    }, 15000);

    it.each(unaryContentTypes)('unary: SayHello request-response (%s)', async (contentType) => {
        if (!testSetupInstance) throw new Error("Test setup not initialized.");
        const { goServerPeerId } = testSetupInstance;
        const { encodeSayHelloRequest, decodeSayHelloResponse } = getCodecFns(contentType);

        try {
            console.log(`[test script] Starting unary test for contentType: ${contentType}`);

            const requestMessage = { name: "Libp2pUnary" };

            const response = await connectClient.unaryCall(
                goServerPeerId,
                "SayHello",
                requestMessage,
                encodeSayHelloRequest,
                decodeSayHelloResponse,
                contentType
            );

            console.log("[test script] Received response:", response);

            // Assertion: For Connect unary (application/json), expect .message; for gRPC/gRPC-Web, expect object
            if (contentType === "application/json" || contentType === "application/proto") {
                expect(response.message).toBeDefined();
                expect(response.message).toContain("Hello, Libp2pUnary");
            } else {
                expect(typeof response).toBe('object');
            }

            console.log("[test script] Unary test completed successfully");
        } catch (error) {
            console.error("[test script] Unary test error:", error);
            throw error;
        }
    }, 15000);

    // Test bidirectional streaming
    it.each(streamingContentTypes)('bidirectional: BidiStreamingEcho stream (%s)', async (contentType) => {
        if (!testSetupInstance) throw new Error("Test setup not initialized.");
        const { goServerPeerId } = testSetupInstance;
        const { encodeBidiStreamingEchoRequest, decodeBidiStreamingEchoResponse } = getCodecFns(contentType);

        try {
            console.log(`[test script] Starting bidirectional streaming test for contentType: ${contentType}`);

            const requestMessages = [
                { name: "BidiClientMsg1" },
                { name: "BidiClientMsg2" },
                { name: "BidiClientMsg3" },
            ];

            const responses = await connectClient.bidiStreamingCall(
                goServerPeerId,
                "BidiStreamingEcho",
                requestMessages,
                encodeBidiStreamingEchoRequest,
                decodeBidiStreamingEchoResponse,
                contentType
            );

            console.log(`[test script] Received ${responses.length} responses`);

            // For Connect streaming and gRPC/gRPC-Web, expect responses.length === requestMessages.length
            expect(responses.length).toBe(requestMessages.length);
            for (let i = 0; i < responses.length; i++) {
                expect(responses[i]).toBeDefined();
                expect(responses[i].greeting).toContain(`Hello, ${requestMessages[i].name}`);
                console.log(`[test script] Verified response ${i + 1}: "${responses[i].greeting}"`);
            }

            console.log("[test script] Bidirectional streaming test completed successfully");
        } catch (error) {
            console.error("[test script] Bidirectional streaming test error:", error);
            throw error;
        }
    }, 30000);
});
