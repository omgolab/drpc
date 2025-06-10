/**
 * Shared functionality for libp2p transport handlers
 */
import { Code, ConnectError } from "@connectrpc/connect";
import { ILogger } from "../logger";
import { Multiaddr } from "@multiformats/multiaddr";
import { Libp2p } from "libp2p";
import {
    createManagedAbortController,
    Libp2pConnectionManager,
    ContentTypeValidator,
} from "../transport-common";
import { prepareInitialHeaderPayload } from "../utils";

export interface BaseHandlerDependencies {
    libp2p: Libp2p;
    ma: Multiaddr;
    PROTOCOL_ID: string;
    logger: ILogger;
}

/**
 * Shared connection establishment logic for both unary and stream handlers
 */
export async function establishConnection(
    deps: BaseHandlerDependencies,
    signal: AbortSignal | undefined,
    timeoutMs: number | undefined,
    context: string,
): Promise<{ p2pStream: any; linkedSignal: AbortSignal }> {
    const { libp2p, ma, PROTOCOL_ID, logger } = deps;

    // Create abort controller with timeout if specified
    const abortController = createManagedAbortController(signal, logger, context);
    const linkedSignal = abortController.signal;

    // Check if already aborted before starting connection
    if (linkedSignal.aborted) {
        throw new ConnectError(
            "Request aborted before sending",
            Code.Canceled,
        );
    }

    try {
        // Dial the peer using shared connection manager
        const p2pStream = await Libp2pConnectionManager.dialProtocol(
            libp2p,
            ma,
            PROTOCOL_ID,
            linkedSignal,
            logger,
            context
        );

        return { p2pStream, linkedSignal };
    } catch (error) {
        // Clean up abort controller on error
        abortController.abort();
        throw error;
    }
}

/**
 * Shared request preparation logic
 */
export function prepareRequestHeader(
    method: { parent: { typeName: string }; name: string },
    contentType: string,
): Uint8Array {
    return prepareInitialHeaderPayload(method, contentType);
}

/**
 * Shared content type validation for unary calls
 */
export function validateUnaryContentType(
    contentType: string,
    validTypes: readonly string[],
): void {
    ContentTypeValidator.validateUnaryContentType(contentType, validTypes);
}

/**
 * Shared content type validation for streaming calls
 */
export function validateStreamingContentType(
    contentType: string,
    validTypes: readonly string[],
): void {
    ContentTypeValidator.validateStreamingContentType(contentType, validTypes);
}

/**
 * Shared response chunk validation
 */
export function processResponseChunk(
    chunk: any,
    signal: AbortSignal,
    context: string,
): Uint8Array {
    if (signal.aborted) {
        throw new ConnectError(
            `${context} response aborted by client`,
            Code.Canceled,
        );
    }

    // Convert chunk to Uint8Array with type safety
    if (chunk instanceof Uint8Array) {
        return chunk;
    } else if (chunk.subarray) {
        return new Uint8Array(chunk.subarray());
    } else {
        return new Uint8Array(chunk);
    }
}
