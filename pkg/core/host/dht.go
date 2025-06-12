package host

import (
	"context"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/omgolab/drpc/pkg/config"
)

// setupDHT initializes the DHT and starts peer discovery if applicable.
// It relies on the provided dhtOpts to configure behavior, including bootstrapping.
func setupDHT(ctx context.Context, h host.Host, cfg *hostCfg, userDhtOptions ...dht.Option) (*dht.IpfsDHT, error) {
	// default dht options
	dhtOptions := []dht.Option{dht.Mode(dht.ModeAuto)} // Default to server mode

	// Set up DHT with default bootstrap peers
	// This is a good starting point for peer discovery
	peers, _ := peer.AddrInfosFromP2pAddrs(dht.DefaultBootstrapPeers...)
	dhtOptions = append(dhtOptions, dht.BootstrapPeers(peers...))

	// update dht options with user-provided options
	if len(userDhtOptions) > 0 && userDhtOptions[0] != nil {
		dhtOptions = append(dhtOptions, userDhtOptions...)
	}

	// Start a DHT, for use in peer discovery
	kademliaDHT, err := dht.New(ctx, h, dhtOptions...)
	if err != nil {
		return nil, err
	}

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	cfg.logger.Debug("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		return nil, err
	}

	// Set up DHT discovery
	go func() {
		// Wait a moment for DHT to potentially stabilize before advertising/finding
		time.Sleep(2 * time.Second)

		routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
		cfg.logger.Info("Advertising self on DHT")
		dutil.Advertise(ctx, routingDiscovery, config.DISCOVERY_TAG)

		cfg.logger.Info("Starting DHT peer discovery loop")
		findPeersLoop(ctx, routingDiscovery, h, cfg)
		cfg.logger.Info("DHT peer discovery loop stopped")
	}()
	return kademliaDHT, nil
}

// findPeersLoop continuously searches for peers using DHT discovery
func findPeersLoop(ctx context.Context, routingDiscovery *drouting.RoutingDiscovery, h host.Host, cfg *hostCfg) {
	ticker := time.NewTicker(config.DHT_PEER_DISCOVERY_INTERVAL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cfg.logger.Info("Stopping DHT peer discovery loop due to context cancellation")
			return
		case <-ticker.C:
			// cfg.logger.Debug("Finding peers via DHT")
			peerChan, err := routingDiscovery.FindPeers(ctx, config.DISCOVERY_TAG)
			if err != nil {
				cfg.logger.Error("DHT FindPeers error", err)
				continue // Wait for next tick
			}

			// Process peers found in this round
			go connectToFoundPeers(ctx, h, cfg, peerChan)
		}
	}
}

// connectToFoundPeers connects to peers found via DHT discovery
func connectToFoundPeers(ctx context.Context, h host.Host, cfg *hostCfg, peerChan <-chan peer.AddrInfo) {
	for pi := range peerChan {
		// Skip connecting to self
		if pi.ID == h.ID() {
			continue
		}

		// Create a context for this connection attempt with a timeout
		connCtx, cancel := context.WithTimeout(ctx, 15*time.Second)

		go func(peerInfo peer.AddrInfo) {
			defer cancel()

			if err := h.Connect(connCtx, peerInfo); err != nil {
				cfg.logger.Debug("Failed to connect to peer found via DHT", map[string]interface{}{
					"peer":  peerInfo.ID.String(),
					"error": err.Error(),
				})
			} else {
				cfg.logger.Debug("Connected to peer found via DHT", map[string]interface{}{
					"peer": peerInfo.ID.String(),
				})
			}
		}(pi)
	}
}
