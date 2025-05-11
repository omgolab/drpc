// Use @bufbuild/protobuf and generated message classes for all encoding/decoding
// removed duplicate import of toBinary, fromBinary, toJson, fromJson
import {
    SayHelloRequestSchema,
    SayHelloResponseSchema,
    BidiStreamingEchoRequestSchema,
    BidiStreamingEchoResponseSchema,
} from "../../demo/gen/ts/greeter/v1/greeter_pb";
import { create, toBinary, fromBinary, toJson, fromJson } from "@bufbuild/protobuf";
import { createLibp2p } from 'libp2p';
import { noise } from '@chainsafe/libp2p-noise';
import { yamux } from '@chainsafe/libp2p-yamux';
import { tcp } from '@libp2p/tcp';
import { webSockets } from '@libp2p/websockets';
import { mdns } from '@libp2p/mdns';

/**
 * Creates a libp2p node with standard transports and protocols
 * 
 * @returns A configured libp2p node
 */
export async function createNode() {
    const node = await createLibp2p({
        transports: [tcp(), webSockets()],
        connectionEncrypters: [noise()],
        streamMuxers: [yamux()],
        peerDiscovery: [mdns({ interval: 1000 })]
    });

    await node.start();
    console.log("TS client started with ID:", node.peerId.toString());
    return node;
}

/**
 * Encodes a SayHello request message
 * 
 * @param obj The message object with a name field
 * @returns A Uint8Array containing the encoded message
 */
export function encodeSayHelloRequest(obj: { name: string }): Uint8Array {
    return toBinary(SayHelloRequestSchema, create(SayHelloRequestSchema, obj));
}

/**
 * Encodes a SayHello request message as JSON (Uint8Array)
 */
export function encodeSayHelloRequestJson(obj: { name: string }): Uint8Array {
    return new TextEncoder().encode(JSON.stringify(toJson(SayHelloRequestSchema, create(SayHelloRequestSchema, obj))));
}

/**
 * Decodes a SayHello response message
 * 
 * @param buf The buffer containing the encoded message
 * @returns The decoded message with a message field
 */
export function decodeSayHelloResponse(buf: Uint8Array): { message: string } {
    return fromBinary(SayHelloResponseSchema, buf);
}

/**
 * Decodes a SayHello response message from JSON (Uint8Array)
 */
export function decodeSayHelloResponseJson(buf: Uint8Array): { message: string } {
    return fromJson(SayHelloResponseSchema, JSON.parse(new TextDecoder().decode(buf)));
}

/**
 * Encodes a BidiStreamingEcho request message
 * 
 * @param obj The message object with a name field 
 * @returns A Uint8Array containing the encoded message
 */
export function encodeBidiStreamingEchoRequest(obj: { name: string }): Uint8Array {
    return toBinary(BidiStreamingEchoRequestSchema, create(BidiStreamingEchoRequestSchema, obj));
}

/**
 * Encodes a BidiStreamingEcho request message as JSON (Uint8Array)
 */
export function encodeBidiStreamingEchoRequestJson(obj: { name: string }): Uint8Array {
    return new TextEncoder().encode(JSON.stringify(toJson(BidiStreamingEchoRequestSchema, create(BidiStreamingEchoRequestSchema, obj))));
}

/**
 * Decodes a BidiStreamingEcho response message
 * 
 * @param buf The buffer containing the encoded message
 * @returns The decoded message with a greeting field
 */
export function decodeBidiStreamingEchoResponse(buf: Uint8Array): { greeting: string } {
    return fromBinary(BidiStreamingEchoResponseSchema, buf);
}

/**
 * Decodes a BidiStreamingEcho response message from JSON (Uint8Array)
 */
export function decodeBidiStreamingEchoResponseJson(buf: Uint8Array): { greeting: string } {
    return fromJson(BidiStreamingEchoResponseSchema, JSON.parse(new TextDecoder().decode(buf)));
}

/**
 * Utility to select encode/decode functions by content type
 */
export function getCodecFns(contentType: string) {
    // Proto types
    if (
        contentType.endsWith('+proto') ||
        contentType === 'application/proto'
    ) {
        return {
            encodeSayHelloRequest,
            decodeSayHelloResponse,
            encodeBidiStreamingEchoRequest,
            decodeBidiStreamingEchoResponse,
        };
    }
    // JSON types
    if (
        contentType.endsWith('+json') ||
        contentType === 'application/json'
    ) {
        return {
            encodeSayHelloRequest: encodeSayHelloRequestJson,
            decodeSayHelloResponse: decodeSayHelloResponseJson,
            encodeBidiStreamingEchoRequest: encodeBidiStreamingEchoRequestJson,
            decodeBidiStreamingEchoResponse: decodeBidiStreamingEchoResponseJson,
        };
    }
    throw new Error(`Unknown content type: ${contentType}`);
}
