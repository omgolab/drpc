/**
 * Smart HTTP transport factory with libp2p fallback for streaming
 */
import { ConnectError, Code } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { UnaryResponse, StreamResponse, ContextValues } from "../types";
import { DRPCOptions } from "../types";
import { createLibp2pTransport } from "../libp2p-transport/";
import { resolveHttpToP2p } from "./resolver";
import { discoverOptimalConnectPath } from "../discover";
import type {
    DescMessage,
    DescMethodUnary,
    DescMethodStreaming,
    MessageInitShape,
} from "@bufbuild/protobuf";
import { Transport } from "@connectrpc/connect";

/**
 * Create a smart HTTP transport that can switch to libp2p for streaming
 */
export async function createSmartHttpLibp2pTransport(
    httpServerAddr: string,
    libp2pHost: any,
    clientOptions: DRPCOptions,
): Promise<Transport> {
    // Get logger from options or use the default
    const logger = clientOptions.logger.createChildLogger({
        contextName: "HTTP-Transport",
    });
    // Standard HTTP transport for most requests
    const httpTransport = createConnectTransport({ baseUrl: httpServerAddr });

    // Cached instances
    let resolvedLibp2pTransportInstance: Transport | null = null;

    /**
     * Resolve HTTP URL to libp2p multiaddress via /p2pinfo endpoint
     */
    async function getResolvedLibp2pTransportForStream(
        originalHttpUrl: string,
    ): Promise<Transport> {
        // Avoid re-resolving and re-creating transport if already done
        if (resolvedLibp2pTransportInstance) return resolvedLibp2pTransportInstance;

        const p2pAddr = await resolveHttpToP2p(originalHttpUrl, clientOptions);

        // query the peer info
        const res = await discoverOptimalConnectPath(libp2pHost, p2pAddr);
        if (!res.addr) {
            throw new Error(`Failed to find connection path to ${p2pAddr} : ${res.totalTime}ms`);
        }

        // Create a new libp2p transport instance using the resolved multiaddress
        resolvedLibp2pTransportInstance = createLibp2pTransport(
            libp2pHost,
            res.addr,
            clientOptions,
        );
        return resolvedLibp2pTransportInstance;
    }

    // Create the smart transport that can handle both HTTP and libp2p
    const smartTransport: Transport = {
        async unary<I extends DescMessage, O extends DescMessage>(
            method: DescMethodUnary<I, O>,
            signal: AbortSignal | undefined,
            timeoutMs: number | undefined,
            header: HeadersInit | undefined,
            input: MessageInitShape<I>,
            contextValues?: ContextValues,
        ): Promise<UnaryResponse<I, O>> {
            logger.debug(
                `[SmartTransport] Unary call '${method.name}' via HTTP to ${httpServerAddr}`,
            );
            return httpTransport.unary(
                method,
                signal,
                timeoutMs,
                header,
                input,
                contextValues,
            );
        },

        async stream<I extends DescMessage, O extends DescMessage>(
            method: DescMethodStreaming<I, O>,
            signal: AbortSignal | undefined,
            timeoutMs: number | undefined,
            header: HeadersInit | undefined,
            input: AsyncIterable<MessageInitShape<I>>,
            contextValues?: ContextValues,
        ): Promise<StreamResponse<I, O>> {
            const isClientSideStreaming =
                method.methodKind === "client_streaming" ||
                method.methodKind === "bidi_streaming";

            if (isClientSideStreaming) {
                logger.debug(
                    `[SmartTransport] Client/Bidi stream '${method.name}' (kind: ${method.methodKind}) detected for HTTP URL ${httpServerAddr}. Attempting /p2pinfo lookup.`,
                );
                try {
                    const libp2pTransportForStream =
                        await getResolvedLibp2pTransportForStream(httpServerAddr);

                    // Pass options to the libp2p transport's stream method
                    return libp2pTransportForStream.stream(
                        method,
                        signal,
                        timeoutMs,
                        header,
                        input,
                        contextValues,
                    );
                } catch (error: any) {
                    console.error(
                        `[SmartTransport] Failed to switch to libp2p for streaming call '${method.name}':`,
                        error,
                    );
                    if (error instanceof ConnectError) throw error;
                    throw new ConnectError(
                        `Error during p2pinfo lookup or libp2p stream setup for '${method.name}': ${error.message}`,
                        Code.Internal,
                        undefined,
                        undefined,
                        error,
                    );
                }
            } else {
                // For Unary (though handled by smartTransport.unary) or ServerStreaming, use HTTP transport
                // Standard httpTransport.stream.
                logger.debug(
                    `[SmartTransport] Unary/Server stream '${method.name}' via HTTP to ${httpServerAddr}`,
                );
                return httpTransport.stream(
                    method,
                    signal,
                    timeoutMs,
                    header,
                    input,
                    contextValues,
                );
            }
        },
    };

    // Return the smart transport
    return smartTransport;
}
