/**
 * Integration tests for the dRPC client to verify content type handling
 */
import { beforeAll, afterAll, describe, it } from "vitest";
import { GreeterService } from "../../demo/gen/ts/greeter/v1/greeter_pb";
import {
  ClientManager,
  createManagedClient,
  testClientUnaryRequest,
  testServerStreamingRequest,
  testClientAndBidiStreamingRequest,
} from "./helpers";
import { getUtilServer, isUtilServerAccessible } from '../util/util-server';
import { createLogger, LogLevel } from "../client/core/logger";
import {
  UnaryContentType,
  StreamingContentType,
  unaryContentTypes as importedUnaryContentTypes,
  streamingContentTypes as importedStreamingContentTypes,
  CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE,
  CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE,
  GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE,
  GRPC_PROTO_WITH_UNARY_CONTENT_TYPE,
  CONNECT_CONTENT_TYPE,
  CONNECT_JSON_CONTENT_TYPE,
  GRPC_WEB_JSON_CONTENT_TYPE,
  GRPC_JSON_CONTENT_TYPE,
} from "../client/core/types";

// Create a logger for the test
const testLogger = createLogger({
  contextName: "Content-Type-Test",
  logLevel: LogLevel.DEBUG,
});

// Helper function to get content type name from its value
function getContentTypeName(contentType: string): string {
  const contentTypeMap: Record<string, string> = {
    [CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE]: "CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE",
    [CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE]: "CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE",
    [GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE]: "GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE",
    [GRPC_PROTO_WITH_UNARY_CONTENT_TYPE]: "GRPC_PROTO_WITH_UNARY_CONTENT_TYPE",
    [CONNECT_CONTENT_TYPE]: "CONNECT_CONTENT_TYPE",
    [CONNECT_JSON_CONTENT_TYPE]: "CONNECT_JSON_CONTENT_TYPE",
    [GRPC_WEB_JSON_CONTENT_TYPE]: "GRPC_WEB_JSON_CONTENT_TYPE",
    [GRPC_JSON_CONTENT_TYPE]: "GRPC_JSON_CONTENT_TYPE",
  };

  return contentTypeMap[contentType] || contentType;
}

// Track utility server instance
// Track utility server instance from the shared util-server
let utilServer: any;
// Create a client manager for resource tracking
let clientManager: ClientManager;

// Emergency cleanup handler for unexpected process termination
function setupEmergencyCleanup() {
  const cleanup = async () => {
    console.log("Emergency cleanup triggered...");
    console.log("(Utility server managed globally, not stopping here)");
    await getUtilServer().cleanupOrphanedProcesses();
  };

  // Handle different termination signals
  process.on('SIGINT', cleanup);
  process.on('SIGTERM', cleanup);
  process.on('exit', () => {
    console.log("Process exiting... (Utility server managed globally)");
  });
}

// Start Go server before all tests
beforeAll(async () => {
  // Clean up any orphaned processes from previous test runs FIRST
  console.log("Cleaning up any orphaned processes from previous test runs...");
  await getUtilServer().cleanupOrphanedProcesses();

  // Set up emergency cleanup handlers
  setupEmergencyCleanup();

  console.log("Getting shared utility server instance...");
  // Get the shared util server instance
  utilServer = getUtilServer();
  
  // Start the server if it's not accessible
  if (!(await isUtilServerAccessible())) {
    console.log("🔄 Utility server not accessible, starting it...");
    await utilServer.startServer();
  }
  
  // Verify the server is now accessible
  if (!(await isUtilServerAccessible())) {
    throw new Error("Utility server is not accessible after startup attempt");
  }
  
  clientManager = new ClientManager();
  console.log("Utility server is accessible and ready.");
});

// Constants for timeout
const TEST_TIMEOUT = 300000; // 5 minutes

describe("dRPC Client Content Type Tests", () => {
  let httpAddr: string;

  beforeAll(async () => {
    try {
      // Get HTTP address from utility server as the baseline connection
      // We'll use HTTP direct for all tests as it's the most reliable for content type testing
      const publicNodeInfo = await utilServer.getPublicNodeInfo();
      httpAddr = publicNodeInfo.http_address || "";
      if (!httpAddr) {
        throw new Error(
          "Public node info doesn't contain a valid HTTP address",
        );
      }
      console.log(
        `Using public node HTTP address for content type tests: ${httpAddr}`,
      );

      // Quick connectivity check
      const client = await createManagedClient(
        clientManager,
        httpAddr,
        GreeterService,
        { logger: testLogger },
      );
      await testClientUnaryRequest(
        client,
        "Content Type Test Connectivity Check",
      );
    } catch (error) {
      console.error("Failed to set up content type tests:", error);
      throw new Error(
        `Failed to initialize environment for content type tests: ${error}`,
      );
    }
  });

  describe("Unary Content Types", () => {
    // Use the pre-defined unary content types array from types.ts

    // Test each unary content type
    importedUnaryContentTypes.forEach((contentType) => {
      // Get the name of the content type from its value
      const contentTypeName = getContentTypeName(contentType);

      it(
        `should handle unary request with ${contentTypeName}`,
        async () => {
          // Create client with specific content type option
          const client = await createManagedClient(
            clientManager,
            httpAddr,
            GreeterService,
            {
              logger: testLogger,
              unaryContentType: contentType,
            },
          );

          // Test unary request with custom message to identify the content type used
          await testClientUnaryRequest(client, `Test with ${contentTypeName}`);
        },
        TEST_TIMEOUT,
      );
    });
  });

  describe("Streaming Content Types", () => {
    // Use the pre-defined streaming content types array from types.ts

    // Test each streaming content type for server streaming
    describe("Server Streaming", () => {
      importedStreamingContentTypes.forEach((contentType) => {
        // Get the name of the content type from its value
        const contentTypeName = getContentTypeName(contentType);

        it(
          `should handle server streaming with ${contentTypeName}`,
          async () => {
            try {
              // Create client with specific content type
              const client = await createManagedClient(
                clientManager,
                httpAddr,
                GreeterService,
                {
                  logger: testLogger,
                  streamingContentType: contentType,
                },
              );

              // Test server streaming with this content type
              await testServerStreamingRequest(
                client,
                `ServerStream_${contentTypeName}`,
              );
            } catch (err) {
              throw err;
            }
          },
          TEST_TIMEOUT,
        );
      });
    });

    // Test each streaming content type for client/bidirectional streaming
    describe("Client and Bidirectional Streaming", () => {
      // Only test the first two content types to avoid connection issues
      importedStreamingContentTypes.forEach((contentType) => {
        // Get the name of the content type from its value
        const contentTypeName = getContentTypeName(contentType);

        it(
          `should handle client/bidi streaming with ${contentTypeName}`,
          async () => {
            try {
              // Add a small delay to avoid overwhelming the connection pool
              await new Promise(resolve => setTimeout(resolve, 100));

              // Create client with specific content type
              const client = await createManagedClient(
                clientManager,
                httpAddr,
                GreeterService,
                {
                  logger: testLogger,
                  streamingContentType: contentType,
                },
              );

              // Test client/bidi streaming with this content type
              // We send 3 messages in the bidirectional test
              await testClientAndBidiStreamingRequest(
                client,
                3,
                `BidiStream_${contentTypeName}`,
              );
            } catch (err) {
              throw err;
            }
          },
          TEST_TIMEOUT,
        );
      });
    });
  });

  // Test matrix for all combinations of content types
  // This is an advanced test that tries different combinations to ensure they work together
  describe("Content Type Matrix Test", () => {
    // This test validates that different combinations of content types work together
    it(
      "should work with all combinations of unary and streaming content types",
      async () => {
        // For brevity, test only a smaller matrix of combinations using the first two elements from each array
        const unaryOptions = importedUnaryContentTypes.slice(0, 2);
        const streamingOptions = importedStreamingContentTypes.slice(0, 2);

        // Log that we're running the matrix test
        console.log(
          "Running content type matrix test with selected combinations",
        );

        for (const unary of unaryOptions) {
          for (const streaming of streamingOptions) {
            const unaryName = getContentTypeName(unary);
            const streamingName = getContentTypeName(streaming);

            console.log(
              `Testing combination: Unary=${unaryName}, Streaming=${streamingName}`,
            );

            try {
              // Create client with both content types specified
              const client = await createManagedClient(
                clientManager,
                httpAddr,
                GreeterService,
                {
                  logger: testLogger,
                  unaryContentType: unary as UnaryContentType,
                  streamingContentType: streaming as StreamingContentType,
                },
              );

              // Run a quick test of each type
              await testClientUnaryRequest(client, `Matrix-${unaryName}`);
              await testServerStreamingRequest(
                client,
                `Matrix-${streamingName}`,
              );
            } catch (err) {
              console.error(
                `Failed with combination: Unary=${unaryName}, Streaming=${streamingName}`,
                err,
              );
              throw err;
            }
          }
        }
      },
      TEST_TIMEOUT,
    );
  });
});

// After all tests, clean up resources
afterAll(async () => {
  console.log("Starting cleanup process...");

  try {
    console.log(`Cleaning up ${clientManager.clientCount} clients...`);
    await clientManager.cleanup();
    console.log("Client cleanup completed.");
  } catch (err) {
    console.error("Error during client cleanup:", err);
  }

  console.log("Test cleanup completed. (Utility server managed globally)");

  // Final cleanup of any orphaned processes
  console.log("Final cleanup of any orphaned processes...");
  await getUtilServer().cleanupOrphanedProcesses();

  console.log("Cleanup process completed.");
});
