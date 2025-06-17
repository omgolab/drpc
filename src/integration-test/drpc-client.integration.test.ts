/**
 * Integration tests for the dRPC client
 * 
 * Usage:
 *   tsx src/integration-test/orchestrator.ts  (recommended - runs all environments)
 *   tsx src/integration-test/orchestrator.ts --env=node
 *   tsx src/integration-test/orchestrator.ts --env=chrome
 *   tsx src/integration-test/orchestrator.ts --env=firefox
 * 
 * Test Paths:
 * 1. HTTP Direct (Path1_HTTPDirect)
 * 2. HTTP Gateway/Relay (Path2_HTTPGatewayRelay) 
 * 3. LibP2P Direct (Path3_LibP2PDirect)
 * 4. LibP2P Relay (Path4_LibP2PRelay)
 */

import { beforeAll, afterAll, describe, it } from "vitest";
import { GreeterService } from "../../demo/gen/ts/greeter/v1/greeter_pb";
import { createLogger, LogLevel } from "../client/core/logger";
import {
    ClientManager,
    createManagedClient,
    testClientUnaryRequest,
    testServerStreamingRequest,
    testClientAndBidiStreamingRequest,
    fetchNodeInfo,
    setupBrowserNetworkMonitoring,
    environment,
    browserName,
    NodeInfo
} from "./helpers";

// Create a logger for the test
const testLogger = createLogger({
    contextName: `Integration-Test-${browserName}`,
    logLevel: LogLevel.ERROR,
});

// Since orchestrator handles util server startup, we just need the URL
const UTIL_SERVER_BASE_URL = 'http://127.0.0.1:8080';

// Create a client manager for resource tracking
let clientManager: ClientManager;

// Initialize client manager before tests
beforeAll(async () => {
    console.log(`Initializing client manager for dRPC integration tests (${environment})...`);
    clientManager = new ClientManager();

    // Set up browser network monitoring if in browser
    setupBrowserNetworkMonitoring();

    // Quick connectivity check to ensure utility server is available
    try {
        const nodeInfo = await fetchNodeInfo("/public-node", UTIL_SERVER_BASE_URL);
        console.log(`✅ Utility server connectivity confirmed: ${nodeInfo.http_address}`);
    } catch (error) {
        const errorMsg = `❌ Utility server not accessible: ${error}`;
        console.error(errorMsg);
        throw new Error(`Tests require util server to be running and accessible at ${UTIL_SERVER_BASE_URL}. ${errorMsg}`);
    }
});

// Constants for timeout
const TEST_TIMEOUT = 60000; // 60 seconds

describe("DrpcClient Integration", () => {
    // Path 1: HTTP Direct
    describe("Path1_HTTPDirect", () => {
        it(
            "1.1-unary",
            async () => {
                const publicNodeInfo = await fetchNodeInfo("/public-node", UTIL_SERVER_BASE_URL);
                const addr = publicNodeInfo.http_address;

                if (!addr) {
                    throw new Error("Public node info doesn't contain a valid HTTP address");
                }
                console.log(`Using public node HTTP address for Path 1: ${addr}`);

                const client = await createManagedClient(
                    clientManager,
                    addr,
                    GreeterService,
                    { logger: testLogger },
                );

                await testClientUnaryRequest(client, "dRPC Test");
            },
            TEST_TIMEOUT,
        );

        it(
            "1.2-streaming",
            async () => {
                const publicNodeInfo = await fetchNodeInfo("/public-node", UTIL_SERVER_BASE_URL);
                const addr = publicNodeInfo.http_address || "";
                if (!addr) {
                    throw new Error("Public node info doesn't contain a valid HTTP address");
                }
                console.log(`Using public node HTTP address for Path 1 streaming: ${addr}`);

                const client = await createManagedClient(
                    clientManager,
                    addr,
                    GreeterService,
                    { logger: testLogger },
                );
                await testServerStreamingRequest(client, "AliceHTTPDirect");
            },
            TEST_TIMEOUT,
        );

        it(
            "1.3-client/bidi streaming",
            async () => {
                const publicNodeInfo = await fetchNodeInfo("/public-node", UTIL_SERVER_BASE_URL);
                const addr = publicNodeInfo.http_address || "";
                if (!addr) {
                    throw new Error("Public node info doesn't contain a valid HTTP address");
                }
                console.log(`Using public node HTTP address for Path 1 client/bidi streaming: ${addr}`);

                const client = await createManagedClient(
                    clientManager,
                    addr,
                    GreeterService,
                    { logger: testLogger },
                );

                await testClientAndBidiStreamingRequest(client, 3, "HTTPBidi");
            },
            TEST_TIMEOUT,
        );
    });

    // Path 2: Gateway/Relay test address
    describe("Path2_HTTPGatewayRelay", () => {
        describe("http_gw_force_relay", () => {
            let gatewayRelayBaseUrl: string;

            beforeAll(async () => {
                try {
                    console.log("Fetching gateway relay node info for Path 2 force relay...");
                    const gatewayInfo = await fetchNodeInfo("/gateway-relay-node", UTIL_SERVER_BASE_URL);
                    const httpAddr = gatewayInfo.http_address || "";
                    if (!httpAddr) {
                        throw new Error("Gateway relay node does not have an HTTP address");
                    }
                    gatewayRelayBaseUrl = httpAddr;
                    console.log(`Using gateway relay HTTP address for Path 2: ${gatewayRelayBaseUrl}`);

                    // Quick connectivity check
                    const client = await createManagedClient(
                        clientManager,
                        gatewayRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testClientUnaryRequest(client, "dRPC Test Gateway Relay Connectivity Check");
                } catch (error) {
                    console.error("Failed to fetch gateway relay node info:", error);
                    throw new Error(`Failed to initialize gateway relay for tests: ${error}`);
                }
            });

            it(
                "2.1-unary (force relay address)",
                async () => {
                    const client = await createManagedClient(
                        clientManager,
                        gatewayRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testClientUnaryRequest(client, "dRPC Test");
                },
                TEST_TIMEOUT,
            );

            it(
                "2.2-streaming (force relay address)",
                async () => {
                    const client = await createManagedClient(
                        clientManager,
                        gatewayRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testServerStreamingRequest(client, "AliceGatewayRelay");
                },
                TEST_TIMEOUT,
            );

            it(
                "2.3-client/bidi streaming (force relay address)",
                async () => {
                    const client = await createManagedClient(
                        clientManager,
                        gatewayRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testClientAndBidiStreamingRequest(client, 2, "GatewayBidi");
                },
                TEST_TIMEOUT,
            );
        });

        describe("http_gw_auto_relay", () => {
            let gatewayAutoRelayBaseUrl: string;

            beforeAll(async () => {
                try {
                    console.log("Fetching gateway auto relay node info for Path 2 auto relay...");
                    const gatewayInfo = await fetchNodeInfo("/gateway-auto-relay-node", UTIL_SERVER_BASE_URL);
                    const httpAddr = gatewayInfo.http_address || "";
                    if (!httpAddr) {
                        throw new Error("Gateway auto relay node does not have an HTTP address");
                    }
                    gatewayAutoRelayBaseUrl = httpAddr;
                    console.log(`Using gateway auto relay HTTP address for Path 2: ${gatewayAutoRelayBaseUrl}`);

                    // Quick connectivity check
                    const client = await createManagedClient(
                        clientManager,
                        gatewayAutoRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testClientUnaryRequest(client, "dRPC Test Gateway Auto Relay Connectivity Check");
                } catch (error) {
                    console.error("Failed to fetch gateway auto relay node info:", error);
                    throw new Error(`Failed to initialize gateway auto relay for tests: ${error}`);
                }
            });

            it(
                "2.4-unary (no/auto relay address)",
                async () => {
                    const client = await createManagedClient(
                        clientManager,
                        gatewayAutoRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testClientUnaryRequest(client, "dRPC Test");
                },
                TEST_TIMEOUT,
            );

            it(
                "2.5-streaming (no/auto relay address)",
                async () => {
                    const client = await createManagedClient(
                        clientManager,
                        gatewayAutoRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testServerStreamingRequest(client, "AliceGatewayAuto");
                },
                TEST_TIMEOUT,
            );

            it(
                "2.6-client/bidi streaming (no/auto relay address)",
                async () => {
                    const client = await createManagedClient(
                        clientManager,
                        gatewayAutoRelayBaseUrl,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testClientAndBidiStreamingRequest(client, 4, "GatewayAutoBidi");
                },
                TEST_TIMEOUT,
            );
        });
    });

    // Path 3: LibP2P Direct
    describe("Path3_LibP2PDirect", () => {
        let directAddr: string | undefined;

        beforeAll(async () => {
            try {
                console.log("Fetching public node info for Path 3 (LibP2P Direct)...");
                const publicNodeInfo = await fetchNodeInfo("/public-node", UTIL_SERVER_BASE_URL);
                const libp2pAddr = publicNodeInfo.libp2p_ma || "";
                if (libp2pAddr) {
                    directAddr = libp2pAddr;
                    console.log(`Using public node libp2p address for Path 3: ${directAddr}`);
                } else {
                    throw new Error("Public node info doesn't contain a valid libp2p multiaddress");
                }
            } catch (error) {
                console.error("Failed to fetch public node libp2p address from utility server:", error);
                console.warn("No direct multiaddr found for Path3_LibP2PDirect; skipping test.");
                directAddr = undefined;
            }

            if (!directAddr) {
                console.warn("No direct (non-relay) multiaddr found for Path3_LibP2PDirect; skipping test.");
            } else {
                console.log("Using direct multiaddr for Path3:", directAddr);
            }
        });

        it(
            "3.1-unary",
            async () => {
                if (!directAddr) {
                    console.warn("Skipping Path3_LibP2PDirect unary test: no direct address available.");
                    return;
                }

                try {
                    const client = await createManagedClient(
                        clientManager,
                        directAddr,
                        GreeterService,
                        { logger: testLogger },
                    );
                    await testClientUnaryRequest(client, "BobLibP2PDirect");
                } catch (err: any) {
                    throw err;
                }
            },
            TEST_TIMEOUT,
        );

        it(
            "3.2-streaming",
            async () => {
                if (!directAddr) {
                    console.warn("Skipping Path3_LibP2PDirect streaming test: no direct address available.");
                    return;
                }

                const client = await createManagedClient(
                    clientManager,
                    directAddr,
                    GreeterService,
                    { logger: testLogger },
                );
                await testServerStreamingRequest(client, "AliceLibP2PDirect");
            },
            TEST_TIMEOUT,
        );

        it(
            "3.3-client/bidi streaming",
            async () => {
                if (!directAddr) {
                    console.warn("Skipping Path3_LibP2PDirect client/bidi streaming test: no direct address available.");
                    return;
                }

                const client = await createManagedClient(
                    clientManager,
                    directAddr,
                    GreeterService,
                    { logger: testLogger },
                );
                await testClientAndBidiStreamingRequest(client, 3, "LibP2PDirectBidi");
            },
            TEST_TIMEOUT,
        );
    });

    // Path 4: LibP2P Relay
    describe("Path4_LibP2PRelay", () => {
        let fixedRelayAddr: string | undefined;
        let autoRelayAddr: string | undefined;

        beforeAll(async () => {
            try {
                console.log("Fetching relay node info for Path 4 (LibP2P Relay)...");
                const relayNodeInfo = await fetchNodeInfo("/relay-node", UTIL_SERVER_BASE_URL);
                const libp2pAddr = relayNodeInfo.libp2p_ma || "";

                if (libp2pAddr && libp2pAddr.includes("/p2p-circuit/")) {
                    fixedRelayAddr = libp2pAddr;
                    console.log(`Using fixed relay node libp2p address for Path 4: ${fixedRelayAddr}`);

                    // Extract target node address for auto relay test
                    const addrParts = libp2pAddr.split("/p2p-circuit/");
                    if (addrParts.length === 2) {
                        autoRelayAddr = addrParts[1].startsWith("/") ? addrParts[1] : "/" + addrParts[1];
                        console.log(`Extracted auto relay target address for Path 4: ${autoRelayAddr}`);
                    } else {
                        console.warn("Could not extract auto relay target address from relay address");
                        autoRelayAddr = undefined;
                    }
                } else {
                    console.warn("Relay node info doesn't contain a valid relay libp2p multiaddress");
                    fixedRelayAddr = undefined;
                    autoRelayAddr = undefined;
                }
            } catch (error) {
                console.error("Failed to fetch relay node info from utility server:", error);
                console.warn("No suitable address found for Path4_LibP2PRelay; skipping test.");
                fixedRelayAddr = undefined;
                autoRelayAddr = undefined;
            }

            if (!fixedRelayAddr) {
                console.warn("No relay address found for Path4_LibP2PRelay; skipping test.");
            } else {
                console.log("Using fixed relay address for Path4:", fixedRelayAddr);
                if (autoRelayAddr) {
                    console.log("Using auto relay target address for Path4:", autoRelayAddr);
                }
            }
        });

        describe("libp2p_fixed_relay", () => {
            it(
                "4.1-unary",
                async () => {
                    if (!fixedRelayAddr) {
                        console.warn("Skipping Path4 fixed relay unary test: no relay address available.");
                        return;
                    }

                    try {
                        const client = await createManagedClient(
                            clientManager,
                            fixedRelayAddr,
                            GreeterService,
                            { logger: testLogger },
                        );
                        await testClientUnaryRequest(client, "BobLibP2PFixedRelay");
                    } catch (err: any) {
                        throw err;
                    }
                },
                TEST_TIMEOUT,
            );

            it(
                "4.2-streaming",
                async () => {
                    if (!fixedRelayAddr) {
                        console.warn("Skipping Path4 fixed relay streaming test: no relay address available.");
                        return;
                    }

                    try {
                        const client = await createManagedClient(
                            clientManager,
                            fixedRelayAddr,
                            GreeterService,
                            { logger: testLogger },
                        );
                        await testServerStreamingRequest(client, "AliceLibP2PFixedRelay");
                    } catch (err: any) {
                        throw err;
                    }
                },
                TEST_TIMEOUT,
            );

            it(
                "4.3-client/bidi streaming",
                async () => {
                    if (!fixedRelayAddr) {
                        console.warn("Skipping Path4 fixed relay client/bidi streaming test: no relay address available.");
                        return;
                    }

                    try {
                        const client = await createManagedClient(
                            clientManager,
                            fixedRelayAddr,
                            GreeterService,
                            { logger: testLogger },
                        );
                        await testClientAndBidiStreamingRequest(client, 2, "LibP2PFixedRelayBidi");
                    } catch (err: any) {
                        throw err;
                    }
                },
                TEST_TIMEOUT,
            );
        });

        describe("libp2p_auto_relay", () => {
            it(
                "4.4-unary",
                async () => {
                    if (!autoRelayAddr) {
                        console.warn("Skipping Path4 auto relay unary test: no target address available.");
                        return;
                    }

                    try {
                        const client = await createManagedClient(
                            clientManager,
                            autoRelayAddr,
                            GreeterService,
                            { logger: testLogger },
                        );
                        await testClientUnaryRequest(client, "BobLibP2PAutoRelay");
                    } catch (err: any) {
                        throw err;
                    }
                },
                TEST_TIMEOUT,
            );

            it(
                "4.5-streaming",
                async () => {
                    if (!autoRelayAddr) {
                        console.warn("Skipping Path4 auto relay streaming test: no target address available.");
                        return;
                    }

                    try {
                        const client = await createManagedClient(
                            clientManager,
                            autoRelayAddr,
                            GreeterService,
                            { logger: testLogger },
                        );
                        await testServerStreamingRequest(client, "AliceLibP2PAutoRelay");
                    } catch (err: any) {
                        throw err;
                    }
                },
                TEST_TIMEOUT,
            );

            it(
                "4.6-client/bidi streaming",
                async () => {
                    if (!autoRelayAddr) {
                        console.warn("Skipping Path4 auto relay client/bidi streaming test: no target address available.");
                        return;
                    }

                    try {
                        const client = await createManagedClient(
                            clientManager,
                            autoRelayAddr,
                            GreeterService,
                            { logger: testLogger },
                        );
                        await testClientAndBidiStreamingRequest(client, 2, "LibP2PAutoRelayBidi");
                    } catch (err: any) {
                        throw err;
                    }
                },
                TEST_TIMEOUT,
            );
        });
    });
});

// After all tests, clean up resources
afterAll(async () => {
    if (clientManager && clientManager.clientCount > 0) {
        console.log(`Cleaning up ${clientManager.clientCount} clients...`);
        await clientManager.cleanup();
        console.log("All clients cleaned up.");
    }
});
