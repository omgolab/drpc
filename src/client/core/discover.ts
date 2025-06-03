import { peerIdFromString } from "@libp2p/peer-id";
import { Multiaddr, multiaddr } from "@multiformats/multiaddr";
import { Libp2p } from "libp2p";

// Constants for default configuration
const DEFAULT_OPTIONS = {
    dialInterval: 10,
    relayRetryInterval: 2000,
    retryDelay: 1000,
    totalTimeout: 60000
} as const;

// Types for input address parsing
interface ParsedAddress {
    peerId: string;
    addr: Multiaddr | null;
    isCircuitRelay: boolean;
}

export interface DiscoveryOptions {
    dialInterval?: number;
    relayRetryInterval?: number;
    retryDelay?: number;
    totalTimeout?: number;
}

export interface DiscoveryResult {
    totalTime: number;
    connectTime: number;
    status: string;
    addr: Multiaddr | null;
    method: 'direct' | 'relay';
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
async function tryConnect(h: Libp2p, addr: Multiaddr, isCircuitRelay: boolean, startTime: number): Promise<DiscoveryResult | null> {
    const attemptStart = Date.now();
    try {
        const connection = await h.dial(addr);
        return connection?.remoteAddr ? {
            totalTime: (Date.now() - startTime) / 1000,
            connectTime: Date.now() - attemptStart,
            status: connection.status,
            addr: connection.remoteAddr,
            method: isCircuitRelay ? 'relay' : 'direct'
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

        dht?.findPeer?.(pid).catch(() => { });
        mdns?.queryForPeers?.().catch(() => { });
    } catch { }
}

/**
 * Discovers the optimal connection path for a libp2p peer using dual discovery methods.
 * 
 * Supports 4 input types:
 * 1. Circuit relay path: `/ip4/.../p2p/.../p2p-circuit/p2p/TARGET_PEER`
 * 2. P2P multiaddr only: `/p2p/TARGET_PEER` 
 * 3. Direct multiaddr: `/ip4/.../tcp/.../p2p/TARGET_PEER`
 * 4. Raw peer ID: `TARGET_PEER`
 * 
 * Uses dual discovery strategy:
 * - Circuit relay attempts (when available)
 * - Direct peer discovery via libp2p services (DHT, mDNS)
 * - Always falls back to discovered addresses for any peer ID input
 * 
 * @param h - Libp2p instance
 * @param input - Input address/peer ID in any of the 4 supported formats
 * @param options - Discovery configuration options
 * @returns Promise resolving to discovery result with connection details
 */
export async function discoverOptimalConnectPath(
    h: Libp2p,
    input: string,
    options: DiscoveryOptions = {}
): Promise<DiscoveryResult> {
    const {
        dialInterval = DEFAULT_OPTIONS.dialInterval,
        relayRetryInterval = DEFAULT_OPTIONS.relayRetryInterval,
        retryDelay = DEFAULT_OPTIONS.retryDelay,
        totalTimeout = DEFAULT_OPTIONS.totalTimeout
    } = options;

    const startTime = Date.now();
    let resolved = false;

    // Parse input to determine type and extract components
    const { peerId: targetPeerId, addr: parsedAddr, isCircuitRelay } = parseInputAddress(input);

    // Type 3: Try direct connection first if we have a direct address
    if (parsedAddr && !isCircuitRelay) {
        try {
            const directConnection = await h.dial(parsedAddr).catch(() => null);
            if (directConnection?.remoteAddr) {
                return {
                    totalTime: 0,
                    connectTime: 0,
                    status: directConnection.status,
                    addr: directConnection.remoteAddr,
                    method: 'direct'
                };
            }
        } catch {
            // If direct connection fails, fall back to discovery
        }
    }

    const pid = peerIdFromString(targetPeerId);

    return new Promise((resolve) => {
        let relayRetryTimer: number | null = null;

        const cleanup = () => {
            clearInterval(discoveryTrigger);
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

        // Always trigger initial discovery to populate peer store
        const triggerDiscovery = async () => {
            if (resolved) return;

            // First check if we already have addresses for this peer
            try {
                const peer = await h.peerStore.get(pid);
                if (peer?.addresses?.length > 0) {
                    for (const addr of peer.addresses) {
                        const result = await tryConnect(h, addr.multiaddr, false, startTime).catch(() => null);
                        if (result) {
                            resolveOnce(result);
                            return;
                        }
                    }
                }
            } catch {
            // Peer not in store, continue with discovery
            }

            // Try direct dial to trigger discovery
            h.dial(pid).catch(() => { });

            // Force discovery services to actively search
            manageServices(h, pid, false);
        };

        // Start discovery immediately for background operation
        triggerDiscovery();

        const attemptConnection = async (addr: Multiaddr, isCircuitRelay: boolean) => {
            if (resolved) return null;
            try {
                const result = await tryConnect(h, addr, isCircuitRelay, startTime);
                if (result) {
                    resolveOnce(result);
                    return result;
                }
            } catch (error) {
                // Silently continue - circuit relay errors are handled by tryConnect
            }
            return null;
        };

        const tryCircuitRelay = async () => {
            if (!parsedAddr || !isCircuitRelay) return;
            await attemptConnection(parsedAddr, true);
        };

        const restartDiscoveryServices = () => manageServices(h, pid, true);

        // Start circuit relay attempts (only if available)
        if (isCircuitRelay && parsedAddr) {
            tryCircuitRelay();
            relayRetryTimer = setInterval(tryCircuitRelay, relayRetryInterval) as any;
        }

        // Continue discovery at regular intervals
        const discoveryTrigger = setInterval(triggerDiscovery, dialInterval);

        const discoveryHandler = async (event: any) => {
            if (resolved || !event.detail.id.equals(pid)) return;

            const multiaddrs = event.detail.multiaddrs || [];
            if (multiaddrs.length === 0) {
                await restartDiscoveryServices();
                await new Promise(resolve => setTimeout(resolve, retryDelay));
                return;
            }

            const results = await Promise.allSettled(
                multiaddrs.map((ma: Multiaddr) => attemptConnection(ma, false).catch(() => null))
            );

            const successfulResult = results
                .filter((r): r is PromiseFulfilledResult<DiscoveryResult> => r.status === 'fulfilled' && r.value?.addr)
                .map(r => r.value)
                .sort((a, b) => a.connectTime - b.connectTime)[0];

            if (successfulResult) {
                resolveOnce(successfulResult);
            } else {
                await restartDiscoveryServices();
                await new Promise(resolve => setTimeout(resolve, retryDelay));
            }
        };

        h.addEventListener('peer:discovery', discoveryHandler);

        const totalTimeoutTimer = setTimeout(() => {
            if (!resolved) {
                resolveOnce({
                    totalTime: (Date.now() - startTime) / 1000,
                    connectTime: 0,
                    status: 'timeout',
                    addr: null,
                    method: 'direct'
                });
            }
        }, totalTimeout);
    });
}
