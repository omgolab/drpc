package pool

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/omgolab/drpc/pkg/config"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// Object pools for connection-related structures
var (
	peerConnectionPool = sync.Pool{
		New: func() any {
			return &peerConnection{
				streams: make([]network.Stream, 0, 4), // Pre-allocate slice capacity
			}
		},
	}

	managedStreamPool = sync.Pool{
		New: func() any {
			return &ManagedStream{}
		},
	}
)

// Connection pool sharding constants
const (
	// Number of shards to distribute connections across
	// Power of 2 for efficient modulo operation using bitwise AND
	ShardCount = 16
	ShardMask  = ShardCount - 1
)

// peerConnection holds streams for a specific peer with atomic optimizations
type peerConnection struct {
	streams         []network.Stream // Using a slice as a stack for O(1) operations
	lastAccessedNs  int64           // Unix nanoseconds - atomic access
	mu              sync.Mutex
}

// getLastAccessed returns the last access time using atomic operation
func (pc *peerConnection) getLastAccessed() time.Time {
	ns := atomic.LoadInt64(&pc.lastAccessedNs)
	return time.Unix(0, ns)
}

// updateLastAccessed sets the last access time using atomic operation
func (pc *peerConnection) updateLastAccessed() {
	atomic.StoreInt64(&pc.lastAccessedNs, time.Now().UnixNano())
}

// ManagedStream is a wrapper around network.Stream that automatically
// returns itself to the pool when Close() is called
type ManagedStream struct {
	network.Stream
	pool   *ConnectionPool
	peerID peer.ID
	closed bool
	mu     sync.Mutex
}

// Close overrides the Close method to return the stream to the pool instead
// of actually closing it. This method is optimized to minimize lock contention.
func (ms *ManagedStream) Close() error {
	ms.mu.Lock()
	if ms.closed {
		ms.mu.Unlock()
		return nil
	}
	ms.closed = true
	ms.mu.Unlock()

	// Call ReleaseStream outside the lock to reduce lock contention
	ms.pool.releaseStream(ms.peerID, ms.Stream)
	
	// Return the ManagedStream object to the pool for reuse
	ms.returnToPool()
	
	return nil
}

// Reset ensures we properly handle the Reset call with optimized locking
func (ms *ManagedStream) Reset() error {
	ms.mu.Lock()
	if ms.closed {
		ms.mu.Unlock()
		return nil
	}
	ms.closed = true
	ms.mu.Unlock()

	// Call Stream.Reset() outside the lock
	err := ms.Stream.Reset()

	// Return the ManagedStream object to the pool for reuse
	ms.returnToPool()

	return err
}

// returnToPool cleans up and returns the ManagedStream to the object pool
func (ms *ManagedStream) returnToPool() {
	// Clear references to prevent memory leaks
	ms.Stream = nil
	ms.pool = nil
	ms.peerID = ""
	ms.closed = false

	// Return to pool for reuse
	managedStreamPool.Put(ms)
}

// ConnectionPool manages stream reuse with sharding for reduced lock contention
type ConnectionPool struct {
	p2pHost     host.Host
	shards      [ShardCount]*connectionShard
	maxIdleTime time.Duration
	maxStreams  int
	logger      glog.Logger
}

// connectionShard represents a single shard of connections to reduce lock contention
type connectionShard struct {
	connections map[peer.ID]*peerConnection
	mu          sync.RWMutex
}

// hashPeerID returns a hash of the peer ID for sharding
func hashPeerID(peerID peer.ID) uint32 {
	h := fnv.New32a()
	h.Write([]byte(peerID))
	return h.Sum32()
}

// getShard returns the appropriate shard for a peer ID
func (p *ConnectionPool) getShard(peerID peer.ID) *connectionShard {
	return p.shards[hashPeerID(peerID)&ShardMask]
}

func NewConnectionPool(p2pHost host.Host, maxIdleTime time.Duration, maxStreams int, logger glog.Logger) *ConnectionPool {
	pool := &ConnectionPool{
		p2pHost:     p2pHost,
		maxIdleTime: maxIdleTime,
		maxStreams:  maxStreams,
		logger:      logger,
	}

	// Initialize shards
	for i := 0; i < ShardCount; i++ {
		pool.shards[i] = &connectionShard{
			connections: make(map[peer.ID]*peerConnection),
		}
	}

	// Start cleanup goroutine
	go pool.periodicCleanup()

	return pool
}

func (p *ConnectionPool) GetStream(ctx context.Context, peerID peer.ID, protocolID protocol.ID) (network.Stream, error) {
	shard := p.getShard(peerID)

	// Fast path: try to get an existing connection with read lock first
	shard.mu.RLock()
	peerConn, exists := shard.connections[peerID]
	shard.mu.RUnlock()

	if !exists {
		// Slow path: create new connection with write lock
		shard.mu.Lock()
		peerConn, exists = shard.connections[peerID]
		if !exists {
			peerConn = peerConnectionPool.Get().(*peerConnection)
			peerConn.streams = make([]network.Stream, 0, p.maxStreams)
			peerConn.updateLastAccessed()
			shard.connections[peerID] = peerConn
		}
		shard.mu.Unlock()
	}

	// Get a stream from the peer connection
	stream, freshlyCreated, err := p.getStreamFromPeerConn(ctx, peerConn, peerID, protocolID)
	if err != nil {
		return nil, err
	}

	// Update lastAccessed time only if we're reusing a stream to reduce lock contention
	if !freshlyCreated {
		peerConn.mu.Lock()
		peerConn.updateLastAccessed()
		peerConn.mu.Unlock()
	}

	return stream, nil
}

// getStreamFromPeerConn is a helper method to get a stream from a peer connection
// It returns the stream, a boolean indicating if the stream was freshly created,
// and an error if any
func (p *ConnectionPool) getStreamFromPeerConn(
	ctx context.Context,
	peerConn *peerConnection,
	peerID peer.ID,
	protocolID protocol.ID,
) (network.Stream, bool, error) {
	peerConn.mu.Lock()

	// Try to get an existing stream
	if len(peerConn.streams) > 0 {
		lastIdx := len(peerConn.streams) - 1
		stream := peerConn.streams[lastIdx]
		peerConn.streams = peerConn.streams[:lastIdx] // O(1) pop operation
		peerConn.mu.Unlock()

		// Return wrapped stream from pool
		ms := managedStreamPool.Get().(*ManagedStream)
		ms.Stream = stream
		ms.pool = p
		ms.peerID = peerID
		ms.closed = false
		return ms, false, nil
	}
	peerConn.mu.Unlock()

	// // Create a new stream if none available
	// p.logger.Debug(fmt.Sprintf("Pool: Attempting to create new stream to %s with protocol %s", peerID, protocolID)) // Use Debug + Sprintf
	// stream, err := p.p2pHost.NewStream(network.WithAllowLimitedConn(ctx, "drpc-gateway-relay"), peerID, protocolID) // Allow limited (relayed) connections
	// if err != nil {
	// 	p.logger.Error(fmt.Sprintf("Pool: Failed to create new stream to %s", peerID), err) // Pass error separately
	// 	return nil, true, err
	// }
	// p.logger.Debug(fmt.Sprintf("Pool: Successfully created new stream to %s", peerID)) // Use Debug + Sprintf

	// Create a new stream if none available
	stream, err := p.p2pHost.NewStream(ctx, peerID, protocolID)
	if err != nil {
		return nil, true, err
	}

	// Return wrapped stream from pool
	ms := managedStreamPool.Get().(*ManagedStream)
	ms.Stream = stream
	ms.pool = p
	ms.peerID = peerID
	ms.closed = false
	return ms, true, nil
}

// releaseStream puts a stream back into the pool or closes it
func (p *ConnectionPool) releaseStream(peerID peer.ID, stream network.Stream) {
	if stream == nil {
		return
	}

	shard := p.getShard(peerID)

	// Quick check for connection existence with read lock
	shard.mu.RLock()
	peerConn, exists := shard.connections[peerID]
	shard.mu.RUnlock()

	if !exists {
		stream.Close()
		return
	}

	// Fast check if stream is viable for reuse
	if stream.Conn() == nil || stream.Conn().IsClosed() {
		stream.Close()
		return
	}

	peerConn.mu.Lock()
	// Check if we can add to the pool
	if len(peerConn.streams) < p.maxStreams {
		// Add to pool for reuse
		peerConn.streams = append(peerConn.streams, stream)
		peerConn.updateLastAccessed()
		peerConn.mu.Unlock()
	} else {
		// Pool is full, close the stream
		peerConn.mu.Unlock()
		stream.Close()
	}
}

func (p *ConnectionPool) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

func (p *ConnectionPool) cleanup() {
	now := time.Now()

	// Process each shard independently to reduce lock contention
	for i := 0; i < ShardCount; i++ {
		shard := p.shards[i]
		
		// Create lists for this shard to minimize lock contention
		var peersToRemove []peer.ID
		var streamsToClose []network.Stream

		// First phase: identify idle connections with read lock
		shard.mu.RLock()
		for peerID, peerConn := range shard.connections {
			peerConn.mu.Lock()
			if now.Sub(peerConn.getLastAccessed()) > p.maxIdleTime {
				// Collect streams and mark peer for removal
				streamsToClose = append(streamsToClose, peerConn.streams...)
				peersToRemove = append(peersToRemove, peerID)
			}
			peerConn.mu.Unlock()
		}
		shard.mu.RUnlock()

		// Second phase: remove identified connections with write lock
		if len(peersToRemove) > 0 {
			shard.mu.Lock()
			for _, peerID := range peersToRemove {
				delete(shard.connections, peerID)
			}
			shard.mu.Unlock()

			// Close streams outside of any locks
			for _, stream := range streamsToClose {
				stream.Close()
			}
		}
	}
}

// ConnectToFirstAvailablePeer attempts to connect to peers in parallel with retries
// until the first successful connection or the timeout period expires.
// This optimized implementation combines fast parallel connection attempts
// with exponential backoff retry for reliability.
func ConnectToFirstAvailablePeer(
	ctx context.Context,
	h host.Host,
	peerInfoMap map[peer.ID]peer.AddrInfo,
	logger glog.Logger,
) (peer.ID, error) {
	if len(peerInfoMap) == 0 {
		return "", fmt.Errorf("no peer addresses provided")
	}

	// Create a context with timeout if not already timeout-bound
	connectCtx, cancel := context.WithTimeout(ctx, 60*time.Second) // Increased timeout to 60s
	defer cancel()

	// Create a child context that can be cancelled when we find the first successful connection
	childCtx, childCancel := context.WithCancel(connectCtx)
	defer childCancel()

	// Channel to receive the first successful peer connection
	successChan := make(chan peer.ID, len(peerInfoMap))

	// Start a goroutine for each peer to try connection
	var wg sync.WaitGroup
	for peerID, addrInfo := range peerInfoMap {
		wg.Add(1)
		go func(pid peer.ID, ai peer.AddrInfo) {
			defer wg.Done()

			// First, check if we're already connected to save time
			if h.Network().Connectedness(pid) == network.Connected {
				if logger != nil {
					logger.Printf("Already connected to peer %s", pid)
				}
				select {
				case successChan <- pid:
					childCancel() // Cancel all other connection attempts
				case <-childCtx.Done():
					// Another connection succeeded or context cancelled
				}
				return
			}

			// Fast first attempt without backoff
			err := h.Connect(childCtx, ai)
			if err == nil {
				if logger != nil {
					logger.Printf("Successfully connected to peer %s", pid)
				}
				select {
				case successChan <- pid:
					childCancel() // Cancel all other connection attempts
				case <-childCtx.Done():
					// Another connection succeeded or context cancelled
				}
				return
			}

			// If fast attempt failed, start retry with backoff
			backoff := time.Millisecond * 100
			maxBackoff := time.Second * 2

			for {
				select {
				case <-childCtx.Done():
					return
				case <-time.After(backoff):
					// Try to connect again
					if err := h.Connect(childCtx, ai); err != nil {
						if logger != nil && config.DEBUG {
							logger.Printf("Failed to connect to peer %s: %v, retrying in %v",
								pid, err, backoff*2)
						}
						// Increase backoff for next retry
						backoff *= 2
						if backoff > maxBackoff {
							backoff = maxBackoff
						}
						continue
					}

					// Successfully connected
					if logger != nil {
						logger.Printf("Successfully connected to peer %s after retry", pid)
					}
					select {
					case successChan <- pid:
						childCancel() // Cancel all other connection attempts
					case <-childCtx.Done():
						// Another connection succeeded or context cancelled
					}
					return
				}
			}
		}(peerID, addrInfo)
	}

	// Close success channel when all goroutines complete
	go func() {
		wg.Wait()
		close(successChan)
	}()

	// Wait for first successful connection or timeout
	select {
	case pid, ok := <-successChan:
		if ok && pid != "" {
			return pid, nil
		}
		return "", errors.New("failed to connect to any peer")
	case <-connectCtx.Done():
		return "", fmt.Errorf("connection timeout after 60 seconds: %v", connectCtx.Err()) // Updated error message
	}
}
