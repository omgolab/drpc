/**
 * Unary response handling utilities
 */
import { Code, ConnectError } from "@connectrpc/connect";
import all from "it-all";
import { ILogger } from "../logger";
import { processResponseChunk } from "./base-handler";
import { BufferUtils } from "../transport-common";

/**
 * Process unary response from a libp2p stream
 */
export async function processUnaryResponse(
    p2pStream: any,
    linkedSignal: AbortSignal,
    logger: ILogger,
): Promise<Uint8Array[]> {
    logger.debug("Unary: Request sent, reading response");

    // Receive and process response
    const responseChunks: Uint8Array[] = [];

    try {
        // Read all response data using all() for reliable stream data collection
        const chunks = await all(p2pStream.source);
        logger.debug(`Unary: Received ${chunks.length} chunks from stream`);

        for (const chunk of chunks) {
            const processedChunk = processResponseChunk(chunk, linkedSignal, "Unary");
            responseChunks.push(processedChunk);
        }

        if (responseChunks.length === 0) {
            throw new ConnectError(
                "Unary: No response data received",
                Code.Internal,
            );
        }

        logger.debug(`Unary: Total response data: ${BufferUtils.getChunkDebugInfo(responseChunks)}`);
        return responseChunks;

    } catch (error: any) {
        if (error instanceof ConnectError) {
            throw error;
        }

        logger.error("Unary: Error reading response:", error);
        throw new ConnectError(
            `Unary response error: ${error.message}`,
            Code.Internal,
        );
    }
}
