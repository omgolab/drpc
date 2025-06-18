import { GreeterService } from "../../../demo/gen/ts/greeter/v1/greeter_pb";
import { create } from "@bufbuild/protobuf";
import { DescService } from "@bufbuild/protobuf";
import {
  SayHelloRequestSchema,
  StreamingEchoRequestSchema,
  BidiStreamingEchoRequestSchema,
  BidiStreamingEchoRequest,
  BidiStreamingEchoResponse,
} from "../../../demo/gen/ts/greeter/v1/greeter_pb";

import { DRPCOptions } from "../../client/core/types";
import { DRPCClient, NewClient } from "../../client";

export interface NodeInfo {
  http_address: string;
  libp2p_ma: string;
}

// Environment detection
export const environment = typeof window !== 'undefined' ? 'browser' : 'node';
export const browserName = typeof window !== 'undefined' ?
  (navigator.userAgent.includes('Chrome') ? 'Chrome' :
    navigator.userAgent.includes('Firefox') ? 'Firefox' : 'Browser') : 'Node.js';

/**
 * Fetch node information from the utility server (browser-compatible)
 */
export async function fetchNodeInfo(endpoint: string, baseUrl: string = 'http://127.0.0.1:8080'): Promise<NodeInfo> {
  const url = `${baseUrl}${endpoint}`;

  try {
    // Create AbortController for timeout
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 10000); // 10 second timeout

    const response = await fetch(url, {
      signal: controller.signal,
      method: 'GET',
      headers: {
        'Accept': 'application/json',
        'Content-Type': 'application/json',
      }
    });

    clearTimeout(timeoutId);

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const data = await response.json() as any;
    return {
      http_address: data.http_address || "",
      libp2p_ma: data.libp2p_ma || "",
    };
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') {
      throw new Error(`Timeout fetching ${endpoint}: Request took longer than 10 seconds`);
    }

    // Browser-specific error handling
    if (environment === 'browser' && error instanceof Error) {
      if (error.message.includes('Failed to fetch') || error.message.includes('NetworkError')) {
        throw new Error(`Network error fetching ${endpoint}. Is the utility server running at ${baseUrl}?`);
      }
    }

    throw new Error(`Failed to fetch ${endpoint}: ${error instanceof Error ? error.message : String(error)}`);
  }
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
 * Tests a unary RPC call using the specified client (browser-compatible)
 * @param client The gRPC client
 * @param name The name to use in the request
 * @returns Promise that resolves when the test is complete
 */
export async function testClientUnaryRequest(
  client: DRPCClient<typeof GreeterService>,
  name: string,
): Promise<void> {
  try {
    console.log(`Environment: ${environment}, Browser: ${browserName}`);
    console.log(`Client details:`, {
      clientType: typeof client,
      clientKeys: Object.keys(client)
    });

    const req = create(SayHelloRequestSchema, { name });
    const resp = await client.sayHello(req);
    console.log(`Received response: ${JSON.stringify(resp)}`);

    if (resp.message !== `Hello, ${name}!`) {
      throw new Error(`Expected "Hello, ${name}!" but got "${resp.message}"`);
    }
  } catch (err) {
    console.error(`Error in sayHello test: ${err}`);

    // Additional debugging for browser environment
    if (environment === 'browser') {
      console.error(`Browser debugging info:`);
      console.error(`- Error type: ${typeof err}`);
      console.error(`- Error constructor: ${err?.constructor?.name}`);
      if (err instanceof Error) {
        console.error(`- Error message: ${err.message}`);
        console.error(`- Error stack: ${err.stack}`);
      } else {
        console.error(`- Error value: ${String(err)}`);
      }
    }

    throw err;
  }
}

/**
 * Tests a server streaming RPC call using the specified client (browser-compatible)
 * @param client The gRPC client
 * @param message The message to use in the request
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

    if (responses.length === 0) {
      throw new Error("Expected at least one response from server stream");
    }

    return responses;
  } catch (err) {
    console.error(`Error in streamingEcho test: ${err}`);
    throw err;
  }
}

/**
 * Tests a bidirectional streaming RPC call using the specified client (browser-compatible)
 * @param client The gRPC client
 * @param messageCount Number of messages to send
 * @param namePrefix Base name for messages
 */
export async function testClientAndBidiStreamingRequest(
  client: DRPCClient<typeof GreeterService>,
  messageCount: number,
  namePrefix: string,
): Promise<void> {
  console.log(`Testing bidiStreamingEcho with ${messageCount} client messages.`);

  try {
    async function* clientMessageGenerator(): AsyncIterable<BidiStreamingEchoRequest> {
      for (let i = 0; i < messageCount; i++) {
        const req = create(BidiStreamingEchoRequestSchema, {
          name: `${namePrefix}-${i}`,
        });
        console.log(`[ClientStreamGen] Yielding: ${JSON.stringify(req)}`);
        yield req;
      }
      console.log(`[ClientStreamGen] Finished yielding client messages.`);
    }

    console.log(`[ClientBidiTest] Starting to consume server responses...`);

    const stream = client.bidiStreamingEcho(clientMessageGenerator());
    const responses: BidiStreamingEchoResponse[] = [];

    for await (const resp of stream) {
      console.log(`[ClientBidiTest] Received server message: ${JSON.stringify(resp)}`);
      responses.push(resp);
    }

    console.log(`[ClientBidiTest] Finished consuming server responses.`);

    if (responses.length !== messageCount) {
      throw new Error(
        `Expected ${messageCount} responses but got ${responses.length}`,
      );
    }

    for (let i = 0; i < messageCount; i++) {
      const expectedGreeting = `Hello, ${namePrefix}-${i}!`;
      if (responses[i].greeting !== expectedGreeting) {
        throw new Error(
          `Expected "${expectedGreeting}" but got "${responses[i].greeting}"`,
        );
      }
    }

    console.log(`[ClientBidiTest] Assertions passed.`);
  } catch (err) {
    console.error(`Error in bidiStreamingEcho test: ${err}`);
    throw err;
  }
}

/**
 * Simple client manager for resource tracking (browser-compatible)
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

    this.clients = [];
    console.log("All clients closed.");
  }
}

/**
 * Create a managed client (browser-compatible)
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
  console.log(`Creating dRPC client for address: ${addr} in environment: ${environment}`);

  try {
    const client = await NewClient(addr, service, options);
    console.log(`Successfully created dRPC client`);
    return clientManager.track(client);
  } catch (error) {
    console.error(`Failed to create dRPC client:`, error);
    throw error;
  }
}

/**
 * Set up browser network monitoring for debugging (browser-only)
 */
export function setupBrowserNetworkMonitoring(): void {
  if (environment === 'browser' && typeof window !== 'undefined') {
    console.log('Setting up browser network monitoring...');

    // Monitor fetch requests (only for localhost/127.0.0.1)
    const originalFetch = window.fetch;
    (window as any).fetch = async function (...args: Parameters<typeof fetch>) {
      const [url, options] = args;
      const urlString = typeof url === 'string' ? url : url.toString();

      // Only log for localhost/127.0.0.1 hosts
      const isLocalhost = urlString.includes('localhost') || urlString.includes('127.0.0.1');

      if (isLocalhost) {
        console.log(`🌐 Browser fetch request: ${urlString}`);
        // if (options) {
        //   console.log('🌐 Request options:', options);
        // }
      }

      try {
        const response = await originalFetch.apply(this, args);
      // if (isLocalhost) {
      //   console.log(`🌐 Response status: ${response.status} ${response.statusText}`);
      // }
        return response;
      } catch (error) {
        if (isLocalhost) {
          console.error(`🌐 Fetch error for ${urlString}:`, error);
        }
        throw error;
      }
    };
  }
}
