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

// Create a logger for the test
const testLogger = createLogger({
    contextName: "Path4-Stream-Test",
    logLevel: LogLevel.DEBUG,
});

async function runPath4StreamTest() {
    console.log("Starting Path4 LibP2P Auto Relay client/bidi streaming test...");

    // Initialize utility server and client manager
    const utilServer = new UtilServerHelper("cmd/util-server/main.go");
    const clientManager = new ClientManager();

    try {
        // Start the Go utility server
        console.log("Starting Go utility server...");
        await utilServer.startServer();
        console.log("Go utility server is ready.");

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
        const client = await createManagedClient(
            clientManager,
            autoRelayAddr,
            GreeterService,
            { logger: testLogger },
        );

        await testClientAndBidiStreamingRequest(
            client,
            2,
            "LibP2PAutoRelayBidi",
        );

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

// Run the test if this script is executed directly
runPath4StreamTest()
    .then(() => {
        console.log("Script completed successfully.");
        process.exit(0);
    })
    .catch((error) => {
        console.error("Script failed:", error);
        process.exit(1);
    });