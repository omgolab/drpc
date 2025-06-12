// Package relay provides relay peer management for libp2p hosts
//
// IMPORTANT: This package is currently UNUSED in the codebase.
// libp2p's built-in AutoRelay functionality handles all relay operations automatically,
// making this custom relay manager redundant. The file is preserved for reference
// but all usage has been removed from the codebase to eliminate overhead and
// potential memory leaks from background maintenance goroutines.
//
// This removal provides immediate benefits:
// - Eliminates 979 lines of unused processing overhead
// - Removes potential memory leaks from background goroutines
// - Simplifies the codebase by using libp2p's battle-tested AutoRelay
// - Reduces memory usage from peer tracking maps and maintenance tasks
package relay

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	glog "github.com/omgolab/go-commons/pkg/log"
)

const (
	// CircuitV2Protocol is the protocol ID for circuit relay v2
	CircuitV2Protocol = "/libp2p/circuit/relay/0.2.0/hop"

	// The tag used by AutoRelay to protect connections to relays
	autoRelayTag = "autorelay"

	// Buffer size for relay peer channel
	relayPeerBufferSize = 100

	// DefaultReuseInterval is the default time before we consider a peer for resubmission to AutoRelay
	DefaultReuseInterval = 4 * time.Hour

	// DefaultMaxAge is the default maximum age for peers in our tracking
	DefaultMaxAge = 24 * time.Hour

	// DefaultMaintenanceInterval is the default interval for maintenance operations
	DefaultMaintenanceInterval = 15 * time.Minute

	// Additional constants for optimization
	// MaxPeerMapSize limits memory usage by capping the number of tracked peers
	MaxPeerMapSize = 1000

	// MinRefillThreshold is the minimum peer channel occupancy before triggering refill
	MinRefillThreshold = 10

	// BatchLatencyMeasurementSize is the maximum number of peers to measure latency for at once
	BatchLatencyMeasurementSize = 20
)

// PeerSource is the function signature expected by libp2p's AutoRelay
type PeerSource func(ctx context.Context, numPeers int) <-chan peer.AddrInfo

// AutoRelayPeerSource is a function that converts our PeerSource to the format expected by autorelay
func AutoRelayPeerSource(source PeerSource) func(context.Context, int) <-chan peer.AddrInfo {
	return source
}

// RelaySorter is an interface for custom relay selection strategies
type RelaySorter interface {
	// SortRelays sorts the candidates list in place from best to worst
	SortRelays(candidates []*RelayCandidate)
}

// RelayCandidate represents a potential relay peer with tracking information
type RelayCandidate struct {
	// AddrInfo contains the peer ID and multiaddresses
	AddrInfo peer.AddrInfo

	// FirstSeen is the time when this peer was first added to our tracking
	FirstSeen time.Time

	// LastSeen is the time when we last interacted with this peer
	LastSeen time.Time

	// LastProvided is the time when this peer was last provided to AutoRelay
	LastProvided time.Time

	// ProvideCount is the number of times this peer has been provided to AutoRelay
	ProvideCount int

	// ConfirmedRelaySupport indicates if we've confirmed this peer supports relay protocol
	ConfirmedRelaySupport bool

	// InferredInUse indicates if we think this peer is currently being used as a relay
	InferredInUse bool

	// Latency is the measured RTT to this peer (if available)
	Latency time.Duration
}

// Options for the relay manager
type Options struct {
	// ReuseInterval is how long to wait before we provide the same peer again
	ReuseInterval time.Duration

	// MaxAge is how long to keep peers in our tracking
	MaxAge time.Duration

	// MaintenanceInterval is how often to perform maintenance
	MaintenanceInterval time.Duration

	// Sorter is the strategy to use for sorting relay candidates
	Sorter RelaySorter
}

// DefaultOptions returns sensible default options
func DefaultOptions() *Options {
	return &Options{
		ReuseInterval:       DefaultReuseInterval,
		MaxAge:              DefaultMaxAge,
		MaintenanceInterval: DefaultMaintenanceInterval,
		Sorter:              &LatencySorter{},
	}
}

// RelayManager manages potential relay peers and feeds them to AutoRelay
type RelayManager struct {
	// Maps peer ID to their relay candidate information
	peerMap map[peer.ID]*RelayCandidate

	// Output channel for peers to be sent to AutoRelay
	peerChan chan peer.AddrInfo

	// Reference to host for connectivity checks
	host host.Host

	// Logger instance
	log glog.Logger

	// Lock for synchronizing access to peerMap
	mu sync.RWMutex

	// Configuration options
	opts *Options

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Used to ensure the host is set only once when upgrading
	hostInitOnce sync.Once

	// Metrics for monitoring performance and usage
	metrics struct {
		peersAdded           int64
		peersProvided        int64
		refillCount          int64
		droppedPeers         int64
		cumulativeAvgLatency time.Duration // Renamed from avgLatency
		latencySamples       int64         // Renamed from latencyMeasured
	}
}

// NewRelayManager creates a new RelayManager with the given options
func NewRelayManager(ctx context.Context, h host.Host, log glog.Logger, opts *Options) *RelayManager {
	if opts == nil {
		opts = DefaultOptions()
	}

	// Ensure we have a parent context
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(ctx)
	rm := &RelayManager{
		peerMap:  make(map[peer.ID]*RelayCandidate),
		peerChan: make(chan peer.AddrInfo, relayPeerBufferSize),
		host:     h,
		log:      log,
		opts:     opts,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start maintenance goroutine with proper error handling
	go func() {
		defer func() {
			if r := recover(); r != nil {
				rm.log.Error("Maintenance goroutine panic", fmt.Errorf("%v", r))
				// Restart maintenance
				go rm.maintenance()
			}
		}()
		rm.maintenance()
	}()

	// Start monitoring with proper error handling
	go func() {
		defer func() {
			if r := recover(); r != nil {
				rm.log.Error("Monitor goroutine panic", fmt.Errorf("%v", r))
				// Restart monitoring
				go rm.monitorPeerConnections(rm.ctx)
			}
		}()
		rm.monitorPeerConnections(rm.ctx)
	}()

	return rm
}

// NewPlaceholderRelayManager creates a placeholder RelayManager that doesn't do anything
// but provides a PeerSource function. This is used during initialization before
// we have a host instance.
func NewPlaceholderRelayManager(log glog.Logger) *RelayManager {
	// Create a channel that will never be used
	placeholderChan := make(chan peer.AddrInfo)

	// Create a cancelable context to prevent nil pointer in Close()
	ctx, cancel := context.WithCancel(context.Background())

	return &RelayManager{
		peerMap:  make(map[peer.ID]*RelayCandidate),
		peerChan: placeholderChan,
		log:      log,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// initWithHost initializes a RelayManager with a host.
// This is the shared implementation used by both New and UpdateHost.
func initWithHost(ctx context.Context, rm *RelayManager, h host.Host, isUpgrade bool) {
	// Create new options
	opts := DefaultOptions()
	opts.Sorter = NewMeasureLatencySorter(h)

	// If upgrading, close the old placeholder context
	if isUpgrade {
		rm.Close()
	}

	// Create new context
	newCtx, cancel := context.WithCancel(ctx)

	// Update the manager fields
	rm.host = h
	rm.opts = opts
	rm.ctx = newCtx
	rm.cancel = cancel
	rm.peerChan = make(chan peer.AddrInfo, relayPeerBufferSize)

	// Start maintenance goroutine with proper error handling
	go func() {
		defer func() {
			if r := recover(); r != nil {
				rm.log.Error("Maintenance goroutine panic", fmt.Errorf("%v", r))
				// Restart maintenance
				go rm.maintenance()
			}
		}()
		rm.maintenance()
	}()

	// Start monitoring with proper error handling
	go func() {
		defer func() {
			if r := recover(); r != nil {
				rm.log.Error("Monitor goroutine panic", fmt.Errorf("%v", r))
				// Restart monitoring
				go rm.monitorPeerConnections(rm.ctx)
			}
		}()
		rm.monitorPeerConnections(rm.ctx)
	}()

	if isUpgrade {
		rm.log.Info("RelayManager upgraded with host")
	} else {
		rm.log.Info("RelayManager initialized with host")
	}
}

// New creates a new RelayManager. If a host is provided, it will be fully initialized.
// Otherwise, it returns a placeholder manager that can be upgraded later with UpdateHost.
func New(ctx context.Context, log glog.Logger, h ...host.Host) *RelayManager {
	hasHost := len(h) > 0 && h[0] != nil

	var rm *RelayManager
	if hasHost {
		// Creating with a host by first creating a placeholder, then updating it
		log.Debug("Creating RelayManager with host")
		rm = NewPlaceholderRelayManager(log)
		// Use UpdateHost which handles initialization via sync.Once
		rm.UpdateHost(ctx, h[0])
	} else {
		// Creating RelayManager (waiting for host)
		log.Debug("Creating RelayManager (waiting for host)")
		rm = NewPlaceholderRelayManager(log)
	}

	return rm
}

// UpdateHost updates the RelayManager with a host if it doesn't already have one.
// This is an atomic operation - it will only happen once per manager instance.
// Returns true if the host was updated, false if the manager already had a host or if nil was provided.
func (rm *RelayManager) UpdateHost(ctx context.Context, h host.Host) bool {
	// No host provided or manager already has a host, nothing to do
	if h == nil || rm.host != nil {
		return false
	}

	updated := false
	// We have a manager without a host and a host was provided - upgrade exactly once
	rm.hostInitOnce.Do(func() {
		rm.log.Debug("RelayManager is fully initialized")
		initWithHost(ctx, rm, h, true)
		updated = true
	})

	return updated
}

// AddPeer adds a peer to the manager's tracking, with deduplication and size limits
func (rm *RelayManager) AddPeer(pi peer.AddrInfo) {
	// Skip if manager isn't properly initialized or if this is our own peer ID
	if rm.host == nil || pi.ID == rm.host.ID() || len(pi.Addrs) == 0 {
		return
	}

	// Fetch initial info outside the lock
	supports, latency, protected := rm.fetchPeerInfo(pi.ID)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check if we need to prune the peer map to respect memory limits
	if len(rm.peerMap) >= MaxPeerMapSize {
		rm.prunePeerMap() // Assumes lock is held
	}

	now := time.Now()
	candidate, exists := rm.peerMap[pi.ID]

	if !exists {
		// New peer logic
		rm.peerMap[pi.ID] = &RelayCandidate{
			AddrInfo:              pi,
			FirstSeen:             now,
			LastSeen:              now,
			ConfirmedRelaySupport: supports,  // Use fetched info
			Latency:               latency,   // Use fetched info
			InferredInUse:         protected, // Use fetched info
			// Initialize LastProvided and ProvideCount appropriately
			LastProvided: time.Time{}, // Not provided yet
			ProvideCount: 0,
		}
		candidate = rm.peerMap[pi.ID] // Get reference to the newly added candidate

		// Update metrics
		rm.metrics.peersAdded++
		if latency > 0 { // Update latency metrics if we got a value
			if rm.metrics.latencySamples > 0 {
				rm.metrics.cumulativeAvgLatency = (rm.metrics.cumulativeAvgLatency*time.Duration(rm.metrics.latencySamples) +
					latency) / time.Duration(rm.metrics.latencySamples+1)
			} else {
				rm.metrics.cumulativeAvgLatency = latency
			}
			rm.metrics.latencySamples++
		}
		rm.log.Debug(fmt.Sprintf("Added new peer %s", pi.ID))

		// Try to provide immediately if channel has space
		select {
		case rm.peerChan <- pi:
			candidate.LastProvided = now // Update only if successfully provided
			candidate.ProvideCount = 1
			rm.metrics.peersProvided++
			rm.log.Debug(fmt.Sprintf("Provided new peer %s immediately", pi.ID))
		default:
			rm.log.Debug(fmt.Sprintf("Added new peer %s (relay channel full)", pi.ID))
		}

		return // Done with new peer
	}

	candidate.LastSeen = now
	if len(pi.Addrs) > 0 {
		candidate.AddrInfo = pi // Update stored addresses
	}

	// Apply the fetched info (lock is already held)
	rm.applyPeerInfoUpdateLocked(pi.ID, supports, latency, protected) // Use locked version

	// Only re-provide after reuse interval has passed
	if now.Sub(candidate.LastProvided) > rm.opts.ReuseInterval {
		select {
		case rm.peerChan <- candidate.AddrInfo:
			candidate.LastProvided = now
			candidate.ProvideCount++
			rm.metrics.peersProvided++
			// rm.log.Debug(fmt.Sprintf("Re-provided existing peer: %s", pi.ID))
		default:
			// Channel full, will be handled by refill logic
		}
	}
}

// GetPeerSource returns an optimized function that satisfies the AutoRelay PeerSource interface
func (rm *RelayManager) GetPeerSource() func(ctx context.Context, numPeers int) <-chan peer.AddrInfo {
	if rm.host == nil {
		// Return a placeholder function that just returns an empty channel
		return func(ctx context.Context, numPeers int) <-chan peer.AddrInfo {
			return rm.peerChan
		}
	}

	return func(ctx context.Context, numPeers int) <-chan peer.AddrInfo {
		// Create a dedicated channel with exact capacity
		resultChan := make(chan peer.AddrInfo, numPeers)

		// Optimization: Try to fill the channel immediately with best peers
		rm.fillRequestChannel(ctx, resultChan, numPeers)

		// Return the pre-filled channel
		return resultChan
	}
}

// fillRequestChannel efficiently fills a request channel with the best available peers
func (rm *RelayManager) fillRequestChannel(ctx context.Context, resultChan chan<- peer.AddrInfo, numPeers int) {
	go func() {
		defer close(resultChan)

		// First try to fill directly from our best candidates
		count := rm.fillFromBestCandidates(ctx, resultChan, numPeers)
		if count >= numPeers {
			return
		}

		// If we still need more peers, wait for new ones from the source channel
		for {
			select {
			case <-ctx.Done():
				return
			case p, ok := <-rm.peerChan:
				if !ok {
					return // source channel closed
				}

				select {
				case resultChan <- p:
					count++
					rm.metrics.peersProvided++
					if count >= numPeers {
						return // fulfilled request
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}

// fillFromBestCandidates selects and returns the best available candidates
// Returns the number of peers added to the channel
func (rm *RelayManager) fillFromBestCandidates(ctx context.Context, resultChan chan<- peer.AddrInfo, numPeers int) int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Gather all viable candidates
	candidates := make([]*RelayCandidate, 0, len(rm.peerMap))
	now := time.Now()

	for id, candidate := range rm.peerMap {
		// Only include connected peers with confirmed relay support
		if rm.host.Network().Connectedness(id) == network.Connected &&
			candidate.ConfirmedRelaySupport {
			candidates = append(candidates, candidate)
		}
	}

	// If we have a sorter, use it
	if rm.opts != nil && rm.opts.Sorter != nil {
		rm.opts.Sorter.SortRelays(candidates)
	}

	// Add the best candidates to the channel
	count := 0
	for _, candidate := range candidates {
		if count >= numPeers {
			break
		}

		select {
		case resultChan <- candidate.AddrInfo:
			count++

			// Update the candidate
			candidate.LastProvided = now
			candidate.ProvideCount++

			// Update metrics
			rm.metrics.peersProvided++
		case <-ctx.Done():
			return count
		default:
			// Channel buffer full
			return count
		}
	}

	return count
}

// Close releases resources used by the RelayManager
func (rm *RelayManager) Close() {
	if rm.cancel != nil {
		rm.cancel()
	}

	// Safe close of channel
	if rm.peerChan != nil {
		close(rm.peerChan)
	}
}

// prunePeerMap removes the least valuable peers to keep memory usage bounded
// Assumes rm.mu lock is already held
func (rm *RelayManager) prunePeerMap() {
	if len(rm.peerMap) < MaxPeerMapSize {
		return
	}

	// Create a slice of all candidates for sorting
	candidates := make([]*RelayCandidate, 0, len(rm.peerMap))
	for _, candidate := range rm.peerMap {
		candidates = append(candidates, candidate)
	}

	// Sort by value (keeping the most valuable peers)
	sort.Slice(candidates, func(i, j int) bool {
		// Prioritize confirmed relay support
		if candidates[i].ConfirmedRelaySupport != candidates[j].ConfirmedRelaySupport {
			return candidates[i].ConfirmedRelaySupport
		}

		// Prioritize recently seen peers
		return candidates[i].LastSeen.After(candidates[j].LastSeen)
	})

	// Remove the least valuable peers
	toRemove := len(rm.peerMap) - (MaxPeerMapSize * 3 / 4) // Remove 25% of peers
	for i := 0; i < toRemove && i < len(candidates); i++ {
		delete(rm.peerMap, candidates[len(candidates)-i-1].AddrInfo.ID)
		rm.metrics.droppedPeers++
	}

	rm.log.Debug(fmt.Sprintf("Pruned %d peers from relay manager to respect memory limits", toRemove))
}

// maintenance optimized with better error handling and metrics
func (rm *RelayManager) maintenance() {
	// If options are nil, use default values
	if rm.opts == nil {
		rm.opts = DefaultOptions()
	}

	ticker := time.NewTicker(rm.opts.MaintenanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			// Use recover to prevent crashes
			func() {
				defer func() {
					if r := recover(); r != nil {
						rm.log.Error("Recovered from maintenance panic", fmt.Errorf("%v", r))
					}
				}()

				rm.cleanupOldPeers()
				rm.refillPeerChannel()

				// More efficient, non-blocking check
				if len(rm.peerChan) < MinRefillThreshold {
					rm.refillPeerChannel()
					rm.metrics.refillCount++
				}
			}()
		}
	}
}

// cleanupOldPeers removes peers that are too old from tracking
func (rm *RelayManager) cleanupOldPeers() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	for id, candidate := range rm.peerMap {
		// Remove peers that haven't been seen recently
		if now.Sub(candidate.LastSeen) > rm.opts.MaxAge {
			delete(rm.peerMap, id)
			rm.log.Debug(fmt.Sprintf("Removed inactive peer from relay manager: %s", id))
		}
	}
}

// refillPeerChannel ensures the peer channel has peers available
func (rm *RelayManager) refillPeerChannel() {
	// Check channel length, no need to fill if already full
	rm.mu.RLock()
	chanLen := len(rm.peerChan)
	rm.mu.RUnlock()
	if chanLen >= relayPeerBufferSize {
		return
	}

	now := time.Now()
	availableSlots := relayPeerBufferSize - chanLen

	// Gather suitable candidates - ones that:
	// 1. Haven't been provided recently
	// 2. Are currently connected
	var candidatesToConsider []*RelayCandidate // Renamed from candidates
	rm.mu.RLock()                              // Use RLock for initial read
	for id, candidate := range rm.peerMap {
		// Skip recently provided peers
		if now.Sub(candidate.LastProvided) < rm.opts.ReuseInterval {
			continue
		}

		// Skip peers we're not connected to
		if rm.host.Network().Connectedness(id) != network.Connected {
			continue
		}
		candidatesToConsider = append(candidatesToConsider, candidate)
	}
	rm.mu.RUnlock() // Release RLock

	// Fetch info and update candidates outside the main lock where possible
	updatedCandidates := make([]*RelayCandidate, 0, len(candidatesToConsider))
	for _, candidate := range candidatesToConsider {
		// Fetch info without holding the main lock
		supports, latency, protected := rm.fetchPeerInfo(candidate.AddrInfo.ID)

		// Apply the update (acquires lock internally)
		rm.applyPeerInfoUpdate(candidate.AddrInfo.ID, supports, latency, protected)

		// Re-read the candidate state after potential update (requires RLock)
		rm.mu.RLock()
		updatedCandidate, exists := rm.peerMap[candidate.AddrInfo.ID]
		if exists {
			// Only add if it's still connected and supports relay after update
			if rm.host.Network().Connectedness(updatedCandidate.AddrInfo.ID) == network.Connected && updatedCandidate.ConfirmedRelaySupport {
				updatedCandidates = append(updatedCandidates, updatedCandidate)
			}
		}
		rm.mu.RUnlock()
	}

	// Sort candidates using the configured sorter
	if len(updatedCandidates) > 0 && rm.opts.Sorter != nil {
		rm.opts.Sorter.SortRelays(updatedCandidates)
	}

	// Add top candidates to channel (requires lock for channel write and candidate update)
	rm.mu.Lock() // Acquire WLock for final updates
	count := 0
	for _, candidate := range updatedCandidates {
		if count >= availableSlots {
			break
		}

		// Double check reuse interval after potential updates and sorting
		if now.Sub(candidate.LastProvided) < rm.opts.ReuseInterval {
			continue
		}

		select {
		case rm.peerChan <- candidate.AddrInfo:
			candidate.LastProvided = now
			candidate.ProvideCount++
			count++
		default:
			// Channel suddenly full, stop trying
			rm.mu.Unlock() // Release lock before returning
			return
		}
	}
	rm.mu.Unlock() // Release WLock

	if count > 0 {
		rm.log.Debug(fmt.Sprintf("Refilled relay peer channel with %d peers", count))
	}
}

// monitorPeerConnections watches for peer connection events
func (rm *RelayManager) monitorPeerConnections(ctx context.Context) {
	// Only monitor connections if host is available
	if rm.host == nil {
		return
	}

	// Subscribe to peer connection events
	evtBus := rm.host.EventBus()
	subConnectedness, err := evtBus.Subscribe(new(event.EvtPeerConnectednessChanged))
	if err != nil {
		rm.log.Error("Failed to subscribe to peer connectedness events", err)
		return
	}
	defer subConnectedness.Close()

	for {
		select {
		case <-rm.ctx.Done(): // Use manager's context instead of passed context
			return
		case <-ctx.Done(): // Also honor passed context for compatibility
			return
		case ev, ok := <-subConnectedness.Out():
			if !ok {
				return
			}

			evt := ev.(event.EvtPeerConnectednessChanged)

			if evt.Connectedness == network.Connected {
				// Fetch info outside the lock
				supports, latency, protected := rm.fetchPeerInfo(evt.Peer)

				// Apply update (acquires lock internally)
				rm.applyPeerInfoUpdate(evt.Peer, supports, latency, protected)

			} else if evt.Connectedness == network.NotConnected {
				// Handle disconnections (requires lock)
				rm.mu.Lock()
				if candidate, exists := rm.peerMap[evt.Peer]; exists {
					candidate.LastSeen = time.Now()
					if candidate.InferredInUse {
						candidate.InferredInUse = false
						rm.log.Debug(fmt.Sprintf("Peer %s disconnected and is no longer in use as relay", evt.Peer))
					}
				}
				rm.mu.Unlock()
			}
		}
	}
}

// fetchPeerInfo fetches relay-relevant information about a peer without locking.
func (rm *RelayManager) fetchPeerInfo(id peer.ID) (supportsProtocol bool, latency time.Duration, isProtected bool) {
	if rm.host == nil {
		return false, 0, false
	}

	// Check protocol support
	protocols, err := rm.host.Peerstore().SupportsProtocols(id, CircuitV2Protocol)
	supportsProtocol = err == nil && len(protocols) > 0

	// Get latency
	latency = rm.host.Peerstore().LatencyEWMA(id)

	// Check protection status
	cm := rm.host.ConnManager()
	if cm != nil {
		isProtected = cm.IsProtected(id, autoRelayTag)
	}

	return supportsProtocol, latency, isProtected
}

// applyPeerInfoUpdate updates the candidate struct with pre-fetched data.
// Acquires the necessary lock internally.
func (rm *RelayManager) applyPeerInfoUpdate(id peer.ID, supportsProtocol bool, latency time.Duration, isProtected bool) {
	rm.mu.Lock()         // Acquire WLock
	defer rm.mu.Unlock() // Ensure lock is released
	rm.applyPeerInfoUpdateLocked(id, supportsProtocol, latency, isProtected)
}

// applyPeerInfoUpdateLocked updates the candidate struct with pre-fetched data.
// Assumes the necessary lock is already held.
func (rm *RelayManager) applyPeerInfoUpdateLocked(id peer.ID, supportsProtocol bool, latency time.Duration, isProtected bool) {
	// Add nil check for safety
	if rm.host == nil {
		return
	}

	candidate, exists := rm.peerMap[id]
	if !exists {
		// If peer connected but wasn't in map, add it now
		rm.peerMap[id] = &RelayCandidate{
			AddrInfo: peer.AddrInfo{
				ID:    id,
				Addrs: rm.host.Peerstore().Addrs(id), // Get addresses now that we have the lock
			},
			FirstSeen:             time.Now(),
			LastSeen:              time.Now(),
			ConfirmedRelaySupport: supportsProtocol,
			Latency:               latency,
			InferredInUse:         isProtected,
		}
		// Update metrics for the newly added peer through connection event
		rm.metrics.peersAdded++
		// rm.log.Debug(fmt.Sprintf("Added peer %s discovered via connection event", id))

		// Update latency metrics if applicable
		if latency > 0 {
			if rm.metrics.latencySamples > 0 {
				rm.metrics.cumulativeAvgLatency = (rm.metrics.cumulativeAvgLatency*time.Duration(rm.metrics.latencySamples) +
					latency) / time.Duration(rm.metrics.latencySamples+1)
			} else {
				rm.metrics.cumulativeAvgLatency = latency
			}
			rm.metrics.latencySamples++
		}

		if supportsProtocol {
			// rm.log.Debug(fmt.Sprintf("Confirmed peer %s supports relay protocol", id))
		}
		if isProtected {
			rm.log.Info(fmt.Sprintf("Peer %s is being used as a relay", id))
		}

		return // Done with the new peer
	}

	candidate.LastSeen = time.Now() // Update last seen time

	// Update protocol support
	if supportsProtocol && !candidate.ConfirmedRelaySupport {
		candidate.ConfirmedRelaySupport = true
		// rm.log.Debug(fmt.Sprintf("Confirmed peer %s supports relay protocol", id))
	}

	// Update latency if it's meaningful
	if latency > 0 && (candidate.Latency == 0 ||
		math.Abs(float64(latency-candidate.Latency)) > float64(candidate.Latency)*0.2) {

		oldLatency := candidate.Latency
		candidate.Latency = latency

		// Update metrics using simple cumulative average
		if rm.metrics.latencySamples > 0 {
			// Adjust cumulative average: remove old value effect, add new value effect
			if oldLatency > 0 {
				currentTotal := rm.metrics.cumulativeAvgLatency * time.Duration(rm.metrics.latencySamples)
				newTotal := currentTotal - oldLatency + latency
				rm.metrics.cumulativeAvgLatency = newTotal / time.Duration(rm.metrics.latencySamples) // Samples count doesn't change here
			} else {
				// This is the first valid latency measurement for this peer contributing to the average
				currentTotal := rm.metrics.cumulativeAvgLatency * time.Duration(rm.metrics.latencySamples)
				newTotal := currentTotal + latency
				rm.metrics.latencySamples++ // Increment samples count
				rm.metrics.cumulativeAvgLatency = newTotal / time.Duration(rm.metrics.latencySamples)
			}
		} else {
			// First sample overall
			rm.metrics.cumulativeAvgLatency = latency
			rm.metrics.latencySamples++
		}
	} else if latency > 0 && candidate.Latency == 0 {
		// First latency measurement for this specific peer, even if not triggering the 20% change rule
		candidate.Latency = latency
		// Update metrics
		currentTotal := rm.metrics.cumulativeAvgLatency * time.Duration(rm.metrics.latencySamples)
		newTotal := currentTotal + latency
		rm.metrics.latencySamples++
		rm.metrics.cumulativeAvgLatency = newTotal / time.Duration(rm.metrics.latencySamples)
	}

	// Update inferred usage status
	if isProtected && !candidate.InferredInUse {
		candidate.InferredInUse = true
		rm.log.Info(fmt.Sprintf("Peer %s is being used as a relay", id))
	}
}

// LatencySorter sorts peers by lowest latency first
type LatencySorter struct{}

// SortRelays sorts candidates by latency (ascending)
func (s *LatencySorter) SortRelays(candidates []*RelayCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		// Peers with no latency measurement are considered worse
		if candidates[i].Latency == 0 && candidates[j].Latency > 0 {
			return false
		}
		if candidates[i].Latency > 0 && candidates[j].Latency == 0 {
			return true
		}
		// If both have latency, sort by lowest
		if candidates[i].Latency != 0 && candidates[j].Latency != 0 {
			return candidates[i].Latency < candidates[j].Latency
		}
		// If both have no latency, maintain relative order (or sort by another metric like LastSeen)
		return candidates[i].LastSeen.After(candidates[j].LastSeen) // Fallback to recency
	})
}

// MeasureLatencySorter attempts to measure latency for peers that don't have it
// before sorting by latency.
type MeasureLatencySorter struct {
	host host.Host
}

// NewMeasureLatencySorter creates a sorter that measures latency
func NewMeasureLatencySorter(h host.Host) *MeasureLatencySorter {
	return &MeasureLatencySorter{host: h}
}

// Define a struct to hold ping results along with the peer ID
type pingResultWithID struct {
	peerID peer.ID
	result ping.Result
}

// SortRelays measures latency if needed, then sorts by latency
func (s *MeasureLatencySorter) SortRelays(candidates []*RelayCandidate) {
	// Identify candidates needing latency measurement
	needsMeasurement := make([]peer.ID, 0, BatchLatencyMeasurementSize)
	candidateMap := make(map[peer.ID]*RelayCandidate) // Quick lookup

	for _, c := range candidates {
		// Only measure if latency is 0 and we haven't hit the batch limit
		if c.Latency == 0 && len(needsMeasurement) < BatchLatencyMeasurementSize {
			// Also check if the peer is actually connected, no point pinging disconnected peers
			if s.host != nil && s.host.Network().Connectedness(c.AddrInfo.ID) == network.Connected {
				needsMeasurement = append(needsMeasurement, c.AddrInfo.ID)
				candidateMap[c.AddrInfo.ID] = c
			}
		}
	}

	// Measure latency in parallel (using ping service)
	if len(needsMeasurement) > 0 && s.host != nil {
		pinger := ping.NewPingService(s.host)
		// Channel now carries the custom struct
		resultsChan := make(chan pingResultWithID, len(needsMeasurement))
		var wg sync.WaitGroup

		// Launch a goroutine for each ping
		for _, pid := range needsMeasurement {
			wg.Add(1)
			go func(p peer.ID) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				select {
				case res := <-pinger.Ping(ctx, p):
					// Send the struct containing both ID and result
					resultsChan <- pingResultWithID{peerID: p, result: res}
				case <-ctx.Done():
					// Handle timeout or cancellation if needed
				}
			}(pid)
		}

		// Close the results channel once all pings are done
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// Process results as they come in
		for resWithID := range resultsChan {
			// Extract the result and peer ID
			res := resWithID.result
			pid := resWithID.peerID
			if res.Error == nil && res.RTT > 0 {
				// Use the correct peer ID (pid) to find the candidate
				if c, ok := candidateMap[pid]; ok {
					c.Latency = res.RTT // Update candidate directly
					// Note: This doesn't update the central metrics average, only the candidate's value for sorting.
				}
			}
		}
	}

	// Now sort using the standard latency sorter logic
	latencySorter := LatencySorter{}
	latencySorter.SortRelays(candidates)
}
