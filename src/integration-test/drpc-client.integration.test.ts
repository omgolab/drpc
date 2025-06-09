/**
 * Integration tests for the dRPC client using a Go utility server
 */
import { beforeAll, afterAll, describe, it } from "vitest";
import { GreeterService } from "../../demo/gen/ts/greeter/v1/greeter_pb";
import {
  UtilServerHelper,
  ClientManager,
  createManagedClient,
  testClientUnaryRequest,
  testServerStreamingRequest,
  testClientAndBidiStreamingRequest,
} from "./helpers";
import { createLogger, LogLevel } from "../client/core/logger";

// Create a logger for the test
const testLogger = createLogger({
  contextName: "Integration-Test",
  logLevel: LogLevel.DEBUG,
});

// Track utility server instance
let utilServer: UtilServerHelper;
// Create a client manager for resource tracking
let clientManager: ClientManager;

// Start Go server before all tests
beforeAll(async () => {
  console.log("Initializing Go utility server helper...");
  utilServer = new UtilServerHelper(8080);
  clientManager = new ClientManager();

  console.log("Starting Go utility server...");
  try {
    await utilServer.startServer();
    console.log("Go utility server is reported as ready.");
  } catch (err) {
    console.error("Failed to start Go utility server in beforeAll:", err);
    if (utilServer) {
      utilServer.stopServer(); // Attempt to clean up
    }
    throw err; // Fail the test suite
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
        const publicNodeInfo = await utilServer.getPublicNodeInfo();
        const addr = publicNodeInfo.http_address;

        if (!addr) {
          throw new Error(
            "Public node info doesn't contain a valid HTTP address",
          );
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
        // Get HTTP address from utility server
        const publicNodeInfo = await utilServer.getPublicNodeInfo();
        const addr = publicNodeInfo.http_address || "";
        if (!addr) {
          throw new Error(
            "Public node info doesn't contain a valid HTTP address",
          );
        }
        console.log(
          `Using public node HTTP address for Path 1 streaming: ${addr}`,
        );

        const client = await createManagedClient(
          clientManager,
          addr,
          GreeterService,
          { logger: testLogger },
        );
        await testServerStreamingRequest(client, "AliceHTTPDirect"); // Use the server streaming test
      },
      TEST_TIMEOUT,
    );

    it(
      "1.3-client/bidi streaming",
      async () => {
        // Get HTTP address from utility server
        const publicNodeInfo = await utilServer.getPublicNodeInfo();
        const addr = publicNodeInfo.http_address || "";
        if (!addr) {
          throw new Error(
            "Public node info doesn't contain a valid HTTP address",
          );
        }
        console.log(
          `Using public node HTTP address for Path 1 client/bidi streaming: ${addr}`,
        );

        // Create a client with the managed client helper
        const client = await createManagedClient(
          clientManager,
          addr,
          GreeterService,
          { logger: testLogger },
        );

        await testClientAndBidiStreamingRequest(client, 3, "HTTPBidi"); // Test Go server stream handling
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
          // Get gateway relay info from utility server
          console.log("Fetching gateway relay node info for Path 2 force relay...");
          const gatewayInfo = await utilServer.getGatewayRelayNodeInfo();
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
          await testClientUnaryRequest(
            client,
            "dRPC Test Gateway Relay Connectivity Check",
          );
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
          // Get gateway auto relay info from utility server
          console.log("Fetching gateway auto relay node info for Path 2 auto relay...");
          const gatewayInfo = await utilServer.getGatewayAutoRelayNodeInfo();
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
          await testClientUnaryRequest(
            client,
            "dRPC Test Gateway Auto Relay Connectivity Check",
          );
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
        // Get direct libp2p address from utility server
        console.log("Fetching public node info for Path 3 (LibP2P Direct)...");
        const publicNodeInfo = await utilServer.getPublicNodeInfo();
        const libp2pAddr = publicNodeInfo.libp2p_ma || "";
        if (libp2pAddr) {
          directAddr = libp2pAddr;
          console.log(
            `Using public node libp2p address for Path 3: ${directAddr}`,
          );
        } else {
          throw new Error(
            "Public node info doesn't contain a valid libp2p multiaddress",
          );
        }
      } catch (error) {
        console.error(
          "Failed to fetch public node libp2p address from utility server:",
          error,
        );
        console.warn(
          "No direct multiaddr found for Path3_LibP2PDirect; skipping test.",
        );
        directAddr = undefined;
      }

      if (!directAddr) {
        console.warn(
          "No direct (non-relay) multiaddr found for Path3_LibP2PDirect; skipping test.",
        );
      } else {
        console.log("Using direct multiaddr for Path3:", directAddr);
      }
    });

    it(
      "3.1-unary",
      async () => {
        if (!directAddr) {
          console.warn(
            "Skipping Path3_LibP2PDirect unary test: no direct address available.",
          );
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
          console.warn(
            "Skipping Path3_LibP2PDirect streaming test: no direct address available.",
          );
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
          console.warn(
            "Skipping Path3_LibP2PDirect client/bidi streaming test: no direct address available.",
          );
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
        // Get relay libp2p address from utility server
        console.log("Fetching relay node info for Path 4 (LibP2P Relay)...");
        const relayNodeInfo = await utilServer.getRelayNodeInfo();
        const libp2pAddr = relayNodeInfo.libp2p_ma || "";

        if (libp2pAddr && libp2pAddr.includes("/p2p-circuit/")) {
          fixedRelayAddr = libp2pAddr;
          console.log(
            `Using fixed relay node libp2p address for Path 4: ${fixedRelayAddr}`,
          );

          // Extract target node address for auto relay test
          const addrParts = libp2pAddr.split("/p2p-circuit/");
          if (addrParts.length === 2) {
            autoRelayAddr = addrParts[1].startsWith("/")
              ? addrParts[1]
              : "/" + addrParts[1];
            console.log(
              `Extracted auto relay target address for Path 4: ${autoRelayAddr}`,
            );
          } else {
            console.warn(
              "Could not extract auto relay target address from relay address",
            );
            autoRelayAddr = undefined;
          }
        } else {
          console.warn(
            "Relay node info doesn't contain a valid relay libp2p multiaddress",
          );
          fixedRelayAddr = undefined;
          autoRelayAddr = undefined;
        }
      } catch (error) {
        console.error(
          "Failed to fetch relay node info from utility server:",
          error,
        );
        console.warn(
          "No suitable address found for Path4_LibP2PRelay; skipping test.",
        );
        fixedRelayAddr = undefined;
        autoRelayAddr = undefined;
      }

      if (!fixedRelayAddr) {
        console.warn(
          "No relay address found for Path4_LibP2PRelay; skipping test.",
        );
      } else {
        console.log("Using fixed relay address for Path4:", fixedRelayAddr);
        if (autoRelayAddr) {
          console.log(
            "Using auto relay target address for Path4:",
            autoRelayAddr,
          );
        }
      }
    });

    describe("libp2p_fixed_relay", () => {
      it(
        "4.1-unary",
        async () => {
          if (!fixedRelayAddr) {
            console.warn(
              "Skipping Path4 fixed relay unary test: no relay address available.",
            );
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
            console.warn(
              "Skipping Path4 fixed relay streaming test: no relay address available.",
            );
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
            console.warn(
              "Skipping Path4 fixed relay client/bidi streaming test: no relay address available.",
            );
            return;
          }

          try {
            const client = await createManagedClient(
              clientManager,
              fixedRelayAddr,
              GreeterService,
              { logger: testLogger },
            );
            await testClientAndBidiStreamingRequest(
              client,
              2,
              "LibP2PFixedRelayBidi",
            );
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
            console.warn(
              "Skipping Path4 auto relay unary test: no target address available.",
            );
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
            console.warn(
              "Skipping Path4 auto relay streaming test: no target address available.",
            );
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
            console.warn(
              "Skipping Path4 auto relay client/bidi streaming test: no target address available.",
            );
            return;
          }

          try {
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
  console.log(`Cleaning up ${clientManager.clientCount} clients...`);
  await clientManager.cleanup();

  // Stop the utility server
  console.log("Stopping Go utility server...");
  if (utilServer) {
    utilServer.stopServer();
  }
  console.log("Go utility server stopped.");
});
