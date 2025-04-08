package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/omgolab/drpc/pkg/config"
	glog "github.com/omgolab/go-commons/pkg/log" // Renamed log to glog

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	libp2pmdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// setupDHT initializes the DHT and starts peer discovery if applicable.
// It relies on the provided dhtOpts to configure behavior, including bootstrapping.
func setupDHT(ctx context.Context, h host.Host, log glog.Logger, userDhtOptions ...dht.Option) (*dht.IpfsDHT, error) { // Use glog.Logger
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
	log.Debug("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		return nil, err
	}

	// Set up DHT discovery
	go func() {
		// Wait a moment for DHT to potentially stabilize before advertising/finding
		time.Sleep(2 * time.Second)

		routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
		log.Info("Advertising self on DHT")
		dutil.Advertise(ctx, routingDiscovery, config.DISCOVERY_TAG)

		log.Info("Starting DHT peer discovery loop")
		findPeersLoop(ctx, routingDiscovery, h, log)
		log.Info("DHT peer discovery loop stopped")
	}()
	return kademliaDHT, nil
}

// CreateLibp2pHost creates a new libp2p Host with default settings.
func CreateLibp2pHost(ctx context.Context, log glog.Logger, libp2pOpts []libp2p.Option, dhtOpts ...dht.Option) (host.Host, error) { // Use glog.Logger
	// We'll use a shared variable for the DHT instance
	// to avoid duplication between setupDHT and the routing constructor
	var kadDHT *dht.IpfsDHT
	var dhtErr error
	var dhtOnce sync.Once

	// fix for nil options
	if dhtOpts == nil {
		dhtOpts = []dht.Option{}
	}
	if libp2pOpts == nil {
		libp2pOpts = []libp2p.Option{}
	}

	// Configure libp2p options
	options := []libp2p.Option{
		// start with sane defaults
		libp2p.FallbackDefaults,

		// NAT traversal enhancements
		libp2p.EnableNATService(), // AutoNAT service
		libp2p.NATPortMap(),       // NAT port mapping

		// Enable hole punching
		libp2p.EnableHolePunching(),

		// Set up AutoRelay
		libp2p.EnableRelay(),

		// Configure AutoNAT
		libp2p.AutoNATServiceRateLimit(
			30, // 30 requests globally per minute
			3,  // 3 requests per peer per minute
			config.AUTONAT_REFRESH_INTERVAL,
		),

		// Create a routing constructor function that uses our DHT instance
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			// Initialize DHT only once
			dhtOnce.Do(func() {
				kadDHT, dhtErr = setupDHT(ctx, h, log, dhtOpts...) // Pass dhtOpts here
			})
			return kadDHT, dhtErr
		}),

		// Enable relays and NAT services
		libp2p.EnableRelayService(), // Enable Relay Service if publicly reachable
		libp2p.EnableAutoNATv2(),    // Enable AutoNATv2
		libp2p.DisableMetrics(),     // Disable metrics collection for performance
	}

	// Add any user-provided options
	options = append(options, libp2pOpts...)

	// Create a new libp2p Host
	h, err := libp2p.New(options...) // Pass combined options
	if err != nil {
		log.Error("Failed to create libp2p host", err)
		return nil, err
	}

	// Setup mDNS discovery
	if err := setupMDNS(h, log); err != nil {
		log.Error("Failed to setup mDNS", err)
		// Continue anyway, some features may still work
	}

	// Log any DHT setup errors
	if dhtErr != nil {
		log.Error("Failed to setup DHT", dhtErr)
		// Continue anyway, some features may still work
	}

	// Log our listening addresses
	addresses := h.Addrs()
	log.Info(fmt.Sprintf("Created libp2p host with ID: %s", h.ID().String()))
	log.Info(fmt.Sprintf("Host is listening on %d addresses:", len(addresses)))
	for i, addr := range addresses {
		log.Info(fmt.Sprintf("  %d: %s", i+1, addr.String()))
	}

	return h, nil
}

// --- Helper functions moved from above for clarity ---

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	h   host.Host
	log glog.Logger // Use glog.Logger
}

// HandlePeerFound connects to peers discovered via mDNS
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	// Skip self
	if pi.ID == n.h.ID() {
		return
	}
	n.log.Debug(fmt.Sprintf("mDNS peer found: %s", pi.ID.String()))

	ctx, cancel := context.WithTimeout(context.Background(), config.PEER_CONNECTION_TIMEOUT)
	defer cancel()

	err := n.h.Connect(ctx, pi)
	if err != nil {
		// Don't log errors for transient connection issues, use Debug
		n.log.Debug(fmt.Sprintf("Failed connecting to mDNS peer %s: %s", pi.ID.String(), err.Error()))
		return
	}
	n.log.Info(fmt.Sprintf("Connected to peer via mDNS: %s", pi.ID.String()))
}

// setupMDNS initializes the mDNS discovery service
func setupMDNS(h host.Host, log glog.Logger) error { // Use glog.Logger
	// Setup mDNS discovery service
	log.Info("Setting up mDNS discovery")
	notifee := &discoveryNotifee{h: h, log: log}
	// Use DefaultServiceTag if config.DISCOVERY_TAG is empty
	tag := config.DISCOVERY_TAG
	if tag == "" {
		// libp2pmdns handles empty string as default tag internally
		tag = ""
		log.Warn("config.DISCOVERY_TAG is empty, using default mDNS tag")
	}
	disc := libp2pmdns.NewMdnsService(h, tag, notifee)
	return disc.Start()
}

// findPeersLoop continuously searches for peers using DHT discovery
func findPeersLoop(ctx context.Context, routingDiscovery *drouting.RoutingDiscovery, h host.Host, log glog.Logger) { // Use glog.Logger
	ticker := time.NewTicker(config.DHT_PEER_DISCOVERY_INTERVAL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping DHT peer discovery loop due to context cancellation")
			return
		case <-ticker.C:
			log.Debug("Finding peers via DHT")
			peerChan, err := routingDiscovery.FindPeers(ctx, config.DISCOVERY_TAG)
			if err != nil {
				log.Error("DHT FindPeers error", err)
				continue // Wait for next tick
			}

			// Process peers found in this round
			go connectToFoundPeers(ctx, h, log, peerChan)
		}
	}
}

// connectToFoundPeers handles connecting to peers from the discovery channel
func connectToFoundPeers(ctx context.Context, h host.Host, log glog.Logger, peerChan <-chan peer.AddrInfo) { // Use glog.Logger
	for p := range peerChan {
		// Skip self
		if p.ID == h.ID() {
			continue
		}
		// Convert addrs to strings for logging
		addrStrings := make([]string, len(p.Addrs))
		for i, addr := range p.Addrs {
			addrStrings[i] = addr.String()
		}
		log.Debug(fmt.Sprintf("DHT peer found: %s, addrs: %v", p.ID.String(), addrStrings))

		connectCtx, connectCancel := context.WithTimeout(ctx, config.PEER_CONNECTION_TIMEOUT)
		err := h.Connect(connectCtx, p)
		connectCancel() // Release context resources promptly
		if err != nil {
			// Use Debug level for potentially transient connection errors
			log.Debug(fmt.Sprintf("Failed connecting to DHT peer %s: %s", p.ID.String(), err.Error()))
		} else {
			log.Info(fmt.Sprintf("Connected to DHT peer: %s", p.ID.String()))
		}
	}
}
