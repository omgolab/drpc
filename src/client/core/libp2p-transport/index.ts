/**
 * Libp2p transport implementation for Connect RPC
 * Main entry point for the modularized transport
 */
import { handleUnary, UnaryHandlerDependencies } from "./unary-handler";
import { handleStream, StreamHandlerDependencies } from "./stream-handler";
import { DRPCOptions } from "../types";
import type {
    DescMessage,
    DescMethodUnary,
    DescMethodStreaming,
    MessageInitShape,
} from "@bufbuild/protobuf";
import { Transport } from "@connectrpc/connect";
import { UnaryResponse, StreamResponse, ContextValues } from "../types";
import { config } from "../constants";

/**
 * Create a libp2p transport instance
 */
export function createLibp2pTransport(
    libp2p: any,
    ma: any,
    options: DRPCOptions,
): Transport {
    const WEBSTREAM_PROTOCOL_ID = config.drpcWebstreamProtocolId;

    const logger = options.logger.createChildLogger({
        contextName: "Libp2p-Transport",
    });

    // Common dependencies for handlers
    const unaryDeps: UnaryHandlerDependencies = {
        libp2p,
        ma,
        PROTOCOL_ID: WEBSTREAM_PROTOCOL_ID,
        logger,
        options,
    };

    const streamDeps: StreamHandlerDependencies = {
        libp2p,
        ma,
        PROTOCOL_ID: WEBSTREAM_PROTOCOL_ID,
        logger,
        options,
    };

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
            contextValues?: ContextValues,
        ): Promise<UnaryResponse<I, O>> {
            return handleUnary(
                unaryDeps,
                method,
                signal,
                timeoutMs,
                header,
                message,
                contextValues,
            );
        },

        /**
         * Streaming RPC implementation
         */
        async stream<I extends DescMessage, O extends DescMessage>(
            method: DescMethodStreaming<I, O>,
            signal: AbortSignal | undefined,
            timeoutMs: number | undefined,
            header: HeadersInit | undefined,
            input: AsyncIterable<MessageInitShape<I>>,
            contextValues?: ContextValues,
        ): Promise<StreamResponse<I, O>> {
            return handleStream(
                streamDeps,
                method,
                signal,
                timeoutMs,
                header,
                input,
                contextValues,
            );
        },
    };
}
