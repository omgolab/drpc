# Active Context

## Current Focus

- Implement proper client streaming and bidirectional streaming in the dRPC TypeScript client
- Fix the 4 failing tests in `src/drpc-client.integration.test.ts` related to Path3_LibP2PDirect and Path4_LibP2PRelay
- Address three key issues identified in the tests:
  1. The "synthetic relay address" problem in Path4_LibP2PRelay test that uses a simulated relay path
  2. The workaround for server streaming tests that returns synthetic responses
  3. The END_STREAM envelope error message from the Go server

## Recent Changes

- Modularized the dRPC client implementation by moving code to the client/ directory:
  - `src/client/index.ts` - Main client entry point
  - `src/client/core/types.ts` - Type definitions for transport interfaces
  - `src/client/core/utils.ts` - Utility functions for handling multiaddr, responses, etc.
  - `src/client/core/http-transport.ts` - HTTP transport implementation
  - `src/client/core/libp2p-transport.ts` - LibP2P transport implementation
- Made `src/drpc-client.ts` a thin wrapper around the modular implementation
- Fixed circular dependencies by defining dRPCOptions only in the main file
- Fixed the TypeScript client to ensure correct content types are used for different streaming protocols
- Improved error handling for END_STREAM envelopes in both unary and streaming modes
- Fixed syntax errors in test functions by properly typing the client with `Client<typeof GreeterService>`
- Identified that the relay-server implementation has compilation errors and needs updating

## Next Steps

1. Fix the failing test for Path4_LibP2PRelay unary operation
2. Implement a proper relay server following the pattern in client_integration_test.go
   - Fix the resource manager issue in the relay-server implementation
   - Update the relay server implementation to work with the latest libp2p version
3. Update the Path4_LibP2PRelay test to use a real relay node instead of a synthetic address
4. Remove synthetic responses in tests by ensuring the Go server sends proper responses
5. Fix the END_STREAM envelope handling to follow instructions from bridge3:
   - Client should close the write side rather than sending explicit END_STREAM
   - Server should return END_STREAM flag after sending all responses

## Active Decisions

- Use the bridge3 example as a reference for implementing client and bidirectional streaming
- Follow the envelope protocol exactly as specified in the bridge3 instructions
- Adopt the content type handling from bridge3 for better cross-language compatibility
- Implement a real relay server based on the integration test implementation rather than using synthetic addresses
- Keep the modular approach with a clean separation between HTTP and LibP2P transports
