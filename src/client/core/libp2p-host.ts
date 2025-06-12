import { createLibp2p, Libp2pOptions, type Libp2p } from 'libp2p';
import { webSockets } from '@libp2p/websockets';
import { webRTC, webRTCDirect } from '@libp2p/webrtc';
import { webTransport } from '@libp2p/webtransport';
import { noise } from '@chainsafe/libp2p-noise';
import { tls } from '@libp2p/tls';
import { yamux } from '@chainsafe/libp2p-yamux';
import { identify, identifyPush } from '@libp2p/identify';
import { kadDHT, KadDHTInit } from '@libp2p/kad-dht';
import { ping } from '@libp2p/ping';
import { autoNAT } from '@libp2p/autonat';
import { dcutr } from '@libp2p/dcutr';
import { bootstrap } from '@libp2p/bootstrap';
import { circuitRelayTransport } from '@libp2p/circuit-relay-v2';
import { pubsubPeerDiscovery } from '@libp2p/pubsub-peer-discovery'
import { gossipsub } from '@chainsafe/libp2p-gossipsub'
import { bootstrapConfig } from '@heliau/bootstrappers';
import { config } from './constants';

export interface Libp2pHostOptions {
  libp2pOptions?: Partial<Libp2pOptions>;
  dhtOptions?: Partial<KadDHTInit>;
}

/**
 * Create a js-libp2p host with DHT, mDNS, relay, and peer discovery.
 * Compatible with browser and Node.js.
 */
export async function createLibp2pHost(
  opts: Libp2pHostOptions = {},
): Promise<Libp2p> {
  // Detect if we're in a browser environment
  const isBrowser = typeof window !== 'undefined';

  // Dynamically import Node.js-only modules
  let mdnsService: any = null;
  let tcpTransport: any = null;

  if (!isBrowser) {
    try {
      // Use string concatenation to hide import paths from static analysis
      const mdnsPath = '@libp2p' + '/' + 'mdns';
      const tcpPath = '@libp2p' + '/' + 'tcp';

      // Try regular dynamic imports first (works in test environments)
      // If that fails, fall back to eval-based imports (for production builds)
      let mdnsModule: any;
      let tcpModule: any;

      try {
        // First attempt: regular dynamic imports with dynamic paths
        [mdnsModule, tcpModule] = await Promise.all([
          import(/* @vite-ignore */ mdnsPath),
          import(/* @vite-ignore */ tcpPath)
        ]);
      } catch (dynamicImportError) {
        // Fallback: eval-based imports for bundled environments
        mdnsModule = await eval(`import("${mdnsPath}")`);
        tcpModule = await eval(`import("${tcpPath}")`);
      }

      mdnsService = mdnsModule.mdns({
        serviceTag: config.discovery.tag
      });
      tcpTransport = tcpModule.tcp();
    } catch (error) {
      console.warn('Failed to load Node.js-specific services:', error);
    }
  }

  // create the options object
  const libp2pConfig: Libp2pOptions = {
    addresses: {
      listen: isBrowser
        ? ["/p2p-circuit", "/webrtc"]
        : [
          "/ip4/0.0.0.0/tcp/0",
          "/ip6/::/tcp/0",
          "/p2p-circuit",
          "/webrtc"
        ]
    },
    transports: [
      circuitRelayTransport(),
      webSockets(),
      webRTC(),
      webRTCDirect(),
      webTransport(),
      ...(tcpTransport ? [tcpTransport] : [])
    ],
    connectionEncrypters: isBrowser ? [noise()] : [tls(), noise()],
    streamMuxers: [yamux()],
    peerDiscovery: [
      bootstrap({
        list: bootstrapConfig.list,
        tagName: config.discovery.tag
      }),
      pubsubPeerDiscovery({
        topics: [config.discovery.pubsubTopic],
      }),
      // mDNS only works in Node.js, not in browsers
      ...(mdnsService ? [mdnsService] : [])
    ],
    services: {
      autoNAT: autoNAT() as any,
      dcutr: dcutr() as any,
      dht: kadDHT({
        clientMode: true,
        ...opts.dhtOptions,
      }) as any,
      identify: identify(),
      identifyPush: identifyPush(),
      ping: ping(),
      pubsub: gossipsub() as any
    },
    ...opts.libp2pOptions,
  };

  // Create the libp2p node
  const libp2p = await createLibp2p(libp2pConfig);

  // Start the node
  await libp2p.start();
  return libp2p;
}
