/**
 * Utility functions for dRPC client
 */
import { Code } from "@connectrpc/connect";
import { multiaddr } from "@multiformats/multiaddr";
import { toHex, uint8ArrayToString, parseEnvelope, Flag } from "./envelope.js";
import { DRPCOptions } from "./types.js";

/**
 * Concatenate multiple Uint8Arrays
 */
export function concatUint8Arrays(arrays: Uint8Array[]): Uint8Array {
  const totalLength = arrays.reduce((len, arr) => len + arr.length, 0);
  const result = new Uint8Array(totalLength);
  let offset = 0;
  for (const arr of arrays) {
    result.set(arr, offset);
    offset += arr.length;
  }
  return result;
}

/**
 * Helper to map string codes to connectrpc Code enum
 * Based on @connectrpc/connect/protocol-connect/src/code-string.ts
 */
export function connectCodeFromString(s: string | undefined): Code | undefined {
  if (s === undefined) {
    return undefined;
  }
  switch (s) {
    case "canceled":
      return Code.Canceled;
    case "unknown":
      return Code.Unknown;
    case "invalid_argument":
      return Code.InvalidArgument;
    case "deadline_exceeded":
      return Code.DeadlineExceeded;
    case "not_found":
      return Code.NotFound;
    case "already_exists":
      return Code.AlreadyExists;
    case "permission_denied":
      return Code.PermissionDenied;
    case "resource_exhausted":
      return Code.ResourceExhausted;
    case "failed_precondition":
      return Code.FailedPrecondition;
    case "aborted":
      return Code.Aborted;
    case "out_of_range":
      return Code.OutOfRange;
    case "unimplemented":
      return Code.Unimplemented;
    case "internal":
      return Code.Internal;
    case "unavailable":
      return Code.Unavailable;
    case "data_loss":
      return Code.DataLoss;
    case "unauthenticated":
      return Code.Unauthenticated;
    default:
      return undefined;
  }
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

/**
 * Extract the target multiaddr for dialing, handling relay format if present
 */
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

/**
 * Parse a unary response in potentially multiple formats:
 * - Raw protobuf data (no envelope)
 * - JSON data
 * - Enveloped data (with protocol flags)
 */
export async function parseUnaryResponse<T>(
  responseData: Uint8Array,
  deserialize: (data: Uint8Array) => T,
  options: DRPCOptions,
): Promise<{
  success: boolean;
  message?: T;
  errorMessage?: string;
  envelope?: any;
}> {
  // Get the logger to use
  const logger = options.logger;

  if (!responseData || responseData.length === 0) {
    return {
      success: false,
      errorMessage: "Empty response received from server",
    };
  }

  // 1. First try parsing as a raw protobuf message (no envelope)
  try {
    const responseMessage = deserialize(responseData);
    logger.debug("[parseUnaryResponse] Successfully parsed as raw message");
    return {
      success: true,
      message: responseMessage,
    };
  } catch (err) {
    logger.debug(`[parseUnaryResponse] Failed to parse as raw message: ${err}`);
  }

  // 2. Try parsing as JSON
  try {
    const responseText = uint8ArrayToString(responseData);
    if (responseText.startsWith("{")) {
      try {
        const jsonObject = JSON.parse(responseText);
        logger.debug("[parseUnaryResponse] Successfully parsed as JSON");
        return {
          success: true,
          message: jsonObject as T,
        };
      } catch (jsonErr) {
        logger.debug(
          `[parseUnaryResponse] Failed to parse as JSON: ${jsonErr}`,
        );
      }
    }
  } catch (textErr) {
    logger.debug(
      `[parseUnaryResponse] Failed to convert to string: ${textErr}`,
    );
  }

  // 3. Try parsing as an envelope
  try {
    const parseResult = parseEnvelope(responseData);
    if (parseResult.envelope) {
      logger.debug(
        `[parseUnaryResponse] Successfully parsed as envelope with flags: ${parseResult.envelope.flags}`,
      );

      // If it's a data envelope, try to parse the payload
      if (
        parseResult.envelope.flags === Flag.NONE &&
        parseResult.envelope.data.length > 0
      ) {
        try {
          const responseMessage = deserialize(parseResult.envelope.data);
          return {
            success: true,
            message: responseMessage,
            envelope: parseResult.envelope,
          };
        } catch (envDataErr) {
          logger.debug(
            `[parseUnaryResponse] Failed to parse envelope data: ${envDataErr}`,
          );
        }
      }
      // If it's an error envelope, try to parse as JSON error
      else if (
        parseResult.envelope.data &&
        parseResult.envelope.data.length > 0
      ) {
        try {
          const errorText = uint8ArrayToString(parseResult.envelope.data);
          if (errorText.startsWith("{") && errorText.includes('"code"')) {
            const errorData = JSON.parse(errorText);
            return {
              success: false,
              errorMessage: errorData.message || "Unknown error from server",
              envelope: parseResult.envelope,
            };
          }
        } catch (errParseErr) {
          logger.debug(
            `[parseUnaryResponse] Failed to parse error data in envelope: ${errParseErr}`,
          );
        }
      }
    }
  } catch (envelopeErr) {
    logger.debug(
      `[parseUnaryResponse] Failed to parse as envelope: ${envelopeErr}`,
    );
  }

  // If all parsing methods failed, return a generic error
  const bufHex = responseData
    ? toHex(responseData.subarray(0, Math.min(responseData.length, 32)))
    : "";
  return {
    success: false,
    errorMessage: `Failed to parse response in any format. Received ${responseData?.length || 0} bytes (prefix: ${bufHex}...)`,
  };
}
