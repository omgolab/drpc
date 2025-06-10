/**
 * P2P address caching for HTTP transport with TTL and LRU eviction
 */

interface CacheEntry {
    value: string;
    timestamp: number;
    lastAccessed: number;
}

interface CacheOptions {
    maxSize: number;
    ttlMs: number;
}

class TTLCache {
    private cache = new Map<string, CacheEntry>();
    private options: CacheOptions;

    constructor(options: CacheOptions = { maxSize: 100, ttlMs: 5 * 60 * 1000 }) {
        this.options = options;
    }

    get(key: string): string | undefined {
        const entry = this.cache.get(key);
        if (!entry) {
            return undefined;
        }

        const now = Date.now();

        // Check if entry has expired
        if (now - entry.timestamp > this.options.ttlMs) {
            this.cache.delete(key);
            return undefined;
        }

        // Update last accessed time for LRU
        entry.lastAccessed = now;
        return entry.value;
    }

    set(key: string, value: string): void {
        const now = Date.now();

        // If cache is at max size, remove least recently used item
        if (this.cache.size >= this.options.maxSize && !this.cache.has(key)) {
            this.evictLRU();
        }

        this.cache.set(key, {
            value,
            timestamp: now,
            lastAccessed: now,
        });
    }

    private evictLRU(): void {
        let oldestKey: string | undefined;
        let oldestTime = Infinity;

        this.cache.forEach((entry, key) => {
            if (entry.lastAccessed < oldestTime) {
                oldestTime = entry.lastAccessed;
                oldestKey = key;
            }
        });

        if (oldestKey) {
            this.cache.delete(oldestKey);
        }
    }

    clear(): void {
        this.cache.clear();
    }

    size(): number {
        return this.cache.size;
    }

    // Cleanup expired entries
    cleanup(): void {
        const now = Date.now();
        const keysToDelete: string[] = [];

        this.cache.forEach((entry, key) => {
            if (now - entry.timestamp > this.options.ttlMs) {
                keysToDelete.push(key);
            }
        });

        keysToDelete.forEach(key => this.cache.delete(key));
    }
}

// Global cache instance for HTTP URL to p2p multiaddress mappings
const p2pAddrCache = new TTLCache({
    maxSize: 100,        // Maximum 100 entries
    ttlMs: 5 * 60 * 1000 // 5 minutes TTL
});

/**
 * Get cached p2p address for an HTTP URL
 */
export function getCachedP2pAddr(httpUrl: string): string | undefined {
    return p2pAddrCache.get(httpUrl);
}

/**
 * Cache a p2p address for an HTTP URL
 */
export function cacheP2pAddr(httpUrl: string, p2pAddr: string): void {
    p2pAddrCache.set(httpUrl, p2pAddr);
}

/**
 * Clear the entire cache
 */
export function clearP2pAddrCache(): void {
    p2pAddrCache.clear();
}

/**
 * Get the size of the cache
 */
export function getCacheSize(): number {
    return p2pAddrCache.size();
}

/**
 * Clean up expired entries from the cache
 */
export function cleanupCache(): void {
    p2pAddrCache.cleanup();
}
