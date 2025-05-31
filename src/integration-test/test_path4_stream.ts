/**
 * Standalone test script for Path4 LibP2P Auto Relay client/bidi streaming
 * Extracted from the main integration test suite
 */
import { GreeterService } from "../../demo/gen/ts/greeter/v1/greeter_pb";
import {
    UtilServerHelper,
    ClientManager,
    createManagedClient,
    testClientAndBidiStreamingRequest,
} from "./helpers";
import { createLogger, LogLevel } from "../client/core/logger";
import { DRPCClient } from "../client";
import { BidiStreamingEchoRequest, BidiStreamingEchoRequestSchema, BidiStreamingEchoResponse } from "../../demo/gen/ts/greeter/v1/greeter_pb";
import { create } from "@bufbuild/protobuf";

// Create a logger for the test
const testLogger = createLogger({
    contextName: "Path4-Stream-Test",
    logLevel: LogLevel.DEBUG,
});

/**
 * Enhanced bidi streaming test with timeout and better abortion handling
 */
async function testClientAndBidiStreamingRequestWithTimeout(
    client: DRPCClient<typeof GreeterService>,
    numClientMessages: number,
    baseName: string = "ClientMessage",
    timeoutMs: number = 30000,
): Promise<void> {
    console.log(
        `Testing bidiStreamingEcho with ${numClientMessages} client messages (timeout: ${timeoutMs}ms).`,
    );

    // Create an AbortController for this operation
    const abortController = new AbortController();

    // Set up timeout that will abort the operation
    const timeoutHandle = setTimeout(() => {
        console.log(`⚠️  Bidi streaming test timed out after ${timeoutMs}ms - aborting`);
        abortController.abort();
    }, timeoutMs);

    try {
        // This creates an AsyncIterable that yields BidiStreamingEchoRequest objects
        async function* generateClientRequests(): AsyncIterable<BidiStreamingEchoRequest> {
            for (let i = 0; i < numClientMessages; i++) {
                // Check for abortion
                if (abortController.signal.aborted) {
                    console.log("[ClientStreamGen] Stream generation aborted");
                    return;
                }

                const request = create(BidiStreamingEchoRequestSchema, {
                    name: `${baseName}-${i}`,
                });
                console.log(`[ClientStreamGen] Yielding: ${JSON.stringify(request)}`);
                yield request;
                // Optional delay between messages
                await new Promise((r) => setTimeout(r, 50));
            }
            console.log("[ClientStreamGen] Finished yielding client messages.");
        }

        const clientRequestStream = generateClientRequests();

        // The responseStream is an AsyncIterable that yields BidiStreamingEchoResponse objects
        const responseStream = await client.bidiStreamingEcho(clientRequestStream);

        const receivedServerResponses: BidiStreamingEchoResponse[] = [];
        console.log("[ClientBidiTest] Starting to consume server responses...");

        // Use a race condition between the stream processing and timeout
        const streamPromise = (async () => {
            for await (const serverMsg of responseStream) {
                // Check for abortion
                if (abortController.signal.aborted) {
                    console.log("[ClientBidiTest] Stream consumption aborted");
                    break;
                }

                console.log(
                    `[ClientBidiTest] Received server message: ${JSON.stringify(serverMsg)}`,
                );
                receivedServerResponses.push(serverMsg);

                // Check if we've received all expected messages
                if (receivedServerResponses.length >= numClientMessages) {
                    console.log("[ClientBidiTest] Received all expected messages, breaking");
                    break;
                }
            }
        })();

        // Wait for either the stream to complete or abortion signal
        await Promise.race([
            streamPromise,
            new Promise((_, reject) => {
                abortController.signal.addEventListener('abort', () => {
                    reject(new Error('Bidi streaming test was aborted due to timeout'));
                });
            })
        ]);

        console.log("[ClientBidiTest] Finished consuming server responses.");

        // Clear the timeout since we completed successfully
        clearTimeout(timeoutHandle);

        // Verify we received the expected number of responses
        if (receivedServerResponses.length !== numClientMessages) {
            throw new Error(
                `Expected ${numClientMessages} responses but got ${receivedServerResponses.length}`,
            );
        }

        // Verify each response matches what we expect
        for (let i = 0; i < numClientMessages; i++) {
            const expected = `Hello, ${baseName}-${i}!`;
            if (receivedServerResponses[i].greeting !== expected) {
                throw new Error(
                    `Expected "${expected}" but got "${receivedServerResponses[i].greeting}"`,
                );
            }
        }

        console.log("[ClientBidiTest] Assertions passed.");
    } catch (err: any) {
        // Clear the timeout in case of error
        clearTimeout(timeoutHandle);
        console.error(`[ClientBidiTest] Error in bidiStreamingEcho test: ${err}`);
        throw err;
    }
}

async function runPath4StreamTest() {
    console.log("Starting Path4 LibP2P Auto Relay client/bidi streaming test...");

    // Initialize utility server and client manager
    const utilServer = new UtilServerHelper("cmd/util-server/main.go");
    const clientManager = new ClientManager();

    // Add timeout to prevent hanging
    const testTimeout = 60000; // 60 seconds
    const timeoutPromise = new Promise((_, reject) => {
        setTimeout(() => reject(new Error("Test timed out after 60 seconds")), testTimeout);
    });

    try {
        // Run the actual test with timeout
        await Promise.race([timeoutPromise, runTestWithoutTimeout(utilServer, clientManager)]);
        console.log("✅ Path4 auto relay client/bidi streaming test completed successfully!");

    } catch (error) {
        console.error("❌ Test failed:", error);
        throw error;
    } finally {
        // Cleanup
        console.log(`Cleaning up ${clientManager.clientCount} clients...`);
        await clientManager.cleanup();

        console.log("Stopping Go utility server...");
        if (utilServer) {
            utilServer.stopServer();
        }
        console.log("Cleanup completed.");
    }
}

async function runTestWithoutTimeout(utilServer: UtilServerHelper, clientManager: ClientManager) {
    // Start the Go utility server
    console.log("Starting Go utility server...");
    await utilServer.startServer();
    console.log("Go utility server is ready.");

    // Wait for the server to fully initialize
    console.log("Waiting for server to fully initialize...");
    await new Promise(resolve => setTimeout(resolve, 2000));

    // Get relay node info and extract auto relay address
    console.log("Fetching relay node info for Path 4 (LibP2P Relay)...");
    const relayNodeInfo = await utilServer.getRelayNodeInfo();
    const libp2pAddr = relayNodeInfo.libp2p_ma || "";

    let autoRelayAddr: string | undefined;

    if (libp2pAddr && libp2pAddr.includes("/p2p-circuit/")) {
        console.log(`Using relay node libp2p address: ${libp2pAddr}`);

        // Extract target node address for auto relay test
        const addrParts = libp2pAddr.split("/p2p-circuit/");
        if (addrParts.length === 2) {
            autoRelayAddr = addrParts[1].startsWith("/")
                ? addrParts[1]
                : "/" + addrParts[1];
            console.log(`Extracted auto relay target address: ${autoRelayAddr}`);
        } else {
            console.warn("Could not extract auto relay target address from relay address");
            autoRelayAddr = undefined;
        }
    } else {
        console.warn("Relay node info doesn't contain a valid relay libp2p multiaddress");
        autoRelayAddr = undefined;
    }

    // Run the test
    if (!autoRelayAddr) {
        console.warn("Skipping Path4 auto relay client/bidi streaming test: no target address available.");
        return;
    }

    console.log("Creating client and running bidi streaming test...");

    // Wait for peer discovery to settle (important for libp2p)
    console.log("Waiting for peer discovery to stabilize...");
    await new Promise(resolve => setTimeout(resolve, 3000));

    // Create client with standard configuration
    const client = await createManagedClient(
        clientManager,
        autoRelayAddr,
        GreeterService,
        { logger: testLogger },
    );

    // Use the original helper function to test baseline behavior
    console.log("Starting original bidi streaming test...");
    await testClientAndBidiStreamingRequest(
        client,
        2,
        "LibP2PAutoRelayBidi",
    );
    console.log("✅ Bidi streaming test completed successfully!");
}

// Run the test if this script is executed directly (ES module compatible)
if (import.meta.url === `file://${process.argv[1]}`) {
    runPath4StreamTest()
        .then(() => {
            console.log("Script completed successfully.");
            process.exit(0);
        })
        .catch((error) => {
            console.error("Script failed:", error);
            process.exit(1);
        });
}

export { runPath4StreamTest };