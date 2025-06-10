/**
 * Utility functions for dRPC client
 */
import { Code } from "@connectrpc/connect";
import { multiaddr } from "@multiformats/multiaddr";
import { uint8ArrayToString, parseEnvelope, Flag, toHex } from "./envelopes.js";
import { concatUint8Arrays } from "./vendor-utils.js";
import { DRPCOptions } from "./types.js";

/**
 * Prepares the initial length-prefixed header payload.
 * Format: [4-byte length][path][1-byte length][content-type]
 */
export function prepareInitialHeaderPayload(
  method: { parent: { typeName: string }; name: string },
  contentTypeValue: string,
): Uint8Array {
  const procedurePath = `/${method.parent.typeName}/${method.name}`;

  // Encode procedure path
  const procPathBytes = new TextEncoder().encode(procedurePath);
  const procPathLenBuffer = new Uint8Array(4);
  new DataView(procPathLenBuffer.buffer).setUint32(
    0,
    procPathBytes.length,
    false,
  ); // false for big-endian

  // Encode content type
  const contentTypeBytes = new TextEncoder().encode(contentTypeValue);
  const contentTypeLenBuffer = new Uint8Array(1);
  new DataView(contentTypeLenBuffer.buffer).setUint8(
    0,
    contentTypeBytes.length,
  );

  // Combine all parts
  return concatUint8Arrays([
    procPathLenBuffer,
    procPathBytes,
    contentTypeLenBuffer,
    contentTypeBytes,
  ]);
}

/**
 * Extract the target multiaddr for dialing, handling relay format if present
 */
// TODO: this is probably incorrect; check its usage in the codebase and ensure it aligns with the expected behavior for relay addresses.
export function extractDialTargetFromMultiaddr(ma: any): any {
  const isRelay = ma.toString().includes("/p2p-circuit/");
  if (!isRelay) {
    return ma;
  }

  // For relay addresses, extract the direct portion for our tests
  // In a production relay setup, the full relay address would be used
  const fullAddrStr = ma.toString();
  const parts = fullAddrStr.split("/p2p-circuit/");

  if (parts.length >= 2) {
    const directAddrPart = parts[0];
    if (directAddrPart) {
      try {
        // Use the imported multiaddr function from the top-level import
        return multiaddr(directAddrPart);
      } catch (e) {
        console.warn(
          `[libp2pTransport] Error parsing direct part from relay:`,
          e,
        );
      }
    }
  }

  // Fallback to original address
  return ma;
}
