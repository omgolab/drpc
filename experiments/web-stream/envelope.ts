// This file implements the Connect envelope protocol for the TypeScript client
// It defines types and functions for serializing and parsing Connect envelopes
// Remove protobufjs/minimal, use only @bufbuild/protobuf for message serialization

export enum Flag {
  NONE = 0,
  COMPRESSED = 1,
  END_STREAM = 2,
}

export interface Envelope {
  flags: Flag;
  data: Uint8Array;
}

/**
 * Serializes an envelope to a Uint8Array
 *
 * @param envelope The envelope to serialize
 * @returns A Uint8Array containing the serialized envelope
 */
export function serializeEnvelope(envelope: Envelope): Uint8Array {
  const { flags, data } = envelope;
  const result = new Uint8Array(5 + data.length);

  // Write flags (1 byte)
  result[0] = flags;

  // Write data length (4 bytes, big-endian)
  new DataView(result.buffer).setUint32(1, data.length, false);

  // Copy data
  result.set(data, 5);

  return result;
}

/**
 * Parses an envelope from a Uint8Array
 *
 * @param buffer The buffer containing the serialized envelope
 * @returns The parsed envelope
 * @throws Error if the buffer is too short or invalid
 */
export function parseEnvelope(buffer: Uint8Array): Envelope {
  if (buffer.length < 5) {
    throw new Error(
      `Envelope too short: expected at least 5 bytes, got ${buffer.length}`,
    );
  }

  // Read flags (1 byte)
  const flags = buffer[0] as Flag;

  // Read data length (4 bytes, big-endian)
  const dataLength = new DataView(
    buffer.buffer,
    buffer.byteOffset + 1,
    4,
  ).getUint32(0, false);

  // Validate total length
  if (buffer.length < 5 + dataLength) {
    throw new Error(
      `Envelope too short: expected ${5 + dataLength} bytes, got ${buffer.length}`,
    );
  }

  // Extract data
  const data = buffer.slice(5, 5 + dataLength);

  return { flags, data };
}

/**
 * Converts a Uint8Array to a hex string
 *
 * @param buffer The buffer to convert
 * @returns A hex string representation of the buffer
 */
export function toHex(buffer: Uint8Array): string {
  return Array.from(buffer)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

/**
 * Converts a Uint8Array to a string
 *
 * @param buffer The buffer to convert
 * @returns A string representation of the buffer
 */
export function uint8ArrayToString(buffer: Uint8Array): string {
  return new TextDecoder().decode(buffer);
}

// All custom message encoding/decoding logic removed.
