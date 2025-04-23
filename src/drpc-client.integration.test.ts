import { describe, it, expect } from 'vitest';

// Integration tests for DrpcClient (TypeScript port of Go client_integration_test.go)
// These tests are expected to fail until the DrpcClient implementation is complete.

import { NewClient } from './drpc-client';
import { GreeterService } from '../examples/typescript/src/greeter/v1/greeter_connect';
import { SayHelloRequestSchema, StreamingEchoRequestSchema } from '../examples/typescript/src/greeter/v1/greeter_pb';

describe('DrpcClient Integration', () => {
  const timeout = 30000;

  // Utility: unary request test
  async function testClientUnaryRequest(client: any, name: string) {
    const req = SayHelloRequestSchema.create({ name });
    const resp = await client.sayHello(req);
    expect(resp.message).toBe(`Hello, ${name}!`);
  }

  // Utility: streaming request test
  async function testClientStreamingRequest(client: any, names: string[]) {
    const stream = client.streamingEcho();
    for (const name of names) {
      await stream.send(StreamingEchoRequestSchema.create({ message: name }));
    }
    await stream.close();
    const received: Record<string, boolean> = {};
    for (let i = 0; i < names.length; i++) {
      const resp = await stream.receive();
      received[resp.greeting] = true;
    }
    for (const name of names) {
      expect(received[`Hello, ${name}!`]).toBe(true);
    }
    await expect(stream.receive()).rejects.toThrow();
  }

  // Path 1: HTTP Direct
  describe('Path1_HTTPDirect', () => {
    it('unary', async () => {
      // TODO: Start HTTP server and get address
      const addr = 'http://localhost:8080'; // placeholder
      const client = await NewClient(addr, GreeterService);
      await testClientUnaryRequest(client, 'DRPC Test');
    }, timeout);

    it('streaming', async () => {
      const addr = 'http://localhost:8080'; // placeholder
      const client = await NewClient(addr, GreeterService);
      const names = ['Alice', 'Bob', 'Charlie', 'Dave'];
      await testClientStreamingRequest(client, names);
    }, timeout);
  });

  // Path 2: HTTP Gateway/Relay
  describe('Path2_HTTPGatewayRelay', () => {
    it('unary (force relay address)', async () => {
      // TODO: Start HTTP gateway/relay and get address
      const addr = 'http://localhost:8081/gateway/relay'; // placeholder
      const client = await NewClient(addr, GreeterService);
      await testClientUnaryRequest(client, 'DRPC Test');
    }, timeout);

    it('streaming (force relay address)', async () => {
      const addr = 'http://localhost:8081/gateway/relay'; // placeholder
      const client = await NewClient(addr, GreeterService);
      const names = ['Alice', 'Bob', 'Charlie', 'Dave'];
      await testClientStreamingRequest(client, names);
    }, timeout);

    it('unary (no/auto relay address)', async () => {
      const addr = 'http://localhost:8081/gateway'; // placeholder
      const client = await NewClient(addr, GreeterService);
      await testClientUnaryRequest(client, 'DRPC Test');
    }, timeout);

    it('streaming (no/auto relay address)', async () => {
      const addr = 'http://localhost:8081/gateway'; // placeholder
      const client = await NewClient(addr, GreeterService);
      const names = ['Alice', 'Bob', 'Charlie', 'Dave'];
      await testClientStreamingRequest(client, names);
    }, timeout);
  });

  // Path 3: LibP2P Direct
  describe('Path3_LibP2PDirect', () => {
    it('unary', async () => {
      // TODO: Start libp2p server and get multiaddr
      const addr = '/ip4/127.0.0.1/tcp/4001/p2p/QmServer'; // placeholder
      const client = await NewClient(addr, GreeterService);
      await testClientUnaryRequest(client, 'DRPC Test');
    }, timeout);

    it('streaming', async () => {
      const addr = '/ip4/127.0.0.1/tcp/4001/p2p/QmServer'; // placeholder
      const client = await NewClient(addr, GreeterService);
      const names = ['Alice', 'Bob', 'Charlie', 'Dave'];
      await testClientStreamingRequest(client, names);
    }, timeout);
  });

  // Path 4: LibP2P Relay
  describe('Path4_LibP2PRelay', () => {
    it('unary', async () => {
      // TODO: Start relay and server, get relay multiaddr
      const addr = '/ip4/127.0.0.1/tcp/4002/p2p/QmRelay/p2p-circuit/p2p/QmServer'; // placeholder
      const client = await NewClient(addr, GreeterService);
      await testClientUnaryRequest(client, 'DRPC Test Path 2 Long');
    }, timeout);

    it('streaming', async () => {
      const addr = '/ip4/127.0.0.1/tcp/4002/p2p/QmRelay/p2p-circuit/p2p/QmServer'; // placeholder
      const client = await NewClient(addr, GreeterService);
      const names = ['Path2Long-Alice', 'Path2Long-Bob', 'Path2Long-Charlie'];
      await testClientStreamingRequest(client, names);
    }, timeout);
  });
});