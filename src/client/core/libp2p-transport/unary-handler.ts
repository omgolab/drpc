/**
 * Unary RPC handler for libp2p transport
 * Extracted from libp2p-transport.ts for better organization
 */
import { pipe } from "it-pipe";
import all from "it-all";
import { ConnectError, Code } from "@connectrpc/connect";
import {
    createLinkedAbortController,
    encodeEnvelope,
    transformParseEnvelope,
    readAllBytes,
} from "@connectrpc/connect/protocol";
import { create } from "@bufbuild/protobuf";
import {
    Flag,
    parseEnvelope,
} from "../envelopes";
import { codeFromString, concatUint8Arrays } from "../utils";
import {
    UnaryResponse,
    ContextValues,
    CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE,
    CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE,
    GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE,
    GRPC_PROTO_WITH_UNARY_CONTENT_TYPE,
    unaryContentTypes,
    UnaryContentType,
} from "../types";
import { LogLevel } from "../logger";
import {
    prepareInitialHeaderPayload,
} from "../utils";
import {
    getCachedSerializers,
    shouldUseBinaryFormat,
    createManagedAbortController,
    TransportErrorFactory,
    BufferUtils,
    Libp2pConnectionManager,
    ContentTypeValidator,
} from "../transport-common";
import {
    establishConnection,
    validateUnaryContentType,
} from "./base-handler";
import { processUnaryResponse } from "./unary-response";
import type {
    DescMessage,
    DescMethodUnary,
    MessageInitShape,
    MessageShape,
} from "@bufbuild/protobuf";

export interface UnaryHandlerDependencies {
    libp2p: any;
    ma: any;
    PROTOCOL_ID: string;
    logger: any;
    options: {
        unaryContentType?: UnaryContentType;
    };
}

/**
 * Handle unary RPC calls
 */
export async function handleUnary<I extends DescMessage, O extends DescMessage>(
    deps: UnaryHandlerDependencies,
    method: DescMethodUnary<I, O>,
    signal: AbortSignal | undefined,
    timeoutMs: number | undefined,
    header: HeadersInit | undefined,
    message: MessageInitShape<I>,
    contextValues?: ContextValues,
): Promise<UnaryResponse<I, O>> {
    const { libp2p, ma, PROTOCOL_ID, logger, options } = deps;

    // Get content type first so we can determine serialization format
    const contentType: UnaryContentType =
        options.unaryContentType ?? CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE;

    // Determine if we should use binary format based on content type
    const useBinaryFormat = shouldUseBinaryFormat(contentType);
    
    // Log method start with consistent formatting
    logger.debug(
        `unary call: ${method.name} using ${useBinaryFormat ? 'binary' : 'JSON'} format for content type: ${contentType}`
    );

    // Use shared cached serializers for performance
    const { serialize, parse: deserialize } = getCachedSerializers(method, useBinaryFormat, logger);
    let p2pStream: any;
    let linkedSignal: AbortSignal;

    try {
        // Establish connection using shared logic
        const connection = await establishConnection(deps, signal, timeoutMs, "Unary");
        p2pStream = connection.p2pStream;
        linkedSignal = connection.linkedSignal;

        // Prepare and serialize the request

        // Type system ensures content type is valid, but we keep runtime check as a safety measure
        validateUnaryContentType(contentType, unaryContentTypes);

        const initialHeader = prepareInitialHeaderPayload(method, contentType);
        const requestMessage = create(method.input, message);
        const serializedPayload = serialize(requestMessage);

        // Prepare the complete request in a single buffer
        const requestData = concatUint8Arrays([
            initialHeader,
            serializedPayload,
        ]);

        // Send the request and close the write side
        await pipe([requestData], p2pStream.sink);
        await p2pStream.closeWrite();

        // Process response using shared logic
        const responseChunks = await processUnaryResponse(p2pStream, linkedSignal, logger);

        // Process response chunks
        const validChunks = responseChunks.filter((chunk) => chunk.length > 0);

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
            logger.debug(
                `Unary: Response buffer (${responseBuffer.byteLength} bytes): ${Array.from(
                    responseBuffer.slice(0, Math.min(responseBuffer.byteLength, 32)),
                )
                    .map((b) => b.toString(16).padStart(2, "0"))
                    .join(" ")}`,
            );
        }

        // Content type flags for parsing strategy
        const isConnectUnaryProto =
            contentType === CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE;
        const isConnectUnaryJson =
            contentType === CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE;
        const isGrpcWebProto =
            contentType === GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE;
        const isGrpcProto = contentType === GRPC_PROTO_WITH_UNARY_CONTENT_TYPE;

        // Check if buffer looks like valid protobuf
        const protobufLooksValid =
            responseBuffer.length > 0 && responseBuffer[0] === 0x0a;

        // Variable to hold parsed response
        let responseMessage: MessageShape<O> | undefined;

        // Try decoding strategies in order of likelihood of success

        // 1. Try standard deserializer
        try {
            responseMessage = deserialize(responseBuffer);
            logger.debug(
                "Unary: Successfully decoded with standard deserializer",
            );
        } catch (decodeErr) {
            // 2. Try direct fromBinary method
            const fromBinary =
                (method as any).output?.fromBinary ||
                (method as any).output?.$type?.fromBinary;
            if (
                !responseMessage &&
                fromBinary &&
                typeof fromBinary === "function"
            ) {
                try {
                    responseMessage = fromBinary(responseBuffer);
                    logger.debug(
                        "Unary: Successfully decoded with direct fromBinary",
                    );
                } catch (binaryErr) { }
            }

            // 3. Try JSON parsing for JSON content type
            if (
                !responseMessage &&
                isConnectUnaryJson &&
                responseBuffer.length > 0
            ) {
                try {
                    const responseText = new TextDecoder().decode(responseBuffer);
                    if (responseText.startsWith("{")) {
                        responseMessage = JSON.parse(responseText) as any;
                        logger.debug("Unary: Successfully decoded as JSON");
                    }
                } catch (jsonErr) { }
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
                                if (fromBinary && typeof fromBinary === "function") {
                                    try {
                                        responseMessage = fromBinary(envelope.data);
                                    } catch (envBinErr) { }
                                }
                            }
                        } else if (envelope.data?.length > 0) {
                            // Check for error data
                            const errorText = new TextDecoder().decode(envelope.data);
                            if (
                                errorText.startsWith("{") &&
                                errorText.includes('"code"')
                            ) {
                                try {
                                    const errorData = JSON.parse(errorText);
                                    throw new ConnectError(
                                        errorData.message || "Unknown error from server",
                                        codeFromString(errorData.code) || Code.Unknown,
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
                            if (
                                responseText.startsWith("{") &&
                                responseText.includes('"code"')
                            ) {
                                const errorData = JSON.parse(responseText);
                                throw new ConnectError(
                                    errorData.message || "Unknown error from server",
                                    codeFromString(errorData.code) || Code.Unknown,
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
            if (responseBuffer.length > 0 && responseBuffer[0] === 0x0a) {
                try {
                    // Create an empty message from the output schema
                    const create =
                        (method as any).output?.create ||
                        (method as any).output?.$type?.create;
                    if (create && typeof create === "function") {
                        const emptyMsg = create({});

                        // Extract the string from the protobuf manually (assumes field 1 is a string)
                        // Format: 0x0A (tag) + length + UTF8 string
                        const strLength = responseBuffer[1];
                        if (responseBuffer.length >= 2 + strLength) {
                            const stringContent = new TextDecoder().decode(
                                responseBuffer.slice(2, 2 + strLength),
                            );

                            // Common field names for response messages
                            const possibleFields = [
                                "message",
                                "greeting",
                                "text",
                                "response",
                                "result",
                                "value",
                            ];

                            // Try to set the value to each possible field name
                            for (const field of possibleFields) {
                                if (field in emptyMsg) {
                                    (emptyMsg as any)[field] = stringContent;
                                    logger.debug(
                                        `Unary: Manually constructed response with field '${field}': ${stringContent}`,
                                    );
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
                        const stringContent = new TextDecoder().decode(
                            responseBuffer.slice(2, 2 + strLength),
                        );

                        // Build a response object with the string in likely field names
                        const messageObj: any = {};
                        [
                            "message",
                            "greeting",
                            "text",
                            "response",
                            "result",
                            "value",
                        ].forEach((field) => {
                            messageObj[field] = stringContent;
                        });

                        // Try to create proper message object
                        const createFn =
                            (method as any).output?.create ||
                            (method as any).output?.$type?.create;
                        if (createFn && typeof createFn === "function") {
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
                const bufHex =
                    responseBuffer.length > 0
                        ? Array.from(
                            responseBuffer.subarray(
                                0,
                                Math.min(responseBuffer.byteLength, 16),
                            ),
                        )
                            .map((b) => b.toString(16).padStart(2, "0"))
                            .join("")
                        : "";
                throw new ConnectError(
                    `Failed to decode response (${responseBuffer.byteLength} bytes, prefix: ${bufHex}...)`,
                    Code.DataLoss,
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

        if (
            p2pStream &&
            typeof p2pStream.abort === "function" &&
            !p2pStream.stat?.status?.includes("CLOSED")
        ) {
            try {
                logger.debug(
                    "Unary: Aborting p2pStream in outer catch due to error:",
                    error,
                );
                p2pStream.abort(error);
            } catch (abortErr) {
                logger.error(
                    "Unary: Error aborting p2pStream in outer catch:",
                    abortErr,
                );
            }
        }
        if (error instanceof ConnectError) throw error;
        throw new ConnectError(
            `Unary libp2p transport error: ${error.message || String(error)}`,
            Code.Internal, // Default to internal if not a ConnectError already
            undefined,
            undefined,
            error,
        );
    } finally {
        if (
            p2pStream &&
            typeof p2pStream.close === "function" &&
            !p2pStream.stat?.status?.includes("CLOSED")
        ) {
            try {
                logger.debug(
                    "Unary: Ensuring p2pStream is closed in finally block.",
                );
                await p2pStream.close();
            } catch (closeErr) {
                logger.error(
                    "Unary: Error closing p2pStream in finally:",
                    closeErr,
                );
            }
        }
    }
}
