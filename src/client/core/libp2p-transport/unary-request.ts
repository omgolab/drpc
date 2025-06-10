/**
 * Unary request handling utilities
 */
import { create } from "@bufbuild/protobuf";
import { concatUint8Arrays } from "../utils";
import { prepareRequestHeader } from "./base-handler";
import type { DescMessage, DescMethod, MessageInitShape } from "@bufbuild/protobuf";
import { ILogger } from "../logger";

/**
 * Prepare a complete unary request payload
 */
export function prepareUnaryRequest<I extends DescMessage>(
    method: { parent: { typeName: string }; name: string; input: any },
    message: MessageInitShape<I>,
    contentType: string,
    serialize: (message: any) => Uint8Array,
    logger: ILogger,
): Uint8Array {
    // Prepare header and serialize the request
    const initialHeader = prepareRequestHeader(method, contentType);
    const requestMessage = create(method.input, message);
    const serializedPayload = serialize(requestMessage);

    logger.debug(
        `Prepared unary request, payload size: ${serializedPayload.length} bytes`
    );

    // Prepare the complete request in a single buffer
    return concatUint8Arrays([initialHeader, serializedPayload]);
}
