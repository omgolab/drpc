/**
 * Core types for the dRPC client
 */
import type {
    DescService,
    DescMethodUnary,
    DescMethodStreaming,
    DescMessage,
    MessageShape
} from "@bufbuild/protobuf";
import { type ContextValues } from "@connectrpc/connect";
import { type Envelope } from "./envelope";
import * as protoConnect from "@connectrpc/connect/protocol-connect";
import * as protoGrpcWeb from "@connectrpc/connect/protocol-grpc-web";
import * as protoGrpc from "@connectrpc/connect/protocol-grpc";
import { ILogger } from "./logger";

// Re-export ContextValues
export { ContextValues };

/**
 * All supported content types for Connect/GRPC/GRPC-Web interop.
 */
export const CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE = protoConnect.contentTypeUnaryProto; // "application/proto";
export const CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE = protoConnect.contentTypeUnaryJson; // "application/json";
export const CONNECT_CONTENT_TYPE = protoConnect.contentTypeStreamProto; // "application/connect+proto";
export const CONNECT_JSON_CONTENT_TYPE = protoConnect.contentTypeStreamJson; // "application/connect+json";
export const GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE = protoGrpcWeb.contentTypeProto; // "application/grpc-web+proto";
export const GRPC_WEB_JSON_CONTENT_TYPE = protoGrpcWeb.contentTypeJson; // "application/grpc-web+json";
export const GRPC_PROTO_WITH_UNARY_CONTENT_TYPE = protoGrpc.contentTypeProto; // "application/grpc+proto";
export const GRPC_JSON_CONTENT_TYPE = protoGrpc.contentTypeJson; // "application/grpc+json";

/**
 * Type-safe content types for unary RPC calls
 */
export type UnaryContentType =
    | typeof CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE
    | typeof CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE
    | typeof GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE
    | typeof GRPC_PROTO_WITH_UNARY_CONTENT_TYPE;

/**
 * Type-safe content types for streaming RPC calls
 */
export type StreamingContentType =
    | typeof CONNECT_CONTENT_TYPE
    | typeof CONNECT_JSON_CONTENT_TYPE
    | typeof GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE
    | typeof GRPC_WEB_JSON_CONTENT_TYPE
    | typeof GRPC_PROTO_WITH_UNARY_CONTENT_TYPE
    | typeof GRPC_JSON_CONTENT_TYPE;

/**
 * Union type for all supported content types
 */
export type ContentType = UnaryContentType | StreamingContentType;

/**
 * Lists of content types for runtime validation if needed
 */
export const unaryContentTypes: UnaryContentType[] = [
    CONNECT_ONLY_UNARY_JSON_CONTENT_TYPE,     // application/json
    CONNECT_ONLY_UNARY_PROTO_CONTENT_TYPE,    // application/proto
    GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE,   // application/grpc-web+proto
    GRPC_PROTO_WITH_UNARY_CONTENT_TYPE,       // application/grpc+proto
];

/**
 * Lists of content types for runtime validation if needed
 */
export const streamingContentTypes: StreamingContentType[] = [
    CONNECT_CONTENT_TYPE,                     // application/connect+proto
    CONNECT_JSON_CONTENT_TYPE,                // application/connect+json
    GRPC_WEB_WITH_UNARY_PROTO_CONTENT_TYPE,   // application/grpc-web+proto
    GRPC_WEB_JSON_CONTENT_TYPE,               // application/grpc-web+json
    GRPC_PROTO_WITH_UNARY_CONTENT_TYPE,       // application/grpc+proto
    GRPC_JSON_CONTENT_TYPE,                   // application/grpc+json
];

/**
 * Response for a unary RPC call
 */
export interface UnaryResponse<I extends DescMessage, O extends DescMessage> {
    stream: false;
    service: DescService;
    method: DescMethodUnary<I, O>;
    header: Headers;
    trailer: Headers;
    message: MessageShape<O>;
}

/**
 * Response for a streaming RPC call
 */
export interface StreamResponse<I extends DescMessage, O extends DescMessage> {
    stream: true;
    service: DescService;
    method: DescMethodStreaming<I, O>;
    header: Headers;
    trailer: Headers;
    message: AsyncIterable<MessageShape<O>>;
}

/**
 * Result of parsing a response
 */
export interface ParseResult<T> {
    success: boolean;
    message?: T;
    errorMessage?: string;
    envelope?: Envelope | null;
}

/**
 * Configuration options for the dRPC client
 * 
 * Note: Debug logging is controlled globally via the setDebugMode function
 * from the logger module, not through these options.
 */
export interface DRPCOptions {
    /**
     * Custom logger instance that overrides the default logger
     * The default logger respects the global DEBUG flag
     */
    logger: ILogger;

    /**
     * Content type for the request
     * 
     * This will be statically type-checked based on whether you're making a
     * unary or streaming call. The compiler will only allow valid content types
     * for the method kind.
     * 
     * For unary calls: application/json, application/proto, etc.
     * For streaming calls: application/connect+proto, application/connect+json, etc.
     */
    unaryContentType?: UnaryContentType;
    streamingContentType?: StreamingContentType;
}
