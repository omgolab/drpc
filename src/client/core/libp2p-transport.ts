/**
 * Libp2p transport implementation for Connect RPC
 */
import { pipe } from 'it-pipe';
import { ConnectError, Code } from "@connectrpc/connect";
// Correct imports for create
import { createClientMethodSerializers } from "@connectrpc/connect/protocol";
import { create } from "@bufbuild/protobuf";
import { hexDump, analyzeBuffer } from "./debug-helpers"; // Add the debug helpers import
import {
    Flag,
    serializeEnvelope,
    parseEnvelope,
    isEndStreamFlag,
    uint8ArrayToString
} from "./envelopes";
import { toHex } from "./envelope";
import {
    UnaryResponse,
    StreamResponse,
    ContextValues,
    CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE,
    CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE,
    CONNECT_CONTENT_TYPE,
    GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE,
    GRPC_PROTO_WITH_UNARY_CONTENT_TYPE,
    unaryContentTypes,
    streamingContentTypes,
    UnaryContentType,
    DRPCOptions
} from "./types";
import { LogLevel } from "./logger";
import {
    concatUint8Arrays,
    prepareInitialHeaderPayload,
    extractDialTargetFromMultiaddr,
    connectCodeFromString
} from "./utils";
import type {
    DescMessage,
    DescMethodUnary,
    DescMethodStreaming,
    MessageInitShape,
    MessageShape
} from "@bufbuild/protobuf";
import { Transport } from "@connectrpc/connect";

// Configuration
const PROTOCOL_ID = "/drpc-webstream/1.0.0";

// Special flag values
const CONNECT_ES_END_STREAM_FLAG = 0b00000010; // 2 in decimal (connect-es standard)
const LEGACY_END_STREAM_FLAG = 128; // Our original value 

/**
 * Helper to check if a flag has the END_STREAM bit set
 * Supporting multiple possible flag values
 */
function checkEndStreamFlag(flagValue: number): boolean {
    return (
        (flagValue & CONNECT_ES_END_STREAM_FLAG) === CONNECT_ES_END_STREAM_FLAG ||
        (flagValue & LEGACY_END_STREAM_FLAG) === LEGACY_END_STREAM_FLAG
    );
}

/**
 * Creates a libp2p transport for Connect RPC
 */
export function createLibp2pTransport(libp2p: any, ma: any, options: DRPCOptions): Transport {
    // Use provided logger
    const logger = options.logger.createChildLogger({ contextName: "Libp2p-Transport" });
    return {
        /**
         * Unary RPC implementation
         */
        async unary<I extends DescMessage, O extends DescMessage>(
            method: DescMethodUnary<I, O>,
            signal: AbortSignal | undefined,
            timeoutMs: number | undefined,
            header: HeadersInit | undefined,
            message: MessageInitShape<I>,
            contextValues?: ContextValues
        ): Promise<UnaryResponse<I, O>> {
            logger.debug(`Unary call: ${method.name}`);
            const { serialize, parse: deserialize } = createClientMethodSerializers(method, true); // true for binary
            let p2pStream: any;

            const abortController = new AbortController();
            const linkedSignal = signal
                ? (() => {
                    const onAbort = () => abortController.abort();
                    signal.addEventListener("abort", onAbort);
                    abortController.signal.addEventListener("abort", () => signal.removeEventListener("abort", onAbort));
                    return abortController.signal;
                })()
                : abortController.signal;

            try {
                if (linkedSignal.aborted) {
                    throw new ConnectError("Request aborted before sending", Code.Canceled);
                }

                const targetMa = extractDialTargetFromMultiaddr(ma);
                logger.debug(`Unary: Dialing ${targetMa.toString()} with protocol ${PROTOCOL_ID}`);
                p2pStream = await libp2p.dialProtocol(targetMa, PROTOCOL_ID, { signal: linkedSignal });
                logger.debug(`Unary: Successfully established stream to ${targetMa.toString()}`);

                // Prepare and serialize the request
                const contentType: UnaryContentType = options.unaryContentType ?? CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE;

                // Type system ensures content type is valid, but we keep runtime check as a safety measure
                if (!unaryContentTypes.includes(contentType)) {
                    throw new ConnectError(
                        `Invalid content type for unary call: ${contentType}. Must be one of: ${unaryContentTypes.join(', ')}`,
                        Code.InvalidArgument
                    );
                }

                const initialHeader = prepareInitialHeaderPayload(method, contentType);
                const requestMessage = create(method.input, message);
                const serializedPayload = serialize(requestMessage);

                // Prepare the complete request in a single buffer
                const requestData = concatUint8Arrays([
                    initialHeader,
                    serializedPayload
                ]);

                // Send the request and close the write side
                await pipe([requestData], p2pStream.sink);
                await p2pStream.closeWrite();

                logger.debug("Unary: Request sent, reading response");

                // Receive and process response
                const responseChunks: Uint8Array[] = [];

                try {
                    // Read all response data
                    for await (const chunk of p2pStream.source) {
                        if (linkedSignal.aborted) {
                            logger.debug("Unary: Aborted during server message receiving");
                            throw new ConnectError("Unary response aborted by client", Code.Canceled);
                        }
                        // Make sure we're working with a standard Uint8Array
                        const chunkArray = new Uint8Array(chunk.length);
                        chunkArray.set(new Uint8Array(chunk.buffer, chunk.byteOffset, chunk.length));
                        responseChunks.push(chunkArray);
                    }

                    if (logger.isMinLevel(LogLevel.DEBUG)) {
                        const totalBytes = responseChunks.reduce((s, c) => s + c.length, 0);
                        console.debug(`Unary: Received all data from server (${totalBytes} bytes total)`);
                    }
                } catch (readErr: any) {
                    logger.error(`Unary: Error reading response: ${readErr}`);
                    throw readErr;
                }

                // No data received
                if (responseChunks.length === 0) {
                    throw new ConnectError("Empty response received from server", Code.DataLoss);
                }

                // Concatenate all received chunks
                const responseBuffer = concatUint8Arrays(responseChunks);

                // Attempt to parse the response in various formats
                let responseMessage: MessageShape<O> | undefined;

                // Determine the appropriate parsing strategy based on content type
                const isConnectUnaryProto = contentType === CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE;
                const isConnectUnaryJson = contentType === CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE;
                const isGrpcWebProto = contentType === GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE;
                const isGrpcProto = contentType === GRPC_PROTO_WITH_UNARY_CONTENT_TYPE;

                // For unary responses with unary-specific content types, first try to parse as a raw response (no envelope)
                if (isConnectUnaryProto || isConnectUnaryJson) {
                    try {
                        responseMessage = deserialize(responseBuffer);
                        logger.debug("Unary: Successfully decoded raw response");
                    } catch (decodeErr) {
                        logger.debug(`Unary: Failed to decode as raw response: ${decodeErr}`);

                        // For JSON content type, try parsing as JSON
                        if (isConnectUnaryJson) {
                            try {
                                const responseText = new TextDecoder().decode(responseBuffer);
                                if (responseText.startsWith('{')) {
                                    responseMessage = JSON.parse(responseText) as any;
                                    logger.debug("Unary: Successfully decoded JSON response");
                                }
                            } catch (jsonErr) {
                                logger.debug(`Unary: Failed to decode as JSON: ${jsonErr}`);
                            }
                        }
                    }
                }

                // If still no message, try to parse as an envelope (required for gRPC formats)
                if (!responseMessage && (isGrpcWebProto || isGrpcProto)) {
                    logger.debug("Unary: Attempting to parse unary response as envelope");
                    try {
                        const parsedEnvelope = parseEnvelope(responseBuffer);
                        if (parsedEnvelope.envelope) {
                            const envelope = parsedEnvelope.envelope;

                            // If it's a data envelope, try to parse the payload
                            if (envelope.flags === Flag.NONE && envelope.data.length > 0) {
                                responseMessage = deserialize(envelope.data);
                                logger.debug("Unary: Successfully decoded envelope data response");
                            }
                            // Check for error data in the envelope
                            else if (envelope.data && envelope.data.length > 0) {
                                try {
                                    const errorText = uint8ArrayToString(envelope.data);
                                    if (errorText.startsWith('{') && errorText.includes('"code"')) {
                                        const errorData = JSON.parse(errorText);
                                        throw new ConnectError(
                                            errorData.message || "Unknown error from server",
                                            connectCodeFromString(errorData.code) || Code.Unknown
                                        );
                                    }
                                } catch (e) {
                                    if (e instanceof ConnectError) throw e;
                                }
                            }
                        }
                    } catch (envelopeErr) {
                        logger.debug(`Unary: Failed to parse as envelope: ${envelopeErr}`);

                        // Last resort: try to parse as JSON error response
                        try {
                            const responseText = new TextDecoder().decode(responseBuffer);
                            if (responseText.startsWith('{') && responseText.includes('"code"')) {
                                const errorData = JSON.parse(responseText);
                                throw new ConnectError(
                                    errorData.message || "Unknown error from server",
                                    connectCodeFromString(errorData.code) || Code.Unknown
                                );
                            }
                        } catch (e) {
                            if (e instanceof ConnectError) throw e;
                        }
                    }
                }

                // At this point, if we still don't have a responseMessage, we should error
                if (!responseMessage) {
                    const bufHex = responseBuffer ? toHex(responseBuffer.subarray(0, Math.min(responseBuffer.byteLength, 32))) : '';
                    throw new ConnectError(
                        `Failed to decode response in any format. Received ${responseBuffer?.byteLength || 0} bytes (prefix: ${bufHex}...)`,
                        Code.DataLoss
                    );
                }

                return {
                    stream: false,
                    service: method.parent,
                    method: method,
                    header: new Headers(),
                    trailer: new Headers(),
                    message: responseMessage,
                };
            } catch (error: any) {
                logger.error(`Unary call error: ${error.message || error}`, error);
                if (!abortController.signal.aborted) abortController.abort(error);

                if (p2pStream && typeof p2pStream.abort === 'function' && !p2pStream.stat?.status?.includes('CLOSED')) {
                    try {
                        logger.debug("Unary: Aborting p2pStream in outer catch due to error:", error);
                        p2pStream.abort(error);
                    } catch (abortErr) {
                        logger.error("Unary: Error aborting p2pStream in outer catch:", abortErr);
                    }
                }
                if (error instanceof ConnectError) throw error;
                throw new ConnectError(
                    `Unary libp2p transport error: ${error.message || String(error)}`,
                    Code.Internal, // Default to internal if not a ConnectError already
                    undefined, undefined, error
                );
            } finally {
                if (p2pStream && typeof p2pStream.close === 'function' && !p2pStream.stat?.status?.includes('CLOSED')) {
                    try {
                        logger.debug("Unary: Ensuring p2pStream is closed in finally block.");
                        await p2pStream.close();
                    } catch (closeErr) {
                        logger.error("Unary: Error closing p2pStream in finally:", closeErr);
                    }
                }
            }
        },

        /**
         * Streaming RPC implementation
         * Following the successful pattern from the connect-client.ts example:
         * 1. Gather all client messages first
         * 2. Send all at once (headers + envelopes)
         * 3. Close the write side explicitly
         * 4. Then process responses
         */
        async stream<I extends DescMessage, O extends DescMessage>(
            method: DescMethodStreaming<I, O>,
            signal: AbortSignal | undefined,
            timeoutMs: number | undefined,
            header: HeadersInit | undefined,
            input: AsyncIterable<MessageInitShape<I>>,
            contextValues?: ContextValues
        ): Promise<StreamResponse<I, O>> {
            logger.debug(`Stream call: ${method.name}`);
            const { serialize, parse: deserialize } = createClientMethodSerializers(method, true); // true for binary format
            let p2pStream: any; // To store the libp2p stream for access in finally

            try {
                const targetMa = extractDialTargetFromMultiaddr(ma);
                logger.debug(`Dialing ${targetMa.toString()} with protocol ${PROTOCOL_ID} for stream`);

                // Create abort controller and linked signal
                const abortController = new AbortController();
                const linkedSignal = signal
                    ? (() => {
                        const onAbort = () => abortController.abort();
                        signal.addEventListener("abort", onAbort);
                        abortController.signal.addEventListener("abort", () => signal.removeEventListener("abort", onAbort));
                        return abortController.signal;
                    })()
                    : abortController.signal;

                if (linkedSignal.aborted) {
                    throw new ConnectError("Request aborted before sending", Code.Canceled);
                }

                // Dial the peer
                p2pStream = await libp2p.dialProtocol(targetMa, PROTOCOL_ID, { signal: linkedSignal });
                logger.debug(`Successfully established stream to ${targetMa.toString()}`);

                // Following the pattern from connect-client.ts:
                // 1. First collect all client messages
                const contentType = options?.streamingContentType ?? CONNECT_CONTENT_TYPE;

                // Type system ensures content type is valid, but we keep runtime check as a safety measure
                if (!streamingContentTypes.includes(contentType)) {
                    throw new ConnectError(
                        `Invalid content type for streaming call: ${contentType}. Must be one of: ${streamingContentTypes.join(', ')}`,
                        Code.InvalidArgument
                    );
                }

                const initialHeader = prepareInitialHeaderPayload(method, contentType);

                // Prepare header + all request messages at once
                const buffers: Uint8Array[] = [initialHeader];

                // Gather all client messages
                try {
                    for await (const msgInit of input) {
                        if (linkedSignal.aborted) {
                            throw new ConnectError("Client stream aborted during message collection", Code.Canceled);
                        }

                        const requestMessage = create(method.input, msgInit);
                        const serializedPayload = serialize(requestMessage);
                        const envelope = { flags: Flag.NONE, data: serializedPayload };
                        const serializedEnvelope = serializeEnvelope(envelope);

                        buffers.push(serializedEnvelope);
                        logger.debug(`Prepared client message, size: ${serializedPayload.length} bytes`);
                    }
                } catch (e: any) {
                    logger.error("Error collecting client messages:", e);
                    throw e;
                }

                if (logger.isMinLevel(LogLevel.DEBUG)) {
                    const totalBytes = buffers.reduce((sum, b) => sum + b.length, 0);
                    logger.debug(`Sending ${buffers.length - 1} client messages, total size: ${totalBytes} bytes`);
                }

                // 2. Send all buffers in a single pipe operation
                await pipe([concatUint8Arrays(buffers)], p2pStream.sink);

                // 3. Close the write side of the stream to signal end of client messages
                // This is critical for bidirectional streaming to work properly
                await p2pStream.closeWrite();
                logger.debug("Closed write side after sending all client messages");

                // 4. Now implement the response handling as an AsyncIterable
                const createResponseAsyncIterable = async function* (): AsyncIterable<MessageShape<O>> {
                    // Instead of processing chunks one by one, collect all chunks first
                    // This approach matches the successful pattern in the working prototype
                    let responseBuffer: Uint8Array | null = null;
                    logger.debug("Starting to collect all response chunks from server");

                    try {
                        // Following the working prototype pattern, collect all chunks then process at once
                        const chunks: Uint8Array[] = [];

                        // Print extra debug info about the p2pStream.source
                        logger.debug(`Stream source type: ${typeof p2pStream.source}`);
                        logger.debug(`Stream source properties: ${Object.keys(p2pStream.source).join(', ')}`);
                        logger.debug(`Stream connection status: ${p2pStream.stat?.status || 'unknown'}`);

                        // Use a timeout to ensure we don't hang forever waiting for data
                        const timeout = 5000; // 5 seconds timeout
                        const startTime = Date.now();

                        for await (const chunk of p2pStream.source) {
                            if (linkedSignal.aborted) {
                                throw new ConnectError("Server stream aborted by client", Code.Canceled);
                            }

                            // Examine the chunk in great detail
                            logger.debug(`Raw chunk type: ${typeof chunk}`);
                            logger.debug(`Raw chunk is array? ${Array.isArray(chunk)}`);
                            logger.debug(`Raw chunk is Uint8Array? ${chunk instanceof Uint8Array}`);
                            logger.debug(`Raw chunk properties: ${Object.keys(chunk).join(', ')}`);

                            // Special handling for the object with 'bufs' property
                            if (chunk && typeof chunk === 'object' && 'bufs' in chunk) {
                                logger.debug(`Found 'bufs' property in chunk with length: ${chunk.bufs?.length || 0}`);

                                // Check if bufs is an array and has elements
                                if (Array.isArray(chunk.bufs) && chunk.bufs.length > 0) {
                                    // Process each buffer in the bufs array
                                    for (const buf of chunk.bufs) {
                                        if (buf && buf.length > 0) {
                                            logger.debug(`Processing buf from bufs array with length: ${buf.length}`);

                                            // Convert to Uint8Array if needed
                                            const bufArray = buf instanceof Uint8Array ? buf : new Uint8Array(buf.buffer || buf);

                                            if (bufArray.length > 0) {
                                                const maxBytesToLog = Math.min(bufArray.length, 32);
                                                logger.debug(`Buf data (${maxBytesToLog} bytes): ${toHex(bufArray.subarray(0, maxBytesToLog))}`);
                                                chunks.push(bufArray);
                                            }
                                        }
                                    }
                                    continue; // Skip regular chunk processing for this type
                                }
                            }

                            // Regular handling for Uint8Array or buffer-like chunks
                            let chunkArray: Uint8Array;

                            if (chunk instanceof Uint8Array) {
                                chunkArray = chunk;
                            } else if (chunk && chunk.buffer) {
                                chunkArray = new Uint8Array(chunk.buffer);
                            } else if (chunk && typeof chunk === 'object' && 'length' in chunk) {
                                // Try to convert array-like to Uint8Array
                                try {
                                    chunkArray = new Uint8Array(chunk);
                                } catch (e) {
                                    logger.debug(`Failed to convert chunk to Uint8Array: ${e}`);
                                    return; // Skip this chunk
                                }
                            } else {
                                logger.debug(`Unprocessable chunk type, skipping`);
                                continue;
                            }

                            logger.debug(`Received chunk from server - length: ${chunkArray.length} bytes`);

                            // Print the hex representation of the chunk
                            if (chunkArray.length > 0) {
                                const maxBytesToLog = Math.min(chunkArray.length, 32);
                                logger.debug(`Chunk data (${maxBytesToLog} bytes): ${toHex(chunkArray.subarray(0, maxBytesToLog))}`);
                                chunks.push(chunkArray);
                            } else {
                                logger.debug(`Empty chunk received, skipping`);
                            }

                            // Check if we've been waiting too long
                            if (Date.now() - startTime > timeout) {
                                logger.warn(`Timeout reached after collecting ${chunks.length} chunks. Proceeding with processing.`);
                                break;
                            }
                        }

                        logger.debug(`Collected ${chunks.length} chunks from server`);

                        // If we didn't receive any data
                        if (chunks.length === 0) {
                            logger.debug("No data received from server stream");
                            return;
                        }

                        // Combine all chunks into a single buffer
                        responseBuffer = concatUint8Arrays(chunks);
                        logger.debug(`Combined response buffer length: ${responseBuffer.length} bytes`);

                        // Debug hex representation if enabled
                        if (logger.isMinLevel(LogLevel.DEBUG)) {
                            const maxBytesToLog = Math.min(responseBuffer.length, 100);
                            logger.debug(`Response buffer prefix (${maxBytesToLog} bytes shown): ${toHex(responseBuffer.subarray(0, maxBytesToLog))}`);
                            if (responseBuffer.length > 0) {
                                logger.debug(`First byte: ${responseBuffer[0]} (${responseBuffer[0].toString(16)})`);
                                if (responseBuffer.length >= 5) {
                                    const view = new DataView(responseBuffer.buffer, responseBuffer.byteOffset + 1, 4);
                                    const dataLength = view.getUint32(0, false); // false = big-endian
                                    logger.debug(`Data length from first envelope: ${dataLength}`);
                                }
                            }
                        }

                        // Process the complete response buffer
                        let currentOffset = 0;
                        let endStreamReceived = false;

                        // Continue parsing envelopes until we've consumed the entire buffer
                        while (currentOffset < responseBuffer.length) {
                            // Make sure we have enough bytes for the envelope header (1 flag byte + 4 length bytes)
                            if (currentOffset + 5 > responseBuffer.length) {
                                logger.debug(`Buffer too short for envelope header at offset ${currentOffset}`);
                                break;
                            }

                            try {
                                const remainingBuffer = responseBuffer.subarray(currentOffset);
                                const parseResult = parseEnvelope(remainingBuffer);

                                // If parsing failed, skip this portion
                                if (!parseResult.envelope || parseResult.bytesRead === 0) {
                                    logger.debug(`Failed to parse envelope at offset ${currentOffset}`);
                                    // Advance by 1 byte to try to find a valid envelope
                                    currentOffset += 1;
                                    continue;
                                }

                                const { envelope, bytesRead } = parseResult;
                                const isEndStream = isEndStreamFlag(envelope.flags);

                                logger.debug(`Parsed envelope at offset ${currentOffset}: flags=${envelope.flags}, data length=${envelope.data.length}, isEndStream=${isEndStream}`);

                                if (isEndStream) {
                                    endStreamReceived = true;
                                    logger.debug(`Received END_STREAM flag with value ${envelope.flags}`);

                                    // Handle potential error data with END_STREAM
                                    if (envelope.data && envelope.data.length > 0) {
                                        try {
                                            const errorText = uint8ArrayToString(envelope.data);
                                            logger.debug(`END_STREAM data: ${errorText}`);
                                            if (errorText.startsWith('{')) {
                                                const errorData = JSON.parse(errorText);
                                                if (errorData.code || errorData.message) {
                                                    throw new ConnectError(
                                                        errorData.message || "Unknown error",
                                                        connectCodeFromString(errorData.code) || Code.Unknown
                                                    );
                                                }
                                            }
                                        } catch (err) {
                                            if (err instanceof ConnectError) throw err;
                                        }
                                    }
                                } else if (envelope.data && envelope.data.length > 0) {
                                    // Regular data envelope
                                    try {
                                        const response = deserialize(envelope.data);
                                        logger.debug(`Successfully parsed response: ${JSON.stringify(response)}`);
                                        yield response;
                                    } catch (decodeErr) {
                                        logger.error(`Failed to decode response data: ${decodeErr}`);
                                    }
                                } else {
                                    logger.debug(`Envelope with flags=${envelope.flags} contains empty data`);
                                }

                                // Move to the next envelope
                                currentOffset += bytesRead;
                            } catch (parseErr) {
                                logger.error(`Error parsing envelope: ${parseErr}`);
                                // Try to advance past this problematic part
                                currentOffset += 1;
                            }
                        }

                        if (!endStreamReceived) {
                            logger.debug("Stream ended without receiving END_STREAM flag");
                        }
                    } catch (error: any) {
                        logger.error(`Error in response stream handling: ${error}`);

                        if (error instanceof ConnectError)
                            throw error;

                        throw new ConnectError(
                            `Error receiving server stream: ${error.message}`,
                            Code.Unknown,
                            undefined,
                            undefined,
                            error
                        );
                    } finally {
                        logger.debug("Response async iterable finalizing.");
                    }
                };

                return {
                    stream: true,
                    service: method.parent,
                    method: method,
                    header: new Headers(),
                    trailer: new Headers(),
                    message: createResponseAsyncIterable(),
                };
            } catch (error: any) {
                logger.error(`Stream call error: ${error}`);

                if (error instanceof ConnectError)
                    throw error;

                throw new ConnectError(
                    `Failed to make streaming call: ${error.message}`,
                    Code.Unknown,
                    undefined,
                    undefined,
                    error
                );
            }
        }
    };
}
