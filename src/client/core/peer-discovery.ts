import { type Libp2p } from 'libp2p';
import { peerIdFromString } from '@libp2p/peer-id';
import { Multiaddr, multiaddr } from '@multiformats/multiaddr';
import type { PeerId } from '@libp2p/interface';

/**
 * Result of a connection attempt including metadata about the connection process
 */
export interface ConnectionResult {
    /** Whether the connection was successfully established */
    success: boolean;
    /** The multiaddress used for successful connection, if applicable */
    multiaddr?: Multiaddr;
    /** The method that successfully established the connection */
    method?: 'already-connected' | 'direct-peer-id' | 'direct-multiaddr' | 'discovered-address';
    /** Error message if connection failed */
    error?: string;
    /** Total time elapsed during the connection attempt in seconds */
    totalTimeSeconds: number;
}

/**
 * Configuration options for peer search and connection behavior
 */
export interface SearchOptions {
    /** Maximum time to wait for connection establishment in milliseconds */
    timeoutMs?: number;
    /** Timeout for individual dial attempts in milliseconds */
    dialTimeoutMs?: number;
    /** Interval between connection retry attempts in milliseconds */
    connectIntervalMs?: number;
}

/**
 * Attempts to establish a libp2p connection to a target peer using ambient relay discovery.
 * 
 * This function implements a comprehensive connection strategy with intelligent fallback mechanisms:
 * 
 * **Connection Strategy (in order of preference):**
 * 1. **Already Connected Check**: Verifies if the target peer is already in the connection pool
 * 2. **Direct Multiaddr**: If input is a multiaddress, attempts direct connection first
 * 3. **Discovered Addresses**: Uses libp2p peer store to find known addresses for the target
 * 4. **Direct Peer ID**: Falls back to libp2p's built-in peer discovery via peer ID
 * 5. **Ambient Discovery**: Continuously discovers new peers through libp2p events to find relay paths
 * 
 * @param h - The libp2p node instance used for connections and peer discovery
 * @param targetPeerIdStr - Target peer identifier (peer ID string or multiaddress with /p2p/ component)
 * @param options - Configuration options for connection behavior and timeouts
 * @returns Promise resolving to detailed connection result with success status and metadata
 * 
 * @example
 * ```typescript
 * // Connect using peer ID with default options
 * const result = await discoverOptimalConnection(libp2pNode, "12D3KooW...");
 * 
 * // Connect with custom timeout and dial settings
 * const result = await discoverOptimalConnection(libp2pNode, "12D3KooW...", {
 *   timeoutMs: 15000,
 *   dialTimeoutMs: 2000,
 *   connectIntervalMs: 200
 * });
 * 
 * // Connect using multiaddress
 * const result = await discoverOptimalConnection(
 *   libp2pNode, 
 *   "/ip4/127.0.0.1/tcp/4001/p2p/12D3KooW..."
 * );
 * 
 * if (result.success) {
 *   console.log(`Connected via ${result.method} in ${result.totalTimeSeconds}s`);
 *   if (result.multiaddr) {
 *     console.log(`Using address: ${result.multiaddr}`);
 *   }
 * } else {
 *   console.error(`Connection failed: ${result.error}`);
 * }
 * ```
 */
export async function discoverOptimalConnection(
    h: Libp2p,
    targetPeerIdStr: string,
    options: SearchOptions = {}
): Promise<ConnectionResult> {
    const {
        timeoutMs = 60000,        // 60 second default timeout
        dialTimeoutMs = 1000,     // 1 second per dial attempt
        connectIntervalMs = 100   // Check every 100ms
    } = options;

    const { peerId: targetPeerID, originalMultiaddr } = extractPeerIdFromInput(targetPeerIdStr);
    const startTime = Date.now();
    const attempted = new Set<string>(); // Track attempted addresses to prevent duplicates
    let result: ConnectionResult | null = null;

    // Helper function to calculate elapsed time in seconds
    const getTimeSeconds = () => (Date.now() - startTime) / 1000;

    // Strategy 1: If input was a multiaddress, try direct connection first
    // This is often the fastest path if the address is directly reachable
    if (originalMultiaddr && originalMultiaddr.tuples().length > 1) {
        const directResult = await tryMultiaddressConnect(h, originalMultiaddr, getTimeSeconds, dialTimeoutMs);
        if (directResult) {
            return directResult;
        }
    }

    // Main connection logic that tries multiple strategies
    const connect = async (): Promise<ConnectionResult | null> => {
        if (result) return result; // Early return if already resolved

        // Strategy 2: Check if already connected
        // Cache the peers array to avoid repeated function calls in hot path
        const currentPeers = h.getPeers();
        if (currentPeers.some(peer => peer.equals(targetPeerID))) {
            result = { success: true, method: 'already-connected' as const, totalTimeSeconds: getTimeSeconds() };
            return result;
        }

        // Strategy 3: Try addresses discovered through peer store
        const discoveredResult = await tryDiscoveredAddresses(h, targetPeerID, attempted, getTimeSeconds, dialTimeoutMs);
        if (discoveredResult) {
            result = discoveredResult;
            return result;
        }

        // Strategy 4: Fallback to direct peer ID dialing
        // Let libp2p handle discovery through its built-in mechanisms
        try {
            await h.dial(targetPeerID, { signal: AbortSignal.timeout(dialTimeoutMs) });
            result = { success: true, method: 'direct-peer-id' as const, totalTimeSeconds: getTimeSeconds() };
            return result;
        } catch {
            // Silent catch - this is expected to fail in many cases
        }

        return null;
    };

    // Set up the discovery and retry mechanism
    return new Promise(resolve => {
        let resolved = false;

        // Ensure we only resolve once to prevent race conditions
        const resolveOnce = (res: ConnectionResult) => {
            if (!resolved) {
                resolved = true;
                h.removeEventListener('peer:discovery', onPeer);
                if (intervalId) clearInterval(intervalId);
                resolve(res);
            }
        };

        // Strategy 5: Handle peer discovery events for ambient relay discovery
        // This is where the magic happens for NAT traversal and relay connections
        const onPeer = (evt: any) => {
            if (resolved) return; // Early return to avoid unnecessary work

            const peer = evt.detail;
            if (peer.id.equals(targetPeerID)) {
                // Found our target peer! Update peer store with its addresses
                process.stdout.write('|'); // Visual indicator for target discovery
                h.peerStore.merge(peer.id, { multiaddrs: peer.multiaddrs });
            } else {
                // Found a potential relay peer - attempt connection
                process.stdout.write('.'); // Visual indicator for relay discovery
                h.dial(peer.id).catch(() => { }); // Silent failure is expected
            }
        };

        h.addEventListener('peer:discovery', onPeer);

        // Periodic connection attempts - gives discovered peers time to be processed
        const intervalId = setInterval(() => {
            if (resolved) return;
            connect().then(res => res && resolveOnce(res)).catch(() => { });
        }, connectIntervalMs);

        // Timeout protection - prevents indefinite waiting
        setTimeout(() => {
            if (intervalId) clearInterval(intervalId);
            resolveOnce({
                success: false,
                error: `Connection timeout after ${timeoutMs}ms`,
                totalTimeSeconds: getTimeSeconds()
            });
        }, timeoutMs);

        // Initial connection attempt - may succeed immediately if peer is already known
        connect().then(res => res && resolveOnce(res)).catch(() => { });
    });
}

/**
 * Extracts and validates peer ID from various input formats.
 * 
 * Supports two input formats:
 * 1. Direct peer ID string (e.g., "12D3KooW...")
 * 2. Multiaddress with peer ID component (e.g., "/ip4/.../tcp/.../p2p/12D3KooW...")
 * 
 * @param input - Peer identifier string in supported format
 * @returns Object containing the extracted peer ID and original multiaddress if applicable
 * @throws Error if input format is invalid or peer ID cannot be extracted
 */
function extractPeerIdFromInput(input: string): { peerId: PeerId; originalMultiaddr?: Multiaddr } {
    try {
        // Strategy 1: Try parsing as direct peer ID string
        const peerId = peerIdFromString(input);
        return { peerId };
    } catch {
        try {
            // Strategy 2: Parse as multiaddress and extract embedded peer ID
            const ma = multiaddr(input);
            const peerIdStr = ma.getPeerId();

            if (!peerIdStr) {
                throw new Error('No peer ID found in multiaddress');
            }

            const peerId = peerIdFromString(peerIdStr);
            return { peerId, originalMultiaddr: ma };
        } catch (error) {
            throw new Error(`Invalid peer ID format: ${input}. Expected peer ID string or multiaddress with /p2p/ component. Error: ${error instanceof Error ? error.message : String(error)}`);
        }
    }
}

/**
 * Attempts direct connection to a specific multiaddress.
 * 
 * This is used when the input is a complete multiaddress, allowing for
 * immediate connection attempts without discovery overhead.
 * 
 * @param h - libp2p node instance
 * @param multiAddr - Target multiaddress for connection
 * @param getTimeSeconds - Function to calculate elapsed time
 * @param dialTimeout - Maximum time to wait for dial completion
 * @returns Connection result if successful, null if failed
 */
async function tryMultiaddressConnect(
    h: Libp2p,
    multiAddr: Multiaddr,
    getTimeSeconds: () => number,
    dialTimeout: number
): Promise<ConnectionResult | null> {
    try {
        // Check if address is dialable before attempting connection
        if (await h.isDialable(multiAddr)) {
            await h.dial(multiAddr, { signal: AbortSignal.timeout(dialTimeout) });
            return {
                success: true,
                method: 'direct-multiaddr' as const,
                multiaddr: multiAddr,
                totalTimeSeconds: getTimeSeconds()
            };
        }
    } catch (err) {
        // Expected to fail in many cases - return null to try other strategies
    }

    return null;
}

/**
 * Attempts connections using addresses discovered through the libp2p peer store.
 * 
 * This function handles the core logic for connecting to peers using previously
 * discovered addresses. It implements several optimizations:
 * - Deduplication to avoid repeated attempts
 * - Address prioritization (localhost first)
 * - Parallel connection attempts for speed
 * 
 * @param h - libp2p node instance
 * @param targetPeerID - Peer ID of the target peer
 * @param attempted - Set of already attempted addresses to avoid duplicates
 * @param getTimeSeconds - Function to calculate elapsed time
 * @param dialTimeout - Maximum time per individual dial attempt
 * @returns Connection result if successful, null if all attempts failed
 */
async function tryDiscoveredAddresses(
    h: Libp2p,
    targetPeerID: PeerId,
    attempted: Set<string>,
    getTimeSeconds: () => number,
    dialTimeout: number
): Promise<ConnectionResult | null> {
    try {
        // Retrieve peer information from the peer store
        const peer = await h.peerStore.get(targetPeerID);
        const targetPeerIdString = targetPeerID.toString(); // Cache to avoid repeated calls

        // Process only new addresses to prevent duplicate attempts
        const newAddresses = peer.addresses
            .filter(paddr => {
                const originalStr = paddr.multiaddr.toString();
                if (attempted.has(originalStr)) return false;
                attempted.add(originalStr); // Mark as attempted
                return true;
            })
            // Encapsulate with peer ID to create complete dialable addresses
            .map(paddr => paddr.multiaddr.encapsulate(`/p2p/${targetPeerIdString}`))
            // Sort to prioritize local addresses for faster connection
            .sort((a, b) => {
                const aStr = a.toString();
                const bStr = b.toString();

                // Efficient localhost detection using indexOf (faster than regex)
                const aIsLocal = aStr.indexOf('127.0.0.1') !== -1 || aStr.indexOf('::1') !== -1;
                const bIsLocal = bStr.indexOf('127.0.0.1') !== -1 || bStr.indexOf('::1') !== -1;

                // Prioritize local addresses: local < remote
                return aIsLocal === bIsLocal ? 0 : aIsLocal ? -1 : 1;
            });

        // Early return if no new addresses to try
        if (newAddresses.length === 0) {
            return null;
        }

        // Create parallel connection attempts for all new addresses
        // This significantly improves connection speed in multi-address scenarios
        const connectionPromises = newAddresses.map(async (addr) => {
            try {
                // Verify address is dialable before attempting connection
                if (await h.isDialable(addr)) {
                    await h.dial(addr, { signal: AbortSignal.timeout(dialTimeout) });
                }
                return {
                    success: true,
                    method: 'discovered-address' as const,
                    multiaddr: addr,
                    totalTimeSeconds: getTimeSeconds()
                };
            } catch (err) {
                throw err; // Re-throw to be handled by Promise.any
            }
        });

        // Wait for the first successful connection
        // Promise.any resolves with the first successful promise
        try {
            return await Promise.any(connectionPromises);
        } catch (aggregateError) {
            // All connection attempts failed - this is expected in many scenarios
        }
    } catch (error) {
        // Peer not found in store or other error - return null to try other strategies
        return null;
    }

    return null;
}
