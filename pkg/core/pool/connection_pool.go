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
	streams      []network.Stream
	lastAccessed time.Time
	mu           sync.Mutex
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
	p.mu.RLock()
	peerConn, exists := p.connections[peerID]
	p.mu.RUnlock()

	if !exists {
		p.mu.Lock()
		// Check again after acquiring write lock
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

	peerConn.mu.Lock()
	defer peerConn.mu.Unlock()

	peerConn.lastAccessed = time.Now()

	// Try to get an existing stream
	if len(peerConn.streams) > 0 {
		stream := peerConn.streams[len(peerConn.streams)-1]
		peerConn.streams = peerConn.streams[:len(peerConn.streams)-1]
		return stream, nil
	}

	// Create a new stream if none available
	return p.p2pHost.NewStream(ctx, peerID, protocolID)
}

func (p *ConnectionPool) ReleaseStream(peerID peer.ID, stream network.Stream) {
	if stream == nil {
		return
	}

	p.mu.RLock()
	peerConn, exists := p.connections[peerID]
	p.mu.RUnlock()

	if !exists {
		stream.Close()
		return
	}

	peerConn.mu.Lock()
	defer peerConn.mu.Unlock()

	// If we've reached max streams, close this one
	if len(peerConn.streams) >= p.maxStreams {
		stream.Close()
		return
	}

	// Reset stream to clean state
	stream.Reset()

	// Add back to pool
	peerConn.streams = append(peerConn.streams, stream)
	peerConn.lastAccessed = time.Now()
}

func (p *ConnectionPool) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

func (p *ConnectionPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	for peerID, peerConn := range p.connections {
		peerConn.mu.Lock()
		if now.Sub(peerConn.lastAccessed) > p.maxIdleTime {
			// Close all streams
			for _, stream := range peerConn.streams {
				stream.Close()
			}
			delete(p.connections, peerID)
		}
		peerConn.mu.Unlock()
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
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
		return "", fmt.Errorf("connection timeout after 30 seconds: %v", connectCtx.Err())
	}
}
