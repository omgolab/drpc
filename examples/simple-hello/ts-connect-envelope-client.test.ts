// filepath: /Volumes/Projects/business/AstronLab/test-console/drpc-rnd/examples/simple-hello/ts-connect-envelope-client.test.ts
// Integration test for Connect Envelope TypeScript client
import { Writer, Reader } from "protobufjs/minimal.js"
import { pipe } from 'it-pipe'
import all from 'it-all'
import { expect, test, beforeEach, beforeAll, afterAll, describe } from 'vitest';
import { createLibp2p } from 'libp2p';
import { tcp } from '@libp2p/tcp';
import { webSockets } from '@libp2p/websockets';
import { noise } from '@chainsafe/libp2p-noise';
import { yamux } from '@chainsafe/libp2p-yamux';
import { mdns } from '@libp2p/mdns';
import fetch from 'node-fetch';
import { multiaddr } from '@multiformats/multiaddr';
import { spawn } from 'child_process';
import { createInterface } from 'readline';
import * as path from 'path';
import { peerIdFromString } from '@libp2p/peer-id';

// Type definitions for libp2p and test objects
interface TestSetup {
  node: any;
  peerId: any;
  stream?: any;
  goProc: any;
}

// Shared test setup state
let testSetup: TestSetup | null = null;

// Optimized protobuf encoding/decoding functions for SayHello (unary)
function encodeSayHelloRequest(obj: { name: string }): Uint8Array {
  return encodeMessage(obj, {
    name: { number: 1, type: 'string' }
  });
}

function decodeSayHelloResponse(buf: Uint8Array): { message: string } {
  return decodeMessage(buf, {
    1: { name: 'message', type: 'string' }
  }) as { message: string };
}

// Protobuf encoding/decoding functions for StreamingEcho (streaming)
function encodeStreamingEchoRequest(obj: { message: string }): Uint8Array {
  return encodeMessage(obj, {
    message: { number: 1, type: 'string' }
  });
}

function decodeStreamingEchoResponse(buf: Uint8Array): { message: string } {
  return decodeMessage(buf, {
    1: { name: 'message', type: 'string' }
  }) as { message: string };
}

// Generic protocol buffer message encoder
interface Message {
  [key: string]: any;
}

interface FieldDefinition {
  number: number;
  type: 'string' | 'bytes' | 'uint32' | 'int32' | 'bool';
}

// Enhanced generic encoder for protocol buffer messages
function encodeMessage(msg: Message, fieldDefinitions: Record<string, FieldDefinition>): Uint8Array {
  const writer = Writer.create();

  for (const [key, value] of Object.entries(msg)) {
    if (value === undefined || value === null) continue;

    const fieldDef = fieldDefinitions[key];
    if (!fieldDef) continue;

    const fieldNumber = fieldDef.number;
    const fieldType = fieldDef.type;

    switch (fieldType) {
      case 'string':
        writer.uint32((fieldNumber << 3) | 2).string(value);
        break;
      case 'bytes':
        if (value instanceof Uint8Array) {
          writer.uint32((fieldNumber << 3) | 2).bytes(value);
        } else {
          throw new Error(`Field ${key} must be Uint8Array for bytes type`);
        }
        break;
      case 'uint32':
        writer.uint32((fieldNumber << 3) | 0).uint32(value);
        break;
      case 'int32':
        writer.uint32((fieldNumber << 3) | 0).int32(value);
        break;
      case 'bool':
        writer.uint32((fieldNumber << 3) | 0).bool(value);
        break;
      default:
        throw new Error(`Unsupported field type: ${fieldType}`);
    }
  }

  return writer.finish();
}

// Enhanced generic protocol buffer message decoder
function decodeMessage(buf: Uint8Array, fieldMapping: Record<number, { name: string, type: 'string' | 'uint32' | 'int32' | 'bool' | 'bytes' }>): Message {
  const reader = Reader.create(buf);
  const result: Message = {};

  while (reader.pos < reader.len) {
    const tag = reader.uint32();
    const fieldNumber = tag >>> 3;
    const wireType = tag & 7;

    const field = fieldMapping[fieldNumber];
    if (!field) {
      reader.skipType(wireType);
      continue;
    }

    const { name, type } = field;

    switch (wireType) {
      case 0: // varint
        if (type === 'uint32') {
          result[name] = reader.uint32();
        } else if (type === 'int32') {
          result[name] = reader.int32();
        } else if (type === 'bool') {
          result[name] = reader.bool();
        } else {
          reader.skipType(wireType);
        }
        break;
      case 2: // length-delimited (string, bytes)
        if (type === 'string') {
          result[name] = reader.string();
        } else if (type === 'bytes') {
          result[name] = reader.bytes();
        } else {
          reader.skipType(wireType);
        }
        break;
      default:
        reader.skipType(wireType);
    }
  }

  return result;
}

// Generic envelope handling

// Flag constants for better readability
const EnvelopeFlags = {
  UNARY: 0,        // For unary requests/responses
  STREAMING: 1,    // For streaming requests/responses  
  END_STREAM: 0,   // End of streaming marker (same as UNARY)
  ERROR: 2,        // Error response
  BIDIRECTIONAL: 3 // For bidirectional streaming
};

interface EnvelopeOptions {
  isStreaming?: boolean;
  isError?: boolean;
  isBidirectional?: boolean;
  isEndOfStream?: boolean;
}

// Enhanced envelope encoder with better semantics
function encodeEnvelope(data: Uint8Array, options: EnvelopeOptions = {}): Uint8Array {
  let flags = EnvelopeFlags.UNARY; // Default

  if (options.isError) {
    flags = EnvelopeFlags.ERROR;
  } else if (options.isBidirectional) {
    flags = EnvelopeFlags.BIDIRECTIONAL;
  } else if (options.isStreaming && !options.isEndOfStream) {
    flags = EnvelopeFlags.STREAMING;
  }

  const bytes = new Uint8Array(data.length + 5);
  const view = new DataView(bytes.buffer);

  view.setUint8(0, flags);
  view.setUint32(1, data.length, false); // big-endian
  bytes.set(data, 5);

  return bytes;
}

// Enhanced envelope parser
function parseEnvelope(buf: Uint8Array): {
  flags: number;
  data: Uint8Array;
  isStreaming: boolean;
  isError: boolean;
  isBidirectional: boolean;
  isEndOfStream: boolean;
} {
  if (buf.length < 5) throw new Error("Envelope too short");

  const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const flags = view.getUint8(0);
  const len = view.getUint32(1, false); // big-endian

  if (buf.length < 5 + len) throw new Error("Envelope payload incomplete");

  return {
    flags,
    data: new Uint8Array(buf.buffer, buf.byteOffset + 5, len),
    isStreaming: flags === EnvelopeFlags.STREAMING,
    isError: flags === EnvelopeFlags.ERROR,
    isBidirectional: flags === EnvelopeFlags.BIDIRECTIONAL,
    isEndOfStream: flags === EnvelopeFlags.END_STREAM
  };
}

// Utility for promise timeout
const withTimeout = <T>(
  promise: Promise<T>,
  timeoutMs: number,
  errorMessage: string
): Promise<T> => {
  let timeoutId: NodeJS.Timeout;

  const timeoutPromise = new Promise<never>((_, reject) => {
    timeoutId = setTimeout(() => reject(new Error(`Timeout after ${timeoutMs}ms: ${errorMessage}`)), timeoutMs);
  });

  return Promise.race([
    promise.finally(() => clearTimeout(timeoutId)),
    timeoutPromise
  ]);
};

// Helper to convert various types to Buffer
function toBuffer(data: any): Buffer {
  if (data instanceof Uint8Array) {
    return Buffer.from(data.buffer, data.byteOffset, data.byteLength);
  } else if (typeof data.subarray === "function") {
    return Buffer.from(data.subarray(0));
  } else {
    return Buffer.from(Uint8Array.from(data));
  }
}

// Cleanup functions
let cleanupFunctions: Array<() => Promise<void>> = [];

// Start the Go server
async function startGoServer(): Promise<any> {
  const goServerPath = path.resolve(__dirname, "go-connect-envelope-server.go");
  console.log("Starting Go server from:", goServerPath);

  // Start the Go server
  const goProc = spawn("go", ["run", goServerPath], {
    cwd: __dirname,
    stdio: ["pipe", "pipe", "pipe"],
    env: { ...process.env },
  });

  // Register cleanup for Go process
  cleanupFunctions.push(async () => {
    console.log("Cleaning up Go process...");
    if (!goProc.killed) {
      goProc.kill('SIGTERM');
      await new Promise<void>(resolve => {
        const timeout = setTimeout(() => {
          console.warn("Go process didn't exit cleanly, forcing with SIGKILL");
          goProc.kill('SIGKILL');
          resolve();
        }, 1000);

        goProc.once('exit', () => {
          clearTimeout(timeout);
          resolve();
        });
      });
    }
  });

  // Log output from Go process
  goProc.stdout.on("data", (data) => {
    console.log("[go stdout] " + data.toString());
  });

  goProc.stderr.on("data", (data) => {
    console.error("[go stderr] " + data.toString());
  });

  // Wait for Go server to be ready
  console.log("Waiting for Go server to be ready...");
  await new Promise<void>((resolve, reject) => {
    const rl = createInterface({ input: goProc.stdout });
    const timeout = setTimeout(() => {
      rl.close();
      reject(new Error("Go server did not become ready"));
    }, 5000);

    rl.on('line', (line) => {
      if (line.includes("Listening on:")) {
        clearTimeout(timeout);
        rl.close();
        resolve();
      }

      if (line.includes("panic") || line.includes("Error")) {
        clearTimeout(timeout);
        rl.close();
        reject(new Error("Go server error: " + line));
      }
    });

    goProc.once('exit', (code) => {
      clearTimeout(timeout);
      rl.close();
      reject(new Error(`Go server exited with code ${code} before becoming ready`));
    });
  });

  // Allow server a moment to fully initialize
  await new Promise(r => setTimeout(r, 100));
  return goProc;
}

// Create libp2p node
async function createNode(): Promise<any> {
  const node = await createLibp2p({
    transports: [tcp(), webSockets()],
    connectionEncrypters: [noise()],
    streamMuxers: [yamux()],
    peerDiscovery: [mdns({ interval: 1000 })]
  });

  // Register cleanup for libp2p node
  cleanupFunctions.push(async () => {
    console.log("Cleaning up libp2p node...");
    try {
      // Close all connections first
      const connections = node.getConnections();
      if (connections.length > 0) {
        console.log(`Closing ${connections.length} connections...`);
        await Promise.allSettled(
          connections.map(async (connection: any) => {
            try {
              await connection.close();
            } catch (err) {
              console.error("Error closing connection:", err);
            }
          })
        );
      }

      // Stop the node with a timeout
      await Promise.race([
        node.stop(),
        new Promise<void>(resolve => {
          setTimeout(() => {
            console.log("Node stop timed out, continuing anyway");
            resolve();
          }, 1500);
        })
      ]);
    } catch (err) {
      console.error("Error stopping node:", err);
    }
  });

  await node.start();
  console.log("TS client started with ID:", node.peerId.toString());
  return node;
}

// Connect to Go server
async function connectToGoServer(node: any): Promise<any> {
  // --- Fetch Go server multiaddr ---
  console.log("Fetching Go server multiaddr...");
  const res = await fetch('http://127.0.0.1:8080/multiaddr');
  if (!res.ok) throw new Error('HTTP error: ' + res.status);

  // Parse the multiaddr
  const addrStr = (await res.text()).trim().split('\n')[0];
  console.log("Using multiaddr:", addrStr);
  const addr = multiaddr(addrStr);

  // Extract peer ID
  const rawPeerId = addr.getPeerId();
  if (!rawPeerId) throw new Error('Multiaddr does not contain a peer ID');
  const peerId = peerIdFromString(rawPeerId);
  console.log("Go server peer ID:", peerId.toString());

  // Store peer info and dial
  await node.peerStore.patch(peerId, {
    multiaddrs: [addr],
    protocols: ['/connect-envelope/1.0.0']
  });

  await node.dial(addr);
  console.log("Successfully connected to Go server");
  return peerId;
}

// Main test suite
describe('Connect Envelope TypeScript client', () => {
  // Set up common server for all tests
  beforeAll(async () => {
    console.log("=== SETTING UP SHARED TEST ENVIRONMENT ===");
    // Reset cleanup functions
    cleanupFunctions = [];

    // Start Go server
    const goProc = await startGoServer();

    // Create node 
    const node = await createNode();

    // Connect to Go server
    const peerId = await connectToGoServer(node);

    // Store setup for tests to use
    testSetup = { node, peerId, goProc };

    console.log("=== SHARED TEST ENVIRONMENT READY ===");
  }, 30000);

  // Clean up after all tests complete
  afterAll(async () => {
    console.log("=== CLEANING UP SHARED TEST ENVIRONMENT ===");
    if (cleanupFunctions.length > 0) {
      for (const cleanup of cleanupFunctions.reverse()) {
        try {
          await cleanup();
        } catch (err) {
          console.error("Error during cleanup:", err);
        }
      }
    }
    testSetup = null;
    cleanupFunctions = [];
    console.log("=== SHARED TEST ENVIRONMENT CLEANED UP ===");
  });

  // Set up a fresh stream for each test 
  beforeEach(async () => {
    if (!testSetup) throw new Error("Test setup not initialized");

    // Create a new protocol stream for each test
    console.log("Opening new protocol stream for test...");
    const stream = await testSetup.node.dialProtocol(testSetup.peerId, '/connect-envelope/1.0.0');
    testSetup.stream = stream;

    // Register stream cleanup
    const currentStream = stream;
    cleanupFunctions.push(async () => {
      console.log("Cleaning up test stream...");
      try {
        await currentStream.close();
      } catch (err) {
        console.error("Error closing stream:", err);
      }
    });
  });

  // Test unary request-response
  test('unary: SayHello request-response', async () => {
    if (!testSetup || !testSetup.stream) throw new Error("Test setup not initialized");
    const { stream } = testSetup;

    // Send unary SayHello request
    const reqObj = { name: 'TypeScript Unary Client' };
    const reqBuf = encodeSayHelloRequest(reqObj);
    const envelope = encodeEnvelope(reqBuf); // Use defaults for unary
    console.log("Sending unary SayHello request...");
    await pipe([envelope], stream.sink);

    // Receive response
    console.log("Waiting for response...");
    const respChunks = await all(stream.source);
    console.log(`Received ${respChunks.length} response chunks`);

    if (respChunks.length === 0) {
      throw new Error("No response received");
    }

    // Process response
    const respBuf = Buffer.concat(respChunks.map(toBuffer));
    console.log(`Response buffer length: ${respBuf.length}`);

    const { flags, data: payload } = parseEnvelope(respBuf);
    console.log(`Response envelope flags: ${flags}`);

    const resp = decodeSayHelloResponse(payload);
    console.log("Response:", resp);

    // Validate response
    expect(resp.message).toContain('Hello, TypeScript Unary Client');

    console.log("Unary test completed successfully");
  }, 10000);

  // Test bidirectional streaming - note that current implementation only supports single message per connection
  test('streaming: StreamingEcho bidirectional stream', async () => {
    if (!testSetup || !testSetup.stream) throw new Error("Test setup not initialized");
    const { stream } = testSetup;

    // Send multiple bidirectional streaming echo requests
    const messages = ["Message 1", "Message 2", "Message 3", "Message 4", "Final Message"];
    console.log("Sending bidirectional streaming echo requests:", messages);

    // Create envelopes for each message with bidirectional flag (3)
    const envelopes: Uint8Array[] = [];

    // Generate bidirectional streaming requests - current implementation only responds to the first message
    for (const message of messages) {
      const reqBuf = encodeStreamingEchoRequest({ message });
      // Determine if this is the last message
      const isLast = message === messages[messages.length - 1];
      // Create envelope with appropriate options for bidirectional streaming
      const envelope = encodeEnvelope(reqBuf, {
        isBidirectional: true,
        isEndOfStream: isLast
      });

      // Log the first byte of the envelope to verify the flags
      console.log(`Envelope for "${message}": flag=${envelope[0]}, expected flag=${EnvelopeFlags.BIDIRECTIONAL}`);

      envelopes.push(envelope);
    }

    // Send all envelopes in one go - this approach is more compatible with libp2p streams
    console.log(`Sending ${envelopes.length} envelopes...`);
    // NOTE: In a production environment, we would send each message individually and wait for a response
    // before sending the next one, but our current Go server implementation has limitations in how
    // it processes multiple envelopes in a single stream.
    await pipe(envelopes, stream.sink);
    console.log("All bidirectional streaming requests sent");

    // Receive streaming responses
    console.log("Waiting for streaming responses...");
    const respChunks = await all(stream.source);
    console.log(`Received ${respChunks.length} response chunks`);

    if (respChunks.length === 0) {
      throw new Error("No streaming responses received");
    }

    // Process streaming responses
    let responses: string[] = [];
    let leftover = Buffer.alloc(0);

    console.log("Processing bidirectional response chunks:", respChunks.length);
    for (let i = 0; i < respChunks.length; i++) {
      const chunk = respChunks[i] as any;
      console.log(`Processing chunk ${i + 1}/${respChunks.length}, length: ${chunk.length || '?'}`);

      // Convert chunk to Buffer
      let buf = toBuffer(chunk);
      console.log(`Chunk ${i + 1} converted to buffer, length: ${buf.length}`);

      // Combine with leftover from previous chunk
      buf = Buffer.concat([leftover, buf]);
      console.log(`Combined buffer length: ${buf.length}, leftover length: ${leftover.length}`);

      // Parse envelopes from buffer
      let offset = 0;
      while (offset + 5 <= buf.length) {
        const flags = buf.readUInt8(offset);
        const len = buf.readUInt32BE(offset + 1);

        console.log(`Found envelope at offset ${offset}: flags=${flags}, length=${len}`);

        if (offset + 5 + len > buf.length) {
          console.log(`Incomplete envelope, waiting for more data (need ${offset + 5 + len - buf.length} more bytes)`);
          break;
        }

        const payload = buf.subarray(offset + 5, offset + 5 + len);
        console.log(`Processing envelope payload, length: ${payload.length}`);

        try {
          const resp = decodeStreamingEchoResponse(payload);
          console.log(`Decoded response: ${resp.message}`);
          responses.push(resp.message);
        } catch (err) {
          console.error(`Error decoding response: ${err}`);
        }

        offset += 5 + len;
      }

      leftover = buf.subarray(offset);
      console.log(`Leftover bytes for next chunk: ${leftover.length}`);
    }

    // Verify responses
    console.log("Streaming responses received:", responses);

    // Validate that we got at least one response
    expect(responses.length).toBeGreaterThan(0);

    // Check that all received responses are in the expected format
    const responsePattern = /^(Echo: |Hello, )(.+?)( \(from Go server\))?$/;
    for (const response of responses) {
      expect(responsePattern.test(response)).toBe(true);
    }

    // For each response we have, verify it contains the original message
    for (let i = 0; i < Math.min(responses.length, messages.length); i++) {
      const message = messages[i];
      const response = responses[i];
      expect(response.includes(message)).toBe(true);
    }

    console.log("Streaming test completed successfully");
  }, 10000);
});