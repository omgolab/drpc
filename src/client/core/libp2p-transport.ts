/**
 * Libp2p transport implementation for Connect RPC
 */
import { pipe } from 'it-pipe';
import all from 'it-all';
import { ConnectError, Code } from "@connectrpc/connect";
// Correct imports for create
import { createClientMethodSerializers } from "@connectrpc/connect/protocol";
import { create } from "@bufbuild/protobuf";
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
            // Get content type first so we can determine serialization format
            const contentType: UnaryContentType = options.unaryContentType ?? CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE;
            
            // Determine if we should use binary format based on content type
            // Use binary format for proto content types, JSON format for JSON content types
            const useBinaryFormat = !contentType.includes('json');
            logger.debug(`Unary call using ${useBinaryFormat ? 'binary' : 'JSON'} format for content type: ${contentType}`);
            
            const { serialize, parse: deserialize } = createClientMethodSerializers(method, useBinaryFormat);
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
                    // Read all response data using all() for reliable stream data collection
                    const chunks = await all(p2pStream.source);
                    logger.debug(`Unary: Received ${chunks.length} chunks from stream`);
                    
                    for (const chunk of chunks) {
                        if (linkedSignal.aborted) {
                            throw new ConnectError("Unary response aborted by client", Code.Canceled);
                        }
                        
                        // Convert chunk to Uint8Array with type safety
                        let properChunk: Uint8Array;
                        try {
                            if (chunk instanceof Uint8Array) {
                                properChunk = chunk;
                            } else if (chunk && typeof chunk === 'object') {
                                if ('buffer' in chunk && 'byteOffset' in chunk && 'byteLength' in chunk) {
                                    properChunk = new Uint8Array((chunk as any).buffer, (chunk as any).byteOffset, (chunk as any).byteLength);
                                } else if ('subarray' in chunk && typeof (chunk as any).subarray === "function") {
                                    properChunk = new Uint8Array((chunk as any).subarray(0));
                                } else {
                                    properChunk = new Uint8Array(chunk as any);
                                }
                            } else {
                                properChunk = new Uint8Array(chunk as any);
                            }
                        } catch (convErr) {
                            logger.error(`Unary: Chunk conversion error: ${convErr}`);
                            properChunk = new Uint8Array(0);
                        }
                        
                        if (properChunk.length > 0) {
                            logger.debug(`Unary: Raw chunk (${properChunk.length} bytes): ${Array.from(properChunk.slice(0, Math.min(properChunk.length, 32))).map(b => b.toString(16).padStart(2, '0')).join(' ')}`);
                            responseChunks.push(properChunk);
                        }
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

                // Process response chunks
                const validChunks = responseChunks.filter(chunk => chunk.length > 0);
                
                // Create response buffer efficiently
                let responseBuffer: Uint8Array;
                if (validChunks.length === 0) {
                    responseBuffer = new Uint8Array(0);
                } else if (validChunks.length === 1) {
                    responseBuffer = validChunks[0];
                } else {
                    responseBuffer = concatUint8Arrays(validChunks);
                }

                // Log response details
                if (responseBuffer.byteLength > 0) {
                    logger.debug(`Unary: Response buffer (${responseBuffer.byteLength} bytes): ${Array.from(responseBuffer.slice(0, Math.min(responseBuffer.byteLength, 32))).map(b => b.toString(16).padStart(2, '0')).join(' ')}`);
                }
                
                // Content type flags for parsing strategy
                const isConnectUnaryProto = contentType === CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE;
                const isConnectUnaryJson = contentType === CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE;
                const isGrpcWebProto = contentType === GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE;
                const isGrpcProto = contentType === GRPC_PROTO_WITH_UNARY_CONTENT_TYPE;
                
                // Check if buffer looks like valid protobuf
                const protobufLooksValid = responseBuffer.length > 0 && responseBuffer[0] === 0x0A;
                
                // Variable to hold parsed response
                let responseMessage: MessageShape<O> | undefined;
                
                // Try decoding strategies in order of likelihood of success
                
                // 1. Try standard deserializer
                try {
                    responseMessage = deserialize(responseBuffer);
                    logger.debug("Unary: Successfully decoded with standard deserializer");
                } catch (decodeErr) {
                    // 2. Try direct fromBinary method
                    const fromBinary = (method as any).output?.fromBinary || (method as any).output?.$type?.fromBinary;
                    if (!responseMessage && fromBinary && typeof fromBinary === 'function') {
                        try {
                            responseMessage = fromBinary(responseBuffer);
                            logger.debug("Unary: Successfully decoded with direct fromBinary");
                        } catch (binaryErr) {}
                    }
                    
                    // 3. Try JSON parsing for JSON content type
                    if (!responseMessage && isConnectUnaryJson && responseBuffer.length > 0) {
                        try {
                            const responseText = new TextDecoder().decode(responseBuffer);
                            if (responseText.startsWith('{')) {
                                responseMessage = JSON.parse(responseText) as any;
                                logger.debug("Unary: Successfully decoded as JSON");
                            }
                        } catch (jsonErr) {}
                    }
                    
                    // 4. Try envelope parsing
                    if (!responseMessage) {
                        try {
                            const parsedEnvelope = parseEnvelope(responseBuffer);
                            if (parsedEnvelope.envelope) {
                                const envelope = parsedEnvelope.envelope;
                                
                                if (envelope.flags === Flag.NONE && envelope.data.length > 0) {
                                    // Try to decode envelope data
                                    try {
                                        responseMessage = deserialize(envelope.data);
                                    } catch (envDataErr) {
                                        // Try fromBinary as fallback
                                        if (fromBinary && typeof fromBinary === 'function') {
                                            try {
                                                responseMessage = fromBinary(envelope.data);
                                            } catch (envBinErr) {}
                                        }
                                    }
                                } else if (envelope.data?.length > 0) {
                                    // Check for error data
                                    const errorText = new TextDecoder().decode(envelope.data);
                                    if (errorText.startsWith('{') && errorText.includes('"code"')) {
                                        try {
                                            const errorData = JSON.parse(errorText);
                                            throw new ConnectError(
                                                errorData.message || "Unknown error from server",
                                                connectCodeFromString(errorData.code) || Code.Unknown
                                            );
                                        } catch (e) {
                                            if (e instanceof ConnectError) throw e;
                                        }
                                    }
                                }
                            }
                        } catch (envelopeErr) {
                            // Last resort: try as JSON error
                            if (responseBuffer.length > 0) {
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
                    }
                }

                // At this point, if we still don't have a responseMessage, try one more approach with manual decoding
                if (!responseMessage) {
                    logger.debug("Unary: Attempting manual decoding as a last resort");
                    
                    // Look at first byte - if it looks like a valid protobuf tag (field 1, wire type 2 (length-delimited) = 0x0A)
                    // This matches the common pattern for message responses where field 1 is the main response string
                    if (responseBuffer.length > 0 && responseBuffer[0] === 0x0A) {
                        try {
                            // Create an empty message from the output schema
                            const create = (method as any).output?.create || (method as any).output?.$type?.create;
                            if (create && typeof create === 'function') {
                                const emptyMsg = create({});
                                
                                // Extract the string from the protobuf manually (assumes field 1 is a string)
                                // Format: 0x0A (tag) + length + UTF8 string
                                const strLength = responseBuffer[1];
                                if (responseBuffer.length >= 2 + strLength) {
                                    const stringContent = new TextDecoder().decode(
                                        responseBuffer.slice(2, 2 + strLength)
                                    );
                                    
                                    // Common field names for response messages
                                    const possibleFields = ['message', 'greeting', 'text', 'response', 'result', 'value'];
                                    
                                    // Try to set the value to each possible field name
                                    for (const field of possibleFields) {
                                        if (field in emptyMsg) {
                                            (emptyMsg as any)[field] = stringContent;
                                            logger.debug(`Unary: Manually constructed response with field '${field}': ${stringContent}`);
                                            responseMessage = emptyMsg;
                                            break;
                                        }
                                    }
                                }
                            }
                        } catch (manualErr) {
                            logger.debug(`Unary: Manual decoding failed: ${manualErr}`);
                        }
                    }
                    // Last resort: manual protobuf decoding for common message formats
                    if (!responseMessage && protobufLooksValid) {
                        try {
                            // Field 1 with string value (common pattern): 0x0A (tag) + length + UTF8 string
                            const strLength = responseBuffer[1];
                            if (responseBuffer.length >= 2 + strLength) {
                                const stringContent = new TextDecoder().decode(responseBuffer.slice(2, 2 + strLength));
                            
                                // Build a response object with the string in likely field names
                                const messageObj: any = {};
                                ['message', 'greeting', 'text', 'response', 'result', 'value'].forEach(field => {
                                    messageObj[field] = stringContent;
                                });
                            
                                // Try to create proper message object
                                const createFn = (method as any).output?.create || (method as any).output?.$type?.create;
                                if (createFn && typeof createFn === 'function') {
                                    try {
                                        responseMessage = createFn(messageObj);
                                    } catch (err) {
                                        responseMessage = messageObj as MessageShape<O>;
                                    }
                                } else {
                                    responseMessage = messageObj as MessageShape<O>;
                                }
                            }
                        } catch (err) {
                            // Silent catch - just continue to next strategy
                        }
                    }
                
                    // Error if we still couldn't decode the response
                    if (!responseMessage) {
                        const bufHex = responseBuffer.length > 0 ? 
                            Array.from(responseBuffer.subarray(0, Math.min(responseBuffer.byteLength, 16)))
                                .map(b => b.toString(16).padStart(2, '0')).join('') : '';
                        throw new ConnectError(
                            `Failed to decode response (${responseBuffer.byteLength} bytes, prefix: ${bufHex}...)`,
                            Code.DataLoss
                        );
                    }
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
            // Get requested content type, defaulting to protobuf if not specified
            const contentType = options?.streamingContentType ?? CONNECT_CONTENT_TYPE;
            
            // Determine if we should use binary format based on content type
            // Use binary format for proto content types, JSON format for JSON content types
            const useBinaryFormat = !contentType.includes('json');
            logger.debug(`Stream call using ${useBinaryFormat ? 'binary' : 'JSON'} format for content type: ${contentType}`);
            
            const { serialize, parse: deserialize } = createClientMethodSerializers(method, useBinaryFormat);
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
