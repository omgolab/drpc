import { peerIdFromString } from "@libp2p/peer-id";
import { Multiaddr, multiaddr } from "@multiformats/multiaddr";
import { Libp2p } from "libp2p";


// Constants for default configuration
const DEFAULT_OPTIONS = {
    standardInterval: 400, // Used for fast-path, peer-store, relay-retry, and retry delay
    dialTimeout: 30000,
    totalTimeout: 60000
} as const;

// Types for input address parsing
interface ParsedAddress {
    peerId: string;
    addr: Multiaddr | null;
    isCircuitRelay: boolean;
}

// Discovery method types
export type DiscoveryMethod = 'fast-path' | 'peer-discovery' | 'circuit-relay';

export interface DiscoveryOptions {
    standardInterval?: number; // Unified interval for fast-path, peer-store, relay-retry, and retry delay
    discoveryInterval?: number;
    dialTimeout?: number;
    totalTimeout?: number;
}

export interface DiscoveryResult {
    totalTime: number;
    connectTime: number;
    status: string;
    addr: Multiaddr | null;
    method: DiscoveryMethod;
    trackDescription: string;
}

// Helper function for input parsing with simplified logic
function parseInputAddress(input: string): ParsedAddress {
    const isCircuitRelay = input.includes('/p2p-circuit/');
    const peerId = input.startsWith('/p2p/') ? input.replace('/p2p/', '') : input.split('/').pop()!;

    let addr: Multiaddr | null = null;
    if (isCircuitRelay) {
        addr = multiaddr(input);
    } else if (input.includes('/p2p/') && !input.startsWith('/p2p/')) {
        addr = multiaddr(input);
    }

    return {
        peerId,
        addr,
        isCircuitRelay
    };
}

// Unified helper for all connection operations
async function tryConnect(
    h: Libp2p,
    addr: Multiaddr,
    isCircuitRelay: boolean,
    startTime: number,
    method: DiscoveryMethod,
    trackDescription: string,
    dialTimeout: number
): Promise<DiscoveryResult | null> {
    const attemptStart = Date.now();
    try {
        const connection = await h.dial(addr, { signal: AbortSignal.timeout(dialTimeout) });
        return connection?.status === 'open' ? {
            totalTime: (Date.now() - startTime) / 1000,
            connectTime: Date.now() - attemptStart,
            status: connection.status,
            addr: connection.remoteAddr,
            method,
            trackDescription
        } : null;
    } catch (error: any) {
        if (isCircuitRelay && error?.code === 'ERR_NO_RESERVATION') return null;
        throw error;
    }
}

// Unified service management with operation mapping
async function manageServices(h: Libp2p, pid: any, shouldRestart: boolean): Promise<void> {
    try {
        const dht = h.services?.dht as any;
        const mdns = h.services?.mdns as any;

        if (shouldRestart) {
            await h.peerStore.delete(pid);
            if (mdns?.start) { await mdns.stop(); await mdns.start(); }
            if (dht?.refreshRoutingTable) await dht.refreshRoutingTable();
        }

        dht?.findPeer(pid).catch(() => { });
        mdns?.queryForPeers().catch(() => { });
    } catch { }
}

/**
 * Discovers the optimal connection path for a libp2p peer using parallel discovery tracks.
 * 
 * Supports 4 input formats:
 * 1. Circuit relay: `/ip4/.../p2p/.../p2p-circuit/p2p/TARGET_PEER`
 * 2. P2P only: `/p2p/TARGET_PEER` or `TARGET_PEER`
 * 3. Direct: `/ip4/.../tcp/.../p2p/TARGET_PEER`
 * 
 * Uses 5 parallel discovery tracks:
 * - Fast Path: Direct connection for known addresses
 * - PeerStore: Cached address polling  
 * - Active Search: DHT/mDNS activation with direct dial attempts
 * - Circuit Relay: Relay-specific connections
 * - Event-Driven: Reactive peer discovery events
 * 
 * @param h - Libp2p instance
 * @param input - Address/peer ID in any supported format
 * @param options - Discovery configuration options
 * @returns Discovery result with connection details and timing
 */
export async function discoverOptimalConnectPath(
    h: Libp2p,
    input: string,
    options: DiscoveryOptions = {}
): Promise<DiscoveryResult> {
    const {
        standardInterval = DEFAULT_OPTIONS.standardInterval,
        dialTimeout = DEFAULT_OPTIONS.dialTimeout,
        totalTimeout = DEFAULT_OPTIONS.totalTimeout
    } = options;

    const startTime = Date.now();
    let resolved = false;

    // Parse input to determine type and extract components
    const { peerId: targetPeerId, addr: parsedAddr, isCircuitRelay } = parseInputAddress(input);
    const pid = peerIdFromString(targetPeerId);

    return new Promise((resolve) => {
        let relayRetryTimer: number | null = null;
        let fastPathTimer: number;
        let peerStoreTimer: number;
        let discoveryTriggerTimer: number;

        const cleanup = () => {
            clearInterval(fastPathTimer);
            clearInterval(peerStoreTimer);
            clearInterval(discoveryTriggerTimer);
            if (relayRetryTimer) clearInterval(relayRetryTimer);
            clearTimeout(totalTimeoutTimer);
            h.removeEventListener('peer:discovery', discoveryHandler);
        };

        const resolveOnce = (result: DiscoveryResult) => {
            if (!resolved) {
                resolved = true;
                cleanup();
                resolve(result);
            }
        };

        const attemptConnections = async (addrs: Multiaddr | Multiaddr[], method: DiscoveryMethod, trackDescription: string) => {
            if (resolved) return null;

            const addresses = Array.isArray(addrs) ? addrs : [addrs];
            if (addresses.length === 0) return null;

            const results = await Promise.allSettled(
                addresses.map(addr => tryConnect(h, addr, addr.toString().includes('/p2p-circuit/'), startTime, method, trackDescription, dialTimeout).catch(() => null))
            );

            const successfulResult = results
                .filter((r): r is PromiseFulfilledResult<DiscoveryResult> =>
                    r.status === 'fulfilled' && r.value !== null && r.value?.addr !== null
                )
                .map(r => r.value)
                .sort((a, b) => a.connectTime - b.connectTime)[0];

            if (successfulResult) {
                resolveOnce(successfulResult);
                return successfulResult;
            }
            return null;
        };

        // Discovery track implementations
        const tracks = {
            // Track 1: Fast Path - Direct Address Connection
            fastPath: async () => {
                if (resolved || !parsedAddr || isCircuitRelay) return;
                await attemptConnections(parsedAddr, 'fast-path', 'Fast Path - Direct connection');
            },

            // Track 2: PeerStore Polling
            peerStore: async () => {
                if (resolved) return;
                try {
                    const peer = await h.peerStore.get(pid);
                    if (peer?.addresses?.length > 0) {
                        await attemptConnections(
                            peer.addresses.map(addr => addr.multiaddr),
                            'peer-discovery',
                            'Peer Discovery - Cached addresses'
                        );
                    }
                } catch {
                    // Peer not in store, continue
                }
            },

            // Track 3: Active Search
            activeSearch: async () => {
                if (resolved) return;

                // Try direct dial - might succeed immediately
                try {
                    const connection = await h.dial(pid, { signal: AbortSignal.timeout(dialTimeout) });
                    if (connection?.status === 'open' && connection?.remoteAddr) {
                        resolveOnce({
                            totalTime: (Date.now() - startTime) / 1000,
                            connectTime: 0,
                            status: connection.status,
                            addr: connection.remoteAddr,
                            method: 'peer-discovery',
                            trackDescription: 'Active Search - Direct dial success'
                        });
                        return;
                    }
                } catch {
                    // Failed, continue with service activation
                }

                // Activate discovery services
                manageServices(h, pid, false);
            },

            // Track 4: Circuit Relay
            circuitRelay: async () => {
                if (!parsedAddr || !isCircuitRelay) return;
                await attemptConnections(parsedAddr, 'circuit-relay', 'Circuit Relay - Relay connection');
            }
        };

        // Track 5: Event-Driven Discovery - peer:discovery handler
        const discoveryHandler = async (event: any) => {
            if (resolved || !event.detail.id.equals(pid)) return;

            console.log(`Found addrs: |${event.detail.multiaddrs.length}|${event.detail.multiaddrs.map((addr: any) => addr.toString()).join(", ")}`);

            const multiaddrs = event.detail.multiaddrs || [];
            if (multiaddrs.length > 0) {
                // Try to connect to newly discovered addresses
                const result = await attemptConnections(
                    multiaddrs,
                    'peer-discovery',
                    'Event-Driven - Discovery events'
                );

                if (!result) {
                    // Connection failed, restart services after delay
                    await new Promise(resolve => setTimeout(resolve, standardInterval));
                    manageServices(h, pid, true);
                }
            }
        };

        // Helper to start track with immediate execution + interval
        const startTrackWithInterval = (trackFn: () => Promise<void>, interval: number): number => {
            trackFn(); // Immediate execution
            return setInterval(trackFn, interval) as unknown as number;
        };

        // Initialize and start all discovery tracks
        const initializeAndStartTracks = () => {
            // Track 1: Fast Path - Only if we have a direct address
            if (parsedAddr && !isCircuitRelay) {
                fastPathTimer = startTrackWithInterval(tracks.fastPath, standardInterval);
            }

            // Track 2: PeerStore - Always active
            peerStoreTimer = startTrackWithInterval(tracks.peerStore, standardInterval);

            // Track 3: Active Search - Always active  
            discoveryTriggerTimer = startTrackWithInterval(tracks.activeSearch, standardInterval);

            // Track 4: Circuit Relay - Only if circuit relay address
            if (isCircuitRelay && parsedAddr) {
                relayRetryTimer = startTrackWithInterval(tracks.circuitRelay, standardInterval);
            }

            // Track 5: Event-driven discovery
            h.addEventListener('peer:discovery', discoveryHandler);
        };

        // Start all tracks
        initializeAndStartTracks();

        const totalTimeoutTimer = setTimeout(() => {
            if (!resolved) {
                resolveOnce({
                    totalTime: (Date.now() - startTime) / 1000,
                    connectTime: 0,
                    status: 'timeout',
                    addr: null,
                    method: 'peer-discovery',
                    trackDescription: 'Discovery Timeout - No successful connection'
                });
            }
        }, totalTimeout);
    });
}
