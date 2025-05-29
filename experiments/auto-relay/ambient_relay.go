package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/omgolab/drpc/pkg/config"
)

func main() {
	// Parse target peer ID
	var targetPeerID peer.ID
	var err error
	if targetPeerID, err = peer.Decode("12D3KooWMy4T1sRX9yR1Tdh9cGHSk3gDhRoSaYuSZvBC9G1wxQc8"); err != nil {
		log.Fatal(err)
	}

	// Create a new libp2p host
	var h host.Host
	if h, err = libp2p.New(); err != nil {
		log.Fatal(err)
	}
	defer h.Close()
	log.Printf("Node: %s", h.ID())

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup discovery
	rd, err := setupDiscovery(ctx, h)
	if err != nil {
		log.Fatalf("Failed to setup discovery: %s", err)
	}

	// Connect to the target peer with timeout
	connectionTimeout := 30 * time.Second
	log.Printf("Attempting to connect to target peer %s (timeout: %s)", targetPeerID, connectionTimeout)

	if err := connectToPeer(ctx, h, targetPeerID, connectionTimeout, rd); err != nil {
		log.Fatalf("Failed to connect to target peer %s: %s", targetPeerID, err)
	}
}

func setupDiscovery(ctx context.Context, h host.Host) (*drouting.RoutingDiscovery, error) {
	// Setup bootstrap peers with pre-allocated slice
	bootstrapPeers := make([]peer.AddrInfo, 0, len(dht.DefaultBootstrapPeers))
	for _, ma := range dht.DefaultBootstrapPeers {
		if ai, err := peer.AddrInfoFromP2pAddr(ma); err == nil {
			bootstrapPeers = append(bootstrapPeers, *ai)
		}
	}

	// Create and bootstrap DHT
	kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeClient), dht.BootstrapPeers(bootstrapPeers...))
	if err != nil {
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	if err := kademliaDHT.Bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	// Create routing discovery and advertise
	rd := drouting.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, rd, config.DISCOVERY_TAG)

	return rd, nil
}

func connectToPeer(ctx context.Context, h host.Host, targetPeerID peer.ID, timeout time.Duration, rd *drouting.RoutingDiscovery) error {
	// Create connection context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create peer info with the target peer ID
	pi := peer.AddrInfo{
		ID: targetPeerID,
	}

	// Attempt direct connection first
	if err := h.Connect(timeoutCtx, pi); err == nil {
		log.Printf("Direct connection established to peer %s at %s",
			pi.ID, h.Network().ConnsToPeer(pi.ID)[0].RemoteMultiaddr())
		return nil
	}

	// Use channels to coordinate concurrent discovery attempts
	var wg sync.WaitGroup
	var once sync.Once
	done := make(chan struct{})

	// update: TODO: remove multi discovery workers - it's redundant since we are using a same rd instance
	// Start multiple discovery workers for better performance
	numWorkers := 3
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-done:
					return
				case <-timeoutCtx.Done():
					return
				default:
				}

				// Find peers with timeout context
				pc, err := rd.FindPeers(timeoutCtx, config.DISCOVERY_TAG)
				if err != nil {
					continue
				}

				// Process peers and attempt connection
				for p := range pc {
					select {
					case <-done:
						return
					case <-timeoutCtx.Done():
						return
					default:
					}

					if p.ID != h.ID() {
						// Try connecting to our target
						if err := h.Connect(timeoutCtx, pi); err == nil {
							log.Printf("Successfully connected to peer %s at %s", pi.ID, h.Network().ConnsToPeer(pi.ID)[0].RemoteMultiaddr())
							// Signal success to all workers
							once.Do(func() {
								close(done)
							})
							return
						}
					}
				}
			}
		}()
	}

	// Wait for either success or timeout
	go func() {
		wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-timeoutCtx.Done():
		return fmt.Errorf("connection timeout reached")
	}
}
