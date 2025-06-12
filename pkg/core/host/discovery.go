package host

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pmdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/omgolab/drpc/pkg/config"
)

var (
	_ libp2pmdns.Notifee = (*discoveryNotifee)(nil) // Ensure discoveryNotifee implements the Notifee interface
)

// Peer connection cache to reduce discovery overhead in production
type peerCache struct {
	mu      sync.RWMutex
	entries map[peer.ID]*peerCacheEntry
	maxSize int
	ttl     time.Duration
}

type peerCacheEntry struct {
	addrInfo  peer.AddrInfo
	timestamp time.Time
	attempts  int
}

var (
	// Global peer cache for optimizing repeated connections
	globalPeerCache = &peerCache{
		entries: make(map[peer.ID]*peerCacheEntry),
		maxSize: 1000,             // Maximum cached peers
		ttl:     10 * time.Minute, // Cache TTL
	}
)

// addToCache adds a peer to the discovery cache
func (pc *peerCache) addToCache(pi peer.AddrInfo) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Clean up expired entries if cache is getting full
	if len(pc.entries) >= pc.maxSize {
		pc.cleanExpiredLocked()
	}

	pc.entries[pi.ID] = &peerCacheEntry{
		addrInfo:  pi,
		timestamp: time.Now(),
		attempts:  0,
	}
}

// getFromCache retrieves a peer from the cache if it's still valid
func (pc *peerCache) getFromCache(peerID peer.ID) (peer.AddrInfo, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	entry, exists := pc.entries[peerID]
	if !exists {
		return peer.AddrInfo{}, false
	}

	// Check if entry is still valid
	if time.Since(entry.timestamp) > pc.ttl {
		return peer.AddrInfo{}, false
	}

	return entry.addrInfo, true
}

// markAttempt records a connection attempt for rate limiting
func (pc *peerCache) markAttempt(peerID peer.ID) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	entry, exists := pc.entries[peerID]
	if !exists {
		return true // Allow attempt if not cached
	}

	entry.attempts++

	// Rate limit: max 3 attempts per TTL period
	return entry.attempts <= 3
}

// cleanExpiredLocked removes expired entries (must be called with write lock)
func (pc *peerCache) cleanExpiredLocked() {
	now := time.Now()
	for id, entry := range pc.entries {
		if now.Sub(entry.timestamp) > pc.ttl {
			delete(pc.entries, id)
		}
	}
}

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	h   host.Host
	cfg *hostCfg
}

// HandlePeerFound connects to peers discovered via mDNS. On error, just log.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	// Skip connecting to self
	if pi.ID == n.h.ID() {
		return
	}

	// Check rate limiting for this peer
	if !globalPeerCache.markAttempt(pi.ID) {
		// Too many recent attempts, skip this connection
		return
	}

	// Check if we're already connected to this peer (with nil check for testing)
	if n.h.Network() != nil && n.h.Network().Connectedness(pi.ID) == network.Connected {
		return
	}

	// Check cache before attempting to connect
	cachedInfo, found := globalPeerCache.getFromCache(pi.ID)
	if found {
		pi = cachedInfo
	} else {
		// Add peer to cache
		globalPeerCache.addToCache(pi)
	}

	// Create a context with a timeout for the connection attempt
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := n.h.Connect(ctx, pi)
	if err != nil {
		// Don't log errors for transient connection issues, use Debug
		// n.cfg.logger.Debug(fmt.Sprintf("Failed connecting to mDNS peer %s: %s", pi.ID.String(), err.Error()))
		return
	}
	// AddPeer call removed - libp2p AutoRelay handles relay discovery automatically
	// n.cfg.logger.Info(fmt.Sprintf("Connected to peer via mDNS: %s", pi.ID.String()))
}

// setupMDNS initializes the mDNS discovery service
func setupMDNS(h host.Host, cfg *hostCfg) error {
	// Setup mDNS discovery service
	cfg.logger.Info("Setting up mDNS discovery")
	notifee := &discoveryNotifee{h: h, cfg: cfg}
	// Use DefaultServiceTag if config.DISCOVERY_TAG is empty
	tag := config.DISCOVERY_TAG
	if tag == "" {
		// libp2pmdns handles empty string as default tag internally
		tag = ""
		cfg.logger.Warn("config.DISCOVERY_TAG is empty, using default mDNS tag")
	}
	cfg.logger.Debug(fmt.Sprintf("Using mDNS tag: %s", tag))
	disc := libp2pmdns.NewMdnsService(h, tag, notifee)
	return disc.Start()
}
