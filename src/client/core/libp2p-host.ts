import { createLibp2p, Libp2pOptions, type Libp2p } from 'libp2p';
import { webSockets } from '@libp2p/websockets';
import { webRTC, webRTCDirect } from '@libp2p/webrtc';
import { tcp } from '@libp2p/tcp';
import { webTransport } from '@libp2p/webtransport';
import { noise } from '@chainsafe/libp2p-noise';
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
import { mdns } from '@libp2p/mdns'
// import { bootstrapConfig } from '@heliau/bootstrappers';
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
  // create the options object
  const libp2pConfig: Libp2pOptions = {
    addresses: {
      listen: [
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
      tcp()
    ],
    connectionEncrypters: [noise()],
    streamMuxers: [yamux()],
    peerDiscovery: [
      bootstrap({
        // list: bootstrapConfig.list,
        list: [
          '/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN',
          '/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb',
          '/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt',
          '/dnsaddr/va1.bootstrap.libp2p.io/p2p/12D3KooWKnDdG3iXw9eTFijk3EWSunZcFi54Zka4wmtqtt6rPxc8',
          '/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ'
        ],
        tagName: config.discovery.tag
      }),
      pubsubPeerDiscovery({
        topics: [config.discovery.pubsubTopic],
      }),
      mdns({
        serviceTag: config.discovery.tag
      })
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
      pubsub: gossipsub()
    },
    ...opts.libp2pOptions,
  };

  // Create the libp2p node
  const libp2p = await createLibp2p(libp2pConfig);

  // Start the node
  await libp2p.start();
  return libp2p;
}
