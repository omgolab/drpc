// Ported from Go pkg/core/host.go to TypeScript for browser and Node.js using js-libp2p

import { createLibp2p, Libp2p, Libp2pOptions } from 'libp2p'
import { kadDHT, KadDHTInit } from '@libp2p/kad-dht'
import { mdns } from '@libp2p/mdns'
import { webSockets } from '@libp2p/websockets'
import { tcp } from '@libp2p/tcp'
import { noise } from '@chainsafe/libp2p-noise'
import { mplex } from '@libp2p/mplex'
import { identify } from '@libp2p/identify'
import { autoNAT } from '@libp2p/autonat'
import { EventEmitter } from 'events'
import { ping } from '@libp2p/ping'

export interface CreateLibp2pHostOptions {
  logger?: { info: (...args: any[]) => void; debug?: (...args: any[]) => void; error?: (...args: any[]) => void }
  libp2pOptions?: Partial<Libp2pOptions>
  dhtOptions?: Partial<KadDHTInit>
  isClientMode?: boolean
}

export interface Libp2pHostResult {
  libp2p: Libp2p
  peerDiscovery: EventEmitter
  relayManager: { enabled: boolean }
}

/**
 * Create a js-libp2p host with DHT, mDNS, relay, and peer discovery.
 * Compatible with browser and Node.js.
 */
export async function createLibp2pHost(opts: CreateLibp2pHostOptions = {}): Promise<Libp2pHostResult> {
  const logger = opts.logger || console
  const isClientMode = !!opts.isClientMode

  // Peer discovery event emitter
  const peerDiscovery = new EventEmitter()

  // DHT config
  const dhtConfig: Partial<KadDHTInit> = {
    clientMode: isClientMode,
    ...opts.dhtOptions,
  }

  // Transport selection
  const transports = [webSockets()]
  if (typeof window === 'undefined') {
    transports.push(tcp())
  }

  // Compose libp2p options
  const libp2pConfig: Libp2pOptions = {
    transports,
    streamMuxers: [mplex()],
    connectionEncrypters: [noise()],
    peerDiscovery: [
      mdns(),
    ],
    services: ({
      identify: identify(),
      ping: ping(),
      dht: kadDHT(dhtConfig),
      autonat: autoNAT(),
      // Relay is enabled by default in js-libp2p >=0.43, no explicit relay service needed here
    } as any),
    ...opts.libp2pOptions,
  }

  // Create the libp2p node
  const libp2p = await createLibp2p(libp2pConfig)

  // Setup peer discovery event forwarding
  libp2p.addEventListener('peer:discovery', (evt: any) => {
    peerDiscovery.emit('peer', evt.detail)
    logger.debug?.('Discovered peer', evt.detail.id?.toString?.())
  })

  // Start the node
  await libp2p.start()
  logger.info('Libp2p host started with id:', libp2p.peerId.toString())
  logger.info('Listening on addresses:', libp2p.getMultiaddrs().map(a => a.toString()))

  // Optionally advertise on DHT (js-libp2p does not have direct advertise API, but can use DHT for peer routing)
  // Optionally, implement periodic peer discovery as in Go if needed

  return {
    libp2p,
    peerDiscovery,
    relayManager: { enabled: true }
  }
}