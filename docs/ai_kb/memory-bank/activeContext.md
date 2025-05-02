# Active Context

## Current Focus

- Path3_LibP2PDirect fails in TypeScript because the Go server returns `application/connect+proto` (protobuf), but the Connect-Web client only supports JSON (`application/connect+json`).
- The Go integration test works because the Go client supports protobuf.
- The content-type middleware was only for debugging and is now removed.
- To pass Path 3 in TS, the Go server must support JSON encoding for libp2p, or a custom protobuf-capable transport must be used in TS.

## Next Steps

- Consider adding JSON encoding support for libp2p in the Go server.
- Alternatively, implement or use a Connect transport in TypeScript that supports protobuf over fetch (not connect-web).
- All other integration paths pass; only Path 3 is blocked by this protocol mismatch.

## Technical Limitation

- This is a protocol compatibility issue between js-libp2p, Go libp2p, and Connect-Web.
- Documented for future reference and to avoid redundant debugging.

## 2025-04-26: HTTP/2 over libp2p (TS) Investigation

- Node.js `http2` cannot be used over libp2p streams (expects net.Socket/TLS).
- No maintained npm package provides HTTP/2 client/server over arbitrary Duplex streams.
- Only viable path: implement/adapt pure-JS HTTP/2 encoder/decoder for libp2p stream.
- No drop-in solution available as of this date.
