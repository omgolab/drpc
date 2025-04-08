package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/omgolab/drpc/pkg/config"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// peerConnection holds streams for a specific peer
type peerConnection struct {
	streams      []network.Stream // Using a slice as a stack for O(1) operations
	lastAccessed time.Time
	mu           sync.Mutex
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
	return ms.Stream.Reset()
}

// ConnectionPool manages stream reuse
type ConnectionPool struct {
	p2pHost     host.Host
	connections map[peer.ID]*peerConnection
	maxIdleTime time.Duration
	maxStreams  int
	mu          sync.RWMutex
}

func NewConnectionPool(p2pHost host.Host, maxIdleTime time.Duration, maxStreams int) *ConnectionPool {
	pool := &ConnectionPool{
		p2pHost:     p2pHost,
		connections: make(map[peer.ID]*peerConnection),
		maxIdleTime: maxIdleTime,
		maxStreams:  maxStreams,
	}

	// Start cleanup goroutine
	go pool.periodicCleanup()

	return pool
}

func (p *ConnectionPool) GetStream(ctx context.Context, peerID peer.ID, protocolID protocol.ID) (network.Stream, error) {
	// Fast path: try to get an existing connection with read lock first
	p.mu.RLock()
	peerConn, exists := p.connections[peerID]
	p.mu.RUnlock()

	if !exists {
		// Slow path: create new connection with write lock
		p.mu.Lock()
		peerConn, exists = p.connections[peerID]
		if !exists {
			peerConn = &peerConnection{
				streams:      make([]network.Stream, 0, p.maxStreams),
				lastAccessed: time.Now(),
			}
			p.connections[peerID] = peerConn
		}
		p.mu.Unlock()
	}

	// Get a stream from the peer connection
	stream, freshlyCreated, err := p.getStreamFromPeerConn(ctx, peerConn, peerID, protocolID)
	if err != nil {
		return nil, err
	}

	// Update lastAccessed time only if we're reusing a stream to reduce lock contention
	if !freshlyCreated {
		peerConn.mu.Lock()
		peerConn.lastAccessed = time.Now()
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

		// Return wrapped stream
		return &ManagedStream{
			Stream: stream,
			pool:   p,
			peerID: peerID,
			closed: false,
		}, false, nil
	}
	peerConn.mu.Unlock()

	// Create a new stream if none available
	stream, err := p.p2pHost.NewStream(ctx, peerID, protocolID)
	if err != nil {
		return nil, true, err
	}

	// Return wrapped stream
	return &ManagedStream{
		Stream: stream,
		pool:   p,
		peerID: peerID,
		closed: false,
	}, true, nil
}

// releaseStream puts a stream back into the pool or closes it
func (p *ConnectionPool) releaseStream(peerID peer.ID, stream network.Stream) {
	if stream == nil {
		return
	}

	// Quick check for connection existence with read lock
	p.mu.RLock()
	peerConn, exists := p.connections[peerID]
	p.mu.RUnlock()

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
		peerConn.lastAccessed = time.Now()
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
	// Create a list of peers to remove to minimize lock contention
	var peersToRemove []peer.ID
	var streamsToClose []network.Stream

	// First phase: identify idle connections with read lock
	p.mu.RLock()
	now := time.Now()
	for peerID, peerConn := range p.connections {
		peerConn.mu.Lock()
		if now.Sub(peerConn.lastAccessed) > p.maxIdleTime {
			// Collect streams and mark peer for removal
			streamsToClose = append(streamsToClose, peerConn.streams...)
			peersToRemove = append(peersToRemove, peerID)
		}
		peerConn.mu.Unlock()
	}
	p.mu.RUnlock()

	// Second phase: remove identified connections with write lock
	if len(peersToRemove) > 0 {
		p.mu.Lock()
		for _, peerID := range peersToRemove {
			delete(p.connections, peerID)
		}
		p.mu.Unlock()

		// Close streams outside of any locks
		for _, stream := range streamsToClose {
			stream.Close()
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
