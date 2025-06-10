/**
 * Utility functions for dRPC client - consolidated vendor and custom utilities
 */
import { Code, ConnectError } from "@connectrpc/connect";
import { multiaddr } from "@multiformats/multiaddr";
import { uint8ArrayToString, parseEnvelope, Flag, toHex } from "./envelopes.js";
import { DRPCOptions } from "./types.js";

/**
 * codeToString returns the string representation of a Code.
 * Source: docs/vendors/connect-es/packages/connect/src/protocol-connect/code-string.ts
 * @private Internal code, does not follow semantic versioning.
 */
export function codeToString(value: Code): string {
    const name = Code[value] as string | undefined;
    if (typeof name != "string") {
        return value.toString();
    }
    return (
        name[0].toLowerCase() +
        name.substring(1).replace(/[A-Z]/g, (c) => "_" + c.toLowerCase())
    );
}

let stringToCode: Record<string, Code> | undefined;

/**
 * codeFromString parses the string representation of a Code in snake_case.
 * For example, the string "permission_denied" parses into Code.PermissionDenied.
 * Source: docs/vendors/connect-es/packages/connect/src/protocol-connect/code-string.ts
 * @private Internal code, does not follow semantic versioning.
 */
export function codeFromString(value: string | undefined): Code | undefined {
    if (value === undefined) {
        return undefined;
    }
    if (!stringToCode) {
        stringToCode = {};
        for (const value of Object.values(Code)) {
            if (typeof value == "string") {
                continue;
            }
            stringToCode[codeToString(value)] = value;
        }
    }
    return stringToCode[value];
}

/**
 * Efficient buffer concatenation using vendor approach.
 * This optimizes our custom concatUint8Arrays implementation.
 */
export function concatUint8Arrays(arrays: Uint8Array[]): Uint8Array {
    if (arrays.length === 0) {
        return new Uint8Array(0);
    }
    if (arrays.length === 1) {
        return arrays[0];
    }

    // Calculate total length
    const totalLength = arrays.reduce((acc, array) => acc + array.byteLength, 0);

    // Create new array and copy data efficiently
    const result = new Uint8Array(totalLength);
    let offset = 0;

    for (const array of arrays) {
        result.set(array, offset);
        offset += array.byteLength;
    }

    return result;
}

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
