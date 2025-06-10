/**
 * Enhanced envelope processing for bidirectional streaming.
 * This implementation is based on the successful approach from the working prototype
 * in experiments/web-stream/bridge-final.test.ts and supports both standard and legacy flag values.
 */

// Flag enum with both standard connect-es and legacy values
export enum Flag {
  NONE = 0,
  COMPRESSED = 1,
  END_STREAM = 2, // Standard Connect-ES value
  LEGACY_END_STREAM = 128, // Legacy value for compatibility
}

export interface Envelope {
  flags: Flag | number; // Allow any number for flexibility with server responses
  data: Uint8Array;
}

// Fixed header length: 1 byte for flags + 4 bytes for data length
const HEADER_LENGTH = 5;

/**
 * Serializes an envelope to a Uint8Array
 *
 * @param envelope The envelope to serialize
 * @returns A Uint8Array containing the serialized envelope
 */
export function serializeEnvelope(envelope: Envelope): Uint8Array {
  const dataLength = envelope.data.byteLength;
  const buffer = new Uint8Array(HEADER_LENGTH + dataLength);
  const view = new DataView(buffer.buffer);

  view.setUint8(0, envelope.flags);
  view.setUint32(1, dataLength, false); // false for big-endian

  buffer.set(envelope.data, HEADER_LENGTH);
  return buffer;
}

export interface ParsedEnvelopeResult {
  envelope: Envelope | null;
  bytesRead: number;
}

/**
 * Attempts to parse an envelope from the buffer.
 * Returns both the parsed envelope and the number of bytes read.
 *
 * @param buffer The buffer to parse from.
 * @param offset The offset in the buffer to start parsing from.
 * @returns An object containing the parsed envelope and bytes read.
 */
export function parseEnvelope(
  buffer: Uint8Array,
  offset: number = 0,
): ParsedEnvelopeResult {
  // Ensure we have at least the header bytes
  if (buffer.byteLength < offset + HEADER_LENGTH) {
    return { envelope: null, bytesRead: 0 };
  }

  // Read flags (1 byte) and data length (4 bytes)
  const view = new DataView(
    buffer.buffer,
    buffer.byteOffset + offset,
    HEADER_LENGTH,
  );
  const flags = view.getUint8(0);
  const dataLength = view.getUint32(1, false); // false for big-endian

  // Ensure we have the full message
  if (buffer.byteLength < offset + HEADER_LENGTH + dataLength) {
    return { envelope: null, bytesRead: 0 };
  }

  // Extract data
  const data = buffer.subarray(
    offset + HEADER_LENGTH,
    offset + HEADER_LENGTH + dataLength,
  );

  // Return the envelope and bytes read
  return {
    envelope: { flags, data },
    bytesRead: HEADER_LENGTH + dataLength,
  };
}

/**
 * Checks if a flag value includes END_STREAM flag (either standard or legacy)
 */
export function isEndStreamFlag(flags: number): boolean {
  return flags === Flag.END_STREAM || flags === Flag.LEGACY_END_STREAM;
}

/**
 * Converts a Uint8Array to a string
 */
export function uint8ArrayToString(buffer: Uint8Array): string {
  return new TextDecoder().decode(buffer);
}

/**
 * Converts a Uint8Array to a hex string
 */
export function toHex(buffer: Uint8Array): string {
  return Array.from(buffer)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
