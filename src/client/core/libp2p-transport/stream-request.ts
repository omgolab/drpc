/**
 * Stream request handling utilities
 */
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { encodeEnvelopes } from "@connectrpc/connect/protocol";
import { concatUint8Arrays } from "../utils";
import { prepareRequestHeader } from "./base-handler";
import { Flag } from "../envelopes";
import type { DescMessage, MessageInitShape } from "@bufbuild/protobuf";
import { ILogger } from "../logger";

/**
 * Collect and prepare streaming request data
 */
export async function prepareStreamingRequest<I extends DescMessage>(
    method: { parent: { typeName: string }; name: string; input: any },
    input: AsyncIterable<MessageInitShape<I>>,
    contentType: string,
    serialize: (message: any) => Uint8Array,
    linkedSignal: AbortSignal,
    logger: ILogger,
): Promise<Uint8Array[]> {
    // Following the pattern from connect-client.ts:
    // 1. First collect all client messages

    const initialHeader = prepareRequestHeader(method, contentType);

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

    logger.debug(`Stream: Collected ${payloads.length} client messages`);

    if (payloads.length === 0) {
        logger.debug("Stream: No client messages to send");
        return buffers;
    }

    // 2. Serialize all envelopes together for efficiency
    const envelopesData = encodeEnvelopes(
        ...payloads.map((p) => ({ flags: Flag.NONE, data: p })),
    );
    buffers.push(envelopesData);

    logger.debug(
        `Stream: Prepared request with ${buffers.length} buffers, total payloads: ${payloads.length}`,
    );

    return buffers;
}
