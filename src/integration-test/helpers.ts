import { spawn, ChildProcess } from "child_process";
import fetch from "node-fetch";
import { GreeterService } from "../../demo/gen/ts/greeter/v1/greeter_pb";
import { create } from "@bufbuild/protobuf";
import { DescService } from "@bufbuild/protobuf";
import {
  SayHelloRequestSchema,
  StreamingEchoRequestSchema,
  BidiStreamingEchoRequestSchema,
  BidiStreamingEchoRequest,
  BidiStreamingEchoResponse,
} from "../../demo/gen/ts/greeter/v1/greeter_pb";

import { DRPCOptions } from "../client/core/types";
import { DRPCClient, NewClient } from "../client";

// Constants matching the Go implementation
const UTIL_SERVER_READY_TIMEOUT = 30000; // 30 seconds for server to start

// Constants matching the Go implementation
const UTIL_SERVER_BASE_URL = "http://localhost:8080";
const PUBLIC_NODE_ENDPOINT = "/public-node";
const RELAY_NODE_ENDPOINT = "/relay-node";
const GATEWAY_NODE_ENDPOINT = "/gateway-node";
const GATEWAY_RELAY_NODE_ENDPOINT = "/gateway-relay-node";
const GATEWAY_AUTO_RELAY_NODE_ENDPOINT = "/gateway-auto-relay-node";

export interface NodeInfo {
  // Original Go struct uses snake_case
  http_address: string;
  libp2p_ma: string;
}

// Helper: run a promise with a timeout
export function withTimeout<T>(
  promise: Promise<T>,
  ms: number,
  errMsg = "Timeout",
): Promise<T> {
  return Promise.race([
    promise,
    new Promise<T>((_, reject) =>
      setTimeout(() => reject(new Error(errMsg)), ms),
    ),
  ]);
}

// Test utility functions

/**
 * Tests a unary RPC call using the specified client
 * @param client The gRPC client
 * @param name The name to use in the request
 * @returns Promise that resolves when the test is complete
 */
export async function testClientUnaryRequest(
  client: DRPCClient<typeof GreeterService>,
  name: string,
): Promise<void> {
  try {
    console.log(`Testing sayHello with name: ${name}`);
    const req = create(SayHelloRequestSchema, { name });
    const resp = await client.sayHello(req);
    console.log(`Received response: ${JSON.stringify(resp)}`);

    // Verify the response
    if (resp.message !== `Hello, ${name}!`) {
      throw new Error(`Expected "Hello, ${name}!" but got "${resp.message}"`);
    }
  } catch (err) {
    console.error(`Error in sayHello test: ${err}`);
    throw err;
  }
}

/**
 * Tests a server streaming RPC call using the specified client
 * @param client The gRPC client
 * @param name The message to use in the request
 * @returns Promise that resolves to the array of received messages
 */
export async function testServerStreamingRequest(
  client: DRPCClient<typeof GreeterService>,
  message: string,
): Promise<string[]> {
  try {
    console.log(`Testing streamingEcho with message: ${message}`);
    const request = create(StreamingEchoRequestSchema, { message });

    const stream = client.streamingEcho(request);
    const responses: string[] = [];

    for await (const resp of stream) {
      console.log(`Received server stream response: ${JSON.stringify(resp)}`);
      responses.push(resp.message);
    }

    console.log(
      `Total server stream responses: ${responses.length}, responses: ${JSON.stringify(responses)}`,
    );

    // Verify we got at least one response
    if (responses.length < 1) {
      throw new Error("No responses received from server stream");
    }

    // Verify the response contains the expected message
    if (!responses[0].includes(`Echo: ${message}`)) {
      throw new Error(
        `Expected response to contain "Echo: ${message}" but got "${responses[0]}"`,
      );
    }

    return responses;
  } catch (err: any) {
    // Handle streaming errors in a uniform way
    console.error(`Error in streamingEcho test: ${err}`);
    throw err;
  }
}

/**
 * Tests a bidirectional streaming RPC call using the specified client
 * @param client The gRPC client
 * @param numClientMessages Number of messages to send
 * @param baseName Base name for messages
 */
export async function testClientAndBidiStreamingRequest(
  client: DRPCClient<typeof GreeterService>,
  numClientMessages: number,
  baseName: string = "ClientMessage",
): Promise<void> {
  console.log(
    `Testing bidiStreamingEcho with ${numClientMessages} client messages.`,
  );

  // This creates an AsyncIterable that yields BidiStreamingEchoRequest objects
  async function* generateClientRequests(): AsyncIterable<BidiStreamingEchoRequest> {
    for (let i = 0; i < numClientMessages; i++) {
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

  try {
    // The responseStream is an AsyncIterable that yields BidiStreamingEchoResponse objects
    const responseStream = await client.bidiStreamingEcho(clientRequestStream);

    const receivedServerResponses: BidiStreamingEchoResponse[] = [];
    console.log("[ClientBidiTest] Starting to consume server responses...");

    for await (const serverMsg of responseStream) {
      console.log(
        `[ClientBidiTest] Received server message: ${JSON.stringify(serverMsg)}`,
      );
      receivedServerResponses.push(serverMsg);
    }

    console.log("[ClientBidiTest] Finished consuming server responses.");

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
    console.error(`[ClientBidiTest] Error in bidiStreamingEcho test: ${err}`);
    throw err;
  }
}

export class UtilServerHelper {
  private serverProcess: ChildProcess | undefined;
  private readonly binaryPath: string;
  private readonly readyTimeout: number;
  private readonly httpTimeout: number;
  private serverReady: Promise<void> | null = null;

  constructor(
    private port: number = 8080,
    readyTimeoutMs = UTIL_SERVER_READY_TIMEOUT,
  ) {
    this.readyTimeout = readyTimeoutMs;
    this.httpTimeout = 10000; // 10 seconds timeout, same as Go's httpClient

    // Use a simple, predictable binary path
    const os = require("os");
    this.binaryPath = `${os.tmpdir()}/tmp/util-server-${port}`;
  }

  public async startServer(): Promise<void> {
    if (this.serverProcess) {
      console.log("Utility server already running.");
      return;
    }

    await this.buildBinary();
    await this.runBinary();
  }

  private async buildBinary(): Promise<void> {
    console.log(`Building utility server: go build -o ${this.binaryPath} cmd/util-server/main.go`);

    await new Promise<void>((resolve, reject) => {
      const buildProcess = spawn(
        "go",
        ["build", "-o", this.binaryPath, "cmd/util-server/main.go"],
        {
          stdio: ["pipe", "pipe", "pipe"],
          cwd: process.cwd(),
        },
      );

      let buildOutput = "";
      let buildError = "";

      buildProcess.stdout?.on("data", (data) => {
        buildOutput += data.toString();
      });

      buildProcess.stderr?.on("data", (data) => {
        buildError += data.toString();
      });

      buildProcess.on("close", (code) => {
        if (code === 0) {
          console.log(`Successfully built utility server binary: ${this.binaryPath}`);
          resolve();
        } else {
          console.error(`Build failed with code ${code}`);
          if (buildError) console.error(`Build error: ${buildError}`);
          if (buildOutput) console.log(`Build output: ${buildOutput}`);
          reject(new Error(`Failed to build utility server: exit code ${code}`));
        }
      });

      buildProcess.on("error", (err) => {
        reject(new Error(`Build process error: ${err.message}`));
      });
    });
  }

  private async runBinary(): Promise<void> {
    console.log(`Starting utility server binary: ${this.binaryPath}`);

    // Create a promise for server readiness
    let resolveReady!: () => void;
    let rejectReady!: (error: Error) => void;
    this.serverReady = new Promise<void>((resolve, reject) => {
      resolveReady = resolve;
      rejectReady = reject;
    });

    // Now run the built binary directly
    this.serverProcess = spawn(this.binaryPath, [], {
      stdio: ["pipe", "pipe", "pipe"],
      cwd: process.cwd(),
      detached: false,
    });

    // Set up logging for server output
    this.serverProcess.stdout?.on("data", (data) => {
      console.log(`[UtilServer STDOUT]: ${data.toString().trim()}`);
    });

    this.serverProcess.stderr?.on("data", (data) => {
      console.error(`[UtilServer STDERR]: ${data.toString().trim()}`);
    });

    this.serverProcess.on("error", (err) => {
      console.error("Failed to start utility server process:", err);
      this.serverProcess = undefined;
      rejectReady(new Error(`Failed to start utility server: ${err.message}`));
    });

    this.serverProcess.on("close", (code) => {
      console.log(`Utility server process exited with code ${code}`);
      this.serverProcess = undefined;

      // If the server exits unexpectedly during startup, reject the ready promise
      if (this.serverReady) {
        rejectReady(
          new Error(`Utility server exited unexpectedly with code ${code}`),
        );
        this.serverReady = null;
      }
    });

    // Check server readiness
    this.checkServerReady(resolveReady, rejectReady);

    try {
      await this.serverReady;
      console.log("Utility server is ready.");
    } catch (err) {
      console.error("Utility server failed to start or become ready:", err);
      this.stopServer();
      throw err;
    }
  }

  private async checkServerReady(
    resolve: () => void,
    reject: (error: Error) => void,
  ): Promise<void> {
    const startTime = Date.now();

    const checkInterval = setInterval(async () => {
      // Stop checking if we've exceeded the timeout
      if (Date.now() - startTime > this.readyTimeout) {
        clearInterval(checkInterval);
        reject(
          new Error(
            "Utility server did not become ready within the timeout period.",
          ),
        );
        return;
      }

      // Stop checking if the server process has disappeared
      if (!this.serverProcess) {
        clearInterval(checkInterval);
        reject(
          new Error("Utility server process not found or already stopped."),
        );
        return;
      }

      try {
        // Using public-node as a health check, assuming it's always available
        // This matches the Go implementation which checks public-node endpoint
        const controller = new AbortController();
        const timeoutId = setTimeout(
          () => controller.abort(),
          this.httpTimeout,
        );

        const response = await fetch(
          `${UTIL_SERVER_BASE_URL}${PUBLIC_NODE_ENDPOINT}`,
          {
            signal: controller.signal as any,
          },
        );

        clearTimeout(timeoutId);

        if (response.ok) {
          clearInterval(checkInterval);
          resolve();
          return;
        }
      } catch (err) {
        // Continue checking until timeout
      }
    }, 500); // Check every 500ms, same as Go implementation
  }

  public async stopServer(): Promise<void> {
    console.log("Stopping utility server...");

    if (this.serverProcess) {
      const pid = this.serverProcess.pid;
      console.log(`Stopping server process PID: ${pid}`);

      return new Promise<void>((resolve) => {
        if (!this.serverProcess) {
          this.cleanupBinaryFile();
          resolve();
          return;
        }

        // Set up a timeout for forceful termination
        let timeoutId: NodeJS.Timeout;
        let resolved = false;

        const cleanup = () => {
          if (!resolved) {
            resolved = true;
            if (timeoutId) clearTimeout(timeoutId);
            this.serverProcess = undefined;
            this.cleanupBinaryFile();
            // As a backup, use targeted pkill for this specific binary path
            this.killBinaryByPath();
            resolve();
          }
        };

        // Listen for the process to exit
        this.serverProcess.once('close', (code) => {
          console.log(`Server process ${pid} exited with code ${code}`);
          cleanup();
        });

        this.serverProcess.once('error', (err) => {
          console.warn(`Server process ${pid} error during shutdown:`, err);
          cleanup();
        });

        // First try graceful shutdown with SIGTERM
        const killed = this.serverProcess.kill("SIGTERM");
        if (!killed) {
          console.warn(`Error sending SIGTERM to server process ${pid}. Process may not exist.`);
          cleanup();
          return;
        }

        console.log(`Sent SIGTERM to server process ${pid}, waiting for graceful shutdown...`);

        // Set a timeout for forceful termination
        timeoutId = setTimeout(() => {
          if (this.serverProcess && !resolved) {
            console.warn(`Server process ${pid} did not stop gracefully, sending SIGKILL...`);
            this.serverProcess.kill("SIGKILL");
            // Give it a moment for SIGKILL to take effect, then cleanup
            setTimeout(cleanup, 1000);
          }
        }, 5000); // 5 seconds timeout for graceful shutdown
      });
    } else {
      console.log("Server process not found or already stopped.");
      this.cleanupBinaryFile();
      this.killBinaryByPath();
    }
  }

  private cleanupBinaryFile(): void {
    try {
      const fs = require('fs');
      if (fs.existsSync(this.binaryPath)) {
        fs.unlinkSync(this.binaryPath);
        console.log(`Cleaned up binary file: ${this.binaryPath}`);
      }
    } catch (err) {
      console.warn(`Failed to cleanup binary file ${this.binaryPath}:`, err);
    }
  }

  private killBinaryByPath(): void {
    try {
      const { execSync } = require('child_process');
      // Use targeted pkill with the exact binary path - much safer than the old approach
      const pkillCmd = `pkill -f "^${this.binaryPath}$"`;
      console.log(`Running backup process cleanup: ${pkillCmd}`);
      execSync(pkillCmd, { stdio: 'pipe' });
      console.log(`Successfully killed any remaining processes for ${this.binaryPath}`);
    } catch (err: any) {
      // pkill returns non-zero exit code if no processes were found, which is fine
      if (err.status === 1) {
        console.log(`No remaining processes found for ${this.binaryPath} (expected)`);
      } else {
        console.warn(`pkill command failed:`, err.message);
      }
    }
  }

  private async getNodeInfo(endpoint: string): Promise<NodeInfo> {
    if (!this.serverProcess) {
      throw new Error("Utility server is not running. Cannot fetch node info.");
    }

    const serverURL = `${UTIL_SERVER_BASE_URL}${endpoint}`;
    console.log(`Requesting node info from: ${serverURL}`);

    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), this.httpTimeout);

      const response = await fetch(serverURL, {
        signal: controller.signal as any,
      });

      clearTimeout(timeoutId);

      if (!response.ok) {
        const bodyText = await response.text();
        throw new Error(
          `Received non-OK status code ${response.status} from ${serverURL}: ${bodyText}`,
        );
      }

      const rawInfo = (await response.json()) as any;
      console.log(
        `Received node info from ${serverURL}: ${JSON.stringify(rawInfo)}`,
      );

      // Create NodeInfo object using snake_case property names
      const info: NodeInfo = {
        http_address: rawInfo.http_address || "", // Primary format from Go
        libp2p_ma: rawInfo.libp2p_ma || "", // Primary format from Go
      };

      return info;
    } catch (err) {
      throw new Error(
        `Error making GET request to ${serverURL}: ${(err as Error).message}`,
      );
    }
  }

  // GetPublicNodeInfo retrieves information for a public node.
  public async getPublicNodeInfo(): Promise<NodeInfo> {
    try {
      const info = await this.getNodeInfo(PUBLIC_NODE_ENDPOINT);

      if (!info.http_address) {
        throw new Error(
          `Public node info from ${PUBLIC_NODE_ENDPOINT} is missing HTTP address`,
        );
      }
      if (!info.libp2p_ma) {
        throw new Error(
          `Public node info from ${PUBLIC_NODE_ENDPOINT} is missing libp2p multiaddress`,
        );
      }
      return info;
    } catch (err) {
      throw new Error(
        `Failed to get public node info from ${PUBLIC_NODE_ENDPOINT}: ${(err as Error).message}`,
      );
    }
  }

  // GetRelayNodeInfo retrieves information for a relay node.
  public async getRelayNodeInfo(): Promise<NodeInfo> {
    try {
      const info = await this.getNodeInfo(RELAY_NODE_ENDPOINT);

      if (!info.libp2p_ma) {
        throw new Error(
          `Relay node info from ${RELAY_NODE_ENDPOINT} is missing libp2p multiaddress`,
        );
      }
      return info;
    } catch (err) {
      throw new Error(
        `Failed to get relay node info from ${RELAY_NODE_ENDPOINT}: ${(err as Error).message}`,
      );
    }
  }

  // GetGatewayNodeInfo retrieves information for a gateway node.
  public async getGatewayNodeInfo(): Promise<NodeInfo> {
    try {
      const info = await this.getNodeInfo(GATEWAY_NODE_ENDPOINT);

      if (!info.http_address) {
        throw new Error(
          `Gateway node info from ${GATEWAY_NODE_ENDPOINT} is missing HTTP address`,
        );
      }
      return info;
    } catch (err) {
      throw new Error(
        `Failed to get gateway node info from ${GATEWAY_NODE_ENDPOINT}: ${(err as Error).message}`,
      );
    }
  }

  // GetGatewayRelayNodeInfo retrieves information for a gateway relay node.
  public async getGatewayRelayNodeInfo(): Promise<NodeInfo> {
    try {
      const info = await this.getNodeInfo(GATEWAY_RELAY_NODE_ENDPOINT);

      if (!info.http_address) {
        throw new Error(
          `Gateway relay node info from ${GATEWAY_RELAY_NODE_ENDPOINT} is missing HTTP address`,
        );
      }
      return info;
    } catch (err) {
      throw new Error(
        `Failed to get gateway relay node info from ${GATEWAY_RELAY_NODE_ENDPOINT}: ${(err as Error).message}`,
      );
    }
  }

  // GetGatewayAutoRelayNodeInfo retrieves information for a gateway auto relay node.
  public async getGatewayAutoRelayNodeInfo(): Promise<NodeInfo> {
    try {
      const info = await this.getNodeInfo(GATEWAY_AUTO_RELAY_NODE_ENDPOINT);

      if (!info.http_address) {
        throw new Error(
          `Gateway auto relay node info from ${GATEWAY_AUTO_RELAY_NODE_ENDPOINT} is missing HTTP address`,
        );
      }
      return info;
    } catch (err) {
      throw new Error(
        `Failed to get gateway auto relay node info from ${GATEWAY_AUTO_RELAY_NODE_ENDPOINT}: ${(err as Error).message}`,
      );
    }
  }

  // Cleanup method to stop only the util-server processes started by our test suite
  public static async cleanupOrphanedProcesses(): Promise<void> {
    try {
      console.log("Cleaning up any orphaned util-server processes...");

      // Kill any remaining util-server binaries by pattern
      const { execSync } = require('child_process');
      execSync('pkill -f /tmp/util-server-', { stdio: 'ignore' });
      console.log("Killed orphaned util-server processes");
    } catch (err) {
      // pkill returns non-zero if no processes found, which is fine
      console.log("No orphaned util-server processes found");
    }

    console.log("Orphaned process cleanup completed.");

  }
}

/**
 * Simple test utility to track clients and ensure they're properly cleaned up after tests
 */
export class ClientManager {
  private clients: DRPCClient<any>[] = [];

  /**
   * Tracks a client for later cleanup
   * @param client The client to track
   * @returns The same client for chaining
   */
  track<T extends DRPCClient<any>>(client: T): T {
    this.clients.push(client);
    return client;
  }

  /**
   * Gets the count of managed clients
   */
  get clientCount(): number {
    return this.clients.length;
  }

  /**
   * Cleans up all tracked clients by calling their Close() methods
   */
  async cleanup(): Promise<void> {
    if (this.clients.length === 0) {
      return;
    }

    console.log(`Closing ${this.clients.length} dRPC clients...`);

    await Promise.all(
      this.clients.map(async (client) => {
        try {
          await client.Close();
        } catch (err) {
          console.warn("Error closing client:", err);
        }
      }),
    );

    this.clients = []; // Clear references
    console.log("All clients closed.");
  }
}

/**
 * Creates a client and registers it with the ClientManager for cleanup
 *
 * @param clientManager The client manager instance
 * @param addr The server address (HTTP or libp2p multiaddr)
 * @param service The service definition
 * @param options Additional client options
 * @returns A client that's being tracked for resource cleanup
 */
export async function createManagedClient<TService extends DescService>(
  clientManager: ClientManager,
  addr: string,
  service: TService,
  options: DRPCOptions,
): Promise<DRPCClient<TService>> {
  // Create the client and track it with the ClientManager
  const client = await NewClient(addr, service, options);
  return clientManager.track(client);
}
