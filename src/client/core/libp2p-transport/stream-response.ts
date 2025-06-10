/**
 * Stream response handling utilities
 */
import { Code, ConnectError } from "@connectrpc/connect";
import all from "it-all";
import { ILogger } from "../logger";
import { processResponseChunk } from "./base-handler";
import { BufferUtils } from "../transport-common";
import { parseEnvelope, isEndStreamFlag, uint8ArrayToString, toHex } from "../envelopes";
import { concatUint8Arrays } from "../utils";

export interface StreamMessage {
    data: Uint8Array;
    isEndStream: boolean;
}

/**
 * Process streaming response from a libp2p stream
 */
export async function processStreamingResponse(
    p2pStream: any,
    linkedSignal: AbortSignal,
    logger: ILogger,
): Promise<StreamMessage[]> {
    logger.debug("Stream: Processing server responses");

    const serverMessages: StreamMessage[] = [];
    const allChunks: Uint8Array[] = [];

    try {
        // Collect all chunks first
        const chunks = await all(p2pStream.source);
        logger.debug(`Stream: Received ${chunks.length} chunks from server`);

        for (const chunk of chunks) {
            const processedChunk = processResponseChunk(chunk, linkedSignal, "Stream");
            allChunks.push(processedChunk);
        }

        if (allChunks.length === 0) {
            logger.debug("Stream: No response data received from server");
            return serverMessages;
        }

        // Concatenate all chunks and process envelopes
        const responseBuffer = concatUint8Arrays(allChunks);
        logger.debug(`Stream: Processing ${responseBuffer.length} bytes of response data`);

        // Parse all envelopes from the complete buffer
        let currentOffset = 0;
        let messageCount = 0;

        while (currentOffset < responseBuffer.length) {
            if (linkedSignal.aborted) {
                throw new ConnectError(
                    "Stream response processing aborted by client",
                    Code.Canceled,
                );
            }

            // Get remaining buffer from current offset
            const remainingBuffer = responseBuffer.slice(currentOffset);

            // Make sure we have enough bytes for the envelope header (1 flag byte + 4 length bytes)
            if (remainingBuffer.length < 5) {
                logger.debug(
                    `Buffer too short for envelope header at offset ${currentOffset}`,
                );
                break; // Not enough data for another envelope
            }

            try {
                const parseResult = parseEnvelope(remainingBuffer);

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

                // Add the parsed message
                serverMessages.push({
                    data: envelope.data,
                    isEndStream,
                });

                messageCount++;
                currentOffset += bytesRead;

                // Break if we hit an end stream envelope
                if (isEndStream) {
                    logger.debug(
                        `Stream: End stream envelope found after ${messageCount} messages`,
                    );
                    break;
                }
            } catch (error: any) {
                logger.error(
                    `Error parsing envelope at offset ${currentOffset}: ${error.message}`,
                );
                // Try to advance and continue
                currentOffset += 1;
            }
        }

        logger.debug(
            `Stream: Parsed ${serverMessages.length} server messages from response`,
        );

        return serverMessages;

    } catch (error: any) {
        if (error instanceof ConnectError) {
            throw error;
        }

        logger.error("Stream: Error processing response:", error);
        throw new ConnectError(
            `Stream response error: ${error.message}`,
            Code.Internal,
        );
    }
}
