export enum Flag {
  NONE = 0,
  COMPRESSED = 1,
  END_STREAM = 2,
}

export interface Envelope {
  flags: Flag;
  data: Uint8Array;
}

const HEADER_LENGTH = 5; // 1 byte for flags, 4 bytes for data length

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
 * Attempts to parse an envelope from the beginning of the buffer.
 *
 * @param buffer The buffer to parse from.
 * @param offset The offset in the buffer to start parsing from.
 * @returns An object containing the parsed envelope (or null if not enough data)
 *          and the number of bytes consumed from the buffer for this attempt.
 */
export function parseEnvelope(
  buffer: Uint8Array,
  offset: number = 0,
): ParsedEnvelopeResult {
  if (buffer.byteLength < offset + HEADER_LENGTH) {
    // Not enough data for header
    return { envelope: null, bytesRead: 0 };
  }

  const view = new DataView(
    buffer.buffer,
    buffer.byteOffset + offset,
    HEADER_LENGTH,
  );
  const flags = view.getUint8(0) as Flag;
  const dataLength = view.getUint32(1, false); // false for big-endian

  if (buffer.byteLength < offset + HEADER_LENGTH + dataLength) {
    // Not enough data for the full message
    return { envelope: null, bytesRead: 0 };
  }

  const data = buffer.subarray(
    offset + HEADER_LENGTH,
    offset + HEADER_LENGTH + dataLength,
  );
  const envelope: Envelope = { flags, data };
  const bytesRead = HEADER_LENGTH + dataLength;

  return { envelope, bytesRead };
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
