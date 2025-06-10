/**
 * Streaming RPC handler for libp2p transport
 */
import { pipe } from "it-pipe";
import { ConnectError, Code } from "@connectrpc/connect";
import {
    createClientMethodSerializers,
    createLinkedAbortController,
    encodeEnvelopes,
} from "@connectrpc/connect/protocol";
import { create } from "@bufbuild/protobuf";
import {
    Flag,
    parseEnvelope,
    isEndStreamFlag,
    uint8ArrayToString,
    toHex,
} from "../envelopes";
import { concatUint8Arrays } from "../vendor-utils";
import {
    StreamResponse,
    ContextValues,
    CONNECT_CONTENT_TYPE,
    streamingContentTypes,
    StreamingContentType,
} from "../types";
import { ILogger, LogLevel } from "../logger";
import {
    prepareInitialHeaderPayload,
    extractDialTargetFromMultiaddr,
    connectCodeFromString,
} from "../utils";
import type {
    DescMessage,
    DescMethodStreaming,
    MessageInitShape,
    MessageShape,
} from "@bufbuild/protobuf";
import { Multiaddr } from "@multiformats/multiaddr";
import { Libp2p } from "libp2p";

export interface StreamHandlerDependencies {
    libp2p: Libp2p;
    ma: Multiaddr;
    PROTOCOL_ID: string;
    logger: ILogger;
    options: {
        streamingContentType?: StreamingContentType;
    };
}

// Serializer cache to avoid repeated createClientMethodSerializers calls
// Using any type to avoid complex generic constraints for now
const serializerCache = new Map<string, any>();

/**
 * Handle streaming RPC calls
 * Following the successful pattern from the connect-client.ts example:
 * 1. Gather all client messages first
 * 2. Send all at once (headers + envelopes)
 * 3. Close the write side explicitly
 * 4. Then process responses
 */
export async function handleStream<I extends DescMessage, O extends DescMessage>(
    deps: StreamHandlerDependencies,
    method: DescMethodStreaming<I, O>,
    signal: AbortSignal | undefined,
    timeoutMs: number | undefined,
    header: HeadersInit | undefined,
    input: AsyncIterable<MessageInitShape<I>>,
    contextValues?: ContextValues,
): Promise<StreamResponse<I, O>> {
    const { libp2p, ma, PROTOCOL_ID, logger, options } = deps;

    logger.debug(`Stream call: ${method.name}`);
    // Get requested content type, defaulting to protobuf if not specified
    const contentType = options?.streamingContentType ?? CONNECT_CONTENT_TYPE;

    // Determine if we should use binary format based on content type
    // Use binary format for proto content types, JSON format for JSON content types
    const useBinaryFormat = !contentType.includes("json");
    logger.debug(
        `Stream call using ${useBinaryFormat ? "binary" : "JSON"} format for content type: ${contentType}`,
    );

    // Use cached serializers for performance
    const cacheKey = `${method.parent.typeName}.${method.name}:${useBinaryFormat}`;
    let serializers = serializerCache.get(cacheKey);
    if (!serializers) {
        serializers = createClientMethodSerializers(
            method,
            useBinaryFormat,
        );
        serializerCache.set(cacheKey, serializers);
        logger.debug(`Created and cached serializers for ${cacheKey}`);
    } else {
        logger.debug(`Using cached serializers for ${cacheKey}`);
    }
    const { serialize, parse: deserialize } = serializers;
    let p2pStream: any; // To store the libp2p stream for access in finally

    try {
        const targetMa = extractDialTargetFromMultiaddr(ma);
        logger.debug(
            `Dialing ${targetMa.toString()} with protocol ${PROTOCOL_ID} for stream`,
        );

        // Use Connect's createLinkedAbortController for better abort handling
        const abortController = createLinkedAbortController(signal);
        const linkedSignal = abortController.signal;

        if (linkedSignal.aborted) {
            throw new ConnectError(
                "Request aborted before sending",
                Code.Canceled,
            );
        }

        // Dial the peer
        p2pStream = await libp2p.dialProtocol(targetMa, PROTOCOL_ID, {
            signal: linkedSignal,
        });
        logger.debug(
            `Successfully established stream to ${targetMa.toString()}`,
        );

        // Following the pattern from connect-client.ts:
        // 1. First collect all client messages

        // Type system ensures content type is valid, but we keep runtime check as a safety measure
        if (!streamingContentTypes.includes(contentType)) {
            throw new ConnectError(
                `Invalid content type for streaming call: ${contentType}. Must be one of: ${streamingContentTypes.join(", ")}`,
                Code.InvalidArgument,
            );
        }

        const initialHeader = prepareInitialHeaderPayload(method, contentType);

        // Prepare header + all request messages at once
        const buffers: Uint8Array[] = [initialHeader];

        // Gather client messages and their payloads
        const payloads: Uint8Array[] = [];

        try {
            for await (const msgInit of input) {
                if (linkedSignal.aborted) {
                    throw new ConnectError(
                        "Client stream aborted during message collection",
                        Code.Canceled,
                    );
                }

                const requestMessage = create(method.input, msgInit);
                const serializedPayload = serialize(requestMessage);

                // Collect payloads to encode together later
                payloads.push(serializedPayload);
                logger.debug(
                    `Prepared client message, size: ${serializedPayload.length} bytes`,
                );
            }
        } catch (e: any) {
            logger.error("Error collecting client messages:", e);
            throw e;
        }

        // Create envelopes for all messages in one go using Connect's encodeEnvelopes
        if (payloads.length > 0) {
            const envelopedData = encodeEnvelopes(
                ...payloads.map((p) => ({ flags: Flag.NONE, data: p })),
            );
            buffers.push(envelopedData);
        }

        if (logger.isMinLevel(LogLevel.DEBUG)) {
            const totalBytes = buffers.reduce((sum, b) => sum + b.length, 0);
            logger.debug(
                `Sending ${payloads.length} client messages, total size: ${totalBytes} bytes`,
            );
        }

        // 2. Send all buffers in a single pipe operation
        await pipe([concatUint8Arrays(buffers)], p2pStream.sink);

        // 3. Close the write side of the stream to signal end of client messages
        // This is critical for bidirectional streaming to work properly
        await p2pStream.closeWrite();
        logger.debug("Closed write side after sending all client messages");

        // 4. Now implement the response handling as an AsyncIterable
        const createResponseAsyncIterable = async function* (): AsyncIterable<
            MessageShape<O>
        > {
            // Instead of processing chunks one by one, collect all chunks first
            // This approach matches the successful pattern in the working prototype
            let responseBuffer: Uint8Array | null = null;
            logger.debug("Starting to collect all response chunks from server");

            try {
                // Following the working prototype pattern, collect all chunks then process at once
                const chunks: Uint8Array[] = [];

                // Print extra debug info about the p2pStream.source
                logger.debug(`Stream source type: ${typeof p2pStream.source}`);
                logger.debug(
                    `Stream source properties: ${Object.keys(p2pStream.source).join(", ")}`,
                );
                logger.debug(
                    `Stream connection status: ${p2pStream.stat?.status || "unknown"}`,
                );

                // Use a timeout to ensure we don't hang forever waiting for data
                const timeout = 5000; // 5 seconds timeout
                const startTime = Date.now();

                for await (const chunk of p2pStream.source) {
                    if (linkedSignal.aborted) {
                        throw new ConnectError(
                            "Server stream aborted by client",
                            Code.Canceled,
                        );
                    }

                    // Examine the chunk in great detail
                    logger.debug(`Raw chunk type: ${typeof chunk}`);
                    logger.debug(`Raw chunk is array? ${Array.isArray(chunk)}`);
                    logger.debug(
                        `Raw chunk is Uint8Array? ${chunk instanceof Uint8Array}`,
                    );
                    logger.debug(
                        `Raw chunk properties: ${Object.keys(chunk).join(", ")}`,
                    );

                    // Special handling for the object with 'bufs' property
                    if (chunk && typeof chunk === "object" && "bufs" in chunk) {
                        logger.debug(
                            `Found 'bufs' property in chunk with length: ${chunk.bufs?.length || 0}`,
                        );

                        // Check if bufs is an array and has elements
                        if (Array.isArray(chunk.bufs) && chunk.bufs.length > 0) {
                            // Process each buffer in the bufs array
                            for (const buf of chunk.bufs) {
                                if (buf && buf.length > 0) {
                                    logger.debug(
                                        `Processing buf from bufs array with length: ${buf.length}`,
                                    );

                                    // Convert to Uint8Array if needed
                                    const bufArray =
                                        buf instanceof Uint8Array
                                            ? buf
                                            : new Uint8Array(buf.buffer || buf);

                                    if (bufArray.length > 0) {
                                        const maxBytesToLog = Math.min(bufArray.length, 32);
                                        logger.debug(
                                            `Buf data (${maxBytesToLog} bytes): ${toHex(bufArray.subarray(0, maxBytesToLog))}`,
                                        );
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
                    } else if (
                        chunk &&
                        typeof chunk === "object" &&
                        "length" in chunk
                    ) {
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

                    logger.debug(
                        `Received chunk from server - length: ${chunkArray.length} bytes`,
                    );

                    // Print the hex representation of the chunk
                    if (chunkArray.length > 0) {
                        const maxBytesToLog = Math.min(chunkArray.length, 32);
                        logger.debug(
                            `Chunk data (${maxBytesToLog} bytes): ${toHex(chunkArray.subarray(0, maxBytesToLog))}`,
                        );
                        chunks.push(chunkArray);
                    } else {
                        logger.debug(`Empty chunk received, skipping`);
                    }

                    // Check if we've been waiting too long
                    if (Date.now() - startTime > timeout) {
                        logger.warn(
                            `Timeout reached after collecting ${chunks.length} chunks. Proceeding with processing.`,
                        );
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
                logger.debug(
                    `Combined response buffer length: ${responseBuffer.length} bytes`,
                );

                // Debug hex representation if enabled
                if (logger.isMinLevel(LogLevel.DEBUG)) {
                    const maxBytesToLog = Math.min(responseBuffer.length, 100);
                    logger.debug(
                        `Response buffer prefix (${maxBytesToLog} bytes shown): ${toHex(responseBuffer.subarray(0, maxBytesToLog))}`,
                    );
                    if (responseBuffer.length > 0) {
                        logger.debug(
                            `First byte: ${responseBuffer[0]} (${responseBuffer[0].toString(16)})`,
                        );
                        if (responseBuffer.length >= 5) {
                            const view = new DataView(
                                responseBuffer.buffer,
                                responseBuffer.byteOffset + 1,
                                4,
                            );
                            const dataLength = view.getUint32(0, false); // false = big-endian
                            logger.debug(
                                `Data length from first envelope: ${dataLength}`,
                            );
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
                        logger.debug(
                            `Buffer too short for envelope header at offset ${currentOffset}`,
                        );
                        break;
                    }

                    try {
                        const remainingBuffer = responseBuffer.subarray(currentOffset);
                        const parseResult = parseEnvelope(remainingBuffer);

                        // If parsing failed, skip this portion
                        if (!parseResult.envelope || parseResult.bytesRead === 0) {
                            logger.debug(
                                `Failed to parse envelope at offset ${currentOffset}`,
                            );
                            // Advance by 1 byte to try to find a valid envelope
                            currentOffset += 1;
                            continue;
                        }

                        const { envelope, bytesRead } = parseResult;
                        const isEndStream = isEndStreamFlag(envelope.flags);

                        logger.debug(
                            `Parsed envelope at offset ${currentOffset}: flags=${envelope.flags}, data length=${envelope.data.length}, isEndStream=${isEndStream}`,
                        );

                        if (isEndStream) {
                            endStreamReceived = true;
                            logger.debug(
                                `Received END_STREAM flag with value ${envelope.flags}`,
                            );

                            // Handle potential error data with END_STREAM
                            if (envelope.data && envelope.data.length > 0) {
                                try {
                                    const errorText = uint8ArrayToString(envelope.data);
                                    logger.debug(`END_STREAM data: ${errorText}`);
                                    if (errorText.startsWith("{")) {
                                        const errorData = JSON.parse(errorText);
                                        if (errorData.code || errorData.message) {
                                            throw new ConnectError(
                                                errorData.message || "Unknown error",
                                                connectCodeFromString(errorData.code) ||
                                                Code.Unknown,
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
                                logger.debug(
                                    `Successfully parsed response: ${JSON.stringify(response)}`,
                                );
                                yield response;
                            } catch (decodeErr) {
                                logger.error(
                                    `Failed to decode response data: ${decodeErr}`,
                                );
                            }
                        } else {
                            logger.debug(
                                `Envelope with flags=${envelope.flags} contains empty data`,
                            );
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

                if (error instanceof ConnectError) throw error;

                throw new ConnectError(
                    `Error receiving server stream: ${error.message}`,
                    Code.Unknown,
                    undefined,
                    undefined,
                    error,
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

        if (error instanceof ConnectError) throw error;

        throw new ConnectError(
            `Failed to make streaming call: ${error.message}`,
            Code.Unknown,
            undefined,
            undefined,
            error,
        );
    }
}
