/**
 * Generic dRPC client factory for HTTP/2 and libp2p transports.
 * 1:1 port of Go's NewClient[T] pattern.
 */

import { createPromiseClient, UnaryResponse } from '@bufbuild/connect';
import { createConnectTransport } from '@bufbuild/connect-web';
import type { DescService, Message } from '@bufbuild/protobuf';
import { createLibp2pHost } from './libp2p-host';
import { multiaddr } from '@multiformats/multiaddr';

/**
 * NewClient<T> creates a generic dRPC client for HTTP/2 or libp2p.
 * @param addr - server address (http(s)://... or multiaddr)
 * @param service - DescService (from generated code)
 * @param options - optional config
 * @returns Promise client for the service
 */
export async function NewClient<T extends DescService>(
  addr: string,
  service: T,
  options?: { logger?: any }
): Promise<ReturnType<typeof createPromiseClient<T>>> {
  // HTTP/2 path
  if (addr.startsWith('http://') || addr.startsWith('https://')) {
    const transport = createConnectTransport({ baseUrl: addr });
    return createPromiseClient(service, transport);
  }

  // libp2p path
  if (isMultiaddr(addr)) {
    // Create libp2p host in client mode
    const { libp2p } = await createLibp2pHost({ isClientMode: true, logger: options?.logger });
    const ma = multiaddr(addr);
    const peerIdStr = ma.getPeerId();
    if (!peerIdStr) throw new Error('Multiaddr missing peer ID');
    // Provide a Connect-compatible transport for libp2p
    const transport = createLibp2pConnectTransport(libp2p, ma);
    return createPromiseClient(service, transport);
  }

  throw new Error('Unsupported address format');
}

function isMultiaddr(addr: string): boolean {
  try {
    const ma = multiaddr(addr);
    return ma.protoNames().includes('p2p');
  } catch {
    return false;
  }
}

/**
 * Creates a Connect-compatible transport for libp2p.
 */
function createLibp2pConnectTransport(libp2p: any, ma: any) {
  return {
    async unary<I extends Message<any>, O extends Message<any>>(
      service: DescService,
      methodInfo: any,
      signal: AbortSignal | undefined,
      timeoutMs: number | undefined,
      header: HeadersInit | undefined,
      input: any
    ): Promise<UnaryResponse<I, O>> {
      // Serialize input using methodInfo
      const inputBytes: Uint8Array = methodInfo.I.toBinary(input);
      // Open stream to peer using DRPC protocol
      const protocol = '/drpc/1.0.0';
      const stream = await libp2p.dialProtocol(ma, protocol);
      const writer = stream.sink;
      const reader = stream.source;
      // Send request: { service, method, input }
      const msg = JSON.stringify({
        service: service.typeName,
        method: methodInfo.name,
        input: Array.from(inputBytes)
      });
      await writer([new TextEncoder().encode(msg)]);
      // Read response (assume single message)
      const chunks: Uint8Array[] = [];
      for await (const chunk of reader) {
        chunks.push(chunk as Uint8Array);
      }
      const respStr = new TextDecoder().decode(Uint8Array.from(chunks.flat()));
      const resp = JSON.parse(respStr);
      if (resp.error) throw new Error(resp.error);
      // Return full UnaryResponse for Connect
      return {
        message: new Uint8Array(resp.result),
        service: service,
        method: methodInfo,
        header: new Headers(),
        trailer: new Headers(),
        stream: false
      } as unknown as UnaryResponse<I, O>;
    },
    // Stub stream method to satisfy Transport interface
    stream() {
      throw new Error('Streaming not implemented for libp2p transport');
    }
  };
}