/**
 * Smart HTTP transport with libp2p fallback for streaming
 */
import { ConnectError, Code } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { multiaddr } from "@multiformats/multiaddr";
import { UnaryResponse, StreamResponse, ContextValues } from "./types";
import { DRPCOptions } from "./types";
import { createLibp2pTransport } from "./libp2p-transport";
import type {
  DescMessage,
  DescMethodUnary,
  DescMethodStreaming,
  MessageInitShape,
} from "@bufbuild/protobuf";
import { Transport } from "@connectrpc/connect";

// Global cache for HTTP URL to p2p multiaddress mappings
const p2pAddrCache = new Map<string, string>();

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
  let libp2pNodeInstanceForThisTransport: any = null;

  /**
   * Resolve HTTP URL to libp2p multiaddress via /p2pinfo endpoint
   */
  async function getResolvedLibp2pTransportForStream(
    originalHttpUrl: string,
  ): Promise<Transport> {
    // Avoid re-resolving and re-creating transport if already done
    if (resolvedLibp2pTransportInstance) return resolvedLibp2pTransportInstance;

    let p2pAddr = p2pAddrCache.get(originalHttpUrl);
    if (!p2pAddr) {
      const p2pInfoUrl = new URL(originalHttpUrl);
      // Ensure the path is exactly /p2pinfo, respecting potential existing base paths
      const baseHostnameAndPort = `${p2pInfoUrl.protocol}//${p2pInfoUrl.host}`;
      const finalP2pInfoUrl = `${baseHostnameAndPort}/p2pinfo`;

      logger.debug(`[SmartTransport] Fetching p2pinfo from ${finalP2pInfoUrl}`);
      try {
        const response = await fetch(finalP2pInfoUrl.toString());
        if (!response.ok) {
          const errorText = await response.text();
          throw new ConnectError(
            `Failed to fetch /p2pinfo: ${response.status} ${response.statusText}. Body: ${errorText}`,
            Code.Unavailable,
          );
        }
        const data = await response.json();
        if (
          !data.Addrs ||
          !Array.isArray(data.Addrs) ||
          data.Addrs.length === 0
        ) {
          throw new ConnectError(
            "Invalid /p2pinfo response: missing or empty 'Addrs' array",
            Code.DataLoss,
          );
        }

        let selectedAddr: string | undefined = undefined;
        let firstP2PAddr: string | undefined = undefined;

        // Always prefer local addresses for local development/testing, even if public addresses are present
        for (const addrStr of data.Addrs) {
          if (typeof addrStr === "string" && addrStr.includes("/p2p/")) {
            try {
              multiaddr(addrStr); // Validate if it's a proper multiaddr

              if (!firstP2PAddr) {
                firstP2PAddr = addrStr; // Keep track of the first valid p2p address
              }

              // Always prefer local addresses for local development/testing
              if (
                addrStr.startsWith("/ip4/127.0.0.1/") ||
                addrStr.startsWith("/ip6/::1/")
              ) {
                selectedAddr = addrStr;
                logger.debug(
                  `[SmartTransport] Preferred local p2p address from /p2pinfo: ${selectedAddr}`,
                );
                break; // Found a local address, use it
              }
            } catch (e) {
              logger.warn(
                `[SmartTransport] Invalid multiaddr string in /p2pinfo Addrs array: "${addrStr}"`,
                e,
              );
            }
          }
        }

        // If no local address was found, use the first valid P2P address encountered
        if (!selectedAddr && firstP2PAddr) {
          selectedAddr = firstP2PAddr;
          logger.debug(
            `[SmartTransport] No local p2p address found, using first available: ${selectedAddr}`,
          );
        }

        if (!selectedAddr) {
          throw new ConnectError(
            "No suitable p2p multiaddress found in /p2pinfo response's 'Addrs' array",
            Code.DataLoss,
          );
        }
        p2pAddr = selectedAddr;
        p2pAddrCache.set(originalHttpUrl, p2pAddr);
        logger.debug(
          `[SmartTransport] Resolved ${originalHttpUrl} to ${p2pAddr} and cached.`,
        );
      } catch (error: any) {
        console.error(
          `[SmartTransport] Error fetching or parsing /p2pinfo for ${originalHttpUrl}:`,
          error,
        );
        if (error instanceof ConnectError) throw error;
        throw new ConnectError(
          `Failed to resolve p2p address for ${originalHttpUrl} via /p2pinfo: ${error.message}`,
          Code.Unavailable,
        );
      }
    } else {
      logger.debug(
        `[SmartTransport] Using cached p2p address ${p2pAddr} for ${originalHttpUrl}`,
      );
    }

    libp2pNodeInstanceForThisTransport = libp2pHost;
    const ma = multiaddr(p2pAddr!);
    // Create a new libp2p transport instance using the resolved multiaddress
    resolvedLibp2pTransportInstance = createLibp2pTransport(
      libp2pNodeInstanceForThisTransport,
      ma,
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
        // Standard httpTransport.stream does not take the 7th drpcOptions argument.
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
