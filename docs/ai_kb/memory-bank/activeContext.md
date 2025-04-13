# DRPC Active Context

## Current Focus

- **Investigating libp2p integration test failures:** Specifically focusing on `TestPath2_HTTPGatewayRelay`, `TestPath3_LibP2PDirect`, and `TestPath4_LibP2PRelay` in `pkg/drpc/client_integration_test.go`. Issues involve gateway relay timeouts, direct libp2p streaming incompatibility with ConnectRPC, and client relay connection timeouts.
- Refining core interfaces and types based on implementation findings.
- Improving error handling mechanisms.

## Recent Changes

- Attempted to debug and fix integration tests in `pkg/drpc/client_integration_test.go`.
    - `TestPath1_HTTPDirect` was successfully fixed.
    - `TestPath2_HTTPGatewayRelay`, `TestPath3_LibP2PDirect`, and `TestPath4_LibP2PRelay` remain failing due to libp2p transport issues.
- Basic client/server communication established over HTTP.
- Project initialized
- Basic directory structure established
- Core requirements defined

## Next Steps

1.  **Resolve LibP2P Integration Issues:** Deep dive into the root causes of failures in `TestPath2`, `TestPath3`, and `TestPath4`. This may involve:
    *   Verifying libp2p relay setup and functionality (including interaction with AutoRelay and discovered peers).
    *   Investigating potential incompatibilities between ConnectRPC streaming and direct libp2p streams.
    *   Debugging gateway relay stream opening.
2.  **Enhance Relay Usage:** Implement logic in the server to utilize the dynamically discovered relays (e.g., configure AutoRelay, attempt reservations, potentially advertise through them).
2.  Implement robust authentication mechanism.
3.  Implement comprehensive error handling across Go and TypeScript.
4.  Add streaming support verification once transport issues are resolved.
5.  Expand unit and end-to-end test coverage.
6.  Refine protocol specification based on implementation learnings.

## Active Decisions and Considerations

- How to best handle streaming over libp2p (direct vs. relayed, potential protocol adjustments).
- How the server should select and utilize dynamically discovered relays (vs. relying solely on AutoRelay or static configuration).
- Choosing the final serialization format (Protocol Buffers likely).
- Finalizing authentication mechanism.
- Defining a comprehensive set of error codes and handling strategies.
- Confirming transport protocols to officially support beyond HTTP and libp2p.
