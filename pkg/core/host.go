package core

//go:generate sh -c "cd ./proto/; buf generate"

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	libp2pmdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/omgolab/drpc/pkg/config"
	pb "github.com/omgolab/drpc/pkg/core/proto"
	"github.com/omgolab/drpc/pkg/core/relay"
	glog "github.com/omgolab/go-commons/pkg/log"
	"google.golang.org/protobuf/proto"
)

var (
	_ libp2pmdns.Notifee = (*discoveryNotifee)(nil) // Ensure discoveryNotifee implements the Notifee interface
)

// hostCfg holds the host configuration
type hostCfg struct {
	logger                 glog.Logger
	libp2pOptions          []libp2p.Option
	dhtOptions             []dht.Option
	isClientMode           bool
	relayManager           *relay.RelayManager
	disablePubsubDiscovery bool
}

// HostOption configures a Host.
type HostOption func(*hostCfg) error

// WithHostLogger sets the logger for the host.
func WithHostLogger(logger glog.Logger) HostOption {
	return func(c *hostCfg) error {
		c.logger = logger
		return nil
	}
}

// WithHostLibp2pOptions adds libp2p options to the host.
func WithHostLibp2pOptions(opts ...libp2p.Option) HostOption {
	return func(c *hostCfg) error {
		if c.libp2pOptions == nil {
			c.libp2pOptions = make([]libp2p.Option, 0)
		}
		c.libp2pOptions = append(c.libp2pOptions, opts...)
		return nil
	}
}

// WithHostDHTOptions adds DHT options to the host.
func WithHostDHTOptions(opts ...dht.Option) HostOption {
	return func(c *hostCfg) error {
		if c.dhtOptions == nil {
			c.dhtOptions = make([]dht.Option, 0)
		}
		c.dhtOptions = append(c.dhtOptions, opts...)
		return nil
	}
}

// WithHostAsClientMode marks the host to operate in client mode (no server DHT bootstrap).
func WithHostAsClientMode() HostOption {
	return func(c *hostCfg) error {
		// client mode implies disabling server ModeAuto (no bootstrap)
		c.dhtOptions = append(c.dhtOptions, dht.Mode(dht.ModeClient))
		c.isClientMode = true
		return nil
	}
}

// WithPubsubDiscovery enables pubsub discovery for the host.
func WithPubsubDiscovery(enable bool) HostOption {
	return func(c *hostCfg) error {
		c.disablePubsubDiscovery = enable
		return nil
	}
}

// setupDHT initializes the DHT and starts peer discovery if applicable.
// It relies on the provided dhtOpts to configure behavior, including bootstrapping.
func setupDHT(ctx context.Context, h host.Host, cfg *hostCfg, userDhtOptions ...dht.Option) (*dht.IpfsDHT, error) { // Use glog.Logger
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

// CreateLibp2pHost creates a new libp2p Host with default settings.
func CreateLibp2pHost(ctx context.Context, opts ...HostOption) (host.Host, error) {
	// apply HostOption to build config
	cfg := &hostCfg{}
	for _, o := range opts {
		if err := o(cfg); err != nil {
			return nil, err
		}
	}
	log := cfg.logger
	libp2pOpts := cfg.libp2pOptions
	dhtOpts := cfg.dhtOptions
	// We'll use a shared variable for the DHT instance
	// to avoid duplication between setupDHT and the routing constructor
	var kadDHT *dht.IpfsDHT
	var dhtErr error
	var dhtOnce sync.Once
	cfg.relayManager = relay.New(ctx, log, nil) // Initialize relay manager

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
				kadDHT, dhtErr = setupDHT(ctx, h, cfg, dhtOpts...) // Pass dhtOpts here
			})
			return kadDHT, dhtErr
		}),

		libp2p.EnableAutoNATv2(), // Enable AutoNATv2
		libp2p.DisableMetrics(),  // Disable metrics collection for performance

		// Use EnableAutoRelayWithPeerSource with our RelayManager
		// This provides a persistent store of relay candidates with smart selection
		// libp2p.EnableAutoRelayWithPeerSource(cfg.relayManager.GetPeerSource()),
	}

	// allow mDNS and relays in client mode by adding an ephemeral listen addr
	if cfg.isClientMode {
		options = append(options,
			libp2p.NoListenAddrs,
			libp2p.EnableRelay(), // Enable only relay transport support in client mode
		)
	} else {
		// add relay service options
		options = append(options,
			libp2p.EnableRelayService(), // Enable Relay Service if publicly reachable
		)
	}

	// add minimum WS listen addr if configured
	options = append(options, libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0/ws"))

	// Add any user-provided options
	options = append(options, libp2pOpts...)

	// Create a new libp2p Host
	h, err := libp2p.New(options...) // Pass combined options
	if err != nil {
		log.Error("Failed to create libp2p host", err)
		return nil, err
	}

	// Set up the relay manager with the host
	cfg.relayManager.UpdateHost(ctx, h)

	// Setup mDNS discovery
	if err := setupMDNS(h, cfg); err != nil {
		log.Error("Failed to setup mDNS", err)
		// Continue anyway, some features may still work
	}

	// Setup built-in pubsub discovery for browser-Go node connectivity
	if !cfg.disablePubsubDiscovery {
		if err := setupBuiltinPubsubDiscovery(ctx, h, cfg); err != nil {
			log.Error("Failed to setup built-in pubsub discovery", err)
			// Continue anyway, other discovery mechanisms will work
		}
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
	cfg *hostCfg
}

// HandlePeerFound connects to peers discovered via mDNS
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	// Skip self
	if pi.ID == n.h.ID() {
		return
	}
	// n.cfg.logger.Debug(fmt.Sprintf("mDNS peer found: %s", pi.ID.String()))

	ctx, cancel := context.WithTimeout(context.Background(), config.PEER_CONNECTION_TIMEOUT)
	defer cancel()

	err := n.h.Connect(ctx, pi)
	if err != nil {
		// Don't log errors for transient connection issues, use Debug
		// n.cfg.logger.Debug(fmt.Sprintf("Failed connecting to mDNS peer %s: %s", pi.ID.String(), err.Error()))
		return
	}
	// Add to relay manager for potential relay usage
	n.cfg.relayManager.AddPeer(pi)
	// n.cfg.logger.Info(fmt.Sprintf("Connected to peer via mDNS: %s", pi.ID.String()))
}

// setupMDNS initializes the mDNS discovery service
func setupMDNS(h host.Host, cfg *hostCfg) error { // Use glog.Logger
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

// findPeersLoop continuously searches for peers using DHT discovery
func findPeersLoop(ctx context.Context, routingDiscovery *drouting.RoutingDiscovery, h host.Host, cfg *hostCfg) { // Use glog.Logger
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

// connectToFoundPeers handles connecting to peers from the discovery channel
func connectToFoundPeers(ctx context.Context, h host.Host, cfg *hostCfg, peerChan <-chan peer.AddrInfo) { // Use glog.Logger
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
		// cfg.logger.Debug(fmt.Sprintf("DHT peer found: %s, addrs: %v", p.ID.String(), addrStrings))

		connectCtx, connectCancel := context.WithTimeout(ctx, config.PEER_CONNECTION_TIMEOUT)
		err := h.Connect(connectCtx, p)
		connectCancel() // Release context resources promptly
		if err != nil {
			// Use Debug level for potentially transient connection errors
			// cfg.logger.Debug(fmt.Sprintf("Failed connecting to DHT peer %s: %s", p.ID.String(), err.Error()))
		} else {
			// cfg.logger.Info(fmt.Sprintf("Connected to DHT peer: %s", p.ID.String()))
			// Add to relay manager for potential relay usage
			cfg.relayManager.AddPeer(p)
		}
	}
}

// setupBuiltinPubsubDiscovery sets up the built-in libp2p pubsub discovery
func setupBuiltinPubsubDiscovery(ctx context.Context, h host.Host, cfg *hostCfg) error {
	cfg.logger.Info("Setting up built-in pubsub discovery for browser-Go node connectivity")

	// Create a basic pubsub instance with built-in discovery
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return fmt.Errorf("failed to create pubsub for discovery: %w", err)
	}

	// Join the discovery topic that browsers will use
	topic, err := ps.Join(config.DISCOVERY_PUBSUB_TOPIC)
	if err != nil {
		return fmt.Errorf("failed to join pubsub discovery topic: %w", err)
	}

	// Subscribe to allow participation in the discovery protocol
	subscription, err := topic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to pubsub discovery topic: %w", err)
	}

	cfg.logger.Info(fmt.Sprintf("Joined pubsub discovery topic: %s", config.DISCOVERY_PUBSUB_TOPIC))

	// Start broadcasting our presence periodically
	go func() {
		defer subscription.Cancel()

		// TODO: Take this broadcasting time as input in the discovery input; default to 30 seconds
		ticker := time.NewTicker(5 * time.Second) // Broadcast every 30 seconds
		defer ticker.Stop()

		// Helper function to create protobuf-compatible peer data
		createPeerData := func() (*pb.Peer, error) {
			// Get our public key
			pubKey, err := h.ID().ExtractPublicKey()
			if err != nil {
				return nil, fmt.Errorf("failed to extract public key from peer ID: %w", err)
			}

			// Marshal public key to bytes (libp2p protobuf format)
			pubKeyBytes, err := pubKey.Raw()
			if err != nil {
				return nil, fmt.Errorf("failed to get raw public key: %w", err)
			}

			// Get our addresses as bytes
			var addrBytes [][]byte
			for _, addr := range h.Addrs() {
				addrBytes = append(addrBytes, addr.Bytes())
			}

			return &pb.Peer{
				PublicKey: pubKeyBytes,
				Addrs:     addrBytes,
			}, nil
		}

		// Create initial peer data
		peerData, err := createPeerData()
		if err != nil {
			cfg.logger.Error("Failed to create peer data", err)
			return
		}

		// Marshal to protobuf bytes
		msgBytes, err := proto.Marshal(peerData)
		if err != nil {
			cfg.logger.Error("Failed to marshal peer data", err)
			return
		}

		// Check if there are subscribers before broadcasting
		if len(topic.ListPeers()) == 0 {
			// cfg.logger.Debug("Skipping initial broadcast: no peers subscribed to discovery topic")
		} else {
			// Send initial discovery message
			if err := topic.Publish(ctx, msgBytes); err != nil {
				cfg.logger.Error("Failed to publish initial discovery message", err)
			} else {
				cfg.logger.Info(fmt.Sprintf("Published initial discovery message (peer ID: %s, addrs: %d)",
					h.ID().String(), len(peerData.Addrs)))
			}
		}

		// Start periodic broadcasting
		for {
			select {
			case <-ctx.Done():
				cfg.logger.Info("Stopping pubsub discovery broadcasting due to context cancellation")
				return
			case <-ticker.C:
				// Check if there are subscribers
				peers := topic.ListPeers()
				if len(peers) == 0 {
					// cfg.logger.Debug("Skipping broadcast: no peers subscribed to discovery topic")
					continue
				}

				// Create updated peer data (addresses might have changed)
				peerData, err := createPeerData()
				if err != nil {
					cfg.logger.Error("Failed to create peer data", err)
					continue
				}

				// Marshal updated peer data
				msgBytes, err := proto.Marshal(peerData)
				if err != nil {
					cfg.logger.Error("Failed to marshal peer data", err)
					continue
				}

				// Publish discovery message
				if err := topic.Publish(ctx, msgBytes); err != nil {
					cfg.logger.Error("Failed to publish discovery message", err)
				} else {
					// cfg.logger.Debug(fmt.Sprintf("Published discovery message to %d peers (peer ID: %s, addrs: %d)",
						// len(peers), h.ID().String(), len(peerData.Addrs)))
				}
			}
		}
	}()

	return nil
}
