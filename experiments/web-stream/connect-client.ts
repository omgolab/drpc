import { Libp2p } from 'libp2p';
import { pipe } from 'it-pipe';
import all from 'it-all';
import { PeerId } from '@libp2p/interface-peer-id';
import { Flag, Envelope, serializeEnvelope, parseEnvelope, toHex, uint8ArrayToString } from './envelope';
import * as protoConnect from "@connectrpc/connect/protocol-connect";
import * as protoGrpcWeb from "@connectrpc/connect/protocol-grpc-web";
import * as protoGrpc from "@connectrpc/connect/protocol-grpc";

// Protocol ID for Connect RPC over libp2p
export const CONNECT_PROTOCOL_ID = "/drpc-webstream/1.0.0";

/**
 * All supported content types for Connect/GRPC/GRPC-Web interop tests.
 */
const CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE = protoConnect.contentTypeUnaryProto; // "application/proto";
const CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE = protoConnect.contentTypeUnaryJson; // "application/json";
const CONNECT_CONTENT_TYPE = protoConnect.contentTypeStreamProto; // "application/connect+proto";
const CONNECT_JSON_CONTENT_TYPE = protoConnect.contentTypeStreamJson; // "application/connect+json";
const GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE = protoGrpcWeb.contentTypeProto; // "application/grpc-web+proto";
const GRPC_WEB_JSON_CONTENT_TYPE = protoGrpcWeb.contentTypeJson; // "application/grpc-web+json";
const GRPC_PROTO_WITH_UNARY_CONTENT_TYPE = protoGrpc.contentTypeProto; // "application/grpc+proto";
const GRPC_JSON_CONTENT_TYPE = protoGrpc.contentTypeJson; // "application/grpc+json";

/**
 * Content types supported for unary and streaming calls.
 */
export const unaryContentTypes = [
    CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE,     // application/json
    CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE,    // application/proto
    GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE,   // application/grpc-web+proto
    GRPC_PROTO_WITH_UNARY_CONTENT_TYPE,       // application/grpc+proto
];

export const streamingContentTypes = [
    CONNECT_JSON_CONTENT_TYPE,            // application/connect+json
    GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE,// application/grpc-web+proto
    GRPC_WEB_JSON_CONTENT_TYPE,           // application/grpc-web+json
    GRPC_PROTO_WITH_UNARY_CONTENT_TYPE,   // application/grpc+proto
    GRPC_JSON_CONTENT_TYPE,               // application/grpc+json
];

/**
 * Helper function to convert various types to Buffer
 */
function toBuffer(data: any): Buffer {
    if (data instanceof Uint8Array) {
        return Buffer.from(data.buffer, data.byteOffset, data.byteLength);
    } else if (typeof data.subarray === "function") {
        return Buffer.from(data.subarray(0));
    } else {
        return Buffer.from(Uint8Array.from(data));
    }
}

/**
 * A client for making Connect RPC calls over libp2p
 */
export class ConnectClient {
    private node: Libp2p;
    private servicePath: string;
    private debug: boolean;

    /**
     * Create a new ConnectClient
     * 
     * @param node The libp2p node to use for connections
     * @param servicePath The service path, e.g. "/greeter.v1.GreeterService"
     * @param debug Whether to enable debug logging
     */
    constructor(node: Libp2p, servicePath: string, debug = false) {
        this.node = node;
        this.servicePath = servicePath;
        this.debug = debug;
    }

    /**
     * Log a debug message if debug is enabled
     * @param message The message to log
     */
    private log(message: string) {
        if (this.debug) {
            console.log(`[ConnectClient] ${message}`);
        }
    }

    /**
     * Prepare the headers (procedure path and content type) for a Connect RPC call
     * 
     * @param method The method name
     * @param contentType The content type to use
     * @returns A buffer containing the procedure path and content type
     */
    private prepareHeaders(method: string, contentType: string): Buffer {
        // Build the full procedure path
        const procedurePath = `${this.servicePath}/${method}`;

        // Create the procedure path buffer (4-byte length + path)
        const procPathBytes = new TextEncoder().encode(procedurePath);
        const procPathLenBuffer = Buffer.alloc(4);
        procPathLenBuffer.writeUInt32BE(procPathBytes.length, 0);
        const procPathBuffer = Buffer.concat([procPathLenBuffer, Buffer.from(procPathBytes)]);

        // Create the content type buffer (1-byte length + content type)
        const contentTypeBytes = new TextEncoder().encode(contentType);
        const contentTypeLenBuffer = Buffer.alloc(1);
        contentTypeLenBuffer.writeUInt8(contentTypeBytes.length, 0);
        const contentTypeBuffer = Buffer.concat([contentTypeLenBuffer, Buffer.from(contentTypeBytes)]);

        // Combine the headers
        return Buffer.concat([procPathBuffer, contentTypeBuffer]);
    }

    /**
     * Make a unary RPC call
     * 
     * @param peerId The peer ID to connect to
     * @param method The method name
     * @param request The request message
     * @param encodeFunc Function to encode the request message
     * @param decodeFunc Function to decode the response message
     * @returns The decoded response
     */
    async unaryCall<TReq, TResp>(
        peerId: PeerId | any,
        method: string,
        request: TReq,
        encodeFunc: (req: TReq) => Uint8Array,
        decodeFunc: (data: Uint8Array) => TResp,
        contentType: string = CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE
    ): Promise<TResp> {
        const stream = await this.node.dialProtocol(peerId, CONNECT_PROTOCOL_ID);
        this.log(`Opened stream for unary call to ${method}`);

        try {
            // Prepare headers with the selected content type for unary calls
            const headers = this.prepareHeaders(method, contentType);

            // Encode the request message
            const requestPayload = encodeFunc(request);
            this.log(`Request encoded to ${requestPayload.length} bytes: ${Buffer.from(requestPayload).toString("hex")}`);

            // For Connect unary call, we don't add the envelope. Just send the raw protobuf data.
            // This is what the Go server expects with application/proto content type.
            const dataToSend = Buffer.concat([headers, Buffer.from(requestPayload)]);

            this.log(`Sending unary request to ${this.servicePath}/${method}, payload size: ${requestPayload.length} bytes`);

            // Send the request
            await pipe([dataToSend], stream.sink);

            // Receive the response
            const responseChunks = await all(stream.source);
            const responseBuffer = Buffer.concat(responseChunks.map(toBuffer));

            this.log(`Received response, size: ${responseBuffer.length} bytes`);


            try {
                // Try to parse as a raw response first (no envelope for unary responses)
                const response = decodeFunc(responseBuffer);
                this.log(`Successfully decoded raw response: ${JSON.stringify(response)}`);
                return response;
            } catch (decodeError) {
                this.log(`Failed to decode as raw response: ${decodeError}`);

                // Try to parse as an envelope
                try {
                    const parsedEnvelope = parseEnvelope(responseBuffer);
                    this.log(`Response envelope flags: ${parsedEnvelope.flags}, data size: ${parsedEnvelope.data.length} bytes`);

                    if (parsedEnvelope.flags === Flag.NONE || parsedEnvelope.flags === Flag.END_STREAM) {
                        try {
                            // Try to decode as a successful response
                            const response = decodeFunc(parsedEnvelope.data);
                            return response;
                        } catch (envelopeDecodeError) {
                            // Try to parse as an error response
                            try {
                                const errorString = uint8ArrayToString(parsedEnvelope.data);
                                const errorData = JSON.parse(errorString);
                                throw new Error(`Server returned error: ${JSON.stringify(errorData)}`);
                            } catch (jsonError) {
                                throw new Error(`Failed to decode response: ${envelopeDecodeError}. Raw data: ${toHex(parsedEnvelope.data)}`);
                            }
                        }
                    } else {
                        throw new Error(`Unexpected response envelope flags: ${parsedEnvelope.flags}`);
                    }
                } catch (envelopeError) {
                    // If we can't parse as an envelope, try to handle the raw response
                    this.log(`Failed to parse response as envelope: ${envelopeError}`);

                    const responseText = responseBuffer.toString();
                    if (responseText.startsWith('{') && responseText.includes('"code"')) {
                        try {
                            const errorData = JSON.parse(responseText);
                            throw new Error(`Server returned error: ${JSON.stringify(errorData)}`);
                        } catch (jsonError) {
                            throw new Error(`Server returned raw response (not an envelope): ${responseText}`);
                        }
                    } else {
                        throw new Error(`Failed to decode response in any format: ${responseText}`);
                    }
                }
            }
        } finally {
            // Close the stream
            await stream.close();
            this.log("Stream closed");
        }
    }

    /**
     * Make a bidirectional streaming RPC call
     * 
     * @param peerId The peer ID to connect to
     * @param method The method name
     * @param requests An array of request messages
     * @param encodeFunc Function to encode request messages
     * @param decodeFunc Function to decode response messages
     * @returns An array of decoded responses
     */
    async bidiStreamingCall<TReq, TResp>(
        peerId: PeerId | any,
        method: string,
        requests: TReq[],
        encodeFunc: (req: TReq) => Uint8Array,
        decodeFunc: (data: Uint8Array) => TResp,
        contentType: string = CONNECT_CONTENT_TYPE
    ): Promise<TResp[]> {
        const stream = await this.node.dialProtocol(peerId, CONNECT_PROTOCOL_ID);
        this.log(`Opened stream for bidirectional streaming call to ${method}`);

        try {
            // Prepare headers with the selected content type for streaming calls
            const headers = this.prepareHeaders(method, contentType);

            // Create envelopes for all request messages
            const requestEnvelopes: Buffer[] = [];

            for (let i = 0; i < requests.length; i++) {
                const requestPayload = encodeFunc(requests[i]);
                const requestEnvelope: Envelope = { flags: Flag.NONE, data: requestPayload };
                const serializedEnvelope = serializeEnvelope(requestEnvelope);
                requestEnvelopes.push(Buffer.from(serializedEnvelope));

                this.log(`Prepared request ${i + 1}/${requests.length}, payload size: ${requestPayload.length} bytes`);
            }

            // Combine all envelopes
            const combinedEnvelopesBuffer = Buffer.concat(requestEnvelopes);

            // Combine everything into a single buffer to send
            const dataToSend = Buffer.concat([headers, combinedEnvelopesBuffer]);

            this.log(`Sending ${requests.length} requests in ${dataToSend.length} bytes`);

            // Send the request
            await pipe([dataToSend], stream.sink);

            // Close the write side of the stream to signal end of requests
            await stream.closeWrite();
            this.log("Closed write side of stream");

            // Receive the responses
            const responseChunks = await all(stream.source);
            const responseBuffer = Buffer.concat(responseChunks.map(toBuffer));

            this.log(`Received response buffer of size: ${responseBuffer.length} bytes`);

            // Parse the response buffer as a series of envelopes
            const responses: TResp[] = [];
            let currentOffset = 0;
            let endStreamReceived = false;

            while (currentOffset < responseBuffer.length) {
                // Ensure there's enough data left for at least the envelope header
                if (responseBuffer.length < currentOffset + 5) {
                    this.log(`Remaining buffer too short for an envelope header at offset ${currentOffset}`);
                    break;
                }

                try {
                    const remainingBuffer = responseBuffer.subarray(currentOffset);
                    const envelope = parseEnvelope(remainingBuffer);
                    const envelopeSize = 5 + envelope.data.length;

                    this.log(`Parsed envelope at offset ${currentOffset}: flags=${envelope.flags}, data size=${envelope.data.length}`);

                    if (envelope.flags === Flag.END_STREAM) {
                        endStreamReceived = true;
                        this.log("Received END_STREAM flag");

                        // Check if there's data with the END_STREAM flag
                        if (envelope.data && envelope.data.length > 0) {
                            try {
                                const errorString = uint8ArrayToString(envelope.data);
                                if (errorString.startsWith('{')) {
                                    try {
                                        const errorData = JSON.parse(errorString);
                                        this.log(`Error data with END_STREAM: ${JSON.stringify(errorData)}`);

                                        // Only throw an error if this appears to be a real error, not just an empty object
                                        if (errorData.code || errorData.message) {
                                            throw new Error(`Server returned error: ${errorString}`);
                                        }
                                    } catch (jsonError) {
                                        if (jsonError instanceof Error && jsonError.message.startsWith('Server returned error:')) {
                                            throw jsonError; // Re-throw the error we just created
                                        }
                                        // Otherwise just log and continue - empty objects can be ignored
                                        this.log(`Couldn't parse as error JSON: ${errorString}`);
                                    }
                                }
                            } catch (err) {
                                if (err instanceof Error && err.message.startsWith('Server returned error:')) {
                                    throw err; // Re-throw the error we just created
                                }
                                this.log(`Non-critical error processing END_STREAM data: ${err}`);
                            }
                        }

                        // Advance past this envelope
                        currentOffset += envelopeSize;
                        break; // End of stream
                    } else if (envelope.flags === Flag.NONE) {
                        // Regular data message
                        if (envelope.data.length > 0) {
                            try {
                                const response = decodeFunc(envelope.data);
                                responses.push(response);
                                this.log(`Decoded response: ${JSON.stringify(response)}`);
                            } catch (decodeError) {
                                this.log(`Failed to decode response data: ${decodeError}`);
                            }
                        }

                        // Advance past this envelope
                        currentOffset += envelopeSize;
                    } else {
                        this.log(`Unexpected envelope flags: ${envelope.flags}`);
                        currentOffset += envelopeSize;
                    }
                } catch (envelopeError) {
                    this.log(`Error parsing envelope at offset ${currentOffset}: ${envelopeError}`);
                    break;
                }
            }

            if (!endStreamReceived) {
                this.log("WARNING: Did not receive END_STREAM flag from server");
            }

            this.log(`Received ${responses.length} responses`);
            return responses;
        } finally {
            // Close the stream
            await stream.close();
            this.log("Stream closed");
        }
    }
}
