// Ported from Go pkg/core/host.go to TypeScript for browser and Node.js using js-libp2p

import { createLibp2p, Libp2p, Libp2pOptions } from "libp2p";
import { kadDHT, KadDHTInit } from "@libp2p/kad-dht";
import { mdns } from "@libp2p/mdns";
import { webSockets } from "@libp2p/websockets";
import { tcp } from "@libp2p/tcp";
import { tls } from "@libp2p/tls";
import { identify } from "@libp2p/identify";
import { autoNAT } from "@libp2p/autonat";
import { EventEmitter } from "events";
import { ping } from "@libp2p/ping";
import { yamux } from "@chainsafe/libp2p-yamux";
import { circuitRelayTransport } from "@libp2p/circuit-relay-v2";
import { ILogger } from "./logger";

export interface CreateLibp2pHostOptions {
  logger: ILogger;
  libp2pOptions?: Partial<Libp2pOptions>;
  dhtOptions?: Partial<KadDHTInit>;
  isClientMode?: boolean;
}

export interface Libp2pHostResult {
  libp2p: Libp2p;
  peerDiscovery: EventEmitter;
}

/**
 * Create a js-libp2p host with DHT, mDNS, relay, and peer discovery.
 * Compatible with browser and Node.js.
 */
export async function createLibp2pHost(
  opts: CreateLibp2pHostOptions,
): Promise<Libp2pHostResult> {
  const logger = opts.logger;
  const isClientMode = opts.isClientMode === undefined ? true : opts.isClientMode;

  // Peer discovery event emitter
  const peerDiscovery = new EventEmitter();

  // DHT config
  const dhtConfig: Partial<KadDHTInit> = {
    clientMode: isClientMode,
    ...opts.dhtOptions,
  };

  // Transport selection with enhanced relay configuration
  const transports = [
    tcp(),
    webSockets(),
    circuitRelayTransport({
      // These options are set internally by circuitRelayTransport
      // but we'll be explicit to ensure proper configuration:
      reservationConcurrency: 3,
    })
  ];

  // Compose libp2p options
  const defaultListenAddrs = [
    "/ip4/0.0.0.0/tcp/0",
    "/ip6/::/tcp/0",
    "/p2p-circuit",
  ];
  const libp2pConfig: Libp2pOptions = {
    transports,
    streamMuxers: [yamux()],
    connectionEncrypters: [tls()], // Only TLS, disables Noise
    peerDiscovery: [
      mdns({
        interval: 10000, // More frequent discovery
        broadcast: true,
        serviceTag: 'drpc',
      })
    ],
    addresses: {
      ...(opts.libp2pOptions?.addresses || {}),
      listen:
        opts.libp2pOptions?.addresses?.listen && opts.libp2pOptions.addresses.listen.length > 0
          ? opts.libp2pOptions.addresses.listen
          : defaultListenAddrs,
    },
    services: {
      identify: identify(),
      ping: ping({
        // More frequent ping to keep connections alive
        timeout: 5000
      }),
      dht: kadDHT({
        ...dhtConfig,
        // Enable DHT server mode for better peer routing
        clientMode: false,
        validators: {},
        selectors: {}
      }),
      autonat: autoNAT({
        // More aggressive autonat for better connectivity
        refreshInterval: 10000
      }),
    },
    connectionGater: {
      // Allow dialing any multiadress - needed for some relay scenarios
      denyDialMultiaddr: async () => false,
      // Also allow all connections
      denyDialPeer: async () => false,
      denyInboundConnection: async () => false,
      denyOutboundConnection: async () => false,
      denyInboundEncryptedConnection: async () => false,
      denyOutboundEncryptedConnection: async () => false,
      denyInboundUpgradedConnection: async () => false,
      denyOutboundUpgradedConnection: async () => false,
    },
    connectionManager: {
      // Increase max connections for relay discovery
      maxConnections: 100
    },
    ...opts.libp2pOptions,
  };

  // Create the libp2p node
  const libp2p = await createLibp2p(libp2pConfig);

  // Setup peer discovery event forwarding
  libp2p.addEventListener("peer:discovery", (evt: any) => {
    peerDiscovery.emit("peer", evt.detail);
    logger.debug?.("Discovered peer", evt.detail.id?.toString?.());
  });

  // Start the node
  await libp2p.start();
  logger.info("Libp2p host started with id:", libp2p.peerId.toString());
  logger.info(
    "Listening on addresses:",
    libp2p.getMultiaddrs().map((a) => a.toString()),
  );

  // Optionally advertise on DHT (js-libp2p does not have direct advertise API, but can use DHT for peer routing)
  // Optionally, implement periodic peer discovery as in Go if needed

  return {
    libp2p,
    peerDiscovery,
  };
}
